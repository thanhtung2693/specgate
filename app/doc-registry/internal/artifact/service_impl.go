package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/workspace"
)

type Repository interface {
	InsertWithEvent(context.Context, *Artifact, Event) error
	Get(context.Context, string) (*Artifact, error)
	List(context.Context, ListFilter) ([]Artifact, error)
	Count(context.Context, ListFilter) (int64, error)
	InsertReadinessRuns(context.Context, []ReadinessRun) error
	ListReadinessRuns(context.Context, string, int) ([]ReadinessRun, error)
	UpdateStatus(context.Context, string, Status, string, Event) error
	Delete(context.Context, string) error
	FindOverlappingServices(context.Context, []string, string) ([]Artifact, error)
	ListEvents(context.Context, EventFilter) ([]Event, error)
}

type ObjectStore interface {
	PutObject(ctx context.Context, key string, body []byte) error
	GetObject(ctx context.Context, key string, maxBytes int64) ([]byte, error)
	DeleteObject(ctx context.Context, key string) error
}

type FilenameFunc func(artifactID, version, filename string) string

var _ Service = (*RegistryService)(nil)

type RegistryService struct {
	repo      Repository
	store     ObjectStore
	objectKey FilenameFunc
	now       func() time.Time
}

func NewService(repo Repository, store ObjectStore, objectKey FilenameFunc) *RegistryService {
	return &RegistryService{
		repo:      repo,
		store:     store,
		objectKey: objectKey,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

// normalizeDocPath returns a safe normalized repository-relative POSIX path.
// Rejects empty, absolute, path-traversal, and backslash paths.
func normalizeDocPath(p string) (string, bool) {
	p = strings.TrimSpace(p)
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) || strings.ContainsRune(p, '\\') {
		return "", false
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == ".." {
			return "", false
		}
	}
	clean := path.Clean(p)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

func digestBytes(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func scopedObjectKey(workspaceID, key string) string {
	return "workspaces/" + workspaceID + "/" + strings.TrimPrefix(key, "/")
}

func snapshotDigest(files []File) string {
	ordered := append([]File(nil), files...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Path != ordered[j].Path {
			return ordered[i].Path < ordered[j].Path
		}
		return ordered[i].Role < ordered[j].Role
	})
	h := sha256.New()
	for _, file := range ordered {
		fmt.Fprintf(h, "%s\x00%s\x00%s\n", file.Path, file.Role, file.ContentSHA256)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func (s *RegistryService) Publish(ctx context.Context, in PublishInput) (*Artifact, error) {
	if workspaceID, ok := WorkspaceFromContext(ctx); ok {
		if strings.TrimSpace(in.WorkspaceID) == "" {
			in.WorkspaceID = workspaceID
		} else if strings.TrimSpace(in.WorkspaceID) != workspaceID {
			return nil, ErrWorkspaceMismatch
		}
	}
	var validWorkspace bool
	if in.WorkspaceID, validWorkspace = workspace.NormalizeID(in.WorkspaceID); !validWorkspace {
		return nil, ErrWorkspaceRequired
	}
	// Optimistic lock: when a base version is supplied, reject unless it matches
	// the feature's current latest. The caller's explicit Version is preserved;
	// this only gates whether the write proceeds. Empty base skips the check.
	if in.BaseVersion != "" {
		if err := s.requireFreshBase(ctx, in.FeatureID, in.BaseVersion); err != nil {
			return nil, err
		}
	}
	now := s.now()
	id := uuid.NewString()
	type preparedDocument struct {
		path       string
		role       Role
		content    []byte
		objectKey  string
		contentSHA string
	}
	prepared := make([]preparedDocument, 0, len(in.Documents))
	for _, doc := range in.Documents {
		normalizedPath, valid := normalizeDocPath(doc.Path)
		if !valid {
			return nil, fmt.Errorf("%w: %q", ErrInvalidPath, doc.Path)
		}
		if doc.Content != nil {
			objectKey := scopedObjectKey(in.WorkspaceID, s.objectKey(id, in.Version, normalizedPath))
			prepared = append(prepared, preparedDocument{
				path:       normalizedPath,
				role:       NormalizeRole(doc.Role),
				content:    doc.Content,
				objectKey:  objectKey,
				contentSHA: digestBytes(doc.Content),
			})
		}
		// nil content means no inline snapshot and is skipped. A non-nil,
		// zero-length document is a valid immutable file.
	}
	uploaded := make([]string, 0, len(prepared))
	cleanupUploaded := true
	defer func() {
		if !cleanupUploaded {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		for _, key := range uploaded {
			_ = s.store.DeleteObject(cleanupCtx, key)
		}
	}()
	files := make([]File, 0, len(prepared))
	for _, doc := range prepared {
		if err := s.store.PutObject(ctx, doc.objectKey, doc.content); err != nil {
			return nil, err
		}
		uploaded = append(uploaded, doc.objectKey)
		files = append(files, File{
			ArtifactID:    id,
			Path:          doc.path,
			Role:          doc.role,
			ObjectKey:     doc.objectKey,
			SizeBytes:     int64(len(doc.content)),
			ContentSHA256: doc.contentSHA,
		})
	}

	services := make([]ServiceRef, 0, len(in.ImpactedServices))
	for _, svc := range in.ImpactedServices {
		if svc.Name == "" {
			continue
		}
		services = append(services, ServiceRef{
			ArtifactID: id,
			Name:       svc.Name,
			Kind:       svc.Kind,
		})
	}

	createdBy := in.CreatedBy
	if createdBy == "" {
		createdBy = "governance-ops"
	}
	a := &Artifact{
		ID:                   id,
		WorkspaceID:          in.WorkspaceID,
		FeatureID:            in.FeatureID,
		Version:              in.Version,
		Status:               in.Status,
		RequestType:          in.RequestType,
		ImpactLevel:          in.ImpactLevel,
		ArtifactPhase:        in.ArtifactPhase,
		ArtifactCompleteness: in.ArtifactCompleteness,
		ConfidenceScore:      in.ConfidenceScore,
		AmbiguityScore:       in.AmbiguityScore,
		GovernanceVersion:    in.GovernanceVersion,
		CreatedBy:            createdBy,
		CreatedAt:            now,
		UpdatedAt:            now,
		SourceKind:           in.SourceKind,
		SourceID:             in.SourceID,
		SourceRevision:       in.SourceRevision,
		Authority:            in.Authority,
		SnapshotDigest:       snapshotDigest(files),
		PolicyVersion:        in.PolicyVersion,
		PolicyDigest:         in.PolicyDigest,
		PolicySnapshotJSON:   in.PolicySnapshotJSON,
		ParentArtifactID:     in.ParentArtifactID,
		LineageRootID:        in.LineageRootID,
		Services:             services,
		Files:                files,
	}
	if a.Status == "" {
		a.Status = StatusDraft
	}
	if a.ArtifactPhase == "" {
		a.ArtifactPhase = ArtifactPhasePhase1
	}
	if a.ArtifactCompleteness == "" {
		a.ArtifactCompleteness = ArtifactCompletenessPartial
	}
	// First artifact in a chain roots to itself.
	if a.LineageRootID == "" {
		a.LineageRootID = a.ID
	}
	payload := mustJSON(map[string]any{
		"feature_id": a.FeatureID,
		"version":    a.Version,
		"status":     a.Status,
	})
	ev := Event{
		ID:         uuid.NewString(),
		ArtifactID: id,
		EventType:  EventPublished,
		Payload:    payload,
		CreatedAt:  now,
	}
	// per spec §14: artifact row and publish event are persisted in one transaction.
	if err := s.repo.InsertWithEvent(ctx, a, ev); err != nil {
		return nil, err
	}
	cleanupUploaded = false
	return a, nil
}

func (s *RegistryService) Get(ctx context.Context, id string) (*Artifact, error) {
	return s.repo.Get(ctx, id)
}

func (s *RegistryService) GetInWorkspace(ctx context.Context, workspaceID, id string) (*Artifact, error) {
	type scopedGetter interface {
		GetInWorkspace(context.Context, string, string) (*Artifact, error)
	}
	if repo, ok := s.repo.(scopedGetter); ok {
		return repo.GetInWorkspace(ctx, workspaceID, id)
	}
	a, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(workspaceID) == "" || a.WorkspaceID != workspaceID {
		return nil, ErrNotFound
	}
	return a, nil
}

func (s *RegistryService) List(ctx context.Context, f ListFilter) ([]Artifact, error) {
	return s.repo.List(ctx, f)
}

func (s *RegistryService) Count(ctx context.Context, f ListFilter) (int64, error) {
	return s.repo.Count(ctx, f)
}

func (s *RegistryService) Delete(ctx context.Context, id string) error {
	a, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for _, f := range a.Files {
		if f.ObjectKey != "" {
			_ = s.store.DeleteObject(cleanupCtx, f.ObjectKey)
		}
	}
	return nil
}

func (s *RegistryService) UpdateStatus(ctx context.Context, id string, in StatusUpdate) (*Artifact, error) {
	eventType := EventTypeForStatus(in.Status)
	if eventType == "" {
		return nil, fmt.Errorf("%w: %q", ErrInvalidStatus, in.Status)
	}
	actorKind := strings.TrimSpace(in.ActorKind)
	if actorKind == "" {
		actorKind = "human"
	}
	// Approval guards run before writing so a refused approval leaves no partial
	// state change. This is cooperative over the client-asserted actor kind; the
	// trusted local surface still lacks authentication.
	if in.Status == StatusApproved {
		current, err := s.repo.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if strings.EqualFold(actorKind, "agent") {
			return nil, ErrApprovalRequiresHuman
		}
		if current.PolicySnapshotJSON == "" {
			return nil, ErrUnsupportedApprovalPolicy
		}
		snap, err := governanceprofile.ParseSnapshot(current.PolicySnapshotJSON)
		if err != nil || !governanceprofile.KnownApprovalPolicies[snap.ApprovalPolicy] {
			return nil, ErrUnsupportedApprovalPolicy
		}
	}

	payload := mustJSON(map[string]any{
		"status":        in.Status,
		"actor":         in.Actor,
		"actor_kind":    actorKind,
		"review_rating": in.ReviewRating,
		"note":          in.Note,
	})
	ev := Event{
		ID:         uuid.NewString(),
		ArtifactID: id,
		EventType:  eventType,
		Payload:    payload,
		CreatedAt:  s.now(),
	}
	// per spec §14: status change and state-transition event are persisted in one transaction.
	if err := s.repo.UpdateStatus(ctx, id, in.Status, in.Actor, ev); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, id)
}

func (s *RegistryService) FileContent(ctx context.Context, id string, path string) ([]byte, error) {
	a, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, f := range a.Files {
		if f.Path != path {
			continue
		}
		if f.SizeBytes < 0 {
			return nil, fmt.Errorf("artifact file %q has invalid size", path)
		}
		readLimit := f.SizeBytes
		if readLimit == 0 {
			readLimit = 1
		}
		return s.store.GetObject(ctx, f.ObjectKey, readLimit)
	}
	return nil, fmt.Errorf("%w: file %q not found for artifact %s", ErrFileNotFound, path, id)
}

func (s *RegistryService) CheckConflicts(ctx context.Context, services []string) (*ConflictReport, error) {
	existing, err := s.repo.FindOverlappingServices(ctx, services, "")
	return conflictReport(services, existing, err)
}

// CheckConflictsInWorkspace is the workspace-bound conflict path used by product APIs.
func (s *RegistryService) CheckConflictsInWorkspace(ctx context.Context, workspaceID string, services []string) (*ConflictReport, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, ErrNotFound
	}
	existing, err := s.repo.List(ctx, ListFilter{WorkspaceID: workspaceID})
	return conflictReport(services, existing, err)
}

func conflictReport(services []string, existing []Artifact, err error) (*ConflictReport, error) {
	if err != nil {
		return nil, err
	}
	report := &ConflictReport{State: "no_conflict"}
	for _, a := range existing {
		overlap := overlappingServices(services, a.Services)
		if len(overlap) == 0 {
			continue
		}
		conflictType := "warning_conflict"
		if a.Status == StatusDraft || a.Status == StatusApproved {
			conflictType = "blocking_conflict"
			report.State = "blocking_conflict"
		} else if report.State == "no_conflict" {
			report.State = "warning_conflict"
		}
		report.Conflicts = append(report.Conflicts, Conflict{
			ID:                  uuid.NewString(),
			Type:                conflictType,
			Existing:            a,
			OverlappingServices: overlap,
			ResolutionOptions: []string{
				"narrow impacted services",
				"wait for active artifact review",
				"coordinate and supersede the older artifact",
			},
		})
	}
	return report, nil
}

func (s *RegistryService) RefreshReadinessRuns(ctx context.Context, artifactID string, evaluations []ReadinessEvaluation) ([]ReadinessRun, error) {
	if strings.TrimSpace(artifactID) == "" {
		return nil, ErrNotFound
	}
	if _, err := s.repo.Get(ctx, artifactID); err != nil {
		return nil, err
	}
	now := s.now()
	rows := make([]ReadinessRun, 0, len(evaluations))
	for _, eval := range evaluations {
		gate := strings.TrimSpace(eval.Gate)
		if gate == "" {
			continue
		}
		rows = append(rows, ReadinessRun{
			ID:           uuid.NewString(),
			ArtifactID:   artifactID,
			Gate:         gate,
			State:        eval.State,
			Hint:         strings.TrimSpace(eval.Hint),
			Executor:     "platform",
			EvidenceJSON: readinessEvidenceJSON(eval, gate),
			CreatedAt:    now,
		})
	}
	if len(rows) == 0 {
		return rows, nil
	}
	if err := s.repo.InsertReadinessRuns(ctx, rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// readinessEvidenceJSON preserves the judge metadata the caller supplied by
// wrapping it into the same gate-run-v1 envelope the workboard path persists
// (per spec: a stored run must say who judged it and with what confidence).
// Evidence that is already an envelope, and evaluations with no judge
// metadata, pass through unchanged.
func readinessEvidenceJSON(eval ReadinessEvaluation, gate string) string {
	evidence := strings.TrimSpace(eval.Evidence)
	if strings.TrimSpace(eval.JudgeModel) == "" && eval.Confidence == 0 {
		return evidence
	}
	var envelope struct {
		Version string `json:"evidence_contract_version"`
	}
	if json.Unmarshal([]byte(evidence), &envelope) == nil && envelope.Version == "gate-run-v1" {
		return evidence
	}
	payload, err := json.Marshal(map[string]any{
		"evidence_contract_version": "gate-run-v1",
		"gate":                      gate,
		"evaluator": map[string]any{
			"type":               "platform_llm",
			"judge_model":        strings.TrimSpace(eval.JudgeModel),
			"eval_suite_version": strings.TrimSpace(eval.EvalSuiteVersion),
		},
		"verdict":    string(eval.State),
		"confidence": eval.Confidence,
		"evidence":   evidence,
	})
	if err != nil {
		return evidence
	}
	return string(payload)
}

func (s *RegistryService) ListReadinessRuns(ctx context.Context, artifactID string, limit int) ([]ReadinessRun, error) {
	return s.repo.ListReadinessRuns(ctx, artifactID, limit)
}

func (s *RegistryService) ListEvents(ctx context.Context, f EventFilter) ([]Event, error) {
	return s.repo.ListEvents(ctx, f)
}

// bumpVersion returns the next vMAJOR.MINOR after prev ("" -> v0.1; v0.1 -> v0.2; v0.9 -> v0.10).
func bumpVersion(prev string) string {
	p := strings.TrimSpace(prev)
	if p == "" {
		return "v0.1"
	}
	p = strings.TrimPrefix(p, "v")
	major, minor := 0, 0
	if _, err := fmt.Sscanf(p, "%d.%d", &major, &minor); err != nil {
		return "v0.1"
	}
	return fmt.Sprintf("v%d.%d", major, minor+1)
}

// CompareVersion compares two vMAJOR.MINOR strings; unparseable sorts lowest.
func CompareVersion(a, b string) int {
	pa := func(s string) (int, int) {
		s = strings.TrimPrefix(strings.TrimSpace(s), "v")
		maj, min := -1, -1
		fmt.Sscanf(s, "%d.%d", &maj, &min) //nolint:errcheck
		return maj, min
	}
	amaj, amin := pa(a)
	bmaj, bmin := pa(b)
	if amaj != bmaj {
		if amaj < bmaj {
			return -1
		}
		return 1
	}
	if amin != bmin {
		if amin < bmin {
			return -1
		}
		return 1
	}
	return 0
}

// LatestArtifact returns the highest-version artifact for a feature, or nil if none.
func (s *RegistryService) LatestArtifact(ctx context.Context, featureID string) (*Artifact, error) {
	items, err := s.repo.List(ctx, ListFilter{FeatureID: featureID, Limit: 1000})
	if err != nil {
		return nil, err
	}
	var latest *Artifact
	for i := range items {
		if latest == nil || CompareVersion(items[i].Version, latest.Version) > 0 {
			latest = &items[i]
		}
	}
	return latest, nil
}

// NextVersion returns the next version string for a feature based on its latest artifact.
func (s *RegistryService) NextVersion(ctx context.Context, featureID string) (string, error) {
	latest, err := s.LatestArtifact(ctx, featureID)
	if err != nil {
		return "", err
	}
	if latest == nil {
		return bumpVersion(""), nil
	}
	return bumpVersion(latest.Version), nil
}

// ResolveNextVersion derives the next version for a feature, optionally guarded
// by an optimistic lock. With an empty base it bumps the latest version (or
// returns the initial version when none exists). With a base it requires the
// base to equal the current latest, returning ErrStaleBase otherwise — letting
// the caller re-fetch and rebase instead of silently overwriting.
func (s *RegistryService) ResolveNextVersion(ctx context.Context, featureID string, baseVersion string) (string, error) {
	if baseVersion == "" {
		return s.NextVersion(ctx, featureID)
	}
	latest, err := s.LatestArtifact(ctx, featureID)
	if err != nil {
		return "", err
	}
	if latest == nil || CompareVersion(latest.Version, baseVersion) != 0 {
		return "", ErrStaleBase
	}
	return bumpVersion(latest.Version), nil
}

// requireFreshBase returns ErrStaleBase unless baseVersion equals the feature's
// current latest version.
func (s *RegistryService) requireFreshBase(ctx context.Context, featureID string, baseVersion string) error {
	latest, err := s.LatestArtifact(ctx, featureID)
	if err != nil {
		return err
	}
	if latest == nil || CompareVersion(latest.Version, baseVersion) != 0 {
		return ErrStaleBase
	}
	return nil
}

func overlappingServices(want []string, refs []ServiceRef) []string {
	lookup := map[string]bool{}
	for _, svc := range want {
		lookup[svc] = true
	}
	out := []string{}
	for _, ref := range refs {
		if ref.Kind == "service" && lookup[ref.Name] {
			out = append(out, ref.Name)
		}
	}
	return out
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}
