package command

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/output"
)

func registerPolicyCommands(root *cobra.Command, deps *Deps) {
	root.AddCommand(newPolicyCmd(deps))
}

// specgate policy
//
// Prints a plain three-level table: level name, approval policy, evidence policy.
func newPolicyCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "policy",
		Short: "List built-in governance tiers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			levels, err := deps.Client.ListGovernanceLevels(cmd.Context())
			if err != nil {
				return apiExitError(deps, "policy.list", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("policy", map[string]any{"levels": levels})
				return nil
			}
			if len(levels) == 0 {
				fmt.Fprintln(deps.Stdout, "No governance levels found.")
				return nil
			}
			if humanVisuals(deps) {
				fmt.Fprintln(deps.Stdout, title(deps, "Governance policies"))
				fmt.Fprintln(deps.Stdout, visualRule(deps))
			}
			for _, l := range levels {
				fmt.Fprintf(deps.Stdout, "%s\t%s\t%s\n", styled(deps, output.StyleBold, l.GovernanceLevel), l.ApprovalPolicy, l.EvidencePolicy)
			}
			return nil
		},
	}
}

// printPolicyExplanation writes the policy explanation in plain text to deps.Stdout.
// It renders the real Explanation DTO fields: Title, Reasons, Summary, Obligations.
func printPolicyExplanation(deps *Deps, titleText string, reasons []string, summary string, obligations []string) {
	fmt.Fprintln(deps.Stdout, title(deps, titleText))
	if len(reasons) > 0 {
		fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Why:"), strings.Join(reasons, "; "))
	}
	if summary != "" {
		fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Approval:"), summary)
	}
	for _, o := range obligations {
		fmt.Fprintf(deps.Stdout, "  - %s\n", o)
	}
}
