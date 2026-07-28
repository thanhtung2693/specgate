package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/specgate/specgate/app/cli/internal/client"
)

func TestResolveGovernancePolicyUsesWorkspaceAndDecodesProjection(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/policies/resolve" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("workspace_id"); got != "ws-1" {
			t.Fatalf("workspace_id = %q", got)
		}
		var body client.ResolveGovernancePolicyInput
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.RequestType != "bugfix" || body.ImpactLevel != "unknown" {
			t.Fatalf("body = %#v", body)
		}
		_, _ = w.Write([]byte(`{"governance_level":"standard","reason_codes":["default_standard"],"required_roles":["plan","spec"],"required_topics":["verification"],"required_evidence":["tests"],"enabled_gates":["spec_completeness"],"approval_policy":"human_required","evidence_policy":"attested_ok","policy_digest":"sha256:test","rubrics":[{"gate":"spec_completeness","source":"workspace_skill"}],"explanation":{"governance_level":"standard","title":"Standard","summary":"Review required"}}`))
	}))
	defer srv.Close()

	ctx := client.WithWorkspace(context.Background(), "ws-1")
	result, err := client.New(srv.URL, time.Second).ResolveGovernancePolicy(ctx, client.ResolveGovernancePolicyInput{
		RequestType: "bugfix",
		ImpactLevel: "unknown",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.PolicyDigest != "sha256:test" || len(result.Rubrics) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestDispatchGateTasksDecodesPendingTaskIDs(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/artifacts/art-1/gate-tasks" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"artifact_id":"art-1","created_task_ids":["task-1"],"skipped_gate_keys":["scope_clear"],"pending_task_ids":["task-1","task-2"]}`))
	}))
	defer srv.Close()

	result, err := client.New(srv.URL, time.Second).DispatchGateTasks(context.Background(), "art-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.PendingTaskIDs) != 2 || result.PendingTaskIDs[1] != "task-2" {
		t.Fatalf("pending task ids = %#v", result.PendingTaskIDs)
	}
}
