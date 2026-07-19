package governanceops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
)

type deliveryDecisionRecorder interface {
	RecordDeliveryDecision(context.Context, workboard.DeliveryDecisionInput) (*workboard.GateRun, error)
}

func (s *Service) DecideDelivery(ctx context.Context, in DeliveryDecisionInput) (*DeliveryDecisionResult, error) {
	recorder, ok := s.WorkBoard.(deliveryDecisionRecorder)
	if !ok {
		return nil, fmt.Errorf("%w: delivery decision writer not configured", ErrUnavailable)
	}
	changeRequestID := strings.TrimSpace(in.ChangeRequestID)
	actor := strings.TrimSpace(in.Actor)
	note := strings.TrimSpace(in.Note)
	decision := strings.TrimSpace(strings.ToLower(in.Decision))
	if changeRequestID == "" {
		return nil, fmt.Errorf("%w: change_request_id is required", ErrValidation)
	}
	if actor == "" {
		return nil, fmt.Errorf("%w: actor is required", ErrValidation)
	}
	if decision != string(workboard.DeliveryDecisionApprove) && decision != string(workboard.DeliveryDecisionReject) {
		return nil, fmt.Errorf("%w: decision must be approve or reject", ErrValidation)
	}
	if trustedWorkspace(ctx) != "" && s.WorkBoard != nil {
		cr, err := s.WorkBoard.GetChangeRequest(ctx, changeRequestID)
		if err != nil {
			return nil, err
		}
		if err := requireChangeRequestWorkspace(ctx, cr); err != nil {
			return nil, ErrNotFound
		}
	}
	currentReview, err := authoritativeDeliveryReviewRun(ctx, s.WorkBoard, changeRequestID)
	if err != nil {
		return nil, err
	}
	if currentReview == nil {
		return nil, fmt.Errorf(
			"%w: run delivery review before recording a human decision",
			ErrValidation,
		)
	}
	if currentReview.Executor == workboard.GateRunExecutorHuman {
		return nil, fmt.Errorf(
			"%w: a human decision is already recorded for this completion; submit corrected evidence before another decision",
			ErrValidation,
		)
	}
	completion, err := s.latestCompletionRecord(ctx, changeRequestID)
	if err != nil {
		return nil, err
	}
	var reviewBinding struct {
		CompletionFeedbackEventID string `json:"completion_feedback_event_id"`
	}
	_ = json.Unmarshal([]byte(currentReview.EvidenceJSON), &reviewBinding)
	reviewedCompletionID := strings.TrimSpace(reviewBinding.CompletionFeedbackEventID)
	if completion == nil || reviewedCompletionID == "" ||
		reviewedCompletionID != completion.Event.ID {
		return nil, fmt.Errorf(
			"%w: the latest completion has not been reviewed; run delivery review before recording a human decision",
			ErrValidation,
		)
	}
	// Identity bar (approve only — the reporter MAY reject its own delivery). The
	// match is case-insensitive and best-effort: actor is the CLI human identity
	// while reporter is the completion event's agent.Name, so an exact-name match
	// mainly guards the case where the same identity string filed and approved.
	if decision == string(workboard.DeliveryDecisionApprove) {
		reporter := ""
		if completion != nil {
			reporter = strings.TrimSpace(completion.Payload.Agent.Name)
		}
		if reporter != "" && strings.EqualFold(reporter, actor) {
			return nil, fmt.Errorf("%w: completion reporter %q cannot approve its own delivery", ErrValidation, actor)
		}
	}
	run, err := recorder.RecordDeliveryDecision(ctx, workboard.DeliveryDecisionInput{
		ChangeRequestID:           changeRequestID,
		ReviewedGateRunID:         currentReview.ID,
		CompletionFeedbackEventID: reviewedCompletionID,
		Decision:                  workboard.DeliveryDecision(decision),
		Actor:                     actor,
		Note:                      note,
	})
	if err != nil {
		return nil, err
	}
	return &DeliveryDecisionResult{
		ChangeRequestID: changeRequestID,
		GateRunID:       run.ID,
		Verdict:         string(run.State),
		Hint:            run.Hint,
		Executor:        run.Executor,
		Actor:           actor,
		Note:            note,
		Summary:         strings.TrimSpace(run.Hint),
		ReviewedAt:      formatRFC3339(run.CreatedAt),
	}, nil
}

type completionRecord struct {
	Event   integrations.GovernanceFeedbackEvent
	Payload ReportFeedbackInput
}

func (s *Service) latestCompletionRecord(
	ctx context.Context,
	changeRequestID string,
) (*completionRecord, error) {
	if s.FeedbackStore == nil {
		return nil, nil
	}
	rows, err := s.FeedbackStore.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{
		ChangeRequestID: changeRequestID,
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Limit:           200,
	})
	if err != nil {
		return nil, err
	}
	var latest *integrations.GovernanceFeedbackEvent
	for i := range rows {
		if rows[i].EventType != integrations.FeedbackEventCodingAgentCompleted {
			continue
		}
		if latest == nil || governanceFeedbackEventNewer(rows[i], *latest) {
			cp := rows[i]
			latest = &cp
		}
	}
	if latest == nil {
		return nil, nil
	}
	var payload ReportFeedbackInput
	_ = json.Unmarshal([]byte(latest.PayloadJSON), &payload)
	return &completionRecord{Event: *latest, Payload: payload}, nil
}
