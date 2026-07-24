package agentsclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/specgate/doc-registry/internal/workspace"
)

func TestNewEmptyBaseURLReturnsNil(t *testing.T) {
	t.Parallel()
	if got := New("  "); got != nil {
		t.Fatalf("New(empty) = %#v, want nil", got)
	}
}

func TestNew_EmptyBaseURLDisabled(t *testing.T) {
	t.Parallel()
	if c := New(""); c != nil {
		t.Fatalf("New(\"\") = %v, want nil", c)
	}
	if c := New("   "); c != nil {
		t.Fatalf("New(whitespace) = %v, want nil", c)
	}
}

func TestChatHealthGetsConfigurationWithoutExposingSecrets(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/governance/chat/health" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"configured":true,"provider":"openai","model":"gpt-5.4-mini"}`))
	}))
	defer srv.Close()

	health, err := New(srv.URL).ChatHealth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !health.Configured || health.Provider != "openai" || health.Model != "gpt-5.4-mini" {
		t.Fatalf("health = %+v", health)
	}
}

func TestRunReadiness_PostsToEndpointAndDecodes(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"artifact_id": "art-1",
			"evaluations_posted": 2,
			"readiness_runs": [
				{"gate": "spec_completeness", "state": "pass", "hint": "ok", "executor": "platform", "created_at": "2026-01-01T00:00:00Z"},
				{"gate": "ac_verifiable", "state": "warn", "created_at": "2026-01-02T00:00:00Z"}
			]
		}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	if c == nil {
		t.Fatal("New returned nil for a non-empty base URL")
	}

	v, err := c.RunReadiness(context.Background(), "art-1")
	if err != nil {
		t.Fatalf("RunReadiness error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/artifacts/art-1/run-readiness" {
		t.Errorf("path = %q, want /artifacts/art-1/run-readiness", gotPath)
	}
	if v.ArtifactID != "art-1" {
		t.Errorf("artifact_id = %q, want art-1", v.ArtifactID)
	}
	if v.EvaluationsPosted != 2 {
		t.Errorf("evaluations_posted = %d, want 2", v.EvaluationsPosted)
	}
	if len(v.ReadinessRuns) != 2 {
		t.Fatalf("readiness_runs len = %d, want 2", len(v.ReadinessRuns))
	}
	if v.ReadinessRuns[0].Gate != "spec_completeness" || v.ReadinessRuns[0].State != "pass" {
		t.Errorf("run[0] = %+v", v.ReadinessRuns[0])
	}
	if v.ReadinessRuns[0].Executor != "platform" {
		t.Errorf("run[0].executor = %q, want platform", v.ReadinessRuns[0].Executor)
	}
}

func TestRunReadiness_DecodesDispatchReceipt(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"artifact_id":"art-1","readiness_runs":[],"dispatched_to_ide_agent":{"created_task_ids":["new"],"skipped_gate_keys":["scope_clear"],"pending_task_ids":["old","new"]}}`))
	}))
	defer srv.Close()
	v, err := New(srv.URL).RunReadiness(context.Background(), "art-1")
	if err != nil {
		t.Fatal(err)
	}
	if v.DispatchedToIDEAgent == nil || len(v.DispatchedToIDEAgent.PendingTaskIDs) != 2 || v.DispatchedToIDEAgent.PendingTaskIDs[0] != "old" {
		t.Fatalf("dispatched_to_ide_agent = %+v; want complete receipt", v.DispatchedToIDEAgent)
	}
}

func TestRunReadiness_SendsTrustedWorkspaceQuery(t *testing.T) {
	t.Parallel()
	var gotWorkspace string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotWorkspace = r.URL.Query().Get("workspace_id")
		_, _ = w.Write([]byte(`{"artifact_id":"art-1","readiness_runs":[]}`))
	}))
	defer srv.Close()

	client := New(srv.URL)
	if _, err := client.RunReadiness(workspace.WithID(context.Background(), "ws-a"), "art-1"); err != nil {
		t.Fatalf("RunReadiness: %v", err)
	}
	if gotWorkspace != "ws-a" {
		t.Fatalf("workspace_id query = %q, want ws-a", gotWorkspace)
	}
}

func TestRunReadiness_TrimsTrailingSlash(t *testing.T) {
	t.Parallel()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"artifact_id":"a","readiness_runs":[]}`))
	}))
	defer srv.Close()

	c := New(srv.URL + "/")
	if _, err := c.RunReadiness(context.Background(), "a"); err != nil {
		t.Fatalf("RunReadiness error: %v", err)
	}
	if gotPath != "/artifacts/a/run-readiness" {
		t.Errorf("path = %q, want /artifacts/a/run-readiness", gotPath)
	}
}

func TestRunReadiness_Non200Errors(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"detail":"boom"}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.RunReadiness(context.Background(), "art-1")
	if err == nil {
		t.Fatal("expected error on non-200")
	}
	var responseErr *ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("error = %T %v, want ResponseError", err, err)
	}
	if responseErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", responseErr.StatusCode)
	}
	if responseErr.Detail() != "boom" {
		t.Fatalf("detail = %q, want boom", responseErr.Detail())
	}
}

func TestRunReadiness_TransportErrorPropagated(t *testing.T) {
	t.Parallel()

	// Point at a closed server to force a transport error.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := New(url)
	if _, err := c.RunReadiness(context.Background(), "art-1"); err == nil {
		t.Fatal("expected transport error against a closed server")
	}
}

func TestRunReadiness_NilClient(t *testing.T) {
	t.Parallel()
	var c *Client
	if _, err := c.RunReadiness(context.Background(), "art-1"); err == nil {
		t.Fatal("expected error from nil client")
	}
}

func TestCreateQuickWorkItem_EncodesOnlyBoundAcceptanceCriteriaAsObjects(t *testing.T) {
	t.Parallel()

	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workboard/quick-work-item" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"change_request_id":"cr-1"}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.CreateQuickWorkItem(
		context.Background(),
		"Fix login",
		"Users cannot log in",
		"",
		"",
		"",
		"",
		[]AcceptanceCriterionInput{
			{Text: "Login succeeds"},
			{Text: "Regression suite passes", VerificationBinding: "tests"},
		},
		"",
		"",
	)
	if err != nil {
		t.Fatalf("CreateQuickWorkItem: %v", err)
	}

	acs, ok := got["acceptance_criteria"].([]any)
	if !ok || len(acs) != 2 {
		t.Fatalf("acceptance_criteria = %#v", got["acceptance_criteria"])
	}
	if got, want := acs[0], "Login succeeds"; got != want {
		t.Fatalf("unbound criterion = %#v, want %q", got, want)
	}
	bound, ok := acs[1].(map[string]any)
	if !ok {
		t.Fatalf("bound criterion = %#v, want object", acs[1])
	}
	if bound["text"] != "Regression suite passes" || bound["verification_binding"] != "tests" {
		t.Fatalf("bound criterion = %#v", bound)
	}
}
