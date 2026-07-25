package local

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// The window bounds the derived signals. Workspace totals stay all-time, so a
// long-lived workspace never looks empty just because the window is short.
func TestStatsWindowExcludesOlderReviews(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	selection, err := store.Initialize(context.Background(), InitInput{
		WorkspaceName: "Alpha", DisplayName: "Human", Username: "human",
	})
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := selection.Workspace.ID
	work, err := store.CreateQuickWork(context.Background(), workspaceID, QuickWorkInput{
		Title: "Old delivery", Description: "d", AcceptanceCriteria: []string{"Behavior holds"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SubmitDelivery(context.Background(), workspaceID, work.Key, map[string]any{
		"context_digest": work.ContextDigest,
		"agent":          map[string]any{"name": "builder"},
		"criteria": []any{map[string]any{
			"criterion_id": "local-1", "claim": "satisfied",
			"evidence": map[string]any{"heading": "evidence"},
		}},
		"checks": []any{map[string]any{"name": "unit", "status": "pass"}},
	}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().AddDate(0, 0, -90).Format(time.RFC3339Nano)
	if _, err := store.db.Exec(`UPDATE delivery_reviews SET created_at = ?`, old); err != nil {
		t.Fatal(err)
	}

	narrow, err := store.Stats(context.Background(), workspaceID, 30)
	if err != nil {
		t.Fatal(err)
	}
	if narrow.ReviewedItems != 0 {
		t.Fatalf("reviewed items = %d, want 0 for a review outside the window", narrow.ReviewedItems)
	}
	if narrow.DeliveryReviews != 1 || narrow.WorkItems != 1 {
		t.Fatalf("all-time totals must ignore the window: %#v", narrow)
	}

	wide, err := store.Stats(context.Background(), workspaceID, 365)
	if err != nil {
		t.Fatal(err)
	}
	if wide.ReviewedItems != 1 || wide.FirstPass != 1 {
		t.Fatalf("365-day window = %#v, want the older review counted", wide)
	}
}

// An item whose failure predates the window and whose pass lands inside it took
// two attempts. Reporting it as a clean first pass would invert the honesty the
// readout exists for.
func TestStatsFirstPassUsesTheItemsOwnFirstReviewNotTheWindows(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	selection, err := store.Initialize(context.Background(), InitInput{
		WorkspaceName: "Alpha", DisplayName: "Human", Username: "human",
	})
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := selection.Workspace.ID
	work, err := store.CreateQuickWork(context.Background(), workspaceID, QuickWorkInput{
		Title: "Caught long ago", Description: "d", AcceptanceCriteria: []string{"Behavior holds"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, verdict := range []string{"failed", "passed"} {
		if _, err := store.db.Exec(
			`INSERT INTO delivery_reviews(id, workspace_id, work_id, report_id, verdict, summary, created_at)
			 VALUES (?, ?, ?, '', ?, '', ?)`,
			"review-"+verdict, workspaceID, work.ID, verdict, staleOrFreshReviewTime(verdict),
		); err != nil {
			t.Fatal(err)
		}
	}

	stats, err := store.Stats(context.Background(), workspaceID, 30)
	if err != nil {
		t.Fatal(err)
	}
	if stats.ReviewedItems != 1 {
		t.Fatalf("reviewed items = %d, want 1", stats.ReviewedItems)
	}
	if stats.FirstPass != 0 {
		t.Fatalf("first pass = %d, want 0 — the item's first review ever failed", stats.FirstPass)
	}
	if stats.Rework != 1 || stats.ItemsWithRework != 1 {
		t.Fatalf("rework = %d across %d items, want the in-window resubmit counted", stats.Rework, stats.ItemsWithRework)
	}
}

func staleOrFreshReviewTime(verdict string) string {
	if verdict == "failed" {
		return time.Now().UTC().AddDate(0, 0, -90).Format(time.RFC3339Nano)
	}
	return time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
}
