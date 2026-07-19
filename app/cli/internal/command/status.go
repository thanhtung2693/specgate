package command

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// specgate status
func newStatusCmd(deps *Deps) *cobra.Command {
	var allWorkspaces bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show governance board overview",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if deps.Topology == config.ModeLocal {
				if allWorkspaces {
					return localExitError(deps, "status", fmt.Errorf("--all-workspaces is not available in Local mode; select a workspace"))
				}
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "status", err)
				}
				defer store.Close()
				selection, err := localSelection(cmd.Context(), deps, store)
				if err != nil {
					return localExitError(deps, "status", err)
				}
				items, err := store.ListWork(cmd.Context(), selection.Workspace.ID)
				if err != nil {
					return localExitError(deps, "status", err)
				}
				ready, delivered := 0, 0
				for _, item := range items {
					if item.Phase == "ready" {
						ready++
					}
					if item.Phase == "delivered" {
						delivered++
					}
				}
				result := map[string]any{"mode": "local", "workspace": localWorkspaceConfig(selection.Workspace), "work_items": len(items), "ready": ready, "delivered": delivered}
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("status", result)
					return nil
				}
				fmt.Fprintf(deps.Stdout, "Local workspace: %s\nWork items: %d\nReady: %d\nDelivered: %d\n", selection.Workspace.Slug, len(items), ready, delivered)
				return nil
			}
			workspaceID := ""
			scope := "all workspaces"
			unscoped := allWorkspaces
			ctx := cmd.Context()
			if !allWorkspaces {
				selection := currentWorkspaceSelection(deps)
				if selection.Source == config.WorkspaceSourceNone {
					payload := output.ErrorPayload{Code: "validation", Message: "select a workspace first with `specgate workspace select`, or use `status --all-workspaces` for an explicit cross-workspace view"}
					code := deps.Printer.Error("status", payload)
					return &output.ExitError{Code: code}
				}
				scope = statusScopeLabel(selection)
				var err error
				workspaceID, err = workspaceIDForSelection(cmd.Context(), deps, selection)
				if err != nil {
					return apiExitError(deps, "status", err)
				}
			} else {
				ctx = client.WithAllWorkspaces(ctx)
			}
			st, err := deps.Client.Status(ctx, workspaceID)
			if err != nil {
				return apiExitError(deps, "status", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("status", st)
				return nil
			}
			if humanVisuals(deps) {
				printStatusDashboard(deps, st, unscoped, scope)
			} else {
				printStatusSummary(deps, st, unscoped, scope)
				printAttentionSection(deps, st.NeedsAttention)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&allWorkspaces, "all-workspaces", false, "Show governance board overview across all workspaces")
	return cmd
}

func statusScopeLabel(selection config.ResolvedWorkspace) string {
	if selection.Workspace.Slug == "" {
		return "all workspaces (none selected)"
	}
	switch selection.Source {
	case config.WorkspaceSourceProject:
		return fmt.Sprintf("project workspace %q", selection.Workspace.Slug)
	case config.WorkspaceSourceRepo:
		return fmt.Sprintf("repo workspace %q (.specgate/config)", selection.Workspace.Slug)
	case config.WorkspaceSourceGlobal:
		return fmt.Sprintf("global workspace %q", selection.Workspace.Slug)
	case config.WorkspaceSourceOverride:
		return fmt.Sprintf("override workspace %q", selection.Workspace.Slug)
	default:
		return fmt.Sprintf("selected workspace %q", selection.Workspace.Slug)
	}
}

func printStatusSummary(deps *Deps, st *client.GovernanceStatus, unscoped bool, scope string) {
	fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Scope:"), scope)
	fmt.Fprintf(deps.Stdout, "%s %s total", label(deps, "Work:"), styled(deps, output.StyleBold, fmt.Sprintf("%d", st.Counts.Total)))
	if st.Counts.Total > 0 {
		fmt.Fprintf(deps.Stdout, " %s", styled(deps, output.StyleMuted, "("+phaseBreakdown(st.Counts)+")"))
	}
	fmt.Fprintln(deps.Stdout)
	fmt.Fprintf(deps.Stdout, "%s %s  %s %s\n",
		label(deps, "Ready:"),
		styled(deps, output.StyleSuccess, fmt.Sprintf("%d", st.Counts.Ready)),
		label(deps, "Needs attention:"),
		styled(deps, output.StyleWarning, fmt.Sprintf("%d", len(st.NeedsAttention))))
	if len(st.NeedsAttention) == 0 {
		if st.Counts.Total == 0 {
			fmt.Fprintln(deps.Stdout, "Next: create a quick work item with `specgate work create-quick`.")
		} else if unscoped {
			fmt.Fprintln(deps.Stdout, "Next: no work needs attention right now. Use `specgate work show <ref>` if you know the work item.")
		} else {
			fmt.Fprintln(deps.Stdout, "Next: no work needs attention right now. Use `specgate work list --all-workspaces` to check other workspaces.")
		}
	}
}

func printStatusDashboard(deps *Deps, st *client.GovernanceStatus, unscoped bool, scope string) {
	fmt.Fprintln(deps.Stdout, title(deps, "SpecGate Board"))
	fmt.Fprintln(deps.Stdout, visualRule(deps))
	fmt.Fprintln(deps.Stdout, styled(deps, output.StyleBold, "Summary:"))
	fmt.Fprintf(deps.Stdout, "  %s %s %s\n",
		coloredBullet(deps, output.StyleInfo),
		label(deps, "Scope:"),
		scope)
	fmt.Fprintf(deps.Stdout, "  %s %s %s total",
		coloredBullet(deps, output.StyleInfo),
		label(deps, "Work:"),
		styled(deps, output.StyleBold, fmt.Sprintf("%d", st.Counts.Total)))
	if st.Counts.Total > 0 {
		fmt.Fprintf(deps.Stdout, " %s", styled(deps, output.StyleMuted, "("+phaseBreakdown(st.Counts)+")"))
	}
	fmt.Fprintln(deps.Stdout)
	fmt.Fprintf(deps.Stdout, "  %s %s %s\n",
		coloredBullet(deps, output.StyleSuccess),
		label(deps, "Ready:"),
		styled(deps, output.StyleSuccess, fmt.Sprintf("%d", st.Counts.Ready)))
	fmt.Fprintf(deps.Stdout, "  %s %s %s\n",
		coloredBullet(deps, output.StyleWarning),
		label(deps, "Needs attention:"),
		styled(deps, output.StyleWarning, fmt.Sprintf("%d", len(st.NeedsAttention))))
	if st.Counts.Total > 0 {
		fmt.Fprintf(deps.Stdout, "  %s %s %s %d/%d (%d%%)\n",
			coloredBullet(deps, output.StyleSuccess),
			label(deps, "Ready work:"),
			progressBar(deps, st.Counts.Ready, st.Counts.Total, 18),
			st.Counts.Ready,
			st.Counts.Total,
			percent(st.Counts.Ready, st.Counts.Total))
	}

	if len(st.NeedsAttention) == 0 {
		fmt.Fprintln(deps.Stdout)
		printStatusNextAction(deps, st, unscoped)
		return
	}

	fmt.Fprintln(deps.Stdout)
	printAttentionSection(deps, st.NeedsAttention)
}

// printAttentionSection renders the status board's "Needs Attention" section.
// Shared by `status` and `work list` so both surfaces stay identical.
func printAttentionSection(deps *Deps, items []client.NeedsAttentionItem) {
	if humanVisuals(deps) {
		fmt.Fprintln(deps.Stdout, styled(deps, output.StyleWarning, "Needs Attention"))
		fmt.Fprintln(deps.Stdout, visualRule(deps))
		for _, item := range items {
			fmt.Fprintf(deps.Stdout, "  %s %s — %s\n",
				statusIcon(deps, "warning"),
				styled(deps, output.StyleBold, item.Key),
				item.Title)
			if len(item.Issues) > 0 {
				fmt.Fprintf(deps.Stdout, "    %s %s\n",
					label(deps, "issues:"),
					styled(deps, output.StyleWarning, strings.Join(item.Issues, "; ")))
			}
		}
		return
	}
	for _, item := range items {
		fmt.Fprintf(deps.Stdout, "  ! %s — %s (%s)\n",
			item.Key,
			item.Title,
			strings.Join(item.Issues, "; "))
	}
}

func phaseBreakdown(counts client.PhaseCounts) string {
	parts := make([]string, 0, 6)
	for _, phase := range []struct {
		name  string
		count int
	}{
		{name: "intake", count: counts.Intake},
		{name: "draft", count: counts.Draft},
		{name: "review", count: counts.Review},
		{name: "ready", count: counts.Ready},
		{name: "delivered", count: counts.Delivered},
	} {
		if phase.count > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", phase.name, phase.count))
		}
	}
	if len(parts) == 0 {
		return "no phase counts"
	}
	return strings.Join(parts, ", ")
}

func printStatusNextAction(deps *Deps, st *client.GovernanceStatus, unscoped bool) {
	prefix := "Next:"
	if humanVisuals(deps) {
		prefix = statusIcon(deps, "ready") + " " + label(deps, "Next:")
	}
	if st.Counts.Total == 0 {
		fmt.Fprintf(deps.Stdout, "%s create a quick work item with `specgate work create-quick`.\n", prefix)
	} else if unscoped {
		fmt.Fprintf(deps.Stdout, "%s no work needs attention right now. Use `specgate work show <ref>` if you know the work item.\n", prefix)
	} else {
		fmt.Fprintf(deps.Stdout, "%s no work needs attention right now. Use `specgate work list --all-workspaces` to check other workspaces.\n", prefix)
	}
}
