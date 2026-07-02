package governanceops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/specgate/doc-registry/internal/integrations"
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
		return nil, fmt.Errorf("change_request_id is required")
	}
	if !allowedCodingAgentFeedbackType(in.EventType) {
		return nil, fmt.Errorf("unsupported event_type %q", in.EventType)
	}
	if in.Summary == "" {
		return nil, fmt.Errorf("summary is required")
	}

	// Evidence provenance is server-stamped, never trusted from agent input (Slice B).
	for i := range in.Evidence {
		in.Evidence[i].Source = ""
	}
	for i := range in.Checks {
		in.Checks[i].Source = ""
	}
	for i := range in.Criteria {
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
			Status:          integrations.CanonicalFeedbackStatus(existing.Status),
			DraftProposal:   in.Severity == "blocking",
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
		Status:          integrations.CanonicalFeedbackStatus(row.Status),
		DraftProposal:   in.Severity == "blocking",
	}, nil
}

// Clarifications returns human-answered clarifications for coding-agent
// blocked-ambiguity events on the given work item.
func (s *Service) Clarifications(ctx context.Context, in ClarificationsInput) (*ClarificationsResult, error) {
	if s.FeedbackStore == nil {
		return nil, fmt.Errorf("feedback store is not configured")
	}
	changeRequestID := strings.TrimSpace(in.ChangeRequestID)
	if changeRequestID == "" {
		return nil, fmt.Errorf("change_request_id is required")
	}
	since, err := parseOptionalSince(in.Since)
	if err != nil {
		return nil, err
	}

	rows, err := s.FeedbackStore.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{
		ChangeRequestID: changeRequestID,
		Limit:           200,
	})
	if err != nil {
		return nil, err
	}

	items := make([]ClarificationItem, 0)
	for _, row := range rows {
		if row.EventType != integrations.FeedbackEventCodingAgentBlockedAmbiguity {
			continue
		}
		status := integrations.CanonicalFeedbackStatus(row.Status)
		if status != "accepted" && status != "rejected" {
			continue
		}
		answeredAt := row.UpdatedAt
		if answeredAt.IsZero() {
			answeredAt = row.CreatedAt
		}
		if !since.IsZero() && !answeredAt.After(since) {
			continue
		}
		cp := decodeClarificationPayload(row.PayloadJSON)
		question := strings.TrimSpace(cp.Summary)
		if question == "" {
			question = strings.TrimSpace(row.Reason)
		}
		answer := strings.TrimSpace(row.Reason)
		if answer == "" {
			answer = strings.TrimSpace(cp.SuggestedCorrection)
		}
		items = append(items, ClarificationItem{
			FeedbackEventID: row.ID,
			QuestionRef:     row.ID,
			QuestionMD:      question,
			AnswerMD:        answer,
			Status:          status,
			AnsweredAt:      formatRFC3339(answeredAt),
		})
	}
	return &ClarificationsResult{
		ChangeRequestID: changeRequestID,
		Found:           len(items) > 0,
		Clarifications:  items,
	}, nil
}

func allowedCodingAgentFeedbackType(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case integrations.FeedbackEventCodingAgentBlockedAmbiguity,
		integrations.FeedbackEventCodingAgentCompleted,
		integrations.FeedbackEventCodingAgentDocsUpdated:
		return true
	default:
		return false
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

type clarificationFeedbackPayload struct {
	Summary             string `json:"summary,omitempty"`
	SuggestedCorrection string `json:"suggested_correction,omitempty"`
}

func decodeClarificationPayload(raw string) clarificationFeedbackPayload {
	var payload clarificationFeedbackPayload
	_ = json.Unmarshal([]byte(strings.TrimSpace(raw)), &payload)
	return payload
}

func parseOptionalSince(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("since must be RFC3339: %w", err)
	}
	return t, nil
}
