package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/specgate/specgate/app/cli/internal/client"
)

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
