package governanceops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/workboard"
)

// fakeStatsReader is an in-memory StatsReader for tests. It records the since
// argument and returns preset rows without applying the window filter (the
// repository owns filtering; the service just projects).
type fakeStatsReader struct {
	runs         []workboard.StatsGateRun
	deliveryRuns []workboard.StatsGateRun
	events       []workboard.StatsFeedbackEvent

	lastWorkspaceID string
	lastSince       time.Time
}

func (f *fakeStatsReader) ListGateRunsForStats(_ context.Context, workspaceID string, since time.Time) ([]workboard.StatsGateRun, error) {
	f.lastWorkspaceID = workspaceID
	f.lastSince = since
	return f.runs, nil
}

func (f *fakeStatsReader) ListDeliveryRunsForStats(_ context.Context, workspaceID string) ([]workboard.StatsGateRun, error) {
	f.lastWorkspaceID = workspaceID
	return f.deliveryRuns, nil
}

func (f *fakeStatsReader) ListAmbiguityFeedbackForStats(_ context.Context, _ string, _ time.Time) ([]workboard.StatsFeedbackEvent, error) {
	return f.events, nil
}

func statsRun(cr, key, gate string, state workboard.NextActionState, hint string, runAt, crAt time.Time) workboard.StatsGateRun {
	return workboard.StatsGateRun{
		RunID:            "run-" + gate + runAt.Format("150405"),
		ChangeRequestID:  cr,
		ChangeRequestKey: key,
		Gate:             gate,
		State:            state,
		Hint:             hint,
		RunCreatedAt:     runAt,
		CRCreatedAt:      crAt,
	}
}

func TestStatsRequiresStatsSource(t *testing.T) {
	t.Parallel()
	svc := &Service{}
	_, err := svc.Stats(context.Background(), StatsInput{})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestStatsProjectsMetricsFromGateRunsAndFeedback(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	crA := now.Add(-100 * time.Hour) // CR A created 100h ago
	crB := now.Add(-20 * time.Hour)  // CR B created 20h ago
	crC := now.Add(-40 * time.Hour)

	reader := &fakeStatsReader{
		deliveryRuns: []workboard.StatsGateRun{
			// CR A: delivery fail then pass → reviewed, not first-pass, one
			// resubmit, one fixed review catch, cycle 90h.
			statsRun("cr-a", "CR-A", "delivery_review", workboard.NextActionStateFail, "missing evidence", now.Add(-50*time.Hour), crA),
			statsRun("cr-a", "CR-A", "delivery_review", workboard.NextActionStatePass, "all criteria met", now.Add(-10*time.Hour), crA),
			// CR B: first-pass delivery review → cycle 12h.
			statsRun("cr-b", "CR-B", "delivery_review", workboard.NextActionStatePass, "clean pass", now.Add(-8*time.Hour), crB),
			// CR C: unresolved review catch (needs_human_review, never passed).
			statsRun("cr-c", "CR-C", "delivery_review", workboard.NextActionStateNeedsHumanReview, "needs human review", now.Add(-5*time.Hour), crC),
		},
		runs: []workboard.StatsGateRun{
			// Pre-build gate catch on CR A.
			statsRun("cr-a", "CR-A", "scope_clear", workboard.NextActionStateFail, "scope unclear", now.Add(-60*time.Hour), crA),
			// Warn is not a catch.
			statsRun("cr-b", "CR-B", "ac_coverage", workboard.NextActionStateWarn, "thin coverage", now.Add(-9*time.Hour), crB),
		},
		events: []workboard.StatsFeedbackEvent{
			{EventID: "evt-1", ChangeRequestID: "cr-b", ChangeRequestKey: "CR-B", Detail: "which auth flow applies?", CreatedAt: now.Add(-30 * time.Hour)},
		},
	}
	svc := &Service{StatsSource: reader}

	got, err := svc.Stats(context.Background(), StatsInput{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatal(err)
	}

	if reader.lastWorkspaceID != "ws-1" {
		t.Errorf("workspace forwarded = %q, want ws-1", reader.lastWorkspaceID)
	}
	if got.WindowDays != 30 || got.WorkspaceID != "ws-1" {
		t.Errorf("window echo = (%d, %q), want (30, ws-1)", got.WindowDays, got.WorkspaceID)
	}
	if got.ReviewedItems != 3 {
		t.Errorf("reviewed_items = %d, want 3", got.ReviewedItems)
	}
	if got.FirstPass != 1 {
		t.Errorf("first_pass = %d, want 1", got.FirstPass)
	}
	if got.GateCatchesPreBuild != 1 {
		t.Errorf("gate_catches_pre_build = %d, want 1", got.GateCatchesPreBuild)
	}
	if got.ReviewCatchesPostBuild != 2 {
		t.Errorf("review_catches_post_build = %d, want 2", got.ReviewCatchesPostBuild)
	}
	if got.ReviewCatchesFixed != 1 {
		t.Errorf("review_catches_fixed = %d, want 1", got.ReviewCatchesFixed)
	}
	if got.Rework != 1 || got.ItemsWithRework != 1 {
		t.Errorf("rework = (%d, %d), want (1, 1)", got.Rework, got.ItemsWithRework)
	}
	if got.AmbiguityBlocks != 1 {
		t.Errorf("ambiguity_blocks = %d, want 1", got.AmbiguityBlocks)
	}
	if got.CycleTimeItems != 2 {
		t.Errorf("cycle_time_items = %d, want 2", got.CycleTimeItems)
	}
	if got.CycleTimeAvgHours < 50.9 || got.CycleTimeAvgHours > 51.1 { // (90 + 12) / 2
		t.Errorf("cycle_time_avg_hours = %f, want ~51", got.CycleTimeAvgHours)
	}

	// Ledger: newest first — CR-C review catch, CR-B ambiguity, CR-A review
	// catch, CR-A gate catch.
	wantKinds := []string{"review_catch", "ambiguity_block", "review_catch", "gate_catch"}
	if len(got.Ledger) != len(wantKinds) {
		t.Fatalf("ledger len = %d, want %d — %#v", len(got.Ledger), len(wantKinds), got.Ledger)
	}
	for i, want := range wantKinds {
		if got.Ledger[i].Kind != want {
			t.Errorf("ledger[%d].kind = %q, want %q", i, got.Ledger[i].Kind, want)
		}
	}
	if got.Ledger[0].ChangeRequestKey != "CR-C" || got.Ledger[0].Gate != "delivery_review" {
		t.Errorf("ledger[0] = %#v, want CR-C delivery_review", got.Ledger[0])
	}
	if got.Ledger[1].Detail != "which auth flow applies?" || got.Ledger[1].Gate != "" {
		t.Errorf("ledger[1] = %#v, want ambiguity detail without gate", got.Ledger[1])
	}
	if got.Ledger[3].Gate != "scope_clear" {
		t.Errorf("ledger[3].gate = %q, want scope_clear", got.Ledger[3].Gate)
	}
}

func TestStatsClampsWindowDays(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		days int
		want int
	}{
		{days: 0, want: 30},
		{days: -5, want: 1},
		{days: 7, want: 7},
		{days: 1000, want: 365},
	} {
		reader := &fakeStatsReader{}
		svc := &Service{StatsSource: reader}
		got, err := svc.Stats(context.Background(), StatsInput{Days: tc.days})
		if err != nil {
			t.Fatal(err)
		}
		if got.WindowDays != tc.want {
			t.Errorf("days %d → window_days = %d, want %d", tc.days, got.WindowDays, tc.want)
		}
		wantSince := time.Now().UTC().AddDate(0, 0, -tc.want)
		if diff := reader.lastSince.Sub(wantSince); diff < -time.Minute || diff > time.Minute {
			t.Errorf("days %d → since = %v, want ~%v", tc.days, reader.lastSince, wantSince)
		}
	}
}

func TestStatsLedgerCapsTruncatesAndFallsBackToID(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	longHint := strings.Repeat("x", 200)
	reader := &fakeStatsReader{}
	for i := 0; i < 25; i++ {
		reader.runs = append(reader.runs, statsRun(
			fmt.Sprintf("cr-%d", i), "", "scope_clear", workboard.NextActionStateFail,
			longHint, now.Add(-time.Duration(i)*time.Hour), now.Add(-100*time.Hour)))
	}
	svc := &Service{StatsSource: reader}

	got, err := svc.Stats(context.Background(), StatsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if got.GateCatchesPreBuild != 25 {
		t.Errorf("gate_catches_pre_build = %d, want 25", got.GateCatchesPreBuild)
	}
	if len(got.Ledger) != 20 {
		t.Fatalf("ledger len = %d, want 20", len(got.Ledger))
	}
	if got.Ledger[0].ChangeRequestKey != "cr-0" {
		t.Errorf("ledger[0].change_request_key = %q, want cr-0 (id fallback)", got.Ledger[0].ChangeRequestKey)
	}
	if len([]rune(got.Ledger[0].Detail)) > 140 {
		t.Errorf("ledger detail len = %d, want <= 140", len([]rune(got.Ledger[0].Detail)))
	}
	if !strings.HasSuffix(got.Ledger[0].Detail, "…") {
		t.Errorf("ledger detail %q should be truncated with ellipsis", got.Ledger[0].Detail)
	}
}

func TestStatsFirstPassUsesFirstReviewEver(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	crAt := now.Add(-90 * 24 * time.Hour)

	reader := &fakeStatsReader{
		deliveryRuns: []workboard.StatsGateRun{
			// First review ever failed 60 days ago (outside the 30-day window);
			// the in-window pass must not count as passing "first review".
			statsRun("cr-old", "CR-OLD", "delivery_review", workboard.NextActionStateFail, "old fail", now.Add(-60*24*time.Hour), crAt),
			statsRun("cr-old", "CR-OLD", "delivery_review", workboard.NextActionStatePass, "finally", now.Add(-2*time.Hour), crAt),
		},
	}
	svc := &Service{StatsSource: reader}

	got, err := svc.Stats(context.Background(), StatsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if got.ReviewedItems != 1 {
		t.Fatalf("reviewed_items = %d, want 1", got.ReviewedItems)
	}
	if got.FirstPass != 0 {
		t.Errorf("first_pass = %d, want 0 (first review ever failed)", got.FirstPass)
	}
	// The in-window run is a resubmit relative to the first-ever review.
	if got.Rework != 1 || got.ItemsWithRework != 1 {
		t.Errorf("rework = (%d, %d), want (1, 1)", got.Rework, got.ItemsWithRework)
	}
	// The pre-window catch is not an in-window post-build catch.
	if got.ReviewCatchesPostBuild != 0 {
		t.Errorf("review_catches_post_build = %d, want 0", got.ReviewCatchesPostBuild)
	}
}

func TestStatsPreBuildCatchesCountDistinctFailingGates(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	crAt := now.Add(-30 * time.Hour)

	reader := &fakeStatsReader{
		runs: []workboard.StatsGateRun{
			// The same gate failing across three snapshot refreshes is one
			// defect, not three catches.
			statsRun("cr-x", "CR-X", "scope_clear", workboard.NextActionStateFail, "scope unclear", now.Add(-20*time.Hour), crAt),
			statsRun("cr-x", "CR-X", "scope_clear", workboard.NextActionStateFail, "scope unclear", now.Add(-15*time.Hour), crAt),
			statsRun("cr-x", "CR-X", "scope_clear", workboard.NextActionStateFail, "still unclear", now.Add(-10*time.Hour), crAt),
			// A different failing gate on the same item is a second catch.
			statsRun("cr-x", "CR-X", "ac_quality", workboard.NextActionStateFail, "untestable AC", now.Add(-9*time.Hour), crAt),
		},
	}
	svc := &Service{StatsSource: reader}

	got, err := svc.Stats(context.Background(), StatsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if got.GateCatchesPreBuild != 2 {
		t.Fatalf("gate_catches_pre_build = %d, want 2 distinct (item, gate) pairs", got.GateCatchesPreBuild)
	}
	gateCatches := 0
	for _, entry := range got.Ledger {
		if entry.Kind == "gate_catch" {
			gateCatches++
			if entry.Gate == "scope_clear" && entry.Detail != "still unclear" {
				t.Errorf("ledger should keep the latest snapshot per pair, got detail %q", entry.Detail)
			}
		}
	}
	if gateCatches != 2 {
		t.Errorf("ledger gate_catch entries = %d, want 2", gateCatches)
	}
}
