package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	linearprovider "github.com/specgate/doc-registry/internal/integrations/linear"
	"github.com/specgate/doc-registry/internal/workboard"
)

type handoffStore struct {
	Store
	integration *Integration
	resource    *Resource
	links       []TrackerLink
	upsertErr   error
}

func (s *handoffStore) GetIntegration(context.Context, string) (*Integration, error) {
	return s.integration, nil
}

func (s *handoffStore) GetResource(context.Context, string, string) (*Resource, error) {
	return s.resource, nil
}

func (s *handoffStore) ListTrackerLinksByChangeRequest(context.Context, string) ([]TrackerLink, error) {
	return s.links, nil
}

func (s *handoffStore) UpsertTrackerLink(_ context.Context, link TrackerLink) (*TrackerLink, error) {
	if s.upsertErr != nil {
		err := s.upsertErr
		s.upsertErr = nil
		return nil, err
	}
	s.links = []TrackerLink{link}
	return &s.links[0], nil
}

func handoffReadyFixture(t *testing.T) (*handoffStore, *fakeWorkBoard) {
	t.Helper()
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	return &handoffStore{integration: &Integration{ID: "int", WorkspaceID: "ws", Provider: ProviderLinear, Status: StatusConnected, APITokenEncrypted: encryptedSecretForTest(t, "token")}, resource: &Resource{ID: "team", IntegrationID: "int", ResourceType: ResourceTypeTeam, ExternalID: "team-id"}}, &fakeWorkBoard{cr: &workboard.ChangeRequest{ID: "cr", Key: "SG-1", Title: "title", Phase: workboard.BoardPhaseReady}}
}

func TestHandoffLinear_CreatesOneSelectedTeamIssueForReadyWork(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	oldURL := linearprovider.GraphQLURL
	t.Cleanup(func() { linearprovider.GraphQLURL = oldURL })
	createCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if strings.Contains(payload.Query, "IssueCreate") {
			createCalls++
			_, _ = io.WriteString(w, `{"data":{"issueCreate":{"success":true,"issue":{"id":"issue-id","identifier":"ENG-123","url":"https://linear.app/eng/issue/ENG-123","team":{"id":"selected-team"}}}}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"issue":null}}`)
	}))
	t.Cleanup(server.Close)
	linearprovider.GraphQLURL = server.URL

	store := &handoffStore{
		integration: &Integration{ID: "int-linear", WorkspaceID: "ws-a", Provider: ProviderLinear, Status: StatusConnected, APITokenEncrypted: encryptedSecretForTest(t, "token")},
		resource:    &Resource{ID: "team-resource", IntegrationID: "int-linear", ResourceType: ResourceTypeTeam, ExternalID: "selected-team"},
	}
	board := &fakeWorkBoard{cr: &workboard.ChangeRequest{ID: "cr-1", Key: "SG-123", WorkspaceID: "ws-a", Title: "Ready work", IntentMD: "Persist the outcome", Phase: workboard.BoardPhaseReady}, criteria: []workboard.AcceptanceCriterion{{Text: "First criterion", SortOrder: 1}}}

	result, err := NewServiceWithWorkBoard(store, board).HandoffLinear(context.Background(), LinearHandoffInput{ChangeRequestID: "cr-1", IntegrationID: "int-linear", ResourceID: "team-resource"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || result.Link.ResourceID != "team-resource" || result.Link.ExternalKey != "ENG-123" {
		t.Fatalf("handoff result = %#v", result)
	}
	if createCalls != 1 {
		t.Fatalf("linear creates = %d, want 1", createCalls)
	}
}

func TestHandoffLinear_RejectsPersistedNonReadyPhase(t *testing.T) {
	t.Setenv(SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")
	for _, phase := range []workboard.BoardPhase{workboard.BoardPhaseDelivered, workboard.BoardPhaseReview} {
		t.Run(string(phase), func(t *testing.T) {
			store := &handoffStore{integration: &Integration{ID: "int-linear", WorkspaceID: "ws-a", Provider: ProviderLinear, Status: StatusConnected, APITokenEncrypted: encryptedSecretForTest(t, "token")}, resource: &Resource{ID: "team-resource", IntegrationID: "int-linear", ResourceType: ResourceTypeTeam, ExternalID: "selected-team"}}
			board := &fakeWorkBoard{cr: &workboard.ChangeRequest{ID: "cr-1", Key: "SG-123", WorkspaceID: "ws-a", Title: "work", LeadArtifactID: "artifact-1", Phase: phase}}
			_, err := NewServiceWithWorkBoard(store, board).HandoffLinear(context.Background(), LinearHandoffInput{ChangeRequestID: "cr-1", IntegrationID: "int-linear", ResourceID: "team-resource"})
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("phase %s error = %v, want ErrValidation", phase, err)
			}
		})
	}
}

func TestDeterministicLinearIssueID_IsStableUUIDv4AndSeedDistinct(t *testing.T) {
	first := deterministicLinearIssueID("ws-a", "cr-a")
	if first != deterministicLinearIssueID("ws-a", "cr-a") || first == deterministicLinearIssueID("ws-a", "cr-b") || first[14] != '4' || first[19] != '8' && first[19] != '9' && first[19] != 'a' && first[19] != 'b' {
		t.Fatalf("deterministic ID = %q", first)
	}
}

func TestHandoffLinear_ValidatesReadyConnectedLinearSelectedTeam(t *testing.T) {
	base := func() (*handoffStore, *fakeWorkBoard) {
		return &handoffStore{integration: &Integration{ID: "int", WorkspaceID: "ws", Provider: ProviderLinear, Status: StatusConnected}, resource: &Resource{ID: "team", IntegrationID: "int", ResourceType: ResourceTypeTeam, ExternalID: "team-id"}}, &fakeWorkBoard{cr: &workboard.ChangeRequest{ID: "cr", Key: "SG-1", Phase: workboard.BoardPhaseReady}}
	}
	for _, tc := range []struct {
		name   string
		mutate func(*handoffStore, *fakeWorkBoard)
		ctx    context.Context
	}{
		{"provider", func(s *handoffStore, _ *fakeWorkBoard) { s.integration.Provider = ProviderGitHub }, context.Background()},
		{"disconnected", func(s *handoffStore, _ *fakeWorkBoard) { s.integration.Status = StatusDisabled }, context.Background()},
		{"wrong workspace", func(_ *handoffStore, _ *fakeWorkBoard) {}, WithWorkspace(context.Background(), "other")},
		{"wrong resource owner", func(s *handoffStore, _ *fakeWorkBoard) { s.resource.IntegrationID = "other" }, context.Background()},
		{"wrong resource type", func(s *handoffStore, _ *fakeWorkBoard) { s.resource.ResourceType = ResourceTypeProject }, context.Background()},
		{"archived", func(_ *handoffStore, b *fakeWorkBoard) { b.cr.Archived = true }, context.Background()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, b := base()
			tc.mutate(s, b)
			_, err := NewServiceWithWorkBoard(s, b).HandoffLinear(tc.ctx, LinearHandoffInput{ChangeRequestID: "cr", IntegrationID: "int", ResourceID: "team"})
			if !errors.Is(err, ErrValidation) && !errors.Is(err, ErrNotFound) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestHandoffLinear_RendersAuthoritativeIssue(t *testing.T) {
	cr := workboard.ChangeRequest{ID: "cr", Key: "SG-7", Title: "Persisted title", IntentMD: "Persisted intent", Phase: workboard.BoardPhaseReady}
	body := renderLinearIssueDescription(cr, []workboard.AcceptanceCriterion{{Text: "Second", SortOrder: 2}, {Text: "First", SortOrder: 1}})
	for _, want := range []string{"## Intent\nPersisted intent", "- Second\n- First", "`specgate work context SG-7 --json`", "`<!-- specgate-work-ref: SG-7 -->`"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
	if strings.Contains(body, "http") {
		t.Fatalf("public URL leaked: %s", body)
	}
}

func TestHandoffLinear_RepeatedCallReturnsExistingLink(t *testing.T) {
	store := &handoffStore{integration: &Integration{ID: "int", WorkspaceID: "ws", Provider: ProviderLinear, Status: StatusConnected}, resource: &Resource{ID: "team", IntegrationID: "int", ResourceType: ResourceTypeTeam, ExternalID: "team-id"}, links: []TrackerLink{{ID: "link", ChangeRequestID: "cr", ExternalKey: "ENG-1"}}}
	board := &fakeWorkBoard{cr: &workboard.ChangeRequest{ID: "cr", Key: "SG-1", Phase: workboard.BoardPhaseReady}}
	result, err := NewServiceWithWorkBoard(store, board).HandoffLinear(context.Background(), LinearHandoffInput{ChangeRequestID: "cr", IntegrationID: "int", ResourceID: "team"})
	if err != nil || result.Created || result.Link.ID != "link" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestHandoffLinear_AmbiguousCreateRecoversByID(t *testing.T) {
	store, board := handoffReadyFixture(t)
	old := linearprovider.GraphQLURL
	t.Cleanup(func() { linearprovider.GraphQLURL = old })
	created := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&p)
		if strings.Contains(p.Query, "IssueCreate") {
			created = true
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if created {
			_, _ = io.WriteString(w, `{"data":{"issue":{"id":"i","identifier":"ENG-1","url":"u","team":{"id":"team-id"}}}}`)
		} else {
			_, _ = io.WriteString(w, `{"data":{"issue":null}}`)
		}
	}))
	t.Cleanup(srv.Close)
	linearprovider.GraphQLURL = srv.URL
	result, err := NewServiceWithWorkBoard(store, board).HandoffLinear(context.Background(), LinearHandoffInput{ChangeRequestID: "cr", IntegrationID: "int", ResourceID: "team"})
	if err != nil || !result.Created || result.Link.ExternalID != "i" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestHandoffLinear_DefiniteFailureLeavesNoLinkAndRetrySucceeds(t *testing.T) {
	store, board := handoffReadyFixture(t)
	old := linearprovider.GraphQLURL
	t.Cleanup(func() { linearprovider.GraphQLURL = old })
	fail := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&p)
		if strings.Contains(p.Query, "IssueCreate") {
			if fail {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"data":{"issueCreate":{"success":true,"issue":{"id":"i","identifier":"ENG-1","url":"u","team":{"id":"team-id"}}}}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"issue":null}}`)
	}))
	t.Cleanup(srv.Close)
	linearprovider.GraphQLURL = srv.URL
	_, err := NewServiceWithWorkBoard(store, board).HandoffLinear(context.Background(), LinearHandoffInput{ChangeRequestID: "cr", IntegrationID: "int", ResourceID: "team"})
	if !errors.Is(err, ErrUpstream) || len(store.links) != 0 {
		t.Fatalf("err=%v links=%v", err, store.links)
	}
	fail = false
	result, err := NewServiceWithWorkBoard(store, board).HandoffLinear(context.Background(), LinearHandoffInput{ChangeRequestID: "cr", IntegrationID: "int", ResourceID: "team"})
	if err != nil || !result.Created {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestHandoffLinear_PersistFailureRecoversRemoteIssueOnRetry(t *testing.T) {
	store, board := handoffReadyFixture(t)
	store.upsertErr = errors.New("disk")
	old := linearprovider.GraphQLURL
	t.Cleanup(func() { linearprovider.GraphQLURL = old })
	created := false
	createCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&p)
		if strings.Contains(p.Query, "IssueCreate") {
			created = true
			createCalls++
			_, _ = io.WriteString(w, `{"data":{"issueCreate":{"success":true,"issue":{"id":"i","identifier":"ENG-1","url":"u","team":{"id":"team-id"}}}}}`)
			return
		}
		if created {
			_, _ = io.WriteString(w, `{"data":{"issue":{"id":"i","identifier":"ENG-1","url":"u","team":{"id":"team-id"}}}}`)
		} else {
			_, _ = io.WriteString(w, `{"data":{"issue":null}}`)
		}
	}))
	t.Cleanup(srv.Close)
	linearprovider.GraphQLURL = srv.URL
	_, err := NewServiceWithWorkBoard(store, board).HandoffLinear(context.Background(), LinearHandoffInput{ChangeRequestID: "cr", IntegrationID: "int", ResourceID: "team"})
	if err == nil {
		t.Fatal("want persist failure")
	}
	result, err := NewServiceWithWorkBoard(store, board).HandoffLinear(context.Background(), LinearHandoffInput{ChangeRequestID: "cr", IntegrationID: "int", ResourceID: "team"})
	if err != nil || !result.Created {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if createCalls != 1 {
		t.Fatalf("Linear creates = %d, want exactly one remote issue", createCalls)
	}
}

func TestHandoffLinear_RecoveryRequiresSelectedTeamOwnership(t *testing.T) {
	for _, tc := range []struct {
		name       string
		retryTeam  string
		wantErr    error
		wantLinked bool
	}{
		{name: "same team", retryTeam: "team-a", wantLinked: true},
		{name: "other team", retryTeam: "team-b", wantErr: ErrValidation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, board := handoffReadyFixture(t)
			store.resource.ExternalID = "team-a"
			store.upsertErr = errors.New("local persistence failed")
			old := linearprovider.GraphQLURL
			t.Cleanup(func() { linearprovider.GraphQLURL = old })
			created := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var p struct {
					Query string `json:"query"`
				}
				_ = json.NewDecoder(r.Body).Decode(&p)
				if strings.Contains(p.Query, "IssueCreate") {
					created = true
					_, _ = io.WriteString(w, `{"data":{"issueCreate":{"success":true,"issue":{"id":"i","identifier":"ENG-1","url":"u","team":{"id":"team-a"}}}}}`)
					return
				}
				if created {
					_, _ = io.WriteString(w, `{"data":{"issue":{"id":"i","identifier":"ENG-1","url":"u","team":{"id":"team-a"}}}}`)
					return
				}
				_, _ = io.WriteString(w, `{"data":{"issue":null}}`)
			}))
			t.Cleanup(srv.Close)
			linearprovider.GraphQLURL = srv.URL

			_, err := NewServiceWithWorkBoard(store, board).HandoffLinear(context.Background(), LinearHandoffInput{ChangeRequestID: "cr", IntegrationID: "int", ResourceID: "team"})
			if err == nil || len(store.links) != 0 {
				t.Fatalf("first handoff err=%v links=%#v, want persistence failure without link", err, store.links)
			}
			store.resource.ExternalID = tc.retryTeam
			result, err := NewServiceWithWorkBoard(store, board).HandoffLinear(context.Background(), LinearHandoffInput{ChangeRequestID: "cr", IntegrationID: "int", ResourceID: "team"})
			if !errors.Is(err, tc.wantErr) || (result != nil) != tc.wantLinked || (len(store.links) > 0) != tc.wantLinked {
				t.Fatalf("retry result=%#v err=%v links=%#v", result, err, store.links)
			}
		})
	}
}
