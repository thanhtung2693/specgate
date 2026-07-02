package api

import (
	"context"

	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/mcp"
)

// integrationRepoSource adapts *integrations.Service to mcp.IntegrationRepoSource.
// It lives in the api package because that package already imports both mcp and
// integrations; keeping the adapter here lets the mcp package stay free of an
// integrations import (no import cycle).
type integrationRepoSource struct {
	svc *integrations.Service
}

// NewIntegrationRepoSource returns an mcp.IntegrationRepoSource backed by the
// integrations service, or nil when svc is nil (the dynamic MCP handler is
// nil-safe). Used by both router.go and cmd/doc-registry/main.go.
func NewIntegrationRepoSource(svc *integrations.Service) mcp.IntegrationRepoSource {
	if svc == nil {
		return nil
	}
	return integrationRepoSource{svc: svc}
}

func (s integrationRepoSource) GitLabRepoConfigs(ctx context.Context) ([]mcp.GitLabRepoConfig, error) {
	configs, err := s.svc.ListGitLabRepoConfigs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]mcp.GitLabRepoConfig, 0, len(configs))
	for _, c := range configs {
		out = append(out, mcp.GitLabRepoConfig{
			ProjectID:  c.ProjectID,
			APIURL:     c.APIURL,
			Token:      c.Token,
			DefaultRef: c.DefaultRef,
			Bearer:     c.Bearer,
		})
	}
	return out, nil
}

func (s integrationRepoSource) IntegrationsHash() string {
	return s.svc.RepoConfigHash()
}
