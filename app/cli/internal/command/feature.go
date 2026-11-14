package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/output"
	"github.com/specgate/specgate/app/cli/internal/workflow"
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

// specgate feature list [--search <text>] [--all]
func newFeatureListCmd(deps *Deps) *cobra.Command {
	var search string
	var includeArchived bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List features (their keys, status, and name)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			features, err := listLocalFeatures(cmd.Context(), deps, search)
			if err != nil {
				return localExitError(deps, "feature.list", err)
			}
			return printFeatureList(deps, features, includeArchived)
		},
	}
	cmd.Flags().StringVar(&search, "search", "", "Case-insensitive filter on feature key or name")
	cmd.Flags().BoolVar(&includeArchived, "all", false, "Include archived features (hidden by default)")
	return cmd
}

// specgate feature show <key-or-id>
func newFeatureShowCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "show <key-or-id>",
		Short: "Show a feature by its key or id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			features, err := listLocalFeatures(cmd.Context(), deps, "")
			if err != nil {
				return localExitError(deps, "feature.show", err)
			}
			f := findFeature(features, args[0])
			if f == nil {
				err := fmt.Errorf("feature %q not found", args[0])
				code := deps.Printer.Error("feature.show", output.ErrorPayload{
					Code:    "not_found",
					Message: err.Error() + " in the selected Local workspace; run `specgate feature list --all`",
				})
				return &output.ExitError{Code: code, Err: err}
			}
			return printFeature(deps, f)
		},
	}
}

func listLocalFeatures(ctx context.Context, deps *Deps, search string) ([]workflow.Feature, error) {
	store, err := openLocalStore(deps)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	selection, err := localSelection(ctx, deps, store)
	if err != nil {
		return nil, err
	}
	items, err := store.ListFeatures(ctx, selection.Workspace.ID)
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(strings.TrimSpace(search))
	features := make([]workflow.Feature, 0, len(items))
	for _, item := range items {
		if needle != "" && !strings.Contains(strings.ToLower(item.Key), needle) {
			continue
		}
		features = append(features, workflow.Feature{
			ID:                  item.ID,
			WorkspaceID:         item.WorkspaceID,
			Key:                 item.Key,
			Name:                item.Key,
			Status:              "active",
			Version:             item.Version,
			CanonicalArtifactID: item.CanonicalArtifactID,
		})
	}
	return features, nil
}

func printFeatureList(deps *Deps, features []workflow.Feature, includeArchived bool) error {
	if !includeArchived {
		current := make([]workflow.Feature, 0, len(features))
		for _, feature := range features {
			if feature.Status != "archived" {
				current = append(current, feature)
			}
		}
		features = current
	}
	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success("feature.list", map[string]any{"items": features})
		return nil
	}
	if len(features) == 0 {
		fmt.Fprintln(deps.Stdout, notice(deps, output.StyleWarning, "Notice", "No features found."))
		return nil
	}
	for _, feature := range features {
		fmt.Fprintf(deps.Stdout, "%s  %s  %s\n", styled(deps, output.StyleBold, fmt.Sprintf("%-42s", feature.Key)), styledStatusPadded(deps, feature.Status, 12), feature.Name)
	}
	return nil
}

func printFeature(deps *Deps, feature *workflow.Feature) error {
	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success("feature.show", feature)
		return nil
	}
	fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Key:"), styled(deps, output.StyleBold, feature.Key))
	fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Name:"), feature.Name)
	fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Status:"), styledStatus(deps, feature.Status))
	fmt.Fprintf(deps.Stdout, "%s v%d\n", label(deps, "Version:"), feature.Version)
	fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "ID:"), feature.ID)
	return nil
}

func findFeature(features []workflow.Feature, ref string) *workflow.Feature {
	needle := strings.ToLower(strings.TrimSpace(ref))
	for index := range features {
		if strings.ToLower(features[index].Key) == needle || strings.ToLower(features[index].ID) == needle {
			return &features[index]
		}
	}
	return nil
}
