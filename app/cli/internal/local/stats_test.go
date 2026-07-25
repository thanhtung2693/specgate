package local_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/local"
)

// Local governance history already records everything the value readout claims:
// a first review that failed is a caught gap, a second one is rework, and the
// gap between creation and the first pass is cycle time.

func statsStore(t *testing.T) (*local.Store, string) {
	t.Helper()
	store, err := local.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	selection, err := store.Initialize(context.Background(), local.InitInput{
		WorkspaceName: "Alpha", DisplayName: "Human", Username: "human",
	})
	if err != nil {
		t.Fatal(err)
	}
	return store, selection.Workspace.ID
}

func statsWork(t *testing.T, store *local.Store, workspaceID, title string) local.WorkItem {
	t.Helper()
	work, err := store.CreateQuickWork(context.Background(), workspaceID, local.QuickWorkInput{
		Title: title, Description: "d", AcceptanceCriteria: []string{"Behavior holds"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return work
}

func statsSubmit(t *testing.T, store *local.Store, workspaceID string, work local.WorkItem, satisfied bool) {
	t.Helper()
	claim := "not_done"
	if satisfied {
		claim = "satisfied"
	}
	if _, err := store.SubmitDelivery(context.Background(), workspaceID, work.Key, map[string]any{
		"context_digest": work.ContextDigest,
		"agent":          map[string]any{"name": "builder"},
		"criteria": []any{map[string]any{
			"criterion_id": "local-1", "claim": claim,
			"evidence": map[string]any{"summary": "evidence"},
		}},
		"checks": []any{map[string]any{"name": "unit", "status": "pass"}},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestStatsDerivesGovernanceValueFromRecordedHistory(t *testing.T) {
	store, workspaceID := statsStore(t)
	// One item passes first time.
	clean := statsWork(t, store, workspaceID, "Clean pass")
	statsSubmit(t, store, workspaceID, clean, true)
	// One item is caught, reworked, then passes.
	caught := statsWork(t, store, workspaceID, "Caught then fixed")
	statsSubmit(t, store, workspaceID, caught, false)
	statsSubmit(t, store, workspaceID, caught, true)
	// One item is created but never delivered; it must not dilute the ratios.
	statsWork(t, store, workspaceID, "Never delivered")

	stats, err := store.Stats(context.Background(), workspaceID, 30)
	if err != nil {
		t.Fatal(err)
	}

	if stats.WorkItems != 3 || stats.DeliveryReviews != 3 {
		t.Fatalf("workspace totals = %#v", stats)
	}
	if stats.WindowDays != 30 {
		t.Fatalf("window days = %d, want 30", stats.WindowDays)
	}
	if stats.ReviewedItems != 2 {
		t.Fatalf("reviewed items = %d, want 2 (the undelivered item is not reviewed)", stats.ReviewedItems)
	}
	if stats.FirstPass != 1 {
		t.Fatalf("first pass = %d, want 1", stats.FirstPass)
	}
	if stats.ReviewCatchesPostBuild != 1 {
		t.Fatalf("post-build catches = %d, want 1 failed review", stats.ReviewCatchesPostBuild)
	}
	if stats.ReviewCatchesFixed != 1 {
		t.Fatalf("catches later fixed = %d, want 1", stats.ReviewCatchesFixed)
	}
	if stats.Rework != 1 || stats.ItemsWithRework != 1 {
		t.Fatalf("rework = %d across %d items, want 1 across 1", stats.Rework, stats.ItemsWithRework)
	}
	if stats.CycleTimeItems != 2 {
		t.Fatalf("cycle-time items = %d, want 2 items that reached a pass", stats.CycleTimeItems)
	}
	// A measured cycle time, not a silently dropped one: both timestamps must
	// parse for the average to be positive.
	if stats.CycleTimeAvgHours <= 0 {
		t.Fatalf("cycle time = %v, want a measured duration", stats.CycleTimeAvgHours)
	}
}

// Ratios must never be computed from an empty window.
func TestStatsReportsNoReviewedItemsRatherThanZeroPercent(t *testing.T) {
	store, workspaceID := statsStore(t)
	statsWork(t, store, workspaceID, "Never delivered")

	stats, err := store.Stats(context.Background(), workspaceID, 30)
	if err != nil {
		t.Fatal(err)
	}
	if stats.ReviewedItems != 0 || stats.FirstPass != 0 || stats.CycleTimeItems != 0 {
		t.Fatalf("stats invented data for an unreviewed workspace: %#v", stats)
	}
	if stats.WorkItems != 1 {
		t.Fatalf("work items = %d, want 1", stats.WorkItems)
	}
}

