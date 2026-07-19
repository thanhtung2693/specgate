package command

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// specgate up
func newUpCmd(deps *Deps) *cobra.Command {
	var deployDir string
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start the Full appliance",
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := deployDir
			if dir == "" {
				dir = resolveDeployDir(deps)
			}
			svc := makeDeployService(deps, dir)
			if err := svc.Up(cmd.Context()); err != nil {
				code := deps.Printer.Error("up", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}
			cfg, _ := config.LoadFrom(deps.ConfigPath)
			cfg.Mode = config.ModeFull
			cfg.Local = config.LocalStore{}
			cfg.DeploymentDir = dir
			if (cfg.Server == "" || cfg.Server == config.DefaultServerURL) && os.Getenv("SPECGATE_SERVER") == "" && deps.ServerURL == config.DefaultServerURL {
				cfg.Server = inferLocalServerURL(dir)
			}
			if err := saveConfig(deps, cfg); err != nil {
				code := deps.Printer.Error("up", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() != output.ModeJSON {
				fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleSuccess, "Full appliance is up at"), styled(deps, output.StyleAction, dir))
				return nil
			}
			deps.Printer.Success("up", map[string]string{"dir": dir})
			return nil
		},
	}
	cmd.Flags().StringVar(&deployDir, "dir", "", "Deployment directory (overrides config)")
	return cmd
}
