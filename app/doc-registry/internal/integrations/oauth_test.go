package integrations

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

func TestOAuthAPIGetJSONRejectsOversizedResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", (1<<20)+1))
	}))
	t.Cleanup(srv.Close)

	var out map[string]any
	err := oauthAPIGetJSON(context.Background(), srv.URL, "token", &out)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oauthAPIGetJSON() error = %v, want response limit", err)
	}
}

type oauthResolveFakeStore struct {
	Store
	integration        *Integration
	resources          []Resource
	updatedGrant       *Integration
	updatedErrorRow    *Integration
	createdIntegration *Integration
	clearedOAuthID     string
	clearedConfigs     map[string]string
	clearedSecrets     []string
	configErr          error
	oauthStates        map[string]OAuthState
}

func (f *oauthResolveFakeStore) WithTx(_ context.Context, fn func(Store) error) error {
	return fn(f)
}

func (f *oauthResolveFakeStore) CreateIntegration(_ context.Context, in Integration) (*Integration, error) {
	row := in
	if row.ID == "" {
		row.ID = "created-int-id"
	}
	f.createdIntegration = &row
	return &row, nil
}

func (f *oauthResolveFakeStore) GetIntegration(context.Context, string) (*Integration, error) {
	if f.integration == nil {
		return nil, ErrNotFound
	}
	row := *f.integration
	return &row, nil
}

func (f *oauthResolveFakeStore) UpdateOAuthGrant(_ context.Context, in Integration) error {
	row := in
	f.updatedGrant = &row
	if f.integration != nil {
		f.integration.AuthMethod = in.AuthMethod
		f.integration.OAuthAccessTokenEncrypted = in.OAuthAccessTokenEncrypted
		f.integration.OAuthRefreshTokenEncrypted = in.OAuthRefreshTokenEncrypted
		f.integration.OAuthExpiresAt = in.OAuthExpiresAt
		f.integration.OAuthTokenType = in.OAuthTokenType
		f.integration.OAuthScope = in.OAuthScope
		f.integration.OAuthAccountID = in.OAuthAccountID
		f.integration.OAuthAccountName = in.OAuthAccountName
		f.integration.OAuthAccountEmail = in.OAuthAccountEmail
		f.integration.OAuthHostKey = in.OAuthHostKey
	}
	return nil
}

func (f *oauthResolveFakeStore) UpdateIntegration(_ context.Context, in Integration) (*Integration, error) {
	row := in
	f.updatedErrorRow = &row
	if f.integration != nil {
		if in.Status != "" {
			f.integration.Status = in.Status
		}
		f.integration.LastError = in.LastError
	}
	return f.integration, nil
}

func (f *oauthResolveFakeStore) ClearOAuthGrant(_ context.Context, id string) error {
	f.clearedOAuthID = id
	if f.integration != nil {
		f.integration.AuthMethod = ""
		f.integration.OAuthAccessTokenEncrypted = ""
		f.integration.OAuthRefreshTokenEncrypted = ""
	}
	return nil
}

func (f *oauthResolveFakeStore) ListResources(context.Context, string) ([]Resource, error) {
	return append([]Resource(nil), f.resources...), nil
}

func (f *oauthResolveFakeStore) UpdateResourceConfigJSON(_ context.Context, _ string, resourceID, configJSON string) error {
	if f.configErr != nil {
		return f.configErr
	}
	if f.clearedConfigs == nil {
		f.clearedConfigs = map[string]string{}
	}
	f.clearedConfigs[resourceID] = configJSON
	return nil
}

func TestDisconnectOAuth_RetriesAfterRemoteSuccessAndLocalFailure(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	store := &oauthResolveFakeStore{
		integration: &Integration{
			ID: "int-disconnect", Provider: ProviderLinear, Status: StatusConnected,
			AuthMethod: AuthMethodOAuth, HasOAuthToken: true, OAuthAccessTokenEncrypted: encryptedSecretForTest(t, "oauth-token"),
		},
		resources: []Resource{{ID: "resource-1", IntegrationID: "int-disconnect", ResourceType: ResourceTypeTeam, ExternalID: "team-1", ConfigJSON: `{"provider_webhook_id":"hook-1"}`}},
		configErr: errors.New("temporary database failure"),
	}
	driver := &webhookRollbackDriver{}
	previous, ok := coretypes.LookupWebhookDriver(ProviderLinear)
	if !ok {
		t.Fatal("missing Linear webhook driver")
	}
	coretypes.RegisterWebhookDriver(ProviderLinear, driver)
	t.Cleanup(func() { coretypes.RegisterWebhookDriver(ProviderLinear, previous) })
	svc := NewService(store)

	if err := svc.DisconnectOAuth(context.Background(), "int-disconnect"); err == nil {
		t.Fatal("first disconnect should fail on local cleanup")
	}
	if store.clearedOAuthID != "" {
		t.Fatal("OAuth grant cleared after local cleanup failure")
	}
	store.configErr = nil
	if err := svc.DisconnectOAuth(context.Background(), "int-disconnect"); err != nil {
		t.Fatalf("retry DisconnectOAuth: %v", err)
	}
	if len(driver.deleted) != 2 || store.clearedOAuthID != "int-disconnect" {
		t.Fatalf("remote deletes=%d cleared=%q, want retry then grant cleanup", len(driver.deleted), store.clearedOAuthID)
	}
}

func (f *oauthResolveFakeStore) UpdateResourceWebhookSecretEncrypted(_ context.Context, _, resourceID, encrypted string) error {
	if encrypted == "" {
		f.clearedSecrets = append(f.clearedSecrets, resourceID)
	}
	return nil
}

func (f *oauthResolveFakeStore) CreateOAuthState(_ context.Context, in OAuthState) (*OAuthState, error) {
	if f.oauthStates == nil {
		f.oauthStates = map[string]OAuthState{}
	}
	row := in
	if row.ID == "" {
		row.ID = "state-id"
	}
	f.oauthStates[row.State] = row
	return &row, nil
}

func (f *oauthResolveFakeStore) GetOAuthState(_ context.Context, state string) (*OAuthState, error) {
	row, ok := f.oauthStates[state]
	if !ok {
		return nil, ErrNotFound
	}
	cp := row
	return &cp, nil
}

func (f *oauthResolveFakeStore) ConsumeOAuthState(_ context.Context, state string) (*OAuthState, error) {
	row, ok := f.oauthStates[state]
	if !ok {
		return nil, ErrNotFound
	}
	now := time.Now().UTC()
	row.ConsumedAt = &now
	f.oauthStates[state] = row
	cp := row
	return &cp, nil
}

func encryptedSecretForTest(t *testing.T, value string) string {
	t.Helper()
	enc, err := EncryptSecret(value)
	if err != nil {
		t.Fatalf("EncryptSecret(%q): %v", value, err)
	}
	return enc
}

func TestResolveAPIToken_PATIntegrationStillWorks(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	store := &oauthResolveFakeStore{
		integration: &Integration{
			ID:                "int-pat",
			Provider:          ProviderGitHub,
			Status:            StatusConnected,
			APITokenEncrypted: encryptedSecretForTest(t, "github-pat-test-token"),
		},
	}
	svc := NewService(store)

	token, err := svc.ResolveAPIToken(context.Background(), "int-pat")
	if err != nil {
		t.Fatal(err)
	}
	if token != "github-pat-test-token" {
		t.Fatalf("token = %q, want github-pat-test-token", token)
	}
}

func TestResolveAPIToken_OAuthIntegrationUsesStoredAccessToken(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	expiresAt := time.Now().UTC().Add(time.Hour)
	store := &oauthResolveFakeStore{
		integration: &Integration{
			ID:                         "int-oauth",
			Provider:                   ProviderGitLab,
			Status:                     StatusConnected,
			AuthMethod:                 AuthMethodOAuth,
			OAuthAccessTokenEncrypted:  encryptedSecretForTest(t, "gl_oauth_access"),
			OAuthRefreshTokenEncrypted: encryptedSecretForTest(t, "gl_oauth_refresh"),
			OAuthExpiresAt:             &expiresAt,
		},
	}
	svc := NewService(store)

	token, err := svc.ResolveAPIToken(context.Background(), "int-oauth")
	if err != nil {
		t.Fatal(err)
	}
	if token != "gl_oauth_access" {
		t.Fatalf("token = %q, want gl_oauth_access", token)
	}
	if store.updatedGrant != nil {
		t.Fatalf("stored-access path should not update grant, got %#v", store.updatedGrant)
	}
}

func TestResolveAPIToken_OAuthRefreshesExpiredToken(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			http.NotFound(w, r)
			return
		}
		_ = r.ParseForm()
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", got)
		}
		if got := r.Form.Get("refresh_token"); got != "gl_refresh_token" {
			t.Errorf("refresh_token = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"gl_new_access","refresh_token":"gl_new_refresh","token_type":"Bearer","scope":"api","expires_in":7200}`)
	}))
	defer srv.Close()

	expiredAt := time.Now().UTC().Add(-time.Minute)
	store := &oauthResolveFakeStore{
		integration: &Integration{
			ID:                         "int-refresh",
			Provider:                   ProviderGitLab,
			Status:                     StatusConnected,
			AuthMethod:                 AuthMethodOAuth,
			BaseURL:                    srv.URL,
			OAuthAccessTokenEncrypted:  encryptedSecretForTest(t, "gl_old_access"),
			OAuthRefreshTokenEncrypted: encryptedSecretForTest(t, "gl_refresh_token"),
			OAuthExpiresAt:             &expiredAt,
			OAuthHostKey:               "gitlab.gitlab_com",
		},
	}
	svc := NewService(store).WithOAuthAppLookup(func(context.Context, string, string) (*OAuthAppConfig, error) {
		return &OAuthAppConfig{Provider: ProviderGitLab, HostKey: "gitlab.gitlab_com", ClientID: "gl-client", ClientSecret: "gl-secret", Scopes: []string{"api"}}, nil
	})

	token, err := svc.ResolveAPIToken(context.Background(), "int-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if token != "gl_new_access" {
		t.Fatalf("token = %q, want gl_new_access", token)
	}
	if store.updatedGrant == nil {
		t.Fatal("expected refreshed oauth grant to be persisted")
	}
	if store.updatedGrant.AuthMethod != AuthMethodOAuth {
		t.Fatalf("auth method = %q, want oauth", store.updatedGrant.AuthMethod)
	}
	if store.updatedErrorRow == nil || store.updatedErrorRow.Status != StatusConnected {
		t.Fatalf("expected integration status reset to connected, got %#v", store.updatedErrorRow)
	}
}

func TestResolveAPIToken_OAuthRefreshFailureMarksIntegrationError(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
	}))
	defer srv.Close()

	expiredAt := time.Now().UTC().Add(-time.Minute)
	store := &oauthResolveFakeStore{
		integration: &Integration{
			ID:                         "int-refresh-fail",
			Provider:                   ProviderGitLab,
			Status:                     StatusConnected,
			AuthMethod:                 AuthMethodOAuth,
			BaseURL:                    srv.URL,
			OAuthAccessTokenEncrypted:  encryptedSecretForTest(t, "gl_old_access"),
			OAuthRefreshTokenEncrypted: encryptedSecretForTest(t, "gl_refresh_token"),
			OAuthExpiresAt:             &expiredAt,
			OAuthHostKey:               "gitlab.gitlab_com",
		},
	}
	svc := NewService(store).WithOAuthAppLookup(func(context.Context, string, string) (*OAuthAppConfig, error) {
		return &OAuthAppConfig{Provider: ProviderGitLab, HostKey: "gitlab.gitlab_com", ClientID: "gl-client", ClientSecret: "gl-secret", Scopes: []string{"api"}}, nil
	})

	if _, err := svc.ResolveAPIToken(context.Background(), "int-refresh-fail"); err == nil {
		t.Fatal("expected oauth refresh failure error")
	}
	if store.updatedErrorRow == nil {
		t.Fatal("expected integration error status update")
	}
	if store.updatedErrorRow.Status != StatusError {
		t.Fatalf("status = %q, want error", store.updatedErrorRow.Status)
	}
	if store.updatedErrorRow.LastError == "" {
		t.Fatal("expected last_error to be recorded")
	}
}

func TestBeginOAuthConnect_CreatesStateAndReturnsAuthorizeURL(t *testing.T) {
	store := &oauthResolveFakeStore{
		integration: &Integration{
			ID:       "int-auth",
			Provider: ProviderGitLab,
			Status:   StatusConnected,
			BaseURL:  "https://gitlab.example/group/project",
		},
	}
	svc := NewService(store).WithOAuthAppLookup(func(context.Context, string, string) (*OAuthAppConfig, error) {
		return &OAuthAppConfig{Provider: ProviderGitLab, HostKey: "gitlab.gitlab_example", ClientID: "gl-client", ClientSecret: "gl-secret", Scopes: []string{"api"}}, nil
	})

	authorizeURL, err := svc.BeginOAuthConnect(context.Background(), "int-auth", "https://callback.example", "/?settings=integrations")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "gitlab.example" || u.Path != "/oauth/authorize" {
		t.Fatalf("authorize URL = %q", authorizeURL)
	}
	q := u.Query()
	if q.Get("client_id") != "gl-client" {
		t.Fatalf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != "https://callback.example/integrations/oauth-callback" {
		t.Fatalf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Fatalf("expected PKCE S256 challenge, got method=%q challenge=%q", q.Get("code_challenge_method"), q.Get("code_challenge"))
	}
	state := q.Get("state")
	if state == "" {
		t.Fatal("expected non-empty oauth state")
	}
	stored, ok := store.oauthStates[state]
	if !ok || stored.IntegrationID != "int-auth" || stored.Provider != ProviderGitLab {
		t.Fatalf("unexpected stored oauth state: %#v", stored)
	}
	if stored.CodeVerifier == "" {
		t.Fatal("expected PKCE code_verifier stored on the state row")
	}
}

func TestCompleteOAuthCallback_PersistsGrantAndConsumesState(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	prevOAuthBase := gitHubOAuthBaseURL
	prevAPIBase := gitHubAPIBaseURL
	t.Cleanup(func() {
		gitHubOAuthBaseURL = prevOAuthBase
		gitHubAPIBaseURL = prevAPIBase
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			_ = r.ParseForm()
			if got := r.Form.Get("code"); got != "oauth-code" {
				t.Errorf("code = %q", got)
			}
			if r.Form.Get("code_verifier") == "" {
				t.Error("expected PKCE code_verifier in the token exchange")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"gh_access","scope":"repo read:user","token_type":"bearer"}`)
		case "/user":
			if got := r.Header.Get("Authorization"); got != "Bearer gh_access" {
				t.Errorf("Authorization = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":42,"login":"octocat","name":"The Octocat","email":"octocat@example.com"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	gitHubOAuthBaseURL = srv.URL
	gitHubAPIBaseURL = srv.URL

	store := &oauthResolveFakeStore{
		integration: &Integration{
			ID:          "int-callback",
			WorkspaceID: "ws-a",
			Provider:    ProviderGitHub,
			Status:      StatusConnected,
			BaseURL:     "https://github.com",
		},
		oauthStates: map[string]OAuthState{
			"state-token": {
				ID:             "state-id",
				State:          "state-token",
				WorkspaceID:    "ws-a",
				IntegrationID:  "int-callback",
				Provider:       ProviderGitHub,
				HostKey:        "github.github_com",
				RedirectTarget: "/?settings=integrations",
				CodeVerifier:   "verifier-abcdefghijklmnopqrstuvwxyz0123456789",
				ExpiresAt:      time.Now().UTC().Add(time.Minute),
			},
		},
	}
	svc := NewService(store).WithOAuthAppLookup(func(context.Context, string, string) (*OAuthAppConfig, error) {
		return &OAuthAppConfig{Provider: ProviderGitHub, HostKey: "github.github_com", ClientID: "gh-client", ClientSecret: "gh-secret", Scopes: []string{"repo", "read:user"}}, nil
	})

	result, err := svc.CompleteOAuthCallback(context.Background(), "state-token", "oauth-code", "https://callback.example")
	if err != nil {
		t.Fatal(err)
	}
	if result.IntegrationID != "int-callback" || result.RedirectTarget != "/?settings=integrations" {
		t.Fatalf("unexpected callback result: %#v", result)
	}
	if store.updatedGrant == nil {
		t.Fatal("expected OAuth grant to be persisted")
	}
	if store.updatedGrant.OAuthAccountName != "The Octocat" {
		t.Fatalf("account name = %q, want The Octocat", store.updatedGrant.OAuthAccountName)
	}
	if store.updatedErrorRow == nil || store.updatedErrorRow.Status != StatusConnected {
		t.Fatalf("expected connected status update, got %#v", store.updatedErrorRow)
	}
	if store.oauthStates["state-token"].ConsumedAt == nil {
		t.Fatal("expected oauth state to be consumed")
	}
}

func TestBeginPendingOAuthConnect_StoresPendingSpec(t *testing.T) {
	store := &oauthResolveFakeStore{}
	svc := NewService(store).WithOAuthAppLookup(func(context.Context, string, string) (*OAuthAppConfig, error) {
		return &OAuthAppConfig{Provider: ProviderGitLab, HostKey: "gitlab.gitlab_example", ClientID: "gl-client", ClientSecret: "gl-secret", Scopes: []string{"api"}}, nil
	})

	authorizeURL, err := svc.BeginPendingOAuthConnect(WithWorkspace(context.Background(), "ws-a"), PendingOAuthSpec{
		Provider:   ProviderGitLab,
		Name:       "Acme GitLab",
		BaseURL:    "https://gitlab.example",
		ConfigJSON: `{"enabled":true}`,
	}, "https://callback.example", "/?settings=integrations")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("code_challenge") == "" {
		t.Fatal("expected PKCE challenge on pending authorize URL")
	}
	state := u.Query().Get("state")
	stored, ok := store.oauthStates[state]
	if !ok {
		t.Fatalf("state %q not stored", state)
	}
	if stored.IntegrationID != "" {
		t.Fatalf("pending state must have empty integration_id, got %q", stored.IntegrationID)
	}
	if stored.WorkspaceID != "ws-a" {
		t.Fatalf("pending state workspace_id = %q, want ws-a", stored.WorkspaceID)
	}
	if stored.PendingName != "Acme GitLab" || stored.PendingBaseURL != "https://gitlab.example" || stored.CodeVerifier == "" {
		t.Fatalf("unexpected pending state: %#v", stored)
	}
}

func TestBeginPendingOAuthConnect_RejectsOAuthAppForDifferentHost(t *testing.T) {
	store := &oauthResolveFakeStore{}
	svc := NewService(store).WithOAuthAppLookup(func(context.Context, string, string) (*OAuthAppConfig, error) {
		return &OAuthAppConfig{Provider: ProviderGitLab, HostKey: "gitlab.gitlab_com", ClientID: "gl-client", ClientSecret: "gl-secret"}, nil
	})

	_, err := svc.BeginPendingOAuthConnect(WithWorkspace(context.Background(), "ws-a"), PendingOAuthSpec{
		Provider: ProviderGitLab,
		Name:     "Untrusted GitLab",
		BaseURL:  "https://attacker.example",
	}, "https://callback.example", "/")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("BeginPendingOAuthConnect error = %v, want ErrValidation", err)
	}
}

func TestBeginPendingOAuthConnect_RequiresWorkspace(t *testing.T) {
	t.Parallel()
	store := &oauthResolveFakeStore{}
	svc := NewService(store).WithOAuthAppLookup(func(context.Context, string, string) (*OAuthAppConfig, error) {
		return &OAuthAppConfig{Provider: ProviderGitLab, HostKey: "gitlab.gitlab_example", ClientID: "gl-client", ClientSecret: "gl-secret"}, nil
	})

	_, err := svc.BeginPendingOAuthConnect(context.Background(), PendingOAuthSpec{
		Provider: ProviderGitLab,
		Name:     "Acme GitLab",
	}, "https://callback.example", "/?settings=integrations")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("BeginPendingOAuthConnect error = %v, want ErrValidation", err)
	}
	if len(store.oauthStates) != 0 {
		t.Fatal("unscoped OAuth begin created state")
	}
}

func TestCompleteOAuthCallback_RequiresConsistentWorkspaceOwnership(t *testing.T) {
	t.Parallel()

	appLookup := func(context.Context, string, string) (*OAuthAppConfig, error) {
		return &OAuthAppConfig{}, nil
	}

	t.Run("state must have owner", func(t *testing.T) {
		t.Parallel()

		store := &oauthResolveFakeStore{
			oauthStates: map[string]OAuthState{
				"state-token": {
					State:     "state-token",
					Provider:  ProviderGitHub,
					ExpiresAt: time.Now().UTC().Add(time.Minute),
				},
			},
		}
		svc := NewService(store).WithOAuthAppLookup(appLookup)

		_, err := svc.CompleteOAuthCallback(context.Background(), "state-token", "code", "https://callback.example")
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("error = %v, want ErrValidation", err)
		}
	})

	t.Run("reconnect integration must share state owner", func(t *testing.T) {
		t.Parallel()

		store := &oauthResolveFakeStore{
			integration: &Integration{
				ID:          "int-callback",
				WorkspaceID: "ws-other",
				Provider:    ProviderGitHub,
			},
			oauthStates: map[string]OAuthState{
				"state-token": {
					State:         "state-token",
					WorkspaceID:   "ws-owner",
					IntegrationID: "int-callback",
					Provider:      ProviderGitHub,
					ExpiresAt:     time.Now().UTC().Add(time.Minute),
				},
			},
		}
		svc := NewService(store).WithOAuthAppLookup(appLookup)

		_, err := svc.CompleteOAuthCallback(context.Background(), "state-token", "code", "https://callback.example")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("error = %v, want ErrNotFound", err)
		}
	})
}

func TestCompleteOAuthCallback_PendingCreatesIntegration(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"gl_access","refresh_token":"gl_refresh","token_type":"Bearer","scope":"api","expires_in":7200}`)
		case "/api/v4/user":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":7,"username":"gitlab-user","name":"GitLab User","email":"gitlab@example.com"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	store := &oauthResolveFakeStore{
		oauthStates: map[string]OAuthState{
			"st": {
				ID: "sid", State: "st", IntegrationID: "", Provider: ProviderGitLab,
				WorkspaceID: "ws-a",
				HostKey:     "gitlab.gitlab_com", RedirectTarget: "/?settings=integrations",
				CodeVerifier: "verifier-abcdefghijklmnopqrstuvwxyz0123456789",
				PendingName:  "Acme GitLab", PendingBaseURL: srv.URL, PendingConfigJSON: `{"enabled":true}`,
				ExpiresAt: time.Now().UTC().Add(time.Minute),
			},
		},
	}
	svc := NewService(store).WithOAuthAppLookup(func(context.Context, string, string) (*OAuthAppConfig, error) {
		return &OAuthAppConfig{Provider: ProviderGitLab, HostKey: "gitlab.gitlab_com", ClientID: "gl-client", ClientSecret: "gl-secret", Scopes: []string{"api"}}, nil
	})

	result, err := svc.CompleteOAuthCallback(context.Background(), "st", "oauth-code", "https://callback.example")
	if err != nil {
		t.Fatal(err)
	}
	if store.createdIntegration == nil {
		t.Fatal("expected a new integration to be created on callback")
	}
	created := store.createdIntegration
	if created.Name != "Acme GitLab" || created.Provider != ProviderGitLab {
		t.Fatalf("unexpected created integration: %#v", created)
	}
	if created.AuthMethod != AuthMethodOAuth || created.Status != StatusConnected {
		t.Fatalf("created integration not connected/oauth: %#v", created)
	}
	if created.WorkspaceID != "ws-a" {
		t.Fatalf("created integration workspace_id = %q, want ws-a", created.WorkspaceID)
	}
	if created.OAuthAccessTokenEncrypted == "" || created.OAuthAccountName != "GitLab User" {
		t.Fatalf("created integration missing grant: %#v", created)
	}
	if result.IntegrationID != created.ID {
		t.Fatalf("result id = %q, want %q", result.IntegrationID, created.ID)
	}
	if store.oauthStates["st"].ConsumedAt == nil {
		t.Fatal("expected oauth state to be consumed")
	}
}

func TestDisconnectOAuth_ClearsGrant(t *testing.T) {
	store := &oauthResolveFakeStore{integration: &Integration{ID: "int-disconnect"}}
	svc := NewService(store)

	if err := svc.DisconnectOAuth(context.Background(), "int-disconnect"); err != nil {
		t.Fatal(err)
	}
	if store.clearedOAuthID != "int-disconnect" {
		t.Fatalf("cleared oauth id = %q, want int-disconnect", store.clearedOAuthID)
	}
}

func TestDisconnectOAuth_RemovesManagedWebhooksBeforeClearingGrant(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	store := &oauthResolveFakeStore{
		integration: &Integration{
			ID: "int-disconnect", Provider: ProviderGitHub, Status: StatusConnected,
			AuthMethod: AuthMethodOAuth, HasOAuthToken: true, OAuthAccessTokenEncrypted: encryptedSecretForTest(t, "oauth-token"),
		},
		resources: []Resource{{
			ID: "resource-1", IntegrationID: "int-disconnect", ResourceType: ResourceTypeRepo,
			ExternalKey: "owner/repo", ConfigJSON: `{"provider_webhook_id":"hook-1","webhook_status":"connected","keep":"value"}`,
			WebhookSecretEncrypted: "encrypted-secret",
		}},
	}
	driver := &webhookRollbackDriver{onDelete: func() {
		if store.clearedOAuthID != "" {
			t.Error("OAuth grant cleared before managed webhook deletion")
		}
	}}
	previous, ok := coretypes.LookupWebhookDriver(ProviderGitHub)
	if !ok {
		t.Fatal("missing GitHub webhook driver")
	}
	coretypes.RegisterWebhookDriver(ProviderGitHub, driver)
	t.Cleanup(func() { coretypes.RegisterWebhookDriver(ProviderGitHub, previous) })

	if err := NewService(store).DisconnectOAuth(context.Background(), "int-disconnect"); err != nil {
		t.Fatalf("DisconnectOAuth: %v", err)
	}
	if len(driver.deleted) != 1 || driver.deleted[0].hookID != "hook-1" {
		t.Fatalf("remote deletes = %#v, want hook-1", driver.deleted)
	}
	if store.clearedOAuthID != "int-disconnect" {
		t.Fatalf("cleared oauth id = %q", store.clearedOAuthID)
	}
	if got := store.clearedConfigs["resource-1"]; got != `{"keep":"value"}` {
		t.Fatalf("cleaned config = %s, want unrelated config preserved", got)
	}
	if len(store.clearedSecrets) != 1 || store.clearedSecrets[0] != "resource-1" {
		t.Fatalf("cleared secrets = %#v", store.clearedSecrets)
	}
}

func TestDisconnectOAuth_PreservesGrantWhenManagedWebhookDeleteFails(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	store := &oauthResolveFakeStore{
		integration: &Integration{
			ID: "int-disconnect", Provider: ProviderGitHub, Status: StatusConnected,
			AuthMethod: AuthMethodOAuth, HasOAuthToken: true, OAuthAccessTokenEncrypted: encryptedSecretForTest(t, "oauth-token"),
		},
		resources: []Resource{{ID: "resource-1", IntegrationID: "int-disconnect", ResourceType: ResourceTypeRepo, ExternalKey: "owner/repo", ConfigJSON: `{"provider_webhook_id":"hook-1"}`}},
	}
	driver := &webhookRollbackDriver{deleteErr: errors.New("provider unavailable")}
	previous, ok := coretypes.LookupWebhookDriver(ProviderGitHub)
	if !ok {
		t.Fatal("missing GitHub webhook driver")
	}
	coretypes.RegisterWebhookDriver(ProviderGitHub, driver)
	t.Cleanup(func() { coretypes.RegisterWebhookDriver(ProviderGitHub, previous) })

	err := NewService(store).DisconnectOAuth(context.Background(), "int-disconnect")
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("DisconnectOAuth error = %v, want ErrUpstream", err)
	}
	if store.clearedOAuthID != "" {
		t.Fatal("OAuth grant cleared despite provider cleanup failure")
	}
}

func TestBeginOAuthConnect_RejectsExternalRedirectTarget(t *testing.T) {
	store := &oauthResolveFakeStore{
		integration: &Integration{
			ID:       "int-auth",
			Provider: ProviderGitLab,
			Status:   StatusConnected,
			BaseURL:  "https://gitlab.example/group/project",
		},
	}
	svc := NewService(store).WithOAuthAppLookup(func(context.Context, string, string) (*OAuthAppConfig, error) {
		return &OAuthAppConfig{Provider: ProviderGitLab, HostKey: "gitlab.gitlab_example", ClientID: "gl-client", ClientSecret: "gl-secret", Scopes: []string{"api"}}, nil
	})

	_, err := svc.BeginOAuthConnect(context.Background(), "int-auth", "https://callback.example", "https://evil.example")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want validation", err)
	}
}

func TestNormalizeOAuthRedirectTarget(t *testing.T) {
	t.Parallel()
	// Off-site forms must be rejected. "/\\evil" and "/\\/evil" are the open-redirect
	// vector: browsers normalize "\" to "/", so the Location resolves protocol-relative.
	rejected := []string{
		"//evil.example",
		"/\\evil.example",
		"/\\/evil.example",
		"https://evil.example",
		"\\\\evil.example",
		"javascript:alert(1)",
	}
	for _, target := range rejected {
		if _, err := normalizeOAuthRedirectTarget(target); !errors.Is(err, ErrValidation) {
			t.Errorf("normalizeOAuthRedirectTarget(%q) err = %v, want ErrValidation", target, err)
		}
	}
	accepted := map[string]string{
		"":                        defaultOAuthRedirect,
		"/?settings=integrations": "/?settings=integrations",
		"/handoff":                "/handoff",
	}
	for target, want := range accepted {
		got, err := normalizeOAuthRedirectTarget(target)
		if err != nil || got != want {
			t.Errorf("normalizeOAuthRedirectTarget(%q) = %q, %v; want %q, nil", target, got, err, want)
		}
	}
}

func TestOAuthResultRedirect(t *testing.T) {
	t.Parallel()
	if got := OAuthResultRedirect("/after-auth", "oauth", "connected"); got != "/after-auth?oauth=connected" {
		t.Errorf("no-query target = %q", got)
	}
	if got := OAuthResultRedirect("/?settings=integrations", "oauth", "connected"); got != "/?settings=integrations&oauth=connected" {
		t.Errorf("existing-query target = %q", got)
	}
	// defaultOAuthRedirect already carries a query, so the indicator joins with "&".
	if got, want := OAuthErrorRedirect(), defaultOAuthRedirect+"&oauth_error=1"; got != want {
		t.Errorf("OAuthErrorRedirect() = %q, want %q", got, want)
	}
}

func TestCompleteOAuthCallback_GitLabExchangeAndIdentity(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			_ = r.ParseForm()
			if got := r.Form.Get("client_id"); got != "gl-client" {
				t.Errorf("client_id = %q", got)
			}
			if got := r.Form.Get("redirect_uri"); got != "https://callback.example/integrations/oauth-callback" {
				t.Errorf("redirect_uri = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"gl_access","refresh_token":"gl_refresh","token_type":"Bearer","scope":"api","expires_in":7200}`)
		case "/api/v4/user":
			if got := r.Header.Get("Authorization"); got != "Bearer gl_access" {
				t.Errorf("Authorization = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":7,"username":"gitlab-user","name":"GitLab User","email":"gitlab@example.com"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	store := &oauthResolveFakeStore{
		integration: &Integration{ID: "int-gl", WorkspaceID: "ws-a", Provider: ProviderGitLab, Status: StatusConnected, BaseURL: srv.URL},
		oauthStates: map[string]OAuthState{
			"st": {ID: "sid", State: "st", WorkspaceID: "ws-a", IntegrationID: "int-gl", Provider: ProviderGitLab, HostKey: "gitlab.gitlab_com", RedirectTarget: "/?settings=integrations", CodeVerifier: "verifier-abcdefghijklmnopqrstuvwxyz0123456789", ExpiresAt: time.Now().UTC().Add(time.Minute)},
		},
	}
	svc := NewService(store).WithOAuthAppLookup(func(context.Context, string, string) (*OAuthAppConfig, error) {
		return &OAuthAppConfig{Provider: ProviderGitLab, HostKey: "gitlab.gitlab_com", ClientID: "gl-client", ClientSecret: "gl-secret", Scopes: []string{"api"}}, nil
	})

	if _, err := svc.CompleteOAuthCallback(context.Background(), "st", "oauth-code", "https://callback.example"); err != nil {
		t.Fatal(err)
	}
	if store.updatedGrant == nil || store.updatedGrant.OAuthAccountName != "GitLab User" || store.updatedGrant.OAuthAccountID != "7" {
		t.Fatalf("unexpected grant: %#v", store.updatedGrant)
	}
}

func TestBeginOAuthConnect_LinearUsesCommaScopesAndPKCE(t *testing.T) {
	store := &oauthResolveFakeStore{
		integration: &Integration{ID: "int-lin", Provider: ProviderLinear, Status: StatusConnected},
	}
	svc := NewService(store).WithOAuthAppLookup(func(context.Context, string, string) (*OAuthAppConfig, error) {
		return &OAuthAppConfig{Provider: ProviderLinear, HostKey: "linear.linear_app", ClientID: "lin-client", ClientSecret: "lin-secret", Scopes: []string{"read", "write", "issues:create"}}, nil
	})
	authorizeURL, err := svc.BeginOAuthConnect(context.Background(), "int-lin", "https://callback.example", "/?settings=integrations")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "linear.app" {
		t.Fatalf("host = %q, want linear.app", u.Host)
	}
	q := u.Query()
	if got := q.Get("client_id"); got != "lin-client" {
		t.Fatalf("client_id = %q", got)
	}
	if got := q.Get("scope"); got != "read,write,issues:create" {
		t.Fatalf("scope = %q (want comma-separated for Linear)", got)
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Fatal("expected PKCE S256 challenge")
	}
}

func TestOAuthHostKeyForIntegration_Linear(t *testing.T) {
	key, err := OAuthHostKeyForIntegration(Integration{Provider: ProviderLinear})
	if err != nil {
		t.Fatal(err)
	}
	if key != "linear.linear_app" {
		t.Fatalf("host key = %q, want linear.linear_app", key)
	}
}

func TestCompleteOAuthCallback_LinearExchangeAndIdentity(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			if got := r.Form.Get("grant_type"); got != "authorization_code" {
				t.Errorf("grant_type = %q", got)
			}
			if got := r.Form.Get("code"); got != "auth-code-xyz" {
				t.Errorf("code = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"lin_access","refresh_token":"lin_refresh","token_type":"Bearer","scope":"read write issues:create","expires_in":86399}`)
		case "/graphql":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":{"viewer":{"id":"user-123","name":"Linear User","email":"user@linear.app"}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	oldToken := linearOAuthTokenURL
	oldGraph := linearGraphQLURL
	linearOAuthTokenURL = srv.URL + "/oauth/token"
	linearGraphQLURL = srv.URL + "/graphql"
	t.Cleanup(func() {
		linearOAuthTokenURL = oldToken
		linearGraphQLURL = oldGraph
	})

	store := &oauthResolveFakeStore{
		integration: &Integration{ID: "int-lin", WorkspaceID: "ws-a", Provider: ProviderLinear, Status: StatusConnected},
		oauthStates: map[string]OAuthState{
			"st": {ID: "sid", State: "st", WorkspaceID: "ws-a", IntegrationID: "int-lin", Provider: ProviderLinear, HostKey: "linear.linear_app", RedirectTarget: "/?settings=integrations", CodeVerifier: "verifier-abcdefghijklmnopqrstuvwxyz0123456789", ExpiresAt: time.Now().UTC().Add(time.Minute)},
		},
	}
	svc := NewService(store).WithOAuthAppLookup(func(context.Context, string, string) (*OAuthAppConfig, error) {
		return &OAuthAppConfig{Provider: ProviderLinear, HostKey: "linear.linear_app", ClientID: "lin-client", ClientSecret: "lin-secret", Scopes: []string{"read", "write", "issues:create"}}, nil
	})

	if _, err := svc.CompleteOAuthCallback(context.Background(), "st", "auth-code-xyz", "https://callback.example"); err != nil {
		t.Fatal(err)
	}
	if store.updatedGrant == nil {
		t.Fatal("expected grant persisted")
	}
	if store.updatedGrant.OAuthAccountName != "Linear User" || store.updatedGrant.OAuthAccountID != "user-123" {
		t.Fatalf("unexpected grant account: %#v", store.updatedGrant)
	}
	if store.updatedGrant.OAuthHostKey != "linear.linear_app" {
		t.Fatalf("host_key = %q", store.updatedGrant.OAuthHostKey)
	}
}
