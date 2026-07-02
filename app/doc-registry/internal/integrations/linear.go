package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/specgate/doc-registry/internal/integrations/linear"
)

// SetApiToken stores a provider API token for outbound calls (Linear hosted MCP
// / GraphQL, GitLab REST). The token is AES-256-GCM encrypted before storage and
// never returned in plaintext; Integration.HasAPIToken reports presence on read.
// An empty token is rejected so a blank submit cannot silently wipe the stored
// value. Requires a configured secret key (the token must be recoverable).
func (s *Service) SetApiToken(ctx context.Context, integrationID string, token string) error {
	integrationID = strings.TrimSpace(integrationID)
	if integrationID == "" {
		return fmt.Errorf("%w: integration_id is required", ErrValidation)
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("%w: api_token is required", ErrValidation)
	}
	integration, err := s.integrations.GetIntegration(ctx, integrationID)
	if err != nil {
		return err
	}
	if !providerSupportsAPIToken(integration.Provider) {
		return fmt.Errorf("%w: integration provider does not support an outbound api token", ErrValidation)
	}
	encrypted, err := EncryptSecret(strings.TrimSpace(token))
	if err != nil {
		// Unlike the verify-only webhook secret, the API token is useless if it
		// cannot be recovered, so a missing key is a hard error here rather than
		// a silent "stored empty".
		return fmt.Errorf("%w: cannot encrypt api token (%v)", ErrValidation, err)
	}
	return s.integrations.UpdateApiTokenEncrypted(ctx, integrationID, encrypted)
}

// ResolveAPIToken returns the decrypted provider API token for outbound issue
// creation. The plaintext is recovered server-side only and never crosses a
// network boundary (the handoff service uses it in-process). A disabled
// integration, an unsupported provider, or a missing token is a clear error so
// the outbound handoff fails loudly rather than silently producing no issue.
func (s *Service) ResolveAPIToken(ctx context.Context, integrationID string) (string, error) {
	integrationID = strings.TrimSpace(integrationID)
	if integrationID == "" {
		return "", fmt.Errorf("%w: integration_id is required", ErrValidation)
	}
	integration, err := s.integrations.GetIntegration(ctx, integrationID)
	if err != nil {
		return "", err
	}
	if !providerSupportsAPIToken(integration.Provider) {
		return "", fmt.Errorf("%w: integration provider does not support an outbound api token", ErrValidation)
	}
	if integration.Status == StatusDisabled {
		return "", fmt.Errorf("%w: integration is disabled", ErrValidation)
	}
	if strings.TrimSpace(integration.AuthMethod) == AuthMethodOAuth {
		return s.resolveOAuthToken(ctx, integration)
	}
	if strings.TrimSpace(integration.APITokenEncrypted) == "" {
		return "", fmt.Errorf("%w: integration has no api token configured", ErrValidation)
	}
	token, err := DecryptSecret(integration.APITokenEncrypted)
	if err != nil {
		return "", fmt.Errorf("%w: cannot decrypt api token", ErrUnauthorized)
	}
	return token, nil
}

// HandleLinearWebhook is the tracker peer of the git webhook handlers. Trackers
// are an OPTIONAL upgrade on the git/MCP floor, so this path is intentionally
// lighter than commitDelivery: it does not require a registered resource and
// does not gate on a matched work item. It authenticates the integration by id
// + Linear-Signature (HMAC-SHA256 of the raw body, bare hex), normalizes the
// Issue payload via the linear subpackage, reads the `fixes SPECGATE-{key}` footer
// for correlation, and emits a delivery.tracker_status_changed feedback event
// carrying the Linear identifier and raw workflow state.type — whether or not a
// work item matches.
func (s *Service) HandleLinearWebhook(ctx context.Context, integrationID string, in LinearWebhookInput) (*LinearWebhookResult, error) {
	integrationID = strings.TrimSpace(integrationID)
	if integrationID == "" {
		return nil, fmt.Errorf("%w: integration_id is required", ErrValidation)
	}
	integration, err := s.integrations.GetIntegration(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	if integration.Provider != ProviderLinear {
		return nil, fmt.Errorf("%w: integration is not a linear provider", ErrValidation)
	}
	// Linear signs the body with HMAC-SHA256 using the shared secret. Verify
	// against the configured per-provider env secret; an unset secret refuses
	// the call rather than acting as an open relay.
	secret := s.webhookSecretFor(ProviderLinear)
	if secret == "" {
		return nil, fmt.Errorf("%w: no linear webhook secret configured", ErrUnauthorized)
	}
	if !VerifyLinearSignature(secret, []byte(in.PayloadJSON), in.Signature) {
		return nil, fmt.Errorf("%w: linear signature mismatch", ErrUnauthorized)
	}
	return s.processLinearWebhookPayload(ctx, integration, nil, in.PayloadJSON)
}

// HandleLinearResourceWebhook is the resource-scoped peer of HandleLinearWebhook.
// It verifies the delivery against the resource's stored per-resource webhook
// secret, falling back to the global LINEAR_WEBHOOK_SECRET env secret when the
// resource has no stored secret (manual env-configured setups). The resource
// must be a team-type resource belonging to the integration. per spec §6.
func (s *Service) HandleLinearResourceWebhook(ctx context.Context, integrationID, resourceID string, in LinearWebhookInput) (*LinearWebhookResult, error) {
	integrationID = strings.TrimSpace(integrationID)
	resourceID = strings.TrimSpace(resourceID)
	if integrationID == "" {
		return nil, fmt.Errorf("%w: integration_id is required", ErrValidation)
	}
	if resourceID == "" {
		return nil, fmt.Errorf("%w: resource_id is required", ErrValidation)
	}
	integration, err := s.integrations.GetIntegration(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	if integration.Provider != ProviderLinear {
		return nil, fmt.Errorf("%w: integration is not a linear provider", ErrValidation)
	}
	resource, err := s.resources.GetResource(ctx, integrationID, resourceID)
	if err != nil {
		return nil, err
	}
	if resource.ResourceType != ResourceTypeTeam {
		return nil, fmt.Errorf("%w: resource is not a team", ErrValidation)
	}

	// Resolve the secret to verify against: prefer the per-resource stored secret
	// (managed provisioning), fall back to the global env secret (manual setup).
	secret, err := resolveResourceWebhookSecret(resource)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot decrypt resource webhook secret", ErrUnauthorized)
	}
	if secret == "" {
		// Fall back to the global LINEAR_WEBHOOK_SECRET env secret (manual setup
		// without per-resource provisioning).
		secret = s.webhookSecretFor(ProviderLinear)
	}
	if secret == "" {
		return nil, fmt.Errorf("%w: no linear webhook secret configured (per-resource or global)", ErrUnauthorized)
	}
	if !VerifyLinearSignature(secret, []byte(in.PayloadJSON), in.Signature) {
		return nil, fmt.Errorf("%w: linear signature mismatch", ErrUnauthorized)
	}
	return s.processLinearWebhookPayload(ctx, integration, resource, in.PayloadJSON)
}

// processLinearWebhookPayload normalizes the Linear webhook payload and runs the
// shared tracker-status / comment-scope-drift commit pipeline. Called by both the
// integration-level and resource-level handlers after signature verification.
// resource may be nil when called from the integration-level handler.
func (s *Service) processLinearWebhookPayload(ctx context.Context, integration *Integration, resource *Resource, payloadJSON string) (*LinearWebhookResult, error) {
	nd, err := linear.ParseAndNormalize(payloadJSON)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(strings.TrimSpace(asLinearWebhookType(payloadJSON)), "Comment") {
		comment, err := linear.ParseCommentScopeDrift(payloadJSON)
		if err != nil {
			return nil, err
		}
		res, err := s.handleCommentScopeDrift(ctx, integration, resource, comment)
		if err != nil {
			return nil, err
		}
		return &LinearWebhookResult{
			WebhookEventID:   res.WebhookEventID,
			FeedbackEventIDs: res.FeedbackEventIDs,
			IntegrationID:    integration.ID,
			CorrelationID:    res.ChangeRequestID,
			Status:           res.Status,
			IgnoredReason:    res.IgnoredReason,
		}, nil
	}
	if nd.EventType != WebhookEventTrackerIssue {
		return &LinearWebhookResult{Status: WebhookStatusIgnored, IgnoredReason: "unsupported_linear_event"}, nil
	}
	// A deleted issue (Linear top-level action "remove") surfaces as a terminal
	// "removed" tracker state so the derived work-item chip clears and the link
	// drops out of the open set, instead of leaving a stale last state.
	if linearWebhookAction(payloadJSON) == "remove" {
		nd.RawState = TrackerStateRemoved
	}

	correlationID := ""
	if refs := parseFixesRefs(nd.Description, nd.Title); len(refs) > 0 {
		correlationID = refs[0]
	}

	var result *LinearWebhookResult
	txErr := s.txStore.WithTx(ctx, func(tx Store) error {
		created, event, err := tx.RecordWebhookEvent(ctx, WebhookEvent{
			IntegrationID:   integration.ID,
			Provider:        ProviderLinear,
			EventType:       WebhookEventTrackerIssue,
			ExternalEventID: nd.ExternalEventID,
			CorrelationID:   correlationID,
			PayloadJSON:     nd.RawPayload,
			Status:          WebhookStatusPending,
		})
		if err != nil {
			return err
		}
		if !created {
			result = &LinearWebhookResult{
				WebhookEventID: event.ID,
				IntegrationID:  integration.ID,
				Identifier:     nd.ExternalKey,
				TrackerState:   nd.RawState,
				Status:         event.Status,
				IgnoredReason:  "duplicate_webhook_event",
			}
			return nil
		}

		feedbackID, changed, err := s.recordTrackerStatusChange(ctx, tx, integration.ID, event.ID, correlationID, nd)
		if err != nil {
			return err
		}
		updated, err := tx.UpdateWebhookEventStatus(ctx, event.ID, WebhookStatusProcessed, "")
		if err != nil {
			return err
		}
		result = &LinearWebhookResult{
			WebhookEventID: updated.ID,
			IntegrationID:  integration.ID,
			Identifier:     nd.ExternalKey,
			TrackerState:   nd.RawState,
			CorrelationID:  correlationID,
			Status:         updated.Status,
		}
		if changed {
			result.FeedbackEventIDs = []string{feedbackID}
		} else {
			result.IgnoredReason = "tracker_status_unchanged"
		}
		return nil
	})
	if txErr != nil {
		_, _, _ = s.webhookEvents.RecordWebhookEvent(ctx, WebhookEvent{
			IntegrationID:   integration.ID,
			Provider:        ProviderLinear,
			EventType:       WebhookEventTrackerIssue,
			ExternalEventID: nd.ExternalEventID,
			CorrelationID:   correlationID,
			PayloadJSON:     nd.RawPayload,
			Status:          WebhookStatusFailed,
			Error:           txErr.Error(),
		})
		return nil, txErr
	}
	return result, nil
}

func asLinearWebhookType(raw string) string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	if s, ok := payload["type"].(string); ok {
		return s
	}
	return ""
}

// linearWebhookAction reads the top-level webhook action (create/update/remove),
// distinct from the issue's workflow state. Linear sends "remove" on deletion.
func linearWebhookAction(raw string) string {
	var p struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(p.Action))
}

// createTrackerFeedback emits the delivery.tracker_status_changed signal. The
// payload carries the tracker identifier (Linear LOY-128, GitLab #42), the raw
// workflow state, the issue url/title, and the correlation key — provider-neutral
// keys (provider taken from the normalized delivery) so downstream consumers do
// not branch on tracker.
func (s *Service) createTrackerFeedback(ctx context.Context, store Store, integrationID, eventID, correlationID string, nd normalizedDelivery) (*GovernanceFeedbackEvent, error) {
	body, err := json.Marshal(map[string]any{
		"provider":       nd.Provider,
		"identifier":     nd.ExternalKey,
		"tracker_state":  nd.RawState,
		"state_name":     nd.Action,
		"issue_url":      nd.URL,
		"issue_title":    nd.Title,
		"correlation_id": correlationID,
		"priority":       nd.Priority,
	})
	if err != nil {
		return nil, err
	}
	feedback := GovernanceFeedbackEvent{
		IntegrationID:  integrationID,
		WebhookEventID: eventID,
		EventType:      FeedbackEventTrackerStatusChanged,
		PayloadJSON:    string(body),
		Status:         FeedbackStatusPending,
	}
	// Correlate to the work item: prefer the persisted handoff link (by immutable
	// issue id/key, surviving description edits), fall back to the `fixes SPECGATE-{key}`
	// footer. No match ⇒ unlinked (the UI queries feedback by change_request_id).
	if cr := s.resolveTrackerWorkItem(ctx, store, integrationID, nd, correlationID); cr != nil {
		feedback.ChangeRequestID = cr.ID
		feedback.FeatureID = cr.FeatureID
	}
	return store.CreateGovernanceFeedbackEvent(ctx, feedback)
}
