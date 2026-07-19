package api

import "github.com/specgate/doc-registry/internal/integrations"

type gitHubResourceWebhookInput struct {
	ID               string `path:"id" doc:"integration id"`
	ResourceID       string `path:"resource_id" doc:"resource id"`
	XGitHubEvent     string `header:"X-GitHub-Event" doc:"GitHub event name; only pull_request and issue_comment are processed"`
	XGitHubDelivery  string `header:"X-GitHub-Delivery" doc:"GitHub delivery UUID (replay/dedup key)"`
	XHubSignature256 string `header:"X-Hub-Signature-256" doc:"HMAC-SHA256 of the body as sha256=<hex>; verified against the per-resource GitHub webhook secret"`
	RawBody          []byte
}

type gitHubWebhookOutput struct {
	Body integrations.GitLabWebhookResult
}
