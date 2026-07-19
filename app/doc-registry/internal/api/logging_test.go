package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestRequestLoggerSkipsRoutineHealthProbes(t *testing.T) {
	var logs bytes.Buffer
	handler := RequestLogger(zerolog.New(&logs))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, path := range []string{"/healthz", "/readyz"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	if logs.Len() != 0 {
		t.Fatalf("routine health probes were logged: %s", logs.String())
	}

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
	if !strings.Contains(logs.String(), `"message":"http"`) {
		t.Fatalf("normal request was not logged: %s", logs.String())
	}
}
