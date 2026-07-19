package api

import (
	"context"

	"github.com/specgate/doc-registry/internal/integrations"
)

func (h *Handlers) HandleGitLabResourceWebhook(ctx context.Context, in *gitLabResourceWebhookInput) (*gitLabWebhookOutput, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	result, err := h.Integrations.HandleResourceWebhook(ctx, in.ID, in.ResourceID, integrations.ProviderGitLab, integrations.InboundWebhook{
		EventHeader:      in.XGitlabEvent,
		EventUUID:        in.XGitlabEventUUID,
		WebhookID:        in.WebhookID,
		WebhookTimestamp: in.WebhookTimestamp,
		WebhookSignature: in.WebhookSignature,
		PayloadJSON:      string(in.RawBody),
	})
	if err != nil {
		return nil, mapIntegrationError("handle gitlab resource webhook", err)
	}
	out := &gitLabWebhookOutput{}
	out.Body = *result
	return out, nil
}
