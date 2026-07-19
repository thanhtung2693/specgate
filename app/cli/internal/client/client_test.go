package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

func TestClientReportsOversizedResponseExplicitly(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"body":{"api_version":"`))
		_, _ = w.Write([]byte(strings.Repeat("x", (4<<20)+128)))
		_, _ = w.Write([]byte(`"}}`))
	}))
	defer srv.Close()

	_, err := client.New(srv.URL, time.Second).Meta(context.Background())
	if err == nil || !strings.Contains(err.Error(), "response exceeds 4 MiB") {
		t.Fatalf("error = %v, want explicit response-size error", err)
	}
}

func TestPluginFileReportsOversizedResponseExplicitly(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", (4<<20)+1)))
	}))
	defer srv.Close()

	_, err := client.New(srv.URL, time.Second).PluginFile(context.Background(), "package.json")
	if err == nil || !strings.Contains(err.Error(), "response exceeds 4 MiB") {
		t.Fatalf("error = %v, want explicit response-size error", err)
	}
}

func TestClientPreservesServerURLPathPrefix(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body": map[string]any{"api_version": "specgate.api/v1"},
		})
	}))
	defer srv.Close()

	c := client.New(srv.URL+"/api/doc-registry", time.Second)
	if _, err := c.Meta(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/doc-registry/api/v1/meta" {
		t.Fatalf("request path = %q, want /api/doc-registry/api/v1/meta", gotPath)
	}
}

func TestClientPreservesEscapedPathSegments(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"", "/api/doc-registry"} {
		prefix := prefix
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()
			for _, id := range []string{"owner/repo", "50%", "two words", "tài-liệu"} {
				id := id
				t.Run(id, func(t *testing.T) {
					t.Parallel()
					var gotURI string
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						gotURI = r.URL.RequestURI()
						_ = json.NewEncoder(w).Encode(map[string]any{"body": map[string]any{"id": id}})
					}))
					defer srv.Close()

					c := client.New(srv.URL+prefix, time.Second)
					if _, err := c.GetArtifact(context.Background(), id); err != nil {
						t.Fatal(err)
					}
					want := prefix + "/api/v1/artifacts/" + url.PathEscape(id)
					if gotURI != want {
						t.Fatalf("request URI = %q, want %q", gotURI, want)
					}
				})
			}
		})
	}
}

func TestClientWorkspaceContextScopesFacadeRequests(t *testing.T) {
	t.Parallel()
	var gotWorkspace string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotWorkspace = r.URL.Query().Get("workspace_id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"change_request_id":"cr-1"}`))
	}))
	defer srv.Close()

	c := client.New(srv.URL, time.Second)
	if _, err := c.ResolveWorkRef(client.WithWorkspace(context.Background(), "ws-core"), "cr-1"); err != nil {
		t.Fatalf("ResolveWorkRef: %v", err)
	}
	if gotWorkspace != "ws-core" {
		t.Fatalf("workspace_id = %q, want ws-core", gotWorkspace)
	}
}

func TestClientAllWorkspacesContextMarksCrossWorkspaceRead(t *testing.T) {
	t.Parallel()
	var gotAll string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAll = r.URL.Query().Get("all_workspaces")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"change_request_id":"cr-1"}`))
	}))
	defer srv.Close()

	c := client.New(srv.URL, time.Second)
	if _, err := c.ResolveWorkRef(client.WithAllWorkspaces(context.Background()), "cr-1"); err != nil {
		t.Fatalf("ResolveWorkRef: %v", err)
	}
	if gotAll != "true" {
		t.Fatalf("all_workspaces = %q, want true", gotAll)
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

func TestListArtifactReadinessRunsIsReadOnly(t *testing.T) {
	t.Parallel()
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []any{map[string]any{
				"id":            "run-1",
				"artifact_id":   "art-7",
				"gate":          "spec_completeness",
				"state":         "pass",
				"evidence_json": `{"topics": ["scope"]}`,
			}},
		})
	}))
	defer srv.Close()

	c := client.New(srv.URL, time.Second)
	got, err := c.ListArtifactReadinessRuns(context.Background(), "art-7")
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodGet || path != "/artifacts/art-7/readiness-runs" {
		t.Fatalf("request = %s %s", method, path)
	}
	items, ok := got["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v", got["items"])
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

func TestClientKnowledgeRoutes(t *testing.T) {
	t.Parallel()
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.RequestURI()
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
		}
		switch r.URL.Path {
		case "/documents":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"body": map[string]any{
					"items":              []map[string]any{{"document_id": "doc-1", "version": "v1", "title": "Rules", "status": "indexed"}},
					"total":              1,
					"embeddings_enabled": true,
				},
			})
		case "/governance/context/search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"body": map[string]any{"results": []map[string]any{{"document_id": "doc-1", "title": "Rules", "score": 0.8}}},
			})
		case "/documents/doc-1/links":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"body": map[string]any{"document_id": "doc-1", "version": "v1.1", "parent_version": "v1", "linked_feature_id": "feat-1"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := client.New(srv.URL, time.Second)
	list, err := c.ListKnowledgeDocuments(context.Background(), client.KnowledgeListFilter{WorkspaceID: "ws-1", Limit: 25, DocumentType: "policy_doc"})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet || gotPath != "/documents?document_type=policy_doc&limit=25&workspace_id=ws-1" {
		t.Fatalf("list request = %s %s", gotMethod, gotPath)
	}
	if len(list.Items) != 1 || list.Items[0].DocumentID != "doc-1" {
		t.Fatalf("list = %#v", list)
	}

	results, err := c.SearchKnowledge(context.Background(), client.KnowledgeSearchInput{WorkspaceID: "ws-1", Query: "refunds", MaxChunks: 3})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/governance/context/search" {
		t.Fatalf("search request = %s %s", gotMethod, gotPath)
	}
	if gotBody["workspace_id"] != "ws-1" || gotBody["query"] != "refunds" || gotBody["max_chunks"].(float64) != 3 {
		t.Fatalf("search body = %#v", gotBody)
	}
	if len(results) != 1 || results[0].DocumentID != "doc-1" {
		t.Fatalf("results = %#v", results)
	}

	doc, err := c.CurateKnowledgeLinks(context.Background(), "doc-1", client.KnowledgeCurateLinksInput{LinkedFeatureID: "feat-1"})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/documents/doc-1/links" {
		t.Fatalf("curate request = %s %s", gotMethod, gotPath)
	}
	if gotBody["linked_feature_id"] != "feat-1" {
		t.Fatalf("curate body = %#v", gotBody)
	}
	if doc.Version != "v1.1" || doc.ParentVersion != "v1" || doc.LinkedFeatureID != "feat-1" {
		t.Fatalf("curated doc = %#v", doc)
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

func TestClientComponentHealthPreservesServerPrefix(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"components": map[string]any{
				"postgres": map[string]string{"status": "ok"},
			},
		})
	}))
	defer srv.Close()

	c := client.New(srv.URL+"/api/doc-registry", time.Second)
	got, err := c.ComponentHealth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/doc-registry/healthz/components" {
		t.Fatalf("request path = %q", gotPath)
	}
	if got == nil || got.Components["postgres"].Status != "ok" {
		t.Fatalf("component health = %#v", got)
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

func TestClientListsArchivedWorkForPortableRetry(t *testing.T) {
	t.Parallel()
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	defer srv.Close()

	c := client.New(srv.URL, time.Second)
	if _, err := c.ListWorkItemsIncludingArchived(context.Background(), "ws-1"); err != nil {
		t.Fatal(err)
	}
	if gotQuery.Get("workspace_id") != "ws-1" || gotQuery.Get("include_archived") != "true" {
		t.Fatalf("query = %v", gotQuery)
	}
}

func TestClientUpdateArtifactStatusPatchesStatus(t *testing.T) {
	t.Parallel()
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "art-1", "version": "v2", "status": "approved",
		})
	}))
	defer srv.Close()

	c := client.New(srv.URL, time.Second)
	got, err := c.UpdateArtifactStatus(context.Background(), "art-1", client.UpdateArtifactStatusInput{
		Status:     "approved",
		ApprovedBy: "lead",
		Note:       "ship it",
		ActorKind:  "human",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPatch || gotPath != "/artifacts/art-1/status" {
		t.Fatalf("request = %s %s, want PATCH /artifacts/art-1/status", gotMethod, gotPath)
	}
	if gotBody["status"] != "approved" || gotBody["approved_by"] != "lead" ||
		gotBody["note"] != "ship it" || gotBody["actor_kind"] != "human" {
		t.Fatalf("body = %v", gotBody)
	}
	if got.Status != "approved" || got.Version != "v2" {
		t.Fatalf("artifact = %+v", got)
	}
}

func TestClientDecideDeliveryPostsHumanDecision(t *testing.T) {
	t.Parallel()
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"change_request_id": "cr-1",
			"gate_run_id":       "run-human",
			"verdict":           "pass",
			"executor":          "human",
			"actor":             "lead",
			"note":              "looks good",
		})
	}))
	defer srv.Close()

	c := client.New(srv.URL, time.Second)
	got, err := c.DecideDelivery(context.Background(), "cr-1", client.DeliveryDecisionInput{
		Decision: "approve",
		Actor:    "lead",
		Note:     "looks good",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/work-items/cr-1/delivery-decision" {
		t.Fatalf("request = %s %s, want POST /api/v1/work-items/cr-1/delivery-decision", gotMethod, gotPath)
	}
	if gotBody["decision"] != "approve" || gotBody["actor"] != "lead" || gotBody["note"] != "looks good" {
		t.Fatalf("body = %v", gotBody)
	}
	if got.GateRunID != "run-human" || got.Executor != "human" || got.Actor != "lead" {
		t.Fatalf("result = %+v", got)
	}
}
