package command

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/output"
)

// specgate workspace credential <username> [--revoke]
//
// Issues the gateway credential a teammate needs on a shared appliance (ADR
// 2026-07-29 gateway-asserted identity). The appliance generates the secret and
// returns it once; only its bcrypt hash is stored, so a lost secret is reissued
// rather than recovered.
//
// Issuing the first credential turns gateway authentication on for the whole
// appliance: from then on every caller must present one, and the identity nginx
// forwards becomes the actor recorded against approvals and delivery decisions.
// Revoking the last one turns it off again.
func newWorkspaceCredentialCmd(deps *Deps) *cobra.Command {
	var revoke bool
	cmd := &cobra.Command{
		Use:   "credential <username>",
		Short: "Issue, rotate, or revoke a member's gateway credential (Full mode)",
		Long: "Issue the credential a member presents to the appliance gateway. The appliance\n" +
			"generates the secret and shows it once; it is stored only as a hash, so a lost\n" +
			"secret is reissued rather than recovered. Re-running rotates: the previous secret\n" +
			"stops working.\n\n" +
			"Issuing the first credential requires authentication from every caller afterwards,\n" +
			"including the web UI. Revoking the last one returns the appliance to trusting its\n" +
			"network. Local mode has no gateway and no credentials.",
		Example: "  specgate workspace credential mai\n" +
			"  specgate workspace credential mai --revoke",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// ponytail: the command catalog already refuses Full-mode-only commands in
			// Local mode, naming the command and the fix. No second check here.
			result, err := deps.Client.IssueGatewayCredential(cmd.Context(), args[0], revoke)
			if err != nil {
				return apiExitError(deps, "workspace.credential", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("workspace.credential", result)
				return nil
			}
			if revoke {
				fmt.Fprintf(deps.Stdout, "%s %s\n",
					styled(deps, output.StyleSuccess, "Credential revoked for"), styled(deps, output.StyleBold, result.Username))
				return nil
			}
			// Shown once by design: the appliance keeps only a hash.
			fmt.Fprintf(deps.Stdout, "%s %s\n",
				styled(deps, output.StyleSuccess, "Credential issued for"), styled(deps, output.StyleBold, result.Username))
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Secret:"), result.Secret)
			fmt.Fprintln(deps.Stdout, "This is the only time the secret is shown. Rotate it if it is lost.")
			fmt.Fprintln(deps.Stdout, nextStep(deps, "On that developer's machine, store it with",
				"specgate config credential "+result.Username))
			return nil
		},
	}
	cmd.Flags().BoolVar(&revoke, "revoke", false, "Remove this member's credential instead of issuing one")
	return cmd
}
