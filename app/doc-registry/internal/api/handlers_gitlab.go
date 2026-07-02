package api

import (
	"context"
	"encoding/json"

	"github.com/danielgtaylor/huma/v2"

	"github.com/specgate/doc-registry/internal/integrations"
)

func (h *Handlers) HandleGitLabWebhook(ctx context.Context, in *gitLabWebhookInput) (*gitLabWebhookOutput, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	if int64(len(in.RawBody)) > gitLabWebhookMaxBodyBytes {
		return nil, huma.Error413RequestEntityTooLarge("gitlab webhook payload exceeds size limit")
	}
	// Parse-validate first so a malformed body short-circuits before the
	// service touches the DB. Service still reparses for typed access.
	if !json.Valid(in.RawBody) {
		return nil, huma.Error400BadRequest("invalid gitlab webhook payload: not valid json")
	}
	result, err := h.Integrations.HandleGitLabWebhook(ctx, in.ID, integrations.GitLabWebhookInput{
		EventHeader:      in.XGitlabEvent,
		EventUUID:        in.XGitlabEventUUID,
		WebhookID:        in.WebhookID,
		WebhookTimestamp: in.WebhookTimestamp,
		WebhookSignature: in.WebhookSignature,
		PayloadJSON:      string(in.RawBody),
	})
	if err != nil {
		return nil, mapIntegrationError("handle gitlab webhook", err)
	}
	out := &gitLabWebhookOutput{}
	out.Body = *result
	return out, nil
}

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
		Token:            in.XGitlabToken,
		PayloadJSON:      string(in.RawBody),
	})
	if err != nil {
		return nil, mapIntegrationError("handle gitlab resource webhook", err)
	}
	out := &gitLabWebhookOutput{}
	out.Body = *result
	return out, nil
}
