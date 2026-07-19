package artifact

import (
	"context"

	"github.com/specgate/doc-registry/internal/workspace"
)

// WithWorkspace pins artifact child operations to the trusted workspace
// selected by the product request.
func WithWorkspace(ctx context.Context, workspaceID string) context.Context {
	return workspace.WithID(ctx, workspaceID)
}

// WorkspaceFromContext returns the trusted artifact workspace, if present.
func WorkspaceFromContext(ctx context.Context) (string, bool) {
	workspaceID := workspace.ID(ctx)
	return workspaceID, workspaceID != ""
}
