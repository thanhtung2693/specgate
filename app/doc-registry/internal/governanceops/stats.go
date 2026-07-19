package governanceops

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/specgate/doc-registry/internal/workboard"
)

const (
	statsDefaultWindowDays = 30
	statsMaxWindowDays     = 365
	statsLedgerLimit       = 20
	statsDetailMaxRunes    = 140
)

// StatsReader is the read surface for the governance stats projection. The
// windowed queries are workspace-filtered in SQL so the service never loads
// unbounded rows; delivery runs are fetched across all time because
// first-pass yield is defined against a work item's first review ever.
type StatsReader interface {
	// ListGateRunsForStats returns all gate runs created at or after since,
	// joined with their change request (workspace filter + key + CR created_at).
	ListGateRunsForStats(ctx context.Context, workspaceID string, since time.Time) ([]workboard.StatsGateRun, error)
	// ListDeliveryRunsForStats returns every delivery_review run for the
	// workspace regardless of age, joined with its change request.
	ListDeliveryRunsForStats(ctx context.Context, workspaceID string) ([]workboard.StatsGateRun, error)
	// ListAmbiguityFeedbackForStats returns coding_agent.blocked_ambiguity
	// feedback events created at or after since, joined with their change request.
	ListAmbiguityFeedbackForStats(ctx context.Context, workspaceID string, since time.Time) ([]workboard.StatsFeedbackEvent, error)
}

// StatsInput filters the governance stats projection.
type StatsInput struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	Days        int    `json:"days,omitempty"`
}

// StatsLedgerEntry is one concrete "SpecGate caught something" event.
type StatsLedgerEntry struct {
	OccurredAt       string `json:"occurred_at"`
	ChangeRequestKey string `json:"change_request_key"`
	Kind             string `json:"kind"` // gate_catch | review_catch | ambiguity_block
	Gate             string `json:"gate,omitempty"`
	Detail           string `json:"detail,omitempty"`
}

// StatsResult is the output of Service.Stats — a governance-value readout
// projected from existing gate runs and feedback events.
type StatsResult struct {
	WindowDays             int                `json:"window_days"`
	WorkspaceID            string             `json:"workspace_id,omitempty"`
	ReviewedItems          int                `json:"reviewed_items"`
	FirstPass              int                `json:"first_pass"`
	GateCatchesPreBuild    int                `json:"gate_catches_pre_build"`
	ReviewCatchesPostBuild int                `json:"review_catches_post_build"`
	ReviewCatchesFixed     int                `json:"review_catches_fixed"`
	Rework                 int                `json:"rework"`
	ItemsWithRework        int                `json:"items_with_rework"`
	AmbiguityBlocks        int                `json:"ambiguity_blocks"`
	CycleTimeAvgHours      float64            `json:"cycle_time_avg_hours"`
	CycleTimeItems         int                `json:"cycle_time_items"`
	Ledger                 []StatsLedgerEntry `json:"ledger"`
}

// Stats projects governance-effectiveness signals from existing gate runs and
// feedback events inside a rolling window. No new
// data is collected; everything derives from gate_runs (change_request
// subjects), change_requests, and governance_feedback_events.
func (s *Service) Stats(ctx context.Context, in StatsInput) (StatsResult, error) {
	if s.StatsSource == nil {
		return StatsResult{}, fmt.Errorf("%w: stats source not configured", ErrUnavailable)
	}
	days := in.Days
	if days == 0 {
		days = statsDefaultWindowDays
	}
	if days < 1 {
		days = 1
	}
	if days > statsMaxWindowDays {
		days = statsMaxWindowDays
	}
	workspaceID := strings.TrimSpace(in.WorkspaceID)
	since := time.Now().UTC().AddDate(0, 0, -days)

	runs, err := s.StatsSource.ListGateRunsForStats(ctx, workspaceID, since)
	if err != nil {
		return StatsResult{}, err
	}
	deliveryRuns, err := s.StatsSource.ListDeliveryRunsForStats(ctx, workspaceID)
	if err != nil {
		return StatsResult{}, err
	}
	events, err := s.StatsSource.ListAmbiguityFeedbackForStats(ctx, workspaceID, since)
	if err != nil {
		return StatsResult{}, err
	}

	result := StatsResult{
		WindowDays:      days,
		WorkspaceID:     workspaceID,
		AmbiguityBlocks: len(events),
		Ledger:          make([]StatsLedgerEntry, 0),
	}

	// Delivery-review runs grouped per CR across all time, in chronological
	// order: first-pass yield is about a work item's first review ever, not
	// its first review inside the window.
	deliveryByCR := map[string][]workboard.StatsGateRun{}
	for _, run := range deliveryRuns {
		deliveryByCR[run.ChangeRequestID] = append(deliveryByCR[run.ChangeRequestID], run)
	}

	var cycleHours float64
	for _, seq := range deliveryByCR {
		sort.SliceStable(seq, func(i, j int) bool { return seq[i].RunCreatedAt.Before(seq[j].RunCreatedAt) })

		// A work item counts as reviewed when any of its delivery runs falls
		// inside the window.
		inWindow := 0
		for _, run := range seq {
			if !run.RunCreatedAt.Before(since) {
				inWindow++
			}
		}
		if inWindow == 0 {
			continue
		}

		result.ReviewedItems++
		if seq[0].State == workboard.NextActionStatePass {
			result.FirstPass++
		}
		// Rework = in-window runs beyond the item's first review ever.
		resubmits := inWindow
		if !seq[0].RunCreatedAt.Before(since) {
			resubmits--
		}
		if resubmits > 0 {
			result.Rework += resubmits
			result.ItemsWithRework++
		}
		if latest := seq[len(seq)-1]; latest.State == workboard.NextActionStatePass {
			result.CycleTimeItems++
			// Create → first pass: the earliest passing review closes the loop.
			for _, run := range seq {
				if run.State == workboard.NextActionStatePass {
					cycleHours += run.RunCreatedAt.Sub(run.CRCreatedAt).Hours()
					break
				}
			}
		}
		for i, run := range seq {
			if !statsIsCatch(run.State) || run.RunCreatedAt.Before(since) {
				continue
			}
			result.ReviewCatchesPostBuild++
			for _, later := range seq[i+1:] {
				if later.State == workboard.NextActionStatePass {
					result.ReviewCatchesFixed++
					break
				}
			}
			result.Ledger = append(result.Ledger, statsGateRunLedgerEntry(run, "review_catch"))
		}
	}
	if result.CycleTimeItems > 0 {
		result.CycleTimeAvgHours = cycleHours / float64(result.CycleTimeItems)
	}

	// Pre-build catches count distinct failing (item, gate) pairs — gate runs
	// are point-in-time snapshots, so re-running gates against the same defect
	// must not inflate the readout. The ledger keeps the latest row per pair.
	latestCatchByPair := map[string]workboard.StatsGateRun{}
	for _, run := range runs {
		if run.Gate == "delivery_review" || !statsIsCatch(run.State) {
			continue
		}
		pair := run.ChangeRequestID + "\x00" + run.Gate
		if existing, ok := latestCatchByPair[pair]; !ok || run.RunCreatedAt.After(existing.RunCreatedAt) {
			latestCatchByPair[pair] = run
		}
	}
	result.GateCatchesPreBuild = len(latestCatchByPair)
	for _, run := range latestCatchByPair {
		result.Ledger = append(result.Ledger, statsGateRunLedgerEntry(run, "gate_catch"))
	}
	for _, ev := range events {
		result.Ledger = append(result.Ledger, StatsLedgerEntry{
			OccurredAt:       formatRFC3339(ev.CreatedAt),
			ChangeRequestKey: statsCRKey(ev.ChangeRequestKey, ev.ChangeRequestID),
			Kind:             "ambiguity_block",
			Detail:           statsTruncate(ev.Detail),
		})
	}

	// Newest first. All timestamps are UTC RFC3339 so string order == time order.
	sort.SliceStable(result.Ledger, func(i, j int) bool {
		return result.Ledger[i].OccurredAt > result.Ledger[j].OccurredAt
	})
	if len(result.Ledger) > statsLedgerLimit {
		result.Ledger = result.Ledger[:statsLedgerLimit]
	}
	return result, nil
}

// statsIsCatch reports whether a gate-run state counts as a governance catch.
func statsIsCatch(state workboard.NextActionState) bool {
	return state == workboard.NextActionStateFail || state == workboard.NextActionStateNeedsHumanReview
}

func statsGateRunLedgerEntry(run workboard.StatsGateRun, kind string) StatsLedgerEntry {
	return StatsLedgerEntry{
		OccurredAt:       formatRFC3339(run.RunCreatedAt),
		ChangeRequestKey: statsCRKey(run.ChangeRequestKey, run.ChangeRequestID),
		Kind:             kind,
		Gate:             run.Gate,
		Detail:           statsTruncate(run.Hint),
	}
}

func statsCRKey(key, id string) string {
	if strings.TrimSpace(key) != "" {
		return key
	}
	return id
}

func statsTruncate(s string) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= statsDetailMaxRunes {
		return string(runes)
	}
	return string(runes[:statsDetailMaxRunes-1]) + "…"
}
