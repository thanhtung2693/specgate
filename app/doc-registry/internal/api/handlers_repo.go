package api

import (
	"context"
	"errors"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/mcp"
	"github.com/specgate/doc-registry/internal/mcp/repo"
)

// repoFileMaxBytes caps a single bootstrap repo-file read. Bootstrap only needs
// README / key markdown files, so a 1 MB cap is generous while bounding memory.
const repoFileMaxBytes = 1 * 1024 * 1024

// GetRepoFileInput selects one file in a connected GitLab project. `project` is
// the integration resource external_key (the GitLab project path/id). `ref`
// empty falls back to the resource's configured default ref.
type GetRepoFileInput struct {
	Project string `query:"project" required:"true" doc:"GitLab project external_key (integration resource)"`
	Path    string `query:"path" required:"true" doc:"repository file path"`
	Ref     string `query:"ref" doc:"git ref; empty uses the resource default ref"`
}

// GetRepoFileResponse returns the file content. found=false means the file does
// not exist at the ref (not an error); an unknown project is a 404 and an
// upstream GitLab/transport failure is a 502.
type GetRepoFileResponse struct {
	Body struct {
		Content string `json:"content"`
		Found   bool   `json:"found"`
	}
}

// GetRepoFile reads one repository file through the integration-backed GitLab
// provider — the integration token stays server-side and never crosses the
// network boundary. Source is the GitLab integrations × project resources
// (integrations.ListGitLabRepoConfigs), never the external "gitlab" MCP rows.
func (h *Handlers) GetRepoFile(ctx context.Context, in *GetRepoFileInput) (*GetRepoFileResponse, error) {
	project := strings.TrimSpace(in.Project)
	path := strings.TrimSpace(in.Path)
	if project == "" || path == "" {
		return nil, huma.Error400BadRequest("project and path are required")
	}
	if h.Integrations == nil {
		return nil, huma.Error404NotFound("no integrations configured")
	}

	configs, err := h.Integrations.ListGitLabRepoConfigs(ctx)
	if err != nil {
		return nil, huma.Error502BadGateway("list gitlab repo configs", err)
	}

	var matched *integrations.GitLabRepoConfig
	for i := range configs {
		if strings.TrimSpace(configs[i].ProjectID) == project {
			matched = &configs[i]
			break
		}
	}
	if matched == nil {
		return nil, huma.Error404NotFound("unknown repo project: " + project)
	}

	// ListGitLabRepoConfigs returns a host-only APIURL (scheme://host); the
	// provider appends /projects/... directly, so add /api/v4 first.
	provider := repo.NewGitLabProvider(repo.GitLabConfig{
		APIURL:     mcp.NormalizeGitLabAPIURL(matched.APIURL),
		Token:      matched.Token,
		ProjectID:  matched.ProjectID,
		DefaultRef: matched.DefaultRef,
		Bearer:     matched.Bearer,
	})

	file, err := provider.GetFileContent(ctx, path, strings.TrimSpace(in.Ref), repoFileMaxBytes)
	if err != nil {
		if errors.Is(err, repo.ErrFileNotFound) {
			out := &GetRepoFileResponse{}
			out.Body.Found = false
			return out, nil
		}
		return nil, huma.Error502BadGateway("read repo file", err)
	}

	out := &GetRepoFileResponse{}
	out.Body.Content = string(file.Content)
	out.Body.Found = true
	return out, nil
}
