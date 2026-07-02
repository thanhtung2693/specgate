package governanceops

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/specgate/doc-registry/internal/integrations"
)

// fakeFeedbackStore implements FeedbackStore for unit tests.
type fakeFeedbackStore struct {
	created []integrations.GovernanceFeedbackEvent
}

func (f *fakeFeedbackStore) CreateGovernanceFeedbackEvent(_ context.Context, in integrations.GovernanceFeedbackEvent) (*integrations.GovernanceFeedbackEvent, error) {
	in.ID = "feedback-1"
	f.created = append(f.created, in)
	return &in, nil
}

func (f *fakeFeedbackStore) ListGovernanceFeedbackEvents(_ context.Context, filter integrations.GovernanceFeedbackFilter) ([]integrations.GovernanceFeedbackEvent, error) {
	out := make([]integrations.GovernanceFeedbackEvent, 0, len(f.created))
	for _, row := range f.created {
		if filter.ChangeRequestID != "" && row.ChangeRequestID != filter.ChangeRequestID {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func newFeedbackService(store FeedbackStore, notify func(string, string)) *Service {
	return &Service{FeedbackStore: store, FeedbackNotify: notify}
}

// --- ReportFeedback tests ---

func TestReportFeedback_CreatesReceivedFeedbackEvent(t *testing.T) {
	t.Parallel()

	store := &fakeFeedbackStore{}
	svc := newFeedbackService(store, nil)

	result, err := svc.ReportFeedback(context.Background(), ReportFeedbackInput{
		ChangeRequestID:     "cr-123",
		EventType:           integrations.FeedbackEventCodingAgentBlockedAmbiguity,
		Severity:            "blocking",
		Summary:             "Refund acceptance criteria are ambiguous.",
		SuggestedCorrection: "Clarify partial refund loyalty reversal.",
		AffectedFiles:       []string{"services/orders/refunds.go"},
		Evidence: []FeedbackEvidence{{
			Kind: "file",
			Path: "services/orders/refunds.go",
			Line: 42,
		}},
		Agent: FeedbackAgent{Name: "cursor", Version: "unknown"},
	})
	if err != nil {
		t.Fatalf("ReportFeedback: %v", err)
	}
	if result.FeedbackEventID == "" {
		t.Fatal("result.FeedbackEventID is empty")
	}
	if len(store.created) != 1 {
		t.Fatalf("created events = %d, want 1", len(store.created))
	}
	if store.created[0].Status != integrations.FeedbackStatusReceived {
		t.Fatalf("status = %q, want %q", store.created[0].Status, integrations.FeedbackStatusReceived)
	}
	if store.created[0].EventType != integrations.FeedbackEventCodingAgentBlockedAmbiguity {
		t.Fatalf("event_type = %q", store.created[0].EventType)
	}
}

// Evidence provenance (Source) is server-stamped, never trusted from agent
// input: an agent self-claiming "ci"/"webhook" corroboration must be stripped.
func TestReportFeedback_StripsAgentProvidedProvenance(t *testing.T) {
	t.Parallel()
	store := &fakeFeedbackStore{}
	svc := newFeedbackService(store, nil)

	_, err := svc.ReportFeedback(context.Background(), ReportFeedbackInput{
		ChangeRequestID: "cr-prov",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Severity:        "info",
		Summary:         "Implemented and verified.",
		Evidence:        []FeedbackEvidence{{Kind: "file", Path: "x.go", Source: "ci"}},
		Checks:          []FeedbackCheck{{Name: "tests", Status: "pass", Source: "webhook"}},
		Criteria: []FeedbackCriterion{{
			CriterionID: "AC1",
			Claim:       "satisfied",
			Evidence:    &FeedbackEvidence{Kind: "file", Source: "ci"},
		}},
		Agent: FeedbackAgent{Name: "cursor"},
	})
	if err != nil {
		t.Fatalf("ReportFeedback: %v", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("created events = %d, want 1", len(store.created))
	}

	var payload ReportFeedbackInput
	if err := json.Unmarshal([]byte(store.created[0].PayloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got := payload.Evidence[0].Source; got != "" {
		t.Fatalf("evidence source = %q, want empty (server-stamped only)", got)
	}
	if got := payload.Checks[0].Source; got != "" {
		t.Fatalf("check source = %q, want empty (server-stamped only)", got)
	}
	if payload.Criteria[0].Evidence == nil || payload.Criteria[0].Evidence.Source != "" {
		t.Fatalf("criterion evidence source = %#v, want empty", payload.Criteria[0].Evidence)
	}
}

// notify fires once on a newly-recorded event, NOT on a deduplicated re-report.
func TestReportFeedback_NotifiesOnNewEventNotOnDedup(t *testing.T) {
	t.Parallel()
	store := &fakeFeedbackStore{}
	var got [][2]string
	notify := func(crID, eventType string) {
		got = append(got, [2]string{crID, eventType})
	}
	svc := newFeedbackService(store, notify)

	in := ReportFeedbackInput{
		ChangeRequestID: "cr-9",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Severity:        "info",
		Summary:         "Done.",
		DedupeKey:       "k1",
	}
	if _, err := svc.ReportFeedback(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReportFeedback(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if len(store.created) != 1 {
		t.Fatalf("created = %d, want 1 (deduped)", len(store.created))
	}
	if len(got) != 1 || got[0] != [2]string{"cr-9", integrations.FeedbackEventCodingAgentCompleted} {
		t.Fatalf("notify calls = %v, want one [cr-9, coding_agent.completed]", got)
	}
}

func TestReportFeedback_RoundtripsChecksAndCriteria(t *testing.T) {
	t.Parallel()
	store := &fakeFeedbackStore{}
	svc := newFeedbackService(store, nil)

	_, err := svc.ReportFeedback(context.Background(), ReportFeedbackInput{
		ChangeRequestID: "cr-1",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Severity:        "info",
		Summary:         "Implemented redeem/unredeem.",
		Checks: []FeedbackCheck{
			{Name: "tests", Status: "pass"},
			{Name: "lint", Status: "fail", Detail: "2 issues"},
		},
		Criteria: []FeedbackCriterion{
			{CriterionID: "ac-1", Claim: "satisfied", Evidence: &FeedbackEvidence{Kind: "file", Path: "x.go"}},
			{Text: "Show CTA", Claim: "not_done"},
		},
	})
	if err != nil {
		t.Fatalf("ReportFeedback: %v", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("created = %d, want 1", len(store.created))
	}
	payload := store.created[0].PayloadJSON
	for _, want := range []string{`"checks"`, `"tests"`, `"lint"`, `"criteria"`, `"satisfied"`, `"not_done"`, `"ac-1"`} {
		if !strings.Contains(payload, want) {
			t.Fatalf("payload missing %s: %s", want, payload)
		}
	}
}

func TestReportFeedback_CompletedWithoutChecksIsBackwardCompatible(t *testing.T) {
	t.Parallel()
	store := &fakeFeedbackStore{}
	svc := newFeedbackService(store, nil)

	_, err := svc.ReportFeedback(context.Background(), ReportFeedbackInput{
		ChangeRequestID: "cr-1",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Severity:        "info",
		Summary:         "done",
	})
	if err != nil {
		t.Fatalf("ReportFeedback: %v", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("created = %d, want 1", len(store.created))
	}
	if strings.Contains(store.created[0].PayloadJSON, `"checks"`) {
		t.Fatalf("omitempty failed — unexpected checks: %s", store.created[0].PayloadJSON)
	}
}

func TestReportFeedback_DedupesRunID(t *testing.T) {
	t.Parallel()
	store := &fakeFeedbackStore{}
	svc := newFeedbackService(store, nil)

	input := ReportFeedbackInput{
		ChangeRequestID: "cr-123",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Severity:        "info",
		Summary:         "Implementation complete.",
		RunID:           "run-1",
		Agent:           FeedbackAgent{Name: "codex"},
	}

	first, err := svc.ReportFeedback(context.Background(), input)
	if err != nil {
		t.Fatalf("first ReportFeedback: %v", err)
	}
	second, err := svc.ReportFeedback(context.Background(), input)
	if err != nil {
		t.Fatalf("second ReportFeedback: %v", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("created events = %d, want 1", len(store.created))
	}
	if first.FeedbackEventID != second.FeedbackEventID {
		t.Fatalf("dedupe IDs differ: first=%s second=%s", first.FeedbackEventID, second.FeedbackEventID)
	}
}

func TestReportFeedback_CheckSourceOmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	store := &fakeFeedbackStore{}
	svc := newFeedbackService(store, nil)

	_, err := svc.ReportFeedback(context.Background(), ReportFeedbackInput{
		ChangeRequestID: "cr-src-3",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Severity:        "info",
		Summary:         "done",
		Checks:          []FeedbackCheck{{Name: "tests", Status: "pass"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	payload := store.created[0].PayloadJSON
	if strings.Contains(payload, `"source"`) {
		t.Fatalf("omitempty failed — unexpected source field in: %s", payload)
	}
}

func TestReportFeedback_NilStoreErrors(t *testing.T) {
	t.Parallel()
	svc := &Service{}
	_, err := svc.ReportFeedback(context.Background(), ReportFeedbackInput{
		ChangeRequestID: "cr-1",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Severity:        "info",
		Summary:         "done",
	})
	if err == nil {
		t.Fatal("expected error for nil store")
	}
}

// TestFeedbackCheck_SourceRoundtrips verifies the Source field exists and round-trips.
func TestFeedbackCheck_SourceRoundtrips(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(FeedbackCheck{Name: "tests", Status: "pass", Source: "ci"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"source":"ci"`) {
		t.Fatalf("payload missing ci source value: %s", b)
	}
	var out FeedbackCheck
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Source != "ci" {
		t.Fatalf("source = %q, want ci", out.Source)
	}
}

// TestFeedbackEvidence_SourceRoundtrips verifies the Source field on FeedbackEvidence round-trips.
func TestFeedbackEvidence_SourceRoundtrips(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(FeedbackEvidence{Kind: "ci_run", URL: "https://ci.example.com/1", Source: "webhook"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"source":"webhook"`) {
		t.Fatalf("payload missing webhook source: %s", b)
	}
	var out FeedbackEvidence
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Source != "webhook" {
		t.Fatalf("source = %q, want webhook", out.Source)
	}
}

// --- Clarifications tests ---

func TestClarifications_ReturnsAnsweredAmbiguityEvents(t *testing.T) {
	t.Parallel()

	store := &fakeFeedbackStore{
		created: []integrations.GovernanceFeedbackEvent{
			{
				ID:              "fb-1",
				ChangeRequestID: "cr-1",
				EventType:       integrations.FeedbackEventCodingAgentBlockedAmbiguity,
				Status:          "accepted",
				Reason:          "Use the refund table.",
				PayloadJSON:     `{"summary":"What table?","suggested_correction":""}`,
			},
			{
				ID:              "fb-2",
				ChangeRequestID: "cr-1",
				EventType:       integrations.FeedbackEventCodingAgentCompleted,
				Status:          "received",
				Reason:          "Done.",
				PayloadJSON:     `{"summary":"done"}`,
			},
		},
	}
	svc := &Service{FeedbackStore: store}

	result, err := svc.Clarifications(context.Background(), ClarificationsInput{ChangeRequestID: "cr-1"})
	if err != nil {
		t.Fatalf("Clarifications: %v", err)
	}
	if !result.Found {
		t.Fatal("found = false, want true")
	}
	if len(result.Clarifications) != 1 {
		t.Fatalf("clarifications len = %d, want 1", len(result.Clarifications))
	}
	item := result.Clarifications[0]
	if item.FeedbackEventID != "fb-1" {
		t.Errorf("feedback_event_id = %q, want fb-1", item.FeedbackEventID)
	}
	if item.QuestionMD != "What table?" {
		t.Errorf("question_md = %q, want 'What table?'", item.QuestionMD)
	}
	if item.AnswerMD != "Use the refund table." {
		t.Errorf("answer_md = %q, want 'Use the refund table.'", item.AnswerMD)
	}
	if item.Status != "accepted" {
		t.Errorf("status = %q, want accepted", item.Status)
	}
}

func TestClarifications_ExcludesUnresolved(t *testing.T) {
	t.Parallel()

	store := &fakeFeedbackStore{
		created: []integrations.GovernanceFeedbackEvent{
			{
				ID:              "fb-1",
				ChangeRequestID: "cr-1",
				EventType:       integrations.FeedbackEventCodingAgentBlockedAmbiguity,
				Status:          "received", // not accepted/rejected
				Reason:          "Unanswered.",
				PayloadJSON:     `{"summary":"Open question"}`,
			},
		},
	}
	svc := &Service{FeedbackStore: store}

	result, err := svc.Clarifications(context.Background(), ClarificationsInput{ChangeRequestID: "cr-1"})
	if err != nil {
		t.Fatalf("Clarifications: %v", err)
	}
	if result.Found {
		t.Fatal("found = true, want false for unresolved-only")
	}
	if len(result.Clarifications) != 0 {
		t.Fatalf("clarifications len = %d, want 0", len(result.Clarifications))
	}
}

func TestClarifications_NilStoreErrors(t *testing.T) {
	t.Parallel()
	svc := &Service{}
	_, err := svc.Clarifications(context.Background(), ClarificationsInput{ChangeRequestID: "cr-1"})
	if err == nil {
		t.Fatal("expected error for nil store")
	}
}
