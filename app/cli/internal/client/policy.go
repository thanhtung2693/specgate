package client

import (
	"context"
	"fmt"
	"net/url"
)

// GateTask is the response shape for a single gate task.
type GateTask struct {
	TaskID         string `json:"task_id"`
	GateKey        string `json:"gate_key"`
	GateVersion    string `json:"gate_version"`
	GateDigest     string `json:"gate_digest"`
	ArtifactID     string `json:"artifact_id"`
	ArtifactDigest string `json:"artifact_digest"`
	ProfileDigest  string `json:"profile_digest"`
	Executor       string `json:"executor"`
	SkillContent   string `json:"skill_content,omitempty"`
	ExpiresAt      string `json:"expires_at"`
}

// GateResultResponse is the response from POST /api/v1/gate-tasks/{task_id}/result.
type GateResultResponse struct {
	ResultID string `json:"result_id"`
	Trust    string `json:"trust"`
	State    string `json:"state"`
}

// ListGateTasks calls GET /api/v1/gate-tasks?artifact_id=<artifactID>.
func (c *Client) ListGateTasks(ctx context.Context, artifactID string) ([]GateTask, error) {
	var out struct {
		Tasks []GateTask `json:"tasks"`
	}
	if err := c.get(ctx, "/api/v1/gate-tasks?artifact_id="+artifactID, &out); err != nil {
		return nil, err
	}
	return out.Tasks, nil
}

// GetGateTask calls GET /api/v1/gate-tasks/{task_id}.
func (c *Client) GetGateTask(ctx context.Context, taskID string) (*GateTask, error) {
	var task GateTask
	if err := c.get(ctx, "/api/v1/gate-tasks/"+taskID, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// SubmitGateResult calls POST /api/v1/gate-tasks/{task_id}/result.
func (c *Client) SubmitGateResult(ctx context.Context, taskID string, body any) (*GateResultResponse, error) {
	var result GateResultResponse
	if err := c.post(ctx, fmt.Sprintf("/api/v1/gate-tasks/%s/result", taskID), body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GatePreview calls GET /api/v1/artifacts/{artifactID}/gate-preview.
func (c *Client) GatePreview(ctx context.Context, artifactID string) (map[string]any, error) {
	var result map[string]any
	path := fmt.Sprintf("/api/v1/artifacts/%s/gate-preview", url.PathEscape(artifactID))
	if err := c.get(ctx, path, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// DispatchGateTasksResult is the response from dispatching ide_agent gate tasks.
type DispatchGateTasksResult struct {
	ArtifactID      string   `json:"artifact_id"`
	CreatedTaskIDs  []string `json:"created_task_ids"`
	SkippedGateKeys []string `json:"skipped_gate_keys"`
}

// DispatchGateTasks calls POST /api/v1/artifacts/{artifact_id}/gate-tasks to
// create ide_agent gate tasks for the artifact's enabled gates.
func (c *Client) DispatchGateTasks(ctx context.Context, artifactID string) (*DispatchGateTasksResult, error) {
	var result DispatchGateTasksResult
	path := fmt.Sprintf("/api/v1/artifacts/%s/gate-tasks", url.PathEscape(artifactID))
	if err := c.post(ctx, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
