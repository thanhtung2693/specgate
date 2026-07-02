package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/agentsclient"
	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
)

type fakeWorkItemWorkBoard struct {
	changeRequests []workboard.ChangeRequest
	gateRuns       map[string][]workboard.GateRun
	acs            map[string][]workboard.AcceptanceCriterion
}

func (f *fakeWorkItemWorkBoard) ListChangeRequests(_ context.Context, _ bool) ([]workboard.ChangeRequest, error) {
	return f.changeRequests, nil
}

func (f *fakeWorkItemWorkBoard) GetChangeRequest(_ context.Context, id string) (*workboard.ChangeRequest, error) {
	for i := range f.changeRequests {
		if f.changeRequests[i].ID == id {
			cp := f.changeRequests[i]
			return &cp, nil
		}
	}
	return nil, workboard.ErrNotFound
}

func (f *fakeWorkItemWorkBoard) ListGateRuns(_ context.Context, changeRequestID string, _ int) ([]workboard.GateRun, error) {
	return f.gateRuns[changeRequestID], nil
}

func (f *fakeWorkItemWorkBoard) ListAcceptanceCriteria(_ context.Context, changeRequestID string) ([]workboard.AcceptanceCriterion, error) {
	return f.acs[changeRequestID], nil
}

func (f *fakeWorkItemWorkBoard) GetFeature(_ context.Context, _ string) (*workboard.Feature, error) {
	return nil, workboard.ErrNotFound
}

func (f *fakeWorkItemWorkBoard) ListStaleWarnings(_ context.Context, _ workboard.StaleWarningFilter) ([]workboard.StaleWarning, error) {
	return nil, nil
}

type fakeWorkItemTracker struct {
	integrations []integrations.Integration
	linksByCR    map[string][]integrations.TrackerLink
}

func (f *fakeWorkItemTracker) List(_ context.Context) ([]integrations.Integration, error) {
	return f.integrations, nil
}

func (f *fakeWorkItemTracker) ListTrackerLinks(_ context.Context, changeRequestID string) ([]integrations.TrackerLink, error) {
	return f.linksByCR[changeRequestID], nil
}

type fakeClarificationFeedbackStore struct {
	rows []integrations.GovernanceFeedbackEvent
}

func (f *fakeClarificationFeedbackStore) CreateGovernanceFeedbackEvent(_ context.Context, in integrations.GovernanceFeedbackEvent) (*integrations.GovernanceFeedbackEvent, error) {
	return &in, nil
}

func (f *fakeClarificationFeedbackStore) ListGovernanceFeedbackEvents(_ context.Context, filter integrations.GovernanceFeedbackFilter) ([]integrations.GovernanceFeedbackEvent, error) {
	out := make([]integrations.GovernanceFeedbackEvent, 0, len(f.rows))
	for _, row := range f.rows {
		if filter.ChangeRequestID != "" && row.ChangeRequestID != filter.ChangeRequestID {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func TestResolveWorkItem_ByIssueKeyReturnsLaneScopedContextPack(t *testing.T) {
	t.Parallel()

	governance := &fakeWorkItemWorkBoard{
		changeRequests: []workboard.ChangeRequest{{
			ID:             "cr-1",
			Key:            "CR-1",
			FeatureID:      "feat-1",
			Title:          "Ship loyalty redeem",
			LeadArtifactID: "art-1",
		}},
	}
	tracker := &fakeWorkItemTracker{
		integrations: []integrations.Integration{{
			ID:       "int-linear",
			Provider: integrations.ProviderLinear,
			Name:     "Linear",
			Status:   integrations.StatusConnected,
		}},
		linksByCR: map[string][]integrations.TrackerLink{
			"cr-1": {{
				IntegrationID:   "int-linear",
				ChangeRequestID: "cr-1",
				Lane:            "fe",
				ExternalKey:     "LOY-128",
				URL:             "https://linear.app/acme/issue/LOY-128",
				UpdatedAt:       time.Unix(200, 0),
			}},
		},
	}

	svc := &governanceops.Service{WorkBoard: governance, Trackers: tracker}
	handler := NewResolveWorkItemHandler(svc)
	out, err := handler(context.Background(), ResolveWorkItemInput{
		Provider: integrations.ProviderLinear,
		IssueKey: "LOY-128",
	})
	if err != nil {
		t.Fatalf("ResolveWorkItem: %v", err)
	}

	var got struct {
		ChangeRequestID string `json:"change_request_id"`
		ContextPackURI  string `json:"context_pack_uri"`
		Phase           string `json:"phase"`
		IssueKey        string `json:"issue_key"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.ChangeRequestID != "cr-1" {
		t.Fatalf("change_request_id = %q, want cr-1", got.ChangeRequestID)
	}
	if got.ContextPackURI != "specgate://context-pack/cr-1/fe" {
		t.Fatalf("context_pack_uri = %q", got.ContextPackURI)
	}
	if got.Phase != string(workboard.BoardPhaseReady) {
		t.Fatalf("phase = %q", got.Phase)
	}
	if got.IssueKey != "LOY-128" {
		t.Fatalf("issue_key = %q", got.IssueKey)
	}
}

func TestResolveWorkItem_ByIssueURLReturnsNewestMatch(t *testing.T) {
	t.Parallel()

	governance := &fakeWorkItemWorkBoard{
		changeRequests: []workboard.ChangeRequest{
			{ID: "cr-old", FeatureID: "feat-1", Title: "Old", LeadArtifactID: "art-old"},
			{ID: "cr-new", FeatureID: "feat-2", Title: "New", ContextPackArtifactID: "pack-new"},
		},
	}
	tracker := &fakeWorkItemTracker{
		integrations: []integrations.Integration{{
			ID:       "int-gitlab",
			Provider: integrations.ProviderGitLab,
			Name:     "GitLab",
			Status:   integrations.StatusConnected,
		}},
		linksByCR: map[string][]integrations.TrackerLink{
			"cr-old": {{
				IntegrationID:   "int-gitlab",
				ChangeRequestID: "cr-old",
				ExternalKey:     "77",
				URL:             "https://gitlab.example.com/group/proj/-/issues/77",
				UpdatedAt:       time.Unix(100, 0),
			}},
			"cr-new": {{
				IntegrationID:   "int-gitlab",
				ChangeRequestID: "cr-new",
				ExternalKey:     "77",
				URL:             "https://gitlab.example.com/group/proj/-/issues/77/",
				UpdatedAt:       time.Unix(300, 0),
			}},
		},
	}

	svc := &governanceops.Service{WorkBoard: governance, Trackers: tracker}
	handler := NewResolveWorkItemHandler(svc)
	out, err := handler(context.Background(), ResolveWorkItemInput{
		Provider: integrations.ProviderGitLab,
		IssueURL: "https://gitlab.example.com/group/proj/-/issues/77",
	})
	if err != nil {
		t.Fatalf("ResolveWorkItem: %v", err)
	}

	var got struct {
		ChangeRequestID string `json:"change_request_id"`
		ContextPackURI  string `json:"context_pack_uri"`
		Phase           string `json:"phase"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.ChangeRequestID != "cr-new" {
		t.Fatalf("change_request_id = %q, want cr-new", got.ChangeRequestID)
	}
	if got.ContextPackURI != "specgate://context-pack/cr-new" {
		t.Fatalf("context_pack_uri = %q", got.ContextPackURI)
	}
	if got.Phase != string(workboard.BoardPhaseHandoff) {
		t.Fatalf("phase = %q, want Handoff", got.Phase)
	}
}

func TestListWorkItems_DefaultsToReadyAndHandedOff(t *testing.T) {
	t.Parallel()

	governance := &fakeWorkItemWorkBoard{
		changeRequests: []workboard.ChangeRequest{
			{ID: "cr-intake", Title: "Intake only"},
			{ID: "cr-ready", Title: "Ready", LeadArtifactID: "art-ready"},
			{ID: "cr-handoff", Title: "Handoff", ContextPackArtifactID: "pack-1"},
		},
	}

	svc := &governanceops.Service{WorkBoard: governance}
	handler := NewListWorkItemsHandler(svc)
	out, err := handler(context.Background(), ListWorkItemsInput{})
	if err != nil {
		t.Fatalf("ListWorkItems: %v", err)
	}

	var got struct {
		Items []struct {
			ChangeRequestID string `json:"change_request_id"`
			Phase           string `json:"phase"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(got.Items))
	}
	if got.Items[0].ChangeRequestID != "cr-ready" || got.Items[1].ChangeRequestID != "cr-handoff" {
		t.Fatalf("unexpected items = %+v", got.Items)
	}
}

func TestReadDeliveryReview_DecodesLatestFailedGateRun(t *testing.T) {
	t.Parallel()

	governance := &fakeWorkItemWorkBoard{
		gateRuns: map[string][]workboard.GateRun{
			"cr-1": {
				{
					Gate:         "delivery_review",
					State:        workboard.NextActionStatePass,
					Hint:         "older pass",
					CreatedAt:    time.Unix(100, 0),
					EvidenceJSON: `{"verdict":"pass","confidence":0.91,"evidence":"{\"criteria\":[{\"criterion_id\":\"ac-1\",\"text\":\"Old\",\"verdict\":\"met\",\"why\":\"older\"}],\"checks\":[{\"name\":\"tests\",\"status\":\"pass\"}]}"}`,
				},
				{
					Gate:         "delivery_review",
					State:        workboard.NextActionStateFail,
					Hint:         "1 criterion still unclear",
					CreatedAt:    time.Unix(200, 0),
					EvidenceJSON: `{"verdict":"fail","confidence":0.73,"evidence":"{\"criteria\":[{\"criterion_id\":\"ac-1\",\"text\":\"Cannot over-redeem\",\"verdict\":\"unmet\",\"why\":\"redeem allows negative balance\"},{\"criterion_id\":\"ac-2\",\"text\":\"Audit log exists\",\"verdict\":\"met\",\"why\":\"covered\"}],\"checks\":[{\"name\":\"tests\",\"status\":\"fail\",\"detail\":\"loyalty/redeem regression\"},{\"name\":\"lint\",\"status\":\"pass\"}]}"}`,
				},
			},
		},
	}

	svc := &governanceops.Service{WorkBoard: governance}
	handler := NewReadDeliveryReviewHandler(svc)
	out, err := handler(context.Background(), ReadDeliveryReviewInput{ChangeRequestID: "cr-1", Detail: true})
	if err != nil {
		t.Fatalf("ReadDeliveryReview: %v", err)
	}

	var got struct {
		Found         bool    `json:"found"`
		Verdict       string  `json:"verdict"`
		Confidence    float64 `json:"confidence"`
		OutstandingMD string  `json:"outstanding_md"`
		PerCriterion  []struct {
			CriterionID string `json:"criterion_id"`
			Verdict     string `json:"verdict"`
		} `json:"per_criterion"`
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Found {
		t.Fatal("found = false, want true")
	}
	if got.Verdict != "fail" {
		t.Fatalf("verdict = %q, want fail", got.Verdict)
	}
	if got.Confidence != 0.73 {
		t.Fatalf("confidence = %v, want 0.73", got.Confidence)
	}
	if len(got.PerCriterion) != 2 || got.PerCriterion[0].CriterionID != "ac-1" {
		t.Fatalf("per_criterion = %+v", got.PerCriterion)
	}
	if len(got.Checks) != 2 || got.Checks[0].Name != "tests" {
		t.Fatalf("checks = %+v", got.Checks)
	}
	if got.OutstandingMD == "" {
		t.Fatal("outstanding_md is empty")
	}
}

func TestReadClarification_ReturnsAnsweredBlockedAmbiguityEvents(t *testing.T) {
	t.Parallel()

	store := &fakeClarificationFeedbackStore{
		rows: []integrations.GovernanceFeedbackEvent{
			{
				ID:              "fb-open",
				ChangeRequestID: "cr-1",
				EventType:       integrations.FeedbackEventCodingAgentBlockedAmbiguity,
				Status:          integrations.FeedbackStatusReceived,
				Reason:          "Still pending",
				CreatedAt:       time.Unix(100, 0),
				UpdatedAt:       time.Unix(100, 0),
			},
			{
				ID:              "fb-answered",
				ChangeRequestID: "cr-1",
				EventType:       integrations.FeedbackEventCodingAgentBlockedAmbiguity,
				Status:          integrations.FeedbackStatusAccepted,
				Reason:          "Use proportional reversal for partial returns.",
				PayloadJSON:     `{"summary":"How should partial returns reverse points?","suggested_correction":"Clarify refund math"}`,
				CreatedAt:       time.Unix(200, 0),
				UpdatedAt:       time.Unix(300, 0),
			},
			{
				ID:              "fb-completed",
				ChangeRequestID: "cr-1",
				EventType:       integrations.FeedbackEventCodingAgentCompleted,
				Status:          integrations.FeedbackStatusAccepted,
				Reason:          "Done",
				CreatedAt:       time.Unix(400, 0),
				UpdatedAt:       time.Unix(400, 0),
			},
		},
	}

	handler := NewReadClarificationHandler(&governanceops.Service{FeedbackStore: store})
	out, err := handler(context.Background(), ReadClarificationInput{ChangeRequestID: "cr-1"})
	if err != nil {
		t.Fatalf("ReadClarification: %v", err)
	}

	var got struct {
		Found          bool `json:"found"`
		Clarifications []struct {
			FeedbackEventID string `json:"feedback_event_id"`
			QuestionRef     string `json:"question_ref"`
			QuestionMD      string `json:"question_md"`
			AnswerMD        string `json:"answer_md"`
			Status          string `json:"status"`
			AnsweredAt      string `json:"answered_at"`
		} `json:"clarifications"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Found {
		t.Fatal("found = false, want true")
	}
	if len(got.Clarifications) != 1 {
		t.Fatalf("clarifications = %d, want 1", len(got.Clarifications))
	}
	row := got.Clarifications[0]
	if row.FeedbackEventID != "fb-answered" {
		t.Fatalf("feedback_event_id = %q", row.FeedbackEventID)
	}
	if row.Status != "accepted" {
		t.Fatalf("status = %q, want accepted", row.Status)
	}
	if row.QuestionMD != "How should partial returns reverse points?" {
		t.Fatalf("question_md = %q", row.QuestionMD)
	}
	if row.AnswerMD != "Use proportional reversal for partial returns." {
		t.Fatalf("answer_md = %q", row.AnswerMD)
	}
	if row.AnsweredAt == "" {
		t.Fatal("answered_at is empty")
	}
}

func TestReadClarification_FiltersBySince(t *testing.T) {
	t.Parallel()

	store := &fakeClarificationFeedbackStore{
		rows: []integrations.GovernanceFeedbackEvent{
			{
				ID:              "fb-old",
				ChangeRequestID: "cr-1",
				EventType:       integrations.FeedbackEventCodingAgentBlockedAmbiguity,
				Status:          integrations.FeedbackStatusAccepted,
				Reason:          "Old answer",
				CreatedAt:       time.Unix(100, 0),
				UpdatedAt:       time.Unix(100, 0),
			},
			{
				ID:              "fb-new",
				ChangeRequestID: "cr-1",
				EventType:       integrations.FeedbackEventCodingAgentBlockedAmbiguity,
				Status:          integrations.FeedbackStatusIgnored,
				Reason:          "New answer",
				CreatedAt:       time.Unix(200, 0),
				UpdatedAt:       time.Unix(300, 0),
			},
		},
	}

	handler := NewReadClarificationHandler(&governanceops.Service{FeedbackStore: store})
	out, err := handler(context.Background(), ReadClarificationInput{
		ChangeRequestID: "cr-1",
		Since:           time.Unix(150, 0).UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("ReadClarification: %v", err)
	}

	var got struct {
		Found          bool `json:"found"`
		Clarifications []struct {
			FeedbackEventID string `json:"feedback_event_id"`
			Status          string `json:"status"`
		} `json:"clarifications"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Found {
		t.Fatal("found = false, want true")
	}
	if len(got.Clarifications) != 1 || got.Clarifications[0].FeedbackEventID != "fb-new" {
		t.Fatalf("clarifications = %+v", got.Clarifications)
	}
	if got.Clarifications[0].Status != "rejected" {
		t.Fatalf("status = %q, want rejected", got.Clarifications[0].Status)
	}
}

func TestListWorkItems_WorkTypeFilter(t *testing.T) {
	t.Parallel()

	governance := &fakeWorkItemWorkBoard{
		changeRequests: []workboard.ChangeRequest{
			{ID: "cr-feature", Title: "New feature", WorkType: workboard.WorkTypeNewFeature, LeadArtifactID: "art-1"},
			{ID: "cr-bugfix", Title: "Bug fix", WorkType: workboard.WorkTypeBugFix, LeadArtifactID: "art-2"},
		},
	}

	svc := &governanceops.Service{WorkBoard: governance}
	handler := NewListWorkItemsHandler(svc)
	out, err := handler(context.Background(), ListWorkItemsInput{WorkType: "bug_fix"})
	if err != nil {
		t.Fatalf("ListWorkItems: %v", err)
	}

	var got struct {
		Items []struct {
			ChangeRequestID string `json:"change_request_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].ChangeRequestID != "cr-bugfix" {
		t.Fatalf("items = %+v, want only cr-bugfix", got.Items)
	}
}

func TestGetWorkStatus_SummaryAndPendingActions(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0)
	governance := &fakeWorkItemWorkBoard{
		changeRequests: []workboard.ChangeRequest{{
			ID:             "cr-1",
			Title:          "Add provenance",
			WorkType:       workboard.WorkTypeNewFeature,
			LeadArtifactID: "art-1",
		}},
		acs: map[string][]workboard.AcceptanceCriterion{
			"cr-1": {
				{ID: "ac-1", Text: "JSON field present", Done: true},
				{ID: "ac-2", Text: "Freshness correct", Done: true},
				{ID: "ac-3", Text: "Empty is []", Done: false},
			},
		},
		gateRuns: map[string][]workboard.GateRun{
			"cr-1": {
				{ID: "gr-1", Gate: "scope_clear", State: workboard.NextActionStatePass, CreatedAt: now},
				{ID: "gr-2", Gate: "canonical_spec", State: workboard.NextActionStatePending, Hint: "No canonical yet", CreatedAt: now},
				{ID: "gr-3", Gate: "delivery_review", State: workboard.NextActionStateFail, Hint: "AC-3 unmet", CreatedAt: now},
			},
		},
	}

	svc := &governanceops.Service{WorkBoard: governance, AppBaseURL: "http://app.example.com"}
	handler := NewGetWorkStatusHandler(svc)
	out, err := handler(context.Background(), GetWorkStatusInput{ChangeRequestID: "cr-1"})
	if err != nil {
		t.Fatalf("GetWorkStatus: %v", err)
	}

	var got struct {
		Title    string `json:"title"`
		Phase    string `json:"phase"`
		AcsDone  int    `json:"acs_done"`
		AcsTotal int    `json:"acs_total"`
		Gates    []struct {
			Gate  string `json:"gate"`
			State string `json:"state"`
		} `json:"gates"`
		DeliveryReview *struct {
			Verdict string `json:"verdict"`
		} `json:"delivery_review"`
		PendingHumanActions []struct {
			Action string `json:"action"`
			URL    string `json:"url"`
		} `json:"pending_human_actions"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Title != "Add provenance" {
		t.Fatalf("title = %q", got.Title)
	}
	if got.AcsDone != 2 || got.AcsTotal != 3 {
		t.Fatalf("acs = %d/%d, want 2/3", got.AcsDone, got.AcsTotal)
	}
	if len(got.Gates) != 2 { // scope_clear + canonical_spec; delivery_review excluded
		t.Fatalf("gates = %+v, want 2 entries", got.Gates)
	}
	if got.DeliveryReview == nil || got.DeliveryReview.Verdict != string(workboard.NextActionStateFail) {
		t.Fatalf("delivery_review = %+v", got.DeliveryReview)
	}
	if len(got.PendingHumanActions) != 1 || got.PendingHumanActions[0].Action != "canonical_spec" {
		t.Fatalf("pending_human_actions = %+v", got.PendingHumanActions)
	}
	if got.PendingHumanActions[0].URL != "http://app.example.com/work-items/cr-1" {
		t.Fatalf("url = %q", got.PendingHumanActions[0].URL)
	}
}

func TestGetGateHistory_FilterByGate(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0)
	governance := &fakeWorkItemWorkBoard{
		changeRequests: []workboard.ChangeRequest{{ID: "cr-1", Title: "Test"}},
		gateRuns: map[string][]workboard.GateRun{
			"cr-1": {
				{ID: "gr-1", Gate: "scope_clear", State: workboard.NextActionStatePass, CreatedAt: now},
				{ID: "gr-2", Gate: "scope_clear", State: workboard.NextActionStateWarn, CreatedAt: time.Unix(900, 0)},
				{ID: "gr-3", Gate: "canonical_spec", State: workboard.NextActionStatePending, CreatedAt: now},
			},
		},
	}

	svc := &governanceops.Service{WorkBoard: governance}
	handler := NewGetGateHistoryHandler(svc)
	out, err := handler(context.Background(), GetGateHistoryInput{ChangeRequestID: "cr-1", Gate: "scope_clear"})
	if err != nil {
		t.Fatalf("GetGateHistory: %v", err)
	}

	var got struct {
		Runs []struct {
			Gate string `json:"gate"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Runs) != 2 {
		t.Fatalf("runs = %d, want 2", len(got.Runs))
	}
	for _, r := range got.Runs {
		if r.Gate != "scope_clear" {
			t.Fatalf("unexpected gate %q in filtered history", r.Gate)
		}
	}
}

// --- create_quick_work_item handler tests ---

// fakeQuickWorkItemCreator implements governanceops.AgentsRunner for create_quick_work_item tests.
type fakeQuickWorkItemCreator struct {
	result map[string]any
	err    error
	acs    []string
}

func (f *fakeQuickWorkItemCreator) RunReadiness(_ context.Context, artifactID string) (*agentsclient.Verdict, error) {
	return &agentsclient.Verdict{ArtifactID: artifactID}, nil
}
func (f *fakeQuickWorkItemCreator) RunLLMGates(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}
func (f *fakeQuickWorkItemCreator) ReviewDelivery(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}
func (f *fakeQuickWorkItemCreator) CreateQuickWorkItem(_ context.Context, title, _, _, _, _, _ string, acceptanceCriteria []string, _, _ string) (map[string]any, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.acs = acceptanceCriteria
	return f.result, nil
}

func TestNewCreateQuickWorkItemHandler_MissingTitle(t *testing.T) {
	t.Parallel()
	h := NewCreateQuickWorkItemHandler(&governanceops.Service{})
	_, err := h(context.Background(), CreateQuickWorkItemInput{Title: "", Description: "desc"})
	if err == nil || !strings.Contains(err.Error(), "title") {
		t.Fatalf("expected title error, got %v", err)
	}
}

func TestNewCreateQuickWorkItemHandler_MissingDescription(t *testing.T) {
	t.Parallel()
	h := NewCreateQuickWorkItemHandler(&governanceops.Service{})
	_, err := h(context.Background(), CreateQuickWorkItemInput{Title: "Fix login", Description: ""})
	if err == nil || !strings.Contains(err.Error(), "description") {
		t.Fatalf("expected description error, got %v", err)
	}
}

func TestNewCreateQuickWorkItemHandler_NilAgentsRunner(t *testing.T) {
	t.Parallel()
	h := NewCreateQuickWorkItemHandler(&governanceops.Service{})
	_, err := h(context.Background(), CreateQuickWorkItemInput{Title: "Fix login", Description: "Users can't log in"})
	if err == nil || !strings.Contains(err.Error(), "agents service not configured") {
		t.Fatalf("expected not-configured error, got %v", err)
	}
}

func TestNewCreateQuickWorkItemHandler_Success(t *testing.T) {
	t.Parallel()
	creator := &fakeQuickWorkItemCreator{
		result: map[string]any{
			"change_request_id":   "cr-abc",
			"change_request_key":  "CR-001",
			"feature_id":          "feat-1",
			"context_pack_uri":    "specgate://context-pack/cr-abc",
			"acceptance_criteria": []string{"The login flow succeeds."},
			"phase":               "handoff",
		},
	}
	h := NewCreateQuickWorkItemHandler(&governanceops.Service{AgentsRunner: creator})
	out, err := h(context.Background(), CreateQuickWorkItemInput{
		Title:              "Fix login",
		Description:        "Users can't log in after password reset",
		AcceptanceCriteria: []string{"The login flow succeeds."},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "cr-abc") {
		t.Errorf("expected change_request_id in output, got: %s", out)
	}
	if !strings.Contains(out, "context-pack") {
		t.Errorf("expected context_pack_uri in output, got: %s", out)
	}
	if got := strings.Join(creator.acs, "|"); got != "The login flow succeeds." {
		t.Errorf("acceptance criteria = %q", got)
	}
}
