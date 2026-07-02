package integrations

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
)

// gitLabAPIBase reduces a stored integration base URL to scheme://host so the
// caller can append /api/v4. Integrations frequently store the full repository
// URL (e.g. https://gitlab.example.com/group/sub/repo) rather than the bare
// host, and the GitLab API lives at the host root — so derive the host rather
// than treat base_url as already-an-API-root. Falls back to the trimmed input
// when it cannot be parsed (the caller's normalizer still guards empties).
// Note: GitLab installed under a relative subpath is not handled here.
func gitLabAPIBase(baseURL string) string {
	s := strings.TrimSpace(baseURL)
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return s
	}
	return u.Scheme + "://" + u.Host
}

// GitLabRepoConfig is one repo-reading provider derived from a connected GitLab
// integration and one of its project resources. The governance's repo_* MCP tools
// read code through providers built from these — the unified source that also
// backs the tracker, replacing the separate external "gitlab" MCP rows. The
// token is resolved (decrypted) server-side; it never crosses a network
// boundary.
type GitLabRepoConfig struct {
	ProjectID  string
	APIURL     string
	Token      string
	DefaultRef string
	// Bearer is true when Token is an OAuth access token (sent as
	// Authorization: Bearer) rather than a PAT (sent as PRIVATE-TOKEN).
	Bearer bool
}

// ListGitLabRepoConfigs returns one repo config per (connected GitLab
// integration that has a recoverable API token) × (its project resource). The
// caller maps each into a repo provider. Mapping:
//   - ProjectID  ← resource.ExternalKey
//   - APIURL     ← scheme://host of integration.BaseURL (the caller appends /api/v4)
//   - Token      ← ResolveAPIToken(integrationID) (decrypts the stored token)
//   - DefaultRef ← resource.DefaultRef
//
// Integrations that are not gitlab, have no API token, or whose token cannot be
// resolved (e.g. disabled) are skipped; only project resources are considered
// (matching the webhook FindResourceByProvider convention). A genuine DB error
// from List/ListResources propagates.
func (s *Service) ListGitLabRepoConfigs(ctx context.Context) ([]GitLabRepoConfig, error) {
	integrations, err := s.integrations.ListIntegrations(ctx)
	if err != nil {
		return nil, err
	}
	var out []GitLabRepoConfig
	for _, integration := range integrations {
		if integration.Provider != ProviderGitLab || !integrationHasResolvedToken(integration) {
			continue
		}
		token, err := s.ResolveAPIToken(ctx, integration.ID)
		if err != nil || strings.TrimSpace(token) == "" {
			// Skip this integration (disabled, no token, undecryptable) without
			// failing the whole list; other integrations may still be usable.
			continue
		}
		apiURL := gitLabAPIBase(integration.BaseURL)
		resources, err := s.resources.ListResources(ctx, integration.ID)
		if err != nil {
			return nil, err
		}
		for _, resource := range resources {
			if resource.ResourceType != ResourceTypeProject {
				continue
			}
			out = append(out, GitLabRepoConfig{
				ProjectID:  strings.TrimSpace(resource.ExternalKey),
				APIURL:     apiURL,
				Token:      token,
				DefaultRef: strings.TrimSpace(resource.DefaultRef),
				Bearer:     strings.TrimSpace(integration.AuthMethod) == AuthMethodOAuth,
			})
		}
	}
	return out, nil
}

// RepoConfigHash returns a deterministic hash over the dimensions that change
// the built GitLab repo-provider set: each connected gitlab integration's
// {id, token-presence, base url} plus each project resource's {external_key,
// default_ref}. It never decrypts a token (presence only), so the dynamic MCP
// handler rebuilds when integrations change. Returns empty string on error.
func (s *Service) RepoConfigHash() string {
	type hashResource struct {
		ExternalKey string `json:"external_key"`
		DefaultRef  string `json:"default_ref"`
	}
	type hashRow struct {
		ID         string         `json:"id"`
		HasToken   bool           `json:"has_token"`
		BaseURL    string         `json:"base_url"`
		AuthMethod string         `json:"auth_method"`
		Resources  []hashResource `json:"resources"`
	}

	integrations, err := s.integrations.ListIntegrations(context.Background())
	if err != nil {
		return ""
	}
	var rows []hashRow
	for _, integration := range integrations {
		if integration.Provider != ProviderGitLab || !integrationHasResolvedToken(integration) {
			continue
		}
		if integration.Status == StatusDisabled {
			continue
		}
		resources, err := s.resources.ListResources(context.Background(), integration.ID)
		if err != nil {
			return ""
		}
		row := hashRow{
			ID:         strings.TrimSpace(integration.ID),
			HasToken:   integrationHasResolvedToken(integration),
			BaseURL:    strings.TrimSpace(integration.BaseURL),
			AuthMethod: strings.TrimSpace(integration.AuthMethod),
		}
		for _, resource := range resources {
			if resource.ResourceType != ResourceTypeProject {
				continue
			}
			row.Resources = append(row.Resources, hashResource{
				ExternalKey: strings.TrimSpace(resource.ExternalKey),
				DefaultRef:  strings.TrimSpace(resource.DefaultRef),
			})
		}
		sort.Slice(row.Resources, func(i, j int) bool {
			return row.Resources[i].ExternalKey < row.Resources[j].ExternalKey
		})
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	b, err := json.Marshal(rows)
	if err != nil {
		return ""
	}
	sum := sha1.Sum(b)
	return hex.EncodeToString(sum[:])
}
