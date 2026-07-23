package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/integrations"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
)

// TestMain sets the encryption key process-wide so encrypt/decrypt of stored
// webhook secrets works in parallel webhook tests (t.Setenv is disallowed once
// t.Parallel is called). Individual tests may still t.Setenv the same value.
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
