package db

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
)

func seedStatsFixtures(t *testing.T, gdb *gorm.DB, now time.Time) {
	t.Helper()
	crs := []workboard.ChangeRequest{
		{ID: "cr-ws1", Key: "CR-WS1", WorkspaceID: "ws-1", WorkType: workboard.WorkTypeBugFix, Title: "In workspace 1", CreatedAt: now.Add(-72 * time.Hour), UpdatedAt: now},
		{ID: "cr-ws2", Key: "CR-WS2", WorkspaceID: "ws-2", WorkType: workboard.WorkTypeBugFix, Title: "In workspace 2", CreatedAt: now.Add(-72 * time.Hour), UpdatedAt: now},
	}
	for i := range crs {
		if err := gdb.Create(&crs[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	runs := []workboard.GateRun{
		{ID: "run-recent-ws1", WorkspaceID: "ws-1", SubjectKind: workboard.GateRunSubjectChangeRequest, SubjectID: "cr-ws1", Gate: "delivery_review", State: workboard.NextActionStatePass, Hint: "clean", Executor: workboard.GateRunExecutorPlatform, CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "run-recent-ws2", WorkspaceID: "ws-2", SubjectKind: workboard.GateRunSubjectChangeRequest, SubjectID: "cr-ws2", Gate: "scope_clear", State: workboard.NextActionStateFail, Hint: "unclear", Executor: workboard.GateRunExecutorPlatform, CreatedAt: now.Add(-3 * time.Hour)},
		{ID: "run-old-ws1", WorkspaceID: "ws-1", SubjectKind: workboard.GateRunSubjectChangeRequest, SubjectID: "cr-ws1", Gate: "delivery_review", State: workboard.NextActionStateFail, Hint: "stale", Executor: workboard.GateRunExecutorPlatform, CreatedAt: now.Add(-40 * 24 * time.Hour)},
		// Artifact-scoped readiness row inside the window whose subject id
		// collides with a CR id: must never surface in change-request stats
		// (subject_kind filter).
		{ID: "run-artifact-noise", WorkspaceID: "ws-1", SubjectKind: workboard.GateRunSubjectArtifact, SubjectID: "cr-ws1", Gate: "spec_completeness", State: workboard.NextActionStatePass, Hint: "readiness", Executor: workboard.GateRunExecutorPlatform, CreatedAt: now.Add(-time.Hour)},
		// A malformed row must never cross into ws-1 stats just because its
		// subject_id points to a ws-1 change request.
		{ID: "run-cross-workspace", WorkspaceID: "ws-2", SubjectKind: workboard.GateRunSubjectChangeRequest, SubjectID: "cr-ws1", Gate: "delivery_review", State: workboard.NextActionStatePass, Hint: "wrong workspace", Executor: workboard.GateRunExecutorPlatform, CreatedAt: now.Add(-time.Hour)},
	}
	for i := range runs {
		if err := gdb.Create(&runs[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	events := []integrations.GovernanceFeedbackEvent{
		{ID: "evt-ambiguity-ws1", WorkspaceID: "ws-1", ChangeRequestID: "cr-ws1", EventType: integrations.FeedbackEventCodingAgentBlockedAmbiguity, Reason: "which flag?", PayloadJSON: "{}", Status: integrations.FeedbackStatusReceived, CreatedAt: now.Add(-4 * time.Hour), UpdatedAt: now},
		{ID: "evt-ambiguity-ws2", WorkspaceID: "ws-2", ChangeRequestID: "cr-ws2", EventType: integrations.FeedbackEventCodingAgentBlockedAmbiguity, Reason: "which env?", PayloadJSON: "{}", Status: integrations.FeedbackStatusReceived, CreatedAt: now.Add(-5 * time.Hour), UpdatedAt: now},
		{ID: "evt-other-type", WorkspaceID: "ws-1", ChangeRequestID: "cr-ws1", EventType: integrations.FeedbackEventCodingAgentCompleted, Reason: "done", PayloadJSON: "{}", Status: integrations.FeedbackStatusReceived, CreatedAt: now.Add(-4 * time.Hour), UpdatedAt: now},
		{ID: "evt-old", WorkspaceID: "ws-1", ChangeRequestID: "cr-ws1", EventType: integrations.FeedbackEventCodingAgentBlockedAmbiguity, Reason: "old", PayloadJSON: "{}", Status: integrations.FeedbackStatusReceived, CreatedAt: now.Add(-40 * 24 * time.Hour), UpdatedAt: now},
		{ID: "evt-cross-workspace", WorkspaceID: "ws-2", ChangeRequestID: "cr-ws1", EventType: integrations.FeedbackEventCodingAgentBlockedAmbiguity, Reason: "wrong workspace", PayloadJSON: "{}", Status: integrations.FeedbackStatusReceived, CreatedAt: now.Add(-time.Hour), UpdatedAt: now},
	}
	for i := range events {
		if err := gdb.Create(&events[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestWorkBoardRepository_ListGateRunsForStats(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		seedStatsFixtures(t, gdb, now)
		since := now.Add(-30 * 24 * time.Hour)

		// No workspace filter: both recent runs, old one excluded.
		rows, err := repo.ListGateRunsForStats(ctx, "", since)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Fatalf("rows = %d, want 2 — %#v", len(rows), rows)
		}
		byID := map[string]workboard.StatsGateRun{}
		for _, row := range rows {
			byID[row.RunID] = row
		}
		got, ok := byID["run-recent-ws1"]
		if !ok {
			t.Fatalf("run-recent-ws1 missing: %#v", rows)
		}
		if got.ChangeRequestKey != "CR-WS1" {
			t.Errorf("change_request_key = %q, want CR-WS1", got.ChangeRequestKey)
		}
		if got.Gate != "delivery_review" || got.State != workboard.NextActionStatePass || got.Hint != "clean" {
			t.Errorf("run fields = %#v", got)
		}
		if !got.CRCreatedAt.Equal(now.Add(-72 * time.Hour)) {
			t.Errorf("cr_created_at = %v, want %v", got.CRCreatedAt, now.Add(-72*time.Hour))
		}

		// Workspace filter joins change_requests.
		rows, err = repo.ListGateRunsForStats(ctx, "ws-2", since)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].RunID != "run-recent-ws2" {
			t.Fatalf("ws-2 rows = %#v, want only run-recent-ws2", rows)
		}
	})
}

func TestWorkBoardRepository_ListAmbiguityFeedbackForStats(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		seedStatsFixtures(t, gdb, now)
		since := now.Add(-30 * 24 * time.Hour)

		// No workspace filter: only recent blocked-ambiguity events; other
		// event types and out-of-window events excluded.
		rows, err := repo.ListAmbiguityFeedbackForStats(ctx, "", since)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Fatalf("rows = %d, want 2 — %#v", len(rows), rows)
		}
		byID := map[string]workboard.StatsFeedbackEvent{}
		for _, row := range rows {
			byID[row.EventID] = row
		}
		got, ok := byID["evt-ambiguity-ws1"]
		if !ok {
			t.Fatalf("evt-ambiguity-ws1 missing: %#v", rows)
		}
		if got.ChangeRequestKey != "CR-WS1" || got.Detail != "which flag?" {
			t.Errorf("event fields = %#v", got)
		}

		// Workspace filter joins change_requests.
		rows, err = repo.ListAmbiguityFeedbackForStats(ctx, "ws-1", since)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].EventID != "evt-ambiguity-ws1" {
			t.Fatalf("ws-1 rows = %#v, want only evt-ambiguity-ws1", rows)
		}
	})
}

func TestWorkBoardRepository_ListDeliveryRunsForStatsIsUnwindowed(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		seedStatsFixtures(t, gdb, now)

		rows, err := repo.ListDeliveryRunsForStats(ctx, "ws-1")
		if err != nil {
			t.Fatal(err)
		}
		// Both delivery runs for ws-1 — including the 40-day-old one — and
		// nothing else (no non-delivery gates, no artifact-subject noise).
		if len(rows) != 2 {
			t.Fatalf("rows = %d, want 2 — %#v", len(rows), rows)
		}
		for _, row := range rows {
			if row.Gate != "delivery_review" {
				t.Errorf("unexpected gate %q", row.Gate)
			}
		}
		if rows[0].RunID != "run-old-ws1" || rows[1].RunID != "run-recent-ws1" {
			t.Errorf("expected chronological order old->recent, got %#v", rows)
		}
	})
}
