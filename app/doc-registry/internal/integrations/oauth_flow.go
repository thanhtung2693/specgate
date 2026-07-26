package integrations

import (
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

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
		WorkspaceID:    integration.WorkspaceID,
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
	workspaceID := WorkspaceID(ctx)
	if workspaceID == "" {
		return "", fmt.Errorf("%w: workspace_id is required", ErrValidation)
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
	if app == nil || app.Provider != spec.Provider || app.HostKey != hostKey {
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
		WorkspaceID:       workspaceID,
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
	workspaceID := strings.TrimSpace(state.WorkspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: oauth state workspace_id is required", ErrValidation)
	}
	ctx = WithWorkspace(ctx, workspaceID)
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
		ctx, err = bindIntegrationWorkspace(ctx, integration)
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
			WorkspaceID:                state.WorkspaceID,
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
	integration, err := s.integrations.GetIntegration(ctx, integrationID)
	if err != nil {
		return err
	}
	resources, err := s.resources.ListResources(ctx, integrationID)
	if err != nil {
		return err
	}
	for i := range resources {
		cfg := managedWebhookConfigFromJSON(resources[i].ConfigJSON)
		if strings.TrimSpace(cfg.ProviderHookID) == "" {
			continue
		}
		if err := s.deleteManagedProviderWebhook(ctx, integration, &resources[i]); err != nil {
			return err
		}
		cleanedConfig := removeManagedWebhookConfig(resources[i].ConfigJSON)
		if err := s.txStore.WithTx(ctx, func(tx Store) error {
			if err := tx.UpdateResourceConfigJSON(ctx, integrationID, resources[i].ID, cleanedConfig); err != nil {
				return err
			}
			return tx.UpdateResourceWebhookSecretEncrypted(ctx, integrationID, resources[i].ID, "")
		}); err != nil {
			return err
		}
	}
	return s.oauth.ClearOAuthGrant(ctx, integrationID)
}
