package api

import (
	"context"

	"github.com/specgate/doc-registry/internal/integrations"
)

func (h *Handlers) HandleGitHubResourceWebhook(ctx context.Context, in *gitHubResourceWebhookInput) (*gitHubWebhookOutput, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	result, err := h.Integrations.HandleResourceWebhook(ctx, in.ID, in.ResourceID, integrations.ProviderGitHub, integrations.InboundWebhook{
		EventHeader: in.XGitHubEvent,
		EventUUID:   in.XGitHubDelivery,
		Signature:   in.XHubSignature256,
		PayloadJSON: string(in.RawBody),
	})
	if err != nil {
		return nil, mapIntegrationError("handle github resource webhook", err)
	}
	out := &gitHubWebhookOutput{}
	out.Body = *result
	return out, nil
}
