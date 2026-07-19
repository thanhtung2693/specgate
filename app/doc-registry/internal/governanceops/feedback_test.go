package governanceops

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
	"github.com/specgate/doc-registry/internal/workspace"
)

// fakeFeedbackStore implements FeedbackStore for unit tests.
type fakeFeedbackStore struct {
	created []integrations.GovernanceFeedbackEvent
}

func (f *fakeFeedbackStore) CreateGovernanceFeedbackEvent(_ context.Context, in integrations.GovernanceFeedbackEvent) (*integrations.GovernanceFeedbackEvent, error) {
	in.ID = fmt.Sprintf("feedback-%d", len(f.created)+1)
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
	return &Service{
		FeedbackStore:  store,
		FeedbackNotify: notify,
		WorkBoard:      &fakeWorkBoardReader{criteria: map[string][]workboard.AcceptanceCriterion{"cr-peer-ok": {{ID: "ac-1"}}}},
	}
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

func TestReportFeedback_RejectsCompletionWithoutAgentName(t *testing.T) {
	t.Parallel()
	store := &fakeFeedbackStore{}
	svc := newFeedbackService(store, nil)

	_, err := svc.ReportFeedback(context.Background(), ReportFeedbackInput{
		ChangeRequestID: "cr-1",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Summary:         "Done.",
	})
	if err == nil || !strings.Contains(err.Error(), "completion agent.name is required") {
		t.Fatalf("ReportFeedback error = %v", err)
	}
	if len(store.created) != 0 {
		t.Fatalf("created events = %d, want 0", len(store.created))
	}
}

func TestReportFeedbackRejectsNewCompletionAfterHumanAcceptance(t *testing.T) {
	t.Parallel()
	store := &fakeFeedbackStore{}
	svc := &Service{
		FeedbackStore: store,
		WorkBoard: &fakeWorkBoardReader{crs: []workboard.ChangeRequest{{
			ID: "cr-accepted", WorkspaceID: "ws-a", Archived: true,
			Phase: workboard.BoardPhaseDelivered,
		}}},
	}

	_, err := svc.ReportFeedback(
		workspace.WithID(context.Background(), "ws-a"),
		ReportFeedbackInput{
			ChangeRequestID: "cr-accepted",
			EventType:       integrations.FeedbackEventCodingAgentCompleted,
			Summary:         "A later completion must start as new work.",
			Agent:           FeedbackAgent{Name: "builder"},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "already accepted") {
		t.Fatalf("ReportFeedback error = %v, want accepted-work validation", err)
	}
	if len(store.created) != 0 {
		t.Fatalf("created events = %d, want 0", len(store.created))
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
		Agent:           FeedbackAgent{Name: "builder"},
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
		Agent:           FeedbackAgent{Name: "builder"},
		Checks: []FeedbackCheck{
			{Name: "tests", Status: "pass", Command: "go test ./..."},
			{Name: "lint", Status: "fail", Detail: "2 issues"},
		},
		Criteria: []FeedbackCriterion{
			{CriterionID: "ac-1", Claim: "satisfied", VerificationBinding: "trust-smoke", Evidence: &FeedbackEvidence{Kind: "file", Path: "x.go"}},
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
	for _, want := range []string{`"checks"`, `"tests"`, `"command":"go test ./..."`, `"lint"`, `"criteria"`, `"satisfied"`, `"not_done"`, `"ac-1"`, `"verification_binding"`, "trust-smoke"} {
		if !strings.Contains(payload, want) {
			t.Fatalf("payload missing %s: %s", want, payload)
		}
	}
}

func TestReportFeedback_RoundtripsCriterionEvidenceGrounding(t *testing.T) {
	t.Parallel()
	store := &fakeFeedbackStore{}
	svc := newFeedbackService(store, nil)

	_, err := svc.ReportFeedback(context.Background(), ReportFeedbackInput{
		ChangeRequestID: "cr-grounded",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Severity:        "info",
		Summary:         "Implemented handler.",
		Agent:           FeedbackAgent{Name: "builder"},
		Criteria: []FeedbackCriterion{{
			CriterionID: "ac-1",
			Claim:       "satisfied",
			Evidence: &FeedbackEvidence{
				Kind: "file",
				Path: "handler.go",
				Grounding: &FeedbackGrounding{
					Status:  "grounded",
					Excerpt: "func Handler() {}",
					Digest:  "sha256:abc",
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("ReportFeedback: %v", err)
	}
	var payload ReportFeedbackInput
	if err := json.Unmarshal([]byte(store.created[0].PayloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Criteria[0].Evidence == nil || payload.Criteria[0].Evidence.Grounding == nil {
		t.Fatalf("grounding missing from payload: %s", store.created[0].PayloadJSON)
	}
	if payload.Criteria[0].Evidence.Grounding.Excerpt != "func Handler() {}" {
		t.Fatalf("grounding = %#v", payload.Criteria[0].Evidence.Grounding)
	}
}

func TestReportFeedback_RejectsPeerReviewFromCompletionAgent(t *testing.T) {
	t.Parallel()
	store := &fakeFeedbackStore{}
	svc := newFeedbackService(store, nil)

	_, err := svc.ReportFeedback(context.Background(), ReportFeedbackInput{
		ChangeRequestID: "cr-peer",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Severity:        "info",
		Summary:         "Implemented.",
		Agent:           FeedbackAgent{Name: "codex"},
		GitReceipt:      &GitReceipt{Repository: "specgate", HeadRevision: "abc"},
	})
	if err != nil {
		t.Fatalf("completion ReportFeedback: %v", err)
	}

	_, err = svc.ReportFeedback(context.Background(), ReportFeedbackInput{
		ChangeRequestID: "cr-peer",
		EventType:       integrations.FeedbackEventCodingAgentPeerReviewed,
		Severity:        "info",
		Summary:         "Peer reviewed.",
		Agent:           FeedbackAgent{Name: "codex"},
	})
	if err == nil || !strings.Contains(err.Error(), "cannot peer-review its own completion") {
		t.Fatalf("peer review err = %v, want self-review validation", err)
	}
}

func TestReportFeedback_AcceptsPeerReviewFromDifferentAgent(t *testing.T) {
	t.Parallel()
	store := &fakeFeedbackStore{}
	svc := newFeedbackService(store, nil)

	_, err := svc.ReportFeedback(context.Background(), ReportFeedbackInput{
		ChangeRequestID: "cr-peer-ok",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Severity:        "info",
		Summary:         "Implemented.",
		Agent:           FeedbackAgent{Name: "codex"},
		GitReceipt:      &GitReceipt{Repository: "specgate", HeadRevision: "abc"},
	})
	if err != nil {
		t.Fatalf("completion ReportFeedback: %v", err)
	}

	_, err = svc.ReportFeedback(context.Background(), ReportFeedbackInput{
		ChangeRequestID: "cr-peer-ok",
		EventType:       integrations.FeedbackEventCodingAgentPeerReviewed,
		Severity:        "info",
		Summary:         "Peer reviewed.",
		Agent:           FeedbackAgent{Name: "cursor"},
		PeerReviewOf: &PeerReviewBinding{
			CompletionFeedbackEventID: "feedback-1",
			GitReceipt:                &GitReceipt{Repository: "specgate", HeadRevision: "abc"},
		},
		Criteria: []FeedbackCriterion{{
			CriterionID: "ac-1",
			Claim:       "satisfied",
			Evidence:    &FeedbackEvidence{Kind: "file", Path: "implementation.go"},
		}},
	})
	if err != nil {
		t.Fatalf("peer review ReportFeedback: %v", err)
	}
	if len(store.created) != 2 {
		t.Fatalf("created = %d, want completion + peer review", len(store.created))
	}
}

func TestReportFeedback_PeerReviewBindsHighestIDWhenCompletionTimestampsTie(t *testing.T) {
	t.Parallel()
	createdAt := time.Unix(100, 0).UTC()
	store := &fakeFeedbackStore{created: []integrations.GovernanceFeedbackEvent{
		{
			ID:              "completion-a",
			ChangeRequestID: "cr-peer-ok",
			EventType:       integrations.FeedbackEventCodingAgentCompleted,
			PayloadJSON:     `{"agent":{"name":"builder-a"},"git_receipt":{"repository":"specgate","head_revision":"aaa"}}`,
			CreatedAt:       createdAt,
		},
		{
			ID:              "completion-z",
			ChangeRequestID: "cr-peer-ok",
			EventType:       integrations.FeedbackEventCodingAgentCompleted,
			PayloadJSON:     `{"agent":{"name":"builder-z"},"git_receipt":{"repository":"specgate","head_revision":"zzz"}}`,
			CreatedAt:       createdAt,
		},
	}}
	svc := newFeedbackService(store, nil)

	_, err := svc.ReportFeedback(context.Background(), ReportFeedbackInput{
		ChangeRequestID: "cr-peer-ok",
		EventType:       integrations.FeedbackEventCodingAgentPeerReviewed,
		Severity:        "info",
		Summary:         "Reviewed the deterministic latest completion.",
		Agent:           FeedbackAgent{Name: "reviewer"},
		PeerReviewOf: &PeerReviewBinding{
			CompletionFeedbackEventID: "completion-z",
			GitReceipt:                &GitReceipt{Repository: "specgate", HeadRevision: "zzz"},
		},
		Criteria: []FeedbackCriterion{{
			CriterionID: "ac-1",
			Claim:       "satisfied",
			Evidence:    &FeedbackEvidence{Kind: "file", Path: "implementation.go"},
		}},
	})
	if err != nil {
		t.Fatalf("peer review should bind completion-z: %v", err)
	}
}

func TestReportFeedback_RejectsSatisfiedPeerCriterionWithoutEvidence(t *testing.T) {
	t.Parallel()
	store := &fakeFeedbackStore{}
	svc := newFeedbackService(store, nil)
	_, err := svc.ReportFeedback(context.Background(), ReportFeedbackInput{
		ChangeRequestID: "cr-peer-ok",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Severity:        "info",
		Summary:         "Implemented.",
		Agent:           FeedbackAgent{Name: "builder"},
		GitReceipt:      &GitReceipt{Repository: "specgate", HeadRevision: "abc"},
	})
	if err != nil {
		t.Fatalf("completion ReportFeedback: %v", err)
	}
	_, err = svc.ReportFeedback(context.Background(), ReportFeedbackInput{
		ChangeRequestID: "cr-peer-ok",
		EventType:       integrations.FeedbackEventCodingAgentPeerReviewed,
		Severity:        "info",
		Summary:         "Peer reviewed.",
		Agent:           FeedbackAgent{Name: "reviewer"},
		PeerReviewOf: &PeerReviewBinding{
			CompletionFeedbackEventID: "feedback-1",
			GitReceipt:                &GitReceipt{Repository: "specgate", HeadRevision: "abc"},
		},
		Criteria: []FeedbackCriterion{{CriterionID: "ac-1", Claim: "satisfied"}},
	})
	if err == nil || !strings.Contains(err.Error(), "has no evidence") {
		t.Fatalf("peer review error = %v, want evidence validation", err)
	}
}

func TestReportFeedback_RejectsUnboundPeerReview(t *testing.T) {
	t.Parallel()
	store := &fakeFeedbackStore{}
	svc := newFeedbackService(store, nil)

	_, err := svc.ReportFeedback(context.Background(), ReportFeedbackInput{
		ChangeRequestID: "cr-peer-ok",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Severity:        "info",
		Summary:         "Implemented.",
		Agent:           FeedbackAgent{Name: "codex"},
	})
	if err != nil {
		t.Fatalf("completion ReportFeedback: %v", err)
	}
	_, err = svc.ReportFeedback(context.Background(), ReportFeedbackInput{
		ChangeRequestID: "cr-peer-ok",
		EventType:       integrations.FeedbackEventCodingAgentPeerReviewed,
		Severity:        "info",
		Summary:         "Peer reviewed.",
		Agent:           FeedbackAgent{Name: "cursor"},
		Criteria:        []FeedbackCriterion{{CriterionID: "ac-1", Claim: "satisfied"}},
	})
	if err == nil || !strings.Contains(err.Error(), "completion_feedback_event_id") {
		t.Fatalf("peer review err = %v, want binding validation", err)
	}
}

func TestReportFeedback_RejectsCheckStatusAliases(t *testing.T) {
	t.Parallel()
	store := &fakeFeedbackStore{}
	svc := newFeedbackService(store, nil)

	for _, status := range []string{"passed", "failed", "skip"} {
		_, err := svc.ReportFeedback(context.Background(), ReportFeedbackInput{
			ChangeRequestID: "cr-alias",
			EventType:       integrations.FeedbackEventCodingAgentCompleted,
			Severity:        "info",
			Summary:         "Implemented and verified.",
			Agent:           FeedbackAgent{Name: "builder"},
			Checks:          []FeedbackCheck{{Name: "tests", Status: status}},
		})
		if err == nil {
			t.Fatalf("status %q accepted, want exact enum validation", status)
		}
	}
	if len(store.created) != 0 {
		t.Fatalf("created events = %d, want 0", len(store.created))
	}
}

func TestReportFeedback_RejectsCriterionClaimAliases(t *testing.T) {
	t.Parallel()
	store := &fakeFeedbackStore{}
	svc := newFeedbackService(store, nil)

	_, err := svc.ReportFeedback(context.Background(), ReportFeedbackInput{
		ChangeRequestID: "cr-alias",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Severity:        "info",
		Summary:         "Implemented.",
		Agent:           FeedbackAgent{Name: "builder"},
		Criteria: []FeedbackCriterion{{
			CriterionID: "ac-1",
			Claim:       "SATISFIED",
			Evidence:    &FeedbackEvidence{Kind: "file", Path: "implementation.go"},
		}},
	})
	if err == nil {
		t.Fatal("uppercase claim accepted, want exact enum validation")
	}
	if len(store.created) != 0 {
		t.Fatalf("created events = %d, want 0", len(store.created))
	}
}

func TestReportFeedback_RejectsUnsupportedCheckStatus(t *testing.T) {
	t.Parallel()
	store := &fakeFeedbackStore{}
	svc := newFeedbackService(store, nil)

	_, err := svc.ReportFeedback(context.Background(), ReportFeedbackInput{
		ChangeRequestID: "cr-bad-status",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Severity:        "info",
		Summary:         "Implemented and verified.",
		Agent:           FeedbackAgent{Name: "builder"},
		Checks:          []FeedbackCheck{{Name: "tests", Status: "green"}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "unsupported check status") {
		t.Fatalf("error = %v, want unsupported check status", err)
	}
	if len(store.created) != 0 {
		t.Fatalf("created events = %d, want 0", len(store.created))
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
		Agent:           FeedbackAgent{Name: "builder"},
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
		Agent:           FeedbackAgent{Name: "builder"},
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

func TestReportFeedback_RoundtripsGitReceipt(t *testing.T) {
	t.Parallel()
	store := &fakeFeedbackStore{}
	svc := newFeedbackService(store, nil)
	want := &GitReceipt{
		Repository:   "https://github.com/acme/project.git",
		Availability: "available",
		Branch:       "feature/receipt",
		BaseRevision: "base123",
		HeadRevision: "head456",
		ChangedFiles: []string{"app.go", "README.md"},
		DiffDigest:   "sha256:abc123",
		Warnings:     []string{"reported files differ from Git status"},
	}
	_, err := svc.ReportFeedback(context.Background(), ReportFeedbackInput{
		ChangeRequestID: "cr-receipt",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Severity:        "info",
		Summary:         "Implemented and verified.",
		Agent:           FeedbackAgent{Name: "builder"},
		GitReceipt:      want,
	})
	if err != nil {
		t.Fatalf("ReportFeedback: %v", err)
	}
	var payload ReportFeedbackInput
	if err := json.Unmarshal([]byte(store.created[0].PayloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.GitReceipt == nil {
		t.Fatal("git_receipt missing from persisted payload")
	}
	if !reflect.DeepEqual(payload.GitReceipt, want) {
		t.Fatalf("git_receipt = %#v, want %#v", payload.GitReceipt, want)
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
