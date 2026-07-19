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

// ClientConfig configures a GitLab REST API client.
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

type CreateProjectWebhookRequest struct {
	URL          string
	SigningToken string
}

type ProjectWebhook struct {
	ID int `json:"id"`
	// SigningTokenPresent reflects GitLab's response field. False means inbound
	// webhook-signature verification can never pass, so provisioning fails.
	SigningTokenPresent bool `json:"signing_token_present"`
}

const maxGitLabResponseBytes = 4 << 20

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

func readGitLabResponse(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxGitLabResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxGitLabResponseBytes {
		return nil, fmt.Errorf("gitlab response exceeds %d bytes", maxGitLabResponseBytes)
	}
	return data, nil
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
	if strings.TrimSpace(in.SigningToken) == "" {
		return nil, fmt.Errorf("gitlab webhook create: signing token is required")
	}

	body := map[string]any{
		"url":                     url,
		"note_events":             true,
		"merge_requests_events":   true,
		"enable_ssl_verification": true,
	}
	if t := strings.TrimSpace(in.SigningToken); t != "" {
		body["signing_token"] = t
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
	raw, err := readGitLabResponse(resp.Body)
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
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	raw, err := readGitLabResponse(resp.Body)
	if err != nil {
		return fmt.Errorf("gitlab webhook delete read: %w", err)
	}
	return fmt.Errorf("gitlab webhook delete: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
}
