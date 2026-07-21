package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/policy"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
)

const gateTaskWorkspaceID = "workspace-gate-task"

func newGateTasksTestServer(t *testing.T) http.Handler {
	t.Helper()
	h := &Handlers{GateTaskStore: newGateTaskTestStore()}
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
	req := httptest.NewRequest(http.MethodGet, "/api/v1/gate-tasks?artifact_id=does-not-exist&workspace_id="+gateTaskWorkspaceID, nil)
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
	req := httptest.NewRequest(http.MethodGet, "/api/v1/gate-tasks/nonexistent-id?workspace_id="+gateTaskWorkspaceID, nil)
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
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gate-tasks/unknown-task/result?workspace_id="+gateTaskWorkspaceID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body)
	}
}

func TestSubmitGateResult_StaleDigest_422(t *testing.T) {
	t.Parallel()
	store := newGateTaskTestStore()
	task, err := store.CreateTask(context.Background(), policy.GateTaskRecord{
		WorkspaceID:    gateTaskWorkspaceID,
		ArtifactID:     "art-1",
		GateKey:        "acme/check@1",
		GateDigest:     "sha256:correct",
		ArtifactDigest: "sha256:art",
		PolicyDigest:   "sha256:prof",
		Executor:       policy.ExecutorIDEAgent,
		ExpiresAt:      time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	h := &Handlers{GateTaskStore: store}
	srv := (&Router{Handlers: h, Config: testConfig()}).Build()

	body := mustMarshal(t, map[string]any{
		"gate":         "acme/check@1",
		"gate_digest":  "sha256:STALE",
		"input_digest": task.ArtifactDigest,
		"state":        "pass",
		"evaluator":    map[string]string{"executor": "ide_agent"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gate-tasks/"+task.ID+"/result?workspace_id="+gateTaskWorkspaceID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body)
	}
}

func TestSubmitGateResult_WrongGate_400(t *testing.T) {
	t.Parallel()
	store := newGateTaskTestStore()
	task, err := store.CreateTask(context.Background(), policy.GateTaskRecord{
		WorkspaceID:    gateTaskWorkspaceID,
		ArtifactID:     "art-1",
		GateKey:        "scope_clear",
		GateVersion:    "v1",
		GateDigest:     "sha256:correct",
		ArtifactDigest: "sha256:art",
		PolicyDigest:   "sha256:policy",
		Executor:       policy.ExecutorIDEAgent,
		ExpiresAt:      time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := (&Router{Handlers: &Handlers{GateTaskStore: store}, Config: testConfig()}).Build()
	body := mustMarshal(t, map[string]any{
		"gate":         "spec_completeness",
		"gate_digest":  task.GateDigest,
		"input_digest": task.ArtifactDigest,
		"state":        "pass",
		"evaluator":    map[string]string{"executor": "ide_agent"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gate-tasks/"+task.ID+"/result?workspace_id="+gateTaskWorkspaceID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body)
	}
}

func TestSubmitGateResult_WrongInputDigest_422(t *testing.T) {
	t.Parallel()
	store := newGateTaskTestStore()
	task, err := store.CreateTask(context.Background(), policy.GateTaskRecord{
		WorkspaceID: gateTaskWorkspaceID, ArtifactID: "art-1", GateKey: "check", GateDigest: "sha256:gate", ArtifactDigest: "sha256:artifact",
		Executor: policy.ExecutorIDEAgent, ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := (&Router{Handlers: &Handlers{GateTaskStore: store}, Config: testConfig()}).Build()
	body := mustMarshal(t, map[string]any{"gate": task.GateKey, "gate_digest": task.GateDigest, "input_digest": "sha256:wrong", "state": "pass", "evaluator": map[string]string{"executor": "ide_agent"}})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gate-tasks/"+task.ID+"/result?workspace_id="+gateTaskWorkspaceID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "stale input digest") {
		t.Fatalf("status/body = %d %s; want 422 stale input digest", w.Code, w.Body.String())
	}
}

func TestSubmitGateResult_ExpiredTask_422(t *testing.T) {
	t.Parallel()
	store := newGateTaskTestStore()
	task, err := store.CreateTask(context.Background(), policy.GateTaskRecord{
		WorkspaceID: gateTaskWorkspaceID, ArtifactID: "art-1", GateKey: "check", GateDigest: "sha256:gate", ArtifactDigest: "sha256:artifact",
		Executor: policy.ExecutorIDEAgent, ExpiresAt: time.Now().UTC().Add(-time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := (&Router{Handlers: &Handlers{GateTaskStore: store}, Config: testConfig()}).Build()
	body := mustMarshal(t, map[string]any{"gate": task.GateKey, "gate_digest": task.GateDigest, "input_digest": task.ArtifactDigest, "state": "pass", "evaluator": map[string]string{"executor": "ide_agent"}})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gate-tasks/"+task.ID+"/result?workspace_id="+gateTaskWorkspaceID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "gate task expired") {
		t.Fatalf("status/body = %d %s; want 422 gate task expired", w.Code, w.Body.String())
	}
}

func TestSubmitGateResult_IDEAgent_AgentAttestedTrust(t *testing.T) {
	t.Parallel()
	store := newGateTaskTestStore()
	task, err := store.CreateTask(context.Background(), policy.GateTaskRecord{
		WorkspaceID:    gateTaskWorkspaceID,
		ArtifactID:     "art-1",
		GateKey:        "acme/check@1",
		GateDigest:     "sha256:correct",
		ArtifactDigest: "sha256:art",
		PolicyDigest:   "sha256:prof",
		Executor:       policy.ExecutorIDEAgent,
		ExpiresAt:      time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	h := &Handlers{GateTaskStore: store}
	srv := (&Router{Handlers: h, Config: testConfig()}).Build()

	body := mustMarshal(t, map[string]any{
		"gate":         "acme/check@1",
		"gate_digest":  "sha256:correct",
		"input_digest": task.ArtifactDigest,
		"state":        "pass",
		"evaluator":    map[string]string{"executor": "ide_agent"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gate-tasks/"+task.ID+"/result?workspace_id="+gateTaskWorkspaceID, bytes.NewReader(body))
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

// newDriftTask creates a claimable spec_repo_drift gate task on store and returns it.
func newDriftTask(t *testing.T, store policy.GateTaskStore, artifactID string) *policy.GateTaskRecord {
	t.Helper()
	task, err := store.CreateTask(context.Background(), policy.GateTaskRecord{
		WorkspaceID:    gateTaskWorkspaceID,
		ArtifactID:     artifactID,
		GateKey:        "spec_repo_drift",
		GateDigest:     "sha256:drift",
		ArtifactDigest: "sha256:art",
		PolicyDigest:   "sha256:prof",
		Executor:       policy.ExecutorIDEAgent,
		ExpiresAt:      time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return task
}

// submitDrift posts a spec_repo_drift result body and returns the recorder.
func submitDrift(t *testing.T, srv http.Handler, taskID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	if _, ok := body["input_digest"]; !ok {
		body["input_digest"] = "sha256:art"
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gate-tasks/"+taskID+"/result?workspace_id="+gateTaskWorkspaceID, bytes.NewReader(mustMarshal(t, body)))
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(w, req)
	return w
}

// TestSubmitGateResult_SpecRepoDrift_AttestationValidation proves AC1: a
// spec_repo_drift result whose evidence lacks examined_docs (or has an empty
// array) or repo_commit is rejected at submission (400). Bound to the
// `attestation-validation-test` delivery check.
func TestSubmitGateResult_SpecRepoDrift_AttestationValidation(t *testing.T) {
	t.Parallel()
	store := newGateTaskTestStore()
	task := newDriftTask(t, store, "art-drift")
	srv := (&Router{Handlers: &Handlers{GateTaskStore: store}, Config: testConfig()}).Build()

	base := map[string]any{
		"gate":        "spec_repo_drift",
		"gate_digest": "sha256:drift",
		"state":       "pass",
		"evaluator":   map[string]string{"executor": "ide_agent"},
	}
	cases := []struct {
		name     string
		evidence map[string]any
	}{
		{"missing examined_docs", map[string]any{"repo_commit": "abc123"}},
		{"empty examined_docs", map[string]any{"examined_docs": []string{}, "repo_commit": "abc123"}},
		{"missing repo_commit", map[string]any{"examined_docs": []string{"docs/spec.md"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{}
			for k, v := range base {
				body[k] = v
			}
			body["evidence"] = tc.evidence
			w := submitDrift(t, srv, task.ID, body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("want 400 for %s, got %d: %s", tc.name, w.Code, w.Body)
			}
		})
	}
}

// TestSubmitGateResult_SpecRepoDrift_VerdictMapping proves AC2: a valid
// attestation with zero findings maps to pass; one or more findings maps to warn
// (never fail/block) — regardless of the client-submitted state.
func TestSubmitGateResult_SpecRepoDrift_VerdictMapping(t *testing.T) {
	t.Parallel()
	decode := func(t *testing.T, w *httptest.ResponseRecorder) (state, trust string) {
		t.Helper()
		if w.Code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", w.Code, w.Body)
		}
		var resp struct{ State, Trust string }
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		return resp.State, resp.Trust
	}

	// Zero findings + valid attestation → pass, even though client claims "fail".
	t.Run("zero findings -> pass", func(t *testing.T) {
		store := newGateTaskTestStore()
		task := newDriftTask(t, store, "art-pass")
		srv := (&Router{Handlers: &Handlers{GateTaskStore: store}, Config: testConfig()}).Build()
		w := submitDrift(t, srv, task.ID, map[string]any{
			"gate":        "spec_repo_drift",
			"gate_digest": "sha256:drift",
			"state":       "fail",
			"evaluator":   map[string]string{"executor": "ide_agent"},
			"evidence":    map[string]any{"examined_docs": []string{"docs/spec.md"}, "repo_commit": "abc123"},
		})
		state, trust := decode(t, w)
		if state != "pass" {
			t.Errorf("state = %q, want pass", state)
		}
		if trust != "agent_attested" {
			t.Errorf("trust = %q, want agent_attested", trust)
		}
	})

	// One or more findings + valid attestation → warn, even though client claims "fail".
	t.Run("findings -> warn", func(t *testing.T) {
		store := newGateTaskTestStore()
		task := newDriftTask(t, store, "art-warn")
		srv := (&Router{Handlers: &Handlers{GateTaskStore: store}, Config: testConfig()}).Build()
		w := submitDrift(t, srv, task.ID, map[string]any{
			"gate":        "spec_repo_drift",
			"gate_digest": "sha256:drift",
			"state":       "fail",
			"evaluator":   map[string]string{"executor": "ide_agent"},
			"evidence":    map[string]any{"examined_docs": []string{"docs/spec.md"}, "repo_commit": "abc123"},
			"findings": []map[string]string{{
				"doc_path": "README.md", "conflicting_claim": "says X", "spec_section": "§4",
			}},
		})
		state, _ := decode(t, w)
		if state != "warn" {
			t.Errorf("state = %q, want warn", state)
		}
	})
}

// TestSubmitGateResult_SpecRepoDrift_FindingsPersist proves AC3: findings entries
// carrying doc_path, conflicting_claim, spec_section ride through FindingsJSON
// onto the agent-attested readiness history row.
func TestSubmitGateResult_SpecRepoDrift_FindingsPersist(t *testing.T) {
	gdb := newTestGormDB(t)
	ctx := context.Background()
	artifactRepo := storagedb.NewRepository(gdb)
	art := &artifact.Artifact{
		ID:          "00000000-0000-4000-8000-000000000002",
		WorkspaceID: gateTaskWorkspaceID,
		FeatureID:   "feat-drift-persist",
		Version:     "v0.1",
		Status:      artifact.StatusApproved,
		RequestType: artifact.RequestTypeNewFeature,
		ImpactLevel: artifact.ImpactLevelMedium,
		CreatedBy:   "tester",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := artifactRepo.Insert(ctx, art); err != nil {
		t.Fatalf("insert artifact: %v", err)
	}
	store := storagedb.NewPGGateTaskStore(gdb)
	task := newDriftTask(t, store, art.ID)
	srv := (&Router{Handlers: &Handlers{GateTaskStore: store}, Config: testConfig()}).Build()

	w := submitDrift(t, srv, task.ID, map[string]any{
		"gate":         task.GateKey,
		"gate_digest":  task.GateDigest,
		"input_digest": task.ArtifactDigest,
		"state":        "fail",
		"evaluator":    map[string]string{"executor": "ide_agent"},
		"evidence":     map[string]any{"examined_docs": []string{"docs/spec.md"}, "repo_commit": "deadbeef"},
		"findings": []map[string]string{{
			"doc_path": "README.md", "conflicting_claim": "README says the gate blocks", "spec_section": "§5",
		}},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("submit: %d %s", w.Code, w.Body)
	}

	runs, err := artifactRepo.ListReadinessRuns(artifact.WithWorkspace(ctx, gateTaskWorkspaceID), art.ID, 50)
	if err != nil {
		t.Fatalf("ListReadinessRuns: %v", err)
	}
	var found *artifact.ReadinessRun
	for i := range runs {
		if runs[i].Gate == "spec_repo_drift" {
			found = &runs[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no spec_repo_drift readiness run; got %+v", runs)
	}
	if string(found.State) != "warn" {
		t.Errorf("state = %q, want warn", found.State)
	}
	for _, want := range []string{
		"examined_docs", "docs/spec.md", "repo_commit", "deadbeef",
		"doc_path", "README.md", "conflicting_claim", "spec_section", "§5",
	} {
		if !strings.Contains(found.EvidenceJSON, want) {
			t.Errorf("attestation field %q missing from readiness evidence: %s", want, found.EvidenceJSON)
		}
	}
}

// newArtifactSvcForGatePreview builds a real in-memory artifact.Service.
func newArtifactSvcForGatePreview(t *testing.T) artifact.Service {
	t.Helper()
	repo := newMemArtifactRepo()
	store := newMemArtifactStore()
	return artifact.NewService(repo, store, func(featureID, version, path string) string {
		return "artifacts/" + featureID + "/" + version + "/" + path
	})
}

func TestGatePreview_ArtifactNotFound(t *testing.T) {
	t.Parallel()
	artSvc := newArtifactSvcForGatePreview(t)
	h := &Handlers{
		Artifacts:     artSvc,
		GateTaskStore: newGateTaskTestStore(),
	}
	srv := (&Router{Handlers: h, Config: testConfig()}).Build()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/nonexistent/gate-preview?workspace_id="+gateTaskWorkspaceID, nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d: %s", w.Code, w.Body)
	}
}

func TestGatePreview_NoProfileSnapshot(t *testing.T) {
	t.Parallel()
	// An artifact with no PolicySnapshotJSON returns empty preview_tasks.
	artSvc := newArtifactSvcForGatePreview(t)
	art, err := artSvc.Publish(context.Background(), artifact.PublishInput{
		WorkspaceID: gateTaskWorkspaceID,
		FeatureID:   "feat-no-snapshot",
		Version:     "v0.1",
		RequestType: artifact.RequestTypeNewFeature,
		ImpactLevel: artifact.ImpactLevelLow,
		// PolicySnapshotJSON intentionally empty
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	h := &Handlers{
		Artifacts:     artSvc,
		GateTaskStore: newGateTaskTestStore(),
	}
	srv := (&Router{Handlers: h, Config: testConfig()}).Build()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/"+art.ID+"/gate-preview?workspace_id="+gateTaskWorkspaceID, nil)
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
	// An artifact with a populated PolicySnapshotJSON returns one preview_task per enabled gate.
	artSvc := newArtifactSvcForGatePreview(t)
	snapshotJSON := `{"snapshot_schema_version":"specgate.policy/v1","approval_policy":"human_required","evidence_policy":"attested_ok","enabled_gates":["spec_completeness","scope_clear"],"required_roles":["spec"],"required_topics":[],"required_evidence":[],"digest":"sha256:test"}`
	art, err := artSvc.Publish(context.Background(), artifact.PublishInput{
		WorkspaceID:        gateTaskWorkspaceID,
		FeatureID:          "feat-with-snapshot",
		Version:            "v0.1",
		RequestType:        artifact.RequestTypeNewFeature,
		ImpactLevel:        artifact.ImpactLevelLow,
		PolicySnapshotJSON: snapshotJSON,
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	h := &Handlers{
		Artifacts:     artSvc,
		GateTaskStore: newGateTaskTestStore(),
	}
	srv := (&Router{Handlers: h, Config: testConfig()}).Build()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/"+art.ID+"/gate-preview?workspace_id="+gateTaskWorkspaceID, nil)
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
		if task.Note != "" {
			t.Errorf("note = %q, want empty preview note", task.Note)
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
	snapshotJSON := `{"snapshot_schema_version":"specgate.policy/v1","approval_policy":"human_required","evidence_policy":"attested_ok","enabled_gates":["spec_completeness","scope_clear"],"required_roles":["spec"],"required_topics":[],"required_evidence":[],"digest":"sha256:test"}`
	art, err := artSvc.Publish(context.Background(), artifact.PublishInput{
		WorkspaceID:        gateTaskWorkspaceID,
		FeatureID:          "feat-dispatch",
		Version:            "v0.1",
		RequestType:        artifact.RequestTypeNewFeature,
		ImpactLevel:        artifact.ImpactLevelLow,
		PolicySnapshotJSON: snapshotJSON,
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	store := newGateTaskTestStore()
	h := &Handlers{Artifacts: artSvc, GateTaskStore: store}
	srv := (&Router{Handlers: h, Config: testConfig()}).Build()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/"+art.ID+"/gate-tasks?workspace_id="+gateTaskWorkspaceID, nil)
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

func TestDispatchGateTasksUsesFrozenSnapshotRubric(t *testing.T) {
	t.Parallel()
	artSvc := newArtifactSvcForGatePreview(t)
	snapshotJSON := `{
		"snapshot_schema_version":"specgate.policy/v1",
		"approval_policy":"human_required",
		"evidence_policy":"attested_ok",
		"enabled_gates":["scope_clear"],
		"gate_skills":{"scope_clear":"prd-review"},
		"gate_definitions":[{
			"key":"scope_clear",
			"version":"v7",
			"skill_name":"prd-review",
			"skill_content":"Frozen publication rubric",
			"skill_digest":"sha256:frozen"
		}],
		"digest":"sha256:policy"
	}`
	art, err := artSvc.Publish(context.Background(), artifact.PublishInput{
		WorkspaceID:        gateTaskWorkspaceID,
		FeatureID:          "feat-frozen-rubric",
		Version:            "v0.1",
		RequestType:        artifact.RequestTypeNewFeature,
		ImpactLevel:        artifact.ImpactLevelLow,
		PolicyDigest:       "sha256:policy",
		PolicySnapshotJSON: snapshotJSON,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := newGateTaskTestStore()
	srv := (&Router{Handlers: &Handlers{Artifacts: artSvc, GateTaskStore: store}, Config: testConfig()}).Build()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/"+art.ID+"/gate-tasks?workspace_id="+gateTaskWorkspaceID, nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body)
	}
	tasks, err := store.ListTasksForArtifact(context.Background(), art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %#v", tasks)
	}
	if tasks[0].GateVersion != "v7" || tasks[0].SkillContent != "Frozen publication rubric" {
		t.Fatalf("task = %#v", tasks[0])
	}
}

// TestDispatchGateTasks_SpecRepoDrift_FreezesRubricAndDigest proves AC2: dispatching
// gates for an approved artifact whose profile enables spec_repo_drift creates a gate
// task that carries the resolved drift rubric (built-in default here) and is
// digest-stamped to the approved artifact version — the spec content the executing
// agent reads. Reuses the existing dispatchGateTasks path; no new dispatch code.
func TestDispatchGateTasks_SpecRepoDrift_FreezesRubricAndDigest(t *testing.T) {
	t.Parallel()
	artSvc := newArtifactSvcForGatePreview(t)
	snapshotJSON := `{"snapshot_schema_version":"specgate.policy/v1","approval_policy":"human_required","evidence_policy":"attested_ok","enabled_gates":["spec_repo_drift"],"required_roles":["spec"],"required_topics":[],"required_evidence":[],"digest":"sha256:test"}`
	art, err := artSvc.Publish(context.Background(), artifact.PublishInput{
		WorkspaceID:        gateTaskWorkspaceID,
		FeatureID:          "feat-drift",
		Version:            "v0.1",
		RequestType:        artifact.RequestTypeNewFeature,
		ImpactLevel:        artifact.ImpactLevelMedium,
		PolicySnapshotJSON: snapshotJSON,
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, err := artSvc.UpdateStatus(context.Background(), art.ID, artifact.StatusUpdate{Status: artifact.StatusApproved, Actor: "reviewer"}); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	store := newGateTaskTestStore()
	h := &Handlers{Artifacts: artSvc, GateTaskStore: store}
	srv := (&Router{Handlers: h, Config: testConfig()}).Build()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/"+art.ID+"/gate-tasks?workspace_id="+gateTaskWorkspaceID, nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body)
	}

	tasks, err := store.ListTasksForArtifact(context.Background(), art.ID)
	if err != nil {
		t.Fatalf("ListTasksForArtifact: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("want 1 drift task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.GateKey != "spec_repo_drift" {
		t.Fatalf("gate_key = %q, want spec_repo_drift", task.GateKey)
	}
	// Frozen resolved rubric: the built-in default drift rubric.
	if task.SkillContent != defaultGateRubric("spec_repo_drift") {
		t.Errorf("SkillContent did not freeze the resolved drift rubric:\n%s", task.SkillContent)
	}
	// Digest-stamped binding to the approved artifact version (the spec content).
	if task.GateDigest == "" {
		t.Error("missing gate_digest stamp")
	}
	if task.ArtifactDigest != art.SnapshotDigest {
		t.Errorf("artifact_digest = %q, want immutable snapshot_digest %q", task.ArtifactDigest, art.SnapshotDigest)
	}
	if task.Executor != policy.ExecutorIDEAgent {
		t.Errorf("executor = %q, want ide_agent", task.Executor)
	}
}

func TestDispatchGateTasks_Idempotent(t *testing.T) {
	t.Parallel()
	artSvc := newArtifactSvcForGatePreview(t)
	snapshotJSON := `{"snapshot_schema_version":"specgate.policy/v1","approval_policy":"human_required","evidence_policy":"attested_ok","enabled_gates":["spec_completeness","scope_clear"],"required_roles":["spec"],"required_topics":[],"required_evidence":[],"digest":"sha256:test"}`
	art, err := artSvc.Publish(context.Background(), artifact.PublishInput{
		WorkspaceID:        gateTaskWorkspaceID,
		FeatureID:          "feat-dispatch-idem",
		Version:            "v0.1",
		RequestType:        artifact.RequestTypeNewFeature,
		ImpactLevel:        artifact.ImpactLevelLow,
		PolicySnapshotJSON: snapshotJSON,
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	store := newGateTaskTestStore()
	h := &Handlers{Artifacts: artSvc, GateTaskStore: store}
	srv := (&Router{Handlers: h, Config: testConfig()}).Build()

	do := func() (created, skipped int) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/"+art.ID+"/gate-tasks?workspace_id="+gateTaskWorkspaceID, nil)
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
		GateTaskStore: newGateTaskTestStore(),
	}
	srv := (&Router{Handlers: h, Config: testConfig()}).Build()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/nonexistent/gate-tasks?workspace_id="+gateTaskWorkspaceID, nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d: %s", w.Code, w.Body)
	}
}

func TestSubmitGateResult_PersistsExactlyOneIDEAgentRun(t *testing.T) {
	gdb := newTestGormDB(t)
	ctx := context.Background()
	artifactRepo := storagedb.NewRepository(gdb)
	art := &artifact.Artifact{
		ID:          "00000000-0000-4000-8000-000000000001",
		WorkspaceID: gateTaskWorkspaceID,
		FeatureID:   "feat-glue",
		Version:     "v0.1",
		Status:      artifact.StatusDraft,
		RequestType: artifact.RequestTypeNewFeature,
		ImpactLevel: artifact.ImpactLevelLow,
		CreatedBy:   "tester",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := artifactRepo.Insert(ctx, art); err != nil {
		t.Fatalf("insert artifact: %v", err)
	}
	artSvc := artifact.NewService(artifactRepo, newMemArtifactStore(), nil)
	store := storagedb.NewPGGateTaskStore(gdb)
	task, err := store.CreateTask(ctx, policy.GateTaskRecord{
		WorkspaceID:    gateTaskWorkspaceID,
		ArtifactID:     art.ID,
		GateKey:        "spec_completeness",
		GateVersion:    "v1",
		GateDigest:     "sha256:gate",
		ArtifactDigest: "sha256:artifact",
		PolicyDigest:   "sha256:policy",
		Executor:       policy.ExecutorIDEAgent,
		ExpiresAt:      time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	h := &Handlers{Artifacts: artSvc, GateTaskStore: store}
	srv := (&Router{Handlers: h, Config: testConfig()}).Build()

	body := mustMarshal(t, map[string]any{
		"gate":         task.GateKey,
		"gate_digest":  task.GateDigest,
		"input_digest": task.ArtifactDigest,
		"state":        "not_run",
		"summary":      "evaluator unavailable",
		"evaluator":    map[string]string{"executor": "ide_agent"},
	})
	sw := httptest.NewRecorder()
	subReq := httptest.NewRequest(http.MethodPost, "/api/v1/gate-tasks/"+task.ID+"/result?workspace_id="+gateTaskWorkspaceID, bytes.NewReader(body))
	subReq.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(sw, subReq)
	if sw.Code != http.StatusOK {
		t.Fatalf("submit: %d %s", sw.Code, sw.Body)
	}

	runs, err := artSvc.ListReadinessRuns(artifact.WithWorkspace(ctx, gateTaskWorkspaceID), art.ID, 50)
	if err != nil {
		t.Fatalf("ListReadinessRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("readiness runs = %d, want exactly one ide_agent result: %+v", len(runs), runs)
	}
	if runs[0].ID != task.ID || runs[0].Executor != "ide_agent" || runs[0].Gate != task.GateKey || runs[0].State != artifact.ReadinessStateNotRun {
		t.Fatalf("run = %+v, want task %q as ide_agent not_run", runs[0], task.ID)
	}
	if runs[0].Hint != "evaluator unavailable" {
		t.Fatalf("hint = %q, want submitted summary", runs[0].Hint)
	}
}
