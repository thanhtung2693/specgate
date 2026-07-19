package githubapi

import (
	"net/url"
	"strings"
)

// APIURL returns the hosted GitHub API root for github.com and the conventional
// GitHub Enterprise API root for every other configured host.
func APIURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return "https://api.github.com"
	}
	if parsed, err := url.Parse(base); err == nil {
		host := strings.ToLower(parsed.Hostname())
		if host == "github.com" || host == "www.github.com" {
			return "https://api.github.com"
		}
	}
	return base + "/api/v3"
}
