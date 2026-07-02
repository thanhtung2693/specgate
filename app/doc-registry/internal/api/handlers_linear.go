package api

import (
	"context"
	"encoding/json"

	"github.com/danielgtaylor/huma/v2"

	"github.com/specgate/doc-registry/internal/integrations"
)

func (h *Handlers) HandleLinearWebhook(ctx context.Context, in *linearWebhookInput) (*linearWebhookOutput, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	if int64(len(in.RawBody)) > gitLabWebhookMaxBodyBytes {
		return nil, huma.Error413RequestEntityTooLarge("linear webhook payload exceeds size limit")
	}
	if !json.Valid(in.RawBody) {
		return nil, huma.Error400BadRequest("invalid linear webhook payload: not valid json")
	}
	result, err := h.Integrations.HandleLinearWebhook(ctx, in.ID, integrations.LinearWebhookInput{
		Signature:   in.LinearSignature,
		PayloadJSON: string(in.RawBody),
	})
	if err != nil {
		return nil, mapIntegrationError("handle linear webhook", err)
	}
	out := &linearWebhookOutput{}
	out.Body = *result
	return out, nil
}

func (h *Handlers) HandleLinearResourceWebhook(ctx context.Context, in *linearResourceWebhookInput) (*linearWebhookOutput, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	if int64(len(in.RawBody)) > gitLabWebhookMaxBodyBytes {
		return nil, huma.Error413RequestEntityTooLarge("linear webhook payload exceeds size limit")
	}
	if !json.Valid(in.RawBody) {
		return nil, huma.Error400BadRequest("invalid linear webhook payload: not valid json")
	}
	result, err := h.Integrations.HandleLinearResourceWebhook(ctx, in.ID, in.ResourceID, integrations.LinearWebhookInput{
		Signature:   in.LinearSignature,
		PayloadJSON: string(in.RawBody),
	})
	if err != nil {
		return nil, mapIntegrationError("handle linear resource webhook", err)
	}
	out := &linearWebhookOutput{}
	out.Body = *result
	return out, nil
}
