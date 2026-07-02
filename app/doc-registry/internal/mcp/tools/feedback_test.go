package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/integrations"
)

// feedbackTestStore implements tools.FeedbackStore (= governanceops.FeedbackStore).
type feedbackTestStore struct {
	created []integrations.GovernanceFeedbackEvent
}

func (f *feedbackTestStore) CreateGovernanceFeedbackEvent(_ context.Context, in integrations.GovernanceFeedbackEvent) (*integrations.GovernanceFeedbackEvent, error) {
	in.ID = "feedback-1"
	f.created = append(f.created, in)
	return &in, nil
}

func (f *feedbackTestStore) ListGovernanceFeedbackEvents(_ context.Context, filter integrations.GovernanceFeedbackFilter) ([]integrations.GovernanceFeedbackEvent, error) {
	out := make([]integrations.GovernanceFeedbackEvent, 0, len(f.created))
	for _, row := range f.created {
		if filter.ChangeRequestID != "" && row.ChangeRequestID != filter.ChangeRequestID {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

// TestReportImplementationFeedbackHandler_JSONOutput verifies the thin adapter
// marshals the service result into valid JSON with the expected shape.
func TestReportImplementationFeedbackHandler_JSONOutput(t *testing.T) {
	t.Parallel()
	store := &feedbackTestStore{}
	svc := &governanceops.Service{FeedbackStore: store}
	handler := NewReportImplementationFeedbackHandler(svc)

	out, err := handler(context.Background(), ReportImplementationFeedbackInput{
		ChangeRequestID: "cr-123",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Severity:        "info",
		Summary:         "Done.",
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	for _, want := range []string{`"feedback_event_id"`, `"status"`, `"draft_proposal"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %s: %s", want, out)
		}
	}
}

// TestReportImplementationFeedbackHandler_PropagatesError verifies errors from
// the service are returned without modification.
func TestReportImplementationFeedbackHandler_PropagatesError(t *testing.T) {
	t.Parallel()
	// nil FeedbackStore → service returns error
	svc := &governanceops.Service{}
	handler := NewReportImplementationFeedbackHandler(svc)

	_, err := handler(context.Background(), ReportImplementationFeedbackInput{
		ChangeRequestID: "cr-1",
		EventType:       integrations.FeedbackEventCodingAgentCompleted,
		Severity:        "info",
		Summary:         "Done.",
	})
	if err == nil {
		t.Fatal("expected error when FeedbackStore is nil")
	}
}
