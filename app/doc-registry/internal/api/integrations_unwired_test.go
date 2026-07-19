package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestIntegrationRouteReturns503WhenServiceUnwired verifies an unwired
// Integrations service degrades cleanly: the route returns 503, not a panic.
// Guards the requireService typed-nil check.
func TestIntegrationRouteReturns503WhenServiceUnwired(t *testing.T) {
	t.Parallel()
	h := &Handlers{} // Integrations is the zero value: a nil *integrations.Service
	srv := (&Router{Handlers: h, Config: testConfig()}).Build()

	req := httptest.NewRequest(http.MethodGet, "/integrations", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req) // must not panic

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unwired integration route: want 503, got %d: %s", w.Code, w.Body)
	}
}
