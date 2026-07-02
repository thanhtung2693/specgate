package gitlabapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ClientConfig configures a GitLab REST API client for outbound issue creation.
// APIURL is the API root (e.g. https://gitlab.example/api/v4); Token is the
// access token; ProjectID is the numeric id or namespace path the issue is
// filed under. Bearer selects the auth header: GitLab requires OAuth access
// tokens on `Authorization: Bearer`, while a personal/project access token uses
// the bare `PRIVATE-TOKEN` header (the default).
type ClientConfig struct {
	APIURL     string
	Token      string
	ProjectID  string
	Bearer     bool
	HTTPClient *http.Client
}

type Client struct {
	apiURL     string
	token      string
	projectID  string
	bearer     bool
	httpClient *http.Client
}

// CreateIssueRequest is the provider-neutral outbound issue fields mapped onto
// GitLab's POST /projects/:id/issues body.
type CreateIssueRequest struct {
	Title       string
	Description string
}

// Issue is the created GitLab issue handle returned to the caller.
type Issue struct {
	URL string `json:"url"`
	IID int    `json:"iid"`
}

type CreateProjectWebhookRequest struct {
	URL          string
	SigningToken string
	// SecretToken is the legacy X-Gitlab-Token verbatim secret, sent alongside
	// SigningToken as a fallback: GitLab < 19.0 ignores SigningToken but honors
	// this, so the receiver can verify X-Gitlab-Token when the signing token was
	// not stored.
	SecretToken string
}

type ProjectWebhook struct {
	ID int `json:"id"`
	// SigningTokenPresent reflects GitLab's response field: true when the hook
	// stored a signing token. GitLab < 19.0 silently ignores the signing_token
	// body field (introduced in 19.0), so a false here means inbound
	// webhook-signature verification can never pass — the caller should treat it
	// as a provisioning failure.
	SigningTokenPresent bool `json:"signing_token_present"`
}

func NewClient(cfg ClientConfig) *Client {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{
		apiURL:     strings.TrimSuffix(cfg.APIURL, "/"),
		token:      strings.TrimSpace(cfg.Token),
		projectID:  strings.TrimSpace(cfg.ProjectID),
		bearer:     cfg.Bearer,
		httpClient: client,
	}
}

// setAuthHeader applies the GitLab auth header for the client's token. OAuth
// access tokens must travel as `Authorization: Bearer`; personal/project access
// tokens use the bare `PRIVATE-TOKEN` header. Sending an OAuth token as
// PRIVATE-TOKEN yields 401 Unauthorized.
func (c *Client) setAuthHeader(req *http.Request) {
	if c.bearer {
		req.Header.Set("Authorization", "Bearer "+c.token)
		return
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)
}

// CreateIssue creates an issue under the configured project via the GitLab REST
// API. The token is sent as the bare PRIVATE-TOKEN header (the personal/project
// access token convention). LIVE path; mocked in tests via a custom APIURL.
func (c *Client) CreateIssue(ctx context.Context, in CreateIssueRequest) (*Issue, error) {
	if c.apiURL == "" {
		return nil, fmt.Errorf("gitlab issue create: api url is required")
	}
	if c.token == "" {
		return nil, fmt.Errorf("gitlab issue create: token is required")
	}
	if c.projectID == "" {
		return nil, fmt.Errorf("gitlab issue create: project id is required")
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, fmt.Errorf("gitlab issue create: title is required")
	}

	body := map[string]string{
		"title":       title,
		"description": in.Description,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	reqURL := fmt.Sprintf("%s/projects/%s/issues", c.apiURL, ProjectAPIPathSegment(c.projectID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuthHeader(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitlab issue create: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gitlab issue create read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gitlab issue create: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		IID    int    `json:"iid"`
		WebURL string `json:"web_url"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("gitlab issue create decode: %w", err)
	}
	return &Issue{URL: out.WebURL, IID: out.IID}, nil
}

// CloseIssue closes an existing issue via PUT /projects/{id}/issues/{iid}
// with state_event=close. Best-effort: the caller decides whether to propagate
// the error.
func (c *Client) CloseIssue(ctx context.Context, issueIID int) error {
	if c.apiURL == "" {
		return fmt.Errorf("gitlab issue close: api url is required")
	}
	if c.token == "" {
		return fmt.Errorf("gitlab issue close: token is required")
	}
	if c.projectID == "" {
		return fmt.Errorf("gitlab issue close: project id is required")
	}
	if issueIID <= 0 {
		return fmt.Errorf("gitlab issue close: issue iid must be positive")
	}
	payload, err := json.Marshal(map[string]string{"state_event": "close"})
	if err != nil {
		return err
	}
	reqURL := fmt.Sprintf("%s/projects/%s/issues/%d", c.apiURL, ProjectAPIPathSegment(c.projectID), issueIID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, reqURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuthHeader(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gitlab issue close: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gitlab issue close: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (c *Client) CreateProjectWebhook(ctx context.Context, in CreateProjectWebhookRequest) (*ProjectWebhook, error) {
	if c.apiURL == "" {
		return nil, fmt.Errorf("gitlab webhook create: api url is required")
	}
	if c.token == "" {
		return nil, fmt.Errorf("gitlab webhook create: token is required")
	}
	if c.projectID == "" {
		return nil, fmt.Errorf("gitlab webhook create: project id is required")
	}
	url := strings.TrimSpace(in.URL)
	if url == "" {
		return nil, fmt.Errorf("gitlab webhook create: url is required")
	}
	if strings.TrimSpace(in.SigningToken) == "" && strings.TrimSpace(in.SecretToken) == "" {
		return nil, fmt.Errorf("gitlab webhook create: a signing token or secret token is required")
	}

	body := map[string]any{
		"url":                     url,
		"issues_events":           true,
		"note_events":             true,
		"merge_requests_events":   true,
		"enable_ssl_verification": true,
	}
	if t := strings.TrimSpace(in.SigningToken); t != "" {
		body["signing_token"] = t
	}
	if t := strings.TrimSpace(in.SecretToken); t != "" {
		body["token"] = t
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	reqURL := fmt.Sprintf("%s/projects/%s/hooks", c.apiURL, ProjectAPIPathSegment(c.projectID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuthHeader(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitlab webhook create: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gitlab webhook create read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gitlab webhook create: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		ID                  int  `json:"id"`
		SigningTokenPresent bool `json:"signing_token_present"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("gitlab webhook create decode: %w", err)
	}
	return &ProjectWebhook{ID: out.ID, SigningTokenPresent: out.SigningTokenPresent}, nil
}

func (c *Client) DeleteProjectWebhook(ctx context.Context, hookID string) error {
	if c.apiURL == "" {
		return fmt.Errorf("gitlab webhook delete: api url is required")
	}
	if c.token == "" {
		return fmt.Errorf("gitlab webhook delete: token is required")
	}
	if c.projectID == "" {
		return fmt.Errorf("gitlab webhook delete: project id is required")
	}
	hookID = strings.TrimSpace(hookID)
	if hookID == "" {
		return fmt.Errorf("gitlab webhook delete: hook id is required")
	}
	reqURL := fmt.Sprintf("%s/projects/%s/hooks/%s", c.apiURL, ProjectAPIPathSegment(c.projectID), url.PathEscape(hookID))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return err
	}
	c.setAuthHeader(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gitlab webhook delete: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("gitlab webhook delete read: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gitlab webhook delete: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}
