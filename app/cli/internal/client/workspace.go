package client

import (
	"context"
	"strings"
)

type workspaceContextKey struct{}
type allWorkspacesContextKey struct{}

// WithWorkspace attaches the selected workspace to a request without changing
// the CLI client interface. Workspace-owned endpoints use it to add their
// query/body boundary.
func WithWorkspace(ctx context.Context, workspaceID string) context.Context {
	return context.WithValue(ctx, workspaceContextKey{}, strings.TrimSpace(workspaceID))
}

func workspaceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(workspaceContextKey{}).(string)
	return strings.TrimSpace(id)
}

// WorkspaceID returns the workspace attached to a request context.
func WorkspaceID(ctx context.Context) string { return workspaceID(ctx) }

// WithAllWorkspaces marks an intentional cross-workspace read. It is distinct
// from an omitted workspace, which scoped API facade routes reject.
func WithAllWorkspaces(ctx context.Context) context.Context {
	return context.WithValue(ctx, allWorkspacesContextKey{}, true)
}

func allWorkspaces(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	all, _ := ctx.Value(allWorkspacesContextKey{}).(bool)
	return all
}
