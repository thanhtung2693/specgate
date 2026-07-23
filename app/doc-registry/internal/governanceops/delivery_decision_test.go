package governanceops

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
)

type fakeDeliveryDecisionWorkBoard struct {
	runs []workboard.GateRun
	in   workboard.DeliveryDecisionInput
}

func (f *fakeDeliveryDecisionWorkBoard) ListChangeRequests(_ context.Context, _ bool) ([]workboard.ChangeRequest, error) {
	return nil, nil
}

func (f *fakeDeliveryDecisionWorkBoard) GetChangeRequest(_ context.Context, id string) (*workboard.ChangeRequest, error) {
	return &workboard.ChangeRequest{ID: id, Key: "CR-1", Title: "Delivery decision"}, nil
}

func (f *fakeDeliveryDecisionWorkBoard) GetFeature(_ context.Context, _ string) (*workboard.Feature, error) {
	return nil, workboard.ErrNotFound
}

func (f *fakeDeliveryDecisionWorkBoard) ListAcceptanceCriteria(_ context.Context, _ string) ([]workboard.AcceptanceCriterion, error) {
	return nil, nil
}

func (f *fakeDeliveryDecisionWorkBoard) ListGateRuns(_ context.Context, _ string, _ int) ([]workboard.GateRun, error) {
	return f.runs, nil
}

func (f *fakeDeliveryDecisionWorkBoard) ListStaleWarnings(_ context.Context, _ workboard.StaleWarningFilter) ([]workboard.StaleWarning, error) {
	return nil, nil
}

func (f *fakeDeliveryDecisionWorkBoard) RecordDeliveryDecision(_ context.Context, in workboard.DeliveryDecisionInput) (*workboard.GateRun, error) {
	f.in = in
	return &workboard.GateRun{
		ID:           "run-human",
		SubjectID:    in.ChangeRequestID,
		Gate:         "delivery_review",
		State:        workboard.NextActionStatePass,
		Executor:     workboard.GateRunExecutorHuman,
		Hint:         "delivery accepted",
		CreatedAt:    time.Unix(100, 0).UTC(),
		EvidenceJSON: `{"evaluator":{"type":"human_decision","actor":"` + in.Actor + `","trust":"human_decision"},"decision":"approve"}`,
	}, nil
}

type fakeDeliveryDecisionFeedbackStore struct {
	rows []integrations.GovernanceFeedbackEvent
}

func (f *fakeDeliveryDecisionFeedbackStore) CreateGovernanceFeedbackEvent(_ context.Context, in integrations.GovernanceFeedbackEvent) (*integrations.GovernanceFeedbackEvent, error) {
	return &in, nil
}

func (f *fakeDeliveryDecisionFeedbackStore) ListGovernanceFeedbackEvents(_ context.Context, filter integrations.GovernanceFeedbackFilter) ([]integrations.GovernanceFeedbackEvent, error) {
	out := make([]integrations.GovernanceFeedbackEvent, 0, len(f.rows))
	for _, row := range f.rows {
		if filter.ChangeRequestID != "" && row.ChangeRequestID != filter.ChangeRequestID {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func reviewedDeliveryRun(state workboard.NextActionState) []workboard.GateRun {
	return []workboard.GateRun{{
		ID: "run-platform", Gate: "delivery_review", State: state,
		Executor: workboard.GateRunExecutorPlatform, CreatedAt: time.Unix(50, 0).UTC(),
		EvidenceJSON: `{"completion_feedback_event_id":"completion-1"}`,
	}}
}

func TestDeliveryDecisionRequiresCurrentPlatformReview(t *testing.T) {
	t.Parallel()
	workBoard := &fakeDeliveryDecisionWorkBoard{}
	svc := &Service{WorkBoard: workBoard}

	_, err := svc.DecideDelivery(context.Background(), DeliveryDecisionInput{
		ChangeRequestID: "cr-1", Decision: "approve", Actor: "lead",
	})
	if err == nil || !strings.Contains(err.Error(), "run delivery review") {
		t.Fatalf("error = %v, want missing-review validation", err)
	}
}

func TestDeliveryDecisionRejectsSelfApprovalFromCompletionReporter(t *testing.T) {
	t.Parallel()
	workBoard := &fakeDeliveryDecisionWorkBoard{runs: reviewedDeliveryRun(workboard.NextActionStatePass)}
	feedback := &fakeDeliveryDecisionFeedbackStore{
		rows: []integrations.GovernanceFeedbackEvent{{
			ID:              "completion-1",
			ChangeRequestID: "cr-1",
			EventType:       integrations.FeedbackEventCodingAgentCompleted,
			PayloadJSON:     `{"agent":{"name":"codex"},"summary":"done"}`,
			CreatedAt:       time.Unix(100, 0).UTC(),
		}},
	}
	svc := &Service{WorkBoard: workBoard, FeedbackStore: feedback}

	_, err := svc.DecideDelivery(context.Background(), DeliveryDecisionInput{
		ChangeRequestID: "cr-1",
		Decision:        "approve",
		Actor:           "codex",
		Note:            "looks good",
	})
	if err == nil || !strings.Contains(err.Error(), "cannot approve its own delivery") {
		t.Fatalf("expected self-approval validation error, got %v", err)
	}
	if workBoard.in.ChangeRequestID != "" {
		t.Fatalf("RecordDeliveryDecision was called despite self-approval: %+v", workBoard.in)
	}
}

func TestDeliveryDecisionAllowsDifferentHumanReviewer(t *testing.T) {
	t.Parallel()
	workBoard := &fakeDeliveryDecisionWorkBoard{runs: reviewedDeliveryRun(workboard.NextActionStateNeedsHumanReview)}
	feedback := &fakeDeliveryDecisionFeedbackStore{
		rows: []integrations.GovernanceFeedbackEvent{{
			ID:              "completion-1",
			ChangeRequestID: "cr-1",
			EventType:       integrations.FeedbackEventCodingAgentCompleted,
			PayloadJSON:     `{"agent":{"name":"codex"},"summary":"done"}`,
			CreatedAt:       time.Unix(100, 0).UTC(),
		}},
	}
	svc := &Service{WorkBoard: workBoard, FeedbackStore: feedback}

	result, err := svc.DecideDelivery(context.Background(), DeliveryDecisionInput{
		ChangeRequestID: "cr-1",
		Decision:        "approve",
		Actor:           "lead@example.com",
		Note:            "evidence checks out",
	})
	if err != nil {
		t.Fatalf("DecideDelivery: %v", err)
	}
	if result.Verdict != "pass" || result.Executor != workboard.GateRunExecutorHuman {
		t.Fatalf("result = %+v, want human pass", result)
	}
	if workBoard.in.Actor != "lead@example.com" || workBoard.in.Note != "evidence checks out" {
		t.Fatalf("decision input = %+v", workBoard.in)
	}
	if workBoard.in.ReviewedGateRunID != "run-platform" ||
		workBoard.in.CompletionFeedbackEventID != "completion-1" {
		t.Fatalf("decision CAS input = %+v, want exact review and completion IDs", workBoard.in)
	}
}

func TestDeliveryDecisionRejectsConfirmationForDifferentReviewCycle(t *testing.T) {
	t.Parallel()
	workBoard := &fakeDeliveryDecisionWorkBoard{runs: reviewedDeliveryRun(workboard.NextActionStatePass)}
	feedback := &fakeDeliveryDecisionFeedbackStore{rows: []integrations.GovernanceFeedbackEvent{{
		ID: "completion-1", ChangeRequestID: "cr-1",
		EventType:   integrations.FeedbackEventCodingAgentCompleted,
		PayloadJSON: `{"agent":{"name":"codex"},"summary":"done"}`,
		CreatedAt:   time.Unix(100, 0).UTC(),
	}}}
	svc := &Service{WorkBoard: workBoard, FeedbackStore: feedback}

	for _, tc := range []struct {
		name         string
		gateRunID    string
		completionID string
	}{
		{name: "review changed", gateRunID: "run-platform-older", completionID: "completion-1"},
		{name: "completion changed", gateRunID: "run-platform", completionID: "completion-older"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.DecideDelivery(context.Background(), DeliveryDecisionInput{
				ChangeRequestID:           "cr-1",
				Decision:                  "approve",
				Actor:                     "lead@example.com",
				ReviewedGateRunID:         tc.gateRunID,
				CompletionFeedbackEventID: tc.completionID,
			})
			if err == nil || !strings.Contains(err.Error(), "reviewed delivery changed") {
				t.Fatalf("error = %v, want stale-confirmation validation", err)
			}
			if workBoard.in.ChangeRequestID != "" {
				t.Fatalf("RecordDeliveryDecision called for stale confirmation: %+v", workBoard.in)
			}
		})
	}
}

func TestDeliveryDecisionRejectsUnboundPlatformReviewWhenCompletionExists(t *testing.T) {
	t.Parallel()
	workBoard := &fakeDeliveryDecisionWorkBoard{runs: []workboard.GateRun{{
		ID: "legacy-unbound-platform", Gate: "delivery_review",
		State: workboard.NextActionStatePass, Executor: workboard.GateRunExecutorPlatform,
		CreatedAt: time.Unix(50, 0).UTC(),
	}}}
	feedback := &fakeDeliveryDecisionFeedbackStore{rows: []integrations.GovernanceFeedbackEvent{{
		ID: "completion-1", ChangeRequestID: "cr-1",
		EventType:   integrations.FeedbackEventCodingAgentCompleted,
		PayloadJSON: `{"agent":{"name":"codex"},"summary":"done"}`,
		CreatedAt:   time.Unix(100, 0).UTC(),
	}}}

	_, err := (&Service{WorkBoard: workBoard, FeedbackStore: feedback}).DecideDelivery(
		context.Background(),
		DeliveryDecisionInput{
			ChangeRequestID: "cr-1", Decision: "approve", Actor: "lead@example.com",
		},
	)
	if err == nil || !strings.Contains(err.Error(), "latest completion has not been reviewed") {
		t.Fatalf("error = %v, want unbound-review validation", err)
	}
	if workBoard.in.ChangeRequestID != "" {
		t.Fatalf("RecordDeliveryDecision called for unbound platform review: %+v", workBoard.in)
	}
}

func TestDeliveryDecisionRejectsReviewBoundToOlderCompletion(t *testing.T) {
	t.Parallel()
	workBoard := &fakeDeliveryDecisionWorkBoard{runs: []workboard.GateRun{{
		ID: "run-platform-completion-1", Gate: "delivery_review",
		State: workboard.NextActionStatePass, Executor: workboard.GateRunExecutorPlatform,
		EvidenceJSON: `{"completion_feedback_event_id":"completion-1"}`,
		CreatedAt:    time.Unix(100, 0).UTC(),
	}}}
	feedback := &fakeDeliveryDecisionFeedbackStore{
		rows: []integrations.GovernanceFeedbackEvent{{
			ID:              "completion-2",
			ChangeRequestID: "cr-1",
			EventType:       integrations.FeedbackEventCodingAgentCompleted,
			PayloadJSON:     `{"agent":{"name":"codex"},"summary":"corrected"}`,
			CreatedAt:       time.Unix(200, 0).UTC(),
		}},
	}
	svc := &Service{WorkBoard: workBoard, FeedbackStore: feedback}

	_, err := svc.DecideDelivery(context.Background(), DeliveryDecisionInput{
		ChangeRequestID: "cr-1",
		Decision:        "approve",
		Actor:           "lead@example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "latest completion has not been reviewed") {
		t.Fatalf("error = %v, want stale-review validation", err)
	}
	if workBoard.in.ChangeRequestID != "" {
		t.Fatalf("RecordDeliveryDecision called for stale review: %+v", workBoard.in)
	}
}

func TestLatestCompletionRecordBreaksTimestampTiesByID(t *testing.T) {
	t.Parallel()
	createdAt := time.Unix(100, 0).UTC()
	svc := &Service{FeedbackStore: &fakeDeliveryDecisionFeedbackStore{
		rows: []integrations.GovernanceFeedbackEvent{
			{
				ID:              "completion-a",
				ChangeRequestID: "cr-1",
				EventType:       integrations.FeedbackEventCodingAgentCompleted,
				PayloadJSON:     `{"summary":"older id"}`,
				CreatedAt:       createdAt,
			},
			{
				ID:              "completion-z",
				ChangeRequestID: "cr-1",
				EventType:       integrations.FeedbackEventCodingAgentCompleted,
				PayloadJSON:     `{"summary":"newer id"}`,
				CreatedAt:       createdAt,
			},
		},
	}}

	got, err := svc.latestCompletionRecord(context.Background(), "cr-1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Event.ID != "completion-z" {
		t.Fatalf("latest completion = %#v, want completion-z", got)
	}
}

// TestDeliveryDecisionAllowsReporterToRejectOwnDelivery pins the identity bar's
// scope: it is approve-only, so the completion reporter MAY reject its own
// delivery (a reject keeps the item blocked — no self-serving risk).
func TestDeliveryDecisionAllowsReporterToRejectOwnDelivery(t *testing.T) {
	t.Parallel()
	workBoard := &fakeDeliveryDecisionWorkBoard{runs: reviewedDeliveryRun(workboard.NextActionStateNeedsHumanReview)}
	feedback := &fakeDeliveryDecisionFeedbackStore{
		rows: []integrations.GovernanceFeedbackEvent{{
			ID:              "completion-1",
			ChangeRequestID: "cr-1",
			EventType:       integrations.FeedbackEventCodingAgentCompleted,
			PayloadJSON:     `{"agent":{"name":"codex"},"summary":"done"}`,
			CreatedAt:       time.Unix(100, 0).UTC(),
		}},
	}
	svc := &Service{WorkBoard: workBoard, FeedbackStore: feedback}

	if _, err := svc.DecideDelivery(context.Background(), DeliveryDecisionInput{
		ChangeRequestID: "cr-1",
		Decision:        "reject",
		Actor:           "codex",
		Note:            "found a regression",
	}); err != nil {
		t.Fatalf("reporter should be allowed to reject its own delivery: %v", err)
	}
	if workBoard.in.ChangeRequestID != "cr-1" {
		t.Fatalf("RecordDeliveryDecision not called for self-reject: %+v", workBoard.in)
	}
}
