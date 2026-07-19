package githubapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Repo is a GitHub repository summary for the repo picker.
type Repo struct {
	ID            int    `json:"id"`
	FullName      string `json:"full_name"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
}

// ListRepos returns repositories the token can access, most-recently-updated
// first. GitHub's list endpoint has no server-side name filter, so when `search`
// is set the fetched page is filtered client-side by case-insensitive substring
// on full_name. `limit` caps the fetched page (1..100; out-of-range falls back
// to 50). LIVE path; mocked in tests via a custom APIURL. Repo is not used —
// the list is account-scoped.
func (c *Client) ListRepos(ctx context.Context, search string, limit int) ([]Repo, error) {
	if c.apiURL == "" {
		return nil, fmt.Errorf("github list repos: api url is required")
	}
	if c.token == "" {
		return nil, fmt.Errorf("github list repos: token is required")
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
	q.Set("sort", "updated")
	q.Set("affiliation", "owner,collaborator,organization_member")
	reqURL := fmt.Sprintf("%s/user/repos?%s", c.apiURL, q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github list repos: %w", err)
	}
	defer resp.Body.Close()
	raw, err := readGitHubResponse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("github list repos read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github list repos: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var all []Repo
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, fmt.Errorf("github list repos decode: %w", err)
	}
	s := strings.ToLower(strings.TrimSpace(search))
	if s == "" {
		return all, nil
	}
	out := make([]Repo, 0, len(all))
	for _, r := range all {
		if strings.Contains(strings.ToLower(r.FullName), s) {
			out = append(out, r)
		}
	}
	return out, nil
}
