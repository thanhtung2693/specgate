package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/specgate/doc-registry/internal/workboard"
)

type listFeaturesInput struct {
	WorkspaceID string `query:"workspace_id"`
}

func workboardWorkspaceContext(ctx context.Context, workspaceID string) (context.Context, string, error) {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return nil, "", err
	}
	return workboard.WithWorkspace(ctx, workspaceID), workspaceID, nil
}

func (h *Handlers) ListFeatures(ctx context.Context, in *listFeaturesInput) (*ListFeaturesOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("list features")
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = workboard.WithWorkspace(ctx, workspaceID)
	var items []workboard.Feature
	type scopedLister interface {
		ListFeaturesInWorkspace(context.Context, string) ([]workboard.Feature, error)
	}
	if scoped, ok := h.WorkBoard.(scopedLister); ok {
		items, err = scoped.ListFeaturesInWorkspace(ctx, workspaceID)
	} else {
		items, err = h.WorkBoard.ListFeatures(ctx)
		if err == nil {
			filtered := items[:0]
			for _, item := range items {
				if item.WorkspaceID == workspaceID {
					filtered = append(filtered, item)
				}
			}
			items = filtered
		}
	}
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
	workspaceID, err := requireWorkspaceID(in.Body.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = workboard.WithWorkspace(ctx, workspaceID)
	in.Body.WorkspaceID = workspaceID
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
	workspaceID, err := requireWorkspaceID(in.Body.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = workboard.WithWorkspace(ctx, workspaceID)
	in.Body.WorkspaceID = workspaceID
	var feature *workboard.Feature
	scoped, ok := h.WorkBoard.(interface {
		UpsertFeatureByKeyInWorkspaceForPublish(context.Context, string, string, string) (*workboard.Feature, bool, error)
	})
	if !ok {
		return nil, notImplemented("workspace-scoped upsert feature by key")
	}
	feature, _, err = scoped.UpsertFeatureByKeyInWorkspaceForPublish(ctx, workspaceID, in.Body.Key, in.Body.Name)
	if err != nil {
		return nil, mapWorkBoardError("upsert feature by key", err)
	}
	return &FeatureOutput{Body: *feature}, nil
}

func (h *Handlers) GetFeature(ctx context.Context, in *WorkBoardIDInput) (*FeatureOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("get feature")
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = workboard.WithWorkspace(ctx, workspaceID)
	feature, err := getFeatureForWorkspace(ctx, h.WorkBoard, workspaceID, in.ID)
	if err != nil {
		return nil, mapWorkBoardError("get feature", err)
	}
	return &FeatureOutput{Body: *feature}, nil
}

func (h *Handlers) PatchFeature(ctx context.Context, in *PatchFeatureInput) (*FeatureOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("patch feature")
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = workboard.WithWorkspace(ctx, workspaceID)
	in.Body.ID = in.ID
	in.Body.WorkspaceID = workspaceID
	if _, err := getFeatureForWorkspace(ctx, h.WorkBoard, workspaceID, in.ID); err != nil {
		return nil, mapWorkBoardError("patch feature", err)
	}
	feature, err := h.WorkBoard.UpdateFeature(ctx, in.Body)
	if err != nil {
		return nil, mapWorkBoardError("patch feature", err)
	}
	return &FeatureOutput{Body: *feature}, nil
}

func getFeatureForWorkspace(ctx context.Context, store workboard.Store, workspaceID, id string) (*workboard.Feature, error) {
	type scopedGetter interface {
		GetFeatureInWorkspace(context.Context, string, string) (*workboard.Feature, error)
	}
	if workspaceID != "" {
		if scoped, ok := store.(scopedGetter); ok {
			return scoped.GetFeatureInWorkspace(ctx, workspaceID, id)
		}
	}
	feature, err := store.GetFeature(ctx, id)
	if err != nil {
		return nil, err
	}
	if workspaceID != "" && feature.WorkspaceID != workspaceID {
		return nil, workboard.ErrNotFound
	}
	return feature, nil
}

// PromoteArtifactToCanonical sets an approved artifact as its feature's
// canonical (reusing SetFeatureCanonicalArtifact — the deliberate promotion
// action that unblocks the feature-backed handoff; aligns with
// WarningCanonicalPromotionAvailable). The feature is resolved from the
// artifact's feature_id; a featureless or non-approved artifact is rejected
// with a 400. Promotion is never automatic on approval.
func (h *Handlers) PromoteArtifactToCanonical(ctx context.Context, in *PromoteArtifactCanonicalInput) (*FeatureOutput, error) {
	if h.WorkBoard == nil || h.Artifacts == nil {
		return nil, notImplemented("promote artifact to canonical")
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = workboard.WithWorkspace(ctx, workspaceID)
	art, err := getArtifactForWorkspace(ctx, h.Artifacts, workspaceID, in.ID)
	if err != nil {
		return nil, mapArtifactError("promote artifact to canonical", err)
	}
	if strings.TrimSpace(art.FeatureID) == "" {
		return nil, huma.Error400BadRequest(
			"promote artifact to canonical",
			fmt.Errorf("artifact %s has no feature to promote to", in.ID),
		)
	}
	if _, err := getFeatureForWorkspace(ctx, h.WorkBoard, workspaceID, art.FeatureID); err != nil {
		return nil, mapWorkBoardError("promote artifact to canonical", err)
	}
	feature, err := h.WorkBoard.SetFeatureCanonicalArtifact(ctx, art.FeatureID, in.ID, strings.TrimSpace(in.Body.ApprovedBy))
	if err != nil {
		return nil, mapWorkBoardError("promote artifact to canonical", err)
	}
	return &FeatureOutput{Body: *feature}, nil
}

func (h *Handlers) ListChangeRequests(ctx context.Context, in *ListChangeRequestsInput) (*ListChangeRequestsOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("list change requests")
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = workboard.WithWorkspace(ctx, workspaceID)
	type scopedLister interface {
		ListChangeRequestsInWorkspace(context.Context, string, bool) ([]workboard.ChangeRequest, error)
	}
	scoped, ok := h.WorkBoard.(scopedLister)
	if !ok {
		return nil, notImplemented("workspace-scoped change-request list")
	}
	items, err := scoped.ListChangeRequestsInWorkspace(ctx, workspaceID, in.IncludeArchived)
	if err != nil {
		return nil, mapWorkBoardError("list change requests", err)
	}
	out := &ListChangeRequestsOutput{}
	out.Body.Items = items
	return out, nil
}

func (h *Handlers) CreateChangeRequest(ctx context.Context, in *CreateChangeRequestInput) (*ChangeRequestOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("create change request")
	}
	workspaceID, err := requireWorkspaceID(in.Body.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = workboard.WithWorkspace(ctx, workspaceID)
	in.Body.WorkspaceID = workspaceID
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
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = workboard.WithWorkspace(ctx, workspaceID)
	cr, err := h.WorkBoard.GetChangeRequest(ctx, in.ID)
	if err != nil {
		return nil, mapWorkBoardError("get change request", err)
	}
	if cr.WorkspaceID != workspaceID {
		return nil, mapWorkBoardError("get change request", workboard.ErrNotFound)
	}
	return &ChangeRequestOutput{Body: *cr}, nil
}

func (h *Handlers) ListAcceptanceCriteria(ctx context.Context, in *WorkBoardIDInput) (*ListAcceptanceCriteriaOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("list acceptance criteria")
	}
	ctx, _, err := workboardWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if err := h.requireChangeRequestWorkspace(ctx, in.WorkspaceID, in.ID); err != nil {
		return nil, mapWorkBoardError("list acceptance criteria", err)
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
	ctx, _, err := workboardWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if err := h.requireChangeRequestWorkspace(ctx, in.WorkspaceID, in.ID); err != nil {
		return nil, mapWorkBoardError("refresh gate runs", err)
	}
	evaluations := []workboard.GateEvaluation{}
	evaluationsOnly := false
	if in.Body != nil {
		evaluations = in.Body.Evaluations
		evaluationsOnly = in.Body.EvaluationsOnly
	}
	for _, eval := range evaluations {
		if eval.Confidence < 0 || eval.Confidence > 1 {
			return nil, huma.Error400BadRequest("refresh gate runs", errors.New("confidence must be between 0 and 1"))
		}
	}
	items, err := h.WorkBoard.RefreshGateRuns(ctx, workboard.RefreshGateRunsInput{
		ChangeRequestID: in.ID,
		Evaluations:     evaluations,
		EvaluationsOnly: evaluationsOnly,
	})
	if err != nil {
		return nil, mapWorkBoardError("refresh gate runs", err)
	}
	out := &ListGateRunsOutput{}
	out.Body.Items = items
	return out, nil
}

func (h *Handlers) ListGateRuns(ctx context.Context, in *ListGateRunsInput) (*ListGateRunsOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("list gate runs")
	}
	ctx, _, err := workboardWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if err := h.requireChangeRequestWorkspace(ctx, in.WorkspaceID, in.ID); err != nil {
		return nil, mapWorkBoardError("list gate runs", err)
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
	ctx, _, err := workboardWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if err := h.requireChangeRequestWorkspace(ctx, in.WorkspaceID, in.ID); err != nil {
		return nil, mapWorkBoardError("change request next actions", err)
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
	ctx, workspaceID, err := workboardWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if err := h.requireChangeRequestWorkspace(ctx, strings.TrimSpace(in.WorkspaceID), in.ID); err != nil {
		return nil, mapWorkBoardError("patch change request", err)
	}
	in.Body.ID = in.ID
	in.Body.WorkspaceID = workspaceID
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
	ctx, _, err := workboardWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if err := h.requireChangeRequestWorkspace(ctx, strings.TrimSpace(in.WorkspaceID), in.ID); err != nil {
		return nil, mapWorkBoardError("unarchive change request", err)
	}
	cr, err := h.WorkBoard.UnarchiveChangeRequest(ctx, in.ID, strings.TrimSpace(in.Body.Actor))
	if err != nil {
		return nil, mapWorkBoardError("unarchive change request", err)
	}
	return &ChangeRequestOutput{Body: *cr}, nil
}

func (h *Handlers) requireChangeRequestWorkspace(ctx context.Context, workspaceID, id string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return workboard.ErrWorkspaceRequired
	}
	cr, err := h.WorkBoard.GetChangeRequest(ctx, id)
	if err != nil {
		return err
	}
	if cr.WorkspaceID != workspaceID {
		return workboard.ErrNotFound
	}
	return nil
}

func (h *Handlers) ListWorkBoardStaleWarnings(ctx context.Context, in *ListStaleWarningsInput) (*ListStaleWarningsOutput, error) {
	if h.WorkBoard == nil {
		return nil, notImplemented("list workboard stale warnings")
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = workboard.WithWorkspace(ctx, workspaceID)
	items, err := h.WorkBoard.ListStaleWarnings(ctx, workboard.StaleWarningFilter{
		FeatureID:       in.FeatureID,
		ChangeRequestID: in.ChangeRequestID,
		WorkspaceID:     workspaceID,
	})
	if err != nil {
		return nil, mapWorkBoardError("list workboard stale warnings", err)
	}
	out := &ListStaleWarningsOutput{}
	out.Body.Items = items
	return out, nil
}
