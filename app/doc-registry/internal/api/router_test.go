package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/specgate/doc-registry/internal/config"
)

func testConfig() *config.Config {
	return &config.Config{
		HTTPAddr: ":8080",
		OpenAPI: config.OpenAPIConfig{
			Enabled: true,
		},
	}
}

func TestRouter_Healthz(t *testing.T) {
	t.Parallel()
	rt := &Router{
		Handlers: &Handlers{},
		Config:   testConfig(),
	}
	srv := rt.Build()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestRouter_Readyz(t *testing.T) {
	t.Parallel()
	rt := &Router{
		Handlers: &Handlers{},
		Config:   testConfig(),
	}
	srv := rt.Build()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestDevCORS_PreflightGovernancePresign(t *testing.T) {
	t.Parallel()
	rt := &Router{
		Handlers: &Handlers{},
		Config:   testConfig(),
	}
	h := DevCORS(rt.Build())
	req := httptest.NewRequest(http.MethodOptions, "/governance/files/presign", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "content-type" {
		t.Fatalf("Access-Control-Allow-Headers = %q (want echo of request)", got)
	}
}

func TestDevCORS_PreflightIPv6Loopback(t *testing.T) {
	t.Parallel()
	rt := &Router{
		Handlers: &Handlers{},
		Config:   testConfig(),
	}
	h := DevCORS(rt.Build())
	req := httptest.NewRequest(http.MethodOptions, "/governance/files/presign", nil)
	req.Header.Set("Origin", "http://[::1]:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://[::1]:3000" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
}

func TestRouter_PresignFile_NoS3(t *testing.T) {
	t.Parallel()
	rt := &Router{
		Handlers: &Handlers{},
		Config:   testConfig(),
	}
	srv := rt.Build()
	req := httptest.NewRequest(
		http.MethodPost,
		"/governance/files/presign",
		strings.NewReader(`{"name":"hello.png","content_type":"image/png","size_bytes":1024}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRouter_GovernanceRoutesOnly(t *testing.T) {
	t.Parallel()
	rt := &Router{
		Handlers: &Handlers{},
		Config:   testConfig(),
	}
	srv := rt.Build()
	req := httptest.NewRequest(
		http.MethodPost,
		"/governance/files/presign",
		strings.NewReader(`{"name":"hello.png","content_type":"image/png","size_bytes":1024}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 from configured route, got %d body=%s", rec.Code, rec.Body.String())
	}

	for _, path := range []string{
		"/workboard/change-requests",
		"/governance/threads",
		"/governance/feedback-events",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
			t.Fatalf("%s: expected registered route, got %d body=%s", path, rec.Code, rec.Body.String())
		}
	}

	for _, path := range []string{
		"/old-workboard/change-requests",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s: expected old governance route to be unavailable, got %d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestRouter_SpecSectionSixOneCoreEndpointsRegistered(t *testing.T) {
	t.Parallel()
	rt := &Router{
		Handlers: &Handlers{},
		Config:   testConfig(),
	}
	srv := rt.Build()

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected OpenAPI document, got %d body=%s", rec.Code, rec.Body.String())
	}

	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode OpenAPI: %v", err)
	}
	openAPIJSON := rec.Body.String()
	if strings.Contains(openAPIJSON, "depends_on") {
		t.Fatal("legacy artifact depends_on field still exposed in OpenAPI")
	}

	for _, endpoint := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/artifacts"},
		{http.MethodGet, "/artifacts"},
		{http.MethodGet, "/artifacts/{id}"},
		{http.MethodDelete, "/artifacts/{id}"},
		{http.MethodPatch, "/artifacts/{id}/status"},
		{http.MethodGet, "/artifacts/{id}/files"},
		{http.MethodGet, "/artifacts/{id}/files/{key}"},
		{http.MethodPost, "/artifacts/{id}/readiness-runs/refresh"},
		{http.MethodGet, "/artifacts/{id}/readiness-runs"},
		{http.MethodPost, "/features/{id}/attachments"},
		{http.MethodGet, "/features/{id}/attachments"},
		{http.MethodDelete, "/attachments/{id}"},
		{http.MethodPost, "/governance/files/presign"},
		{http.MethodPost, "/governance/files/{id}/commit"},
		{http.MethodGet, "/governance/files"},
		{http.MethodPost, "/governance/files/{id}/touch"},
		{http.MethodDelete, "/governance/files/{id}"},
		{http.MethodGet, "/governance/threads"},
		{http.MethodPut, "/governance/threads/{thread_id}"},
		{http.MethodDelete, "/governance/threads/{thread_id}"},
		{http.MethodGet, "/conflicts"},
		{http.MethodGet, "/events"},
		{http.MethodGet, "/repos/file"},
		{http.MethodGet, "/mcp/info"},
		{http.MethodGet, "/mcp/api-key"},
		{http.MethodPost, "/mcp/api-key/rotate"},
		{http.MethodGet, "/settings"},
		{http.MethodPut, "/settings"},
		{http.MethodGet, "/integrations"},
		{http.MethodPost, "/integrations"},
		{http.MethodGet, "/integrations/{id}"},
		{http.MethodPut, "/integrations/{id}"},
		{http.MethodDelete, "/integrations/{id}"},
		{http.MethodGet, "/integrations/{id}/resources"},
		{http.MethodPost, "/integrations/{id}/resources"},
		{http.MethodDelete, "/integrations/{id}/resources/{resource_id}"},
		{http.MethodGet, "/integrations/{id}/repos"},
		{http.MethodGet, "/integrations/{id}/linear/teams"},
		{http.MethodGet, "/integrations/{id}/linear/projects"},
		{http.MethodGet, "/integrations/{id}/webhook-events"},
		{http.MethodPost, "/integrations/{id}/webhook-events"},
		{http.MethodPost, "/integrations/{id}/resources/{resource_id}/gitlab/webhook"},
		{http.MethodPost, "/integrations/{id}/resources/{resource_id}/github/webhook"},
		{http.MethodPost, "/integrations/{id}/resources/{resource_id}/linear/webhook"},
		{http.MethodPost, "/integrations/{id}/gitlab/webhook"},
		{http.MethodPost, "/integrations/{id}/github/webhook"},
		{http.MethodPost, "/integrations/{id}/linear/webhook"},
		{http.MethodPut, "/integrations/{id}/api-token"},
		{http.MethodGet, "/integrations/{id}/webhook-secret"},
		{http.MethodPut, "/integrations/{id}/webhook-secret"},
		{http.MethodPost, "/integrations/{id}/webhook-secret/rotate"},
		{http.MethodPost, "/integrations/oauth/begin"},
		{http.MethodPost, "/integrations/{id}/oauth/authorize"},
		{http.MethodPost, "/integrations/{id}/oauth/disconnect"},
		{http.MethodGet, "/governance/feedback-events"},
		{http.MethodPost, "/governance/feedback-events/{id}/status"},
		{http.MethodGet, "/governance-profiles"},
		{http.MethodGet, "/skills"},
		{http.MethodPost, "/skills"},
		{http.MethodPut, "/skills/{id}"},
		{http.MethodDelete, "/skills/{id}"},
		{http.MethodPost, "/artifact-edit/sessions"},
		{http.MethodGet, "/artifact-edit/proposals"},
		{http.MethodGet, "/artifact-edit/sessions/{id}"},
		{http.MethodDelete, "/artifact-edit/sessions/{id}"},
		{http.MethodGet, "/artifact-edit/sessions/{id}/files"},
		{http.MethodGet, "/artifact-edit/sessions/{id}/files/{key}"},
		{http.MethodPost, "/artifact-edit/sessions/{id}/patch"},
		{http.MethodPut, "/artifact-edit/sessions/{id}/files/{key}"},
		{http.MethodGet, "/artifact-edit/sessions/{id}/diff"},
		{http.MethodPost, "/artifact-edit/sessions/{id}/save"},
		{http.MethodGet, "/artifact-revisions/{revision_id}"},
		{http.MethodGet, "/artifact-revisions/{revision_id}/diff"},
		{http.MethodGet, "/artifacts/{id}/revisions"},
		{http.MethodGet, "/workboard/features"},
		{http.MethodPost, "/workboard/features"},
		{http.MethodPost, "/workboard/features/upsert-by-key"},
		{http.MethodGet, "/workboard/features/{id}"},
		{http.MethodPatch, "/workboard/features/{id}"},
		{http.MethodPut, "/workboard/features/{id}/summary"},
		{http.MethodGet, "/workboard/change-requests"},
		{http.MethodPost, "/workboard/change-requests"},
		{http.MethodGet, "/workboard/change-requests/{id}"},
		{http.MethodPatch, "/workboard/change-requests/{id}"},
		{http.MethodPost, "/workboard/change-requests/{id}/unarchive"},
		{http.MethodGet, "/workboard/change-requests/{id}/acceptance-criteria"},
		{http.MethodGet, "/workboard/change-requests/{id}/next-actions"},
		{http.MethodPost, "/workboard/change-requests/{id}/gate-runs/refresh"},
		{http.MethodGet, "/workboard/change-requests/{id}/gate-runs"},
		{http.MethodPost, "/workboard/change-requests/{id}/lead-artifact"},
		{http.MethodPost, "/workboard/change-requests/{id}/context-pack-artifact"},
		{http.MethodGet, "/workboard/change-requests/{id}/tracker-links"},
		{http.MethodGet, "/workboard/stale-warnings"},
		{http.MethodPost, "/documents/upload"},
		{http.MethodPost, "/documents/text"},
		{http.MethodGet, "/documents"},
		{http.MethodGet, "/documents/{document_id}"},
		{http.MethodPost, "/documents/{document_id}/versions"},
		{http.MethodDelete, "/documents/{document_id}"},
		{http.MethodPost, "/governance/context/search"},
	} {
		methods, ok := doc.Paths[endpoint.path]
		if !ok {
			t.Fatalf("missing OpenAPI path %s", endpoint.path)
		}
		if _, ok := methods[strings.ToLower(endpoint.method)]; !ok {
			t.Fatalf("missing OpenAPI operation %s %s", endpoint.method, endpoint.path)
		}
	}

	for _, path := range []string{
		"/governance/threads/{thread_id}/artifacts",
		"/governance/threads/{thread_id}/artifacts/{artifact_id}",
	} {
		if _, ok := doc.Paths[path]; ok {
			t.Fatalf("legacy thread artifact path still exposed in OpenAPI: %s", path)
		}
	}

	rawRoutes := map[string]bool{
		http.MethodPost + " /governance/files/upload":           false,
		http.MethodGet + " /governance/files/{id}/content":      false,
		http.MethodGet + " /artifacts/{id}/files/{key}/content": false,
		http.MethodGet + " /integrations/oauth-callback":        false,
	}
	if err := chi.Walk(srv.(chi.Routes), func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		key := method + " " + route
		if _, ok := rawRoutes[key]; ok {
			rawRoutes[key] = true
		}
		return nil
	}); err != nil {
		t.Fatalf("walk chi routes: %v", err)
	}
	for endpoint, found := range rawRoutes {
		if !found {
			t.Fatalf("missing raw route %s", endpoint)
		}
	}

	for _, eventType := range []string{
		"artifact.published",
		"artifact.needs_changes",
		"artifact.superseded",
		"feature.canonical_changed",
		"change_request.acceptance_criteria_changed",
	} {
		if !strings.Contains(openAPIJSON, eventType) {
			t.Fatalf("OpenAPI event_type enum missing %q", eventType)
		}
	}
}

func TestRouter_ArtifactsOpen(t *testing.T) {
	t.Parallel()
	rt := &Router{
		Handlers: &Handlers{},
		Config:   testConfig(),
	}
	srv := rt.Build()
	req := httptest.NewRequest(http.MethodGet, "/artifacts", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	// Stub handler returns 501; important: not 401 (no auth gate in local dev).
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRouter_McpStream_NotMountedWithoutKnowledgeAndArtifacts(t *testing.T) {
	t.Parallel()
	h, cleanup := testHandlersSettings(t)
	defer cleanup()
	rt := &Router{
		Handlers: h,
		Config:   testConfig(),
	}
	srv := rt.Build()
	req := httptest.NewRequest(http.MethodGet, "/mcp/stream", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 without full MCP deps, got %d body=%s", rec.Code, rec.Body.String())
	}
}
