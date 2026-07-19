package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/specgate/doc-registry/internal/integrations/linear"
	"github.com/specgate/doc-registry/internal/webhookqueue"
)

// SetApiToken stores a provider API token for supported provider calls. The
// token is AES-256-GCM encrypted before storage and
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

// ResolveAPIToken returns the decrypted provider API token for server-side
// provider calls. The plaintext never crosses the network boundary. A disabled
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

// HandleLinearResourceWebhook verifies a resource-scoped Linear delivery.
// It verifies the delivery against the resource's stored per-resource webhook
// secret. The resource must be a team-type resource belonging to the
// integration. per spec §6.
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
	if integration.Status != StatusConnected {
		return nil, fmt.Errorf("%w: integration is not connected", ErrValidation)
	}
	ctx, err = bindIntegrationWorkspace(ctx, integration)
	if err != nil {
		return nil, err
	}
	resource, err := s.resources.GetResource(ctx, integrationID, resourceID)
	if err != nil {
		return nil, err
	}
	if resource.ResourceType != ResourceTypeTeam {
		return nil, fmt.Errorf("%w: resource is not a team", ErrValidation)
	}

	secret, err := resolveResourceWebhookSecret(resource)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot decrypt resource webhook secret", ErrUnauthorized)
	}
	if secret == "" {
		return nil, fmt.Errorf("%w: no resource webhook secret configured", ErrUnauthorized)
	}
	if !VerifyLinearSignature(secret, []byte(in.PayloadJSON), in.Signature) {
		return nil, fmt.Errorf("%w: linear signature mismatch", ErrUnauthorized)
	}
	if err := validateLinearWebhookTimestamp(in.PayloadJSON, time.Now()); err != nil {
		return nil, err
	}
	deliveryID := strings.TrimSpace(in.DeliveryID)
	if s.enqueuer != nil {
		workspaceID := WorkspaceID(ctx)
		if workspaceID == "" {
			workspaceID = strings.TrimSpace(integration.WorkspaceID)
		}
		if err := s.enqueuer.EnqueueWebhookDelivery(ctx, webhookqueue.Task{
			WorkspaceID: workspaceID, Kind: webhookqueue.KindResource, Provider: ProviderLinear,
			IntegrationID: integrationID, ResourceID: resourceID,
			Inbound: InboundWebhook{EventUUID: deliveryID, PayloadJSON: in.PayloadJSON},
		}); err != nil {
			return nil, fmt.Errorf("%w: enqueue webhook delivery: %v", ErrUpstream, err)
		}
		return &LinearWebhookResult{
			IntegrationID: integration.ID, Status: WebhookStatusPending, IgnoredReason: "queued",
		}, nil
	}
	return s.processLinearWebhookPayloadWithDeliveryID(ctx, integration, resource, in.PayloadJSON, deliveryID)
}

const linearWebhookReplayTolerance = time.Minute

func validateLinearWebhookTimestamp(payloadJSON string, now time.Time) error {
	var envelope struct {
		WebhookTimestamp int64 `json:"webhookTimestamp"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &envelope); err != nil || envelope.WebhookTimestamp <= 0 {
		return fmt.Errorf("%w: linear webhook timestamp is missing or invalid", ErrUnauthorized)
	}
	sentAt := time.UnixMilli(envelope.WebhookTimestamp)
	age := now.Sub(sentAt)
	if age < -linearWebhookReplayTolerance || age > linearWebhookReplayTolerance {
		return fmt.Errorf("%w: linear webhook timestamp is outside the replay window", ErrUnauthorized)
	}
	return nil
}

// processLinearWebhookPayload normalizes the Linear webhook payload and runs the
// shared tracker-status / comment-scope-drift commit pipeline after
// resource-scoped signature verification.
func (s *Service) processLinearWebhookPayload(ctx context.Context, integration *Integration, resource *Resource, payloadJSON string) (*LinearWebhookResult, error) {
	return s.processLinearWebhookPayloadWithDeliveryID(ctx, integration, resource, payloadJSON, "")
}

func (s *Service) processLinearWebhookPayloadWithDeliveryID(ctx context.Context, integration *Integration, resource *Resource, payloadJSON, deliveryID string) (*LinearWebhookResult, error) {
	nd, err := linear.ParseAndNormalize(payloadJSON)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(strings.TrimSpace(asLinearWebhookType(payloadJSON)), "Comment") {
		comment, err := linear.ParseCommentScopeDrift(payloadJSON)
		if err != nil {
			return nil, err
		}
		if deliveryID = strings.TrimSpace(deliveryID); deliveryID != "" {
			comment.ExternalEventID = deliveryID
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
	if deliveryID = strings.TrimSpace(deliveryID); deliveryID != "" {
		nd.ExternalEventID = deliveryID
	}
	// A deleted issue (Linear top-level action "remove") surfaces as a terminal
	// "removed" tracker state so the derived work-item chip clears and the link
	// drops out of the open set, instead of leaving a stale last state.
	if linearWebhookAction(payloadJSON) == "remove" {
		nd.RawState = TrackerStateRemoved
		nd.TrackerLifecycle = TrackerStateRemoved
	}

	correlationID := ""
	if refs := parseWorkRefMarkers(nd.Description, nd.Title); len(refs) > 0 {
		correlationID = refs[0]
	}

	var result *LinearWebhookResult
	txErr := s.txStore.WithTx(ctx, func(tx Store) error {
		created, event, err := tx.RecordWebhookEvent(ctx, WebhookEvent{
			IntegrationID:   integration.ID,
			ResourceID:      resource.ID,
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
		event, process, err := claimWebhookEventForProcessing(ctx, tx, created, event)
		if err != nil {
			return err
		}
		if !process {
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

		feedbackID, changed, err := s.recordTrackerStatusChange(ctx, tx, integration.ID, resource.ID, event.ID, correlationID, nd)
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
			ResourceID:      resource.ID,
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
// payload carries the Linear tracker identifier, the raw
// workflow state, the issue url/title, and the correlation key — provider-neutral
// keys (provider taken from the normalized delivery) so downstream consumers do
// not branch on tracker.
func (s *Service) createTrackerFeedback(ctx context.Context, store Store, integrationID, resourceID, eventID, correlationID string, nd normalizedDelivery) (*GovernanceFeedbackEvent, error) {
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
		ResourceID:     resourceID,
		WebhookEventID: eventID,
		EventType:      FeedbackEventTrackerStatusChanged,
		PayloadJSON:    string(body),
		Status:         FeedbackStatusReceived,
	}
	// Correlate to the work item: prefer the persisted handoff link (by immutable
	// issue id/key, surviving description edits), then use the exact work marker.
	// No match ⇒ unlinked (the UI queries feedback by change_request_id).
	if cr := s.resolveTrackerWorkItem(ctx, store, integrationID, nd, correlationID); cr != nil {
		feedback.ChangeRequestID = cr.ID
		feedback.FeatureID = cr.FeatureID
	}
	return store.CreateGovernanceFeedbackEvent(ctx, feedback)
}
