package gitlabapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Project is a GitLab project summary for the repo picker.
type Project struct {
	ID            int    `json:"id"`
	PathWithNS    string `json:"path_with_namespace"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
}

// ListProjects returns projects the token is a member of in GitLab's default
// order (created_at, newest first), optionally filtered server-side by `search`.
// `limit` caps results (1..100; out-of-range falls back to 50). LIVE path;
// mocked in tests via a custom APIURL. ProjectID is not used — the list is
// account-scoped.
func (c *Client) ListProjects(ctx context.Context, search string, limit int) ([]Project, error) {
	if c.apiURL == "" {
		return nil, fmt.Errorf("gitlab list projects: api url is required")
	}
	if c.token == "" {
		return nil, fmt.Errorf("gitlab list projects: token is required")
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := url.Values{}
	q.Set("membership", "true")
	// Default ordering (created_at, indexed) — GitLab.com returns 500 (statement
	// timeout) on `order_by=last_activity_at` combined with membership=true for
	// accounts in many groups.
	q.Set("per_page", strconv.Itoa(limit))
	q.Set("simple", "true")
	if s := strings.TrimSpace(search); s != "" {
		q.Set("search", s)
	}
	reqURL := fmt.Sprintf("%s/projects?%s", c.apiURL, q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	c.setAuthHeader(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitlab list projects: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gitlab list projects read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gitlab list projects: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out []Project
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("gitlab list projects decode: %w", err)
	}
	return out, nil
}
