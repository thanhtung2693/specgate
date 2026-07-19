package api

import "github.com/specgate/doc-registry/internal/integrations"

type gitLabResourceWebhookInput struct {
	ID               string `path:"id" doc:"integration id"`
	ResourceID       string `path:"resource_id" doc:"resource id"`
	XGitlabEvent     string `header:"X-Gitlab-Event" doc:"GitLab event name"`
	XGitlabEventUUID string `header:"X-Gitlab-Event-UUID" doc:"GitLab event UUID when provided"`
	WebhookID        string `header:"webhook-id" doc:"Standard Webhooks message id (signed)"`
	WebhookTimestamp string `header:"webhook-timestamp" doc:"Standard Webhooks unix-seconds timestamp (signed; recency-checked)"`
	WebhookSignature string `header:"webhook-signature" doc:"Standard Webhooks signatures (space-separated v1,<base64>) over {id}.{timestamp}.{body}; verified against the resource signing token"`
	RawBody          []byte
}

type gitLabWebhookOutput struct {
	Body integrations.GitLabWebhookResult
}
