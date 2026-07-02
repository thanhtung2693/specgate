package integrations

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/specgate/doc-registry/internal/githubapi"
	"github.com/specgate/doc-registry/internal/gitlabapi"
	"github.com/specgate/doc-registry/internal/integrations/coretypes"
	linearprovider "github.com/specgate/doc-registry/internal/integrations/linear"
)

// AutoTransitionIssueOnDeliveryPass closes any open tracker issues linked to
// the given change request on the provider that owns them: Linear issues are
// moved to their team's first "completed" workflow state; GitHub and GitLab
// issues are closed via their respective REST APIs.
//
// The method is best-effort: errors are logged at warn level and never
// propagated — a tracker API failure must never fail the delivery verdict write.
//
// Constraints:
//   - Only Linear, GitHub, and GitLab links are processed; others are skipped.
//   - Links whose lifecycle State is already closed are skipped (idempotent).
//   - Requires s.trackerLinks, s.integrations, and s.resources to be non-nil.
func (s *Service) AutoTransitionIssueOnDeliveryPass(ctx context.Context, changeRequestID string) {
	changeRequestID = strings.TrimSpace(changeRequestID)
	if changeRequestID == "" {
		return
	}
	links, err := s.trackerLinks.ListTrackerLinksByChangeRequest(ctx, changeRequestID)
	if err != nil {
		log.Warn().Err(err).Str("change_request_id", changeRequestID).
			Msg("auto-transition: failed to list tracker links")
		return
	}
	for _, link := range links {
		// Skip links whose lifecycle state is already closed.
		if strings.ToLower(strings.TrimSpace(link.State)) == TrackerStateClosed {
			continue
		}
		integration, err := s.integrations.GetIntegration(ctx, link.IntegrationID)
		if err != nil {
			log.Warn().Err(err).
				Str("integration_id", link.IntegrationID).
				Msg("auto-transition: failed to load integration")
			continue
		}
		switch integration.Provider {
		case ProviderLinear:
			s.autoTransitionLinearLink(ctx, link, integration)
		case ProviderGitHub:
			s.autoTransitionGitHubLink(ctx, link, integration)
		case ProviderGitLab:
			s.autoTransitionGitLabLink(ctx, link, integration)
		}
	}
}

// autoTransitionLinearLink attempts to transition one tracker link's Linear
// issue to Done. All errors are logged at warn level; nothing is returned.
func (s *Service) autoTransitionLinearLink(ctx context.Context, link TrackerLink, integration *Integration) {
	logger := log.With().
		Str("integration_id", link.IntegrationID).
		Str("external_id", link.ExternalID).
		Str("external_key", link.ExternalKey).
		Logger()

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

	teamID := s.linearTeamIDForIntegration(ctx, link.IntegrationID, integration)
	if teamID == "" {
		logger.Warn().Msg("auto-transition: no Linear team ID found; skipping")
		return
	}

	stateID, err := linearprovider.GetCompletedStateID(ctx, token, bearer, teamID)
	if err != nil {
		logger.Warn().Err(err).Str("team_id", teamID).
			Msg("auto-transition: failed to get completed state ID")
		return
	}

	if err := linearprovider.TransitionIssueToState(ctx, token, bearer, issueID, stateID); err != nil {
		logger.Warn().Err(err).Str("issue_id", issueID).Str("state_id", stateID).
			Msg("auto-transition: failed to transition Linear issue to Done")
		return
	}

	logger.Info().Str("issue_id", issueID).Str("state_id", stateID).
		Msg("auto-transition: Linear issue transitioned to Done")
}

// autoTransitionGitHubLink closes a GitHub issue by its number, parsed from
// ExternalKey (format "#45"). All errors are logged at warn level.
func (s *Service) autoTransitionGitHubLink(ctx context.Context, link TrackerLink, integration *Integration) {
	logger := log.With().
		Str("integration_id", link.IntegrationID).
		Str("external_key", link.ExternalKey).
		Logger()

	issueNumber, err := parseIssueNumber(link.ExternalKey)
	if err != nil {
		logger.Warn().Err(err).Msg("auto-transition: cannot parse GitHub issue number from external_key")
		return
	}

	token, err := s.ResolveAPIToken(ctx, link.IntegrationID)
	if err != nil {
		logger.Warn().Err(err).Msg("auto-transition: failed to resolve API token")
		return
	}

	repo := s.gitHubRepoForIntegration(ctx, link.IntegrationID, integration)
	if repo == "" {
		logger.Warn().Msg("auto-transition: no GitHub repo configured; skipping")
		return
	}

	client := githubapi.NewClient(githubapi.ClientConfig{
		APIURL:     gitHubAPIURL(integration.BaseURL),
		Token:      token,
		Repo:       repo,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	})
	if err := client.CloseIssue(ctx, issueNumber); err != nil {
		logger.Warn().Err(err).Int("issue_number", issueNumber).
			Msg("auto-transition: failed to close GitHub issue")
		return
	}

	logger.Info().Int("issue_number", issueNumber).
		Msg("auto-transition: GitHub issue closed")
}

// autoTransitionGitLabLink closes a GitLab issue by its IID, parsed from
// ExternalKey (format "#42"). All errors are logged at warn level.
func (s *Service) autoTransitionGitLabLink(ctx context.Context, link TrackerLink, integration *Integration) {
	logger := log.With().
		Str("integration_id", link.IntegrationID).
		Str("external_key", link.ExternalKey).
		Logger()

	issueIID, err := parseIssueNumber(link.ExternalKey)
	if err != nil {
		logger.Warn().Err(err).Msg("auto-transition: cannot parse GitLab issue IID from external_key")
		return
	}

	token, err := s.ResolveAPIToken(ctx, link.IntegrationID)
	if err != nil {
		logger.Warn().Err(err).Msg("auto-transition: failed to resolve API token")
		return
	}

	projectID := s.gitLabProjectIDForIntegration(ctx, link.IntegrationID, integration)
	if projectID == "" {
		logger.Warn().Msg("auto-transition: no GitLab project configured; skipping")
		return
	}

	bearer := strings.TrimSpace(integration.AuthMethod) == AuthMethodOAuth
	client := gitlabapi.NewClient(gitlabapi.ClientConfig{
		APIURL:     gitLabAPIURL(integration.BaseURL),
		Token:      token,
		ProjectID:  projectID,
		Bearer:     bearer,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	})
	if err := client.CloseIssue(ctx, issueIID); err != nil {
		logger.Warn().Err(err).Int("issue_iid", issueIID).
			Msg("auto-transition: failed to close GitLab issue")
		return
	}

	logger.Info().Int("issue_iid", issueIID).
		Msg("auto-transition: GitLab issue closed")
}

// linearTeamIDForIntegration returns the team UUID for a Linear integration,
// checking resource_type=team resources first, then config_json.team_id.
// Returns "" when no explicit team is configured.
func (s *Service) linearTeamIDForIntegration(ctx context.Context, integrationID string, integration *Integration) string {
	resources, err := s.resources.ListResources(ctx, integrationID)
	if err != nil {
		return ""
	}
	for _, r := range resources {
		if r.ResourceType == ResourceTypeTeam {
			if id := strings.TrimSpace(r.ExternalID); id != "" {
				return id
			}
			if id := strings.TrimSpace(r.ExternalKey); id != "" {
				return id
			}
		}
	}
	return strings.TrimSpace(integrationConfigString(integration, "team_id"))
}

// gitHubRepoForIntegration returns the owner/repo for a GitHub integration,
// checking resource_type=project resources first, then config_json.repo.
func (s *Service) gitHubRepoForIntegration(ctx context.Context, integrationID string, integration *Integration) string {
	resources, err := s.resources.ListResources(ctx, integrationID)
	if err != nil {
		return ""
	}
	for _, r := range resources {
		if r.ResourceType == coretypes.ResourceTypeProject {
			if key := strings.TrimSpace(r.ExternalKey); key != "" {
				return key
			}
		}
	}
	return strings.TrimSpace(integrationConfigString(integration, "repo"))
}

// gitLabProjectIDForIntegration returns the project ID for a GitLab integration,
// checking resource_type=project resources first, then config_json.project_id.
func (s *Service) gitLabProjectIDForIntegration(ctx context.Context, integrationID string, integration *Integration) string {
	resources, err := s.resources.ListResources(ctx, integrationID)
	if err != nil {
		return ""
	}
	for _, r := range resources {
		if r.ResourceType == coretypes.ResourceTypeProject {
			if id := strings.TrimSpace(r.ExternalID); id != "" {
				return id
			}
			if key := strings.TrimSpace(r.ExternalKey); key != "" {
				return key
			}
		}
	}
	return strings.TrimSpace(integrationConfigString(integration, "project_id"))
}

// parseIssueNumber extracts the positive integer from an issue key of the form
// "#45" or "45". Returns an error when the key is empty or has no parseable
// number.
func parseIssueNumber(key string) (int, error) {
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(key), "#"))
	if s == "" {
		return 0, fmt.Errorf("empty issue key")
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid issue number %q", key)
	}
	return n, nil
}

// gitLabAPIURL derives the GitLab REST API root from the integration's base_url
// (mirrors the same function in the gitlab subpackage without importing it).
func gitLabAPIURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return ""
	}
	return base + "/api/v4"
}
