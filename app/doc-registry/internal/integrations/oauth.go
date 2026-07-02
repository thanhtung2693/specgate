package integrations

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

type OAuthAppConfig struct {
	Provider     string   `json:"provider"`
	HostKey      string   `json:"host_key"`
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	Scopes       []string `json:"scopes"`
}

type OAuthGrant struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    *time.Time
	TokenType    string
	Scope        string
	AccountID    string
	AccountName  string
	AccountEmail string
	HostKey      string
}

type OAuthCallbackResult struct {
	IntegrationID  string
	RedirectTarget string
}

var (
	oauthHTTPClient         = &http.Client{Timeout: 30 * time.Second}
	gitHubOAuthBaseURL      = "https://github.com"
	gitHubAPIBaseURL        = "https://api.github.com"
	linearOAuthAuthorizeURL = "https://linear.app/oauth/authorize"
	linearOAuthTokenURL     = "https://api.linear.app/oauth/token"
	linearGraphQLURL        = "https://api.linear.app/graphql"
	oauthUserAgent          = "specgate-doc-registry"
	defaultOAuthRedirect    = "/?settings=integrations"
)

// oauthSupportsProvider reports whether a provider has an OAuth code flow.
func oauthSupportsProvider(provider string) bool {
	switch strings.TrimSpace(provider) {
	case ProviderGitLab, ProviderGitHub, ProviderLinear:
		return true
	}
	return false
}

// oauthEndpoint returns the authorize + token endpoints for an integration.
// GitLab is derived from the integration base_url (self-hosted aware); GitHub
// and Linear are fixed (overridable in tests via the package vars). All three
// accept client credentials in the request body (AuthStyleInParams).
func oauthEndpoint(integration Integration) (oauth2.Endpoint, error) {
	switch integration.Provider {
	case ProviderGitLab:
		u, err := url.Parse(strings.TrimSpace(gitLabAPIBase(integration.BaseURL)))
		if err != nil || u.Scheme == "" || u.Host == "" {
			return oauth2.Endpoint{}, fmt.Errorf("%w: gitlab integration base_url must be a valid absolute URL", ErrValidation)
		}
		root := u.Scheme + "://" + u.Host
		return oauth2.Endpoint{AuthURL: root + "/oauth/authorize", TokenURL: root + "/oauth/token", AuthStyle: oauth2.AuthStyleInParams}, nil
	case ProviderGitHub:
		root := strings.TrimRight(gitHubOAuthBaseURL, "/")
		return oauth2.Endpoint{AuthURL: root + "/login/oauth/authorize", TokenURL: root + "/login/oauth/access_token", AuthStyle: oauth2.AuthStyleInParams}, nil
	case ProviderLinear:
		return oauth2.Endpoint{AuthURL: linearOAuthAuthorizeURL, TokenURL: linearOAuthTokenURL, AuthStyle: oauth2.AuthStyleInParams}, nil
	}
	return oauth2.Endpoint{}, fmt.Errorf("%w: provider does not support oauth", ErrValidation)
}

// oauthConfig builds the oauth2.Config for an integration's OAuth flow.
func oauthConfig(integration Integration, app OAuthAppConfig, callbackURL string) (*oauth2.Config, error) {
	endpoint, err := oauthEndpoint(integration)
	if err != nil {
		return nil, err
	}
	scopes := oauthScopes(app, integration.Provider)
	if integration.Provider == ProviderLinear {
		// Linear expects comma-separated scopes; oauth2 space-joins Scopes, so
		// collapse them into a single element it emits verbatim.
		scopes = []string{strings.Join(scopes, ",")}
	}
	return &oauth2.Config{
		ClientID:     strings.TrimSpace(app.ClientID),
		ClientSecret: strings.TrimSpace(app.ClientSecret),
		Endpoint:     endpoint,
		RedirectURL:  strings.TrimSpace(callbackURL),
		Scopes:       scopes,
	}, nil
}

// oauthTokenToGrant maps an oauth2 token onto the storage grant.
func oauthTokenToGrant(token *oauth2.Token) *OAuthGrant {
	grant := &OAuthGrant{
		AccessToken:  strings.TrimSpace(token.AccessToken),
		RefreshToken: strings.TrimSpace(token.RefreshToken),
		TokenType:    strings.TrimSpace(token.TokenType),
	}
	if scope, ok := token.Extra("scope").(string); ok {
		grant.Scope = normalizeOAuthScope(scope)
	}
	if !token.Expiry.IsZero() {
		expiry := token.Expiry.UTC()
		grant.ExpiresAt = &expiry
	}
	return grant
}

// fetchOAuthIdentity reads the connected account's id/name/email + host key via
// the provider's user API using the freshly issued access token.
func fetchOAuthIdentity(ctx context.Context, integration Integration, accessToken string) (id, name, email, hostKey string, err error) {
	switch integration.Provider {
	case ProviderGitLab:
		userURL, e := gitLabUserURL(integration)
		if e != nil {
			return "", "", "", "", e
		}
		var user struct {
			ID          any    `json:"id"`
			Username    string `json:"username"`
			Name        string `json:"name"`
			Email       string `json:"email"`
			PublicEmail string `json:"public_email"`
		}
		if e := oauthAPIGetJSON(ctx, userURL, accessToken, &user); e != nil {
			return "", "", "", "", e
		}
		hk, _ := OAuthHostKeyForIntegration(integration)
		return stringifyOAuthSubject(user.ID), firstNonEmpty(user.Name, user.Username), firstNonEmpty(user.Email, user.PublicEmail), hk, nil
	case ProviderGitHub:
		var user struct {
			ID    any    `json:"id"`
			Login string `json:"login"`
			Name  string `json:"name"`
			Email string `json:"email"`
		}
		if e := oauthAPIGetJSON(ctx, strings.TrimRight(gitHubAPIBaseURL, "/")+"/user", accessToken, &user); e != nil {
			return "", "", "", "", e
		}
		return stringifyOAuthSubject(user.ID), firstNonEmpty(user.Name, user.Login), strings.TrimSpace(user.Email), "github.github_com", nil
	case ProviderLinear:
		lid, lname, lemail, e := linearOAuthViewerIdentity(ctx, accessToken)
		if e != nil {
			return "", "", "", "", e
		}
		return lid, lname, lemail, "linear.linear_app", nil
	}
	return "", "", "", "", fmt.Errorf("%w: provider does not support oauth", ErrValidation)
}

func (s *Service) resolveOAuthToken(ctx context.Context, integration *Integration) (string, error) {
	if strings.TrimSpace(integration.OAuthAccessTokenEncrypted) == "" {
		return "", fmt.Errorf("%w: integration has no oauth access token configured", ErrValidation)
	}
	accessToken, err := DecryptSecret(integration.OAuthAccessTokenEncrypted)
	if err != nil {
		return "", fmt.Errorf("%w: cannot decrypt oauth access token", ErrUnauthorized)
	}
	if integration.OAuthExpiresAt == nil || time.Now().UTC().Before(*integration.OAuthExpiresAt) {
		return accessToken, nil
	}
	if strings.TrimSpace(integration.OAuthRefreshTokenEncrypted) == "" {
		return "", s.markOAuthRefreshError(ctx, integration, fmt.Errorf("%w: oauth refresh token is missing", ErrValidation))
	}
	refreshToken, err := DecryptSecret(integration.OAuthRefreshTokenEncrypted)
	if err != nil {
		return "", s.markOAuthRefreshError(ctx, integration, fmt.Errorf("%w: cannot decrypt oauth refresh token", ErrUnauthorized))
	}
	if !oauthSupportsProvider(integration.Provider) {
		return "", fmt.Errorf("%w: integration provider does not support oauth refresh", ErrValidation)
	}
	if s.oauthApps == nil {
		return "", fmt.Errorf("%w: oauth app lookup is not configured", ErrValidation)
	}
	app, err := s.oauthApps(ctx, integration.Provider, integration.OAuthHostKey)
	if err != nil {
		return "", s.markOAuthRefreshError(ctx, integration, err)
	}
	if app == nil {
		return "", s.markOAuthRefreshError(ctx, integration, fmt.Errorf("%w: oauth app config not found", ErrValidation))
	}
	config, err := oauthConfig(*integration, *app, "")
	if err != nil {
		return "", s.markOAuthRefreshError(ctx, integration, err)
	}
	refreshed, err := config.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken}).Token()
	if err != nil {
		return "", s.markOAuthRefreshError(ctx, integration, fmt.Errorf("%w: oauth token refresh failed: %v", ErrUpstream, err))
	}
	grant := oauthTokenToGrant(refreshed)
	if strings.TrimSpace(grant.AccessToken) == "" {
		return "", s.markOAuthRefreshError(ctx, integration, fmt.Errorf("%w: oauth refresh returned no access token", ErrUpstream))
	}
	grant.HostKey = integration.OAuthHostKey
	nextRefresh := refreshToken
	if strings.TrimSpace(grant.RefreshToken) != "" {
		nextRefresh = grant.RefreshToken
	}
	encAccess, err := EncryptSecret(strings.TrimSpace(grant.AccessToken))
	if err != nil {
		return "", err
	}
	encRefresh, err := EncryptSecret(strings.TrimSpace(nextRefresh))
	if err != nil {
		return "", err
	}
	if err := s.oauth.UpdateOAuthGrant(ctx, Integration{
		ID:                         integration.ID,
		AuthMethod:                 AuthMethodOAuth,
		OAuthAccessTokenEncrypted:  encAccess,
		OAuthRefreshTokenEncrypted: encRefresh,
		OAuthExpiresAt:             grant.ExpiresAt,
		OAuthTokenType:             strings.TrimSpace(grant.TokenType),
		OAuthScope:                 strings.TrimSpace(grant.Scope),
		OAuthAccountID:             strings.TrimSpace(grant.AccountID),
		OAuthAccountName:           strings.TrimSpace(grant.AccountName),
		OAuthAccountEmail:          strings.TrimSpace(grant.AccountEmail),
		OAuthHostKey:               firstNonEmpty(strings.TrimSpace(grant.HostKey), strings.TrimSpace(integration.OAuthHostKey)),
	}); err != nil {
		return "", err
	}
	if _, err := s.integrations.UpdateIntegration(ctx, Integration{
		ID:        integration.ID,
		Status:    StatusConnected,
		LastError: "",
	}); err != nil {
		return "", err
	}
	return strings.TrimSpace(grant.AccessToken), nil
}

func (s *Service) BeginOAuthConnect(ctx context.Context, integrationID string, callbackBaseURL string, redirectTarget string) (string, error) {
	if s.oauthApps == nil {
		return "", fmt.Errorf("%w: oauth app lookup is not configured", ErrValidation)
	}
	integrationID = strings.TrimSpace(integrationID)
	if integrationID == "" {
		return "", fmt.Errorf("%w: integration_id is required", ErrValidation)
	}
	integration, err := s.integrations.GetIntegration(ctx, integrationID)
	if err != nil {
		return "", err
	}
	if !oauthSupportsProvider(integration.Provider) {
		return "", fmt.Errorf("%w: integration provider does not support oauth", ErrValidation)
	}
	hostKey, err := OAuthHostKeyForIntegration(*integration)
	if err != nil {
		return "", err
	}
	app, err := s.oauthApps(ctx, integration.Provider, hostKey)
	if err != nil {
		return "", err
	}
	if app == nil {
		return "", fmt.Errorf("%w: oauth app config not found", ErrValidation)
	}
	callbackURL, err := OAuthCallbackURL(callbackBaseURL)
	if err != nil {
		return "", err
	}
	redirectTarget, err = normalizeOAuthRedirectTarget(redirectTarget)
	if err != nil {
		return "", err
	}
	stateToken, err := newOAuthStateToken()
	if err != nil {
		return "", fmt.Errorf("%w: cannot generate oauth state", ErrUpstream)
	}
	config, err := oauthConfig(*integration, *app, callbackURL)
	if err != nil {
		return "", err
	}
	// PKCE: a per-flow verifier is stored on the state row and its S256 challenge
	// is sent in the authorize request; the verifier is replayed at exchange.
	verifier := oauth2.GenerateVerifier()
	authorizeURL := config.AuthCodeURL(stateToken, oauth2.S256ChallengeOption(verifier))
	if _, err := s.oauth.CreateOAuthState(ctx, OAuthState{
		State:          stateToken,
		IntegrationID:  integration.ID,
		Provider:       integration.Provider,
		HostKey:        hostKey,
		RedirectTarget: redirectTarget,
		CodeVerifier:   verifier,
		ExpiresAt:      time.Now().UTC().Add(10 * time.Minute),
	}); err != nil {
		return "", err
	}
	return authorizeURL, nil
}

// PendingOAuthSpec is the to-be-created integration for a fresh OAuth connect:
// no integration row exists until the callback succeeds.
type PendingOAuthSpec struct {
	Provider   string
	Name       string
	BaseURL    string
	ConfigJSON string
}

// BeginPendingOAuthConnect starts OAuth for a NOT-YET-CREATED integration: the
// pending spec is stored on the state row (IntegrationID empty), and the
// integration is created only when CompleteOAuthCallback succeeds. The reconnect
// path (BeginOAuthConnect, existing integration id) is unchanged.
func (s *Service) BeginPendingOAuthConnect(ctx context.Context, spec PendingOAuthSpec, callbackBaseURL string, redirectTarget string) (string, error) {
	if s.oauthApps == nil {
		return "", fmt.Errorf("%w: oauth app lookup is not configured", ErrValidation)
	}
	spec.Provider = strings.TrimSpace(spec.Provider)
	if !oauthSupportsProvider(spec.Provider) {
		return "", fmt.Errorf("%w: provider does not support oauth", ErrValidation)
	}
	if strings.TrimSpace(spec.Name) == "" {
		return "", fmt.Errorf("%w: name is required", ErrValidation)
	}
	pendingIntegration := Integration{
		Provider:   spec.Provider,
		Name:       strings.TrimSpace(spec.Name),
		BaseURL:    strings.TrimSpace(spec.BaseURL),
		ConfigJSON: strings.TrimSpace(spec.ConfigJSON),
	}
	hostKey, err := OAuthHostKeyForIntegration(pendingIntegration)
	if err != nil {
		return "", err
	}
	app, err := s.oauthApps(ctx, spec.Provider, hostKey)
	if err != nil {
		return "", err
	}
	if app == nil {
		return "", fmt.Errorf("%w: oauth app config not found", ErrValidation)
	}
	callbackURL, err := OAuthCallbackURL(callbackBaseURL)
	if err != nil {
		return "", err
	}
	redirectTarget, err = normalizeOAuthRedirectTarget(redirectTarget)
	if err != nil {
		return "", err
	}
	stateToken, err := newOAuthStateToken()
	if err != nil {
		return "", fmt.Errorf("%w: cannot generate oauth state", ErrUpstream)
	}
	config, err := oauthConfig(pendingIntegration, *app, callbackURL)
	if err != nil {
		return "", err
	}
	verifier := oauth2.GenerateVerifier()
	authorizeURL := config.AuthCodeURL(stateToken, oauth2.S256ChallengeOption(verifier))
	if _, err := s.oauth.CreateOAuthState(ctx, OAuthState{
		State:             stateToken,
		IntegrationID:     "",
		Provider:          spec.Provider,
		HostKey:           hostKey,
		RedirectTarget:    redirectTarget,
		CodeVerifier:      verifier,
		PendingName:       pendingIntegration.Name,
		PendingBaseURL:    pendingIntegration.BaseURL,
		PendingConfigJSON: pendingIntegration.ConfigJSON,
		ExpiresAt:         time.Now().UTC().Add(10 * time.Minute),
	}); err != nil {
		return "", err
	}
	return authorizeURL, nil
}

func (s *Service) CompleteOAuthCallback(ctx context.Context, stateToken string, code string, callbackBaseURL string) (*OAuthCallbackResult, error) {
	if s.oauthApps == nil {
		return nil, fmt.Errorf("%w: oauth app lookup is not configured", ErrValidation)
	}
	stateToken = strings.TrimSpace(stateToken)
	if stateToken == "" {
		return nil, fmt.Errorf("%w: state is required", ErrValidation)
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, fmt.Errorf("%w: code is required", ErrValidation)
	}
	state, err := s.oauth.GetOAuthState(ctx, stateToken)
	if err != nil {
		return nil, err
	}
	if state.ConsumedAt != nil {
		return nil, fmt.Errorf("%w: oauth state already consumed", ErrValidation)
	}
	if time.Now().UTC().After(state.ExpiresAt) {
		return nil, fmt.Errorf("%w: oauth state has expired", ErrValidation)
	}
	// Create-on-callback: an empty IntegrationID means no integration exists yet
	// (fresh connect). Build a transient integration from the pending spec for the
	// exchange + identity fetch; the row is created below only on success.
	pending := strings.TrimSpace(state.IntegrationID) == ""
	var integration *Integration
	if pending {
		integration = &Integration{
			Provider:   state.Provider,
			Name:       strings.TrimSpace(state.PendingName),
			BaseURL:    strings.TrimSpace(state.PendingBaseURL),
			ConfigJSON: strings.TrimSpace(state.PendingConfigJSON),
		}
	} else {
		integration, err = s.integrations.GetIntegration(ctx, state.IntegrationID)
		if err != nil {
			return nil, err
		}
	}
	if !oauthSupportsProvider(integration.Provider) {
		return nil, fmt.Errorf("%w: integration provider does not support oauth", ErrValidation)
	}
	app, err := s.oauthApps(ctx, integration.Provider, state.HostKey)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, fmt.Errorf("%w: oauth app config not found", ErrValidation)
	}
	callbackURL, err := OAuthCallbackURL(callbackBaseURL)
	if err != nil {
		return nil, err
	}
	config, err := oauthConfig(*integration, *app, callbackURL)
	if err != nil {
		return nil, err
	}
	exchangeOpts := []oauth2.AuthCodeOption{}
	if v := strings.TrimSpace(state.CodeVerifier); v != "" {
		exchangeOpts = append(exchangeOpts, oauth2.VerifierOption(v))
	}
	token, err := config.Exchange(ctx, code, exchangeOpts...)
	if err != nil {
		return nil, fmt.Errorf("%w: oauth token exchange failed: %v", ErrUpstream, err)
	}
	grant := oauthTokenToGrant(token)
	if strings.TrimSpace(grant.AccessToken) == "" {
		return nil, fmt.Errorf("%w: oauth exchange returned no access token", ErrUpstream)
	}
	accountID, accountName, accountEmail, grantHostKey, err := fetchOAuthIdentity(ctx, *integration, grant.AccessToken)
	if err != nil {
		return nil, err
	}
	grant.AccountID, grant.AccountName, grant.AccountEmail, grant.HostKey = accountID, accountName, accountEmail, grantHostKey
	encAccess, err := EncryptSecret(strings.TrimSpace(grant.AccessToken))
	if err != nil {
		return nil, err
	}
	refreshToken := strings.TrimSpace(grant.RefreshToken)
	encRefresh := ""
	if refreshToken != "" {
		encRefresh, err = EncryptSecret(refreshToken)
		if err != nil {
			return nil, err
		}
	}
	if pending {
		// Create the integration with the grant + consume the state atomically, so
		// a failed connect leaves nothing behind (no orphan).
		row := Integration{
			Provider:                   integration.Provider,
			Name:                       integration.Name,
			BaseURL:                    integration.BaseURL,
			ConfigJSON:                 integration.ConfigJSON,
			Status:                     StatusConnected,
			AuthMethod:                 AuthMethodOAuth,
			OAuthAccessTokenEncrypted:  encAccess,
			OAuthRefreshTokenEncrypted: encRefresh,
			OAuthExpiresAt:             grant.ExpiresAt,
			OAuthTokenType:             strings.TrimSpace(grant.TokenType),
			OAuthScope:                 strings.TrimSpace(grant.Scope),
			OAuthAccountID:             strings.TrimSpace(grant.AccountID),
			OAuthAccountName:           strings.TrimSpace(grant.AccountName),
			OAuthAccountEmail:          strings.TrimSpace(grant.AccountEmail),
			OAuthHostKey:               firstNonEmpty(strings.TrimSpace(grant.HostKey), strings.TrimSpace(state.HostKey)),
		}
		if err := normalizeIntegration(&row); err != nil {
			return nil, err
		}
		var createdID string
		if err := s.txStore.WithTx(ctx, func(tx Store) error {
			created, err := tx.CreateIntegration(ctx, row)
			if err != nil {
				return err
			}
			createdID = created.ID
			_, err = tx.ConsumeOAuthState(ctx, stateToken)
			return err
		}); err != nil {
			return nil, err
		}
		return &OAuthCallbackResult{
			IntegrationID:  createdID,
			RedirectTarget: firstNonEmpty(state.RedirectTarget, defaultOAuthRedirect),
		}, nil
	}
	if err := s.oauth.UpdateOAuthGrant(ctx, Integration{
		ID:                         integration.ID,
		AuthMethod:                 AuthMethodOAuth,
		OAuthAccessTokenEncrypted:  encAccess,
		OAuthRefreshTokenEncrypted: encRefresh,
		OAuthExpiresAt:             grant.ExpiresAt,
		OAuthTokenType:             strings.TrimSpace(grant.TokenType),
		OAuthScope:                 strings.TrimSpace(grant.Scope),
		OAuthAccountID:             strings.TrimSpace(grant.AccountID),
		OAuthAccountName:           strings.TrimSpace(grant.AccountName),
		OAuthAccountEmail:          strings.TrimSpace(grant.AccountEmail),
		OAuthHostKey:               firstNonEmpty(strings.TrimSpace(grant.HostKey), strings.TrimSpace(state.HostKey)),
	}); err != nil {
		return nil, err
	}
	if _, err := s.integrations.UpdateIntegration(ctx, Integration{
		ID:        integration.ID,
		Status:    StatusConnected,
		LastError: "",
	}); err != nil {
		return nil, err
	}
	if _, err := s.oauth.ConsumeOAuthState(ctx, stateToken); err != nil {
		return nil, err
	}
	return &OAuthCallbackResult{
		IntegrationID:  integration.ID,
		RedirectTarget: firstNonEmpty(state.RedirectTarget, defaultOAuthRedirect),
	}, nil
}

func (s *Service) DisconnectOAuth(ctx context.Context, integrationID string) error {
	integrationID = strings.TrimSpace(integrationID)
	if integrationID == "" {
		return fmt.Errorf("%w: integration_id is required", ErrValidation)
	}
	return s.oauth.ClearOAuthGrant(ctx, integrationID)
}

func OAuthHostKeyForIntegration(integration Integration) (string, error) {
	switch integration.Provider {
	case ProviderGitHub:
		return "github.github_com", nil
	case ProviderGitLab:
		base := strings.TrimSpace(integration.BaseURL)
		if base == "" {
			return "", fmt.Errorf("%w: gitlab integration base_url is required for oauth", ErrValidation)
		}
		u, err := url.Parse(base)
		if err != nil || u.Host == "" {
			return "", fmt.Errorf("%w: gitlab integration base_url must be a valid absolute URL", ErrValidation)
		}
		host := strings.ToLower(strings.ReplaceAll(u.Hostname(), ".", "_"))
		return "gitlab." + host, nil
	case ProviderLinear:
		return "linear.linear_app", nil
	default:
		return "", fmt.Errorf("%w: provider does not support oauth host derivation", ErrValidation)
	}
}

func OAuthCallbackURL(base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", fmt.Errorf("%w: oauth callback base URL is required", ErrValidation)
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("%w: oauth callback base URL must be an absolute URL", ErrValidation)
	}
	u.Path = path.Join(strings.TrimRight(u.Path, "/"), "/integrations/oauth-callback")
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func linearOAuthViewerIdentity(ctx context.Context, accessToken string) (id, name, email string, err error) {
	body := `{"query":"{ viewer { id name email } }"}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, linearGraphQLURL, strings.NewReader(body))
	if err != nil {
		return "", "", "", fmt.Errorf("%w: build linear viewer request: %v", ErrUpstream, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("User-Agent", oauthUserAgent)
	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", "", fmt.Errorf("%w: linear viewer api returned status %d", ErrUpstream, resp.StatusCode)
	}
	var result struct {
		Data struct {
			Viewer struct {
				ID    string `json:"id"`
				Name  string `json:"name"`
				Email string `json:"email"`
			} `json:"viewer"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", "", fmt.Errorf("%w: decode linear viewer response: %v", ErrUpstream, err)
	}
	if len(result.Errors) > 0 {
		return "", "", "", fmt.Errorf("%w: linear graphql error: %s", ErrUpstream, result.Errors[0].Message)
	}
	return result.Data.Viewer.ID, result.Data.Viewer.Name, result.Data.Viewer.Email, nil
}

func (s *Service) markOAuthRefreshError(ctx context.Context, integration *Integration, cause error) error {
	_, _ = s.integrations.UpdateIntegration(ctx, Integration{
		ID:        integration.ID,
		Status:    StatusError,
		LastError: strings.TrimSpace(cause.Error()),
	})
	return cause
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func integrationHasResolvedToken(integration Integration) bool {
	switch strings.TrimSpace(integration.AuthMethod) {
	case AuthMethodOAuth:
		return integration.HasOAuthToken
	case AuthMethodPAT:
		return integration.HasAPIToken
	default:
		return integration.HasAPIToken || integration.HasOAuthToken
	}
}

func oauthScopes(app OAuthAppConfig, provider string) []string {
	if len(app.Scopes) > 0 {
		return app.Scopes
	}
	switch provider {
	case ProviderGitLab:
		return []string{"api"}
	case ProviderGitHub:
		return []string{"repo", "read:user"}
	case ProviderLinear:
		return []string{"read", "write", "issues:create"}
	}
	return nil
}

func normalizeOAuthRedirectTarget(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return defaultOAuthRedirect, nil
	}
	// Must be an app-relative path. Reject protocol-relative ("//host") and
	// backslash-prefixed ("/\host") forms: browsers normalize "\" to "/", so
	// both resolve to an off-site origin when emitted as the Location header
	// (open redirect).
	if !strings.HasPrefix(target, "/") || (len(target) >= 2 && (target[1] == '/' || target[1] == '\\')) {
		return "", fmt.Errorf("%w: oauth redirect target must be an app-relative path", ErrValidation)
	}
	return target, nil
}

// OAuthResultRedirect appends an oauth result indicator (key=value) to an
// app-relative redirect target so the SPA can surface a success/error toast
// after the provider round-trip. An empty target falls back to the default.
func OAuthResultRedirect(target, key, value string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		target = defaultOAuthRedirect
	}
	sep := "?"
	if strings.Contains(target, "?") {
		sep = "&"
	}
	return target + sep + url.QueryEscape(key) + "=" + url.QueryEscape(value)
}

// OAuthErrorRedirect is the app-relative target used when the callback fails and
// the intended redirect can't be recovered — the user lands back in the app with
// an error indicator instead of a bare error page (no error detail is leaked).
func OAuthErrorRedirect() string {
	return OAuthResultRedirect(defaultOAuthRedirect, "oauth_error", "1")
}

func gitLabUserURL(integration Integration) (string, error) {
	base := gitLabAPIBase(integration.BaseURL)
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("%w: gitlab integration base_url must be a valid absolute URL", ErrValidation)
	}
	u.Path = "/api/v4/user"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func oauthAPIGetJSON(ctx context.Context, requestURL string, accessToken string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("%w: build oauth api request: %v", ErrUpstream, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("User-Agent", oauthUserAgent)
	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: oauth api returned status %d", ErrUpstream, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: decode oauth api response: %v", ErrUpstream, err)
	}
	return nil
}

func normalizeOAuthScope(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.ReplaceAll(raw, ",", " ")
	return strings.Join(strings.Fields(raw), " ")
}

func stringifyOAuthSubject(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case json.Number:
		return v.String()
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func newOAuthStateToken() (string, error) {
	var raw [16]byte
	if _, err := io.ReadFull(rand.Reader, raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
