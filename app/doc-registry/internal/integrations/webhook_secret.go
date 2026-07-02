package integrations

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

// The webhook replay tolerance + timestamp check live in coretypes (so the
// per-provider drivers share them); these aliases keep the parent's per-
// integration GitLab handler unchanged.
const webhookReplayTolerance = coretypes.WebhookReplayTolerance

var webhookTimestampWithinTolerance = coretypes.WebhookTimestampWithinTolerance

// providerSelfServesWebhookSecret reports whether a provider stores a
// per-integration inbound-webhook secret (GitLab signing token / GitHub HMAC
// secret). Linear manages its signing secret on its side (env secret), so it is
// not included.
func providerSelfServesWebhookSecret(provider string) bool {
	switch provider {
	case ProviderGitLab, ProviderGitHub:
		return true
	default:
		return false
	}
}

// providerAutoGeneratesWebhookSecret reports whether we mint the secret for a
// provider. GitHub's webhook secret is an arbitrary value the user sets on both
// sides, so we generate it and the user pastes it into GitHub. GitLab is
// validate-only — GitLab generates the signing token, so we never mint it.
func providerAutoGeneratesWebhookSecret(provider string) bool {
	return provider == ProviderGitHub
}

// generateWebhookSecret returns a cryptographically random 32-byte secret,
// hex-encoded (64 chars), used to seed/rotate GitHub's webhook secret.
func generateWebhookSecret() (string, error) {
	var raw [32]byte
	if _, err := io.ReadFull(rand.Reader, raw[:]); err != nil {
		return "", fmt.Errorf("generate webhook secret: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

// WebhookSecret returns the integration's inbound-webhook secret in plaintext.
// For GitHub it is generated on first access (get-or-create) so the user can
// copy it into GitHub. For GitLab it returns whatever signing token the user has
// pasted (empty until configured — GitLab owns the value, so we never mint it).
// Any non-self-serve provider (Linear) is a validation error.
func (s *Service) WebhookSecret(ctx context.Context, integrationID string) (string, error) {
	integration, err := s.requireSelfServeWebhookIntegration(ctx, integrationID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(integration.WebhookSecretEncrypted) != "" {
		secret, err := DecryptSecret(integration.WebhookSecretEncrypted)
		if err != nil {
			return "", fmt.Errorf("%w: cannot decrypt webhook secret", ErrUnauthorized)
		}
		return secret, nil
	}
	if providerAutoGeneratesWebhookSecret(integration.Provider) {
		return s.mintWebhookSecret(ctx, integration.ID)
	}
	// GitLab without a pasted signing token yet — not an error; the UI prompts
	// the user to paste one.
	return "", nil
}

// SetWebhookSecret stores a user-provided webhook secret (GitLab's pasted
// signing token; or a custom GitHub secret). GitLab values must be a valid
// `whsec_` signing token. An empty value is rejected.
func (s *Service) SetWebhookSecret(ctx context.Context, integrationID string, secret string) error {
	integration, err := s.requireSelfServeWebhookIntegration(ctx, integrationID)
	if err != nil {
		return err
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return fmt.Errorf("%w: webhook secret is required", ErrValidation)
	}
	if integration.Provider == ProviderGitLab && !IsGitLabSigningToken(secret) {
		return fmt.Errorf("%w: gitlab signing token must be whsec_ + a base64-encoded 32-byte key", ErrValidation)
	}
	encrypted, err := EncryptSecret(secret)
	if err != nil {
		return err
	}
	return s.integrations.UpdateWebhookSecretEncrypted(ctx, integration.ID, encrypted)
}

// RotateWebhookSecret mints a fresh secret for a provider we generate for
// (GitHub) and returns the new plaintext. GitLab is validate-only — rotation
// happens in GitLab, then the user re-pastes — so it is a validation error here.
func (s *Service) RotateWebhookSecret(ctx context.Context, integrationID string) (string, error) {
	integration, err := s.requireSelfServeWebhookIntegration(ctx, integrationID)
	if err != nil {
		return "", err
	}
	if !providerAutoGeneratesWebhookSecret(integration.Provider) {
		return "", fmt.Errorf("%w: rotate the signing token in the provider, then paste it again", ErrValidation)
	}
	return s.mintWebhookSecret(ctx, integration.ID)
}

func (s *Service) requireSelfServeWebhookIntegration(ctx context.Context, integrationID string) (*Integration, error) {
	integrationID = strings.TrimSpace(integrationID)
	if integrationID == "" {
		return nil, fmt.Errorf("%w: integration_id is required", ErrValidation)
	}
	integration, err := s.integrations.GetIntegration(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	if !providerSelfServesWebhookSecret(integration.Provider) {
		return nil, fmt.Errorf("%w: provider %q manages its own webhook secret", ErrValidation, integration.Provider)
	}
	return integration, nil
}

func (s *Service) mintWebhookSecret(ctx context.Context, integrationID string) (string, error) {
	secret, err := generateWebhookSecret()
	if err != nil {
		return "", err
	}
	encrypted, err := EncryptSecret(secret)
	if err != nil {
		return "", err
	}
	if err := s.integrations.UpdateWebhookSecretEncrypted(ctx, integrationID, encrypted); err != nil {
		return "", err
	}
	return secret, nil
}

// resolveWebhookSecret returns the plaintext secret an inbound webhook must be
// verified against — the per-integration secret (GitHub HMAC secret or GitLab
// signing token). An unset secret returns "" so the handler refuses the call
// rather than acting as an open relay.
func (s *Service) resolveWebhookSecret(integration *Integration) (string, error) {
	if strings.TrimSpace(integration.WebhookSecretEncrypted) == "" {
		return "", nil
	}
	return DecryptSecret(integration.WebhookSecretEncrypted)
}
