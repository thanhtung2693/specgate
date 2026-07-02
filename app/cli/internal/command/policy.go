package command

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/output"
)

func registerPolicyCommands(root *cobra.Command, deps *Deps) {
	pol := &cobra.Command{
		Use:   "policy",
		Short: "Advanced governance policy commands",
	}
	pol.AddCommand(newPolicyListCmd(deps))
	root.AddCommand(pol)
}

// specgate policy list
//
// Prints a plain three-level table: level name, approval policy, evidence policy.
func newPolicyListCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List built-in governance tiers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			levels, err := deps.Client.ListGovernanceLevels(cmd.Context())
			if err != nil {
				code := deps.Printer.Error("policy.list", mapAPIError("policy.list", err))
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("policy.list", map[string]any{"levels": levels})
				return nil
			}
			if len(levels) == 0 {
				fmt.Fprintln(deps.Stdout, "No governance levels found.")
				return nil
			}
			for _, l := range levels {
				fmt.Fprintf(deps.Stdout, "%s\t%s\t%s\n", l.GovernanceLevel, l.ApprovalPolicy, l.EvidencePolicy)
			}
			return nil
		},
	}
}

// printPolicyExplanation writes the policy explanation in plain text to deps.Stdout.
// It renders the real Explanation DTO fields: Title, Reasons, Summary, Obligations.
func printPolicyExplanation(deps *Deps, title string, reasons []string, summary string, obligations []string) {
	fmt.Fprintln(deps.Stdout, title)
	if len(reasons) > 0 {
		fmt.Fprintf(deps.Stdout, "Why: %s\n", strings.Join(reasons, "; "))
	}
	if summary != "" {
		fmt.Fprintf(deps.Stdout, "Approval: %s\n", summary)
	}
	for _, o := range obligations {
		fmt.Fprintf(deps.Stdout, "  - %s\n", o)
	}
}
