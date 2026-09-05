package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
	"github.com/spf13/cobra"
)

func newWorkVerificationCmd(deps *Deps) *cobra.Command {
	var file string
	var dryRun bool
	cmd := &cobra.Command{
		Use: "verification <ref>", Short: "Show or pin a Local work item's verification commands",
		Example: "  specgate work verification LOCAL-123 --json\n  specgate work verification LOCAL-123 --file checks.json --dry-run\n  specgate --yes work verification LOCAL-123 --file checks.json",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			const op = "work.verification"
			if deps.Topology != config.ModeLocal {
				return incompatibleCommand(deps, op, "verification contracts are Local-only")
			}
			if dryRun && file == "" {
				return completionValidationError(deps, op, "--dry-run requires --file")
			}
			store, err := openLocalStore(deps)
			if err != nil {
				return localExitError(deps, op, err)
			}
			defer store.Close()
			sel, err := localSelection(cmd.Context(), deps, store)
			if err != nil {
				return localExitError(deps, op, err)
			}
			contract, err := store.GetVerificationContract(cmd.Context(), sel.Workspace.ID, args[0])
			if err != nil {
				return localExitError(deps, op, err)
			}
			if file != "" {
				root, ok := config.FindProjectRoot(deps.WorkingDir)
				if !ok {
					return completionValidationError(deps, op, "pin from a Git repository checkout")
				}
				body, err := readJSONBodyFile(deps, op, file)
				if err != nil {
					return err
				}
				raw, err := json.Marshal(body)
				if err != nil {
					return localExitError(deps, op, err)
				}
				var input local.VerificationContractInput
				decoder := json.NewDecoder(bytes.NewReader(raw))
				decoder.DisallowUnknownFields()
				if err := decoder.Decode(&input); err != nil {
					return completionValidationError(deps, op, err.Error())
				}
				contract, err = store.PreviewVerificationContract(cmd.Context(), sel.Workspace.ID, args[0], root, sel.User.Username, input)
				if err != nil {
					return localExitError(deps, op, err)
				}
				if !dryRun {
					if !deps.Yes {
						for _, check := range contract.Checks {
							fmt.Fprintf(deps.Stderr, "sh in %s: %s\n", check.Cwd, check.Command)
						}
					}
					proceed, err := requireConfirm(deps, "Pin these verification commands? They cannot be changed for this work.")
					if err != nil || !proceed {
						return err
					}
					contract, err = store.PinVerificationContract(cmd.Context(), sel.Workspace.ID, args[0], root, sel.User.Username, input)
					if err != nil {
						return localExitError(deps, op, err)
					}
				} else {
					contract.Status = "preview"
				}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success(op, contract)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "Verification contract: %s\n", contract.Status)
			for _, check := range contract.Checks {
				fmt.Fprintf(deps.Stdout, "  %s (sh, %s): %s\n", check.Name, check.Cwd, check.Command)
			}
			if contract.Status == "unconfigured" {
				fmt.Fprintln(deps.Stdout, "Checks are self-selected; pin reviewed commands with --file checks.json before the first submission.")
			} else {
				fmt.Fprintf(deps.Stdout, "Next: specgate work resume %s\n", args[0])
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "JSON containing context_digest, shell (sh), and checks [{name,command,cwd}]")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview the contract without pinning it")
	return cmd
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
