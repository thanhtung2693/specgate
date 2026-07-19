package api

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
)

// gitLabWebhookMaxBodyBytes caps webhook payload size — see schemas_integrations.go.
const gitLabWebhookMaxBodyBytes = 1 << 20 // 1 MiB

func requiredIntegrationWorkspaceContext(ctx context.Context, workspaceID string) (context.Context, error) {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return nil, err
	}
	return integrations.WithWorkspace(ctx, workspaceID), nil
}

func (h *Handlers) ListIntegrations(ctx context.Context, in *listIntegrationsInput) (*integrationListBody, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = integrations.WithWorkspace(ctx, workspaceID)
	items, err := h.Integrations.List(ctx)
	if err != nil {
		return nil, mapIntegrationError("list integrations", err)
	}
	out := &integrationListBody{}
	out.Body.Items = items
	return out, nil
}

func (h *Handlers) CreateIntegration(ctx context.Context, in *createIntegrationInput) (*integrationBody, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	workspaceID, err := requireWorkspaceID(in.Body.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = integrations.WithWorkspace(ctx, workspaceID)
	created, err := h.Integrations.Create(ctx, integrations.CreateInput{
		WorkspaceID: workspaceID,
		Provider:    in.Body.Provider,
		Name:        in.Body.Name,
		Status:      in.Body.Status,
		BaseURL:     in.Body.BaseURL,
		ConfigJSON:  in.Body.ConfigJSON,
	})
	if err != nil {
		return nil, mapIntegrationError("create integration", err)
	}
	out := &integrationBody{}
	out.Body = *created
	return out, nil
}

func (h *Handlers) ListIntegrationRepos(ctx context.Context, in *listIntegrationReposInput) (*repoSummaryListBody, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	ctx, err := requiredIntegrationWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	items, err := h.Integrations.ListAccessibleRepos(ctx, in.ID, in.Search, in.Limit)
	if err != nil {
		return nil, mapIntegrationError("list integration repos", err)
	}
	out := &repoSummaryListBody{}
	out.Body.Items = items
	return out, nil
}

func (h *Handlers) ListLinearTeams(ctx context.Context, in *integrationIDInput) (*linearTeamSummaryListBody, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	ctx, err := requiredIntegrationWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	items, err := h.Integrations.ListLinearTeams(ctx, in.ID)
	if err != nil {
		return nil, mapIntegrationError("list linear teams", err)
	}
	out := &linearTeamSummaryListBody{}
	out.Body.Items = items
	return out, nil
}

func (h *Handlers) ListLinearProjects(ctx context.Context, in *listLinearProjectsInput) (*linearProjectSummaryListBody, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	ctx, err := requiredIntegrationWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	items, err := h.Integrations.ListLinearProjects(ctx, in.ID, in.TeamID)
	if err != nil {
		return nil, mapIntegrationError("list linear projects", err)
	}
	out := &linearProjectSummaryListBody{}
	out.Body.Items = items
	return out, nil
}

func (h *Handlers) GetIntegration(ctx context.Context, in *integrationIDInput) (*integrationBody, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = integrations.WithWorkspace(ctx, workspaceID)
	item, err := h.Integrations.Get(ctx, in.ID)
	if err != nil {
		return nil, mapIntegrationError("get integration", err)
	}
	out := &integrationBody{}
	out.Body = *item
	return out, nil
}

func (h *Handlers) DeleteIntegration(ctx context.Context, in *integrationIDInput) (*deleteIntegrationOutput, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = integrations.WithWorkspace(ctx, workspaceID)
	if err := h.Integrations.Delete(ctx, in.ID); err != nil {
		return nil, mapIntegrationError("delete integration", err)
	}
	return &deleteIntegrationOutput{}, nil
}

func (h *Handlers) UpdateIntegration(ctx context.Context, in *updateIntegrationInput) (*integrationBody, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	workspaceID, err := requireWorkspaceID(in.Body.WorkspaceID)
	if err != nil {
		return nil, err
	}
	in.Body.WorkspaceID = workspaceID
	ctx = integrations.WithWorkspace(ctx, workspaceID)
	updated, err := h.Integrations.Update(ctx, in.ID, in.Body)
	if err != nil {
		return nil, mapIntegrationError("update integration", err)
	}
	out := &integrationBody{}
	out.Body = *updated
	return out, nil
}

func (h *Handlers) ListIntegrationResources(ctx context.Context, in *integrationIDInput) (*resourceListBody, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	ctx, err := requiredIntegrationWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	items, err := h.Integrations.ListResources(ctx, in.ID)
	if err != nil {
		return nil, mapIntegrationError("list integration resources", err)
	}
	out := &resourceListBody{}
	out.Body.Items = items
	return out, nil
}

func (h *Handlers) CreateIntegrationResource(ctx context.Context, in *createIntegrationResourceInput) (*resourceBody, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	ctx, err := requiredIntegrationWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	created, err := h.Integrations.CreateResourceAndProvisionWebhook(ctx, in.ID, in.Body, oauthCallbackBaseFromContext(ctx))
	if err != nil {
		return nil, mapIntegrationError("create integration resource", err)
	}
	out := &resourceBody{}
	out.Body = *created
	return out, nil
}

func (h *Handlers) ReprovisionIntegrationResourceWebhook(ctx context.Context, in *integrationResourceIDInput) (*resourceBody, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	ctx, err := requiredIntegrationWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	updated, err := h.Integrations.ReprovisionResourceWebhook(ctx, in.ID, in.ResourceID, oauthCallbackBaseFromContext(ctx))
	if err != nil {
		return nil, mapIntegrationError("reprovision integration resource webhook", err)
	}
	out := &resourceBody{}
	out.Body = *updated
	return out, nil
}

func (h *Handlers) DeleteIntegrationResource(ctx context.Context, in *integrationResourceIDInput) (*deleteIntegrationResourceOutput, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	ctx, err := requiredIntegrationWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if err := h.Integrations.DeleteResource(ctx, in.ID, in.ResourceID); err != nil {
		return nil, mapIntegrationError("delete integration resource", err)
	}
	return &deleteIntegrationResourceOutput{}, nil
}

func (h *Handlers) ListIntegrationWebhookEvents(ctx context.Context, in *listWebhookEventsInput) (*webhookEventListBody, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	ctx, err := requiredIntegrationWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	items, err := h.Integrations.ListWebhookEvents(ctx, in.ID, integrations.WebhookEventFilter{
		ResourceID: in.ResourceID,
		Status:     in.Status,
		Limit:      in.Limit,
	})
	if err != nil {
		return nil, mapIntegrationError("list integration webhook events", err)
	}
	out := &webhookEventListBody{}
	out.Body.Items = items
	return out, nil
}

func (h *Handlers) RecordIntegrationWebhookEvent(ctx context.Context, in *recordWebhookEventInput) (*webhookEventBody, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	ctx, err := requiredIntegrationWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	created, err := h.Integrations.RecordWebhookEvent(ctx, in.ID, in.Body)
	if err != nil {
		return nil, mapIntegrationError("record integration webhook event", err)
	}
	out := &webhookEventBody{}
	out.Body = *created
	return out, nil
}

func mapIntegrationError(op string, err error) error {
	return mapHTTPError(op, err, []sentinelMapping{
		// hideErr=true: don't echo whether the integration exists (token mismatch / missing secret).
		{integrations.ErrUnauthorized, huma.Error401Unauthorized, true},
		{integrations.ErrNotFound, huma.Error404NotFound, false},
		{integrations.ErrConflict, huma.Error409Conflict, false},
		{integrations.ErrValidation, huma.Error400BadRequest, false},
		{integrations.ErrUpstream, huma.Error502BadGateway, false},
	})
}

func (h *Handlers) BeginIntegrationOAuth(ctx context.Context, in *beginIntegrationOAuthInput) (*beginIntegrationOAuthOutput, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	ctx, err := requiredIntegrationWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	authorizeURL, err := h.Integrations.BeginOAuthConnect(ctx, in.ID, oauthCallbackBaseFromContext(ctx), in.Body.RedirectTarget)
	if err != nil {
		return nil, mapIntegrationError("begin oauth connect", err)
	}
	out := &beginIntegrationOAuthOutput{}
	out.Body.AuthorizeURL = authorizeURL
	return out, nil
}

func (h *Handlers) BeginPendingIntegrationOAuth(ctx context.Context, in *beginPendingOAuthInput) (*beginIntegrationOAuthOutput, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	ctx, err := requiredIntegrationWorkspaceContext(ctx, in.Body.WorkspaceID)
	if err != nil {
		return nil, err
	}
	authorizeURL, err := h.Integrations.BeginPendingOAuthConnect(ctx, integrations.PendingOAuthSpec{
		Provider:   in.Body.Provider,
		Name:       in.Body.Name,
		BaseURL:    in.Body.BaseURL,
		ConfigJSON: in.Body.ConfigJSON,
	}, oauthCallbackBaseFromContext(ctx), in.Body.RedirectTarget)
	if err != nil {
		return nil, mapIntegrationError("begin oauth connect", err)
	}
	out := &beginIntegrationOAuthOutput{}
	out.Body.AuthorizeURL = authorizeURL
	return out, nil
}

func (h *Handlers) DisconnectIntegrationOAuth(ctx context.Context, in *integrationIDInput) (*disconnectIntegrationOAuthOutput, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	ctx, err := requiredIntegrationWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if err := h.Integrations.DisconnectOAuth(ctx, in.ID); err != nil {
		return nil, mapIntegrationError("disconnect oauth", err)
	}
	return &disconnectIntegrationOAuthOutput{}, nil
}

func (h *Handlers) SetApiToken(ctx context.Context, in *setApiTokenInput) (*setApiTokenOutput, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	ctx, err := requiredIntegrationWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if err := h.Integrations.SetApiToken(ctx, in.ID, in.Body.APIToken); err != nil {
		return nil, mapIntegrationError("set api token", err)
	}
	return &setApiTokenOutput{}, nil
}

// HandoffLinear creates or returns the one selected-team Linear issue for a
// Ready work item.
func (h *Handlers) HandoffLinear(ctx context.Context, in *linearHandoffInput) (*linearHandoffOutput, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	result, err := h.Integrations.HandoffLinear(integrations.WithWorkspace(ctx, workspaceID), integrations.LinearHandoffInput{
		ChangeRequestID: in.ID, IntegrationID: in.Body.IntegrationID, ResourceID: in.Body.ResourceID,
	})
	if err != nil {
		if errors.Is(err, workboard.ErrNotFound) {
			return nil, huma.Error404NotFound("Linear handoff", err)
		}
		return nil, mapIntegrationError("Linear handoff", err)
	}
	out := &linearHandoffOutput{}
	out.Body.Created = result.Created
	out.Body.Link = linearHandoffDTO{Identifier: result.Link.ExternalKey, URL: result.Link.URL, State: result.Link.State, TrackerState: result.Link.TrackerState}
	return out, nil
}

// ListChangeRequestTrackerLinks returns the tracker issue links a handoff created
// for a work item, for the work-item "linked issues" surface.
func (h *Handlers) ListChangeRequestTrackerLinks(ctx context.Context, in *changeRequestTrackerLinksInput) (*changeRequestTrackerLinksOutput, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = integrations.WithWorkspace(ctx, workspaceID)
	links, err := h.Integrations.ListTrackerLinks(ctx, in.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("list tracker links", err)
	}
	out := &changeRequestTrackerLinksOutput{}
	for _, l := range links {
		out.Body.Items = append(out.Body.Items, trackerLinkDTO{
			Identifier:   l.ExternalKey,
			URL:          l.URL,
			State:        l.State,
			TrackerState: l.TrackerState,
		})
	}
	return out, nil
}

// ListChangeRequestDeliveryLinks returns persisted provider delivery links for
// one work item. This endpoint is deliberately a readback, not a provider poll
// or a computed delivery verdict.
func (h *Handlers) ListChangeRequestDeliveryLinks(ctx context.Context, in *changeRequestDeliveryLinksInput) (*changeRequestDeliveryLinksOutput, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = integrations.WithWorkspace(ctx, workspaceID)
	links, err := h.Integrations.ListDeliveryLinks(ctx, in.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("list delivery links", err)
	}
	out := &changeRequestDeliveryLinksOutput{}
	for _, link := range links {
		out.Body.Items = append(out.Body.Items, deliveryLinkDTO{
			ExternalKey:    link.ExternalKey,
			Title:          link.Title,
			URL:            link.URL,
			State:          link.State,
			SourceBranch:   link.SourceBranch,
			TargetBranch:   link.TargetBranch,
			HeadSHA:        link.HeadSHA,
			MergeCommitSHA: link.MergeCommitSHA,
			UpdatedAt:      link.UpdatedAt,
		})
	}
	return out, nil
}

func (h *Handlers) ListGovernanceFeedbackEvents(ctx context.Context, in *listGovernanceFeedbackEventsInput) (*governanceFeedbackEventListBody, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = integrations.WithWorkspace(ctx, workspaceID)
	items, err := h.Integrations.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{
		Status:          in.Status,
		ChangeRequestID: in.ChangeRequestID,
		ArtifactID:      in.ArtifactID,
		EventType:       in.EventType,
		Limit:           in.Limit,
	})
	if err != nil {
		return nil, mapIntegrationError("list governance feedback events", err)
	}
	out := &governanceFeedbackEventListBody{}
	out.Body.Items = items
	return out, nil
}

// UpdateGovernanceFeedbackEventStatus sets a feedback event's triage status from the
// inbox: accepted (resolve) or rejected (dismiss).
func (h *Handlers) UpdateGovernanceFeedbackEventStatus(ctx context.Context, in *UpdateGovernanceFeedbackEventStatusInput) (*governanceFeedbackEventBody, error) {
	if err := h.requireService(h.Integrations, "integrations"); err != nil {
		return nil, err
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = integrations.WithWorkspace(ctx, workspaceID)
	if in.Body.Status != integrations.FeedbackStatusAccepted && in.Body.Status != integrations.FeedbackStatusRejected {
		return nil, huma.Error422UnprocessableEntity("status must be 'accepted' or 'rejected'")
	}
	ev, err := h.Integrations.ReconcileFeedbackEvent(ctx, in.ID, in.Body.Status, in.Body.Reason)
	if err != nil {
		return nil, mapIntegrationError("update feedback status", err)
	}
	out := &governanceFeedbackEventBody{}
	out.Body = *ev
	return out, nil
}
