package command

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/output"
)

// specgate demo — manage the bundled demo data created by `specgate init` seeding.
func newDemoCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "demo",
		Short: "Manage bundled demo data",
	}
	cmd.AddCommand(newDemoRemoveCmd(deps))
	return cmd
}

// specgate demo remove
//
// Mirror of `specgate init` seeding: deletes only fixed demo seed identifiers
// (DEMO-* keys plus demo-* internal rows/integrations/knowledge) from the
// running stack via POST /maintenance/demo-remove. Idempotent — safe to run
// when no demo data exists.
func newDemoRemoveCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "remove",
		Short: "Delete the demo data created by init seeding",
		RunE: func(cmd *cobra.Command, _ []string) error {
			proceed, err := requireConfirm(deps, "Delete all demo seed data (DEMO-* keys plus demo-* internal rows and integrations)?")
			if err != nil || !proceed {
				return err
			}
			counts, err := deps.Client.RemoveDemo(cmd.Context())
			if err != nil {
				return apiExitError(deps, "demo.remove", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("demo.remove", counts)
				return nil
			}
			fmt.Fprintln(deps.Stdout, styled(deps, output.StyleSuccess, "Demo data removed."))
			return nil
		},
	}
}
