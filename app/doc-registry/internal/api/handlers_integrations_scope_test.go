package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/config"
	"github.com/specgate/doc-registry/internal/integrations"
	linearprovider "github.com/specgate/doc-registry/internal/integrations/linear"
	"github.com/specgate/doc-registry/internal/settings"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
	"github.com/specgate/doc-registry/internal/workboard"
	"gorm.io/gorm"
)

// TestMain sets the encryption key process-wide so encrypt/decrypt of stored
// webhook secrets works in parallel webhook tests (t.Setenv is disallowed once
// t.Parallel is called). Individual tests may still t.Setenv the same value.
func TestMain(m *testing.M) {
	_ = os.Setenv(integrations.SecretKeyEnvVar, testSecretKey)
	os.Exit(m.Run())
}

// testGitLabSigningToken is a valid Standard Webhooks signing token (whsec_ +
// base64 of a 32-byte key) the GitLab webhook tests configure + sign with.
var testGitLabSigningToken = "whsec_" + base64.StdEncoding.EncodeToString(make([]byte, 32))

// testGitLabSigningTokenAlt is a second valid token used to produce a signature
// that must NOT verify against testGitLabSigningToken.
var testGitLabSigningTokenAlt = "whsec_" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))

// testSecretKey is a throwaway 32-byte AES key (hex) so the service can encrypt
// and later decrypt the GitHub webhook secret for HMAC verification.
const testSecretKey = "0000000000000000000000000000000000000000000000000000000000000001"

const testGitHubWebhookSecret = "github-resource-webhook-secret"

func testIntegrationSettingsService(t *testing.T, db *gorm.DB) *settings.Service {
	t.Helper()
	settingsCrypto, err := settings.NewCrypto(hex.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := settings.NewServiceWithTTL(storagedb.NewSettingsRepository(db), settingsCrypto, time.Hour)
	if err != nil {
		t.Logf("settings init warning: %v", err)
	}
	t.Cleanup(svc.Stop)
	return svc
}

// testOAuthCallbackBaseURL is the static public callback origin stamped onto
// the test Handlers; OAuth credentials now come from config/env, not settings.
const testOAuthCallbackBaseURL = "https://specgate.example"

// testAppBaseURL is the UI origin the OAuth callback redirects back to (the
// backend and SPA are different origins in dev). Stamped onto the test Handlers.
const testAppBaseURL = "https://app.specgate.test"

// testIntegrationOAuthLookup mirrors the production env-sourced lookup: it
// returns a fixture OAuthAppConfig for every supported provider, keyed on
// provider (host_key is echoed through, not used for matching).
func testIntegrationOAuthLookup() integrations.OAuthAppLookup {
	return func(_ context.Context, provider string, hostKey string) (*integrations.OAuthAppConfig, error) {
		switch provider {
		case integrations.ProviderGitLab, integrations.ProviderGitHub, integrations.ProviderLinear:
			return &integrations.OAuthAppConfig{
				Provider:     provider,
				HostKey:      hostKey,
				ClientID:     "gl-client",
				ClientSecret: "gl-secret",
			}, nil
		default:
			return nil, nil
		}
	}
}

func integrationsTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	db := newTestGormDB(t)

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
	return httptest.NewServer(DevCORS(rt.Build()))
}

func TestIntegrationsAPI_WorkspaceScopesRootCatalog(t *testing.T) {
	srv := integrationsTestServer(t)
	defer srv.Close()

	create := func(workspaceID string) integrations.Integration {
		t.Helper()
		return postIntegrationJSON[integrations.Integration](t, srv.URL+"/integrations", map[string]any{
			"workspace_id": workspaceID,
			"provider":     integrations.ProviderGitHub,
			"name":         "Shared GitHub",
		})
	}
	a := create("ws-a")
	b := create("ws-b")
	if a.WorkspaceID != "ws-a" || b.WorkspaceID != "ws-b" {
		t.Fatalf("created workspace ids = %q, %q", a.WorkspaceID, b.WorkspaceID)
	}

	var listed struct {
		Items []integrations.Integration `json:"items"`
	}
	resp, err := http.Get(srv.URL + "/integrations?workspace_id=ws-a")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("workspace list status = %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		resp.Body.Close()
		t.Fatalf("decode workspace list: %v", err)
	}
	resp.Body.Close()
	if len(listed.Items) != 1 || listed.Items[0].ID != a.ID {
		t.Fatalf("workspace A list = %#v", listed.Items)
	}
	resp, err = http.Get(srv.URL + "/integrations/" + b.ID + "?workspace_id=ws-a")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-workspace get status = %d, want 404", resp.StatusCode)
	}
}

func TestIntegrationsAPI_RootCatalogRequiresWorkspace(t *testing.T) {
	srv := integrationsTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/integrations")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing workspace status = %d, want 400", resp.StatusCode)
	}
}

func TestIntegrationsAPI_RootCreateRequiresWorkspace(t *testing.T) {
	srv := integrationsTestServer(t)
	defer srv.Close()

	resp, err := http.Post(
		srv.URL+"/integrations",
		"application/json",
		strings.NewReader(`{"provider":"github","name":"unscoped"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing workspace status = %d, want 400", resp.StatusCode)
	}
}

func TestIntegrationsAPI_ChildRoutesRequireWorkspace(t *testing.T) {
	srv := integrationsTestServer(t)
	defer srv.Close()

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"repos", http.MethodGet, "/integrations/int-1/repos"},
		{"resources", http.MethodGet, "/integrations/int-1/resources"},
		{"webhook events", http.MethodGet, "/integrations/int-1/webhook-events"},
		{"api token", http.MethodPut, "/integrations/int-1/api-token"},
		{"oauth authorize", http.MethodPost, "/integrations/int-1/oauth/authorize"},
		{"oauth disconnect", http.MethodPost, "/integrations/int-1/oauth/disconnect"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{}`
			if tt.name == "api token" {
				body = `{"api_token":"token"}`
			}
			req, err := http.NewRequest(tt.method, srv.URL+tt.path, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", resp.StatusCode, readBody(resp))
			}
		})
	}
}

func TestIntegrationsAPI_TrackerLinksRequireWorkspace(t *testing.T) {
	srv := integrationsTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/workboard/change-requests/cr-unscoped/tracker-links")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing workspace status=%d, want 400", resp.StatusCode)
	}
}

// TestLinearHandoffAPI exercises the registered HTTP handler against the real
// repositories, rather than only its OpenAPI declaration.  The response shape
// is intentionally exact because the UI consumes this small projection.
func TestLinearHandoffAPI(t *testing.T) {
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()

	oldGraphQLURL := linearprovider.GraphQLURL
	t.Cleanup(func() { linearprovider.GraphQLURL = oldGraphQLURL })
	createCalls := 0
	linear := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(request.Query, "IssueCreate") {
			createCalls++
			_, _ = io.WriteString(w, `{"data":{"issueCreate":{"success":true,"issue":{"id":"linear-issue-1","identifier":"ENG-123","url":"https://linear.app/acme/issue/ENG-123","team":{"id":"team-selected-external"}}}}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"issue":null}}`)
	}))
	t.Cleanup(linear.Close)
	linearprovider.GraphQLURL = linear.URL

	workCtx := workboard.WithWorkspace(context.Background(), "ws-handoff")
	workRepo := storagedb.NewWorkBoardRepository(db)
	cr, err := workRepo.CreateChangeRequest(workCtx, workboard.ChangeRequest{
		ID: "cr-handoff", Key: "SG-HANDOFF", WorkType: workboard.WorkTypeBugFix,
		Title: "Handoff through HTTP", IntentMD: "Use the selected team.", AcceptanceCriteria: `["The issue exists"]`,
	})
	if err != nil {
		t.Fatal(err)
	}

	intRepo := storagedb.NewIntegrationRepository(db)
	token, err := integrations.EncryptSecret("linear-api-token")
	if err != nil {
		t.Fatal(err)
	}
	integration, err := intRepo.CreateIntegration(integrations.WithWorkspace(context.Background(), "ws-handoff"), integrations.Integration{
		ID: "int-linear-handoff", Provider: integrations.ProviderLinear, Name: "Linear", Status: integrations.StatusConnected, APITokenEncrypted: token,
	})
	if err != nil {
		t.Fatal(err)
	}
	team, err := intRepo.CreateResource(integrations.WithWorkspace(context.Background(), "ws-handoff"), integrations.Resource{
		ID: "team-selected", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeTeam, ExternalID: "team-selected-external", DisplayName: "Selected team",
	})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := intRepo.CreateIntegration(integrations.WithWorkspace(context.Background(), "ws-other"), integrations.Integration{
		ID: "int-linear-foreign", Provider: integrations.ProviderLinear, Name: "Other", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	foreignTeam, err := intRepo.CreateResource(integrations.WithWorkspace(context.Background(), "ws-other"), integrations.Resource{
		ID: "team-foreign", IntegrationID: foreign.ID, ResourceType: integrations.ResourceTypeTeam, ExternalID: "other-team",
	})
	if err != nil {
		t.Fatal(err)
	}

	post := func(workspaceID, integrationID, resourceID string, want int) map[string]json.RawMessage {
		t.Helper()
		raw := requestJSONStatus(t, http.MethodPost, want,
			srv.URL+"/workboard/change-requests/"+cr.ID+"/linear-handoff"+workspaceID,
			map[string]string{"integration_id": integrationID, "resource_id": resourceID},
		)
		var out map[string]json.RawMessage
		if want == http.StatusOK {
			if err := json.Unmarshal(raw, &out); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}

	post("", integration.ID, team.ID, http.StatusUnprocessableEntity)
	post("?workspace_id=ws-handoff", "missing", team.ID, http.StatusNotFound)
	post("?workspace_id=ws-handoff", foreign.ID, foreignTeam.ID, http.StatusNotFound)

	invalid, err := intRepo.CreateIntegration(integrations.WithWorkspace(context.Background(), "ws-handoff"), integrations.Integration{
		ID: "int-github-handoff", Provider: integrations.ProviderGitHub, Name: "GitHub", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	post("?workspace_id=ws-handoff", invalid.ID, team.ID, http.StatusBadRequest)
	disabled, err := intRepo.CreateIntegration(integrations.WithWorkspace(context.Background(), "ws-handoff"), integrations.Integration{
		ID: "int-linear-disabled", Provider: integrations.ProviderLinear, Name: "Disabled Linear", Status: integrations.StatusDisabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	post("?workspace_id=ws-handoff", disabled.ID, team.ID, http.StatusBadRequest)
	post("?workspace_id=ws-handoff", integration.ID, "missing-resource", http.StatusNotFound)
	project, err := intRepo.CreateResource(integrations.WithWorkspace(context.Background(), "ws-handoff"), integrations.Resource{
		ID: "not-a-team", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeProject, ExternalID: "project",
	})
	if err != nil {
		t.Fatal(err)
	}
	post("?workspace_id=ws-handoff", integration.ID, project.ID, http.StatusBadRequest)

	first := post("?workspace_id=ws-handoff", integration.ID, team.ID, http.StatusOK)
	// Huma adds its standard $schema annotation; the business response itself
	// has exactly created and link.
	if len(first) != 3 || first["$schema"] == nil || first["created"] == nil || first["link"] == nil {
		t.Fatalf("first response keys = %v, want $schema, created, and link", first)
	}
	var created bool
	if err := json.Unmarshal(first["created"], &created); err != nil || !created {
		t.Fatalf("first created = %s (err %v), want true", first["created"], err)
	}
	var link map[string]json.RawMessage
	if err := json.Unmarshal(first["link"], &link); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"identifier", "url", "state", "tracker_state"} {
		if link[key] == nil {
			t.Fatalf("link keys = %v, missing %q", link, key)
		}
	}
	if len(link) != 4 {
		t.Fatalf("link keys = %v, want exact response projection", link)
	}

	repeat := post("?workspace_id=ws-handoff", integration.ID, team.ID, http.StatusOK)
	var repeated bool
	if err := json.Unmarshal(repeat["created"], &repeated); err != nil || repeated {
		t.Fatalf("repeat created = %s (err %v), want false", repeat["created"], err)
	}
	if string(repeat["link"]) != string(first["link"]) || createCalls != 1 {
		t.Fatalf("repeat link=%s first=%s creates=%d", repeat["link"], first["link"], createCalls)
	}
}

func TestGitLabResourceWebhookRecordsDeliveryFeedbackForMultipleResources(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()

	ctx := workboard.WithWorkspace(context.Background(), "ws-test")
	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(ctx, workboard.Feature{
		ID:     "feature-loyalty",
		Key:    "LOYALTY",
		Name:   "Loyalty",
		Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	cr, err := workBoardRepo.CreateChangeRequest(ctx, workboard.ChangeRequest{
		ID:        "cr-loyalty-v1",
		Key:       "CR-LOYALTY-V1",
		FeatureID: feature.ID,
		WorkType:  workboard.WorkTypeNewFeature,
		Title:     "Build loyalty v1",
	})
	if err != nil {
		t.Fatal(err)
	}

	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID:          "gitlab-main",
		WorkspaceID: "ws-test",
		Provider:    integrations.ProviderGitLab,
		Name:        "Acme GitLab",
		Status:      integrations.StatusConnected,
		BaseURL:     "https://gitlab.acme.io",
	})
	if err != nil {
		t.Fatal(err)
	}
	fe, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID:            "repo-fe",
		IntegrationID: integration.ID,
		ResourceType:  integrations.ResourceTypeProject,
		ExternalID:    "321",
		ExternalKey:   "acme/projects/specgate-fe",
		DisplayName:   "specgate-fe",
		DefaultRef:    "master",
	})
	if err != nil {
		t.Fatal(err)
	}
	be, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID:            "repo-be",
		IntegrationID: integration.ID,
		ResourceType:  integrations.ResourceTypeProject,
		ExternalID:    "654",
		ExternalKey:   "acme/projects/specgate-be",
		DisplayName:   "specgate-be",
		DefaultRef:    "master",
	})
	if err != nil {
		t.Fatal(err)
	}

	opened := postGitLabWebhook(t, srv.URL, integration.ID, map[string]any{
		"object_kind": "merge_request",
		"event_type":  "merge_request",
		"project": map[string]any{
			"id":                  321,
			"path_with_namespace": "acme/projects/specgate-fe",
			"web_url":             "https://gitlab.acme.io/acme/projects/specgate-fe",
		},
		"object_attributes": map[string]any{
			"id":                9001,
			"iid":               42,
			"action":            "open",
			"state":             "opened",
			"title":             "CR-LOYALTY-V1 implement FE checkout points",
			"description":       "Implements SpecGate work item CR-LOYALTY-V1.\n\n<!-- specgate-work-ref: CR-LOYALTY-V1 -->",
			"url":               "https://gitlab.acme.io/acme/projects/specgate-fe/-/merge_requests/42",
			"source_branch":     "specgate/cr-loyalty-v1",
			"target_branch":     "master",
			"target_project_id": 321,
			"last_commit":       map[string]any{"id": "submitted-fe-head"},
		},
	})
	if opened.Status != integrations.WebhookStatusProcessed || opened.ResourceID != fe.ID || opened.ChangeRequestID != cr.ID {
		t.Fatalf("unexpected FE webhook result: %#v", opened)
	}
	if opened.FeedbackEventIDs[0] == "" || opened.DeliveryLinkID == "" {
		t.Fatalf("expected delivery link and feedback ids: %#v", opened)
	}

	merged := postGitLabWebhook(t, srv.URL, integration.ID, map[string]any{
		"object_kind": "merge_request",
		"event_type":  "merge_request",
		"project": map[string]any{
			"id":                  654,
			"path_with_namespace": "acme/projects/specgate-be",
			"web_url":             "https://gitlab.acme.io/acme/projects/specgate-be",
		},
		"object_attributes": map[string]any{
			"id":                9002,
			"iid":               17,
			"action":            "merge",
			"state":             "merged",
			"title":             "CR-LOYALTY-V1 implement BE ledger",
			"description":       "Backend half of CR-LOYALTY-V1.\n\n<!-- specgate-work-ref: CR-LOYALTY-V1 -->",
			"url":               "https://gitlab.acme.io/acme/projects/specgate-be/-/merge_requests/17",
			"source_branch":     "specgate/cr-loyalty-v1",
			"target_branch":     "master",
			"merge_commit_sha":  "abc123",
			"target_project_id": 654,
			"last_commit":       map[string]any{"id": "submitted-be-head"},
		},
	})
	if merged.Status != integrations.WebhookStatusProcessed || merged.ResourceID != be.ID || merged.ChangeRequestID != cr.ID {
		t.Fatalf("unexpected BE webhook result: %#v", merged)
	}

	feedback := getIntegrationJSON[struct {
		Items []integrations.GovernanceFeedbackEvent `json:"items"`
	}](t, srv.URL+"/governance/feedback-events?status=received")
	if len(feedback.Items) != 2 {
		t.Fatalf("expected two feedback events, got %#v", feedback.Items)
	}
	if feedback.Items[0].EventType != integrations.FeedbackEventPRMerged {
		t.Fatalf("newest feedback should be merge, got %#v", feedback.Items[0])
	}
	if feedback.Items[0].FeatureID != feature.ID || feedback.Items[0].ChangeRequestID != cr.ID {
		t.Fatalf("feedback should target governance work item, got %#v", feedback.Items[0])
	}
	// Pin the provider-neutral feedback payload shape (JSON numbers decode as
	// float64 through map[string]any).
	var bePayload map[string]any
	if err := json.Unmarshal([]byte(feedback.Items[0].PayloadJSON), &bePayload); err != nil {
		t.Fatalf("feedback payload is not valid json: %v", err)
	}
	if bePayload["repository"] != "acme/projects/specgate-be" {
		t.Fatalf("feedback payload repository = %v", bePayload["repository"])
	}
	if bePayload["number"] != float64(17) {
		t.Fatalf("feedback payload number = %v, want 17", bePayload["number"])
	}
	for _, providerSpecific := range []string{"project_id", "path_with_namespace", "mr_iid", "mr_url", "mr_title"} {
		if _, ok := bePayload[providerSpecific]; ok {
			t.Fatalf("feedback payload retains provider-specific key %q: %#v", providerSpecific, bePayload)
		}
	}
	if bePayload["head_sha"] != "submitted-be-head" || bePayload["merge_commit_sha"] != "abc123" || bePayload["provider"] != integrations.ProviderGitLab {
		t.Fatalf("feedback payload head_sha/merge_commit_sha/provider = %v/%v/%v", bePayload["head_sha"], bePayload["merge_commit_sha"], bePayload["provider"])
	}
}

func TestIntegrationsAPI_ListChangeRequestDeliveryLinksScopesWorkspace(t *testing.T) {
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()

	createWorkItem := func(workspaceID, featureID, changeRequestID string) {
		t.Helper()
		workCtx := workboard.WithWorkspace(context.Background(), workspaceID)
		workRepo := storagedb.NewWorkBoardRepository(db)
		if _, err := workRepo.CreateFeature(workCtx, workboard.Feature{ID: featureID, Key: featureID, Name: featureID, Status: workboard.FeatureStatusPlanned}); err != nil {
			t.Fatalf("CreateFeature(%s): %v", workspaceID, err)
		}
		if _, err := workRepo.CreateChangeRequest(workCtx, workboard.ChangeRequest{ID: changeRequestID, Key: changeRequestID, FeatureID: featureID, WorkType: workboard.WorkTypeNewFeature, Title: changeRequestID}); err != nil {
			t.Fatalf("CreateChangeRequest(%s): %v", workspaceID, err)
		}
	}

	createDelivery := func(workspaceID, featureID, changeRequestID, integrationID, resourceID, headSHA, mergeCommitSHA string) *integrations.DeliveryLink {
		t.Helper()
		integrationCtx := integrations.WithWorkspace(context.Background(), workspaceID)
		integrationRepo := storagedb.NewIntegrationRepository(db)
		integration, err := integrationRepo.CreateIntegration(integrationCtx, integrations.Integration{ID: integrationID, Provider: integrations.ProviderGitHub, Name: integrationID, Status: integrations.StatusConnected})
		if err != nil {
			t.Fatalf("CreateIntegration(%s): %v", workspaceID, err)
		}
		resource, err := integrationRepo.CreateResource(integrationCtx, integrations.Resource{ID: resourceID, IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeRepo, ExternalKey: integrationID + "/repo"})
		if err != nil {
			t.Fatalf("CreateResource(%s): %v", workspaceID, err)
		}
		link, err := integrationRepo.UpsertDeliveryLink(integrationCtx, integrations.DeliveryLink{
			IntegrationID: integration.ID, ResourceID: resource.ID, FeatureID: featureID, ChangeRequestID: changeRequestID,
			ExternalType: integrations.ExternalTypeMergeRequest, ExternalIID: "42", ExternalKey: integrationID + "#42",
			Title: "Implement " + changeRequestID, URL: "https://provider/" + integrationID + "/42", State: integrations.DeliveryStateMerged,
			SourceBranch: "feature/" + changeRequestID, TargetBranch: "main", HeadSHA: headSHA, MergeCommitSHA: mergeCommitSHA,
		})
		if err != nil {
			t.Fatalf("UpsertDeliveryLink(%s): %v", workspaceID, err)
		}
		return link
	}
	createWorkItem("ws-a", "feature-a", "cr-a")
	createWorkItem("ws-a", "feature-other", "cr-other")
	createWorkItem("ws-b", "feature-b", "cr-b")
	older := createDelivery("ws-a", "feature-a", "cr-a", "integration-a-old", "resource-a-old", "submitted-a-old", "merge-a-old")
	newer := createDelivery("ws-a", "feature-a", "cr-a", "integration-a-new", "resource-a-new", "submitted-a-new", "merge-a-new")
	createDelivery("ws-a", "feature-other", "cr-other", "integration-a-other", "resource-a-other", "submitted-a-other", "merge-a-other")
	createDelivery("ws-b", "feature-b", "cr-b", "integration-b", "resource-b", "submitted-b", "merge-b")
	newerAt := time.Date(2026, time.July, 21, 2, 26, 51, 552045000, time.UTC)
	olderAt := newerAt.Add(-time.Minute)
	for _, update := range []struct {
		id string
		at time.Time
	}{{older.ID, olderAt}, {newer.ID, newerAt}} {
		if err := db.Model(&integrations.DeliveryLink{}).Where("id = ?", update.id).Update("updated_at", update.at).Error; err != nil {
			t.Fatalf("set delivery updated_at: %v", err)
		}
	}

	type deliveryLink struct {
		ExternalKey    string    `json:"external_key"`
		Title          string    `json:"title"`
		URL            string    `json:"url"`
		State          string    `json:"state"`
		SourceBranch   string    `json:"source_branch"`
		TargetBranch   string    `json:"target_branch"`
		HeadSHA        string    `json:"head_sha"`
		MergeCommitSHA string    `json:"merge_commit_sha"`
		UpdatedAt      time.Time `json:"updated_at"`
	}
	read := func(path string) (*http.Response, []byte) {
		t.Helper()
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp, body
	}
	decodeItems := func(body []byte) []map[string]json.RawMessage {
		t.Helper()
		var response struct {
			Items []map[string]json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			t.Fatal(err)
		}
		return response.Items
	}
	decodeLink := func(item map[string]json.RawMessage) deliveryLink {
		t.Helper()
		raw, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		var link deliveryLink
		if err := json.Unmarshal(raw, &link); err != nil {
			t.Fatal(err)
		}
		return link
	}
	expectedKeys := map[string]bool{
		"external_key": true, "title": true, "url": true, "state": true,
		"source_branch": true, "target_branch": true, "head_sha": true,
		"merge_commit_sha": true, "updated_at": true,
	}

	resp, body := read("/workboard/change-requests/cr-a/delivery-links?workspace_id=ws-a")
	items := decodeItems(body)
	if resp.StatusCode != http.StatusOK || len(items) != 2 {
		t.Fatalf("workspace A links status=%d body=%s", resp.StatusCode, body)
	}
	for _, item := range items {
		if len(item) != len(expectedKeys) {
			t.Fatalf("delivery link JSON keys = %v, want %v", item, expectedKeys)
		}
		for key := range item {
			if !expectedKeys[key] {
				t.Fatalf("unexpected delivery link JSON key %q in %v", key, item)
			}
		}
	}
	if first, second := decodeLink(items[0]), decodeLink(items[1]); first.ExternalKey != "integration-a-new#42" || first.HeadSHA != "submitted-a-new" || first.MergeCommitSHA != "merge-a-new" || !first.UpdatedAt.Equal(newerAt) ||
		second.ExternalKey != "integration-a-old#42" || second.HeadSHA != "submitted-a-old" || second.MergeCommitSHA != "merge-a-old" || !second.UpdatedAt.Equal(olderAt) {
		t.Fatalf("delivery links not filtered/newest-first: first=%#v second=%#v", first, second)
	}

	resp, body = read("/workboard/change-requests/cr-b/delivery-links?workspace_id=ws-a")
	if resp.StatusCode != http.StatusOK || len(decodeItems(body)) != 0 {
		t.Fatalf("cross-workspace links status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = read("/workboard/change-requests/unknown/delivery-links?workspace_id=ws-a")
	if resp.StatusCode != http.StatusOK || len(decodeItems(body)) != 0 {
		t.Fatalf("unknown links status=%d body=%s", resp.StatusCode, body)
	}
	resp, _ = read("/workboard/change-requests/cr-a/delivery-links")
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("missing workspace status=%d, want 422", resp.StatusCode)
	}
}

func TestGitLabResourceWebhookUnlinkedMergeRequestCreatesGovernanceFeedback(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()

	ctx := context.Background()
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID:          "gitlab-main",
		WorkspaceID: "ws-test",
		Provider:    integrations.ProviderGitLab,
		Name:        "Acme GitLab",
		Status:      integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	resource, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID:            "repo-fe",
		IntegrationID: integration.ID,
		ResourceType:  integrations.ResourceTypeProject,
		ExternalID:    "321",
		ExternalKey:   "acme/projects/specgate-fe",
		DisplayName:   "specgate-fe",
		DefaultRef:    "master",
	})
	if err != nil {
		t.Fatal(err)
	}

	result := postGitLabWebhook(t, srv.URL, integration.ID, map[string]any{
		"object_kind": "merge_request",
		"event_type":  "merge_request",
		"project": map[string]any{
			"id":                  321,
			"path_with_namespace": "acme/projects/specgate-fe",
		},
		"object_attributes": map[string]any{
			"id":            9003,
			"iid":           43,
			"action":        "open",
			"state":         "opened",
			"title":         "Implement something without a SpecGate work item",
			"url":           "https://gitlab.acme.io/acme/projects/specgate-fe/-/merge_requests/43",
			"source_branch": "feature/no-specgate-work-item",
			"target_branch": "master",
		},
	})
	if result.Status != integrations.WebhookStatusProcessed || result.ResourceID != resource.ID {
		t.Fatalf("unexpected unlinked webhook result: %#v", result)
	}
	if result.ChangeRequestID != "" || result.DeliveryLinkID != "" || result.IgnoredReason != "merge_request_unlinked_to_work_item" {
		t.Fatalf("unlinked MR should not create a delivery link: %#v", result)
	}

	feedback := getIntegrationJSON[struct {
		Items []integrations.GovernanceFeedbackEvent `json:"items"`
	}](t, srv.URL+"/governance/feedback-events?status=received")
	if len(feedback.Items) != 1 || feedback.Items[0].EventType != integrations.FeedbackEventPRUnmatched {
		t.Fatalf("unexpected unmatched feedback: %#v", feedback.Items)
	}
}

func TestGitLabResourceWebhookFeedbackUsesCanonicalStatusVocabulary(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()

	ctx := context.Background()
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID:          "gitlab-canon",
		WorkspaceID: "ws-test",
		Provider:    integrations.ProviderGitLab,
		Name:        "Acme GitLab",
		Status:      integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-canon", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeProject,
		ExternalID: "321", ExternalKey: "acme/projects/specgate-fe", DisplayName: "specgate-fe", DefaultRef: "master",
	}); err != nil {
		t.Fatal(err)
	}

	// An unlinked MR creates one received feedback event.
	postGitLabWebhook(t, srv.URL, integration.ID, map[string]any{
		"object_kind": "merge_request", "event_type": "merge_request",
		"project": map[string]any{"id": 321, "path_with_namespace": "acme/projects/specgate-fe"},
		"object_attributes": map[string]any{
			"id": 9100, "iid": 50, "action": "open", "state": "opened",
			"title": "no work item", "url": "https://gitlab.acme.io/x/-/merge_requests/50",
			"source_branch": "f/x", "target_branch": "master",
		},
	})

	// The lifecycle name `received` is stored and returned directly.
	recv := getIntegrationJSON[struct {
		Items []integrations.GovernanceFeedbackEvent `json:"items"`
	}](t, srv.URL+"/governance/feedback-events?status=received")
	if len(recv.Items) != 1 {
		t.Fatalf("status=received should return the received row, got %#v", recv.Items)
	}
	if recv.Items[0].Status != "received" {
		t.Fatalf("response status = %q, want received", recv.Items[0].Status)
	}
}
