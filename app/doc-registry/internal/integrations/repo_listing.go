package integrations

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/specgate/doc-registry/internal/githubapi"
	"github.com/specgate/doc-registry/internal/gitlabapi"
)

// repoListingTimeout caps the connect-time repo/project listing call. The
// default client timeout is tuned for fast outbound issue creation, but
// GitLab's `/projects?membership=true` can take many seconds for an account
// in many groups, so the picker needs a longer ceiling.
const repoListingTimeout = 30 * time.Second

// RepoSummary is one selectable repo/project for the connect-time picker. The
// fields map directly onto a Resource (external_id / external_key / display_name
// / default_ref) so a picked entry can be saved as a linked project verbatim.
type RepoSummary struct {
	ExternalID  string `json:"external_id"`
	ExternalKey string `json:"external_key"`
	DisplayName string `json:"display_name"`
	DefaultRef  string `json:"default_ref"`
}

// ListAccessibleRepos returns the repos/projects the integration's token can
// see, for the repo picker. GitLab and GitHub only — Linear has no repos. The
// token (PAT or OAuth) is resolved server-side and never leaves the process.
// `search` is an optional name filter; `limit` caps results.
func (s *Service) ListAccessibleRepos(ctx context.Context, integrationID string, search string, limit int) ([]RepoSummary, error) {
	integrationID = strings.TrimSpace(integrationID)
	if integrationID == "" {
		return nil, fmt.Errorf("%w: integration_id is required", ErrValidation)
	}
	integration, err := s.integrations.GetIntegration(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	token, err := s.ResolveAPIToken(ctx, integrationID)
	if err != nil {
		return nil, err
	}

	switch integration.Provider {
	case ProviderGitLab:
		client := gitlabapi.NewClient(gitlabapi.ClientConfig{
			APIURL:     gitLabAPIBase(integration.BaseURL) + "/api/v4",
			Token:      token,
			Bearer:     strings.TrimSpace(integration.AuthMethod) == AuthMethodOAuth,
			HTTPClient: &http.Client{Timeout: repoListingTimeout},
		})
		projects, err := client.ListProjects(ctx, search, limit)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
		}
		out := make([]RepoSummary, 0, len(projects))
		for _, p := range projects {
			out = append(out, RepoSummary{
				ExternalID:  strconv.Itoa(p.ID),
				ExternalKey: p.PathWithNS,
				DisplayName: p.Name,
				DefaultRef:  p.DefaultBranch,
			})
		}
		return out, nil
	case ProviderGitHub:
		client := githubapi.NewClient(githubapi.ClientConfig{
			APIURL:     githubapi.APIURL(integration.BaseURL),
			Token:      token,
			HTTPClient: &http.Client{Timeout: repoListingTimeout},
		})
		repos, err := client.ListRepos(ctx, search, limit)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
		}
		out := make([]RepoSummary, 0, len(repos))
		for _, r := range repos {
			out = append(out, RepoSummary{
				ExternalID:  strconv.Itoa(r.ID),
				ExternalKey: r.FullName,
				DisplayName: r.Name,
				DefaultRef:  r.DefaultBranch,
			})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%w: provider %q does not support repo listing", ErrValidation, integration.Provider)
	}
}
