// Package agentsclient is a thin HTTP client for the agents service's
// readiness endpoint. It backs the specgate_check_readiness MCP tool, which
// delegates the LLM gate compute to agents (POST /artifacts/{id}/run-readiness).
//
// The agents service is un-authed on the internal network (the UI already calls
// it directly), so this client carries no credentials. It is nil/disabled when
// no base URL is configured, so the MCP layer can choose not to register the
// tool.
package agentsclient

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

// DefaultTimeout is generous because run-readiness runs LLM gates server-side.
const DefaultTimeout = 120 * time.Second

// ReadinessRun is one gate verdict in the readiness response. It mirrors the
// agents run-readiness payload and the Doc Registry readiness-run row.
type ReadinessRun struct {
	Gate         string `json:"gate"`
	State        string `json:"state"`
	Hint         string `json:"hint,omitempty"`
	EvidenceJSON string `json:"evidence_json,omitempty"`
	JudgeModel   string `json:"judge_model,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
}

// Verdict is the decoded agents run-readiness response.
type Verdict struct {
	ArtifactID        string         `json:"artifact_id"`
	EvaluationsPosted int            `json:"evaluations_posted,omitempty"`
	ReadinessRuns     []ReadinessRun `json:"readiness_runs"`
}

// Client posts to the agents run-readiness endpoint and decodes the verdict.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a Client for the given agents base URL, or nil when the base URL
// is empty (readiness is disabled — the MCP layer skips tool registration).
func New(baseURL string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: DefaultTimeout},
	}
}

// RunReadiness runs the artifact's readiness gates via the agents service and
// returns the decoded verdict. Transport failures and non-2xx responses return
// a clear error (the caller surfaces "readiness unavailable" to the IDE); it
// never panics.
func (c *Client) RunReadiness(ctx context.Context, artifactID string) (*Verdict, error) {
	if c == nil {
		return nil, fmt.Errorf("readiness unavailable: agents service not configured")
	}
	body, err := c.postEmpty(ctx, c.baseURL+"/artifacts/"+artifactID+"/run-readiness", "readiness unavailable")
	if err != nil {
		return nil, err
	}
	var v Verdict
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, fmt.Errorf("readiness unavailable: decode agents response: %w", err)
	}
	return &v, nil
}

// RunLLMGates runs all LLM quality gates for the change request's lead artifact
// and posts the verdicts to Doc Registry. Returns the raw JSON response from
// the agents service so the MCP layer can forward it to the IDE unchanged.
func (c *Client) RunLLMGates(ctx context.Context, changeRequestID string) (map[string]any, error) {
	if c == nil {
		return nil, fmt.Errorf("run-llm-gates unavailable: agents service not configured")
	}
	body, err := c.postEmpty(ctx, c.baseURL+"/workboard/change-requests/"+changeRequestID+"/run-llm-gates", "run-llm-gates unavailable")
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("run-llm-gates unavailable: decode agents response: %w", err)
	}
	return out, nil
}

// ReviewDelivery triggers the delivery review for a change request and returns
// the raw verdict from the agents service.
func (c *Client) ReviewDelivery(ctx context.Context, changeRequestID string) (map[string]any, error) {
	if c == nil {
		return nil, fmt.Errorf("review-delivery unavailable: agents service not configured")
	}
	body, err := c.postEmpty(ctx, c.baseURL+"/workboard/change-requests/"+changeRequestID+"/review-delivery", "review-delivery unavailable")
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("review-delivery unavailable: decode agents response: %w", err)
	}
	return out, nil
}

// CreateQuickWorkItem creates a quick-route change request from issue content
// submitted by a developer in the IDE. The agents service drafts ACs via LLM,
// creates the feature+CR, and generates the quick context pack.
func (c *Client) CreateQuickWorkItem(ctx context.Context, title, description, issueURL, issueKey, featureKey, featureName string, acceptanceCriteria []string, createdBy string, workspaceID string) (map[string]any, error) {
	if c == nil {
		return nil, fmt.Errorf("create-quick-work-item unavailable: agents service not configured")
	}
	payload, err := json.Marshal(map[string]any{
		"title":               title,
		"description":         description,
		"issue_url":           issueURL,
		"issue_key":           issueKey,
		"feature_key":         featureKey,
		"feature_name":        featureName,
		"acceptance_criteria": acceptanceCriteria,
		"created_by":          createdBy,
		"workspace_id":        workspaceID,
	})
	if err != nil {
		return nil, fmt.Errorf("create-quick-work-item unavailable: marshal request: %w", err)
	}
	body, err := c.postJSON(ctx, c.baseURL+"/workboard/quick-work-item", payload, "create-quick-work-item unavailable")
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("create-quick-work-item unavailable: decode agents response: %w", err)
	}
	return out, nil
}

// postEmpty sends an empty-body POST to url and returns the raw response bytes.
// errPrefix is prepended to all error messages for caller attribution.
func (c *Client) postEmpty(ctx context.Context, url, errPrefix string) ([]byte, error) {
	return c.postJSON(ctx, url, []byte("{}"), errPrefix)
}

// postJSON sends a POST with body to url and returns the raw response bytes.
// errPrefix is prepended to all error messages for caller attribution.
func (c *Client) postJSON(ctx context.Context, url string, body []byte, errPrefix string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%s: build request: %w", errPrefix, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: agents request failed: %w", errPrefix, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("%s: read agents response: %w", errPrefix, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := strings.TrimSpace(string(respBody))
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return nil, fmt.Errorf("%s: agents returned %d: %s", errPrefix, resp.StatusCode, snippet)
	}
	return respBody, nil
}
