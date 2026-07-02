package api

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/integrations"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
)

func openFeedbackTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return newTestGormDB(t)
}

func TestUpdateGovernanceFeedbackEventStatus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cases := []struct {
		name       string
		input      string // canonical wire status
		wantStored string // persisted stored value
		wantWire   string // canonical status in the response
	}{
		{"resolve", "accepted", integrations.FeedbackStatusProcessed, "accepted"},
		{"dismiss", "rejected", integrations.FeedbackStatusIgnored, "rejected"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			intRepo := storagedb.NewIntegrationRepository(openFeedbackTestDB(t))
			h := &Handlers{Integrations: integrations.NewService(intRepo)}
			ev, err := intRepo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
				EventType: integrations.FeedbackEventPRMerged,
				Status:    integrations.FeedbackStatusPending,
			})
			if err != nil {
				t.Fatal(err)
			}
			in := &UpdateGovernanceFeedbackEventStatusInput{ID: ev.ID}
			in.Body.Status = tc.input
			in.Body.Reason = "triaged in inbox"
			out, err := h.UpdateGovernanceFeedbackEventStatus(ctx, in)
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			if out.Body.Status != tc.wantWire {
				t.Errorf("response status = %q, want %q", out.Body.Status, tc.wantWire)
			}
			// No get-by-id on the repo — reload via the list (returns STORED status).
			list, err := intRepo.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{Limit: 50})
			if err != nil {
				t.Fatal(err)
			}
			var stored string
			for i := range list {
				if list[i].ID == ev.ID {
					stored = list[i].Status
				}
			}
			if stored != tc.wantStored {
				t.Errorf("stored status = %q, want %q", stored, tc.wantStored)
			}
		})
	}

	t.Run("invalid status is rejected", func(t *testing.T) {
		intRepo := storagedb.NewIntegrationRepository(openFeedbackTestDB(t))
		h := &Handlers{Integrations: integrations.NewService(intRepo)}
		ev, _ := intRepo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
			EventType: integrations.FeedbackEventPRMerged, Status: integrations.FeedbackStatusPending,
		})
		in := &UpdateGovernanceFeedbackEventStatusInput{ID: ev.ID}
		in.Body.Status = "received" // not a triage transition
		if _, err := h.UpdateGovernanceFeedbackEventStatus(ctx, in); err == nil {
			t.Fatal("expected error for non-triage status")
		}
	})
}
