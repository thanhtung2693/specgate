package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/artifact"
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

func doGitLabWebhookEvent(t *testing.T, baseURL string, integrationID string, eventHeader string, signingToken string, eventUUID string, payload map[string]any) *http.Response {
	return doGitLabResourceWebhookEvent(t, baseURL, integrationID, resourceIDForWebhook(t, baseURL, integrationID, payload), eventHeader, signingToken, eventUUID, payload)
}

func TestGitHubResourceWebhookRecordsDeliveryFeedback(t *testing.T) {
	// Not parallel: t.Setenv configures the secret key used to encrypt/decrypt
	// the webhook secret for HMAC verification.
	t.Setenv(integrations.SecretKeyEnvVar, testSecretKey)
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()

	ctx := workboard.WithWorkspace(context.Background(), "ws-test")
	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(ctx, workboard.Feature{
		ID: "feature-gh", Key: "GH", Name: "GitHub feature", Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	cr, err := workBoardRepo.CreateChangeRequest(ctx, workboard.ChangeRequest{
		ID: "cr-gh-1", Key: "CR-GH-1", FeatureID: feature.ID, WorkType: workboard.WorkTypeNewFeature, Title: "GH work",
	})
	if err != nil {
		t.Fatal(err)
	}

	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "github-main", WorkspaceID: "ws-test", Provider: integrations.ProviderGitHub, Name: "GitHub", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-gh", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeRepo,
		ExternalID: "5005", ExternalKey: "acme/specgate", DisplayName: "specgate", DefaultRef: "main",
	})
	if err != nil {
		t.Fatal(err)
	}

	prBody := func(state string, merged bool, mergeSHA string) map[string]any {
		return map[string]any{
			"action": map[bool]string{true: "closed", false: "opened"}[merged],
			"number": 7,
			"pull_request": map[string]any{
				"id": 8001, "number": 7, "title": "Implement CR-GH-1",
				"body": "<!-- specgate-work-ref: CR-GH-1 -->", "html_url": "https://github.com/acme/specgate/pull/7",
				"state": state, "merged": merged, "merge_commit_sha": mergeSHA,
				"head": map[string]any{"ref": "feat/gh"}, "base": map[string]any{"ref": "main"},
			},
			"repository": map[string]any{"id": 5005, "full_name": "acme/specgate"},
		}
	}

	opened := postGitHubWebhook(t, srv.URL, integration.ID, prBody("open", false, ""))
	if opened.Status != integrations.WebhookStatusProcessed || opened.ResourceID != repo.ID || opened.ChangeRequestID != cr.ID {
		t.Fatalf("unexpected opened result: %#v", opened)
	}
	if opened.DeliveryLinkID == "" || len(opened.FeedbackEventIDs) == 0 {
		t.Fatalf("expected delivery link + feedback: %#v", opened)
	}

	merged := postGitHubWebhook(t, srv.URL, integration.ID, prBody("closed", true, "deadbeef"))
	if merged.Status != integrations.WebhookStatusProcessed || merged.ChangeRequestID != cr.ID {
		t.Fatalf("unexpected merged result: %#v", merged)
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
}

func TestGitHubResourceWebhookMatchesWorkOnlyInsideIntegrationWorkspace(t *testing.T) {
	t.Setenv(integrations.SecretKeyEnvVar, testSecretKey)
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()

	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	ownerCtx := workboard.WithWorkspace(context.Background(), "ws-test")
	otherCtx := workboard.WithWorkspace(context.Background(), "ws-other")
	ownerFeature, err := workBoardRepo.CreateFeature(ownerCtx, workboard.Feature{
		ID: "feature-owner", Key: "SHARED", Name: "Owner feature", Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	ownerCR, err := workBoardRepo.CreateChangeRequest(ownerCtx, workboard.ChangeRequest{
		ID: "cr-owner", Key: "CR-SHARED", FeatureID: ownerFeature.ID, Title: "Owner work",
		CreatedAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	otherFeature, err := workBoardRepo.CreateFeature(otherCtx, workboard.Feature{
		ID: "feature-other", Key: "SHARED", Name: "Other feature", Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = workBoardRepo.CreateChangeRequest(otherCtx, workboard.ChangeRequest{
		ID: "cr-other", Key: "CR-SHARED", FeatureID: otherFeature.ID, Title: "Other work",
		CreatedAt: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ownerCtx, integrations.Integration{
		ID: "github-workspace-owner", WorkspaceID: "ws-test", Provider: integrations.ProviderGitHub,
		Name: "GitHub owner", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = intRepo.CreateResource(ownerCtx, integrations.Resource{
		ID: "repo-workspace-owner", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeRepo,
		ExternalID: "9001", ExternalKey: "acme/workspace-owner", DisplayName: "workspace-owner",
	})
	if err != nil {
		t.Fatal(err)
	}

	result := postGitHubWebhook(t, srv.URL, integration.ID, map[string]any{
		"action": "opened",
		"number": 11,
		"pull_request": map[string]any{
			"id": 9011, "number": 11, "title": "Shared work",
			"body": "<!-- specgate-work-ref: CR-SHARED -->", "html_url": "https://github.com/acme/workspace-owner/pull/11",
			"state": "open", "merged": false,
			"head": map[string]any{"ref": "feature/shared"}, "base": map[string]any{"ref": "main"},
		},
		"repository": map[string]any{"id": 9001, "full_name": "acme/workspace-owner"},
	})
	if result.ChangeRequestID != ownerCR.ID || result.FeatureID != ownerFeature.ID {
		t.Fatalf("webhook linked across workspace: result=%#v", result)
	}
}

func TestGitHubResourceWebhookRejectsBadSignature(t *testing.T) {
	t.Setenv(integrations.SecretKeyEnvVar, testSecretKey)
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()
	ctx := context.Background()
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "github-bad", WorkspaceID: "ws-test", Provider: integrations.ProviderGitHub, Name: "GitHub", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-github-bad", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeRepo,
		ExternalID: "1", ExternalKey: "a/b",
	}); err != nil {
		t.Fatal(err)
	}

	raw, _ := json.Marshal(map[string]any{"action": "opened", "repository": map[string]any{"id": 1, "full_name": "a/b"}})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/integrations/"+integration.ID+"/resources/repo-github-bad/github/webhook", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad signature: status %d body=%s", resp.StatusCode, readBody(resp))
	}
}

func TestGitHubResourceWebhookIssueCommentCreatesScopeDriftFeedback(t *testing.T) {
	t.Setenv(integrations.SecretKeyEnvVar, testSecretKey)
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()
	ctx := workboard.WithWorkspace(context.Background(), "ws-test")

	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(ctx, workboard.Feature{
		ID: "feature-gh-comment", Key: "GH-COMMENT", Name: "GitHub Comment", Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	cr, err := workBoardRepo.CreateChangeRequest(ctx, workboard.ChangeRequest{
		ID: "cr-gh-comment-1", Key: "CR-GH-COMMENT-1", FeatureID: feature.ID, WorkType: workboard.WorkTypeFeatureChange, Title: "comment drift",
	})
	if err != nil {
		t.Fatal(err)
	}
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "github-comment", WorkspaceID: "ws-test", Provider: integrations.ProviderGitHub, Name: "GitHub", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-gh-comment", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeRepo,
		ExternalID: "5005", ExternalKey: "acme/specgate", DisplayName: "specgate", DefaultRef: "main",
	}); err != nil {
		t.Fatal(err)
	}

	payload := map[string]any{
		"action": "created",
		"comment": map[string]any{
			"id":       9901,
			"body":     "This changes the acceptance criteria: partial refunds must reverse points proportionally.\n\n<!-- specgate-work-ref: CR-GH-COMMENT-1 -->",
			"html_url": "https://github.com/acme/specgate/issues/9#issuecomment-9901",
			"user":     map[string]any{"login": "alice"},
		},
		"issue": map[string]any{
			"number": 9,
			"title":  "Checkout refunds",
		},
		"repository": map[string]any{"id": 5005, "full_name": "acme/specgate"},
	}

	first := postGitHubWebhookEvent(t, srv.URL, integration.ID, "issue_comment", payload)
	if first.Status != integrations.WebhookStatusProcessed || first.ChangeRequestID != cr.Key {
		t.Fatalf("unexpected result: %#v", first)
	}
	second := postGitHubWebhookEvent(t, srv.URL, integration.ID, "issue_comment", payload)
	if second.IgnoredReason != "duplicate_webhook_event" {
		t.Fatalf("expected duplicate second delivery, got %#v", second)
	}

	feedback := getIntegrationJSON[struct {
		Items []integrations.GovernanceFeedbackEvent `json:"items"`
	}](t, srv.URL+"/governance/feedback-events?status=received")
	if len(feedback.Items) != 1 {
		t.Fatalf("want 1 feedback event, got %#v", feedback.Items)
	}
	ev := feedback.Items[0]
	// The exact work marker resolves to the work item's UUID + feature so the
	// drift comment surfaces on the work item (the UI queries by change_request_id).
	if ev.EventType != integrations.FeedbackEventCommentScopeDrift || ev.ChangeRequestID != cr.ID || ev.FeatureID != feature.ID {
		t.Fatalf("unexpected feedback event: %#v", ev)
	}
	var payloadJSON map[string]any
	if err := json.Unmarshal([]byte(ev.PayloadJSON), &payloadJSON); err != nil {
		t.Fatal(err)
	}
	if payloadJSON["author"] != "alice" || payloadJSON["correlation_id"] != cr.Key {
		t.Fatalf("unexpected payload: %#v", payloadJSON)
	}
}

func TestGitLabResourceWebhookClosedWithoutMergeWarnsWithoutRollback(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()
	ctx := workboard.WithWorkspace(context.Background(), "ws-test")

	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(ctx, workboard.Feature{
		ID: "feature-abandon", Key: "ABN", Name: "Abandon", Status: workboard.FeatureStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	cr, err := workBoardRepo.CreateChangeRequest(ctx, workboard.ChangeRequest{
		ID: "cr-abn-1", Key: "CR-ABN-1", FeatureID: feature.ID, WorkType: workboard.WorkTypeNewFeature, Title: "abandon work",
	})
	if err != nil {
		t.Fatal(err)
	}
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "gitlab-abn", WorkspaceID: "ws-test", Provider: integrations.ProviderGitLab, Name: "GL", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-abn", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeProject,
		ExternalID: "700", ExternalKey: "ns/abn", DefaultRef: "main",
	}); err != nil {
		t.Fatal(err)
	}

	res := postGitLabWebhook(t, srv.URL, integration.ID, map[string]any{
		"object_kind": "merge_request", "event_type": "merge_request",
		"project": map[string]any{"id": 700, "path_with_namespace": "ns/abn"},
		"object_attributes": map[string]any{
			"id": 7100, "iid": 3, "action": "close", "state": "closed",
			"title": "CR-ABN-1 abandoned attempt", "description": "Abandoned attempt.\n\n<!-- specgate-work-ref: CR-ABN-1 -->", "url": "https://gl/ns/abn/-/merge_requests/3",
			"source_branch": "specgate/cr-abn-1", "target_branch": "main", "target_project_id": 700,
		},
	})
	if res.Status != integrations.WebhookStatusProcessed || res.ChangeRequestID != cr.ID {
		t.Fatalf("unexpected result: %#v", res)
	}

	feedback := getIntegrationJSON[struct {
		Items []integrations.GovernanceFeedbackEvent `json:"items"`
	}](t, srv.URL+"/governance/feedback-events?status=received")
	var sawWarning bool
	for _, it := range feedback.Items {
		if it.EventType == integrations.FeedbackEventPRClosed {
			sawWarning = true
		}
	}
	if !sawWarning {
		t.Fatalf("expected a %s review warning, got %#v", integrations.FeedbackEventPRClosed, feedback.Items)
	}

	// A closed-without-merge webhook must NOT roll back or mutate governance
	// state — it only emits a review warning for a human.
	after, err := workBoardRepo.GetFeature(ctx, feature.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != workboard.FeatureStatusActive {
		t.Fatalf("feature status changed to %q — closed webhook must not mutate state", after.Status)
	}
}

func TestGitLabResourceWebhookRecordsCorrelationIDFromWorkMarker(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()
	ctx := workboard.WithWorkspace(context.Background(), "ws-test")

	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(ctx, workboard.Feature{
		ID: "feature-corr", Key: "CORR", Name: "Corr", Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workBoardRepo.CreateChangeRequest(ctx, workboard.ChangeRequest{
		ID: "cr-corr-1", Key: "CR-CORR-1", FeatureID: feature.ID, WorkType: workboard.WorkTypeNewFeature, Title: "corr work",
	}); err != nil {
		t.Fatal(err)
	}
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "gitlab-corr", WorkspaceID: "ws-test", Provider: integrations.ProviderGitLab, Name: "GL", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-corr", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeProject,
		ExternalID: "800", ExternalKey: "ns/corr", DefaultRef: "main",
	}); err != nil {
		t.Fatal(err)
	}

	postGitLabWebhook(t, srv.URL, integration.ID, map[string]any{
		"object_kind": "merge_request", "event_type": "merge_request",
		"project": map[string]any{"id": 800, "path_with_namespace": "ns/corr"},
		"object_attributes": map[string]any{
			"id": 8100, "iid": 4, "action": "open", "state": "opened",
			"title": "Implement checkout", "description": "Work body.\n\n<!-- specgate-work-ref: CR-CORR-1 -->",
			"url":           "https://gl/ns/corr/-/merge_requests/4",
			"source_branch": "feature/checkout", "target_branch": "main", "target_project_id": 800,
		},
	})

	events := getIntegrationJSON[struct {
		Items []integrations.WebhookEvent `json:"items"`
	}](t, srv.URL+"/integrations/"+integration.ID+"/webhook-events")
	if len(events.Items) != 1 {
		t.Fatalf("expected one recorded webhook event, got %#v", events.Items)
	}
	if events.Items[0].CorrelationID != "CR-CORR-1" {
		t.Fatalf("correlation_id = %q, want CR-CORR-1 (from work marker)", events.Items[0].CorrelationID)
	}
	if events.Items[0].PayloadHash == "" {
		t.Fatalf("expected payload_hash to be recorded, got empty")
	}
}

func TestGitLabResourceWebhookNoteCreatesScopeDriftFeedback(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()
	ctx := workboard.WithWorkspace(context.Background(), "ws-test")

	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(ctx, workboard.Feature{
		ID: "feature-note", Key: "NOTE", Name: "Note", Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	cr, err := workBoardRepo.CreateChangeRequest(ctx, workboard.ChangeRequest{
		ID: "cr-note-1", Key: "CR-NOTE-1", FeatureID: feature.ID, WorkType: workboard.WorkTypeFeatureChange, Title: "note drift",
	})
	if err != nil {
		t.Fatal(err)
	}
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "gitlab-note", WorkspaceID: "ws-test", Provider: integrations.ProviderGitLab, Name: "GL", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-note", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeProject,
		ExternalID: "901", ExternalKey: "ns/note", DefaultRef: "main",
	}); err != nil {
		t.Fatal(err)
	}

	res := doGitLabWebhookWithEvent(t, srv.URL, integration.ID, "Note Hook", testGitLabSigningToken, "note-evt-1", map[string]any{
		"object_kind":   "note",
		"event_type":    "note",
		"project":       map[string]any{"id": 901, "path_with_namespace": "ns/note"},
		"user":          map[string]any{"username": "bob"},
		"merge_request": map[string]any{"title": "Refunds"},
		"object_attributes": map[string]any{
			"id":                771,
			"note":              "This changes the acceptance criteria: partial refunds must reverse points proportionally.\n\n<!-- specgate-work-ref: CR-NOTE-1 -->",
			"noteable_type":     "MergeRequest",
			"url":               "https://gitlab.example/ns/note/-/merge_requests/4#note_771",
			"target_project_id": 901,
		},
	})
	if res.Status != integrations.WebhookStatusProcessed || res.ChangeRequestID != cr.Key {
		t.Fatalf("unexpected result: %#v", res)
	}
	feedback := getIntegrationJSON[struct {
		Items []integrations.GovernanceFeedbackEvent `json:"items"`
	}](t, srv.URL+"/governance/feedback-events?status=received")
	if len(feedback.Items) != 1 || feedback.Items[0].EventType != integrations.FeedbackEventCommentScopeDrift {
		t.Fatalf("unexpected feedback events: %#v", feedback.Items)
	}
}

func TestGitLabResourceWebhookRejectsDeliveryForAnotherResource(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()

	ctx := context.Background()
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "gitlab-resource-isolation", WorkspaceID: "ws-test", Provider: integrations.ProviderGitLab,
		Name: "GitLab", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-first", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeProject,
		ExternalID: "100", ExternalKey: "acme/first",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-second", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeProject,
		ExternalID: "200", ExternalKey: "acme/second",
	}); err != nil {
		t.Fatal(err)
	}

	result := postGitLabResourceWebhook(t, srv.URL, integration.ID, first.ID, map[string]any{
		"object_kind": "merge_request",
		"project":     map[string]any{"id": 200, "path_with_namespace": "acme/second"},
		"object_attributes": map[string]any{
			"id": 2, "iid": 2, "action": "open", "state": "opened",
			"title": "wrong resource", "source_branch": "feature/x", "target_branch": "main",
		},
	})
	if result.Status != integrations.WebhookStatusIgnored || result.IgnoredReason != "resource_project_mismatch" {
		t.Fatalf("resource-isolation result = %#v", result)
	}
}

func TestGitLabResourceWebhook_UsesResourceSigningToken(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()
	ctx := workboard.WithWorkspace(context.Background(), "ws-test")

	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(ctx, workboard.Feature{
		ID:     "feature-resource-hook",
		Key:    "RESOURCE-HOOK",
		Name:   "Resource hook",
		Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	cr, err := workBoardRepo.CreateChangeRequest(ctx, workboard.ChangeRequest{
		ID:        "cr-resource-hook",
		Key:       "CR-RESOURCE-HOOK",
		FeatureID: feature.ID,
		WorkType:  workboard.WorkTypeNewFeature,
		Title:     "Resource webhook path",
	})
	if err != nil {
		t.Fatal(err)
	}
	enc, err := integrations.EncryptSecret(testGitLabSigningToken)
	if err != nil {
		t.Fatal(err)
	}
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID:          "gitlab-resource",
		WorkspaceID: "ws-test",
		Provider:    integrations.ProviderGitLab,
		Name:        "Resource GitLab",
		Status:      integrations.StatusConnected,
		BaseURL:     "https://gitlab.example.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	resource, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID:                     "repo-resource",
		IntegrationID:          integration.ID,
		ResourceType:           integrations.ResourceTypeProject,
		ExternalID:             "321",
		ExternalKey:            "acme/web",
		DisplayName:            "Web",
		DefaultRef:             "main",
		WebhookSecretEncrypted: enc,
	})
	if err != nil {
		t.Fatal(err)
	}

	result := postGitLabResourceWebhook(t, srv.URL, integration.ID, resource.ID, map[string]any{
		"object_kind": "merge_request",
		"event_type":  "merge_request",
		"project": map[string]any{
			"id":                  321,
			"path_with_namespace": "acme/web",
			"web_url":             "https://gitlab.example.test/acme/web",
		},
		"object_attributes": map[string]any{
			"id":                9001,
			"iid":               42,
			"action":            "open",
			"state":             "opened",
			"title":             "CR-RESOURCE-HOOK implement webhook",
			"description":       "Implements SpecGate work item CR-RESOURCE-HOOK.\n\n<!-- specgate-work-ref: CR-RESOURCE-HOOK -->",
			"url":               "https://gitlab.example.test/acme/web/-/merge_requests/42",
			"source_branch":     "feat/resource-hook",
			"target_branch":     "main",
			"target_project_id": 321,
		},
	})
	if result.Status != integrations.WebhookStatusProcessed || result.ResourceID != resource.ID || result.ChangeRequestID != cr.ID {
		t.Fatalf("unexpected resource webhook result: %#v", result)
	}
	duplicate := postGitLabResourceWebhook(t, srv.URL, integration.ID, resource.ID, map[string]any{
		"object_kind": "merge_request",
		"event_type":  "merge_request",
		"project": map[string]any{
			"id":                  321,
			"path_with_namespace": "acme/web",
			"web_url":             "https://gitlab.example.test/acme/web",
		},
		"object_attributes": map[string]any{
			"id":                9001,
			"iid":               42,
			"action":            "open",
			"state":             "opened",
			"title":             "CR-RESOURCE-HOOK implement webhook",
			"description":       "Implements SpecGate work item CR-RESOURCE-HOOK.\n\n<!-- specgate-work-ref: CR-RESOURCE-HOOK -->",
			"url":               "https://gitlab.example.test/acme/web/-/merge_requests/42",
			"source_branch":     "feat/resource-hook",
			"target_branch":     "main",
			"target_project_id": 321,
		},
	})
	if duplicate.WebhookEventID != result.WebhookEventID || duplicate.IgnoredReason != "duplicate_webhook_event" {
		t.Fatalf("duplicate resource webhook = %#v, want existing event %q", duplicate, result.WebhookEventID)
	}
}

func TestGitLabResourceWebhookRejectsNonSigningToken(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()
	ctx := context.Background()

	const secretToken = "gl-secret-token-abc123" // not a whsec_ signing token
	enc, err := integrations.EncryptSecret(secretToken)
	if err != nil {
		t.Fatal(err)
	}
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "gitlab-secret-token", WorkspaceID: "ws-test", Provider: integrations.ProviderGitLab,
		Name: "Secret-token GitLab", Status: integrations.StatusConnected, BaseURL: "https://gitlab.example.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	resource, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-secret-token", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeProject,
		ExternalID: "321", ExternalKey: "acme/web", DisplayName: "Web", DefaultRef: "main",
		WebhookSecretEncrypted: enc,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{
		"object_kind": "merge_request",
		"project":     map[string]any{"id": 321, "path_with_namespace": "acme/web"},
	}

	// A persisted non-signing token cannot authenticate a delivery.
	url := srv.URL + "/integrations/" + integration.ID + "/resources/" + resource.ID + "/gitlab/webhook"
	raw, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	req.Header.Set("X-Gitlab-Token", secretToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("non-signing token: want 401, got %d", resp.StatusCode)
	}
}

func TestLinearResourceWebhookCommentCreatesScopeDriftFeedback(t *testing.T) {
	t.Setenv(integrations.SecretKeyEnvVar, testSecretKey)
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()
	ctx := workboard.WithWorkspace(context.Background(), "ws-test")

	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	feature, err := workBoardRepo.CreateFeature(ctx, workboard.Feature{
		ID: "feature-linear-comment", Key: "LIN-COMMENT", Name: "Linear Comment", Status: workboard.FeatureStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}
	cr, err := workBoardRepo.CreateChangeRequest(ctx, workboard.ChangeRequest{
		ID: "cr-linear-comment-1", Key: "CR-LINEAR-COMMENT-1", FeatureID: feature.ID, WorkType: workboard.WorkTypeFeatureChange, Title: "linear comment drift",
	})
	if err != nil {
		t.Fatal(err)
	}
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "linear-comment", WorkspaceID: "ws-test", Provider: integrations.ProviderLinear, Name: "Linear", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "team-linear-comment", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeTeam,
		ExternalID: "team-acme", ExternalKey: "acme",
	}); err != nil {
		t.Fatal(err)
	}

	payload := map[string]any{
		"type":      "Comment",
		"webhookId": "linear-comment-evt-1",
		"data": map[string]any{
			"id":   "comment-1",
			"body": "This changes the acceptance criteria: partial refunds must reverse points proportionally.\n\n<!-- specgate-work-ref: CR-LINEAR-COMMENT-1 -->",
			"url":  "https://linear.app/acme/issue/LOY-1#comment-1",
			"user": map[string]any{"name": "Carol"},
			"issue": map[string]any{
				"id":         "issue-1",
				"identifier": "LOY-1",
				"title":      "Refunds",
				"url":        "https://linear.app/acme/issue/LOY-1",
			},
		},
	}

	res := postLinearWebhook(t, srv.URL, integration.ID, "team-linear-comment", payload)
	if res.Status != integrations.WebhookStatusProcessed || res.CorrelationID != cr.Key {
		t.Fatalf("unexpected result: %#v", res)
	}
	feedback := getIntegrationJSON[struct {
		Items []integrations.GovernanceFeedbackEvent `json:"items"`
	}](t, srv.URL+"/governance/feedback-events?status=received")
	if len(feedback.Items) != 1 || feedback.Items[0].EventType != integrations.FeedbackEventCommentScopeDrift {
		t.Fatalf("unexpected feedback events: %#v", feedback.Items)
	}
}

func TestGitHubResourceWebhookRejectsWhenNoSecretConfigured(t *testing.T) {
	t.Parallel()
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()
	ctx := context.Background()
	intRepo := storagedb.NewIntegrationRepository(db)
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "github-nosecret", WorkspaceID: "ws-test", Provider: integrations.ProviderGitHub, Name: "GitHub", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID: "repo-github-no-secret", IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeRepo,
		ExternalID: "1", ExternalKey: "a/b", ConfigJSON: `{"test_no_webhook_secret":true}`,
	}); err != nil {
		t.Fatal(err)
	}
	// No secret is configured on the resource, so HMAC cannot be
	// verified — the endpoint must refuse rather than become an open relay.
	raw, _ := json.Marshal(map[string]any{"action": "opened", "repository": map[string]any{"id": 1, "full_name": "a/b"}})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/integrations/"+integration.ID+"/resources/repo-github-no-secret/github/webhook", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", "sha256=anything")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("integration with no recoverable secret: status %d body=%s", resp.StatusCode, readBody(resp))
	}
}

func postGitHubWebhook(t *testing.T, baseURL string, integrationID string, payload map[string]any) integrations.GitLabWebhookResult {
	return postGitHubWebhookEvent(t, baseURL, integrationID, "pull_request", payload)
}

func setIntegrationAPIToken(t *testing.T, baseURL string, integrationID string, token string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"api_token": token})
	req, err := http.NewRequest(http.MethodPut, baseURL+"/integrations/"+integrationID+"/api-token?workspace_id=ws-test", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("set token: status %d body=%s", resp.StatusCode, readBody(resp))
	}
}

func postGitHubWebhookEvent(t *testing.T, baseURL string, integrationID string, event string, payload map[string]any) integrations.GitLabWebhookResult {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte(testGitHubWebhookSecret))
	mac.Write(raw)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	req, err := http.NewRequest(http.MethodPost, baseURL+"/integrations/"+integrationID+"/resources/"+resourceIDForWebhook(t, baseURL, integrationID, payload)+"/github/webhook", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-Hub-Signature-256", sig)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
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

func doGitLabWebhookWithEvent(t *testing.T, baseURL string, integrationID string, eventHeader string, signingToken string, eventUUID string, payload map[string]any) integrations.GitLabWebhookResult {
	t.Helper()
	resp := doGitLabWebhookEvent(t, baseURL, integrationID, eventHeader, signingToken, eventUUID, payload)
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

func postGitLabResourceWebhook(t *testing.T, baseURL string, integrationID string, resourceID string, payload map[string]any) integrations.GitLabWebhookResult {
	t.Helper()
	resp := doGitLabResourceWebhookEvent(t, baseURL, integrationID, resourceID, "Merge Request Hook", testGitLabSigningToken, "", payload)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("gitlab resource webhook status %d body=%s", resp.StatusCode, readBody(resp))
	}
	var out integrations.GitLabWebhookResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode gitlab resource webhook response: %v", err)
	}
	return out
}

func doGitLabResourceWebhookEvent(t *testing.T, baseURL string, integrationID string, resourceID string, eventHeader string, signingToken string, eventUUID string, payload map[string]any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/integrations/"+integrationID+"/resources/"+resourceID+"/gitlab/webhook", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gitlab-Event", eventHeader)
	if eventUUID != "" {
		req.Header.Set("X-Gitlab-Event-UUID", eventUUID)
	}
	if signingToken != "" {
		webhookID := "msg-resource-test"
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		req.Header.Set("webhook-id", webhookID)
		req.Header.Set("webhook-timestamp", timestamp)
		req.Header.Set("webhook-signature", signGitLabDelivery(signingToken, webhookID, timestamp, raw))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func postLinearWebhook(t *testing.T, baseURL string, integrationID string, resourceID string, payload map[string]any) integrations.LinearWebhookResult {
	t.Helper()
	if _, ok := payload["webhookTimestamp"]; !ok {
		payload["webhookTimestamp"] = time.Now().UnixMilli()
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte(testWebhookSecret))
	mac.Write(raw)
	sig := hex.EncodeToString(mac.Sum(nil))
	req, err := http.NewRequest(http.MethodPost, baseURL+"/integrations/"+integrationID+"/resources/"+resourceID+"/linear/webhook", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Linear-Signature", sig)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d body=%s", resp.StatusCode, readBody(resp))
	}
	var out integrations.LinearWebhookResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestIntegrationsAPI_CRUDResourcesAndWebhookEvents(t *testing.T) {
	t.Parallel()
	srv := integrationsTestServer(t)
	defer srv.Close()

	created := postIntegrationJSON[integrations.Integration](t, srv.URL+"/integrations", map[string]any{
		"provider":    integrations.ProviderGitLab,
		"name":        "Acme GitLab",
		"base_url":    "https://gitlab.acme.io",
		"config_json": `{"webhook_enabled":true}`,
	})
	if created.ID == "" || created.Status != integrations.StatusConnected {
		t.Fatalf("unexpected created integration: %#v", created)
	}

	list := getIntegrationJSON[struct {
		Items []integrations.Integration `json:"items"`
	}](t, srv.URL+"/integrations?workspace_id=ws-test")
	if len(list.Items) != 1 || list.Items[0].Provider != integrations.ProviderGitLab {
		t.Fatalf("unexpected integration list: %#v", list.Items)
	}

	resource := postIntegrationJSON[integrations.Resource](t, srv.URL+"/integrations/"+created.ID+"/resources", integrations.Resource{
		ResourceType: integrations.ResourceTypeProject,
		ExternalID:   "321",
		ExternalKey:  "acme/projects/specgate-be",
		DisplayName:  "specgate-be",
		DefaultRef:   "master",
	})
	if resource.ID == "" || resource.IntegrationID != created.ID {
		t.Fatalf("unexpected resource: %#v", resource)
	}

	event := postIntegrationJSON[integrations.WebhookEvent](t, srv.URL+"/integrations/"+created.ID+"/webhook-events", integrations.WebhookEvent{
		ResourceID:      resource.ID,
		EventType:       integrations.WebhookEventMergeRequest,
		ExternalEventID: "evt-1",
		PayloadJSON:     `{"object_kind":"merge_request"}`,
	})
	if event.Provider != integrations.ProviderGitLab || event.Status != integrations.WebhookStatusPending {
		t.Fatalf("unexpected webhook event: %#v", event)
	}

	events := getIntegrationJSON[struct {
		Items []integrations.WebhookEvent `json:"items"`
	}](t, srv.URL+"/integrations/"+created.ID+"/webhook-events")
	if len(events.Items) != 1 || events.Items[0].ExternalEventID != "evt-1" {
		t.Fatalf("unexpected webhook event list: %#v", events.Items)
	}
}

func TestBeginIntegrationOAuth_ReturnsAuthorizeURL(t *testing.T) {
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()

	integration, err := storagedb.NewIntegrationRepository(db).CreateIntegration(context.Background(), integrations.Integration{
		ID:          "gitlab-oauth",
		WorkspaceID: "ws-oauth",
		Provider:    integrations.ProviderGitLab,
		Name:        "Acme GitLab",
		Status:      integrations.StatusConnected,
		BaseURL:     "https://gitlab.example/group/project",
	})
	if err != nil {
		t.Fatal(err)
	}

	body := postJSONStatus(t, http.StatusOK, srv.URL+"/integrations/"+integration.ID+"/oauth/authorize?workspace_id=ws-oauth", map[string]any{
		"redirect_target": "/after-auth",
	})
	var out struct {
		AuthorizeURL string `json:"authorize_url"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(out.AuthorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Query().Get("client_id"); got != "gl-client" {
		t.Fatalf("client_id = %q", got)
	}
	if got := u.Query().Get("redirect_uri"); got != "https://specgate.example/integrations/oauth-callback" {
		t.Fatalf("redirect_uri = %q", got)
	}
	if got := u.Query().Get("state"); got == "" {
		t.Fatal("expected non-empty state")
	}
}

func TestCompleteIntegrationOAuth_RedirectsAndPersistsGrant(t *testing.T) {
	t.Setenv(integrations.SecretKeyEnvVar, testSecretKey)
	srv, db := integrationsWebhookTestServer(t)
	defer srv.Close()

	gitlab := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			_ = r.ParseForm()
			if got := r.Form.Get("code"); got != "oauth-code" {
				t.Fatalf("token exchange code = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"gl_access","refresh_token":"gl_refresh","token_type":"Bearer","scope":"api","expires_in":7200,"created_at":1700000000}`)
		case "/api/v4/user":
			if got := r.Header.Get("Authorization"); got != "Bearer gl_access" {
				t.Fatalf("user auth header = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":42,"username":"gitlab-user","name":"GitLab User","email":"gitlab@example.com"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer gitlab.Close()

	baseURL := gitlab.URL + "/group/project"

	integration, err := storagedb.NewIntegrationRepository(db).CreateIntegration(context.Background(), integrations.Integration{
		ID:          "gitlab-callback",
		WorkspaceID: "ws-oauth",
		Provider:    integrations.ProviderGitLab,
		Name:        "Local GitLab",
		Status:      integrations.StatusConnected,
		BaseURL:     baseURL,
	})
	if err != nil {
		t.Fatal(err)
	}

	authRaw := postJSONStatus(t, http.StatusOK, srv.URL+"/integrations/"+integration.ID+"/oauth/authorize?workspace_id=ws-oauth", map[string]any{
		"redirect_target": "/after-auth",
	})
	var authOut struct {
		AuthorizeURL string `json:"authorize_url"`
	}
	if err := json.Unmarshal(authRaw, &authOut); err != nil {
		t.Fatal(err)
	}
	authURL, err := url.Parse(authOut.AuthorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	state := authURL.Query().Get("state")
	if state == "" {
		t.Fatal("expected oauth state")
	}

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(srv.URL + "/integrations/oauth-callback?state=" + url.QueryEscape(state) + "&code=oauth-code")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status %d body=%s", resp.StatusCode, readBody(resp))
	}
	// The callback redirects to the UI origin (AppBaseURL), not the backend it is
	// served from, joined with the app-relative target — otherwise the browser
	// would land on the backend (404 in dev).
	if got := resp.Header.Get("Location"); got != testAppBaseURL+"/after-auth?oauth=connected" {
		t.Fatalf("location = %q, want %s/after-auth?oauth=connected", got, testAppBaseURL)
	}

	got := getIntegrationJSON[integrations.Integration](t, srv.URL+"/integrations/"+integration.ID+"?workspace_id=ws-oauth")
	if got.AuthMethod != integrations.AuthMethodOAuth {
		t.Fatalf("auth_method = %q", got.AuthMethod)
	}
	if !got.HasOAuthToken || got.HasAPIToken {
		t.Fatalf("unexpected token flags: has_oauth=%v has_pat=%v", got.HasOAuthToken, got.HasAPIToken)
	}
	if got.OAuthAccountName != "GitLab User" || got.OAuthAccountEmail != "gitlab@example.com" {
		t.Fatalf("unexpected oauth account: %#v", got)
	}

	postNoContent(t, srv.URL+"/integrations/"+integration.ID+"/oauth/disconnect")
	got = getIntegrationJSON[integrations.Integration](t, srv.URL+"/integrations/"+integration.ID+"?workspace_id=ws-oauth")
	if got.HasOAuthToken || got.AuthMethod != "" {
		t.Fatalf("oauth disconnect should clear grant, got %#v", got)
	}
}

func appendIntegrationWorkspace(url, workspaceID string) string {
	if (!strings.Contains(url, "/integrations/") && !strings.Contains(url, "/governance/feedback-events")) ||
		strings.Contains(url, "/gitlab/webhook") ||
		strings.Contains(url, "/github/webhook") ||
		strings.Contains(url, "/linear/webhook") ||
		strings.Contains(url, "workspace_id=") {
		return url
	}
	separator := "?"
	if strings.Contains(url, "?") {
		separator = "&"
	}
	return url + separator + "workspace_id=" + workspaceID
}

func postIntegrationJSON[T any](t *testing.T, url string, body any) T {
	t.Helper()
	url = appendIntegrationWorkspace(url, "ws-test")
	if strings.HasSuffix(strings.TrimRight(url, "/"), "/integrations") {
		if fields, ok := body.(map[string]any); ok {
			copyFields := make(map[string]any, len(fields)+1)
			for key, value := range fields {
				copyFields[key] = value
			}
			if _, exists := copyFields["workspace_id"]; !exists {
				copyFields["workspace_id"] = "ws-test"
			}
			body = copyFields
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d body=%s", resp.StatusCode, readBody(resp))
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func postNoContent(t *testing.T, url string) {
	t.Helper()
	url = appendIntegrationWorkspace(url, "ws-oauth")
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d body=%s", resp.StatusCode, readBody(resp))
	}
}

func postJSONStatus(t *testing.T, wantStatus int, url string, body any) []byte {
	t.Helper()
	return requestJSONStatus(t, http.MethodPost, wantStatus, url, body)
}

func requestJSONStatus(t *testing.T, method string, wantStatus int, url string, body any) []byte {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	gotBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("status %d body=%s", resp.StatusCode, string(gotBody))
	}
	return gotBody
}

func readBody(resp *http.Response) string {
	raw, _ := io.ReadAll(resp.Body)
	return string(raw)
}

func getIntegrationJSON[T any](t *testing.T, url string) T {
	t.Helper()
	url = appendIntegrationWorkspace(url, "ws-test")
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d body=%s", resp.StatusCode, readBody(resp))
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}
