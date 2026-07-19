// Package githubapi is a minimal GitHub REST client for repositories and hooks.
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

// ClientConfig configures a GitHub REST API client.
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

type CreateRepositoryWebhookRequest struct {
	URL    string
	Secret string
}

type RepositoryWebhook struct {
	ID int `json:"id"`
}

const maxGitHubResponseBytes = 4 << 20

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

func readGitHubResponse(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxGitHubResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxGitHubResponseBytes {
		return nil, fmt.Errorf("github response exceeds %d bytes", maxGitHubResponseBytes)
	}
	return data, nil
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
	raw, err := readGitHubResponse(resp.Body)
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
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	raw, err := readGitHubResponse(resp.Body)
	if err != nil {
		return fmt.Errorf("github webhook delete read: %w", err)
	}
	return fmt.Errorf("github webhook delete: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
}
