package governanceops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/workboard"
)

// WorkItemStore is the workboard surface for feature-backed work-item creation
// (specgate work create). nil disables CreateWorkItem.
type WorkItemStore interface {
	GetFeature(ctx context.Context, id string) (*workboard.Feature, error)
	GetFeatureByKey(ctx context.Context, key string) (*workboard.Feature, error)
	CreateChangeRequest(ctx context.Context, in workboard.ChangeRequest) (*workboard.ChangeRequest, error)
}

// CreateWorkItemInput is the body of POST /api/v1/work-items/create.
type CreateWorkItemInput struct {
	Feature            string   `json:"feature"`
	Title              string   `json:"title"`
	Description        string   `json:"description,omitempty"`
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
	CreatedBy          string   `json:"created_by,omitempty"`
	WorkspaceID        string   `json:"workspace_id,omitempty"`
	SourceRefs         []string `json:"source_refs,omitempty"`
	// ArtifactID is reserved for trusted portable imports that must preserve an
	// older approved version after a newer version became canonical.
	ArtifactID string `json:"artifact_id,omitempty"`
}

// CreateWorkItemResult reports the created feature-backed work item.
type CreateWorkItemResult struct {
	ChangeRequestID    string   `json:"change_request_id"`
	ChangeRequestKey   string   `json:"change_request_key"`
	FeatureID          string   `json:"feature_id"`
	FeatureKey         string   `json:"feature_key"`
	LeadArtifactID     string   `json:"lead_artifact_id"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
}

// CreateWorkItem creates a feature-backed ChangeRequest bound to the feature's
// approved canonical spec — the full-route sibling of quick work creation (per
// spec §6 work-items/create). The feature resolves by id or key; a feature
// without a canonical artifact is rejected (approve + promote first), because
// the full route hands off approved spec content.
func (s *Service) CreateWorkItem(ctx context.Context, in CreateWorkItemInput) (CreateWorkItemResult, error) {
	if s.WorkItems == nil {
		return CreateWorkItemResult{}, ErrUnavailable
	}
	ref := strings.TrimSpace(in.Feature)
	title := strings.TrimSpace(in.Title)
	if ref == "" || title == "" {
		return CreateWorkItemResult{}, fmt.Errorf("%w: feature and title are required", ErrValidation)
	}
	workspaceID := strings.TrimSpace(in.WorkspaceID)
	if selected := trustedWorkspace(ctx); selected != "" {
		if workspaceID != "" && workspaceID != selected {
			return CreateWorkItemResult{}, fmt.Errorf("%w: workspace_id must match the trusted request workspace", ErrValidation)
		}
		workspaceID = selected
	}

	feat, err := s.WorkItems.GetFeature(ctx, ref)
	if err != nil {
		feat, err = s.WorkItems.GetFeatureByKey(ctx, ref)
	}
	if err != nil {
		return CreateWorkItemResult{}, fmt.Errorf("%w: feature %q not found — try `specgate feature list --all`", ErrNotFound, ref)
	}
	if err := requireFeatureWorkspace(ctx, feat); err != nil {
		return CreateWorkItemResult{}, fmt.Errorf("%w: feature %q not found — try `specgate feature list --all`", ErrNotFound, ref)
	}
	canonical := strings.TrimSpace(feat.CanonicalArtifactID)
	leadArtifactID := strings.TrimSpace(in.ArtifactID)
	if leadArtifactID == "" {
		leadArtifactID = canonical
	}
	if leadArtifactID == "" {
		return CreateWorkItemResult{}, fmt.Errorf(
			"%w: feature %q has no canonical artifact — approve and promote a spec first (`specgate artifact approve`, `specgate artifact promote`)",
			ErrValidation, feat.Key)
	}
	if s.Artifacts == nil {
		return CreateWorkItemResult{}, ErrUnavailable
	}
	art, err := s.Artifacts.Get(ctx, leadArtifactID)
	if err != nil ||
		strings.TrimSpace(art.FeatureID) != strings.TrimSpace(feat.ID) ||
		strings.TrimSpace(art.WorkspaceID) != strings.TrimSpace(feat.WorkspaceID) ||
		(art.Status != artifact.StatusApproved && art.Status != artifact.StatusSuperseded) {
		return CreateWorkItemResult{}, fmt.Errorf("%w: artifact %q is not an approved version of feature %q in this workspace", ErrNotFound, leadArtifactID, feat.Key)
	}

	storedCriteria, criterionTexts := normalizeWorkAcceptanceCriteria(in.AcceptanceCriteria)
	if len(storedCriteria) == 0 {
		return CreateWorkItemResult{}, fmt.Errorf("%w: at least one explicit acceptance criterion is required", ErrValidation)
	}

	acJSON, err := json.Marshal(storedCriteria)
	if err != nil {
		return CreateWorkItemResult{}, err
	}
	sourceRefsJSON, err := json.Marshal(uniqueNonEmptyStrings(in.SourceRefs))
	if err != nil {
		return CreateWorkItemResult{}, err
	}
	created, err := s.WorkItems.CreateChangeRequest(ctx, workboard.ChangeRequest{
		FeatureID:          feat.ID,
		Title:              title,
		WorkType:           workboard.WorkTypeFeatureChange,
		IntentMD:           strings.TrimSpace(in.Description),
		LeadArtifactID:     leadArtifactID,
		AcceptanceCriteria: string(acJSON),
		CreatedBy:          strings.TrimSpace(in.CreatedBy),
		WorkspaceID:        workspaceID,
		SourceRefs:         string(sourceRefsJSON),
	})
	if err != nil {
		return CreateWorkItemResult{}, err
	}
	return CreateWorkItemResult{
		ChangeRequestID:    created.ID,
		ChangeRequestKey:   created.Key,
		FeatureID:          feat.ID,
		FeatureKey:         feat.Key,
		LeadArtifactID:     leadArtifactID,
		AcceptanceCriteria: criterionTexts,
	}, nil
}

func uniqueNonEmptyStrings(input []string) []string {
	seen := make(map[string]struct{}, len(input))
	result := make([]string, 0, len(input))
	for _, raw := range input {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func normalizeWorkAcceptanceCriteria(input []string) ([]any, []string) {
	stored := make([]any, 0, len(input))
	texts := make([]string, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	for _, raw := range input {
		text, binding := parseWorkAcceptanceCriterionBinding(raw)
		if text == "" {
			continue
		}
		key := text + "\x00" + binding
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		texts = append(texts, text)
		if binding == "" {
			stored = append(stored, text)
			continue
		}
		stored = append(stored, map[string]string{
			"text":                 text,
			"verification_binding": binding,
		})
	}
	return stored, texts
}

func parseWorkAcceptanceCriterionBinding(raw string) (string, string) {
	trimmed := strings.TrimSpace(raw)
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return "", ""
	}
	last := fields[len(fields)-1]
	const prefix = "@check:"
	if !strings.HasPrefix(last, prefix) {
		return trimmed, ""
	}
	binding := strings.TrimSpace(strings.TrimPrefix(last, prefix))
	text := strings.TrimSpace(strings.TrimSuffix(trimmed, last))
	if binding == "" || text == "" {
		return trimmed, ""
	}
	return text, binding
}
