package api

import (
	"context"
	"encoding/json"

	"github.com/danielgtaylor/huma/v2"

	"github.com/specgate/doc-registry/internal/integrations"
)

func (h *Handlers) HandleGitHubWebhook(ctx context.Context, in *gitHubWebhookInput) (*gitHubWebhookOutput, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	if int64(len(in.RawBody)) > gitLabWebhookMaxBodyBytes {
		return nil, huma.Error413RequestEntityTooLarge("github webhook payload exceeds size limit")
	}
	if !json.Valid(in.RawBody) {
		return nil, huma.Error400BadRequest("invalid github webhook payload: not valid json")
	}
	result, err := h.Integrations.HandleGitHubWebhook(ctx, in.ID, integrations.GitHubWebhookInput{
		EventHeader: in.XGitHubEvent,
		DeliveryID:  in.XGitHubDelivery,
		Signature:   in.XHubSignature256,
		PayloadJSON: string(in.RawBody),
	})
	if err != nil {
		return nil, mapIntegrationError("handle github webhook", err)
	}
	out := &gitHubWebhookOutput{}
	out.Body = *result
	return out, nil
}

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
