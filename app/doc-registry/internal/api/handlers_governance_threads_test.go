package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	storagedb "github.com/specgate/doc-registry/internal/storage/db"
)

func newGovernanceThreadsTestServer(t *testing.T) http.Handler {
	t.Helper()
	gdb := newTestGormDB(t)

	rt := &Router{
		Handlers: &Handlers{
			GovernanceThreads: storagedb.NewGovernanceThreadsRepository(gdb),
		},
		Config: testConfig(),
	}
	return rt.Build()
}

func putJSON(t *testing.T, srv http.Handler, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("PUT", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	return rr
}

func TestGovernanceThreads_ListUpsertAndDelete(t *testing.T) {
	t.Parallel()
	srv := newGovernanceThreadsTestServer(t)

	for _, row := range []struct {
		id      string
		title   string
		updated string
	}{
		{"thread-1", "First thread", "2026-05-18T10:00:00Z"},
		{"thread-2", "Second thread", "2026-05-18T10:01:00Z"},
	} {
		rr := putJSON(t, srv, "/governance/threads/"+row.id,
			`{"title":"`+row.title+`","preview":"preview","updated_at":"`+row.updated+`"}`)
		if rr.Code != 200 {
			t.Fatalf("upsert %s status=%d body=%s", row.id, rr.Code, rr.Body.String())
		}
	}

	list := getReq(t, srv, "/governance/threads?limit=1")
	if list.Code != 200 {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	body := list.Body.String()
	if !strings.Contains(body, `"total":2`) || !strings.Contains(body, `"thread_id":"thread-2"`) || strings.Contains(body, `"thread_id":"thread-1"`) {
		t.Fatalf("unexpected first page: %s", body)
	}

	del := deleteReq(t, srv, "/governance/threads/thread-2")
	if del.Code != 204 {
		t.Fatalf("delete status=%d body=%s", del.Code, del.Body.String())
	}

	list = getReq(t, srv, "/governance/threads?limit=10")
	body = list.Body.String()
	if !strings.Contains(body, `"total":1`) || strings.Contains(body, `"thread_id":"thread-2"`) {
		t.Fatalf("archived thread still listed: %s", body)
	}
}
