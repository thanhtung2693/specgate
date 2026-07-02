package integrations

import (
	"context"
	"fmt"

	"github.com/specgate/doc-registry/internal/notifications"
	"github.com/specgate/doc-registry/internal/webhookqueue"
)

// ProcessWebhookDelivery runs an enqueued webhook delivery (re-normalize + commit).
// It implements webhookqueue.Processor; the async worker calls it. Returning an
// error tells asynq to retry — and commitDelivery's dedup makes retrying an
// already-processed delivery a no-op, so retries are safe.
func (s *Service) ProcessWebhookDelivery(ctx context.Context, t webhookqueue.Task) error {
	switch t.Kind {
	case webhookqueue.KindResource:
		res, err := s.processResourceDelivery(ctx, t.Provider, t.IntegrationID, t.ResourceID, t.Inbound)
		if err != nil {
			return err
		}
		s.publishWebhookProcessed(t.Provider, res)
		return nil
	default:
		return fmt.Errorf("%w: unsupported webhook task kind %q", ErrValidation, t.Kind)
	}
}

// publishWebhookProcessed publishes a compact invalidation signal after a
// background webhook delivery commits. No-op when no publisher is wired.
func (s *Service) publishWebhookProcessed(provider string, res *GitLabWebhookResult) {
	if s.events == nil || res == nil {
		return
	}
	s.events.Publish(notifications.Event{
		Type: "webhook.delivery.processed",
		Data: map[string]any{
			"provider":           provider,
			"integration_id":     res.IntegrationID,
			"resource_id":        res.ResourceID,
			"feature_id":         res.FeatureID,
			"change_request_id":  res.ChangeRequestID,
			"status":             res.Status,
			"feedback_event_ids": res.FeedbackEventIDs,
		},
	})
}
