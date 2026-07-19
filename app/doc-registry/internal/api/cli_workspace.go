package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/specgate/doc-registry/internal/workspace"
)

// cliWorkspaceMiddleware binds the CLI-selected workspace before a facade
// reaches governance services. The IDE-agent CLI is the authoritative external
// workflow, so scoped facade calls must not fall back to global reads.
func cliWorkspaceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasPrefix(req.URL.Path, "/api/v1/") || cliWorkspaceExempt(req.URL.Path) {
			next.ServeHTTP(w, req)
			return
		}
		workspaceID := strings.TrimSpace(req.URL.Query().Get("workspace_id"))
		if workspaceID == "" {
			if cliAllWorkspacesAllowed(req) {
				next.ServeHTTP(w, req)
				return
			}
			http.Error(w, "workspace_id is required", http.StatusBadRequest)
			return
		}
		var valid bool
		if workspaceID, valid = workspace.NormalizeID(workspaceID); !valid {
			http.Error(w, "workspace_id must be a safe path segment", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, req.WithContext(workspace.WithID(req.Context(), workspaceID)))
	})
}

func cliAllWorkspacesAllowed(req *http.Request) bool {
	return req.Method == http.MethodGet && req.URL.Query().Get("all_workspaces") == "true" &&
		(req.URL.Path == "/api/v1/status" || req.URL.Path == "/api/v1/stats")
}

// applyCLIWorkspace makes the selected CLI workspace authoritative for writes.
// A body may omit workspace_id for convenience, but it cannot target another
// workspace than the request scope.
func applyCLIWorkspace(ctx context.Context, bodyWorkspace *string) error {
	scope := workspace.ID(ctx)
	if *bodyWorkspace == "" {
		*bodyWorkspace = scope
		return nil
	}
	if *bodyWorkspace != scope {
		return huma.Error400BadRequest("workspace_id must match the CLI request scope")
	}
	return nil
}

func cliWorkspaceExempt(path string) bool {
	if strings.HasPrefix(path, "/api/v1/workspaces") || strings.HasPrefix(path, "/api/v1/identity/") {
		return true
	}
	switch path {
	case "/api/v1/meta", "/api/v1/policies/levels", "/api/v1/policies/resolve":
		return true
	default:
		return false
	}
}
