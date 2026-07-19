package command

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
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
		Short: "Show recent governance signals",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if deps.Topology == config.ModeLocal {
				if allWorkspaces {
					return localExitError(deps, "stats", fmt.Errorf("--all-workspaces is not available in Local mode; select a workspace"))
				}
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "stats", err)
				}
				defer store.Close()
				selection, err := localSelection(cmd.Context(), deps, store)
				if err != nil {
					return localExitError(deps, "stats", err)
				}
				stats, err := store.Stats(cmd.Context(), selection.Workspace.ID)
				if err != nil {
					return localExitError(deps, "stats", err)
				}
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("stats", stats)
					return nil
				}
				fmt.Fprintln(deps.Stdout, title(deps, "SpecGate Stats (Local workspace)"))
				fmt.Fprintf(deps.Stdout, "%s %d\n", label(deps, "Work items:"), stats.WorkItems)
				fmt.Fprintf(deps.Stdout, "%s %d\n", label(deps, "Delivered:"), stats.Delivered)
				fmt.Fprintf(deps.Stdout, "%s %d\n", label(deps, "Delivery reviews:"), stats.DeliveryReviews)
				return nil
			}
			workspaceID := ""
			scope := "all workspaces"
			ctx := cmd.Context()
			if !allWorkspaces {
				selection := currentWorkspaceSelection(deps)
				if selection.Source == config.WorkspaceSourceNone {
					payload := output.ErrorPayload{Code: "validation", Message: "select a workspace first with `specgate workspace select`, or use `stats --all-workspaces` for an explicit cross-workspace view"}
					code := deps.Printer.Error("stats", payload)
					return &output.ExitError{Code: code}
				}
				scope = statsScopeLabel(selection)
				var err error
				workspaceID, err = workspaceIDForSelection(cmd.Context(), deps, selection)
				if err != nil {
					return apiExitError(deps, "stats", err)
				}
			} else {
				ctx = client.WithAllWorkspaces(ctx)
			}
			st, err := deps.Client.Stats(ctx, workspaceID, days)
			if err != nil {
				return apiExitError(deps, "stats", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("stats", st)
				return nil
			}
			printStats(deps, st, scope)
			return nil
		},
	}
	cmd.Flags().IntVar(&days, "days", 30, "Window size in days (server clamps to 1-365)")
	cmd.Flags().BoolVar(&allWorkspaces, "all-workspaces", false, "Compute stats across all workspaces")
	return cmd
}

func statsScopeLabel(selection config.ResolvedWorkspace) string {
	if selection.Workspace.Slug == "" {
		return "all workspaces (none selected)"
	}
	return fmt.Sprintf("workspace %q", selection.Workspace.Slug)
}

func printStats(deps *Deps, st *client.StatsResult, scope string) {
	header := fmt.Sprintf("SpecGate Stats (last %d days · %s)", st.WindowDays, scope)
	fmt.Fprintln(deps.Stdout, styled(deps, output.StyleInfo, header))
	if rule := visualRule(deps); rule != "" {
		fmt.Fprintln(deps.Stdout, rule)
	}

	if st.ReviewedItems == 0 {
		// Honesty rule: no fake 0%/100% ratios when nothing was reviewed yet.
		fmt.Fprintln(deps.Stdout, "Not enough data yet — run a few governed work items first.")
		if st.GateCatchesPreBuild > 0 {
			printStatsRow(deps, "Pre-build signals", fmt.Sprintf("%d pre-build gate %s before handoff", st.GateCatchesPreBuild, pluralize(st.GateCatchesPreBuild, "signal", "signals")))
		}
		if st.AmbiguityBlocks > 0 {
			printStatsRow(deps, "Ambiguity reports", fmt.Sprintf("%d blocked-ambiguity %s", st.AmbiguityBlocks, pluralize(st.AmbiguityBlocks, "report", "reports")))
		}
		printStatsLedger(deps, st.Ledger)
		return
	}

	printStatsRow(deps, "Reviewed", fmt.Sprintf("%d work %s", st.ReviewedItems, pluralize(st.ReviewedItems, "item", "items")))
	printStatsRow(deps, "First-pass yield", fmt.Sprintf("%d%% (%d/%d passed first review)", percent(st.FirstPass, st.ReviewedItems), st.FirstPass, st.ReviewedItems))
	printStatsRow(deps, "Pre-build signals", fmt.Sprintf("%d pre-build gate %s before handoff", st.GateCatchesPreBuild, pluralize(st.GateCatchesPreBuild, "signal", "signals")))
	printStatsRow(deps, "Post-build signals", fmt.Sprintf("%d post-build review %s (%d later passed review)", st.ReviewCatchesPostBuild, pluralize(st.ReviewCatchesPostBuild, "signal", "signals"), st.ReviewCatchesFixed))
	printStatsRow(deps, "Rework", fmt.Sprintf("%d %s across %d %s", st.Rework, pluralize(st.Rework, "resubmit", "resubmits"), st.ItemsWithRework, pluralize(st.ItemsWithRework, "item", "items")))
	printStatsRow(deps, "Ambiguity reports", fmt.Sprintf("%d blocked-ambiguity %s", st.AmbiguityBlocks, pluralize(st.AmbiguityBlocks, "report", "reports")))
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
	fmt.Fprintln(deps.Stdout, styled(deps, output.StyleInfo, "Governance signals (recent)"))
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
			styled(deps, output.StyleWarning, fmt.Sprintf("%-15s", statsSignalKind(entry.Kind))),
			detail)
	}
}

func statsSignalKind(kind string) string {
	switch kind {
	case "gate_catch":
		return "gate_signal"
	case "review_catch":
		return "review_signal"
	default:
		return kind
	}
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}
