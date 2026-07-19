package command

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/output"
)

// specgate local-status
func newLocalStatusCmd(deps *Deps) *cobra.Command {
	var deployDir string
	cmd := &cobra.Command{
		Use:   "local-status",
		Short: "Show the status of the Full appliance",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := deployDir
			if dir == "" {
				dir = resolveDeployDir(deps)
			}
			svc := makeDeployService(deps, dir)
			statuses, err := svc.LocalStatus(cmd.Context())
			if err != nil {
				code := deps.Printer.Error("local-status", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("local-status", statuses)
				return nil
			}
			if len(statuses) == 0 {
				fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "No services running in"), dir)
				return nil
			}
			fmt.Fprintln(deps.Stdout, title(deps, "Full appliance"))
			for _, s := range statuses {
				fmt.Fprintf(deps.Stdout, "%-30s  %s\n", s.Name, styledStatus(deps, s.Status))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&deployDir, "dir", "", "Deployment directory (overrides config)")
	return cmd
}
