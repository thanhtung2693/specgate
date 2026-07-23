package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/config"
	"github.com/specgate/doc-registry/internal/integrations"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
	"github.com/specgate/doc-registry/internal/workboard"
	"gorm.io/gorm"
)

// TestMain sets the encryption key process-wide so encrypt/decrypt of stored
// webhook secrets works in parallel webhook tests (t.Setenv is disallowed once
// t.Parallel is called). Individual tests may still t.Setenv the same value.
func TestGitLabResourceWebhookContextPackChangeCreatesStaleFeedback(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()

	ctx := workboard.WithWorkspace(context.Background(), "ws-test")
	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(ctx, workboard.Feature{
		ID:     "feature-context",
		Key:    "CTX",
		Name:   "Context feature",
		Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := storagedb.NewRepository(db).Insert(ctx, &artifact.Artifact{
		ID:          "artifact-context-pack",
		WorkspaceID: "ws-test",
		FeatureID:   feature.ID,
		Version:     "v0.1",
		Status:      artifact.StatusDraft,
		RequestType: artifact.RequestTypeChangeRequest,
		CreatedBy:   "test",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	cr, err := workBoardRepo.CreateChangeRequest(ctx, workboard.ChangeRequest{
		ID:        "cr-context-v1",
		Key:       "CR-CONTEXT-V1",
		FeatureID: feature.ID,
		WorkType:  workboard.WorkTypeFeatureChange,
		Title:     "Change context-backed flow",
	})
	if err != nil {
		t.Fatal(err)
	}
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID:          "gitlab-context",
		WorkspaceID: "ws-test",
		Provider:    integrations.ProviderGitLab,
		Name:        "Acme GitLab",
		Status:      integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = intRepo.CreateResource(ctx, integrations.Resource{
		ID:            "repo-context",
		IntegrationID: integration.ID,
		ResourceType:  integrations.ResourceTypeProject,
		ExternalID:    "777",
		ExternalKey:   "acme/projects/specgate-context",
	})
	if err != nil {
		t.Fatal(err)
	}

	result := postGitLabWebhook(t, srv.URL, integration.ID, map[string]any{
		"object_kind": "merge_request",
		"event_type":  "merge_request",
		"project": map[string]any{
			"id":                  777,
			"path_with_namespace": "acme/projects/specgate-context",
		},
		"object_attributes": map[string]any{
			"id":            9004,
			"iid":           44,
			"action":        "update",
			"state":         "opened",
			"title":         "CR-CONTEXT-V1 implementation changed",
			"description":   "Refresh implementation path.\n\n<!-- specgate-work-ref: CR-CONTEXT-V1 -->",
			"url":           "https://gitlab.acme.io/acme/projects/specgate-context/-/merge_requests/44",
			"source_branch": "specgate/cr-context-v1",
			"target_branch": "master",
		},
	})
	if result.ChangeRequestID != cr.ID || len(result.FeedbackEventIDs) != 1 {
		t.Fatalf("expected one MR feedback event, got %#v", result)
	}
	feedback := getIntegrationJSON[struct {
		Items []integrations.GovernanceFeedbackEvent `json:"items"`
	}](t, srv.URL+"/governance/feedback-events?status=received")
	if len(feedback.Items) != 1 {
		t.Fatalf("expected one feedback event, got %#v", feedback.Items)
	}
}

func TestGitLabResourceWebhookDuplicateEventUUIDIsIdempotent(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()

	ctx := workboard.WithWorkspace(context.Background(), "ws-test")
	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(ctx, workboard.Feature{
		ID:     "feature-duplicate",
		Key:    "DUP",
		Name:   "Duplicate feature",
		Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	cr, err := workBoardRepo.CreateChangeRequest(ctx, workboard.ChangeRequest{
		ID:        "cr-duplicate-v1",
		Key:       "CR-DUPLICATE-V1",
		FeatureID: feature.ID,
		WorkType:  workboard.WorkTypeFeatureChange,
		Title:     "Duplicate-safe delivery",
	})
	if err != nil {
		t.Fatal(err)
	}
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID:          "gitlab-duplicate",
		WorkspaceID: "ws-test",
		Provider:    integrations.ProviderGitLab,
		Name:        "Acme GitLab",
		Status:      integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = intRepo.CreateResource(ctx, integrations.Resource{
		ID:            "repo-duplicate",
		IntegrationID: integration.ID,
		ResourceType:  integrations.ResourceTypeProject,
		ExternalID:    "888",
		ExternalKey:   "acme/projects/specgate-duplicate",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{
		"object_kind": "merge_request",
		"event_type":  "merge_request",
		"project": map[string]any{
			"id":                  888,
			"path_with_namespace": "acme/projects/specgate-duplicate",
		},
		"object_attributes": map[string]any{
			"id":            9005,
			"iid":           45,
			"action":        "open",
			"state":         "opened",
			"title":         "CR-DUPLICATE-V1 implementation",
			"description":   "Duplicate delivery fixture.\n\n<!-- specgate-work-ref: CR-DUPLICATE-V1 -->",
			"url":           "https://gitlab.acme.io/acme/projects/specgate-duplicate/-/merge_requests/45",
			"source_branch": "specgate/cr-duplicate-v1",
			"target_branch": "master",
		},
	}

	first := postGitLabWebhookWithUUID(t, srv.URL, integration.ID, "evt-duplicate-1", payload)
	if first.ChangeRequestID != cr.ID || first.Status != integrations.WebhookStatusProcessed {
		t.Fatalf("unexpected first webhook result: %#v", first)
	}
	second := postGitLabWebhookWithUUID(t, srv.URL, integration.ID, "evt-duplicate-1", payload)
	if second.WebhookEventID != first.WebhookEventID || second.IgnoredReason != "duplicate_webhook_event" {
		t.Fatalf("duplicate should return existing event without reprocessing: first=%#v second=%#v", first, second)
	}
	feedback := getIntegrationJSON[struct {
		Items []integrations.GovernanceFeedbackEvent `json:"items"`
	}](t, srv.URL+"/governance/feedback-events?status=received")
	if len(feedback.Items) != 1 {
		t.Fatalf("duplicate webhook should not create duplicate feedback, got %#v", feedback.Items)
	}
}

func TestGitLabResourceWebhookRejectsMissingOrWrongSignature(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()

	ctx := context.Background()
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID:          "gitlab-secured",
		WorkspaceID: "ws-test",
		Provider:    integrations.ProviderGitLab,
		Name:        "Acme GitLab",
		Status:      integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-secured", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeProject,
		ExternalID: "1", ExternalKey: "ns/repo",
	}); err != nil {
		t.Fatal(err)
	}
	// The resource owns a signing token; a delivery whose signature does not
	// verify against it must be rejected.

	payload := map[string]any{
		"object_kind": "merge_request",
		"project":     map[string]any{"id": 1, "path_with_namespace": "ns/repo"},
	}

	missing := doGitLabWebhook(t, srv.URL, integration.ID, "", "", payload)
	defer missing.Body.Close()
	if missing.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing signature: want 401, got %d body=%s", missing.StatusCode, readBody(missing))
	}

	// Signed with a different token → signature mismatch.
	wrong := doGitLabWebhook(t, srv.URL, integration.ID, testGitLabSigningTokenAlt, "", payload)
	defer wrong.Body.Close()
	if wrong.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong signature: want 401, got %d body=%s", wrong.StatusCode, readBody(wrong))
	}
}

func TestGitLabResourceWebhookRejectsWhenNoSecretConfigured(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()

	ctx := context.Background()
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID:          "gitlab-no-secret",
		WorkspaceID: "ws-test",
		Provider:    integrations.ProviderGitLab,
		Name:        "Acme GitLab",
		Status:      integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-no-secret", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeProject,
		ExternalID: "1", ExternalKey: "ns/repo", ConfigJSON: `{"test_no_webhook_secret":true}`,
	}); err != nil {
		t.Fatal(err)
	}
	// No signing token is stored on this resource, so even a well-formed,
	// validly-signed delivery cannot verify — the endpoint must refuse rather
	// than act as an open relay.
	resp := doGitLabWebhook(t, srv.URL, integration.ID, testGitLabSigningToken, "", map[string]any{
		"object_kind": "merge_request",
		"project":     map[string]any{"id": 1, "path_with_namespace": "ns/repo"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no configured signing token should return 401, got %d", resp.StatusCode)
	}
}

func TestGitLabResourceWebhookDedupsByPayloadHashWhenUUIDMissing(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()

	ctx := workboard.WithWorkspace(context.Background(), "ws-test")
	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(ctx, workboard.Feature{
		ID: "feature-nouuid", Key: "NOUUID", Name: "No UUID", Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	cr, err := workBoardRepo.CreateChangeRequest(ctx, workboard.ChangeRequest{
		ID: "cr-nouuid", Key: "CR-NOUUID-1", FeatureID: feature.ID,
		WorkType: workboard.WorkTypeNewFeature, Title: "No-UUID delivery",
	})
	if err != nil {
		t.Fatal(err)
	}
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "gitlab-nouuid", WorkspaceID: "ws-test", Provider: integrations.ProviderGitLab, Name: "GL", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-nouuid", IntegrationID: integration.ID,
		ResourceType: integrations.ResourceTypeProject, ExternalID: "999",
		ExternalKey: "ns/specgate-nouuid",
	}); err != nil {
		t.Fatal(err)
	}

	payload := map[string]any{
		"object_kind": "merge_request",
		"event_type":  "merge_request",
		"project":     map[string]any{"id": 999, "path_with_namespace": "ns/specgate-nouuid"},
		"object_attributes": map[string]any{
			"id": 11, "iid": 1, "action": "open", "state": "opened",
			"title":         "CR-NOUUID-1 implement",
			"description":   "No UUID delivery fixture.\n\n<!-- specgate-work-ref: CR-NOUUID-1 -->",
			"url":           "https://gl/ns/specgate-nouuid/-/merge_requests/1",
			"source_branch": "specgate/cr-nouuid", "target_branch": "main",
		},
	}

	first := postGitLabWebhook(t, srv.URL, integration.ID, payload)
	if first.ChangeRequestID != cr.ID || first.Status != integrations.WebhookStatusProcessed {
		t.Fatalf("first delivery: %#v", first)
	}
	second := postGitLabWebhook(t, srv.URL, integration.ID, payload)
	if second.WebhookEventID != first.WebhookEventID {
		t.Fatalf("missing-UUID replay should dedup via SHA256(payload); got fresh event id second=%#v first=%#v", second, first)
	}
	if second.IgnoredReason != "duplicate_webhook_event" {
		t.Fatalf("expected duplicate ignored reason, got %q", second.IgnoredReason)
	}

	feedback := getIntegrationJSON[struct {
		Items []integrations.GovernanceFeedbackEvent `json:"items"`
	}](t, srv.URL+"/governance/feedback-events?status=received")
	if len(feedback.Items) != 1 {
		t.Fatalf("expected one feedback row across two deliveries, got %d", len(feedback.Items))
	}
}

func TestListIntegrationRepos_GitLabReturnsProjects(t *testing.T) {
	t.Setenv(integrations.SecretKeyEnvVar, testSecretKey)

	var gotPath string
	gl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Header.Get("PRIVATE-TOKEN") != "gitlab-test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":42,"path_with_namespace":"acme/web","name":"Web","default_branch":"main"}]`))
	}))
	defer gl.Close()

	srv := integrationsTestServer(t)
	defer srv.Close()

	created := postIntegrationJSON[integrations.Integration](t, srv.URL+"/integrations", map[string]any{
		"provider": integrations.ProviderGitLab,
		"name":     "Acme GitLab",
		"base_url": gl.URL,
	})
	// Store a PAT so ResolveAPIToken can authenticate the outbound list call.
	tokenBody, _ := json.Marshal(map[string]string{"api_token": "gitlab-test-token"})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/integrations/"+created.ID+"/api-token?workspace_id=ws-test", bytes.NewReader(tokenBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("set token: %d", resp.StatusCode)
	}

	repos := getIntegrationJSON[struct {
		Items []integrations.RepoSummary `json:"items"`
	}](t, srv.URL+"/integrations/"+created.ID+"/repos?search=web&limit=10")
	if len(repos.Items) != 1 {
		t.Fatalf("want 1 repo, got %#v", repos.Items)
	}
	if r := repos.Items[0]; r.ExternalID != "42" || r.ExternalKey != "acme/web" || r.DisplayName != "Web" || r.DefaultRef != "main" {
		t.Fatalf("unexpected repo summary: %#v", r)
	}
	if gotPath != "/api/v4/projects" {
		t.Fatalf("gitlab path = %q, want /api/v4/projects", gotPath)
	}
}

func TestListIntegrationRepos_LinearRejectedWith400(t *testing.T) {
	srv := integrationsTestServer(t)
	defer srv.Close()
	created := postIntegrationJSON[integrations.Integration](t, srv.URL+"/integrations", map[string]any{
		"provider": integrations.ProviderLinear,
		"name":     "Acme Linear",
	})
	resp, err := http.Get(srv.URL + "/integrations/" + created.ID + "/repos")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("linear repos: want 400, got %d body=%s", resp.StatusCode, readBody(resp))
	}
}

func TestCreateIntegrationResource_AutoProvisionsGitLabWebhook(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotToken string
	var gotBody map[string]any
	gl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode gitlab hook create body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		// GitLab 19.0+ stores the signing token (the preferred path).
		_, _ = w.Write([]byte(`{"id":99,"signing_token_present":true}`))
	}))
	defer gl.Close()

	srv := integrationsTestServer(t)
	defer srv.Close()

	created := postIntegrationJSON[integrations.Integration](t, srv.URL+"/integrations", map[string]any{
		"provider": integrations.ProviderGitLab,
		"name":     "Acme GitLab",
		"base_url": gl.URL,
	})
	setIntegrationAPIToken(t, srv.URL, created.ID, "gitlab-hook-test-token")

	resource := postIntegrationJSON[integrations.Resource](t, srv.URL+"/integrations/"+created.ID+"/resources", integrations.Resource{
		ResourceType: integrations.ResourceTypeProject,
		ExternalID:   "321",
		ExternalKey:  "acme/web",
		DisplayName:  "Web",
		DefaultRef:   "main",
	})
	if !resource.HasWebhookSecret {
		t.Fatalf("expected resource webhook secret presence flag, got %#v", resource)
	}
	if gotPath != "/api/v4/projects/321/hooks" {
		t.Fatalf("gitlab hook path = %q, want /api/v4/projects/321/hooks", gotPath)
	}
	if gotToken != "gitlab-hook-test-token" {
		t.Fatalf("gitlab token header = %q", gotToken)
	}
	if gotBody["merge_requests_events"] != true || gotBody["note_events"] != true {
		t.Fatalf("unexpected gitlab hook event flags: %#v", gotBody)
	}
	if _, ok := gotBody["issues_events"]; ok {
		t.Fatalf("gitlab hook must not subscribe to issue events: %#v", gotBody)
	}
	token, ok := gotBody["signing_token"].(string)
	if !ok || !strings.HasPrefix(token, "whsec_") {
		t.Fatalf("expected whsec_ signing token, got %#v", gotBody["signing_token"])
	}
	if _, ok := gotBody["token"]; ok {
		t.Fatalf("gitlab webhook create must not send legacy token: %#v", gotBody["token"])
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(resource.ConfigJSON), &cfg); err != nil {
		t.Fatalf("resource config json: %v", err)
	}
	wantURL := testOAuthCallbackBaseURL + "/integrations/" + created.ID + "/resources/" + resource.ID + "/gitlab/webhook"
	if cfg["webhook_url"] != wantURL {
		t.Fatalf("webhook_url = %#v, want %q", cfg["webhook_url"], wantURL)
	}
	if cfg["provider_webhook_id"] != "99" || cfg["webhook_status"] != "connected" {
		t.Fatalf("unexpected webhook config: %#v", cfg)
	}
}

func TestCreateIntegrationResource_AutoProvisionsGitHubWebhook(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotAuth string
	var gotBody map[string]any
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode github hook create body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":88}`))
	}))
	defer gh.Close()

	srv := integrationsTestServer(t)
	defer srv.Close()

	created := postIntegrationJSON[integrations.Integration](t, srv.URL+"/integrations", map[string]any{
		"provider": integrations.ProviderGitHub,
		"name":     "Acme GitHub",
		"base_url": gh.URL,
	})
	setIntegrationAPIToken(t, srv.URL, created.ID, "ghp-hooktoken")

	resource := postIntegrationJSON[integrations.Resource](t, srv.URL+"/integrations/"+created.ID+"/resources", integrations.Resource{
		ResourceType: integrations.ResourceTypeRepo,
		ExternalID:   "321",
		ExternalKey:  "acme/web",
		DisplayName:  "Web",
		DefaultRef:   "main",
	})
	if !resource.HasWebhookSecret {
		t.Fatalf("expected resource webhook secret presence flag, got %#v", resource)
	}
	if gotPath != "/api/v3/repos/acme/web/hooks" {
		t.Fatalf("github hook path = %q, want /api/v3/repos/acme/web/hooks", gotPath)
	}
	if gotAuth != "Bearer ghp-hooktoken" {
		t.Fatalf("github auth header = %q", gotAuth)
	}
	events, _ := gotBody["events"].([]any)
	if len(events) != 2 || events[0] != "pull_request" || events[1] != "issue_comment" {
		t.Fatalf("unexpected github hook events: %#v", gotBody["events"])
	}
	configMap, _ := gotBody["config"].(map[string]any)
	if configMap["secret"] == "" {
		t.Fatalf("expected github hook secret in config: %#v", configMap)
	}
	wantURL := testOAuthCallbackBaseURL + "/integrations/" + created.ID + "/resources/" + resource.ID + "/github/webhook"
	if configMap["url"] != wantURL {
		t.Fatalf("github hook url = %#v, want %q", configMap["url"], wantURL)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(resource.ConfigJSON), &cfg); err != nil {
		t.Fatalf("resource config json: %v", err)
	}
	if cfg["provider_webhook_id"] != "88" || cfg["webhook_status"] != "connected" || cfg["webhook_url"] != wantURL {
		t.Fatalf("unexpected webhook config: %#v", cfg)
	}
}

func TestDeleteIntegrationResource_DeletesGitLabWebhookBeforeLocalRow(t *testing.T) {
	t.Parallel()

	var gotDeletePath string
	gl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":99,"signing_token_present":true}`))
		case http.MethodDelete:
			gotDeletePath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("method = %s, want POST or DELETE", r.Method)
		}
	}))
	defer gl.Close()

	srv := integrationsTestServer(t)
	defer srv.Close()

	// Reuse the server's DB through the HTTP surface: create integration + token + resource.
	created := postIntegrationJSON[integrations.Integration](t, srv.URL+"/integrations", map[string]any{
		"provider": integrations.ProviderGitLab,
		"name":     "Acme GitLab",
		"base_url": gl.URL,
	})
	setIntegrationAPIToken(t, srv.URL, created.ID, "gitlab-hook-test-token")
	resource := postIntegrationJSON[integrations.Resource](t, srv.URL+"/integrations/"+created.ID+"/resources", integrations.Resource{
		ResourceType: integrations.ResourceTypeProject,
		ExternalID:   "321",
		ExternalKey:  "acme/web",
		DisplayName:  "Web",
		DefaultRef:   "main",
	})

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/integrations/"+created.ID+"/resources/"+resource.ID+"?workspace_id=ws-test", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete resource: status %d", resp.StatusCode)
	}
	if gotDeletePath != "/api/v4/projects/321/hooks/99" {
		t.Fatalf("gitlab delete path = %q, want /api/v4/projects/321/hooks/99", gotDeletePath)
	}
	resources := getIntegrationJSON[struct {
		Items []integrations.Resource `json:"items"`
	}](t, srv.URL+"/integrations/"+created.ID+"/resources")
	if len(resources.Items) != 0 {
		t.Fatalf("expected resource removed, got %#v", resources.Items)
	}
}

func TestDeleteIntegrationResource_StrictProviderFailureKeepsLocalRow(t *testing.T) {
	t.Parallel()

	gl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":99,"signing_token_present":true}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`boom`))
	}))
	defer gl.Close()

	srv := integrationsTestServer(t)
	defer srv.Close()

	created := postIntegrationJSON[integrations.Integration](t, srv.URL+"/integrations", map[string]any{
		"provider": integrations.ProviderGitLab,
		"name":     "Acme GitLab",
		"base_url": gl.URL,
	})
	setIntegrationAPIToken(t, srv.URL, created.ID, "gitlab-hook-test-token")
	resource := postIntegrationJSON[integrations.Resource](t, srv.URL+"/integrations/"+created.ID+"/resources", integrations.Resource{
		ResourceType: integrations.ResourceTypeProject,
		ExternalID:   "321",
		ExternalKey:  "acme/web",
		DisplayName:  "Web",
		DefaultRef:   "main",
	})

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/integrations/"+created.ID+"/resources/"+resource.ID+"?workspace_id=ws-test", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("delete resource failure: status %d body=%s", resp.StatusCode, readBody(resp))
	}
	resources := getIntegrationJSON[struct {
		Items []integrations.Resource `json:"items"`
	}](t, srv.URL+"/integrations/"+created.ID+"/resources")
	if len(resources.Items) != 1 || resources.Items[0].ID != resource.ID {
		t.Fatalf("expected resource retained on upstream failure, got %#v", resources.Items)
	}
}

func integrationsWebhookTestServer(t *testing.T) (*httptest.Server, *gorm.DB) {
	t.Helper()
	db := newTestGormDB(t)
	configureWebhookTestResourceSecrets(t, db)
	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	settingsSvc := testIntegrationSettingsService(t, db)
	handlers := &Handlers{
		Integrations:         integrations.NewServiceWithWorkBoard(storagedb.NewIntegrationRepository(db), workBoardRepo).WithOAuthAppLookup(testIntegrationOAuthLookup()),
		WorkBoard:            workBoardRepo,
		Settings:             settingsSvc,
		OAuthCallbackBaseURL: testOAuthCallbackBaseURL,
		AppBaseURL:           testAppBaseURL,
	}
	rt := &Router{
		Handlers: handlers,
		Config: &config.Config{
			OpenAPI: config.OpenAPIConfig{Enabled: false},
		},
	}
	return httptest.NewServer(DevCORS(rt.Build())), db
}

// configureWebhookTestResourceSecrets gives directly-seeded webhook resources
// the same provider-specific secret their managed webhook would own. Fixtures
// that exercise the missing-secret guard opt out with test_no_webhook_secret.
func configureWebhookTestResourceSecrets(t *testing.T, db *gorm.DB) {
	t.Helper()
	gitLabSecret, err := integrations.EncryptSecret(testGitLabSigningToken)
	if err != nil {
		t.Fatal(err)
	}
	gitHubSecret, err := integrations.EncryptSecret(testGitHubWebhookSecret)
	if err != nil {
		t.Fatal(err)
	}
	linearSecret, err := integrations.EncryptSecret(testWebhookSecret)
	if err != nil {
		t.Fatal(err)
	}
	db.Callback().Create().Before("gorm:create").Register("test:resource-webhook-secret", func(tx *gorm.DB) {
		resource, ok := tx.Statement.Dest.(*integrations.Resource)
		if !ok || resource.WebhookSecretEncrypted != "" || strings.Contains(resource.ConfigJSON, `"test_no_webhook_secret":true`) {
			return
		}
		switch resource.ResourceType {
		case integrations.ResourceTypeProject:
			resource.WebhookSecretEncrypted = gitLabSecret
		case integrations.ResourceTypeRepo:
			resource.WebhookSecretEncrypted = gitHubSecret
		case integrations.ResourceTypeTeam:
			resource.WebhookSecretEncrypted = linearSecret
		}
	})
}

// testWebhookSecret is the shared secret tests configure on the server (per
// provider) so signed webhook payloads verify against the env-style secret.
const testWebhookSecret = "test-webhook-secret"

// signGitLabDelivery computes the Standard Webhooks v1 signature GitLab sends in
// webhook-signature: HMAC-SHA256 over {id}.{timestamp}.{body} keyed by the
// base64-decoded whsec_ token, base64-encoded with a v1, prefix.
func signGitLabDelivery(signingToken, webhookID, timestamp string, body []byte) string {
	key := []byte(signingToken)
	if raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(signingToken, "whsec_")); err == nil && strings.HasPrefix(signingToken, "whsec_") {
		key = raw
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(webhookID + "." + timestamp + "."))
	mac.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func resourceIDForWebhook(t *testing.T, baseURL, integrationID string, payload map[string]any) string {
	t.Helper()
	var target map[string]any
	if project, ok := payload["project"].(map[string]any); ok {
		target = project
	} else if repository, ok := payload["repository"].(map[string]any); ok {
		target = repository
	}
	if target == nil {
		t.Fatal("webhook payload has no project or repository target")
	}
	wantID := fmt.Sprint(target["id"])
	resources := getIntegrationJSON[struct {
		Items []integrations.Resource `json:"items"`
	}](t, baseURL+"/integrations/"+integrationID+"/resources")
	for _, resource := range resources.Items {
		if resource.ExternalID == wantID {
			return resource.ID
		}
	}
	t.Fatalf("no resource for webhook target %q in %#v", wantID, resources.Items)
	return ""
}

func postGitLabWebhook(t *testing.T, baseURL string, integrationID string, payload map[string]any) integrations.GitLabWebhookResult {
	return postGitLabWebhookWithUUID(t, baseURL, integrationID, "", payload)
}

func postGitLabWebhookWithUUID(t *testing.T, baseURL string, integrationID string, eventUUID string, payload map[string]any) integrations.GitLabWebhookResult {
	t.Helper()
	resp := doGitLabWebhook(t, baseURL, integrationID, testGitLabSigningToken, eventUUID, payload)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d body=%s", resp.StatusCode, readBody(resp))
	}
	var out integrations.GitLabWebhookResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// doGitLabWebhook posts a GitLab webhook signed with signingToken (a whsec_
// token). An empty signingToken sends NO signature headers (to exercise the
// "missing signature" guard). The selected resource owns
// testGitLabSigningToken, so a mismatching signingToken exercises the
// "wrong signature" guard.
func doGitLabWebhook(t *testing.T, baseURL string, integrationID string, signingToken string, eventUUID string, payload map[string]any) *http.Response {
	return doGitLabResourceWebhookEvent(t, baseURL, integrationID, resourceIDForWebhook(t, baseURL, integrationID, payload), "Merge Request Hook", signingToken, eventUUID, payload)
}
