package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/specgate/specgate/app/cli/internal/client"
)

func TestClientUnwrapsHumaBody(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body": map[string]any{"api_version": "specgate.api/v1"},
		})
	}))
	defer srv.Close()

	c := client.New(srv.URL, time.Second)
	got, err := c.Meta(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.APIVersion != "specgate.api/v1" {
		t.Fatalf("api_version = %q", got.APIVersion)
	}
}

func TestClientMaps409ToConflict(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": 409,
			"title":  "Conflict",
			"detail": "version stale",
		})
	}))
	defer srv.Close()

	c := client.New(srv.URL, time.Second)
	_, err := c.Meta(context.Background())

	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T %v, want *APIError", err, err)
	}
	if apiErr.Kind != client.ErrorConflict {
		t.Fatalf("kind = %v, want ErrorConflict", apiErr.Kind)
	}
	if apiErr.Status != 409 {
		t.Fatalf("status = %d, want 409", apiErr.Status)
	}
}

func TestClientPreservesHumaValidationErrors(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": 422,
			"title":  "Unprocessable Entity",
			"detail": "validation failed",
			"errors": []map[string]any{{
				"message":  "unexpected property",
				"location": "body.criteria[0].evidence.detail",
				"value":    map[string]any{"detail": "not allowed"},
			}},
		})
	}))
	defer srv.Close()

	c := client.New(srv.URL, time.Second)
	_, err := c.Meta(context.Background())

	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T %v, want *APIError", err, err)
	}
	raw, ok := apiErr.Details["errors"].([]map[string]any)
	if !ok || len(raw) != 1 {
		t.Fatalf("details errors = %#v, want one error", apiErr.Details["errors"])
	}
	if raw[0]["location"] != "body.criteria[0].evidence.detail" {
		t.Fatalf("location = %v", raw[0]["location"])
	}
}

func TestClientMaps404ToNotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := client.New(srv.URL, time.Second)
	_, err := c.Meta(context.Background())

	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T", err)
	}
	if apiErr.Kind != client.ErrorNotFound {
		t.Fatalf("kind = %v, want ErrorNotFound", apiErr.Kind)
	}
}

func TestClientMaps503ToUnavailable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := client.New(srv.URL, time.Second)
	_, err := c.Status(context.Background(), "")

	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T", err)
	}
	if apiErr.Kind != client.ErrorUnavailable {
		t.Fatalf("kind = %v, want ErrorUnavailable", apiErr.Kind)
	}
}

func TestClientStatusAddsWorkspaceFilter(t *testing.T) {
	t.Parallel()
	var gotWorkspace string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotWorkspace = r.URL.Query().Get("workspace_id")
		_ = json.NewEncoder(w).Encode(map[string]any{"counts": map[string]any{}})
	}))
	defer srv.Close()

	c := client.New(srv.URL, time.Second)
	if _, err := c.Status(context.Background(), "ws-1"); err != nil {
		t.Fatal(err)
	}
	if gotWorkspace != "ws-1" {
		t.Fatalf("workspace_id query = %q, want ws-1", gotWorkspace)
	}
}

func TestClientStatsAddsWorkspaceAndDaysFilters(t *testing.T) {
	t.Parallel()
	var gotWorkspace, gotDays string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotWorkspace = r.URL.Query().Get("workspace_id")
		gotDays = r.URL.Query().Get("days")
		_ = json.NewEncoder(w).Encode(map[string]any{"window_days": 7, "reviewed_items": 2})
	}))
	defer srv.Close()

	c := client.New(srv.URL, time.Second)
	got, err := c.Stats(context.Background(), "ws-1", 7)
	if err != nil {
		t.Fatal(err)
	}
	if gotWorkspace != "ws-1" || gotDays != "7" {
		t.Fatalf("query = (%q, %q), want (ws-1, 7)", gotWorkspace, gotDays)
	}
	if got.WindowDays != 7 || got.ReviewedItems != 2 {
		t.Fatalf("stats = %#v", got)
	}
}

func TestClientHealthzToleratesMissing(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := client.New(srv.URL, time.Second)
	if err := c.Healthz(context.Background()); err != nil {
		t.Fatalf("Healthz with 404 should be tolerated: %v", err)
	}
}

func TestClientListAcceptanceCriteria(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{"id": "ac-1", "text": "Cannot over-redeem", "done": false},
				{"id": "ac-2", "text": "Audit log exists", "done": true},
			},
		})
	}))
	defer srv.Close()

	c := client.New(srv.URL, time.Second)
	got, err := c.ListAcceptanceCriteria(context.Background(), "cr-1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/workboard/change-requests/cr-1/acceptance-criteria" {
		t.Fatalf("path = %q", gotPath)
	}
	if len(got) != 2 || got[0].ID != "ac-1" || got[0].Text != "Cannot over-redeem" || !got[1].Done {
		t.Fatalf("criteria = %+v", got)
	}
}
