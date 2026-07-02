package agentsclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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
				{"gate": "spec_completeness", "state": "pass", "hint": "ok", "created_at": "2026-01-01T00:00:00Z"},
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
