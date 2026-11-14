package command

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/output"
)

func registerAuditCommands(root *cobra.Command, deps *Deps) {
	root.AddCommand(newAuditCmd(deps))
}

// specgate audit <ref>
//
// Read-only "git log for governance": prints the full chronological governance
// trail for a work reference in the selected Local workspace.
func newAuditCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit <ref>",
		Short: "Show the governance trail for a work item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
				deps.Printer.Success("audit", map[string]any{"events": events, "chain": "unavailable"})
				return nil
			}
			for _, event := range events {
				fmt.Fprintf(deps.Stdout, "%s\t%s\t%s\n", event.CreatedAt, event.Action, event.Detail)
			}
			return nil
		},
	}
	return cmd
}
