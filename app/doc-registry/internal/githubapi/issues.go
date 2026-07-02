// Package githubapi is a minimal GitHub REST client for outbound issue
// creation. It mirrors internal/gitlabapi: a provider-neutral CreateIssue
// surface over POST /repos/{owner}/{repo}/issues, authed with a personal access
// token sent as a Bearer Authorization header.
package githubapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ClientConfig configures a GitHub REST API client for outbound issue creation.
// APIURL is the API root (https://api.github.com for github.com, or
// https://{host}/api/v3 for GitHub Enterprise); Token is a personal access
// token sent as the Bearer Authorization header; Repo is the owner/repo slug the
// issue is filed under.
type ClientConfig struct {
	APIURL     string
	Token      string
	Repo       string
	HTTPClient *http.Client
}

type Client struct {
	apiURL     string
	token      string
	repo       string
	httpClient *http.Client
}

// CreateIssueRequest is the provider-neutral outbound issue fields mapped onto
// GitHub's POST /repos/{owner}/{repo}/issues body.
type CreateIssueRequest struct {
	Title string
	Body  string
}

// Issue is the created GitHub issue handle returned to the caller. GitHub
// numbers issues per-repo (Number) and returns the canonical permalink in
// HTMLURL.
type Issue struct {
	URL    string `json:"url"`
	Number int    `json:"number"`
}

type CreateRepositoryWebhookRequest struct {
	URL    string
	Secret string
}

type RepositoryWebhook struct {
	ID int `json:"id"`
}

func NewClient(cfg ClientConfig) *Client {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{
		apiURL:     strings.TrimSuffix(strings.TrimSpace(cfg.APIURL), "/"),
		token:      strings.TrimSpace(cfg.Token),
		repo:       strings.Trim(strings.TrimSpace(cfg.Repo), "/"),
		httpClient: client,
	}
}

// CreateIssue creates an issue under the configured owner/repo via the GitHub
// REST API. The token is sent as a Bearer Authorization header (the convention
// the hosted API + GitHub Enterprise both accept) alongside the documented
// Accept media type. LIVE path; mocked in tests via a custom APIURL.
func (c *Client) CreateIssue(ctx context.Context, in CreateIssueRequest) (*Issue, error) {
	if c.apiURL == "" {
		return nil, fmt.Errorf("github issue create: api url is required")
	}
	if c.token == "" {
		return nil, fmt.Errorf("github issue create: token is required")
	}
	if c.repo == "" {
		return nil, fmt.Errorf("github issue create: repo (owner/repo) is required")
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, fmt.Errorf("github issue create: title is required")
	}

	body := map[string]string{
		"title": title,
		"body":  in.Body,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	reqURL := fmt.Sprintf("%s/repos/%s/issues", c.apiURL, c.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github issue create: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("github issue create read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github issue create: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("github issue create decode: %w", err)
	}
	return &Issue{URL: out.HTMLURL, Number: out.Number}, nil
}

// CloseIssue closes an existing issue via PATCH /repos/{owner}/{repo}/issues/{number}
// with state=closed and state_reason=completed. Best-effort: the caller decides
// whether to propagate the error.
func (c *Client) CloseIssue(ctx context.Context, issueNumber int) error {
	if c.apiURL == "" {
		return fmt.Errorf("github issue close: api url is required")
	}
	if c.token == "" {
		return fmt.Errorf("github issue close: token is required")
	}
	if c.repo == "" {
		return fmt.Errorf("github issue close: repo (owner/repo) is required")
	}
	if issueNumber <= 0 {
		return fmt.Errorf("github issue close: issue number must be positive")
	}
	payload, err := json.Marshal(map[string]string{"state": "closed", "state_reason": "completed"})
	if err != nil {
		return err
	}
	reqURL := fmt.Sprintf("%s/repos/%s/issues/%d", c.apiURL, c.repo, issueNumber)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, reqURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("github issue close: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("github issue close: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (c *Client) CreateRepositoryWebhook(ctx context.Context, in CreateRepositoryWebhookRequest) (*RepositoryWebhook, error) {
	if c.apiURL == "" {
		return nil, fmt.Errorf("github webhook create: api url is required")
	}
	if c.token == "" {
		return nil, fmt.Errorf("github webhook create: token is required")
	}
	if c.repo == "" {
		return nil, fmt.Errorf("github webhook create: repo (owner/repo) is required")
	}
	url := strings.TrimSpace(in.URL)
	if url == "" {
		return nil, fmt.Errorf("github webhook create: url is required")
	}
	secret := strings.TrimSpace(in.Secret)
	if secret == "" {
		return nil, fmt.Errorf("github webhook create: secret is required")
	}

	body := map[string]any{
		"name":   "web",
		"active": true,
		"events": []string{"pull_request", "issue_comment"},
		"config": map[string]string{
			"url":          url,
			"content_type": "json",
			"insecure_ssl": "0",
			"secret":       secret,
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	reqURL := fmt.Sprintf("%s/repos/%s/hooks", c.apiURL, c.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github webhook create: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("github webhook create read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github webhook create: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("github webhook create decode: %w", err)
	}
	return &RepositoryWebhook{ID: out.ID}, nil
}

func (c *Client) DeleteRepositoryWebhook(ctx context.Context, hookID string) error {
	if c.apiURL == "" {
		return fmt.Errorf("github webhook delete: api url is required")
	}
	if c.token == "" {
		return fmt.Errorf("github webhook delete: token is required")
	}
	if c.repo == "" {
		return fmt.Errorf("github webhook delete: repo (owner/repo) is required")
	}
	hookID = strings.TrimSpace(hookID)
	if hookID == "" {
		return fmt.Errorf("github webhook delete: hook id is required")
	}
	reqURL := fmt.Sprintf("%s/repos/%s/hooks/%s", c.apiURL, c.repo, hookID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("github webhook delete: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("github webhook delete read: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("github webhook delete: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}
