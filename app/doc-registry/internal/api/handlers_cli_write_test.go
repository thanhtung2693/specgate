package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/integrations"
	linearprovider "github.com/specgate/doc-registry/internal/integrations/linear"
	"github.com/specgate/doc-registry/internal/workboard"
)

// --- fakes for governanceops dependencies ---

func TestCLI_WorkCreateNoCanonical422(t *testing.T) {
	t.Parallel()
	svc := newCLIGovernanceSvc()
	svc.WorkItems = &cliTestWorkItemStore{
		feature: &workboard.Feature{ID: "feat-1", WorkspaceID: "ws-1", Key: "test-feat"}, // no canonical
	}
	srv := newCLITestRouter(t, svc)
	body, _ := json.Marshal(map[string]any{"feature": "test-feat", "title": "T"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/work-items/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "promote") {
		t.Fatalf("error must name approve+promote: %s", rec.Body.String())
	}
}

func (f *cliTestWorkBoard) RecordDeliveryDecision(_ context.Context, in workboard.DeliveryDecisionInput) (*workboard.GateRun, error) {
	f.deliveryDecisions = append(f.deliveryDecisions, in)
	state := workboard.NextActionStatePass
	if in.Decision == workboard.DeliveryDecisionReject {
		state = workboard.NextActionStateFail
	}
	return &workboard.GateRun{
		ID: "gr-decision", SubjectKind: "change_request", SubjectID: in.ChangeRequestID,
		Gate: "delivery_review", State: state, Executor: workboard.GateRunExecutorHuman,
	}, nil
}

type cliLinearTransitionStore struct {
	integrations.Store
	integration *integrations.Integration
	resource    *integrations.Resource
	links       []integrations.TrackerLink
}

func (s *cliLinearTransitionStore) GetIntegration(context.Context, string) (*integrations.Integration, error) {
	return s.integration, nil
}

func (s *cliLinearTransitionStore) GetResource(_ context.Context, integrationID, resourceID string) (*integrations.Resource, error) {
	if s.resource == nil || s.resource.IntegrationID != integrationID || s.resource.ID != resourceID {
		return nil, integrations.ErrNotFound
	}
	return s.resource, nil
}

func (s *cliLinearTransitionStore) ListTrackerLinksByChangeRequest(context.Context, string) ([]integrations.TrackerLink, error) {
	return s.links, nil
}

// The delivery-decision facade must accept the CLI's actual wire body —
// {decision, actor, note} with the id in the path only. This is the exact JSON
// specgate delivery approve sends; huma validates it against the schema, so a
// required change_request_id in the body kills the command before the handler
// can backfill from the path. (Found live: the human decision step 422'd.)
func TestCLI_DeliveryDecisionAcceptsCLIWireBody(t *testing.T) {
	t.Parallel()
	svc := newCLIGovernanceSvc()
	svc.FeedbackStore = &cliTestFeedbackStore{events: []integrations.GovernanceFeedbackEvent{{
		ID: "completion-1", ChangeRequestID: "cr-1",
		EventType:   integrations.FeedbackEventCodingAgentCompleted,
		PayloadJSON: `{"agent":{"name":"builder"},"summary":"done"}`,
		CreatedAt:   time.Unix(90, 0).UTC(),
	}}}
	svc.WorkBoard.(*cliTestWorkBoard).runs = []workboard.GateRun{{
		ID: "platform-review", Gate: "delivery_review", State: workboard.NextActionStatePass,
		Executor:     workboard.GateRunExecutorPlatform,
		EvidenceJSON: `{"completion_feedback_event_id":"completion-1"}`,
		CreatedAt:    time.Unix(100, 0).UTC(),
	}}
	srv := newCLITestRouter(t, svc)

	body := []byte(`{"decision":"approve","actor":"lead","note":"looks good"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/work-items/cr-1/delivery-decision", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — the CLI wire body must validate; body = %s", rec.Code, rec.Body.String())
	}
}

type cliAutoTransitionStore struct {
	integrations.Store
	requestedChangeRequests []string
}

func (s *cliAutoTransitionStore) ListTrackerLinksByChangeRequest(
	_ context.Context,
	changeRequestID string,
) ([]integrations.TrackerLink, error) {
	s.requestedChangeRequests = append(s.requestedChangeRequests, changeRequestID)
	return nil, nil
}

func TestCLI_DeliveryDecisionTransitionsTrackersOnlyAfterHumanApproval(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		decision string
		wantCall bool
	}{
		{name: "approve", decision: "approve", wantCall: true},
		{name: "reject", decision: "reject", wantCall: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			svc := newCLIGovernanceSvc()
			svc.FeedbackStore = &cliTestFeedbackStore{events: []integrations.GovernanceFeedbackEvent{{
				ID: "completion-1", ChangeRequestID: "cr-1",
				EventType:   integrations.FeedbackEventCodingAgentCompleted,
				PayloadJSON: `{"agent":{"name":"builder"},"summary":"done"}`,
				CreatedAt:   time.Unix(90, 0).UTC(),
			}}}
			svc.WorkBoard.(*cliTestWorkBoard).runs = []workboard.GateRun{{
				ID: "platform-review", Gate: "delivery_review", State: workboard.NextActionStatePass,
				Executor:     workboard.GateRunExecutorPlatform,
				EvidenceJSON: `{"completion_feedback_event_id":"completion-1"}`,
				CreatedAt:    time.Unix(100, 0).UTC(),
			}}
			store := &cliAutoTransitionStore{}
			h := &Handlers{
				Governance:   svc,
				Integrations: integrations.NewService(store),
			}
			in := &CLIDeliveryDecisionInput{ID: "cr-1"}
			in.Body.Decision = tc.decision
			in.Body.Actor = "lead"

			if _, err := h.CLIDeliveryDecision(context.Background(), in); err != nil {
				t.Fatalf("CLIDeliveryDecision: %v", err)
			}
			gotCall := len(store.requestedChangeRequests) > 0
			if gotCall != tc.wantCall {
				t.Fatalf("tracker transition called=%v, want %v", gotCall, tc.wantCall)
			}
		})
	}
}

func TestCLI_DeliveryDecisionPersistsWhenLinearDoneTransitionFails(t *testing.T) {
	oldGraphQLURL := linearprovider.GraphQLURL
	t.Cleanup(func() { linearprovider.GraphQLURL = oldGraphQLURL })
	linear := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Linear unavailable", http.StatusBadGateway)
	}))
	t.Cleanup(linear.Close)
	linearprovider.GraphQLURL = linear.URL

	svc := newCLIGovernanceSvc()
	svc.FeedbackStore = &cliTestFeedbackStore{events: []integrations.GovernanceFeedbackEvent{{
		ID: "completion-1", ChangeRequestID: "cr-1", EventType: integrations.FeedbackEventCodingAgentCompleted,
		PayloadJSON: `{"agent":{"name":"builder"},"summary":"done"}`, CreatedAt: time.Unix(90, 0).UTC(),
	}}}
	board := svc.WorkBoard.(*cliTestWorkBoard)
	board.runs = []workboard.GateRun{{
		ID: "platform-review", Gate: "delivery_review", State: workboard.NextActionStatePass,
		Executor: workboard.GateRunExecutorPlatform, EvidenceJSON: `{"completion_feedback_event_id":"completion-1"}`,
		CreatedAt: time.Unix(100, 0).UTC(),
	}}
	token, err := integrations.EncryptSecret("linear-token")
	if err != nil {
		t.Fatal(err)
	}
	transitionStore := &cliLinearTransitionStore{
		integration: &integrations.Integration{ID: "int-linear", Provider: integrations.ProviderLinear, Status: integrations.StatusConnected, APITokenEncrypted: token},
		resource:    &integrations.Resource{ID: "team-selected", IntegrationID: "int-linear", ResourceType: integrations.ResourceTypeTeam, ExternalID: "team-selected"},
		links:       []integrations.TrackerLink{{IntegrationID: "int-linear", ResourceID: "team-selected", ExternalID: "issue-1", State: integrations.TrackerStateOpened}},
	}
	h := &Handlers{Governance: svc, Integrations: integrations.NewService(transitionStore)}
	in := &CLIDeliveryDecisionInput{ID: "cr-1"}
	in.Body.Decision = "approve"
	in.Body.Actor = "lead"
	result, err := h.CLIDeliveryDecision(context.Background(), in)
	if err != nil {
		t.Fatalf("human acceptance must survive Linear Done failure: %v", err)
	}
	if result.Body == nil || result.Body.Executor != workboard.GateRunExecutorHuman || result.Body.Verdict != string(workboard.NextActionStatePass) || len(board.deliveryDecisions) != 1 {
		t.Fatalf("delivery decision result=%#v persisted=%#v", result, board.deliveryDecisions)
	}
}

// Every CLI facade must accept the CLI's LITERAL wire body. This is the bug
// class that killed `delivery approve` (facade schema required a field the CLI
// sends only in the path) and rejected every publish (schema lacked the
// workspace_id the CLI annotates onto every body). Bodies below are copied
// from what app/cli actually sends — including the created_by/workspace_id
// identity annotation. The assertion is narrow on purpose: the request must
// never die in huma schema validation; downstream service errors are fine.
func TestCLI_FacadesAcceptCLIWireBodies(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		path string
		body string
	}{
		{"resolve", "/api/v1/work-items/resolve",
			`{"ref":"CR-1"}`},
		{"create-quick", "/api/v1/work-items",
			`{"title":"T","description":"D","acceptance_criteria":[{"text":"AC","verification_binding":"tests"}],"created_by":"dev","workspace_id":"ws-1"}`},
		{"work-create", "/api/v1/work-items/create",
			`{"feature":"test-feat","title":"T","description":"D","acceptance_criteria":["AC"],"created_by":"dev","workspace_id":"ws-1"}`},
		{"archive", "/api/v1/work-items/cr-1/archive",
			`{"reason":"done","actor":"dev"}`},
		{"delivery-report", "/api/v1/work-items/cr-1/feedback",
			`{"change_request_id":"cr-1","event_type":"coding_agent.completed","severity":"info","summary":"done","agent":{"name":"builder"},"affected_files":["a.go"],"checks":[{"name":"tests","status":"pass","detail":"ok"}],"criteria":[{"criterion_id":"ac-0","text":"AC","claim":"satisfied","evidence":{"kind":"test","path":"a_test.go","line":10}}]}`},
		{"delivery-decision", "/api/v1/work-items/cr-1/delivery-decision",
			`{"decision":"approve","actor":"lead","note":"ok"}`},
		{"publish", "/api/v1/artifacts/publish",
			`{"feature_key":"f","feature_name":"F","request_type":"new_feature","impact_level":"medium","documents":[{"path":"spec.md","role":"spec","content":"# S"}],"created_by":"dev","workspace_id":"ws-1"}`},
	}
	svc := newCLIGovernanceSvc()
	svc.WorkItems = &cliTestWorkItemStore{
		feature: &workboard.Feature{ID: "feat-1", WorkspaceID: "ws-1", Key: "test-feat", CanonicalArtifactID: "art-canon"},
	}
	srv := newCLITestRouter(t, svc)
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			out := rec.Body.String()
			if strings.Contains(out, "expected required property") || strings.Contains(out, "unexpected property") {
				t.Fatalf("CLI wire body rejected by schema (status %d): %s", rec.Code, out)
			}
		})
	}
}
