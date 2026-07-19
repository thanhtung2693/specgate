package integrations

import (
	"context"
	"fmt"
	"strings"

	"github.com/specgate/doc-registry/internal/workspace"
)

// WithWorkspace attaches the trusted workspace selected by a user-facing
// request or resolved from an authenticated integration callback.
func WithWorkspace(ctx context.Context, workspaceID string) context.Context {
	return workspace.WithID(ctx, workspaceID)
}

// WorkspaceID returns the workspace attached to ctx, if any.
func WorkspaceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	return workspace.ID(ctx)
}

// bindIntegrationWorkspace converts authenticated integration ownership into
// the workspace context required by every downstream repository query.
func bindIntegrationWorkspace(ctx context.Context, integration *Integration) (context.Context, error) {
	if integration == nil {
		return nil, fmt.Errorf("%w: integration is required", ErrValidation)
	}
	workspaceID := strings.TrimSpace(integration.WorkspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: integration workspace_id is required", ErrValidation)
	}
	if selected := strings.TrimSpace(WorkspaceID(ctx)); selected != "" && selected != workspaceID {
		return nil, ErrNotFound
	}
	return WithWorkspace(ctx, workspaceID), nil
}
