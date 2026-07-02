package mcp

import (
	"net/url"
	"strings"
)

// NormalizeGitLabAPIURL ensures a root URL like https://gitlab.com becomes .../api/v4.
// integrations.ListGitLabRepoConfigs returns a host-only APIURL (scheme://host);
// the repo provider appends /projects/... directly, so callers that build a
// provider from a repo config must add /api/v4 first via this helper.
func NormalizeGitLabAPIURL(apiURL string) string {
	return normalizeGitLabAPIURL(apiURL)
}

func normalizeGitLabAPIURL(apiURL string) string {
	apiURL = strings.TrimSuffix(strings.TrimSpace(apiURL), "/")
	if apiURL == "" {
		return ""
	}
	u, err := url.Parse(apiURL)
	if err != nil {
		return apiURL
	}
	if strings.Trim(u.Path, "/") == "" {
		u.Path = "/api/v4"
		return strings.TrimSuffix(u.String(), "/")
	}
	return apiURL
}
