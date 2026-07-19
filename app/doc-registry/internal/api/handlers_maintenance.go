package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
)

// MaintenanceCleanupCounts reports what one workspace-cleanup run deleted.
// The cleanup is housekeeping only: it never touches approved or draft
// artifacts, active features, or non-archived work items.
type MaintenanceCleanupCounts struct {
	ExpiredArtifactsDeleted       int `json:"expired_artifacts_deleted"`
	ReferencedSkipped             int `json:"referenced_skipped"`
	DemoFeaturesDeleted           int `json:"demo_features_deleted"`
	DemoChangeRequestsDeleted     int `json:"demo_change_requests_deleted"`
	DemoArtifactsDeleted          int `json:"demo_artifacts_deleted"`
	ArchivedChangeRequestsDeleted int `json:"archived_change_requests_deleted"`
}

type MaintenanceCleanupResponse struct {
	Body MaintenanceCleanupCounts
}

type maintenanceWorkspaceInput struct {
	WorkspaceID string `query:"workspace_id" doc:"workspace scope"`
}

// MaintenanceCleanup runs the housekeeping cleanup: an immediate retention
// sweep, demo seed removal, and archived change-request purge. Per spec §9.
func (h *Handlers) MaintenanceCleanup(ctx context.Context, in *maintenanceWorkspaceInput) (*MaintenanceCleanupResponse, error) {
	if h.MaintenanceCleanupFn == nil {
		return nil, huma.Error503ServiceUnavailable("maintenance cleanup is not configured")
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	counts, err := h.MaintenanceCleanupFn(ctx, workspaceID)
	if err != nil {
		return nil, huma.Error500InternalServerError("workspace cleanup", err)
	}
	return &MaintenanceCleanupResponse{Body: counts}, nil
}

// MaintenanceDemoRemoveCounts reports what one demo-removal run deleted. It is
// the mirror of the demo seed: it deletes exactly the rows and blob objects the
// seed creates (fixed demo IDs) and leaves everything else untouched.
type MaintenanceDemoRemoveCounts struct {
	FeaturesDeleted       int `json:"features_deleted"`
	ChangeRequestsDeleted int `json:"change_requests_deleted"`
	ArtifactsDeleted      int `json:"artifacts_deleted"`
	IntegrationsDeleted   int `json:"integrations_deleted"`
	KnowledgeDeleted      int `json:"knowledge_deleted"`
	FeedbackDeleted       int `json:"feedback_deleted"`
}

type MaintenanceDemoRemoveResponse struct {
	Body MaintenanceDemoRemoveCounts
}

// MaintenanceDemoRemove removes the bundled demo seed data: the mirror of the
// demo seed, idempotent, touching only the fixed demo IDs. Per spec §9.
func (h *Handlers) MaintenanceDemoRemove(ctx context.Context, in *maintenanceWorkspaceInput) (*MaintenanceDemoRemoveResponse, error) {
	if h.MaintenanceDemoRemoveFn == nil {
		return nil, huma.Error503ServiceUnavailable("maintenance demo removal is not configured")
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	counts, err := h.MaintenanceDemoRemoveFn(ctx, workspaceID)
	if err != nil {
		return nil, huma.Error500InternalServerError("demo removal", err)
	}
	return &MaintenanceDemoRemoveResponse{Body: counts}, nil
}
