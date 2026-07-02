package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/policy"
)

func newGateTasksTestServer(t *testing.T) http.Handler {
	t.Helper()
	h := &Handlers{GateTaskStore: policy.NewInMemGateTaskStore()}
	cfg := testConfig()
	rt := &Router{Handlers: h, Config: cfg}
	return rt.Build()
}

// mustMarshal JSON-encodes v, failing the test on error.
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestListGateTasks_EmptyForUnknownArtifact(t *testing.T) {
	t.Parallel()
	srv := newGateTasksTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/gate-tasks?artifact_id=does-not-exist", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var body struct {
		Tasks []any `json:"tasks"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(body.Tasks))
	}
}

func TestGetGateTask_NotFound(t *testing.T) {
	t.Parallel()
	srv := newGateTasksTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/gate-tasks/nonexistent-id", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body)
	}
}

func TestSubmitGateResult_UnknownTask_404(t *testing.T) {
	t.Parallel()
	srv := newGateTasksTestServer(t)
	body := mustMarshal(t, map[string]any{
		"gate":        "acme/check@1",
		"gate_digest": "sha256:abc",
		"state":       "pass",
		"evaluator":   map[string]string{"executor": "ide_agent"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gate-tasks/unknown-task/result", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body)
	}
}

func TestSubmitGateResult_StaleDigest_422(t *testing.T) {
	t.Parallel()
	store := policy.NewInMemGateTaskStore()
	task, err := store.CreateTask(context.Background(), policy.GateTaskRecord{
		ArtifactID:     "art-1",
		GateKey:        "acme/check@1",
		GateDigest:     "sha256:correct",
		ArtifactDigest: "sha256:art",
		ProfileDigest:  "sha256:prof",
		Executor:       policy.ExecutorIDEAgent,
		ExpiresAt:      time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	h := &Handlers{GateTaskStore: store}
	srv := (&Router{Handlers: h, Config: testConfig()}).Build()

	body := mustMarshal(t, map[string]any{
		"gate":        "acme/check@1",
		"gate_digest": "sha256:STALE",
		"state":       "pass",
		"evaluator":   map[string]string{"executor": "ide_agent"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gate-tasks/"+task.ID+"/result", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body)
	}
}

func TestSubmitGateResult_IDEAgent_AgentAttestedTrust(t *testing.T) {
	t.Parallel()
	store := policy.NewInMemGateTaskStore()
	task, err := store.CreateTask(context.Background(), policy.GateTaskRecord{
		ArtifactID:     "art-1",
		GateKey:        "acme/check@1",
		GateDigest:     "sha256:correct",
		ArtifactDigest: "sha256:art",
		ProfileDigest:  "sha256:prof",
		Executor:       policy.ExecutorIDEAgent,
		ExpiresAt:      time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	h := &Handlers{GateTaskStore: store}
	srv := (&Router{Handlers: h, Config: testConfig()}).Build()

	body := mustMarshal(t, map[string]any{
		"gate":        "acme/check@1",
		"gate_digest": "sha256:correct",
		"state":       "pass",
		"evaluator":   map[string]string{"executor": "ide_agent"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gate-tasks/"+task.ID+"/result", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var resp struct {
		Trust string `json:"trust"`
		State string `json:"state"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Trust != "agent_attested" {
		t.Errorf("trust %q, want agent_attested", resp.Trust)
	}
	if resp.State != "pass" {
		t.Errorf("state %q, want pass", resp.State)
	}
}

// newArtifactSvcForGatePreview builds a real in-memory artifact.Service.
func newArtifactSvcForGatePreview(t *testing.T) artifact.Service {
	t.Helper()
	repo := newMemArtifactRepo()
	store := newMemArtifactStore()
	return artifact.NewService(repo, store, func(featureID, version, path string) string {
		return "artifacts/" + featureID + "/" + version + "/" + path
	}, time.Minute)
}

func TestGatePreview_ArtifactNotFound(t *testing.T) {
	t.Parallel()
	artSvc := newArtifactSvcForGatePreview(t)
	h := &Handlers{
		Artifacts:     artSvc,
		GateTaskStore: policy.NewInMemGateTaskStore(),
	}
	srv := (&Router{Handlers: h, Config: testConfig()}).Build()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/nonexistent/gate-preview", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d: %s", w.Code, w.Body)
	}
}

func TestGatePreview_NoProfileSnapshot(t *testing.T) {
	t.Parallel()
	// An artifact with no GatesProfileSnapshotJSON returns empty preview_tasks.
	artSvc := newArtifactSvcForGatePreview(t)
	art, err := artSvc.Publish(context.Background(), artifact.PublishInput{
		FeatureID:   "feat-no-snapshot",
		Version:     "v0.1",
		RequestType: artifact.RequestTypeNewFeature,
		ImpactLevel: artifact.ImpactLevelLow,
		// GatesProfileSnapshotJSON intentionally empty
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	h := &Handlers{
		Artifacts:     artSvc,
		GateTaskStore: policy.NewInMemGateTaskStore(),
	}
	srv := (&Router{Handlers: h, Config: testConfig()}).Build()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/"+art.ID+"/gate-preview", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d: %s", w.Code, w.Body)
	}
	var resp struct {
		ArtifactID   string `json:"artifact_id"`
		PreviewTasks []any  `json:"preview_tasks"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.PreviewTasks) != 0 {
		t.Errorf("want 0 preview tasks, got %d", len(resp.PreviewTasks))
	}
	if resp.ArtifactID != art.ID {
		t.Errorf("artifact_id = %q, want %q", resp.ArtifactID, art.ID)
	}
}

func TestGatePreview_WithSnapshot_ReturnsExpectedTasks(t *testing.T) {
	t.Parallel()
	// An artifact with a populated GatesProfileSnapshotJSON returns one preview_task per enabled gate.
	artSvc := newArtifactSvcForGatePreview(t)
	snapshotJSON := `{"enabled_gates":["spec_completeness","scope_clear"],"required_roles":["spec"],"required_topics":[],"required_evidence":[],"digest":"sha256:test"}`
	art, err := artSvc.Publish(context.Background(), artifact.PublishInput{
		FeatureID:                "feat-with-snapshot",
		Version:                  "v0.1",
		RequestType:              artifact.RequestTypeNewFeature,
		ImpactLevel:              artifact.ImpactLevelLow,
		GatesProfileSnapshotJSON: snapshotJSON,
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	h := &Handlers{
		Artifacts:     artSvc,
		GateTaskStore: policy.NewInMemGateTaskStore(),
	}
	srv := (&Router{Handlers: h, Config: testConfig()}).Build()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/"+art.ID+"/gate-preview", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body)
	}
	var resp struct {
		ArtifactID   string `json:"artifact_id"`
		PreviewTasks []struct {
			GateKey string `json:"gate_key"`
			Note    string `json:"note"`
		} `json:"preview_tasks"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.PreviewTasks) != 2 {
		t.Errorf("want 2 preview tasks, got %d", len(resp.PreviewTasks))
	}
	for _, task := range resp.PreviewTasks {
		if task.Note != "preview — not persisted" {
			t.Errorf("note = %q, want %q", task.Note, "preview — not persisted")
		}
	}
	// Verify gate keys are present.
	gateKeys := map[string]bool{}
	for _, task := range resp.PreviewTasks {
		gateKeys[task.GateKey] = true
	}
	if !gateKeys["spec_completeness"] || !gateKeys["scope_clear"] {
		t.Errorf("expected gate keys spec_completeness and scope_clear, got %v", gateKeys)
	}
}

func TestDispatchGateTasks_CreatesIDEAgentTasks(t *testing.T) {
	t.Parallel()
	artSvc := newArtifactSvcForGatePreview(t)
	snapshotJSON := `{"enabled_gates":["spec_completeness","scope_clear"],"required_roles":["spec"],"required_topics":[],"required_evidence":[],"digest":"sha256:test"}`
	art, err := artSvc.Publish(context.Background(), artifact.PublishInput{
		FeatureID:                "feat-dispatch",
		Version:                  "v0.1",
		RequestType:              artifact.RequestTypeNewFeature,
		ImpactLevel:              artifact.ImpactLevelLow,
		GatesProfileSnapshotJSON: snapshotJSON,
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	store := policy.NewInMemGateTaskStore()
	h := &Handlers{Artifacts: artSvc, GateTaskStore: store}
	srv := (&Router{Handlers: h, Config: testConfig()}).Build()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/"+art.ID+"/gate-tasks", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body)
	}
	var resp struct {
		ArtifactID      string   `json:"artifact_id"`
		CreatedTaskIDs  []string `json:"created_task_ids"`
		SkippedGateKeys []string `json:"skipped_gate_keys"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.CreatedTaskIDs) != 2 {
		t.Fatalf("want 2 created tasks, got %d", len(resp.CreatedTaskIDs))
	}

	tasks, err := store.ListTasksForArtifact(context.Background(), art.ID)
	if err != nil {
		t.Fatalf("ListTasksForArtifact: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("want 2 persisted tasks, got %d", len(tasks))
	}
	for _, task := range tasks {
		if task.Executor != policy.ExecutorIDEAgent {
			t.Errorf("task %s executor = %q, want ide_agent", task.GateKey, task.Executor)
		}
		if task.GateDigest == "" {
			t.Errorf("task %s missing gate_digest", task.GateKey)
		}
		if task.SkillContent == "" {
			t.Errorf("task %s missing skill_content fallback rubric", task.GateKey)
		}
		if !task.ExpiresAt.After(time.Now()) {
			t.Errorf("task %s expires_at %v not in the future", task.GateKey, task.ExpiresAt)
		}
	}
}

func TestDispatchGateTasks_Idempotent(t *testing.T) {
	t.Parallel()
	artSvc := newArtifactSvcForGatePreview(t)
	snapshotJSON := `{"enabled_gates":["spec_completeness","scope_clear"],"required_roles":["spec"],"required_topics":[],"required_evidence":[],"digest":"sha256:test"}`
	art, err := artSvc.Publish(context.Background(), artifact.PublishInput{
		FeatureID:                "feat-dispatch-idem",
		Version:                  "v0.1",
		RequestType:              artifact.RequestTypeNewFeature,
		ImpactLevel:              artifact.ImpactLevelLow,
		GatesProfileSnapshotJSON: snapshotJSON,
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	store := policy.NewInMemGateTaskStore()
	h := &Handlers{Artifacts: artSvc, GateTaskStore: store}
	srv := (&Router{Handlers: h, Config: testConfig()}).Build()

	do := func() (created, skipped int) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/"+art.ID+"/gate-tasks", nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("want 201, got %d: %s", w.Code, w.Body)
		}
		var resp struct {
			CreatedTaskIDs  []string `json:"created_task_ids"`
			SkippedGateKeys []string `json:"skipped_gate_keys"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return len(resp.CreatedTaskIDs), len(resp.SkippedGateKeys)
	}

	if c, s := do(); c != 2 || s != 0 {
		t.Fatalf("first dispatch: created=%d skipped=%d, want 2/0", c, s)
	}
	if c, s := do(); c != 0 || s != 2 {
		t.Fatalf("second dispatch: created=%d skipped=%d, want 0/2", c, s)
	}
	tasks, _ := store.ListTasksForArtifact(context.Background(), art.ID)
	if len(tasks) != 2 {
		t.Fatalf("re-dispatch must not duplicate: got %d tasks", len(tasks))
	}
}

func TestDispatchGateTasks_ArtifactNotFound(t *testing.T) {
	t.Parallel()
	h := &Handlers{
		Artifacts:     newArtifactSvcForGatePreview(t),
		GateTaskStore: policy.NewInMemGateTaskStore(),
	}
	srv := (&Router{Handlers: h, Config: testConfig()}).Build()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/nonexistent/gate-tasks", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d: %s", w.Code, w.Body)
	}
}

// TestSubmitGateResult_WritesReadinessRun verifies the G1 glue: submitting an
// ide_agent result writes a readiness run so the artifact's aggregation reflects it.
func TestSubmitGateResult_WritesReadinessRun(t *testing.T) {
	t.Parallel()
	artSvc := newArtifactSvcForGatePreview(t)
	snapshotJSON := `{"enabled_gates":["spec_completeness"],"required_roles":["spec"],"required_topics":[],"required_evidence":[],"digest":"sha256:test"}`
	art, err := artSvc.Publish(context.Background(), artifact.PublishInput{
		FeatureID:                "feat-glue",
		Version:                  "v0.1",
		RequestType:              artifact.RequestTypeNewFeature,
		ImpactLevel:              artifact.ImpactLevelLow,
		GatesProfileSnapshotJSON: snapshotJSON,
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	store := policy.NewInMemGateTaskStore()
	h := &Handlers{Artifacts: artSvc, GateTaskStore: store}
	srv := (&Router{Handlers: h, Config: testConfig()}).Build()

	// Dispatch one ide_agent task.
	dw := httptest.NewRecorder()
	srv.ServeHTTP(dw, httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/"+art.ID+"/gate-tasks", nil))
	if dw.Code != http.StatusCreated {
		t.Fatalf("dispatch: %d %s", dw.Code, dw.Body)
	}
	tasks, _ := store.ListTasksForArtifact(context.Background(), art.ID)
	if len(tasks) != 1 {
		t.Fatalf("want 1 task, got %d", len(tasks))
	}
	task := tasks[0]

	// Submit a fail result via the IDE-agent path.
	body := mustMarshal(t, map[string]any{
		"gate":        task.GateKey,
		"gate_digest": task.GateDigest,
		"state":       "fail",
		"summary":     "missing edge cases",
		"evaluator":   map[string]string{"executor": "ide_agent"},
	})
	sw := httptest.NewRecorder()
	subReq := httptest.NewRequest(http.MethodPost, "/api/v1/gate-tasks/"+task.ID+"/result", bytes.NewReader(body))
	subReq.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(sw, subReq)
	if sw.Code != http.StatusOK {
		t.Fatalf("submit: %d %s", sw.Code, sw.Body)
	}

	// A readiness run must now exist for that gate with the submitted state.
	runs, err := artSvc.ListReadinessRuns(context.Background(), art.ID, 50)
	if err != nil {
		t.Fatalf("ListReadinessRuns: %v", err)
	}
	found := false
	for _, r := range runs {
		if r.Gate == task.GateKey && string(r.State) == "fail" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a readiness run for %q with state fail; got %+v", task.GateKey, runs)
	}
}
