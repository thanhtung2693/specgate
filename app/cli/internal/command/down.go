package command

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/output"
)

// specgate down
func newDownCmd(deps *Deps) *cobra.Command {
	var deployDir string
	cmd := &cobra.Command{
		Use:   "down",
		Short: "Stop the Full appliance",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := deployDir
			if dir == "" {
				dir = resolveDeployDir(deps)
			}
			svc := makeDeployService(deps, dir)
			if err := svc.Down(cmd.Context()); err != nil {
				code := deps.Printer.Error("down", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() != output.ModeJSON {
				fmt.Fprintln(deps.Stdout, styled(deps, output.StyleSuccess, "Full appliance stopped"))
				return nil
			}
			deps.Printer.Success("down", map[string]string{"dir": dir})
			return nil
		},
	}
	cmd.Flags().StringVar(&deployDir, "dir", "", "Deployment directory (overrides config)")
	return cmd
}
