package knowledge

import (
	"context"

	"github.com/specgate/doc-registry/internal/workspace"
)

// WithWorkspace pins Knowledge repository operations to one workspace for the
// duration of a trusted request. The API derives this value from request input
// after validating the workspace; callers cannot override it through a model
// prompt or document payload.
func WithWorkspace(ctx context.Context, workspaceID string) context.Context {
	return workspace.WithID(ctx, workspaceID)
}

// WorkspaceFromContext returns the trusted request workspace, when present.
func WorkspaceFromContext(ctx context.Context) (string, bool) {
	workspaceID := workspace.ID(ctx)
	return workspaceID, workspaceID != ""
}
