package command

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/output"
)

// specgate status
func newStatusCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show governance board overview",
		RunE: func(cmd *cobra.Command, _ []string) error {

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
		},
	}
	return cmd
}
