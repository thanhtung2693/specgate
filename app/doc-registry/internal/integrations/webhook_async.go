package integrations

import (
	"context"
	"fmt"
	"strings"

	"github.com/specgate/doc-registry/internal/webhookqueue"
)

// ProcessWebhookDelivery runs an enqueued webhook delivery (re-normalize + commit).
// It implements webhookqueue.Processor; the async worker calls it. Returning an
// error tells asynq to retry — and commitDelivery's dedup makes retrying an
// already-processed delivery a no-op, so retries are safe.
func (s *Service) ProcessWebhookDelivery(ctx context.Context, t webhookqueue.Task) error {
	workspaceID := strings.TrimSpace(t.WorkspaceID)
	if workspaceID == "" {
		return fmt.Errorf("%w: webhook task workspace_id is required", ErrValidation)
	}
	ctx = WithWorkspace(ctx, workspaceID)
	switch t.Kind {
	case webhookqueue.KindResource:
		if t.Provider == ProviderLinear {
			integration, resource, err := s.loadWebhookResource(ctx, t.IntegrationID, t.ResourceID, ProviderLinear, ResourceTypeTeam)
			if err != nil {
				return err
			}
			_, err = s.processLinearWebhookPayloadWithDeliveryID(ctx, integration, resource, t.Inbound.PayloadJSON, t.Inbound.EventUUID)
			return err
		}
		_, err := s.processResourceDelivery(ctx, t.Provider, t.IntegrationID, t.ResourceID, t.Inbound)
		return err
	default:
		return fmt.Errorf("%w: unsupported webhook task kind %q", ErrValidation, t.Kind)
	}
}
