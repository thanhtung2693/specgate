package command

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/output"
)

func registerFeatureCommands(root *cobra.Command, deps *Deps) {
	ft := &cobra.Command{
		Use:   "feature",
		Short: "List and inspect governed features",
		Long: "Features are the stable units artifacts and change requests link to.\n" +
			"Use `feature list` to find an existing feature's key before publishing a\n" +
			"new artifact version, so you link to it instead of creating a duplicate.",
	}
	ft.AddCommand(newFeatureListCmd(deps))
	ft.AddCommand(newFeatureShowCmd(deps))
	root.AddCommand(ft)
}

// specgate feature list [--search <text>]
func newFeatureListCmd(deps *Deps) *cobra.Command {
	var search string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List features (their keys, status, and name)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			features, err := deps.Client.ListFeatures(cmd.Context(), search)
			if err != nil {
				code := deps.Printer.Error("feature.list", mapAPIError("feature.list", err))
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("feature.list", map[string]any{"items": features})
				return nil
			}
			if len(features) == 0 {
				fmt.Fprintln(deps.Stdout, "No features found.")
				return nil
			}
			for _, f := range features {
				fmt.Fprintf(deps.Stdout, "%-42s  %-12s  %s\n", f.Key, f.Status, f.Name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&search, "search", "", "Case-insensitive filter on feature key or name")
	return cmd
}

// specgate feature show <key-or-id>
func newFeatureShowCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "show <key-or-id>",
		Short: "Show a feature by its key or id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := deps.Client.GetFeature(cmd.Context(), args[0])
			if err != nil {
				payload := output.ErrorPayload{Code: "not_found", Message: err.Error()}
				code := deps.Printer.Error("feature.show", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("feature.show", f)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "Key:     %s\n", f.Key)
			fmt.Fprintf(deps.Stdout, "Name:    %s\n", f.Name)
			fmt.Fprintf(deps.Stdout, "Status:  %s\n", f.Status)
			fmt.Fprintf(deps.Stdout, "Version: v%d\n", f.Version)
			fmt.Fprintf(deps.Stdout, "ID:      %s\n", f.ID)
			return nil
		},
	}
}
