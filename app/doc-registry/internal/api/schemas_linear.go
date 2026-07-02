package api

import "github.com/specgate/doc-registry/internal/integrations"

// linearWebhookInput is the tracker peer of gitHubWebhookInput. Linear signs
// the raw body with HMAC-SHA256 and sends a bare hex digest in Linear-Signature
// (no sha256= prefix), so the service must see the exact bytes — RawBody
// preserves them.
type linearWebhookInput struct {
	ID              string `path:"id" doc:"integration id"`
	LinearSignature string `header:"Linear-Signature" doc:"bare hex HMAC-SHA256 of the body; verified against the configured LINEAR_WEBHOOK_SECRET"`
	RawBody         []byte
}

type linearWebhookOutput struct {
	Body integrations.LinearWebhookResult
}

// linearResourceWebhookInput is the resource-scoped peer of linearWebhookInput.
// Linear signs the raw body with HMAC-SHA256 and sends a bare hex digest in
// Linear-Signature; the service verifies against the resource's stored per-resource
// secret (with a fallback to LINEAR_WEBHOOK_SECRET for legacy setups).
type linearResourceWebhookInput struct {
	ID              string `path:"id" doc:"integration id"`
	ResourceID      string `path:"resource_id" doc:"resource id"`
	LinearSignature string `header:"Linear-Signature" doc:"bare hex HMAC-SHA256 of the body; verified against the per-resource secret (or global LINEAR_WEBHOOK_SECRET as fallback)"`
	RawBody         []byte
}
