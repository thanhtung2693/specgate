package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

const maxOAuthAPIResponseBytes = 1 << 20

func decodeOAuthAPIResponse(body io.Reader, out any) error {
	data, err := io.ReadAll(io.LimitReader(body, maxOAuthAPIResponseBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxOAuthAPIResponseBytes {
		return fmt.Errorf("oauth api response exceeds %d bytes", maxOAuthAPIResponseBytes)
	}
	return json.Unmarshal(data, out)
}

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
