package api

import (
	"context"
	"errors"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/workboard"
)

func (h *Handlers) ListFeatures(ctx context.Context, in *struct{}) (*ListFeaturesOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("list features")
	}
	items, err := h.WorkBoard.ListFeatures(ctx)
	if err != nil {
		return nil, mapWorkBoardError("list features", err)
	}
	out := &ListFeaturesOutput{}
	out.Body.Items = items
	return out, nil
}

func (h *Handlers) CreateFeature(ctx context.Context, in *CreateFeatureInput) (*FeatureOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("create feature")
	}
	feature, err := h.WorkBoard.CreateFeature(ctx, in.Body)
	if err != nil {
		return nil, mapWorkBoardError("create feature", err)
	}
	return &FeatureOutput{Body: *feature}, nil
}

func (h *Handlers) UpsertFeatureByKey(ctx context.Context, in *UpsertFeatureByKeyInput) (*FeatureOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("upsert feature by key")
	}
	feature, err := h.WorkBoard.UpsertFeatureByKey(ctx, in.Body.Key, in.Body.Name)
	if err != nil {
		return nil, mapWorkBoardError("upsert feature by key", err)
	}
	return &FeatureOutput{Body: *feature}, nil
}

func (h *Handlers) GetFeature(ctx context.Context, in *WorkBoardIDInput) (*FeatureOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("get feature")
	}
	feature, err := h.WorkBoard.GetFeature(ctx, in.ID)
	if err != nil {
		return nil, mapWorkBoardError("get feature", err)
	}
	return &FeatureOutput{Body: *feature}, nil
}

func (h *Handlers) DeleteFeature(ctx context.Context, in *WorkBoardIDInput) (*DeleteWorkBoardOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("delete feature")
	}
	if err := h.WorkBoard.DeleteFeature(ctx, in.ID); err != nil {
		return nil, mapWorkBoardError("delete feature", err)
	}
	return &DeleteWorkBoardOutput{}, nil
}

func (h *Handlers) PatchFeature(ctx context.Context, in *PatchFeatureInput) (*FeatureOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("patch feature")
	}
	in.Body.ID = in.ID
	feature, err := h.WorkBoard.UpdateFeature(ctx, in.Body)
	if err != nil {
		return nil, mapWorkBoardError("patch feature", err)
	}
	return &FeatureOutput{Body: *feature}, nil
}

func (h *Handlers) SetFeatureSummary(ctx context.Context, in *SetFeatureSummaryInput) (*FeatureOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("set feature summary")
	}
	feature, err := h.WorkBoard.SetFeatureSummary(ctx, in.ID, in.Body.SummaryMD, in.Body.SourceVersion)
	if err != nil {
		return nil, mapWorkBoardError("set feature summary", err)
	}
	return &FeatureOutput{Body: *feature}, nil
}

func (h *Handlers) ListChangeRequests(ctx context.Context, in *ListChangeRequestsInput) (*ListChangeRequestsOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("list change requests")
	}
	items, err := h.WorkBoard.ListChangeRequests(ctx, in.IncludeArchived)
	if err != nil {
		return nil, mapWorkBoardError("list change requests", err)
	}
	if workspaceID := strings.TrimSpace(in.WorkspaceID); workspaceID != "" {
		filtered := items[:0]
		for _, item := range items {
			if item.WorkspaceID == workspaceID {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	out := &ListChangeRequestsOutput{}
	out.Body.Items = items
	return out, nil
}

func (h *Handlers) CreateChangeRequest(ctx context.Context, in *CreateChangeRequestInput) (*ChangeRequestOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("create change request")
	}
	cr, err := h.WorkBoard.CreateChangeRequest(ctx, in.Body)
	if err != nil {
		return nil, mapWorkBoardError("create change request", err)
	}
	return &ChangeRequestOutput{Body: *cr}, nil
}

func (h *Handlers) GetChangeRequest(ctx context.Context, in *WorkBoardIDInput) (*ChangeRequestOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("get change request")
	}
	cr, err := h.WorkBoard.GetChangeRequest(ctx, in.ID)
	if err != nil {
		return nil, mapWorkBoardError("get change request", err)
	}
	return &ChangeRequestOutput{Body: *cr}, nil
}

func (h *Handlers) ListAcceptanceCriteria(ctx context.Context, in *WorkBoardIDInput) (*ListAcceptanceCriteriaOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("list acceptance criteria")
	}
	items, err := h.WorkBoard.ListAcceptanceCriteria(ctx, in.ID)
	if err != nil {
		return nil, mapWorkBoardError("list acceptance criteria", err)
	}
	out := &ListAcceptanceCriteriaOutput{}
	out.Body.Items = items
	return out, nil
}

func (h *Handlers) RefreshGateRuns(ctx context.Context, in *RefreshGateRunsInput) (*ListGateRunsOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("refresh gate runs")
	}
	evaluations := []workboard.GateEvaluation{}
	if in.Body != nil {
		evaluations = in.Body.Evaluations
	}
	for _, eval := range evaluations {
		if eval.Confidence < 0 || eval.Confidence > 1 {
			return nil, huma.Error400BadRequest("refresh gate runs", errors.New("confidence must be between 0 and 1"))
		}
	}
	items, err := h.WorkBoard.RefreshGateRuns(ctx, workboard.RefreshGateRunsInput{
		ChangeRequestID: in.ID,
		Evaluations:     evaluations,
	})
	if err != nil {
		return nil, mapWorkBoardError("refresh gate runs", err)
	}
	// Best-effort: after a delivery_review pass, close any linked tracker issues
	// (Linear, GitHub, GitLab). Errors are swallowed inside AutoTransitionIssueOnDeliveryPass
	// so they never fail the gate-run write. per spec §auto-transition CR-0D60C43C.
	if h.Integrations != nil && deliveryPassInItems(items) {
		h.Integrations.AutoTransitionIssueOnDeliveryPass(ctx, in.ID)
	}
	out := &ListGateRunsOutput{}
	out.Body.Items = items
	return out, nil
}

// deliveryPassInItems reports whether any gate run in the slice is a
// delivery_review pass. Used to decide whether to fire the Linear
// auto-transition side effect.
func deliveryPassInItems(items []workboard.GateRun) bool {
	for _, item := range items {
		if item.Gate == governanceprofile.DeliveryReviewGateKey &&
			item.State == workboard.NextActionStatePass {
			return true
		}
	}
	return false
}

func (h *Handlers) ListGateRuns(ctx context.Context, in *ListGateRunsInput) (*ListGateRunsOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("list gate runs")
	}
	items, err := h.WorkBoard.ListGateRuns(ctx, in.ID, in.Limit)
	if err != nil {
		return nil, mapWorkBoardError("list gate runs", err)
	}
	out := &ListGateRunsOutput{}
	out.Body.Items = items
	return out, nil
}

func (h *Handlers) NextActions(ctx context.Context, in *WorkBoardIDInput) (*NextActionsOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("change request next actions")
	}
	items, err := h.WorkBoard.NextActions(ctx, in.ID)
	if err != nil {
		return nil, mapWorkBoardError("change request next actions", err)
	}
	out := &NextActionsOutput{}
	out.Body.Items = items
	return out, nil
}

func (h *Handlers) PatchChangeRequest(ctx context.Context, in *PatchChangeRequestInput) (*ChangeRequestOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("patch change request")
	}
	in.Body.ID = in.ID
	cr, err := h.WorkBoard.UpdateChangeRequest(ctx, in.Body)
	if err != nil {
		return nil, mapWorkBoardError("patch change request", err)
	}
	return &ChangeRequestOutput{Body: *cr}, nil
}

func (h *Handlers) UnarchiveChangeRequest(ctx context.Context, in *UnarchiveChangeRequestInput) (*ChangeRequestOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("unarchive change request")
	}
	cr, err := h.WorkBoard.UnarchiveChangeRequest(ctx, in.ID, strings.TrimSpace(in.Body.Actor))
	if err != nil {
		return nil, mapWorkBoardError("unarchive change request", err)
	}
	return &ChangeRequestOutput{Body: *cr}, nil
}

func (h *Handlers) PatchChangeRequestLeadArtifact(ctx context.Context, in *PatchArtifactPointerInput) (*ChangeRequestOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("patch change request lead artifact")
	}
	cr, err := h.WorkBoard.PatchLeadArtifact(ctx, in.ID, in.Body.ArtifactID)
	if err != nil {
		return nil, mapWorkBoardError("patch change request lead artifact", err)
	}
	return &ChangeRequestOutput{Body: *cr}, nil
}

func (h *Handlers) PatchChangeRequestContextPackArtifact(ctx context.Context, in *PatchArtifactPointerInput) (*ChangeRequestOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("patch change request context pack artifact")
	}
	cr, err := h.WorkBoard.PatchContextPackArtifact(ctx, in.ID, in.Body.ArtifactID)
	if err != nil {
		return nil, mapWorkBoardError("patch change request context pack artifact", err)
	}
	return &ChangeRequestOutput{Body: *cr}, nil
}

func (h *Handlers) ListWorkBoardStaleWarnings(ctx context.Context, in *ListStaleWarningsInput) (*ListStaleWarningsOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("list workboard stale warnings")
	}
	items, err := h.WorkBoard.ListStaleWarnings(ctx, workboard.StaleWarningFilter{
		FeatureID:       in.FeatureID,
		ChangeRequestID: in.ChangeRequestID,
	})
	if err != nil {
		return nil, mapWorkBoardError("list workboard stale warnings", err)
	}
	out := &ListStaleWarningsOutput{}
	out.Body.Items = items
	return out, nil
}
