package governanceops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
)

// ReportFeedback records a coding-agent feedback event for the given work item.
// Source fields on evidence, checks, and criteria are stripped (Slice B): an
// agent may not self-claim independently-corroborated provenance.
// Returns an error if FeedbackStore is nil.
func (s *Service) ReportFeedback(ctx context.Context, in ReportFeedbackInput) (*ReportFeedbackResult, error) {
	if s.FeedbackStore == nil {
		return nil, fmt.Errorf("feedback store is not configured")
	}

	in.ChangeRequestID = strings.TrimSpace(in.ChangeRequestID)
	in.ArtifactID = strings.TrimSpace(in.ArtifactID)
	in.EventType = strings.TrimSpace(in.EventType)
	in.Severity = strings.TrimSpace(strings.ToLower(in.Severity))
	in.Summary = strings.TrimSpace(in.Summary)
	in.SuggestedCorrection = strings.TrimSpace(in.SuggestedCorrection)
	in.RunID = strings.TrimSpace(in.RunID)
	in.DedupeKey = strings.TrimSpace(in.DedupeKey)
	in.Agent.Name = strings.TrimSpace(in.Agent.Name)
	in.Agent.Version = strings.TrimSpace(in.Agent.Version)

	if in.ChangeRequestID == "" {
		return nil, fmt.Errorf("%w: change_request_id is required", ErrValidation)
	}
	if trustedWorkspace(ctx) != "" && s.WorkBoard != nil {
		cr, err := s.WorkBoard.GetChangeRequest(ctx, in.ChangeRequestID)
		if err != nil {
			return nil, err
		}
		if err := requireChangeRequestWorkspace(ctx, cr); err != nil {
			return nil, ErrNotFound
		}
		if in.EventType == integrations.FeedbackEventCodingAgentCompleted &&
			(cr.Archived || cr.Phase == workboard.BoardPhaseDelivered) {
			return nil, fmt.Errorf(
				"%w: delivery is already accepted; create a new work item for further changes",
				ErrValidation,
			)
		}
	}
	if trustedWorkspace(ctx) != "" && in.ArtifactID != "" && s.Artifacts != nil {
		art, err := s.Artifacts.Get(ctx, in.ArtifactID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(art.WorkspaceID) != trustedWorkspace(ctx) {
			return nil, ErrNotFound
		}
	}
	if !allowedCodingAgentFeedbackType(in.EventType) {
		return nil, fmt.Errorf("%w: unsupported event_type %q", ErrValidation, in.EventType)
	}
	if in.Summary == "" {
		return nil, fmt.Errorf("%w: summary is required", ErrValidation)
	}
	if in.EventType == integrations.FeedbackEventCodingAgentCompleted && in.Agent.Name == "" {
		return nil, fmt.Errorf("%w: completion agent.name is required", ErrValidation)
	}
	if in.EventType == integrations.FeedbackEventCodingAgentPeerReviewed {
		if err := validatePeerReviewAgent(ctx, s.FeedbackStore, s.WorkBoard, in); err != nil {
			return nil, err
		}
	}

	// Evidence provenance is server-stamped, never trusted from agent input (Slice B).
	for i := range in.Evidence {
		in.Evidence[i].Source = ""
	}
	for i := range in.Checks {
		in.Checks[i].Name = strings.TrimSpace(in.Checks[i].Name)
		in.Checks[i].Command = strings.TrimSpace(in.Checks[i].Command)
		in.Checks[i].Detail = strings.TrimSpace(in.Checks[i].Detail)
		in.Checks[i].Source = ""
		status, err := normalizeFeedbackCheckStatus(in.Checks[i].Status)
		if err != nil {
			return nil, err
		}
		in.Checks[i].Status = status
	}
	for i := range in.Criteria {
		claim := strings.TrimSpace(in.Criteria[i].Claim)
		switch claim {
		case "satisfied", "partial", "not_done":
		default:
			return nil, fmt.Errorf("%w: unsupported criterion claim %q (use satisfied, partial, or not_done)", ErrValidation, in.Criteria[i].Claim)
		}
		in.Criteria[i].Claim = claim
		if in.Criteria[i].Evidence != nil {
			in.Criteria[i].Evidence.Source = ""
		}
	}

	payload, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	key := feedbackDedupeKey(in, payload)
	if existing, err := findExistingFeedback(ctx, s.FeedbackStore, in.ChangeRequestID, in.EventType, key); err != nil {
		return nil, err
	} else if existing != nil {
		return &ReportFeedbackResult{
			FeedbackEventID: existing.ID,
			Status:          existing.Status,
		}, nil
	}

	row, err := s.FeedbackStore.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
		ChangeRequestID: in.ChangeRequestID,
		ArtifactID:      in.ArtifactID,
		EventType:       in.EventType,
		PayloadJSON:     string(payload),
		Status:          integrations.FeedbackStatusReceived,
		Reason:          in.Summary,
	})
	if err != nil {
		return nil, err
	}
	if s.FeedbackNotify != nil {
		s.FeedbackNotify(in.ChangeRequestID, in.EventType)
	}
	return &ReportFeedbackResult{
		FeedbackEventID: row.ID,
		Status:          row.Status,
	}, nil
}

func allowedCodingAgentFeedbackType(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case integrations.FeedbackEventCodingAgentBlockedAmbiguity,
		integrations.FeedbackEventCodingAgentCompleted,
		integrations.FeedbackEventCodingAgentDocsUpdated,
		integrations.FeedbackEventCodingAgentPeerReviewed:
		return true
	default:
		return false
	}
}

func governanceFeedbackEventNewer(candidate, current integrations.GovernanceFeedbackEvent) bool {
	return candidate.CreatedAt.After(current.CreatedAt) ||
		(candidate.CreatedAt.Equal(current.CreatedAt) && candidate.ID > current.ID)
}

func validatePeerReviewAgent(ctx context.Context, store FeedbackStore, board WorkBoardReader, in ReportFeedbackInput) error {
	peer := strings.TrimSpace(in.Agent.Name)
	if peer == "" {
		return fmt.Errorf("%w: peer review agent.name is required", ErrValidation)
	}
	rows, err := store.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{
		ChangeRequestID: in.ChangeRequestID,
		Limit:           200,
	})
	if err != nil {
		return err
	}
	var latest *integrations.GovernanceFeedbackEvent
	for i := range rows {
		if rows[i].EventType != integrations.FeedbackEventCodingAgentCompleted {
			continue
		}
		if latest == nil || governanceFeedbackEventNewer(rows[i], *latest) {
			latest = &rows[i]
		}
	}
	if latest == nil {
		return fmt.Errorf("%w: peer review requires a completion report", ErrValidation)
	}
	var completed ReportFeedbackInput
	if err := json.Unmarshal([]byte(latest.PayloadJSON), &completed); err != nil {
		return fmt.Errorf("%w: latest completion report is unreadable", ErrValidation)
	}
	completer := strings.TrimSpace(completed.Agent.Name)
	if completer == "" {
		return fmt.Errorf("%w: latest completion agent.name is required", ErrValidation)
	}
	if strings.EqualFold(completer, peer) {
		return fmt.Errorf("%w: agent %q cannot peer-review its own completion", ErrValidation, peer)
	}
	if in.PeerReviewOf == nil || strings.TrimSpace(in.PeerReviewOf.CompletionFeedbackEventID) == "" {
		return fmt.Errorf("%w: peer_review_of.completion_feedback_event_id is required", ErrValidation)
	}
	if in.PeerReviewOf.CompletionFeedbackEventID != latest.ID {
		return fmt.Errorf("%w: peer review must bind the latest completion report", ErrValidation)
	}
	if completed.GitReceipt == nil || in.PeerReviewOf.GitReceipt == nil || !reflect.DeepEqual(*completed.GitReceipt, *in.PeerReviewOf.GitReceipt) {
		return fmt.Errorf("%w: peer review git_receipt must match the completion receipt", ErrValidation)
	}
	if board == nil {
		return fmt.Errorf("%w: acceptance criteria are unavailable for peer review", ErrValidation)
	}
	criteria, err := board.ListAcceptanceCriteria(ctx, in.ChangeRequestID)
	if err != nil {
		return err
	}
	allowed := make(map[string]struct{}, len(criteria))
	for _, criterion := range criteria {
		allowed[criterion.ID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(in.Criteria))
	for _, criterion := range in.Criteria {
		id := strings.TrimSpace(criterion.CriterionID)
		if id == "" {
			return fmt.Errorf("%w: peer review criterion_id is required", ErrValidation)
		}
		if _, ok := allowed[id]; !ok {
			return fmt.Errorf("%w: peer review criterion_id %q is not canonical", ErrValidation, id)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("%w: peer review criterion_id %q is duplicated", ErrValidation, id)
		}
		claim := strings.TrimSpace(criterion.Claim)
		if claim != "satisfied" && claim != "partial" && claim != "not_done" {
			return fmt.Errorf("%w: peer review claim %q is invalid", ErrValidation, criterion.Claim)
		}
		if claim == "satisfied" && !hasFeedbackEvidence(criterion.Evidence) {
			return fmt.Errorf("%w: peer review criterion %q claims satisfied but has no evidence", ErrValidation, id)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != len(allowed) {
		return fmt.Errorf("%w: peer review must cover every acceptance criterion exactly once", ErrValidation)
	}
	return nil
}

func hasFeedbackEvidence(evidence *FeedbackEvidence) bool {
	if evidence == nil {
		return false
	}
	return strings.TrimSpace(evidence.Kind) != "" ||
		strings.TrimSpace(evidence.Path) != "" ||
		strings.TrimSpace(evidence.FileKey) != "" ||
		strings.TrimSpace(evidence.Heading) != "" ||
		strings.TrimSpace(evidence.URL) != ""
}

func normalizeFeedbackCheckStatus(status string) (string, error) {
	status = strings.TrimSpace(status)
	switch status {
	case "":
		return "", nil
	case "pass", "fail", "skipped":
		return status, nil
	default:
		return "", fmt.Errorf("%w: unsupported check status %q (use pass, fail, or skipped)", ErrValidation, status)
	}
}

func feedbackDedupeKey(in ReportFeedbackInput, payload []byte) string {
	if in.DedupeKey != "" {
		return "dedupe_key:" + in.DedupeKey
	}
	if in.RunID != "" {
		return "run_id:" + in.RunID
	}
	sum := sha256.Sum256(payload)
	return "payload:" + hex.EncodeToString(sum[:])
}

func findExistingFeedback(ctx context.Context, store FeedbackStore, changeRequestID, eventType, dedupeKey string) (*integrations.GovernanceFeedbackEvent, error) {
	rows, err := store.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{
		ChangeRequestID: changeRequestID,
		EventType:       eventType,
		Limit:           200,
	})
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.EventType != eventType {
			continue
		}
		var payload ReportFeedbackInput
		if err := json.Unmarshal([]byte(row.PayloadJSON), &payload); err != nil {
			continue
		}
		if feedbackDedupeKey(payload, []byte(row.PayloadJSON)) == dedupeKey {
			cp := row
			return &cp, nil
		}
	}
	return nil, nil
}
