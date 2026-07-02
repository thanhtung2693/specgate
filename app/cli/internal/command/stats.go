package command

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func registerStatsCommands(root *cobra.Command, deps *Deps) {
	root.AddCommand(newStatsCmd(deps))
}

// specgate stats [--days N] [--all-workspaces]
//
// Governance-value readout projected server-side from existing gate runs and
// feedback events (GET /api/v1/stats) — no new data collection.
func newStatsCmd(deps *Deps) *cobra.Command {
	var (
		days          int
		allWorkspaces bool
	)
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show what governance caught and saved recently",
		RunE: func(cmd *cobra.Command, _ []string) error {
			workspaceID := ""
			if !allWorkspaces {
				workspaceID = currentIdentityConfig(deps).Workspace.ID
			}
			st, err := deps.Client.Stats(cmd.Context(), workspaceID, days)
			if err != nil {
				code := deps.Printer.Error("stats", mapAPIError("stats", err))
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("stats", st)
				return nil
			}
			printStats(deps, st, allWorkspaces)
			return nil
		},
	}
	cmd.Flags().IntVar(&days, "days", 30, "Window size in days (server clamps to 1-365)")
	cmd.Flags().BoolVar(&allWorkspaces, "all-workspaces", false, "Compute stats across all workspaces")
	return cmd
}

func printStats(deps *Deps, st *client.StatsResult, allWorkspaces bool) {
	scope := "all workspaces"
	if !allWorkspaces {
		if ws := currentIdentityConfig(deps).Workspace.Slug; ws != "" {
			scope = fmt.Sprintf("workspace %q", ws)
		} else {
			// No workspace bootstrapped means the query ran globally; say so
			// instead of claiming a selection that does not exist.
			scope = "all workspaces (none selected)"
		}
	}
	header := fmt.Sprintf("SpecGate Stats (last %d days · %s)", st.WindowDays, scope)
	fmt.Fprintln(deps.Stdout, styled(deps, output.StyleInfo, header))
	if rule := visualRule(deps); rule != "" {
		fmt.Fprintln(deps.Stdout, rule)
	}

	if st.ReviewedItems == 0 {
		// Honesty rule: no fake 0%/100% ratios when nothing was reviewed yet.
		fmt.Fprintln(deps.Stdout, "Not enough data yet — run a few governed work items first.")
		if st.GateCatchesPreBuild > 0 {
			printStatsRow(deps, "Caught pre-build", fmt.Sprintf("%d gate %s before handoff", st.GateCatchesPreBuild, pluralize(st.GateCatchesPreBuild, "failure", "failures")))
		}
		if st.AmbiguityBlocks > 0 {
			printStatsRow(deps, "Ambiguity saves", fmt.Sprintf("%d blocked-ambiguity %s", st.AmbiguityBlocks, pluralize(st.AmbiguityBlocks, "report", "reports")))
		}
		printStatsLedger(deps, st.Ledger)
		return
	}

	printStatsRow(deps, "Reviewed", fmt.Sprintf("%d work %s", st.ReviewedItems, pluralize(st.ReviewedItems, "item", "items")))
	printStatsRow(deps, "First-pass yield", fmt.Sprintf("%d%% (%d/%d passed first review)", percent(st.FirstPass, st.ReviewedItems), st.FirstPass, st.ReviewedItems))
	printStatsRow(deps, "Caught pre-build", fmt.Sprintf("%d gate %s before handoff", st.GateCatchesPreBuild, pluralize(st.GateCatchesPreBuild, "failure", "failures")))
	printStatsRow(deps, "Caught post-build", fmt.Sprintf("%d review %s (%d fixed before merge)", st.ReviewCatchesPostBuild, pluralize(st.ReviewCatchesPostBuild, "failure", "failures"), st.ReviewCatchesFixed))
	printStatsRow(deps, "Rework", fmt.Sprintf("%d %s across %d %s", st.Rework, pluralize(st.Rework, "resubmit", "resubmits"), st.ItemsWithRework, pluralize(st.ItemsWithRework, "item", "items")))
	printStatsRow(deps, "Ambiguity saves", fmt.Sprintf("%d blocked-ambiguity %s", st.AmbiguityBlocks, pluralize(st.AmbiguityBlocks, "report", "reports")))
	if st.CycleTimeItems > 0 {
		printStatsRow(deps, "Cycle time", fmt.Sprintf("avg %.1fh create → pass (%d %s)", st.CycleTimeAvgHours, st.CycleTimeItems, pluralize(st.CycleTimeItems, "item", "items")))
	}
	printStatsLedger(deps, st.Ledger)
}

func printStatsRow(deps *Deps, label, value string) {
	padded := fmt.Sprintf("%-19s", label+":")
	if humanVisuals(deps) {
		fmt.Fprintf(deps.Stdout, "  %s %s %s\n",
			coloredBullet(deps, output.StyleInfo),
			styled(deps, output.StyleMuted, padded),
			value)
		return
	}
	fmt.Fprintf(deps.Stdout, "  %s %s\n", padded, value)
}

func printStatsLedger(deps *Deps, entries []client.StatsLedgerEntry) {
	if len(entries) == 0 {
		return
	}
	fmt.Fprintln(deps.Stdout)
	fmt.Fprintln(deps.Stdout, styled(deps, output.StyleInfo, "Caught by SpecGate (recent)"))
	for _, entry := range entries {
		when := entry.OccurredAt
		if t, err := time.Parse(time.RFC3339, entry.OccurredAt); err == nil {
			when = t.Local().Format("2006-01-02 15:04")
		}
		detail := entry.Detail
		if entry.Gate != "" {
			detail = entry.Gate + ": " + detail
		}
		fmt.Fprintf(deps.Stdout, "  %s  %s  %s  %s\n",
			styled(deps, output.StyleMuted, when),
			styled(deps, output.StyleBold, entry.ChangeRequestKey),
			styled(deps, output.StyleWarning, fmt.Sprintf("%-15s", entry.Kind)),
			detail)
	}
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}
