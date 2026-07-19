package command

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/buildinfo"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func newVersionCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version",
		Run: func(*cobra.Command, []string) {
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("version", map[string]string{"version": buildinfo.Version})
				return
			}
			fmt.Fprintf(deps.Stdout, "specgate %s\n", buildinfo.Version)
		},
	}
}
