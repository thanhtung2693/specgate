package command

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// specgate config
func newConfigCmd(deps *Deps) *cobra.Command {
	cfg := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cfg.AddCommand(newConfigServerCmd(deps))
	return cfg
}

func newConfigServerCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "server <url>",
		Short: "Set the SpecGate server URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			serverURL := args[0]
			cfg, err := config.LoadFrom(deps.ConfigPath)
			if err != nil {
				payload := output.ErrorPayload{Code: "unavailable", Message: err.Error()}
				code := deps.Printer.Error("config.server", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			cfg.Server = serverURL
			if err := saveConfig(deps, cfg); err != nil {
				payload := output.ErrorPayload{Code: "unavailable", Message: err.Error()}
				code := deps.Printer.Error("config.server", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() != output.ModeJSON {
				fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleSuccess, "Server set to"), styled(deps, output.StyleAction, serverURL))
				return nil
			}
			deps.Printer.Success("config.server", map[string]string{"server": serverURL})
			return nil
		},
	}
}
