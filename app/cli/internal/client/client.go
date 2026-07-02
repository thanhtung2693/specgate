package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxResponseBody = 4 << 20 // 4 MiB

// Client makes typed HTTP calls to a SpecGate /api/v1/ endpoint.
type Client struct {
	base string
	http *http.Client
}

// New creates a Client for baseURL with the given request timeout.
func New(baseURL string, timeout time.Duration) *Client {
	return &Client{
		base: baseURL,
		http: &http.Client{Timeout: timeout},
	}
}

// Meta calls GET /api/v1/meta.
func (c *Client) Meta(ctx context.Context) (*Meta, error) {
	var m Meta
	if err := c.get(ctx, "/api/v1/meta", &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Status calls GET /api/v1/status.
func (c *Client) Status(ctx context.Context, workspaceID string) (*GovernanceStatus, error) {
	path := "/api/v1/status"
	if workspaceID != "" {
		path += "?" + url.Values{"workspace_id": {workspaceID}}.Encode()
	}
	var s GovernanceStatus
	if err := c.get(ctx, path, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Stats calls GET /api/v1/stats.
func (c *Client) Stats(ctx context.Context, workspaceID string, days int) (*StatsResult, error) {
	q := url.Values{}
	if workspaceID != "" {
		q.Set("workspace_id", workspaceID)
	}
	if days > 0 {
		q.Set("days", strconv.Itoa(days))
	}
	path := "/api/v1/stats"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var s StatsResult
	if err := c.get(ctx, path, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Healthz probes GET /healthz. Returns nil for both 200 and 404 (endpoint optional).
func (c *Client) Healthz(ctx context.Context) error {
	req, err := c.newRequest(ctx, http.MethodGet, "/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil // optional endpoint
	}
	if resp.StatusCode >= 400 {
		return &APIError{Kind: ErrorUnavailable, Status: resp.StatusCode}
	}
	return nil
}

// ResolveWorkRef calls POST /api/v1/work-items/resolve to resolve a ref
// (change-request key, issue URL, tracker key, etc.) to a canonical work item.
func (c *Client) ResolveWorkRef(ctx context.Context, ref string) (*ResolvedWork, error) {
	var r ResolvedWork
	if err := c.post(ctx, "/api/v1/work-items/resolve", map[string]string{"ref": ref}, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ContextPack calls GET /api/v1/work-items/{id}/context-pack.
// lane is optional (fe | be | empty for full pack).
func (c *Client) ContextPack(ctx context.Context, id, lane string) (*ContextPackResult, error) {
	path := "/api/v1/work-items/" + url.PathEscape(id) + "/context-pack"
	if lane != "" {
		path += "?" + url.Values{"lane": {lane}}.Encode()
	}
	var r ContextPackResult
	if err := c.get(ctx, path, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// CreateQuickWorkItem calls POST /api/v1/work-items with an opaque JSON body.
func (c *Client) CreateQuickWorkItem(ctx context.Context, in map[string]any) (map[string]any, error) {
	var result map[string]any
	if err := c.post(ctx, "/api/v1/work-items", in, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ArchiveWorkItem calls POST /api/v1/work-items/{id}/archive.
func (c *Client) ArchiveWorkItem(ctx context.Context, id string, reason string, actor string) (map[string]any, error) {
	body := map[string]string{}
	if reason != "" {
		body["reason"] = reason
	}
	if actor != "" {
		body["actor"] = actor
	}
	var result map[string]any
	if err := c.post(ctx, "/api/v1/work-items/"+url.PathEscape(id)+"/archive", body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ListArtifacts calls GET /api/v1/artifacts with optional filters.
func (c *Client) ListArtifacts(ctx context.Context, filter ArtifactFilter) (*ArtifactList, error) {
	vals := url.Values{}
	if filter.FeatureID != "" {
		vals.Set("feature_id", filter.FeatureID)
	}
	if filter.Status != "" {
		vals.Set("status", filter.Status)
	}
	if filter.Limit > 0 {
		vals.Set("limit", strconv.Itoa(filter.Limit))
	}
	if filter.Offset > 0 {
		vals.Set("offset", strconv.Itoa(filter.Offset))
	}
	path := "/api/v1/artifacts"
	if len(vals) > 0 {
		path += "?" + vals.Encode()
	}
	var r ArtifactList
	if err := c.get(ctx, path, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// GetArtifact calls GET /api/v1/artifacts/{id}.
func (c *Client) GetArtifact(ctx context.Context, id string) (*Artifact, error) {
	var r Artifact
	if err := c.get(ctx, "/api/v1/artifacts/"+url.PathEscape(id), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListArtifactFiles calls GET /api/v1/artifacts/{id}/files.
func (c *Client) ListArtifactFiles(ctx context.Context, id string) ([]ArtifactFile, error) {
	var r struct {
		Items []ArtifactFile `json:"items"`
	}
	if err := c.get(ctx, "/api/v1/artifacts/"+url.PathEscape(id)+"/files", &r); err != nil {
		return nil, err
	}
	return r.Items, nil
}

// GetArtifactFile calls GET /api/v1/artifacts/{id}/files/_?path=<filePath>.
// The ?path= query param takes precedence over {key} in the server handler.
func (c *Client) GetArtifactFile(ctx context.Context, id, filePath string) (*ArtifactFileContent, error) {
	path := "/api/v1/artifacts/" + url.PathEscape(id) + "/files/_?" + url.Values{"path": {filePath}}.Encode()
	var r ArtifactFileContent
	if err := c.get(ctx, path, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// DraftProposal calls POST /api/v1/artifacts/{id}/proposals.
func (c *Client) DraftProposal(ctx context.Context, artifactID string, body map[string]any) (*ProposalResult, error) {
	var r ProposalResult
	if err := c.post(ctx, "/api/v1/artifacts/"+url.PathEscape(artifactID)+"/proposals", body, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListSkills calls GET /api/v1/skills with an optional name prefix filter.
func (c *Client) ListSkills(ctx context.Context, nameFilter string) ([]Skill, error) {
	path := "/api/v1/skills"
	if nameFilter != "" {
		path += "?" + url.Values{"name": {nameFilter}}.Encode()
	}
	var r struct {
		Items []Skill `json:"items"`
	}
	if err := c.get(ctx, path, &r); err != nil {
		return nil, err
	}
	return r.Items, nil
}

// GetSkill calls GET /api/v1/skills/{id}.
func (c *Client) GetSkill(ctx context.Context, id string) (*Skill, error) {
	var r Skill
	if err := c.get(ctx, "/api/v1/skills/"+url.PathEscape(id), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListFeatures calls GET /workboard/features. When search is non-empty it
// filters case-insensitively by feature key or name.
func (c *Client) ListFeatures(ctx context.Context, search string) ([]Feature, error) {
	var r struct {
		Items []Feature `json:"items"`
	}
	if err := c.get(ctx, "/workboard/features", &r); err != nil {
		return nil, err
	}
	if search == "" {
		return r.Items, nil
	}
	q := strings.ToLower(strings.TrimSpace(search))
	out := make([]Feature, 0, len(r.Items))
	for _, f := range r.Items {
		if strings.Contains(strings.ToLower(f.Key), q) || strings.Contains(strings.ToLower(f.Name), q) {
			out = append(out, f)
		}
	}
	return out, nil
}

// GetFeature resolves a feature by its key or id from the feature list.
func (c *Client) GetFeature(ctx context.Context, ref string) (*Feature, error) {
	features, err := c.ListFeatures(ctx, "")
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(strings.TrimSpace(ref))
	for i := range features {
		if strings.ToLower(features[i].Key) == needle || strings.ToLower(features[i].ID) == needle {
			return &features[i], nil
		}
	}
	return nil, fmt.Errorf("feature %q not found", ref)
}

// PublishArtifact calls POST /api/v1/artifacts/publish with an opaque JSON body.
func (c *Client) PublishArtifact(ctx context.Context, body map[string]any) (map[string]any, error) {
	var result map[string]any
	if err := c.post(ctx, "/api/v1/artifacts/publish", body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// RunArtifactReadiness calls POST /api/v1/work-items/{id}/readiness.
// Despite the "work-items" path, id is the artifact UUID (server implementation detail).
func (c *Client) RunArtifactReadiness(ctx context.Context, artifactID string) (map[string]any, error) {
	var result map[string]any
	if err := c.post(ctx, "/api/v1/work-items/"+url.PathEscape(artifactID)+"/readiness", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// RunLLMGates calls POST /api/v1/work-items/{id}/llm-gates.
func (c *Client) RunLLMGates(ctx context.Context, id string) (map[string]any, error) {
	var result map[string]any
	if err := c.post(ctx, "/api/v1/work-items/"+url.PathEscape(id)+"/llm-gates", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GatesStatus calls GET /api/v1/work-items/{id}/gates — returns persisted gate state.
func (c *Client) GatesStatus(ctx context.Context, id string) (*GatesStatusResult, error) {
	var r GatesStatusResult
	if err := c.get(ctx, "/api/v1/work-items/"+url.PathEscape(id)+"/gates", &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// GateHistory calls GET /api/v1/work-items/{id}/gate-history with optional filters.
func (c *Client) GateHistory(ctx context.Context, id, gate string, limit int) (*GateHistoryResult, error) {
	q := url.Values{}
	if gate != "" {
		q.Set("gate", gate)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	path := "/api/v1/work-items/" + url.PathEscape(id) + "/gate-history"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var r GateHistoryResult
	if err := c.get(ctx, path, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ReportFeedback calls POST /api/v1/work-items/{id}/feedback with an opaque JSON body.
func (c *Client) ReportFeedback(ctx context.Context, id string, body map[string]any) (map[string]any, error) {
	var result map[string]any
	if err := c.post(ctx, "/api/v1/work-items/"+url.PathEscape(id)+"/feedback", body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ListAcceptanceCriteria calls GET /workboard/change-requests/{id}/acceptance-criteria.
func (c *Client) ListAcceptanceCriteria(ctx context.Context, id string) ([]AcceptanceCriterion, error) {
	var r struct {
		Items []AcceptanceCriterion `json:"items"`
	}
	if err := c.get(ctx, "/workboard/change-requests/"+url.PathEscape(id)+"/acceptance-criteria", &r); err != nil {
		return nil, err
	}
	return r.Items, nil
}

// TriggerDeliveryReview calls POST /api/v1/work-items/{id}/delivery-review.
func (c *Client) TriggerDeliveryReview(ctx context.Context, id string) (map[string]any, error) {
	var result map[string]any
	if err := c.post(ctx, "/api/v1/work-items/"+url.PathEscape(id)+"/delivery-review", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// DeliveryStatus calls GET /api/v1/work-items/{id}/delivery-status.
func (c *Client) DeliveryStatus(ctx context.Context, id string, detail bool) (*DeliveryStatusResult, error) {
	path := "/api/v1/work-items/" + url.PathEscape(id) + "/delivery-status"
	if detail {
		path += "?detail=true"
	}
	var r DeliveryStatusResult
	if err := c.get(ctx, path, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListGovernanceLevels calls GET /api/v1/policies/levels — returns the
// execution projection for each of the three built-in governance tiers.
func (c *Client) ListGovernanceLevels(ctx context.Context) ([]GovernanceLevel, error) {
	var r struct {
		Levels []GovernanceLevel `json:"levels"`
	}
	if err := c.get(ctx, "/api/v1/policies/levels", &r); err != nil {
		return nil, err
	}
	return r.Levels, nil
}

// ResolvePolicy calls POST /api/v1/policies/resolve — dry-runs the governance
// level resolution for a proposed change.
func (c *Client) ResolvePolicy(ctx context.Context, in ResolvePolicyInput) (*PolicyExplanation, error) {
	var r PolicyExplanation
	if err := c.post(ctx, "/api/v1/policies/resolve", in, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// WorkPolicy calls GET /api/v1/work-items/{id}/policy — returns the
// governance explanation for the lead artifact of a change request.
func (c *Client) WorkPolicy(ctx context.Context, ref string) (*PolicyExplanation, error) {
	var r PolicyExplanation
	if err := c.get(ctx, "/api/v1/work-items/"+url.PathEscape(ref)+"/policy", &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// --- transport helpers ---

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, body, out)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		return c.parseError(resp.StatusCode, data)
	}

	if out == nil {
		return nil
	}

	// Unwrap Huma v2 { "body": ... } envelope.
	var wrapper struct {
		Body json.RawMessage `json:"body"`
	}
	if jsonErr := json.Unmarshal(data, &wrapper); jsonErr == nil && len(wrapper.Body) > 0 && wrapper.Body[0] == '{' {
		return json.Unmarshal(wrapper.Body, out)
	}
	return json.Unmarshal(data, out)
}

func (c *Client) newRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	base, err := url.Parse(c.base)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	ref, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("parse request path: %w", err)
	}
	target := base.ResolveReference(ref).String()

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, target, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *Client) parseError(status int, data []byte) *APIError {
	// Try to decode RFC 9457 / Huma error body.
	var rfc struct {
		Title  string `json:"title"`
		Detail string `json:"detail"`
		Errors []struct {
			Message  string `json:"message"`
			Location string `json:"location,omitempty"`
			Value    any    `json:"value,omitempty"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(data, &rfc)

	kind := ErrorGeneric
	switch status {
	case http.StatusBadRequest:
		kind = ErrorUsage
	case http.StatusNotFound:
		kind = ErrorNotFound
	case http.StatusConflict:
		kind = ErrorConflict
	case http.StatusUnprocessableEntity:
		kind = ErrorIncompatible
	case http.StatusServiceUnavailable:
		kind = ErrorUnavailable
	}

	msg := rfc.Title
	if msg == "" {
		msg = fmt.Sprintf("HTTP %d", status)
	}
	details := map[string]any{}
	if len(rfc.Errors) > 0 {
		errors := make([]map[string]any, 0, len(rfc.Errors))
		for _, e := range rfc.Errors {
			item := map[string]any{"message": e.Message}
			if e.Location != "" {
				item["location"] = e.Location
			}
			if e.Value != nil {
				item["value"] = e.Value
			}
			errors = append(errors, item)
		}
		details["errors"] = errors
	}
	return &APIError{Kind: kind, Status: status, Message: msg, Detail: rfc.Detail, Details: details}
}
