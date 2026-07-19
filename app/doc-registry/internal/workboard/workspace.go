package workboard

import (
	"context"
	"strings"

	"github.com/specgate/doc-registry/internal/workspace"
)

// WithWorkspace binds the trusted workspace used by repository operations.
func WithWorkspace(ctx context.Context, id string) context.Context {
	return workspace.WithID(ctx, id)
}

// WorkspaceID returns the trusted workspace bound to ctx.
func WorkspaceID(ctx context.Context) string {
	return strings.TrimSpace(workspace.ID(ctx))
}
