package governanceops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/skills"
	"github.com/specgate/doc-registry/internal/workspace"
)

const (
	maxArtifactDocumentBytes = 1 << 20
	maxArtifactPackageBytes  = 10 << 20
)

// PublishArtifact creates a new governed artifact version for a feature.
// It upserts the feature by key, resolves the next version, applies the
// automatic governance policy, and lands the artifact as a draft for human review. On
// stale base_version it returns ErrVersionConflict.
func (s *Service) PublishArtifact(ctx context.Context, in PublishArtifactInput) (*PublishArtifactResult, error) {
	if s.ArtifactWriter == nil {
		return nil, fmt.Errorf("artifact writer is not configured")
	}
	if s.FeatureUpserter == nil {
		return nil, fmt.Errorf("feature upserter is not configured")
	}
	if s.ProfileResolver == nil {
		return nil, fmt.Errorf("automatic policy resolver is not configured")
	}
	key := strings.TrimSpace(in.FeatureKey)
	if key == "" {
		return nil, fmt.Errorf("feature_key is required")
	}

	workspaceID := strings.TrimSpace(in.WorkspaceID)
	if selected := trustedWorkspace(ctx); selected != "" {
		if workspaceID != "" && workspaceID != selected {
			return nil, fmt.Errorf("%w: workspace_id must match the trusted request workspace", ErrValidation)
		}
		workspaceID = selected
	}
	var validWorkspace bool
	if workspaceID, validWorkspace = workspace.NormalizeID(workspaceID); !validWorkspace {
		return nil, fmt.Errorf("workspace_id is required and must be a safe path segment")
	}
	if err := validateArtifactPackageSize(in.Documents); err != nil {
		return nil, err
	}
	feat, featureCreated, err := s.FeatureUpserter.UpsertFeatureByKeyInWorkspaceForPublish(ctx, workspaceID, key, in.FeatureName)
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
	createdBy := strings.TrimSpace(in.CreatedBy)
	if createdBy == "" {
		createdBy = "specgate-ide"
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
		RequestType:              reqType,
		ImpactLevel:              impact,
		RequestedGovernanceLevel: governanceprofile.GovernanceLevel(strings.TrimSpace(in.RequestedGovernanceLevel)),
		ImpactDeclaration:        in.ImpactDeclaration,
	})
	if err != nil {
		return nil, err
	}
	// Stamp the v1 schema-version marker on the resolved policy before
	// serializing. This makes the persisted snapshot parseable by ParseSnapshot,
	// which enables fail-closed approval guards for later reads. The marker is
	// not set by the resolver itself to keep the resolver output schema-agnostic.
	resolved.SnapshotSchemaVersion = governanceprofile.SnapshotSchemaPolicyV1
	skillPrompts := map[string]string{}
	if s.Skills != nil && len(resolved.GateSkills) > 0 {
		registered, err := s.Skills.List(skills.WithWorkspace(ctx, workspaceID))
		if err != nil {
			return nil, fmt.Errorf("freeze policy Skill rubrics: %w", err)
		}
		for _, skill := range registered {
			skillPrompts[strings.TrimSpace(skill.Name)] = skill.Prompt
		}
	}
	if err := resolved.FreezeGateDefinitions(skillPrompts); err != nil {
		return nil, fmt.Errorf("freeze policy gate definitions: %w", err)
	}
	snapshotJSON, err := resolved.SnapshotJSON()
	if err != nil {
		return nil, err
	}
	explanation := governanceprofile.ExplainSnapshot(*resolved)

	art, err := s.ArtifactWriter.Publish(ctx, artifact.PublishInput{
		WorkspaceID:          workspaceID,
		FeatureID:            feat.ID,
		Version:              version,
		Status:               artifact.StatusDraft,
		ArtifactCompleteness: artifact.ArtifactCompletenessFull,
		Documents:            docs,
		ParentArtifactID:     parentID,
		LineageRootID:        lineageRoot,
		SourceKind:           in.SourceKind,
		SourceRevision:       in.SourceRevision,
		SourceID:             in.SourceID,
		Authority:            authority,
		PolicyVersion:        resolved.Version,
		PolicyDigest:         resolved.Digest,
		PolicySnapshotJSON:   snapshotJSON,
		ImpactLevel:          artifact.ImpactLevel(impact),
		RequestType:          artifact.RequestType(reqType),
		CreatedBy:            createdBy,
	})
	if err != nil {
		if featureCreated {
			if cleanupErr := s.FeatureUpserter.DeleteCandidateFeatureIfUnreferenced(ctx, feat.ID, key); cleanupErr != nil {
				return nil, fmt.Errorf("publish artifact failed: %w; cleanup new feature failed: %w", err, cleanupErr)
			}
		}
		return nil, err
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
		Status:          "draft",
		ReviewURL:       reviewURL,
		MissingRoles:    missingRoles,
		ReadinessHint:   readinessHint,
		ApprovalPolicy:  resolved.ApprovalPolicy,
		EvidencePolicy:  resolved.EvidencePolicy,
		WorkType:        resolved.WorkType,
		RiskLevel:       resolved.RiskLevel,
		GovernanceLevel: string(resolved.GovernanceLevel),
		GovernanceWhy:   resolved.ReasonCodes,
	}
	result.PolicyExplanation = &explanation
	return result, nil
}

func validateArtifactPackageSize(documents []DocumentInput) error {
	total := 0
	for _, document := range documents {
		size := len(document.Content)
		if size > maxArtifactDocumentBytes {
			return fmt.Errorf("%w: document %q exceeds the 1 MiB limit", ErrValidation, document.Path)
		}
		total += size
		if total > maxArtifactPackageBytes {
			return fmt.Errorf("%w: artifact package exceeds the 10 MiB limit", ErrValidation)
		}
	}
	return nil
}

// publishMissingRoles returns the required roles from the resolved policy that
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
