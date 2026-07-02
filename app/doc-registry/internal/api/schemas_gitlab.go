package api

import "github.com/specgate/doc-registry/internal/integrations"

// gitLabWebhookInput captures raw bytes via Huma's RawBody so the service can
// SHA256-hash the payload (replay key when X-Gitlab-Event-UUID is missing) and
// validate the Standard Webhooks signature over the exact bytes before any DB
// write. Body size is capped per-operation in router.go (MaxBodyBytes) so an
// oversized push event cannot exhaust memory before the signature check runs.
type gitLabWebhookInput struct {
	ID               string `path:"id" doc:"integration id"`
	XGitlabEvent     string `header:"X-Gitlab-Event" doc:"GitLab event name"`
	XGitlabEventUUID string `header:"X-Gitlab-Event-UUID" doc:"GitLab event UUID when provided"`
	WebhookID        string `header:"webhook-id" doc:"Standard Webhooks message id (signed)"`
	WebhookTimestamp string `header:"webhook-timestamp" doc:"Standard Webhooks unix-seconds timestamp (signed; recency-checked)"`
	WebhookSignature string `header:"webhook-signature" doc:"Standard Webhooks signatures (space-separated v1,<base64>) over {id}.{timestamp}.{body}; verified against the integration's signing token"`
	RawBody          []byte
}

type gitLabResourceWebhookInput struct {
	ID               string `path:"id" doc:"integration id"`
	ResourceID       string `path:"resource_id" doc:"resource id"`
	XGitlabEvent     string `header:"X-Gitlab-Event" doc:"GitLab event name"`
	XGitlabEventUUID string `header:"X-Gitlab-Event-UUID" doc:"GitLab event UUID when provided"`
	WebhookID        string `header:"webhook-id" doc:"Standard Webhooks message id (signed)"`
	WebhookTimestamp string `header:"webhook-timestamp" doc:"Standard Webhooks unix-seconds timestamp (signed; recency-checked)"`
	WebhookSignature string `header:"webhook-signature" doc:"Standard Webhooks signatures (space-separated v1,<base64>) over {id}.{timestamp}.{body}; verified against the resource signing token (GitLab 19.0+)"`
	XGitlabToken     string `header:"X-Gitlab-Token" doc:"Legacy secret token (verbatim); verified when the resource stored a secret token instead of a signing token (GitLab < 19.0)"`
	RawBody          []byte
}

type gitLabWebhookOutput struct {
	Body integrations.GitLabWebhookResult
}
