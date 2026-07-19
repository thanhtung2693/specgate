package integrations

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	linearprovider "github.com/specgate/doc-registry/internal/integrations/linear"
)

type atFakeStore struct {
	Store
	integration *Integration
	resources   []Resource
	links       []TrackerLink
}

func (f *atFakeStore) GetIntegration(_ context.Context, _ string) (*Integration, error) {
	return f.integration, nil
}
func (f *atFakeStore) ListResources(_ context.Context, _ string) ([]Resource, error) {
	return f.resources, nil
}

func (f *atFakeStore) GetResource(_ context.Context, integrationID, resourceID string) (*Resource, error) {
	for i := range f.resources {
		if f.resources[i].IntegrationID == integrationID && f.resources[i].ID == resourceID {
			return &f.resources[i], nil
		}
	}
	return nil, ErrNotFound
}
func (f *atFakeStore) ListTrackerLinksByChangeRequest(_ context.Context, _ string) ([]TrackerLink, error) {
	return f.links, nil
}

func TestAutoTransition_SkipsNonLinearProviders(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{ProviderGitHub, ProviderGitLab, "jira"} {
		t.Run(provider, func(t *testing.T) {
			svc := NewService(&atFakeStore{
				integration: &Integration{ID: "int-1", Provider: provider, Status: StatusConnected},
				links:       []TrackerLink{{IntegrationID: "int-1", ExternalID: "issue-1", State: TrackerStateOpened}},
			})
			svc.AutoTransitionIssueOnDeliveryPass(context.Background(), "cr-1")
		})
	}
}

func TestAutoTransition_EmptyChangeRequestIDIsNoop(t *testing.T) {
	svc := NewService(&atFakeStore{})
	svc.AutoTransitionIssueOnDeliveryPass(context.Background(), "")
	svc.AutoTransitionIssueOnDeliveryPass(context.Background(), "   ")
}

func TestAutoTransition_UsesPersistedSelectedTeamResource(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	oldGraphQLURL := linearprovider.GraphQLURL
	t.Cleanup(func() { linearprovider.GraphQLURL = oldGraphQLURL })
	teamIDs := make([]string, 0, 1)
	linear := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(request.Query, "TeamStates") {
			teamIDs = append(teamIDs, request.Variables["teamId"].(string))
			_, _ = io.WriteString(w, `{"data":{"team":{"states":{"nodes":[{"id":"done-selected","name":"Done","type":"completed"}]}}}}`)
			return
		}
		if strings.Contains(request.Query, "IssueUpdate") {
			_, _ = io.WriteString(w, `{"data":{"issueUpdate":{"success":true,"issue":{"id":"issue-selected","state":{"name":"Done","type":"completed"}}}}}`)
			return
		}
		http.Error(w, "unexpected Linear operation", http.StatusBadRequest)
	}))
	t.Cleanup(linear.Close)
	linearprovider.GraphQLURL = linear.URL

	svc := NewService(&atFakeStore{
		integration: &Integration{ID: "linear", Provider: ProviderLinear, Status: StatusConnected, APITokenEncrypted: encryptedSecretForTest(t, "token")},
		resources: []Resource{
			{ID: "first-team", IntegrationID: "linear", ResourceType: ResourceTypeTeam, ExternalID: "first-team-external"},
			{ID: "selected-team", IntegrationID: "linear", ResourceType: ResourceTypeTeam, ExternalID: "selected-team-external"},
		},
		links: []TrackerLink{{IntegrationID: "linear", ResourceID: "selected-team", ExternalID: "issue-selected", State: TrackerStateOpened}},
	})
	svc.AutoTransitionIssueOnDeliveryPass(context.Background(), "cr-selected-team")
	if len(teamIDs) != 1 || teamIDs[0] != "selected-team-external" {
		t.Fatalf("Linear TeamStates team IDs = %v, want only persisted selected team", teamIDs)
	}
}
