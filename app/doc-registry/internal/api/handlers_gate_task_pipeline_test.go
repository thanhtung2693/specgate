package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/policy"
)

const gateTaskPipelineArtifactID = "gate-task-test-artifact"

// TestGateTaskPipeline verifies the full gate task lifecycle in-memory:
// list tasks (empty) → inject task → submit result → verify trust.
func TestGateTaskPipeline(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := policy.NewInMemGateTaskStore()
	h := &Handlers{
		GateTaskStore: store,
	}
	srv := (&Router{Handlers: h, Config: testConfig()}).Build()

	// -- Step 3: List gate tasks for artifact (empty before any tasks created) --
	req := httptest.NewRequest(http.MethodGet, "/api/v1/gate-tasks?artifact_id="+gateTaskPipelineArtifactID, nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Step 3 list tasks: status %d: %s", w.Code, w.Body)
	}
	var listResult struct {
		Tasks []any `json:"tasks"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listResult); err != nil {
		t.Fatalf("Step 3 list tasks: decode: %v", err)
	}
	if len(listResult.Tasks) != 0 {
		t.Fatalf("Step 3 list tasks: expected 0 tasks, got %d", len(listResult.Tasks))
	}

	// -- Step 4: Inject a gate task directly into the store --
	task, err := store.CreateTask(ctx, policy.GateTaskRecord{
		ArtifactID:     gateTaskPipelineArtifactID,
		GateKey:        "test/semantic-check@1",
		GateVersion:    "1",
		GateDigest:     "sha256:test-gate-digest",
		ArtifactDigest: "sha256:test-artifact-digest",
		ProfileDigest:  "sha256:test-profile-digest",
		Executor:       policy.ExecutorIDEAgent,
		ExpiresAt:      time.Now().Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("Step 4 inject task: %v", err)
	}

	// -- Step 5: GET the injected task by ID --
	req = httptest.NewRequest(http.MethodGet, "/api/v1/gate-tasks/"+task.ID, nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Step 5 get task: status %d: %s", w.Code, w.Body)
	}
	var taskResult struct {
		TaskID  string `json:"task_id"`
		GateKey string `json:"gate_key"`
	}
	if err := json.NewDecoder(w.Body).Decode(&taskResult); err != nil {
		t.Fatalf("Step 5 get task: decode: %v", err)
	}
	if taskResult.TaskID != task.ID {
		t.Fatalf("Step 5 get task: task_id = %q, want %q", taskResult.TaskID, task.ID)
	}

	// -- Step 6: Submit a GateResult (correct digest → 200, agent_attested) --
	resultPayload := map[string]any{
		"gate":        "test/semantic-check@1",
		"gate_digest": "sha256:test-gate-digest",
		"state":       "pass",
		"summary":     "All checks passed",
		"evaluator":   map[string]string{"executor": "ide_agent"},
		"findings":    []any{},
	}
	submitBody := mustMarshal(t, resultPayload)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/gate-tasks/"+task.ID+"/result", bytes.NewReader(submitBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Step 6 submit result: status %d: %s", w.Code, w.Body)
	}
	var submitResult struct {
		ResultID string `json:"result_id"`
		Trust    string `json:"trust"`
		State    string `json:"state"`
	}
	if err := json.NewDecoder(w.Body).Decode(&submitResult); err != nil {
		t.Fatalf("Step 6 submit result: decode: %v", err)
	}
	if submitResult.Trust != "agent_attested" {
		t.Errorf("Step 6: trust = %q, want agent_attested", submitResult.Trust)
	}
	if submitResult.State != "pass" {
		t.Errorf("Step 6: state = %q, want pass", submitResult.State)
	}
	if submitResult.ResultID == "" {
		t.Error("Step 6: result_id is empty")
	}

	// -- Step 7: Submit with stale digest → 422 --
	stalePayload := map[string]any{
		"gate":        "test/semantic-check@1",
		"gate_digest": "sha256:STALE-digest",
		"state":       "pass",
		"summary":     "wrong digest",
		"evaluator":   map[string]string{"executor": "ide_agent"},
		"findings":    []any{},
	}
	// Need a fresh task (the first was already submitted)
	task2, _ := store.CreateTask(ctx, policy.GateTaskRecord{
		ArtifactID:     gateTaskPipelineArtifactID,
		GateKey:        "test/semantic-check@1",
		GateVersion:    "1",
		GateDigest:     "sha256:test-gate-digest",
		ArtifactDigest: "sha256:test-artifact-digest",
		ProfileDigest:  "sha256:test-profile-digest",
		Executor:       policy.ExecutorIDEAgent,
		ExpiresAt:      time.Now().Add(24 * time.Hour),
	})
	staleBody := mustMarshal(t, stalePayload)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/gate-tasks/"+task2.ID+"/result", bytes.NewReader(staleBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("Step 7 stale digest: status %d (want 422): %s", w.Code, w.Body)
	}
}
