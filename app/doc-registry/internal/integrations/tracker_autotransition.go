package integrations

import (
	"context"
	"strings"

	"github.com/rs/zerolog/log"
	linearprovider "github.com/specgate/doc-registry/internal/integrations/linear"
)

// AutoTransitionIssueOnDeliveryPass best-effort transitions open Linear links.
// Tracker transition failures never fail human acceptance.
func (s *Service) AutoTransitionIssueOnDeliveryPass(ctx context.Context, changeRequestID string) {
	changeRequestID = strings.TrimSpace(changeRequestID)
	if changeRequestID == "" {
		return
	}
	links, err := s.trackerLinks.ListTrackerLinksByChangeRequest(ctx, changeRequestID)
	if err != nil {
		log.Warn().Err(err).Str("change_request_id", changeRequestID).Msg("auto-transition: failed to list tracker links")
		return
	}
	for _, link := range links {
		if strings.EqualFold(strings.TrimSpace(link.State), TrackerStateClosed) {
			continue
		}
		integration, err := s.integrations.GetIntegration(ctx, link.IntegrationID)
		if err != nil {
			log.Warn().Err(err).Str("integration_id", link.IntegrationID).Msg("auto-transition: failed to load integration")
			continue
		}
		if integration.Provider != ProviderLinear {
			continue
		}
		s.autoTransitionLinearLink(ctx, link, integration)
	}
}

func (s *Service) autoTransitionLinearLink(ctx context.Context, link TrackerLink, integration *Integration) {
	logger := log.With().Str("integration_id", link.IntegrationID).Str("external_id", link.ExternalID).Str("external_key", link.ExternalKey).Logger()
	issueID := strings.TrimSpace(link.ExternalID)
	if issueID == "" {
		logger.Warn().Msg("auto-transition: linear link has no external_id; skipping")
		return
	}
	token, err := s.ResolveAPIToken(ctx, link.IntegrationID)
	if err != nil {
		logger.Warn().Err(err).Msg("auto-transition: failed to resolve API token")
		return
	}
	bearer := strings.TrimSpace(integration.AuthMethod) == AuthMethodOAuth
	resource, err := s.resources.GetResource(ctx, link.IntegrationID, link.ResourceID)
	if err != nil || resource.ResourceType != ResourceTypeTeam || strings.TrimSpace(resource.ExternalID) == "" {
		logger.Warn().Err(err).Str("resource_id", link.ResourceID).Msg("auto-transition: selected Linear team resource is unavailable; skipping")
		return
	}
	teamID := strings.TrimSpace(resource.ExternalID)
	stateID, err := linearprovider.GetCompletedStateID(ctx, token, bearer, teamID)
	if err != nil {
		logger.Warn().Err(err).Str("team_id", teamID).Msg("auto-transition: failed to get completed state ID")
		return
	}
	if err := linearprovider.TransitionIssueToState(ctx, token, bearer, issueID, stateID); err != nil {
		logger.Warn().Err(err).Str("issue_id", issueID).Str("state_id", stateID).Msg("auto-transition: failed to transition Linear issue to Done")
		return
	}
	logger.Info().Str("issue_id", issueID).Str("state_id", stateID).Msg("auto-transition: Linear issue transitioned to Done")
}
