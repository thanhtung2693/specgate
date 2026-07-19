package api

import "github.com/specgate/doc-registry/internal/integrations"

type linearWebhookOutput struct {
	Body integrations.LinearWebhookResult
}

// linearResourceWebhookInput is the resource-scoped peer of linearWebhookInput.
// Linear signs the raw body with HMAC-SHA256 and sends a bare hex digest in
// Linear-Signature; the service verifies against the resource's stored per-resource
// secret.
type linearResourceWebhookInput struct {
	ID              string `path:"id" doc:"integration id"`
	ResourceID      string `path:"resource_id" doc:"resource id"`
	LinearSignature string `header:"Linear-Signature" doc:"bare hex HMAC-SHA256 of the body; verified against the per-resource secret"`
	LinearDelivery  string `header:"Linear-Delivery" doc:"unique Linear delivery UUID used for idempotency"`
	RawBody         []byte
}
