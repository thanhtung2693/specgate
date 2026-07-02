package api

import "github.com/specgate/doc-registry/internal/integrations"

type integrationListBody struct {
	Body struct {
		Items []integrations.Integration `json:"items"`
	}
}

type integrationBody struct {
	Body integrations.Integration
}

type integrationIDInput struct {
	ID string `path:"id" doc:"integration id"`
}

type integrationResourceIDInput struct {
	ID         string `path:"id" doc:"integration id"`
	ResourceID string `path:"resource_id" doc:"resource id"`
}

// webhookSecretBody returns the per-integration inbound-webhook secret in
// plaintext to the trusted internal caller (same network-boundary trust model as
// GET /mcp/api-key). For GitHub it is our generated secret to copy into GitHub;
// for GitLab it is the stored signing token (empty until the user pastes one).
type webhookSecretBody struct {
	Body struct {
		Secret           string `json:"secret" doc:"the inbound-webhook secret (GitHub: copy into GitHub; GitLab: the stored signing token)"`
		HasWebhookSecret bool   `json:"has_webhook_secret" doc:"true once a secret/signing token is stored for this integration"`
	}
}

// setWebhookSecretInput sets a user-provided webhook secret — GitLab's pasted
// whsec_ signing token (format-validated), or a custom GitHub secret.
type setWebhookSecretInput struct {
	ID   string `path:"id" doc:"integration id"`
	Body struct {
		Secret string `json:"secret" doc:"the webhook secret to store (GitLab: a whsec_ signing token copied from GitLab)"`
	}
}

// createIntegrationInput is a dedicated DTO so the create surface stays minimal
// and disjoint from the public Integration struct (reused as the GET/list
// response). Inbound-webhook verification uses a per-provider env secret, so no
// secret is supplied at creation time.
type createIntegrationInput struct {
	Body struct {
		Provider   string `json:"provider"`
		Name       string `json:"name"`
		Status     string `json:"status,omitempty"`
		BaseURL    string `json:"base_url,omitempty"`
		ConfigJSON string `json:"config_json,omitempty"`
	}
}

type updateIntegrationInput struct {
	ID   string `path:"id" doc:"integration id"`
	Body integrations.Integration
}

type resourceListBody struct {
	Body struct {
		Items []integrations.Resource `json:"items"`
	}
}

type resourceBody struct {
	Body integrations.Resource
}

type createIntegrationResourceInput struct {
	ID   string `path:"id" doc:"integration id"`
	Body integrations.Resource
}

type deleteIntegrationResourceOutput struct{}

// listIntegrationReposInput drives the connect-time repo picker: list the
// repos/projects the integration's token can access (GitLab/GitHub only).
type listIntegrationReposInput struct {
	ID     string `path:"id" doc:"integration id"`
	Search string `query:"search" doc:"optional case-insensitive name filter"`
	Limit  int    `query:"limit" doc:"max results (default 50, max 100)"`
}

type repoSummaryListBody struct {
	Body struct {
		Items []integrations.RepoSummary `json:"items"`
	}
}

type linearTeamSummaryListBody struct {
	Body struct {
		Items []integrations.LinearTeamSummary `json:"items"`
	}
}

type linearProjectSummaryListBody struct {
	Body struct {
		Items []integrations.LinearProjectSummary `json:"items"`
	}
}

type listLinearProjectsInput struct {
	ID     string `path:"id" doc:"integration id"`
	TeamID string `query:"team_id" doc:"required linear team id"`
}

type listWebhookEventsInput struct {
	ID         string `path:"id" doc:"integration id"`
	ResourceID string `query:"resource_id" doc:"optional resource id filter"`
	Status     string `query:"status" doc:"optional webhook status filter"`
	Limit      int    `query:"limit" doc:"max events to return (default 100, max 200)"`
}

type webhookEventListBody struct {
	Body struct {
		Items []integrations.WebhookEvent `json:"items"`
	}
}

type webhookEventBody struct {
	Body integrations.WebhookEvent
}

type recordWebhookEventInput struct {
	ID   string `path:"id" doc:"integration id"`
	Body integrations.WebhookEvent
}

// deleteIntegrationOutput is intentionally empty — the route returns 204 with
// no body. Cascade behavior is documented on Service.Delete.
type deleteIntegrationOutput struct{}

// Per-provider webhook input/output structs live in schemas_{gitlab,github,
// linear}.go; their handlers live in handlers_{gitlab,github,linear}.go.

// setApiTokenInput carries a write-only provider API token. The plaintext
// is sent once, AES-256-GCM encrypted at rest, and never returned by GET (the
// derived has_api_token flag reports presence instead).
type setApiTokenInput struct {
	ID   string `path:"id" doc:"integration id"`
	Body struct {
		APIToken string `json:"api_token" doc:"Provider API token (Linear GraphQL / GitLab REST); stored AES-256-GCM encrypted, never returned"`
	}
}

type setApiTokenOutput struct{}

type beginIntegrationOAuthInput struct {
	ID   string `path:"id" doc:"integration id"`
	Body struct {
		RedirectTarget string `json:"redirect_target,omitempty" doc:"Optional app-relative path to redirect back to after OAuth completion"`
	}
}

type beginIntegrationOAuthOutput struct {
	Body struct {
		AuthorizeURL string `json:"authorize_url"`
	}
}

// beginPendingOAuthInput starts OAuth for a not-yet-created integration: the
// integration is created only when the callback succeeds (create-on-callback),
// so a cancelled/failed connect leaves no orphan row.
type beginPendingOAuthInput struct {
	Body struct {
		Provider       string `json:"provider"`
		Name           string `json:"name"`
		BaseURL        string `json:"base_url,omitempty"`
		ConfigJSON     string `json:"config_json,omitempty"`
		RedirectTarget string `json:"redirect_target,omitempty" doc:"Optional app-relative path to redirect back to after OAuth completion"`
	}
}

type disconnectIntegrationOAuthOutput struct{}

// changeRequestTrackerLinksInput drives GET .../tracker-links: the issue links a
// handoff created for a work item, for the "linked issues" surface.
type changeRequestTrackerLinksInput struct {
	ID string `path:"id" doc:"change request id"`
}

type trackerLinkDTO struct {
	Lane         string `json:"lane,omitempty" doc:"handoff lane (\"fe\"/\"be\"), empty for a full handoff"`
	Identifier   string `json:"identifier" doc:"tracker issue key (e.g. ENG-123)"`
	URL          string `json:"url"`
	State        string `json:"state" doc:"link lifecycle: opened | closed | removed"`
	TrackerState string `json:"tracker_state,omitempty" doc:"last raw provider workflow state"`
}

type changeRequestTrackerLinksOutput struct {
	Body struct {
		Items []trackerLinkDTO `json:"items"`
	}
}

type listGovernanceFeedbackEventsInput struct {
	Status          string `query:"status" doc:"optional feedback status filter"`
	ChangeRequestID string `query:"change_request_id" doc:"optional work item filter"`
	ArtifactID      string `query:"artifact_id" doc:"optional artifact filter"`
	Limit           int    `query:"limit" doc:"max events to return (default 100, max 200)"`
}

type governanceFeedbackEventListBody struct {
	Body struct {
		Items []integrations.GovernanceFeedbackEvent `json:"items"`
	}
}

// UpdateGovernanceFeedbackEventStatusInput sets a feedback event's triage status.
type UpdateGovernanceFeedbackEventStatusInput struct {
	ID   string `path:"id"`
	Body struct {
		Status string `json:"status" enum:"accepted,rejected" doc:"canonical triage status: accepted (resolve) or rejected (dismiss)"`
		Reason string `json:"reason,omitempty"`
	}
}

// governanceFeedbackEventBody wraps one event with canonical status (same shape as a list item).
type governanceFeedbackEventBody struct {
	Body integrations.GovernanceFeedbackEvent
}
