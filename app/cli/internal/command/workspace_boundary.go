package command

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// enforceProjectWorkspaceBoundary prevents a Git checkout from silently using
// the user's global workspace. Project bindings, repository defaults, and
// one-command overrides are explicit enough to cross this boundary.
func enforceProjectWorkspaceBoundary(cmd *cobra.Command, deps *Deps, cfg config.Config) error {
	if !commandRequiresWorkspaceBoundary(cmd) {
		return nil
	}
	selection := resolveWorkspaceSelection(deps, cfg)
	if selection.Source != config.WorkspaceSourceGlobal {
		return nil
	}
	projectRoot, ok := config.FindProjectRoot(deps.WorkingDir)
	if !ok {
		return nil
	}

	workspace := selection.Workspace.Slug
	if workspace == "" {
		workspace = selection.Workspace.ID
	}
	payload := output.ErrorPayload{
		Code: "missing_workspace_binding",
		Message: fmt.Sprintf(
			"Git project %s is not bound to a SpecGate workspace; refusing to use the global workspace %q. Run `specgate workspace bind` here, or use `--workspace <slug>` for one command",
			projectRoot,
			workspace,
		),
		Details: map[string]any{
			"project_root":   projectRoot,
			"workspace_slug": workspace,
			"fix":            "specgate workspace bind",
		},
	}
	code := deps.Printer.Error(commandOutputName(cmd), payload)
	return &output.ExitError{Code: code}
}

func commandRequiresWorkspaceBoundary(cmd *cobra.Command) bool {
	if cmd.HasSubCommands() {
		return false
	}
	if commandUsesSelectedWorkspace(cmd) {
		return true
	}
	path := strings.Fields(cmd.CommandPath())
	if len(path) < 3 {
		return false
	}
	commandPath := strings.Join(path[1:], " ")
	return commandPath == "work create" ||
		commandPath == "work create-quick" ||
		commandPath == "artifact publish"
}
