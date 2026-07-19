package policy

import (
	"context"

	"github.com/specgate/doc-registry/internal/workspace"
)

// WithWorkspace pins gate-task reads and writes to the trusted product
// workspace selected by the caller.
func WithWorkspace(ctx context.Context, workspaceID string) context.Context {
	return workspace.WithID(ctx, workspaceID)
}

// WorkspaceFromContext returns the trusted gate-task workspace, if present.
func WorkspaceFromContext(ctx context.Context) (string, bool) {
	workspaceID := workspace.ID(ctx)
	return workspaceID, workspaceID != ""
}
