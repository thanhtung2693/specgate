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

// WorkItemSummary is one row from the change-request listing, used by
// `work list --phase` so agents and humans can discover pickup-ready work.
type WorkItemSummary struct {
	ID             string `json:"id"`
	Key            string `json:"key"`
	Title          string `json:"title"`
	IntentMD       string `json:"intent_md,omitempty"`
	Phase          string `json:"phase"`
	WorkType       string `json:"work_type"`
	LeadArtifactID string `json:"lead_artifact_id,omitempty"`
	SourceRefs     string `json:"source_refs_json,omitempty"`
}

// ListWorkItems calls GET /workboard/change-requests (archived hidden by
// default) and returns the summary rows. Phase filtering is applied by the
// caller — the endpoint scopes by workspace but not phase.
func (c *Client) ListWorkItems(ctx context.Context, workspaceID string) ([]WorkItemSummary, error) {
	return c.listWorkItems(ctx, workspaceID, false)
}

// ListWorkItemsIncludingArchived returns every work item for portable-import
// retry matching, including items auto-archived after delivery approval.
func (c *Client) ListWorkItemsIncludingArchived(ctx context.Context, workspaceID string) ([]WorkItemSummary, error) {
	return c.listWorkItems(ctx, workspaceID, true)
}

func (c *Client) listWorkItems(ctx context.Context, workspaceID string, includeArchived bool) ([]WorkItemSummary, error) {
	path := "/workboard/change-requests"
	query := url.Values{}
	if workspaceID != "" {
		query.Set("workspace_id", workspaceID)
	}
	if includeArchived {
		query.Set("include_archived", "true")
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	var r struct {
		Items []WorkItemSummary `json:"items"`
	}
	if err := c.get(ctx, path, &r); err != nil {
		return nil, err
	}
	return r.Items, nil
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

// ComponentHealth returns appliance child-process diagnostics when available.
// A 404 means the configured server is an origin-only/self-host deployment.
func (c *Client) ComponentHealth(ctx context.Context) (*ComponentHealth, error) {
	var result ComponentHealth
	if err := c.get(ctx, "/healthz/components", &result); err != nil {
		if apiErr, ok := err.(*APIError); ok && apiErr.Kind == ErrorNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &result, nil
}

// SchemaStatus returns database schema compatibility diagnostics when available.
func (c *Client) SchemaStatus(ctx context.Context) (*SchemaStatus, error) {
	var result SchemaStatus
	if err := c.get(ctx, "/api/v1/schema/status", &result); err != nil {
		if apiErr, ok := err.(*APIError); ok && apiErr.Kind == ErrorNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &result, nil
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
func (c *Client) ContextPack(ctx context.Context, id string) (*ContextPackResult, error) {
	path := "/api/v1/work-items/" + url.PathEscape(id) + "/context-pack"
	var r ContextPackResult
	if err := c.get(ctx, path, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// AuditTrail calls GET /api/v1/audit/{ref}. The server resolves ref (CR id or
// key) and returns the chronological governance trail.
func (c *Client) AuditTrail(ctx context.Context, ref string, verify bool) (*AuditTrail, error) {
	var r AuditTrail
	path := "/api/v1/audit/" + url.PathEscape(ref)
	if verify {
		path += "?verify=true"
	}
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

// CreateWorkItem calls POST /api/v1/work-items/create — feature-backed work
// item bound to the feature's canonical spec.
func (c *Client) CreateWorkItem(ctx context.Context, in map[string]any) (map[string]any, error) {
	var result map[string]any
	if err := c.post(ctx, "/api/v1/work-items/create", in, &result); err != nil {
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
	if filter.WorkspaceID != "" {
		vals.Set("workspace_id", filter.WorkspaceID)
	}
	if filter.FeatureID != "" {
		vals.Set("feature_id", filter.FeatureID)
	}
	if filter.Status != "" {
		vals.Set("status", filter.Status)
	}
	if filter.ExcludeStatus != "" {
		vals.Set("exclude_status", filter.ExcludeStatus)
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
	return c.GetArtifactInWorkspace(ctx, "", id)
}

func (c *Client) GetArtifactInWorkspace(ctx context.Context, workspaceID, id string) (*Artifact, error) {
	path := "/api/v1/artifacts/" + url.PathEscape(id)
	if workspaceID != "" {
		path += "?" + url.Values{"workspace_id": {workspaceID}}.Encode()
	}
	var r Artifact
	if err := c.get(ctx, path, &r); err != nil {
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
func (c *Client) GetArtifactFile(ctx context.Context, id, filePath string) (*ArtifactFileContent, error) {
	path := "/api/v1/artifacts/" + url.PathEscape(id) + "/files/_?" + url.Values{"path": {filePath}}.Encode()
	var r ArtifactFileContent
	if err := c.get(ctx, path, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// UpdateArtifactStatus calls PATCH /artifacts/{id}/status — the human-decision
// endpoint the web UI uses to approve or request changes on an artifact.
func (c *Client) UpdateArtifactStatus(ctx context.Context, id string, in UpdateArtifactStatusInput) (*Artifact, error) {
	var r Artifact
	if err := c.do(ctx, http.MethodPatch, "/artifacts/"+url.PathEscape(id)+"/status", in, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListSkills calls GET /api/v1/skills with an optional name prefix filter.
func (c *Client) ListSkills(ctx context.Context, nameFilter string) ([]Skill, error) {
	values := url.Values{}
	if workspaceID := WorkspaceID(ctx); workspaceID != "" {
		values.Set("workspace_id", workspaceID)
	}
	if nameFilter != "" {
		values.Set("name", nameFilter)
	}
	path := "/api/v1/skills"
	if query := values.Encode(); query != "" {
		path += "?" + query
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
	path := "/api/v1/skills/" + url.PathEscape(id)
	if workspaceID := WorkspaceID(ctx); workspaceID != "" {
		path += "?" + url.Values{"workspace_id": {workspaceID}}.Encode()
	}
	if err := c.get(ctx, path, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListFeatures calls GET /workboard/features. When search is non-empty it
// filters case-insensitively by feature key or name.
func (c *Client) ListFeatures(ctx context.Context, search string) ([]Feature, error) {
	return c.ListFeaturesInWorkspace(ctx, "", search)
}

func (c *Client) ListFeaturesInWorkspace(ctx context.Context, workspaceID, search string) ([]Feature, error) {
	var r struct {
		Items []Feature `json:"items"`
	}
	path := "/workboard/features"
	if workspaceID != "" {
		path += "?" + url.Values{"workspace_id": {workspaceID}}.Encode()
	}
	if err := c.get(ctx, path, &r); err != nil {
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

// UpdateFeatureStatus calls PATCH /workboard/features/{id} with a sparse
// status-only body (e.g. "archived").
func (c *Client) UpdateFeatureStatus(ctx context.Context, id, status string) (*Feature, error) {
	return c.UpdateFeatureStatusInWorkspace(ctx, "", id, status)
}

func (c *Client) UpdateFeatureStatusInWorkspace(ctx context.Context, workspaceID, id, status string) (*Feature, error) {
	path := "/workboard/features/" + url.PathEscape(id)
	if workspaceID != "" {
		path += "?" + url.Values{"workspace_id": {workspaceID}}.Encode()
	}
	var f Feature
	if err := c.do(ctx, http.MethodPatch, path, map[string]string{"status": status}, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// PromoteArtifactCanonical calls POST
// /workboard/artifacts/{id}/promote-canonical, setting the approved artifact as
// its feature's canonical. Returns the updated feature.
func (c *Client) PromoteArtifactCanonical(ctx context.Context, artifactID, approvedBy string) (*Feature, error) {
	var f Feature
	body := map[string]string{"approved_by": approvedBy}
	if err := c.post(ctx, "/workboard/artifacts/"+url.PathEscape(artifactID)+"/promote-canonical", body, &f); err != nil {
		return nil, err
	}
	return &f, nil
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

// ListArtifactReadinessRuns reads persisted artifact gate evidence without rerunning checks.
func (c *Client) ListArtifactReadinessRuns(ctx context.Context, artifactID string) (map[string]any, error) {
	var result map[string]any
	if err := c.get(ctx, "/artifacts/"+url.PathEscape(artifactID)+"/readiness-runs", &result); err != nil {
		return nil, err
	}
	return result, nil
}

// RunLLMGates calls the /llm-gates endpoint to trigger model-judged quality gates.
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

// ListGovernanceFeedbackEvents calls GET /governance/feedback-events.
func (c *Client) ListGovernanceFeedbackEvents(ctx context.Context, changeRequestID string) ([]GovernanceFeedbackEvent, error) {
	q := url.Values{}
	q.Set("change_request_id", changeRequestID)
	q.Set("limit", "200")
	var result struct {
		Items []GovernanceFeedbackEvent `json:"items"`
	}
	if err := c.get(ctx, "/governance/feedback-events?"+q.Encode(), &result); err != nil {
		return nil, err
	}
	return result.Items, nil
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

// MaintenanceCleanup calls POST /maintenance/cleanup: an immediate retention
// sweep, demo seed removal, and archived work-item purge. Returns the
// per-category deletion counts.
func (c *Client) MaintenanceCleanup(ctx context.Context) (map[string]any, error) {
	var result map[string]any
	path := "/maintenance/cleanup"
	if workspaceID := WorkspaceID(ctx); workspaceID != "" {
		path += "?" + url.Values{"workspace_id": {workspaceID}}.Encode()
	}
	if err := c.post(ctx, path, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// RemoveDemo calls POST /maintenance/demo-remove: the mirror of the demo seed,
// idempotent, touching only the fixed demo IDs. Returns the per-category
// deletion counts.
func (c *Client) RemoveDemo(ctx context.Context) (map[string]any, error) {
	var result map[string]any
	path := "/maintenance/demo-remove"
	if workspaceID := WorkspaceID(ctx); workspaceID != "" {
		path += "?" + url.Values{"workspace_id": {workspaceID}}.Encode()
	}
	if err := c.post(ctx, path, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// TriggerDeliveryReview calls POST /api/v1/work-items/{id}/delivery-review.
func (c *Client) TriggerDeliveryReview(ctx context.Context, id string) (map[string]any, error) {
	var result map[string]any
	if err := c.post(ctx, "/api/v1/work-items/"+url.PathEscape(id)+"/delivery-review", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// DecideDelivery records a human approve/reject decision for a delivery review.
func (c *Client) DecideDelivery(ctx context.Context, id string, in DeliveryDecisionInput) (*DeliveryDecisionResult, error) {
	var result DeliveryDecisionResult
	if err := c.post(ctx, "/api/v1/work-items/"+url.PathEscape(id)+"/delivery-decision", in, &result); err != nil {
		return nil, err
	}
	return &result, nil
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

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil {
		return err
	}
	if len(data) > maxResponseBody {
		return fmt.Errorf("response exceeds 4 MiB limit")
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
	joinedPath := strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(ref.Path, "/")
	joinedRawPath := strings.TrimRight(base.EscapedPath(), "/") + "/" + strings.TrimLeft(ref.EscapedPath(), "/")
	base.Path = joinedPath
	if joinedRawPath != joinedPath {
		base.RawPath = joinedRawPath
	} else {
		base.RawPath = ""
	}
	base.RawQuery = ref.RawQuery
	base.Fragment = ref.Fragment
	if workspace := workspaceID(ctx); workspace != "" {
		query := base.Query()
		if query.Get("workspace_id") == "" {
			query.Set("workspace_id", workspace)
			base.RawQuery = query.Encode()
		}
	}
	if allWorkspaces(ctx) {
		query := base.Query()
		if query.Get("workspace_id") == "" {
			query.Set("all_workspaces", "true")
			base.RawQuery = query.Encode()
		}
	}
	target := base.String()

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
	case http.StatusForbidden:
		kind = ErrorForbidden
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
