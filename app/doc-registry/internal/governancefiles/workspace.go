package governancefiles

import (
	"context"

	"github.com/specgate/doc-registry/internal/workspace"
)

// WithWorkspace scopes user-facing governance-file operations to one workspace.
func WithWorkspace(ctx context.Context, workspaceID string) context.Context {
	return workspace.WithID(ctx, workspaceID)
}

// WorkspaceID returns the trusted workspace selected for the current request.
func WorkspaceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	return workspace.ID(ctx)
}
