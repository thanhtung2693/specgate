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

func TestRouter_DoesNotExposeDirectArtifactAuthoringRoutes(t *testing.T) {
	t.Parallel()
	rt := &Router{Handlers: &Handlers{}, Config: testConfig()}
	srv := rt.Build()
	for _, methodPath := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/artifacts"},
		{http.MethodDelete, "/artifacts/artifact-id"},
	} {
		req := httptest.NewRequest(methodPath.method, methodPath.path, strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s: status=%d, want route removed", methodPath.method, methodPath.path, rec.Code)
		}
	}
}

func TestRouter_LimitsCreateKnowledgeVersionBody(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.Knowledge.MaxFileBytes = 2 << 20
	srv := (&Router{Handlers: &Handlers{}, Config: cfg}).Build()
	request := func(contentBytes int) *httptest.ResponseRecorder {
		t.Helper()
		body := `{"workspace_id":"ws-a","title":"Rules","document_type":"srs","authority_level":"high","content":"` +
			strings.Repeat("x", contentBytes) + `"}`
		req := httptest.NewRequest(http.MethodPost, "/documents/doc-1/versions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}

	if rec := request(2 << 20); rec.Code != http.StatusNotImplemented {
		t.Fatalf("within configured envelope status=%d, want handler response 501; body=%s", rec.Code, rec.Body.String())
	}
	if rec := request((3 << 20) + 1); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("above configured envelope status=%d, want 413; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRouter_RejectsManualArtifactSuperseding(t *testing.T) {
	t.Parallel()
	rt := &Router{Handlers: &Handlers{}, Config: testConfig()}
	srv := rt.Build()
	req := httptest.NewRequest(http.MethodPatch, "/artifacts/artifact-id/status?workspace_id=workspace-id", strings.NewReader(`{"status":"superseded"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("manual supersede status=%d, want 422 body=%s", rec.Code, rec.Body.String())
	}
}

func TestRouter_ExposesOnlyResourceScopedIntegrationWebhooks(t *testing.T) {
	t.Parallel()
	srv := (&Router{Handlers: &Handlers{}, Config: testConfig()}).Build()
	for _, path := range []string{
		"/integrations/integration-1/github/webhook",
		"/integrations/integration-1/gitlab/webhook",
		"/integrations/integration-1/linear/webhook",
		"/integrations/integration-1/webhook-secret",
		"/integrations/integration-1/webhook-secret/rotate",
	} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("legacy route %s status=%d, want 404", path, rec.Code)
		}
	}
}

func TestRouter_RegistersChangeRequestDeliveryLinks(t *testing.T) {
	t.Parallel()
	srv := (&Router{Handlers: &Handlers{}, Config: testConfig()}).Build()
	req := httptest.NewRequest(http.MethodGet, "/workboard/change-requests/cr-1/delivery-links?workspace_id=ws-a", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatalf("delivery-links route was not registered: status=%d body=%s", rec.Code, rec.Body.String())
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
		strings.NewReader(`{"workspace_id":"ws-test","name":"hello.png","content_type":"image/png","size_bytes":1024}`),
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
		strings.NewReader(`{"workspace_id":"ws-test","name":"hello.png","content_type":"image/png","size_bytes":1024}`),
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
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name     string `json:"name"`
				Required bool   `json:"required"`
			} `json:"parameters"`
		} `json:"paths"`
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
		{http.MethodGet, "/artifacts"},
		{http.MethodGet, "/artifacts/{id}"},
		{http.MethodPatch, "/artifacts/{id}/status"},
		{http.MethodGet, "/artifacts/{id}/files"},
		{http.MethodGet, "/artifacts/{id}/files/_"},
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
		{http.MethodPut, "/integrations/{id}/api-token"},
		{http.MethodPost, "/integrations/oauth/begin"},
		{http.MethodPost, "/integrations/{id}/oauth/authorize"},
		{http.MethodPost, "/integrations/{id}/oauth/disconnect"},
		{http.MethodGet, "/governance/feedback-events"},
		{http.MethodPost, "/governance/feedback-events/{id}/status"},
		{http.MethodGet, "/skills"},
		{http.MethodPost, "/skills"},
		{http.MethodPut, "/skills/{id}"},
		{http.MethodDelete, "/skills/{id}"},
		{http.MethodGet, "/workboard/features"},
		{http.MethodPost, "/workboard/features"},
		{http.MethodPost, "/workboard/features/upsert-by-key"},
		{http.MethodGet, "/workboard/features/{id}"},
		{http.MethodPatch, "/workboard/features/{id}"},
		{http.MethodGet, "/workboard/change-requests"},
		{http.MethodPost, "/workboard/change-requests"},
		{http.MethodGet, "/workboard/change-requests/{id}"},
		{http.MethodPatch, "/workboard/change-requests/{id}"},
		{http.MethodPost, "/workboard/change-requests/{id}/unarchive"},
		{http.MethodGet, "/workboard/change-requests/{id}/acceptance-criteria"},
		{http.MethodGet, "/workboard/change-requests/{id}/next-actions"},
		{http.MethodPost, "/workboard/change-requests/{id}/gate-runs/refresh"},
		{http.MethodGet, "/workboard/change-requests/{id}/gate-runs"},
		{http.MethodGet, "/workboard/change-requests/{id}/tracker-links"},
		{http.MethodPost, "/workboard/change-requests/{id}/linear-handoff"},
		{http.MethodGet, "/workboard/stale-warnings"},
		{http.MethodPost, "/documents/upload"},
		{http.MethodPost, "/documents/text"},
		{http.MethodGet, "/documents"},
		{http.MethodGet, "/documents/{document_id}"},
		{http.MethodPost, "/documents/{document_id}/versions"},
		{http.MethodPost, "/documents/{document_id}/retry"},
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

	for _, endpoint := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/workboard/change-requests/{id}/delivery-links"},
		{http.MethodPost, "/workboard/change-requests/{id}/linear-handoff"},
	} {
		operation := doc.Paths[endpoint.path][strings.ToLower(endpoint.method)]
		foundRequiredWorkspace := false
		for _, parameter := range operation.Parameters {
			if parameter.Name == "workspace_id" && parameter.Required {
				foundRequiredWorkspace = true
			}
		}
		if !foundRequiredWorkspace {
			t.Fatalf("%s %s must declare required workspace_id", endpoint.method, endpoint.path)
		}
	}

	for _, path := range []string{
		"/governance/threads/{thread_id}/artifacts",
		"/governance/threads/{thread_id}/artifacts/{artifact_id}",
		"/workboard/change-requests/{id}/lead-artifact",
	} {
		if _, ok := doc.Paths[path]; ok {
			t.Fatalf("legacy thread artifact path still exposed in OpenAPI: %s", path)
		}
	}

	rawRoutes := map[string]bool{
		http.MethodPost + " /governance/files/upload":      false,
		http.MethodGet + " /governance/files/{id}/content": false,
		http.MethodGet + " /integrations/oauth-callback":   false,
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
		"artifact.approved",
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

func TestRouter_DocsUseScalar(t *testing.T) {
	t.Parallel()
	srv := (&Router{Handlers: &Handlers{}, Config: testConfig()}).Build()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("docs status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "@scalar/api-reference") {
		t.Fatalf("docs do not use Scalar: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "@stoplight/elements") {
		t.Fatalf("docs still use the previous renderer: %s", rec.Body.String())
	}
}

func TestRouter_ReadinessSchemaIncludesNotRun(t *testing.T) {
	t.Parallel()
	srv := (&Router{Handlers: &Handlers{}, Config: testConfig()}).Build()
	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("OpenAPI status = %d body=%s", rec.Code, rec.Body.String())
	}

	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Enum []string `json:"enum"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	states := doc.Components.Schemas["ArtifactReadinessRunDTO"].Properties["state"].Enum
	for _, state := range states {
		if state == "not_run" {
			return
		}
	}
	t.Fatalf("ArtifactReadinessRunDTO state enum = %v, want not_run", states)
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

func TestRouter_DoesNotExposeMCPRoutes(t *testing.T) {
	t.Parallel()
	rt := &Router{
		Handlers: &Handlers{},
		Config:   testConfig(),
	}
	srv := rt.Build()
	for _, methodPath := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/mcp/stream"},
		{http.MethodGet, "/mcp/info"},
		{http.MethodGet, "/mcp/api-key"},
		{http.MethodPost, "/mcp/api-key/rotate"},
	} {
		req := httptest.NewRequest(methodPath.method, methodPath.path, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s: status=%d, want MCP route absent", methodPath.method, methodPath.path, rec.Code)
		}
	}
}
