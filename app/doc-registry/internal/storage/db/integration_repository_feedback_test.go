package db

import (
	"context"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/integrations"
	"gorm.io/gorm"
)

func TestIntegrationRepository_DeliveryLinksAndGovernanceFeedback(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := integrations.WithWorkspace(context.Background(), "ws-test")
		repo := NewIntegrationRepository(gdb)

		integration, err := repo.CreateIntegration(ctx, integrations.Integration{
			ID:       "int_gitlab_delivery",
			Provider: integrations.ProviderGitLab,
			Name:     "Acme GitLab",
			Status:   integrations.StatusConnected,
		})
		if err != nil {
			t.Fatalf("CreateIntegration: %v", err)
		}
		resource, err := repo.CreateResource(ctx, integrations.Resource{
			IntegrationID: integration.ID,
			ResourceType:  integrations.ResourceTypeProject,
			ExternalID:    "321",
			ExternalKey:   "acme/projects/specgate-fe",
		})
		if err != nil {
			t.Fatalf("CreateResource: %v", err)
		}
		_, event, err := repo.RecordWebhookEvent(ctx, integrations.WebhookEvent{
			IntegrationID: integration.ID,
			ResourceID:    resource.ID,
			Provider:      integrations.ProviderGitLab,
			EventType:     integrations.WebhookEventMergeRequest,
			PayloadJSON:   `{"object_kind":"merge_request"}`,
			Status:        integrations.WebhookStatusPending,
		})
		if err != nil {
			t.Fatalf("RecordWebhookEvent: %v", err)
		}

		link, err := repo.UpsertDeliveryLink(ctx, integrations.DeliveryLink{
			IntegrationID:   integration.ID,
			ResourceID:      resource.ID,
			FeatureID:       "feature-loyalty",
			ChangeRequestID: "cr-loyalty-v1",
			ExternalType:    integrations.ExternalTypeMergeRequest,
			ExternalID:      "9001",
			ExternalIID:     "42",
			ExternalKey:     "acme/projects/specgate-fe!42",
			URL:             "https://gitlab.acme.io/acme/projects/specgate-fe/-/merge_requests/42",
			Title:           "CR-LOYALTY-V1 FE",
			State:           integrations.DeliveryStateOpened,
			HeadSHA:         "submitted-head",
			LastEventID:     event.ID,
		})
		if err != nil {
			t.Fatalf("UpsertDeliveryLink(opened): %v", err)
		}
		merged, err := repo.UpsertDeliveryLink(ctx, integrations.DeliveryLink{
			IntegrationID:   integration.ID,
			ResourceID:      resource.ID,
			FeatureID:       "feature-loyalty",
			ChangeRequestID: "cr-loyalty-v1",
			ExternalType:    integrations.ExternalTypeMergeRequest,
			ExternalID:      "9001",
			ExternalIID:     "42",
			ExternalKey:     "acme/projects/specgate-fe!42",
			URL:             link.URL,
			Title:           link.Title,
			State:           integrations.DeliveryStateMerged,
			HeadSHA:         "later-submitted-head",
			MergeCommitSHA:  "abc123",
			LastEventID:     event.ID,
		})
		if err != nil {
			t.Fatalf("UpsertDeliveryLink(merged): %v", err)
		}
		if merged.ID != link.ID || merged.State != integrations.DeliveryStateMerged || merged.HeadSHA != "later-submitted-head" || merged.MergeCommitSHA != "abc123" {
			t.Fatalf("unexpected merged link: %#v opened=%#v", merged, link)
		}
		links, err := repo.ListDeliveryLinksByChangeRequest(ctx, "cr-loyalty-v1")
		if err != nil || len(links) != 1 || links[0].HeadSHA != "later-submitted-head" || links[0].MergeCommitSHA != "abc123" {
			t.Fatalf("list delivery links = %#v (err %v)", links, err)
		}

		feedback, err := repo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
			IntegrationID:   integration.ID,
			ResourceID:      resource.ID,
			WebhookEventID:  event.ID,
			DeliveryLinkID:  link.ID,
			FeatureID:       "feature-loyalty",
			ChangeRequestID: "cr-loyalty-v1",
			EventType:       integrations.FeedbackEventPRMerged,
			PayloadJSON:     `{"mr_iid":42}`,
			Status:          integrations.FeedbackStatusReceived,
		})
		if err != nil {
			t.Fatalf("CreateGovernanceFeedbackEvent: %v", err)
		}
		items, err := repo.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{Status: integrations.FeedbackStatusReceived, Limit: 10})
		if err != nil {
			t.Fatalf("ListGovernanceFeedbackEvents: %v", err)
		}
		if len(items) != 1 || items[0].ID != feedback.ID || items[0].EventType != integrations.FeedbackEventPRMerged {
			t.Fatalf("unexpected feedback items: %#v", items)
		}
	})
}

// A handoff persists one primary tracker link per work item. TrackerLinkByExternal
// resolves either by immutable ID or human key, and returns nil on no match.
func TestIntegrationRepository_TrackerLinkByExternal(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := integrations.WithWorkspace(context.Background(), "ws-test")
		repo := NewIntegrationRepository(gdb)
		integration, err := repo.CreateIntegration(ctx, integrations.Integration{
			ID:       "int_linear_tracker",
			Provider: integrations.ProviderLinear,
			Name:     "Linear",
			Status:   integrations.StatusConnected,
		})
		if err != nil {
			t.Fatalf("CreateIntegration: %v", err)
		}
		resource, err := repo.CreateResource(ctx, integrations.Resource{IntegrationID: integration.ID, ResourceType: integrations.ResourceTypeTeam, ExternalID: "team-1", ExternalKey: "ENG"})
		if err != nil {
			t.Fatalf("CreateResource: %v", err)
		}
		if _, err := repo.UpsertTrackerLink(ctx, integrations.TrackerLink{IntegrationID: integration.ID, ResourceID: resource.ID, FeatureID: "feat-1", ChangeRequestID: "cr-1", ExternalID: "lin-uuid-fe", ExternalKey: "ZOP-6"}); err != nil {
			t.Fatalf("UpsertTrackerLink: %v", err)
		}

		byID, err := repo.TrackerLinkByExternal(ctx, integration.ID, "lin-uuid-fe", "")
		if err != nil || byID == nil || byID.ExternalKey != "ZOP-6" || byID.ChangeRequestID != "cr-1" || byID.ResourceID != resource.ID {
			t.Fatalf("by id: %#v (err %v)", byID, err)
		}
		byKey, err := repo.TrackerLinkByExternal(ctx, integration.ID, "", "ZOP-6")
		if err != nil || byKey == nil || byKey.ExternalID != "lin-uuid-fe" {
			t.Fatalf("by key: %#v (err %v)", byKey, err)
		}
		// Inbound event carries both; an id match must not be defeated by the OR.
		both, err := repo.TrackerLinkByExternal(ctx, integration.ID, "lin-uuid-fe", "ZOP-6")
		if err != nil || both == nil || both.ExternalKey != "ZOP-6" {
			t.Fatalf("by both: %#v (err %v)", both, err)
		}
		none, err := repo.TrackerLinkByExternal(ctx, integration.ID, "nope", "NOPE-1")
		if err != nil || none != nil {
			t.Fatalf("no match: %#v (err %v)", none, err)
		}
		// The work item's "linked issues" surface lists its single primary link.
		byCR, err := repo.ListTrackerLinksByChangeRequest(ctx, "cr-1")
		if err != nil || len(byCR) != 1 {
			t.Fatalf("list by change request = %d (err %v)", len(byCR), err)
		}
		// Re-emit of the same key updates in place (state transition), not a new row.
		if _, err := repo.UpsertTrackerLink(ctx, integrations.TrackerLink{
			IntegrationID: integration.ID, ResourceID: resource.ID, ChangeRequestID: "cr-1", ExternalKey: "ZOP-6", State: integrations.TrackerStateClosed,
		}); err != nil {
			t.Fatalf("re-upsert: %v", err)
		}
		updated, _ := repo.TrackerLinkByExternal(ctx, integration.ID, "", "ZOP-6")
		if updated == nil || updated.State != integrations.TrackerStateClosed {
			t.Fatalf("re-upsert did not update in place: %#v", updated)
		}
	})
}

// Coding-agent feedback has no originating integration, so it
// stores integration_id=”. Regression for the dropped FK: the insert must
// succeed without any integrations row.
func TestIntegrationRepository_CreateGovernanceFeedbackEvent_AgentOriginatedNoIntegration(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewIntegrationRepository(gdb)
		ctx := integrations.WithWorkspace(context.Background(), "ws-test")
		fb, err := repo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
			ChangeRequestID: "cr-loyalty-redeem",
			ArtifactID:      "art-1",
			EventType:       integrations.FeedbackEventCodingAgentCompleted,
			PayloadJSON:     `{"summary":"done"}`,
			Status:          integrations.FeedbackStatusReceived,
			Reason:          "Implemented redeem with idempotency",
			// IntegrationID deliberately empty — agent feedback has no integration.
		})
		if err != nil {
			t.Fatalf("CreateGovernanceFeedbackEvent (no integration): %v", err)
		}
		if fb.IntegrationID != "" {
			t.Fatalf("integration_id = %q, want empty", fb.IntegrationID)
		}
	})
}

func TestIntegrationRepository_UpdateGovernanceFeedbackEventStatus(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := integrations.WithWorkspace(context.Background(), "ws-test")
		repo := NewIntegrationRepository(gdb)

		integration, err := repo.CreateIntegration(ctx, integrations.Integration{
			ID:       "int_reconcile",
			Provider: integrations.ProviderGitLab,
			Name:     "Reconcile GitLab",
			Status:   integrations.StatusConnected,
		})
		if err != nil {
			t.Fatalf("CreateIntegration: %v", err)
		}
		feedback, err := repo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
			IntegrationID:   integration.ID,
			ChangeRequestID: "cr-loyalty-v1",
			EventType:       integrations.FeedbackEventPRMerged,
			PayloadJSON:     `{"mr_iid":42}`,
			Status:          integrations.FeedbackStatusReceived,
		})
		if err != nil {
			t.Fatalf("CreateGovernanceFeedbackEvent: %v", err)
		}

		updated, err := repo.UpdateGovernanceFeedbackEventStatus(
			ctx, feedback.ID, integrations.FeedbackStatusAccepted, "artifact update approved",
		)
		if err != nil {
			t.Fatalf("UpdateGovernanceFeedbackEventStatus: %v", err)
		}
		if updated.Status != integrations.FeedbackStatusAccepted || updated.Reason != "artifact update approved" {
			t.Fatalf("unexpected updated event: %#v", updated)
		}
		if !updated.UpdatedAt.After(feedback.UpdatedAt) && !updated.UpdatedAt.Equal(feedback.UpdatedAt) {
			t.Fatalf("updated_at not advanced: before=%v after=%v", feedback.UpdatedAt, updated.UpdatedAt)
		}

		pending, err := repo.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{Status: integrations.FeedbackStatusReceived, Limit: 10})
		if err != nil {
			t.Fatalf("ListGovernanceFeedbackEvents(pending): %v", err)
		}
		if len(pending) != 0 {
			t.Fatalf("expected no pending events, got %#v", pending)
		}
		processed, err := repo.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{Status: integrations.FeedbackStatusAccepted, Limit: 10})
		if err != nil {
			t.Fatalf("ListGovernanceFeedbackEvents(processed): %v", err)
		}
		if len(processed) != 1 || processed[0].ID != feedback.ID {
			t.Fatalf("expected 1 processed event, got %#v", processed)
		}

		if _, err := repo.UpdateGovernanceFeedbackEventStatus(ctx, "missing", integrations.FeedbackStatusRejected, ""); err == nil {
			t.Fatalf("expected error updating unknown feedback event")
		}
	})
}

func TestIntegrationRepository_ListGovernanceFeedbackEventsFiltersByChangeRequestAndArtifact(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := integrations.WithWorkspace(context.Background(), "ws-test")
		repo := NewIntegrationRepository(gdb)

		integration, err := repo.CreateIntegration(ctx, integrations.Integration{
			ID:       "int_feedback_filter",
			Provider: integrations.ProviderGitHub,
			Name:     "Feedback Filter",
			Status:   integrations.StatusConnected,
		})
		if err != nil {
			t.Fatalf("CreateIntegration: %v", err)
		}

		first, err := repo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
			IntegrationID:   integration.ID,
			FeatureID:       "feature-1",
			ChangeRequestID: "cr-1",
			ArtifactID:      "art-1",
			EventType:       integrations.FeedbackEventCodingAgentBlockedAmbiguity,
			PayloadJSON:     `{"summary":"clarify refunds"}`,
			Status:          integrations.FeedbackStatusReceived,
			Reason:          "Clarify refunds.",
		})
		if err != nil {
			t.Fatalf("CreateGovernanceFeedbackEvent(first): %v", err)
		}
		second, err := repo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
			IntegrationID:   integration.ID,
			FeatureID:       "feature-1",
			ChangeRequestID: "cr-2",
			ArtifactID:      "art-2",
			EventType:       integrations.FeedbackEventCodingAgentCompleted,
			PayloadJSON:     `{"summary":"done"}`,
			Status:          integrations.FeedbackStatusReceived,
			Reason:          "Completed.",
		})
		if err != nil {
			t.Fatalf("CreateGovernanceFeedbackEvent(second): %v", err)
		}

		byCR, err := repo.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{
			ChangeRequestID: "cr-1",
			Limit:           10,
		})
		if err != nil {
			t.Fatalf("ListGovernanceFeedbackEvents(change_request): %v", err)
		}
		if len(byCR) != 1 || byCR[0].ID != first.ID {
			t.Fatalf("unexpected change request filtered rows: %#v", byCR)
		}

		byArtifact, err := repo.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{
			ArtifactID: "art-2",
			Limit:      10,
		})
		if err != nil {
			t.Fatalf("ListGovernanceFeedbackEvents(artifact): %v", err)
		}
		if len(byArtifact) != 1 || byArtifact[0].ID != second.ID {
			t.Fatalf("unexpected artifact filtered rows: %#v", byArtifact)
		}
		byEventType, err := repo.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{
			EventType: integrations.FeedbackEventCodingAgentCompleted,
			Limit:     10,
		})
		if err != nil {
			t.Fatalf("ListGovernanceFeedbackEvents(event_type): %v", err)
		}
		if len(byEventType) != 1 || byEventType[0].ID != second.ID {
			t.Fatalf("unexpected event-type filtered rows: %#v", byEventType)
		}
	})
}

func TestIntegrationRepository_ListGovernanceFeedbackEventsBreaksTimestampTiesByID(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := integrations.WithWorkspace(context.Background(), "ws-test")
		repo := NewIntegrationRepository(gdb)
		createdAt := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
		for _, id := range []string{"feedback-a", "feedback-z"} {
			if _, err := repo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
				ID:              id,
				ChangeRequestID: "cr-tied",
				EventType:       integrations.FeedbackEventCodingAgentBlockedAmbiguity,
				PayloadJSON:     `{"summary":"tied"}`,
				CreatedAt:       createdAt,
			}); err != nil {
				t.Fatalf("CreateGovernanceFeedbackEvent(%s): %v", id, err)
			}
		}

		rows, err := repo.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{
			ChangeRequestID: "cr-tied",
			Limit:           1,
		})
		if err != nil {
			t.Fatalf("ListGovernanceFeedbackEvents: %v", err)
		}
		if len(rows) != 1 || rows[0].ID != "feedback-z" {
			t.Fatalf("rows = %#v, want feedback-z first", rows)
		}
	})
}

func TestIntegrationRepository_DeleteIntegrationCascadesEverything(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := integrations.WithWorkspace(context.Background(), "ws-test")
		repo := NewIntegrationRepository(gdb)

		integration, err := repo.CreateIntegration(ctx, integrations.Integration{
			ID:       "int_delete",
			Provider: integrations.ProviderGitLab,
			Name:     "To Delete",
			Status:   integrations.StatusConnected,
		})
		if err != nil {
			t.Fatalf("CreateIntegration: %v", err)
		}
		resource, err := repo.CreateResource(ctx, integrations.Resource{
			IntegrationID: integration.ID,
			ResourceType:  integrations.ResourceTypeProject,
			ExternalID:    "555",
			ExternalKey:   "ns/cascade-test",
		})
		if err != nil {
			t.Fatalf("CreateResource: %v", err)
		}
		_, _, err = repo.RecordWebhookEvent(ctx, integrations.WebhookEvent{
			IntegrationID:   integration.ID,
			ResourceID:      resource.ID,
			Provider:        integrations.ProviderGitLab,
			EventType:       integrations.WebhookEventMergeRequest,
			ExternalEventID: "evt-cascade",
			Status:          integrations.WebhookStatusProcessed,
		})
		if err != nil {
			t.Fatalf("RecordWebhookEvent: %v", err)
		}

		if err := repo.DeleteIntegration(ctx, integration.ID); err != nil {
			t.Fatalf("DeleteIntegration: %v", err)
		}

		// Top-level row gone.
		if _, err := repo.GetIntegration(ctx, integration.ID); err == nil {
			t.Fatal("expected GetIntegration to return ErrNotFound after delete")
		}
		// Cascade should have nuked the resource and the webhook event row.
		var resourceCount int64
		if err := gdb.Model(&integrations.Resource{}).Where("integration_id = ?", integration.ID).Count(&resourceCount).Error; err != nil {
			t.Fatal(err)
		}
		if resourceCount != 0 {
			t.Fatalf("expected resources cascaded; remaining=%d", resourceCount)
		}
		var eventCount int64
		if err := gdb.Model(&integrations.WebhookEvent{}).Where("integration_id = ?", integration.ID).Count(&eventCount).Error; err != nil {
			t.Fatal(err)
		}
		if eventCount != 0 {
			t.Fatalf("expected webhook events cascaded; remaining=%d", eventCount)
		}

		// Idempotent: second delete returns ErrNotFound.
		if err := repo.DeleteIntegration(ctx, integration.ID); err != integrations.ErrNotFound {
			t.Fatalf("second delete should ErrNotFound, got %v", err)
		}
	})
}
