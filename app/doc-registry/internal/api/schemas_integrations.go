package api

import (
	"time"

	"github.com/specgate/doc-registry/internal/integrations"
)

type integrationListBody struct {
	Body struct {
		Items []integrations.Integration `json:"items"`
	}
}

type listIntegrationsInput struct {
	WorkspaceID string `query:"workspace_id" doc:"workspace scope"`
}

type integrationBody struct {
	Body integrations.Integration
}

type integrationIDInput struct {
	ID          string `path:"id" doc:"integration id"`
	WorkspaceID string `query:"workspace_id" doc:"workspace scope"`
}

type integrationResourceIDInput struct {
	ID          string `path:"id" doc:"integration id"`
	ResourceID  string `path:"resource_id" doc:"resource id"`
	WorkspaceID string `query:"workspace_id" doc:"workspace scope"`
}

// createIntegrationInput is a dedicated DTO so the create surface stays minimal
// and disjoint from the public Integration struct (reused as the GET/list
// response). Inbound-webhook verification uses a managed selected-resource
// secret, so no secret is supplied at creation time.
type createIntegrationInput struct {
	Body struct {
		WorkspaceID string `json:"workspace_id,omitempty"`
		Provider    string `json:"provider"`
		Name        string `json:"name"`
		Status      string `json:"status,omitempty"`
		BaseURL     string `json:"base_url,omitempty"`
		ConfigJSON  string `json:"config_json,omitempty"`
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
	ID          string `path:"id" doc:"integration id"`
	WorkspaceID string `query:"workspace_id" doc:"workspace scope"`
	Body        integrations.Resource
}

type deleteIntegrationResourceOutput struct{}

// listIntegrationReposInput drives the connect-time repo picker: list the
// repos/projects the integration's token can access (GitLab/GitHub only).
type listIntegrationReposInput struct {
	ID          string `path:"id" doc:"integration id"`
	WorkspaceID string `query:"workspace_id" doc:"workspace scope"`
	Search      string `query:"search" doc:"optional case-insensitive name filter"`
	Limit       int    `query:"limit" doc:"max results (default 50, max 100)"`
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
	ID          string `path:"id" doc:"integration id"`
	WorkspaceID string `query:"workspace_id" doc:"workspace scope"`
	TeamID      string `query:"team_id" doc:"required linear team id"`
}

type listWebhookEventsInput struct {
	ID          string `path:"id" doc:"integration id"`
	WorkspaceID string `query:"workspace_id" doc:"workspace scope"`
	ResourceID  string `query:"resource_id" doc:"optional resource id filter"`
	Status      string `query:"status" doc:"optional webhook status filter"`
	Limit       int    `query:"limit" doc:"max events to return (default 100, max 200)"`
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
	ID          string `path:"id" doc:"integration id"`
	WorkspaceID string `query:"workspace_id" doc:"workspace scope"`
	Body        integrations.WebhookEvent
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
	ID          string `path:"id" doc:"integration id"`
	WorkspaceID string `query:"workspace_id" doc:"workspace scope"`
	Body        struct {
		APIToken string `json:"api_token" doc:"Provider API token (Linear GraphQL / GitLab REST); stored AES-256-GCM encrypted, never returned"`
	}
}

type setApiTokenOutput struct{}

type beginIntegrationOAuthInput struct {
	ID          string `path:"id" doc:"integration id"`
	WorkspaceID string `query:"workspace_id" doc:"workspace scope"`
	Body        struct {
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
		WorkspaceID    string `json:"workspace_id,omitempty"`
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
	ID          string `path:"id" doc:"change request id"`
	WorkspaceID string `query:"workspace_id" doc:"selected workspace"`
}

type trackerLinkDTO struct {
	Identifier   string `json:"identifier" doc:"tracker issue key (e.g. ENG-123)"`
	URL          string `json:"url"`
	State        string `json:"state" doc:"link lifecycle: opened | closed | removed"`
	TrackerState string `json:"tracker_state,omitempty" doc:"last raw provider workflow state"`
}

type linearHandoffInput struct {
	ID          string `path:"id" doc:"change request id"`
	WorkspaceID string `query:"workspace_id" required:"true" doc:"selected workspace"`
	Body        struct {
		IntegrationID string `json:"integration_id"`
		ResourceID    string `json:"resource_id"`
	}
}

type linearHandoffDTO struct {
	Identifier   string `json:"identifier"`
	URL          string `json:"url"`
	State        string `json:"state"`
	TrackerState string `json:"tracker_state"`
}

type linearHandoffOutput struct {
	Body struct {
		Created bool             `json:"created"`
		Link    linearHandoffDTO `json:"link"`
	}
}

type changeRequestTrackerLinksOutput struct {
	Body struct {
		Items []trackerLinkDTO `json:"items"`
	}
}

// changeRequestDeliveryLinksInput drives GET .../delivery-links: persisted
// repository deliveries for a work item in the selected workspace.
type changeRequestDeliveryLinksInput struct {
	ID          string `path:"id" doc:"change request id"`
	WorkspaceID string `query:"workspace_id" required:"true" doc:"selected workspace"`
}

type deliveryLinkDTO struct {
	ExternalKey    string    `json:"external_key"`
	Title          string    `json:"title"`
	URL            string    `json:"url"`
	State          string    `json:"state"`
	SourceBranch   string    `json:"source_branch"`
	TargetBranch   string    `json:"target_branch"`
	HeadSHA        string    `json:"head_sha"`
	MergeCommitSHA string    `json:"merge_commit_sha"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type changeRequestDeliveryLinksOutput struct {
	Body struct {
		Items []deliveryLinkDTO `json:"items"`
	}
}

type listGovernanceFeedbackEventsInput struct {
	WorkspaceID     string `query:"workspace_id" doc:"workspace scope"`
	Status          string `query:"status" doc:"optional feedback status filter"`
	ChangeRequestID string `query:"change_request_id" doc:"optional work item filter"`
	ArtifactID      string `query:"artifact_id" doc:"optional artifact filter"`
	EventType       string `query:"event_type" doc:"optional exact event-type filter"`
	Limit           int    `query:"limit" doc:"max events to return (default 100, max 200)"`
}

type governanceFeedbackEventListBody struct {
	Body struct {
		Items []integrations.GovernanceFeedbackEvent `json:"items"`
	}
}

// UpdateGovernanceFeedbackEventStatusInput sets a feedback event's triage status.
type UpdateGovernanceFeedbackEventStatusInput struct {
	ID          string `path:"id"`
	WorkspaceID string `query:"workspace_id" doc:"workspace scope"`
	Body        struct {
		Status string `json:"status" enum:"accepted,rejected" doc:"canonical triage status: accepted (resolve) or rejected (dismiss)"`
		Reason string `json:"reason,omitempty"`
	}
}

// governanceFeedbackEventBody wraps one event with canonical status (same shape as a list item).
type governanceFeedbackEventBody struct {
	Body integrations.GovernanceFeedbackEvent
}
