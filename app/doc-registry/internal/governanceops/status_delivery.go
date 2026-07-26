package governanceops

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
)

// humanActionGates are gates whose pending state requires a human to act in the
// web UI — the agent cannot perform these actions.
func (s *Service) DeliveryStatus(ctx context.Context, in DeliveryStatusInput) (DeliveryStatusResult, error) {
	if s.WorkBoard == nil {
		return DeliveryStatusResult{}, fmt.Errorf("%w: workboard not configured", ErrUnavailable)
	}
	id := strings.TrimSpace(in.ChangeRequestID)
	if id == "" {
		return DeliveryStatusResult{}, fmt.Errorf("change_request_id is required")
	}
	if trustedWorkspace(ctx) != "" {
		cr, err := s.WorkBoard.GetChangeRequest(ctx, id)
		if err != nil {
			return DeliveryStatusResult{}, err
		}
		if err := requireChangeRequestWorkspace(ctx, cr); err != nil {
			return DeliveryStatusResult{}, err
		}
	}

	latest, err := authoritativeDeliveryReviewRun(ctx, s.WorkBoard, id)
	if err != nil {
		return DeliveryStatusResult{}, err
	}
	completion, err := s.latestCompletionRecord(ctx, id)
	if err != nil {
		return DeliveryStatusResult{}, err
	}
	if latest == nil && completion == nil {
		return DeliveryStatusResult{ChangeRequestID: id, Found: false}, nil
	}
	wrapper, detail := decodeDeliveryReview("")
	if latest != nil {
		wrapper, detail = decodeDeliveryReview(latest.EvidenceJSON)
	}
	if completion != nil && (latest == nil ||
		strings.TrimSpace(wrapper.CompletionFeedbackEventID) != completion.Event.ID) {
		result := DeliveryStatusResult{
			ChangeRequestID: id,
			Found:           true,
			Verdict:         string(workboard.NextActionStateNeedsHumanReview),
			ReasonCode:      "delivery_review_outdated",
			Hint:            "The latest completion has not been reviewed; rerun delivery review before a human decision.",
		}
		if in.Detail {
			result.GitReceipt = completion.Payload.GitReceipt
			if peer, peerErr := s.peerReviewState(ctx, id); peerErr != nil {
				return DeliveryStatusResult{}, peerErr
			} else {
				result.PeerReview = peer
			}
		}
		return result, nil
	}
	result := DeliveryStatusResult{
		ChangeRequestID:           id,
		GateRunID:                 latest.ID,
		CompletionFeedbackEventID: wrapper.CompletionFeedbackEventID,
		Found:                     true,
		Verdict:                   string(latest.State),
		EvidenceVerdict:           wrapper.evidenceVerdict(*latest),
		ReasonCode:                detail.ReasonCode,
		Hint:                      latest.Hint,
		Confidence:                wrapper.reviewConfidence(*latest),
		JudgeModel:                wrapper.judgeModel(),
		EvalSuite:                 wrapper.evalSuiteVersion(),
		ReviewedAt:                formatRFC3339(latest.CreatedAt),
		Executor:                  latest.Executor,
		Actor:                     wrapper.actor(),
		Note:                      wrapper.Note,
		Summary:                   workboard.DeliveryDecisionSummary(*latest, wrapper.actor(), wrapper.Note),
		OutstandingMD:             deliveryReviewOutstandingMD(*latest, detail),
		AssuranceSources:          deliveryReviewAssuranceSources(detail),
	}
	if in.Detail {
		if completion != nil {
			result.GitReceipt = completion.Payload.GitReceipt
		}
		if peer, err := s.peerReviewState(ctx, id); err != nil {
			return DeliveryStatusResult{}, err
		} else {
			result.PeerReview = peer
		}
		for _, c := range detail.Criteria {
			result.PerCriterion = append(result.PerCriterion, CriterionReview{
				CriterionID:         c.CriterionID,
				Text:                c.Text,
				Verdict:             c.Verdict,
				Why:                 c.Why,
				VerificationBinding: c.VerificationBinding,
				TrustTier:           c.TrustTier,
			})
		}
		for _, c := range detail.Checks {
			result.Checks = append(result.Checks, CheckResult{Name: c.Name, Status: c.Status, Detail: c.Detail})
		}
	}
	return result, nil
}

func (s *Service) peerReviewState(ctx context.Context, changeRequestID string) (PeerReviewState, error) {
	if s.FeedbackStore == nil {
		return PeerReviewState{State: "not_run"}, nil
	}
	completion, err := s.latestCompletionRecord(ctx, changeRequestID)
	if err != nil {
		return PeerReviewState{}, err
	}
	rows, err := s.FeedbackStore.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{
		ChangeRequestID: changeRequestID,
		EventType:       integrations.FeedbackEventCodingAgentPeerReviewed,
		Limit:           200,
	})
	if err != nil {
		return PeerReviewState{}, err
	}
	var peer *integrations.GovernanceFeedbackEvent
	for i := range rows {
		row := &rows[i]
		if row.EventType == integrations.FeedbackEventCodingAgentPeerReviewed &&
			(peer == nil || governanceFeedbackEventNewer(*row, *peer)) {
			peer = row
		}
	}
	if peer == nil {
		return PeerReviewState{State: "not_run"}, nil
	}
	state := PeerReviewState{State: "failed", ReviewedAt: formatRFC3339(peer.CreatedAt)}
	var review ReportFeedbackInput
	if err := json.Unmarshal([]byte(peer.PayloadJSON), &review); err != nil {
		return state, nil
	}
	state.AgentName = strings.TrimSpace(review.Agent.Name)
	if completion == nil || review.PeerReviewOf == nil || review.PeerReviewOf.CompletionFeedbackEventID != completion.Event.ID {
		state.State = "stale"
		return state, nil
	}
	if completion.Payload.GitReceipt == nil ||
		review.PeerReviewOf.GitReceipt == nil ||
		!reflect.DeepEqual(*completion.Payload.GitReceipt, *review.PeerReviewOf.GitReceipt) {
		state.State = "stale"
		return state, nil
	}
	if len(review.Criteria) == 0 {
		return state, nil
	}
	for _, criterion := range review.Criteria {
		if strings.TrimSpace(strings.ToLower(criterion.Claim)) != "satisfied" {
			return state, nil
		}
	}
	state.State = "passed"
	return state, nil
}

// --- helpers ---
