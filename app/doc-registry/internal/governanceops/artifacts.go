package governanceops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/artifactedit"
	"github.com/specgate/doc-registry/internal/governanceprofile"
)

const codingAgentUpdateSourceKind = "coding_agent_update"

// PublishArtifact creates or updates a governed artifact for a feature.
// It upserts the feature by key, resolves the next version, applies the
// governance profile, and lands the artifact as a draft (or auto-approves it
// when the profile allows). On stale base_version it returns ErrVersionConflict.
func (s *Service) PublishArtifact(ctx context.Context, in PublishArtifactInput) (*PublishArtifactResult, error) {
	if s.ArtifactWriter == nil {
		return nil, fmt.Errorf("artifact writer is not configured")
	}
	if s.FeatureUpserter == nil {
		return nil, fmt.Errorf("feature upserter is not configured")
	}
	if s.ProfileResolver == nil {
		return nil, fmt.Errorf("profile resolver is not configured")
	}
	key := strings.TrimSpace(in.FeatureKey)
	if key == "" {
		return nil, fmt.Errorf("feature_key is required")
	}

	feat, err := s.FeatureUpserter.UpsertFeatureByKey(ctx, key, in.FeatureName)
	if err != nil {
		return nil, err
	}

	latest, err := s.ArtifactWriter.LatestArtifact(ctx, feat.ID)
	if err != nil {
		return nil, err
	}

	base := strings.TrimSpace(in.BaseVersion)
	var version string
	if base == "" {
		version, err = s.ArtifactWriter.NextVersion(ctx, feat.ID)
		if err != nil {
			return nil, err
		}
	} else {
		version, err = s.ArtifactWriter.ResolveNextVersion(ctx, feat.ID, base)
		if errors.Is(err, artifact.ErrStaleBase) {
			current := "(none)"
			if latest != nil {
				current = latest.Version
			}
			return nil, fmt.Errorf("%w: base_version %q is stale; current latest is %s — re-fetch and rebase", ErrVersionConflict, base, current)
		}
		if err != nil {
			return nil, err
		}
	}

	var parentID, lineageRoot string
	if latest != nil {
		parentID = latest.ID
		lineageRoot = latest.LineageRootID
	}

	docs := make([]artifact.DocumentInput, 0, len(in.Documents))
	for _, d := range in.Documents {
		docs = append(docs, artifact.DocumentInput{
			Path:    d.Path,
			Role:    string(artifact.NormalizeRole(d.Role)),
			Content: []byte(d.Content),
		})
	}

	impact := strings.TrimSpace(in.ImpactLevel)
	if impact == "" {
		impact = "medium"
	}
	authority := strings.TrimSpace(in.Authority)
	if authority == "" {
		authority = "product_intent"
	}
	reqType := strings.TrimSpace(in.RequestType)
	if reqType == "" {
		if latest == nil {
			reqType = string(artifact.RequestTypeNewFeature)
		} else {
			reqType = string(artifact.RequestTypeChangeRequest)
		}
	}

	resolved, err := s.ProfileResolver.ResolveProfile(ctx, governanceprofile.ResolveInput{
		RequestedKey:             in.GatesProfile,
		RequestType:              reqType,
		ImpactLevel:              impact,
		RequestedGovernanceLevel: governanceprofile.GovernanceLevel(strings.TrimSpace(in.RequestedGovernanceLevel)),
		ImpactDeclaration:        in.ImpactDeclaration,
	})
	if err != nil {
		return nil, err
	}
	// Stamp the v1 schema-version marker on the resolved profile before
	// serializing. This makes the persisted snapshot parseable by ParseSnapshot,
	// which enables fail-closed approval guards for later reads. The marker is
	// not set by the resolver itself to keep the resolver output schema-agnostic.
	resolved.SnapshotSchemaVersion = governanceprofile.SnapshotSchemaPolicyV1
	snapshotJSON, err := resolved.SnapshotJSON()
	if err != nil {
		return nil, err
	}
	explanation := governanceprofile.ExplainSnapshot(*resolved)

	art, err := s.ArtifactWriter.Publish(ctx, artifact.PublishInput{
		FeatureID:                feat.ID,
		Version:                  version,
		Status:                   artifact.StatusDraft,
		ArtifactCompleteness:     artifact.ArtifactCompletenessFull,
		Documents:                docs,
		ParentArtifactID:         parentID,
		LineageRootID:            lineageRoot,
		SourceKind:               in.SourceKind,
		SourceRevision:           in.SourceRevision,
		SourceID:                 in.SourceID,
		Authority:                authority,
		GatesProfile:             resolved.FullKey,
		GatesProfileVersion:      resolved.Version,
		GatesProfileDigest:       resolved.Digest,
		GatesProfileSnapshotJSON: snapshotJSON,
		ImpactLevel:              artifact.ImpactLevel(impact),
		RequestType:              artifact.RequestType(reqType),
		CreatedBy:                "specgate-ide",
	})
	if err != nil {
		return nil, err
	}

	resultStatus := "draft"
	if governanceprofile.EffectiveApprovalPolicy(resolved.ApprovalPolicy) == "auto" {
		if _, err := s.ArtifactWriter.UpdateStatus(ctx, art.ID, artifact.StatusUpdate{
			Status:    artifact.StatusApproved,
			Actor:     "specgate-ide",
			ActorKind: "agent",
		}); err != nil {
			return nil, fmt.Errorf("auto-approve: %w", err)
		}
		resultStatus = "approved"
	}

	missingRoles := publishMissingRoles(resolved.RequiredRoles, docs)
	readinessHint := "ready: all required roles present"
	if len(missingRoles) > 0 {
		readinessHint = "missing required roles: " + strings.Join(missingRoles, ", ")
	}

	reviewURL := strings.TrimRight(s.AppBaseURL, "/") + "/artifacts/" + art.ID

	result := &PublishArtifactResult{
		ArtifactID:      art.ID,
		FeatureKey:      in.FeatureKey,
		Version:         version,
		Status:          resultStatus,
		ReviewURL:       reviewURL,
		MissingRoles:    missingRoles,
		ReadinessHint:   readinessHint,
		ApprovalPolicy:  governanceprofile.EffectiveApprovalPolicy(resolved.ApprovalPolicy),
		EvidencePolicy:  governanceprofile.EffectiveEvidencePolicy(resolved.EvidencePolicy),
		WorkType:        resolved.WorkType,
		RiskLevel:       resolved.RiskLevel,
		GovernanceLevel: string(resolved.GovernanceLevel),
		GovernanceWhy:   resolved.ReasonCodes,
	}
	result.PolicyExplanation = &explanation
	return result, nil
}

// publishMissingRoles returns the required roles from the governance profile that
// are absent from the published documents. Non-blocking: the draft still lands.
func publishMissingRoles(requiredRoles []string, docs []artifact.DocumentInput) []string {
	present := make(map[string]struct{}, len(docs))
	for _, d := range docs {
		if role := strings.TrimSpace(d.Role); role != "" {
			present[role] = struct{}{}
		}
	}
	missing := make([]string, 0)
	seen := make(map[string]struct{}, len(requiredRoles))
	for _, raw := range requiredRoles {
		role := string(artifact.NormalizeRole(raw))
		if role == "" {
			continue
		}
		if _, dup := seen[role]; dup {
			continue
		}
		seen[role] = struct{}{}
		if _, ok := present[role]; !ok {
			missing = append(missing, role)
		}
	}
	return missing
}

// DraftArtifactUpdate opens a draft-only artifact-edit proposal for a coding agent.
// It reads the base artifact, diffs the provided files, dedupes by source_id or
// content hash, and creates a proposal session if the change is novel.
func (s *Service) DraftArtifactUpdate(ctx context.Context, in DraftArtifactUpdateInput) (*DraftArtifactUpdateResult, error) {
	if s.DraftArtifacts == nil || s.EditStore == nil {
		return nil, fmt.Errorf("artifact draft update not configured")
	}
	artifactID := strings.TrimSpace(in.ArtifactID)
	if artifactID == "" {
		return nil, fmt.Errorf("artifact_id is required")
	}
	summary := strings.TrimSpace(in.Summary)
	if summary == "" {
		return nil, fmt.Errorf("summary is required")
	}
	if len(in.Files) == 0 {
		return nil, fmt.Errorf("files are required")
	}

	baseArtifact, err := s.DraftArtifacts.Get(ctx, artifactID)
	if err != nil {
		return nil, err
	}
	baseFiles := map[string]string{}
	for _, f := range baseArtifact.Files {
		content, err := s.DraftArtifacts.FileContent(ctx, baseArtifact.ID, f.Path)
		if err != nil {
			continue
		}
		baseFiles[f.Path] = string(content)
	}

	normalizedFiles := map[string]string{}
	for key, content := range in.Files {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		normalizedFiles[key] = content
	}
	workingFiles := map[string]string{}
	changedFiles := make([]string, 0, len(normalizedFiles))
	keys := make([]string, 0, len(normalizedFiles))
	for key := range normalizedFiles {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		next := normalizedFiles[key]
		current, ok := baseFiles[key]
		if !ok {
			continue
		}
		if next == current {
			continue
		}
		workingFiles[key] = next
		changedFiles = append(changedFiles, key)
	}
	if len(changedFiles) == 0 {
		return nil, fmt.Errorf("no valid changed files")
	}

	sourceID := strings.TrimSpace(in.DedupeKey)
	if sourceID == "" {
		sourceID = buildDraftUpdateSourceID(artifactID, strings.TrimSpace(in.ChangeRequestID), summary, workingFiles)
	}

	existing, err := findActiveProposalSession(ctx, s.EditStore, artifactID, codingAgentUpdateSourceKind, sourceID)
	if err != nil {
		return nil, err
	}
	if existing != "" {
		return &DraftArtifactUpdateResult{
			Drafted:    false,
			SessionID:  existing,
			SourceKind: codingAgentUpdateSourceKind,
			SourceID:   sourceID,
			Reason:     "duplicate",
		}, nil
	}

	now := time.Now().UTC()
	session := artifactedit.Session{
		ID:              "aes_" + uuid.NewString(),
		BaseArtifactID:  baseArtifact.ID,
		BaseVersion:     baseArtifact.Version,
		State:           "active",
		RequestedBy:     draftUpdateRequestedBy(in.RequestedBy),
		SourceKind:      codingAgentUpdateSourceKind,
		SourceID:        sourceID,
		CompareToken:    uuid.NewString(),
		LastDiffSummary: fmt.Sprintf("%d file(s) changed", len(changedFiles)),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.EditStore.CreateSession(ctx, session, baseFiles, workingFiles); err != nil {
		return nil, err
	}
	return &DraftArtifactUpdateResult{
		Drafted:      true,
		SessionID:    session.ID,
		SourceKind:   codingAgentUpdateSourceKind,
		SourceID:     sourceID,
		ChangedFiles: changedFiles,
	}, nil
}

func buildDraftUpdateSourceID(artifactID, changeRequestID, summary string, files map[string]string) string {
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	hash := sha256.New()
	hash.Write([]byte(artifactID))
	hash.Write([]byte{0})
	hash.Write([]byte(changeRequestID))
	hash.Write([]byte{0})
	hash.Write([]byte(summary))
	for _, key := range keys {
		hash.Write([]byte{0})
		hash.Write([]byte(key))
		hash.Write([]byte{0})
		hash.Write([]byte(files[key]))
	}
	return "cau_" + hex.EncodeToString(hash.Sum(nil))
}

func findActiveProposalSession(ctx context.Context, store ArtifactEditStore, artifactID, sourceKind, sourceID string) (string, error) {
	rows, err := store.ListProposals(ctx)
	if err != nil {
		return "", err
	}
	for _, row := range rows {
		if row.BaseArtifactID == artifactID && row.SourceKind == sourceKind && row.SourceID == sourceID {
			return row.ID, nil
		}
	}
	return "", nil
}

func draftUpdateRequestedBy(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "coding-agent"
	}
	return value
}
