package artifact

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/specgate/doc-registry/internal/governanceprofile"
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
	GetObject(ctx context.Context, key string) ([]byte, error)
	PresignGet(ctx context.Context, key string) (string, error)
	DeleteObject(ctx context.Context, key string) error
}

type FilenameFunc func(featureID, version, filename string) string

var _ Service = (*RegistryService)(nil)
var _ ReadinessService = (*RegistryService)(nil)

type RegistryService struct {
	repo      Repository
	store     ObjectStore
	objectKey FilenameFunc
	urlTTL    time.Duration
	now       func() time.Time
}

func NewService(repo Repository, store ObjectStore, objectKey FilenameFunc, urlTTL time.Duration) *RegistryService {
	return &RegistryService{
		repo:      repo,
		store:     store,
		objectKey: objectKey,
		urlTTL:    urlTTL,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

// validDocPath returns true when p is a safe relative path.
// Rejects empty, absolute, path-traversal, and backslash paths.
func validDocPath(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "..") || strings.ContainsRune(p, '\\') {
		return false
	}
	return true
}

func (s *RegistryService) Publish(ctx context.Context, in PublishInput) (*Artifact, error) {
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
	storageScopeID := in.FeatureID
	if strings.TrimSpace(storageScopeID) == "" {
		storageScopeID = "standalone/" + id
	}
	uploaded := make([]string, 0, len(in.Documents))
	files := make([]File, 0, len(in.Documents))

	for _, doc := range in.Documents {
		if !validDocPath(doc.Path) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidPath, doc.Path)
		}
		role := NormalizeRole(doc.Role)
		if len(doc.Content) > 0 {
			objectKey := s.objectKey(storageScopeID, in.Version, doc.Path)
			if err := s.store.PutObject(ctx, objectKey, doc.Content); err != nil {
				return nil, err
			}
			uploaded = append(uploaded, objectKey)
			files = append(files, File{
				ArtifactID: id,
				Path:       doc.Path,
				Role:       role,
				S3Path:     objectKey,
				SizeBytes:  int64(len(doc.Content)),
			})
		} else if doc.ResolvedS3Path != "" {
			files = append(files, File{
				ArtifactID: id,
				Path:       doc.Path,
				Role:       role,
				S3Path:     doc.ResolvedS3Path,
				SizeBytes:  doc.ResolvedSizeBytes,
			})
		}
		// If neither Content nor a resolved ref is present, skip silently
		// (callers with refs resolve before invoking service).
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
		ID:                       id,
		FeatureID:                in.FeatureID,
		Version:                  in.Version,
		Status:                   in.Status,
		RequestType:              in.RequestType,
		ImpactLevel:              in.ImpactLevel,
		ArtifactPhase:            in.ArtifactPhase,
		ArtifactCompleteness:     in.ArtifactCompleteness,
		ConfidenceScore:          in.ConfidenceScore,
		AmbiguityScore:           in.AmbiguityScore,
		GovernanceVersion:        in.GovernanceVersion,
		CreatedBy:                createdBy,
		CreatedAt:                now,
		UpdatedAt:                now,
		SourceKind:               in.SourceKind,
		SourceID:                 in.SourceID,
		SourceRevision:           in.SourceRevision,
		Authority:                in.Authority,
		GatesProfile:             in.GatesProfile,
		GatesProfileVersion:      in.GatesProfileVersion,
		GatesProfileDigest:       in.GatesProfileDigest,
		GatesProfileSnapshotJSON: in.GatesProfileSnapshotJSON,
		ParentArtifactID:         in.ParentArtifactID,
		LineageRootID:            in.LineageRootID,
		Services:                 services,
		Files:                    files,
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
		for _, key := range uploaded {
			_ = s.store.DeleteObject(ctx, key)
		}
		return nil, err
	}
	return a, nil
}

func (s *RegistryService) Get(ctx context.Context, id string) (*Artifact, error) {
	return s.repo.Get(ctx, id)
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
	for _, f := range a.Files {
		if f.S3Path != "" {
			_ = s.store.DeleteObject(ctx, f.S3Path)
		}
	}
	return s.repo.Delete(ctx, id)
}

func (s *RegistryService) UpdateStatus(ctx context.Context, id string, in StatusUpdate) (*Artifact, error) {
	// actor_kind guard: for the draft→approved transition, check the profile's
	// approval_policy before writing. Fetch here (before the write) so we can
	// return early without a partial state change.
	// This is a cooperative surface check (actor_kind is client-asserted); there
	// is no server-side identity enforcement — the human surface is expected to
	// perform approvals under human_required, not a server identity gate.
	if in.Status == StatusApproved && in.ActorKind == "agent" {
		current, err := s.repo.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if current.GatesProfileSnapshotJSON != "" {
			// Use ParseSnapshot (fail-closed) instead of a direct inline unmarshal.
			// The old `if err == nil` pattern was fail-open: a corrupt or
			// unsupported snapshot silently fell through to "allow agent approval".
			// ParseSnapshot returns an error on corrupt JSON or an unrecognised
			// schema version — both cases must block agent approval (per spec §7).
			snap, err := governanceprofile.ParseSnapshot(current.GatesProfileSnapshotJSON)
			if err != nil {
				// Corrupt or unsupported snapshot → fail closed.
				return nil, ErrApprovalRequiresHuman
			}
			if governanceprofile.EffectiveApprovalPolicy(snap.ApprovalPolicy) == "human_required" {
				return nil, ErrApprovalRequiresHuman
			}
		} else {
			// No snapshot → default to human_required (safe default per spec).
			return nil, ErrApprovalRequiresHuman
		}
	}

	payload := mustJSON(map[string]any{
		"status":        in.Status,
		"actor":         in.Actor,
		"review_rating": in.ReviewRating,
		"note":          in.Note,
	})
	ev := Event{
		ID:         uuid.NewString(),
		ArtifactID: id,
		EventType:  EventTypeForStatus(in.Status),
		Payload:    payload,
		CreatedAt:  s.now(),
	}
	// per spec §14: status change and state-transition event are persisted in one transaction.
	if err := s.repo.UpdateStatus(ctx, id, in.Status, in.Actor, ev); err != nil {
		return nil, err
	}
	a, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Manifest != "" {
		for _, f := range a.Files {
			if f.Path == "manifest.json" {
				if err := s.store.PutObject(ctx, f.S3Path, []byte(in.Manifest)); err != nil {
					return nil, err
				}
				break
			}
		}
	}
	return a, nil
}

func (s *RegistryService) SignedFileURL(ctx context.Context, id string, path string) (*SignedFile, error) {
	a, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, f := range a.Files {
		if f.Path != path {
			continue
		}
		url, err := s.store.PresignGet(ctx, f.S3Path)
		if err != nil {
			return nil, err
		}
		return &SignedFile{
			URL:       url,
			ExpiresAt: s.now().Add(s.urlTTL),
			SizeBytes: f.SizeBytes,
		}, nil
	}
	return nil, fmt.Errorf("%w: file %q not found for artifact %s", ErrFileNotFound, path, id)
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
		return s.store.GetObject(ctx, f.S3Path)
	}
	return nil, fmt.Errorf("%w: file %q not found for artifact %s", ErrFileNotFound, path, id)
}

func (s *RegistryService) CheckConflicts(ctx context.Context, services []string) (*ConflictReport, error) {
	existing, err := s.repo.FindOverlappingServices(ctx, services, "")
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
			EvidenceJSON: strings.TrimSpace(eval.Evidence),
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

// compareVersion compares two vMAJOR.MINOR strings; unparseable sorts lowest.
func compareVersion(a, b string) int {
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
		if latest == nil || compareVersion(items[i].Version, latest.Version) > 0 {
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
	if latest == nil || compareVersion(latest.Version, baseVersion) != 0 {
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
	if latest == nil || compareVersion(latest.Version, baseVersion) != 0 {
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
