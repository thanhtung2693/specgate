package linear

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

// generateSecret returns a cryptographically random 32-byte webhook secret,
// hex-encoded (64 chars), matching the pattern used by the GitHub driver.
func generateSecret() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func init() {
	coretypes.RegisterWebhookDriver(coretypes.ProviderLinear, webhookDriver{})
}

// webhookDriver is the Linear implementation of coretypes.WebhookDriver: bare-hex
// HMAC-SHA256 Linear-Signature verification, Issue/Comment normalization, and
// managed team-scoped webhook provisioning via the Linear GraphQL API.
type webhookDriver struct{}

func (webhookDriver) VerifyDelivery(secret string, in coretypes.InboundWebhook) error {
	if strings.TrimSpace(secret) == "" {
		return fmt.Errorf("%w: no linear webhook secret configured", coretypes.ErrUnauthorized)
	}
	if !coretypes.VerifyLinearSignature(secret, []byte(in.PayloadJSON), in.Signature) {
		return fmt.Errorf("%w: linear signature mismatch", coretypes.ErrUnauthorized)
	}
	return nil
}

func (webhookDriver) Normalize(in coretypes.InboundWebhook) (*coretypes.NormalizedDelivery, *coretypes.NormalizedComment, string, error) {
	// Determine the event type from the payload type field.
	// Linear sends "Issue" or "Comment" in the top-level "type" field.
	switch webhookPayloadType(in.PayloadJSON) {
	case "comment":
		comment, err := ParseCommentScopeDrift(in.PayloadJSON)
		if err != nil {
			return nil, nil, "", err
		}
		return nil, &comment, "", nil
	case "issue":
		nd, err := ParseAndNormalize(in.PayloadJSON)
		if err != nil {
			return nil, nil, "", err
		}
		if nd.EventType == "" {
			return nil, nil, "unsupported_linear_event", nil
		}
		return &nd, nil, "", nil
	default:
		return nil, nil, "unsupported_linear_event", nil
	}
}

func (webhookDriver) SupportsManagedWebhook() bool { return true }

// ProvisionWebhook registers a team-scoped managed webhook on Linear. It
// generates a 32-byte random secret, calls the Linear webhookCreate mutation
// to create the webhook with that secret, and returns the provider hook ID and
// the generated secret for encrypted storage.
//
// Linear's webhookCreate accepts an optional `secret` field in WebhookCreateInput;
// when provided, Linear uses it to sign deliveries (same HMAC-SHA256 as the
// existing per-integration env-secret path). The target.ExternalID must be the
// Linear team UUID.
func (webhookDriver) ProvisionWebhook(ctx context.Context, in coretypes.ProvisionInput) (coretypes.ProvisionResult, error) {
	secret, err := generateSecret()
	if err != nil {
		return coretypes.ProvisionResult{}, fmt.Errorf("generate linear webhook secret: %w", err)
	}

	teamID := strings.TrimSpace(in.Target.ExternalID)
	if teamID == "" {
		return coretypes.ProvisionResult{}, fmt.Errorf("%w: linear team external_id is required to provision a webhook", coretypes.ErrValidation)
	}

	const webhookCreateMutation = `mutation WebhookCreate($input: WebhookCreateInput!) {
  webhookCreate(input: $input) {
    success
    webhook { id url enabled }
  }
}`

	variables := map[string]any{
		"input": map[string]any{
			"url":           in.WebhookURL,
			"teamId":        teamID,
			"resourceTypes": []string{"Issue", "Comment"},
			"secret":        secret,
		},
	}

	var data struct {
		WebhookCreate struct {
			Success bool `json:"success"`
			Webhook struct {
				ID      string `json:"id"`
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
			} `json:"webhook"`
		} `json:"webhookCreate"`
	}

	if err := linearGraphQLRequest(ctx, in.Target.Token, in.Target.Bearer, webhookCreateMutation, variables, &data); err != nil {
		return coretypes.ProvisionResult{}, err
	}
	if !data.WebhookCreate.Success {
		return coretypes.ProvisionResult{}, fmt.Errorf("%w: linear webhookCreate did not report success", coretypes.ErrUpstream)
	}
	hookID := strings.TrimSpace(data.WebhookCreate.Webhook.ID)
	if hookID == "" {
		return coretypes.ProvisionResult{}, fmt.Errorf("%w: linear webhookCreate returned empty webhook id", coretypes.ErrUpstream)
	}

	return coretypes.ProvisionResult{ProviderHookID: hookID, Secret: secret}, nil
}

// DeleteWebhook deregisters a Linear managed webhook by ID.
func (webhookDriver) DeleteWebhook(ctx context.Context, hookID string, target coretypes.ProviderTarget) error {
	hookID = strings.TrimSpace(hookID)
	if hookID == "" {
		return fmt.Errorf("%w: hook_id is required", coretypes.ErrValidation)
	}

	const webhookDeleteMutation = `mutation WebhookDelete($id: String!) {
  webhookDelete(id: $id) { success }
}`

	var data struct {
		WebhookDelete struct {
			Success bool `json:"success"`
		} `json:"webhookDelete"`
	}

	if err := linearGraphQLRequest(ctx, target.Token, target.Bearer, webhookDeleteMutation, map[string]any{"id": hookID}, &data); err != nil {
		return err
	}
	if !data.WebhookDelete.Success {
		return fmt.Errorf("%w: linear webhookDelete did not report success", coretypes.ErrUpstream)
	}
	return nil
}

// webhookPayloadType extracts the lowercase "type" field from a raw Linear
// webhook payload without importing the parent package. Returns "" on error.
func webhookPayloadType(raw string) string {
	payload, err := parseLinearWebhookPayload(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(payload.Type))
}
