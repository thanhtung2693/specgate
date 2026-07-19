package command

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func registerAuditCommands(root *cobra.Command, deps *Deps) {
	root.AddCommand(newAuditCmd(deps))
}

// specgate audit <ref>
//
// Read-only "git log for governance": prints the full chronological governance
// trail for a work reference (change-request id or key). --json prints the raw
// AuditTrail (GET /api/v1/audit/{ref}).
func newAuditCmd(deps *Deps) *cobra.Command {
	var verify bool
	cmd := &cobra.Command{
		Use:   "audit <ref>",
		Short: "Show the governance trail for a work item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if deps.Topology == config.ModeLocal {
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "audit", err)
				}
				defer store.Close()
				selection, err := localSelection(cmd.Context(), deps, store)
				if err != nil {
					return localExitError(deps, "audit", err)
				}
				events, err := store.Audit(cmd.Context(), selection.Workspace.ID, args[0])
				if err != nil {
					return localExitError(deps, "audit", err)
				}
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("audit", map[string]any{"events": events, "verified": true})
					return nil
				}
				for _, event := range events {
					fmt.Fprintf(deps.Stdout, "%s\t%s\t%s\n", event.CreatedAt, event.Action, event.Detail)
				}
				return nil
			}
			trail, err := deps.Client.AuditTrail(cmd.Context(), args[0], verify)
			if err != nil {
				code := deps.Printer.Error("audit", mapWorkRefError(args[0], err))
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("audit", trail)
				return nil
			}
			printAuditTrail(deps, trail)
			return nil
		},
	}
	cmd.Flags().BoolVar(&verify, "verify", false, "Recompute the tamper-evidence event chain and report intact/tampered")
	return cmd
}

func printAuditTrail(deps *Deps, trail *client.AuditTrail) {
	header := trail.ChangeRequestKey
	if header == "" {
		header = trail.Ref
	}
	if trail.Title != "" {
		header += " — " + trail.Title
	}
	fmt.Fprintln(deps.Stdout, title(deps, header))
	if trail.FeatureName != "" {
		fmt.Fprintf(deps.Stdout, "%s %s (%s) · %s\n",
			label(deps, "Feature:"),
			trail.FeatureName, trail.FeatureKey, trail.Phase)
	}
	if rule := visualRule(deps); rule != "" {
		fmt.Fprintln(deps.Stdout, rule)
	}
	if len(trail.Events) == 0 {
		fmt.Fprintln(deps.Stdout, "No governance events recorded yet.")
		return
	}
	for _, ev := range trail.Events {
		fmt.Fprintln(deps.Stdout, formatAuditEvent(deps, ev))
	}
	if trail.Chain != nil {
		fmt.Fprintln(deps.Stdout, formatChainReport(deps, trail.Chain))
	}
}

// formatChainReport renders the tamper-evidence verdict from audit --verify.
func formatChainReport(deps *Deps, c *client.ChainReport) string {
	switch c.State {
	case "intact":
		return styled(deps, output.StyleSuccess, fmt.Sprintf("Chain: intact (%d chained events)", c.ChainedEvents))
	case "tampered":
		return styled(deps, output.StyleDanger,
			fmt.Sprintf("Chain: TAMPERED at event %s (artifact %s)", c.FirstBadEventID, c.ArtifactID))
	default:
		return "Chain: " + c.State
	}
}

// formatAuditEvent renders one timeline line:
// <date>  <actor[+kind]>  <action>  [verdict]  [trust]  [detail]
func formatAuditEvent(deps *Deps, ev client.AuditEvent) string {
	when := ev.Timestamp
	if t, err := time.Parse(time.RFC3339, ev.Timestamp); err == nil {
		when = t.Local().Format("2006-01-02 15:04")
	}
	actor := ev.Actor
	if ev.ActorKind != "" {
		if actor == "" {
			actor = ev.ActorKind
		} else {
			actor = fmt.Sprintf("%s(%s)", actor, ev.ActorKind)
		}
	}
	if actor == "" {
		actor = "-"
	}
	parts := []string{
		styled(deps, output.StyleMuted, when),
		styled(deps, output.StyleBold, fmt.Sprintf("%-16s", actor)),
		ev.Action,
	}
	if ev.Verdict != "" {
		parts = append(parts, styled(deps, output.StyleWarning, auditVerdictLabel(ev.Verdict, ev.Trust)))
	}
	if ev.Trust != "" {
		parts = append(parts, styled(deps, output.StyleMuted, "["+auditTrustLabel(ev.Trust)+"]"))
	}
	if ev.Detail != "" {
		parts = append(parts, ev.Detail)
	}
	return "  " + strings.Join(parts, "  ")
}

func auditVerdictLabel(verdict, trust string) string {
	switch strings.TrimSpace(verdict) {
	case "pass", "passed":
		if strings.TrimSpace(trust) == "human" {
			return "Accepted"
		}
		return "Ready for human review"
	case "fail", "failed":
		return "Evidence gaps found"
	case "needs_human_review":
		return "Independent confirmation required"
	case "warn":
		return "Review advised"
	default:
		return verdict
	}
}

func auditTrustLabel(trust string) string {
	switch strings.TrimSpace(trust) {
	case "agent_attested":
		return "Agent-reported"
	case "platform_evaluated":
		return "Platform-evaluated"
	case "human":
		return "Human decision"
	case "deterministic":
		return "Locally reproduced"
	default:
		return trust
	}
}
