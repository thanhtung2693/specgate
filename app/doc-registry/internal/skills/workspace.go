package skills

import (
	"context"
	"errors"
	"strings"

	"github.com/specgate/doc-registry/internal/workspace"
)

var (
	ErrWorkspaceRequired = errors.New("workspace is required")
	ErrWorkspaceMismatch = errors.New("workspace mismatch")
)

func WithWorkspace(ctx context.Context, workspaceID string) context.Context {
	return workspace.WithID(ctx, workspaceID)
}

func WorkspaceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	return workspace.ID(ctx)
}

func requireWorkspace(ctx context.Context, requested string) (string, error) {
	trusted := WorkspaceID(ctx)
	if trusted == "" {
		return "", ErrWorkspaceRequired
	}
	if requested = strings.TrimSpace(requested); requested != "" && requested != trusted {
		return "", ErrWorkspaceMismatch
	}
	return trusted, nil
}
