package integrations

import (
	"context"
	"fmt"
	"strings"

	linearprovider "github.com/specgate/doc-registry/internal/integrations/linear"
)

type LinearTeamSummary struct {
	ExternalID  string `json:"external_id"`
	ExternalKey string `json:"external_key"`
	DisplayName string `json:"display_name"`
}

type LinearProjectSummary struct {
	ExternalID  string `json:"external_id"`
	ExternalKey string `json:"external_key"`
	DisplayName string `json:"display_name"`
}

func (s *Service) ListLinearTeams(ctx context.Context, integrationID string) ([]LinearTeamSummary, error) {
	integration, token, err := s.resolveLinearCatalogAuth(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	teams, err := linearprovider.ListTeamsViaGraphQL(ctx, token, strings.TrimSpace(integration.AuthMethod) == AuthMethodOAuth)
	if err != nil {
		return nil, err
	}
	out := make([]LinearTeamSummary, 0, len(teams))
	for _, team := range teams {
		out = append(out, LinearTeamSummary{
			ExternalID:  team.ID,
			ExternalKey: team.Key,
			DisplayName: firstNonEmpty(team.Name, team.Key),
		})
	}
	return out, nil
}

func (s *Service) ListLinearProjects(ctx context.Context, integrationID string, teamID string) ([]LinearProjectSummary, error) {
	integration, token, err := s.resolveLinearCatalogAuth(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		return nil, fmt.Errorf("%w: team_id is required", ErrValidation)
	}
	projects, err := linearprovider.ListProjectsForTeamViaGraphQL(ctx, token, strings.TrimSpace(integration.AuthMethod) == AuthMethodOAuth, teamID)
	if err != nil {
		return nil, err
	}
	out := make([]LinearProjectSummary, 0, len(projects))
	for _, project := range projects {
		out = append(out, LinearProjectSummary{
			ExternalID:  project.ID,
			ExternalKey: firstNonEmpty(project.Slug, project.Name, project.ID),
			DisplayName: firstNonEmpty(project.Name, project.Slug),
		})
	}
	return out, nil
}

func (s *Service) resolveLinearCatalogAuth(ctx context.Context, integrationID string) (*Integration, string, error) {
	integrationID = strings.TrimSpace(integrationID)
	if integrationID == "" {
		return nil, "", fmt.Errorf("%w: integration_id is required", ErrValidation)
	}
	integration, err := s.integrations.GetIntegration(ctx, integrationID)
	if err != nil {
		return nil, "", err
	}
	if integration.Provider != ProviderLinear {
		return nil, "", fmt.Errorf("%w: integration is not a linear provider", ErrValidation)
	}
	token, err := s.ResolveAPIToken(ctx, integrationID)
	if err != nil {
		return nil, "", err
	}
	return integration, token, nil
}
