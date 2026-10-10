package command

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func runLocalDoctor(ctx context.Context, deps *Deps) error {
	statePath, err := localStatePath(deps)
	if err != nil {
		return localExitError(deps, "doctor", err)
	}
	store, err := local.Open(statePath)
	if err != nil {
		return localExitError(deps, "doctor", err)
	}
	defer store.Close()
	selection, err := store.Current(ctx)
	missingSelection := errors.Is(err, sql.ErrNoRows)
	if missingSelection {
		selection.StoreID, err = store.ID(ctx)
	}
	if err != nil {
		return localExitError(deps, "doctor", err)
	}
	// Preserve durable selection when a project override cannot be resolved.
	resolvedSelection, selectionErr := localSelection(ctx, deps, store)
	if selectionErr == nil {
		selection = resolvedSelection
	}
	cfg, _ := config.LoadFrom(deps.ConfigPath)
	identity := map[string]any{"status": "ok", "username": selection.User.Username}
	workspace := map[string]any{"status": "ok", "slug": selection.Workspace.Slug}
	if missingSelection {
		identity["status"], identity["command"] = "missing", "specgate user login"
		workspace["status"], workspace["command"] = "missing", "specgate user login"
	}
	result := map[string]any{
		"mode":      "local",
		"store":     map[string]any{"path": filepath.Dir(statePath), "id": selection.StoreID, "status": "ok"},
		"identity":  identity,
		"workspace": workspace,
		"network":   map[string]any{"status": "not_required", "message": "Local mode uses no server or TCP service"},
	}
	diagnostics := buildLocalDoctorDiagnostics(ctx, deps, cfg, selectionErr, selection.Workspace.Slug)
	for key, value := range diagnostics {
		result[key] = value
	}
	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success("doctor", result)
		return nil
	}
	fmt.Fprintln(deps.Stdout, title(deps, "SpecGate Doctor"))
	if missingSelection {
		fmt.Fprintln(deps.Stdout, notice(deps, output.StyleWarning, "Local mode", "setup incomplete"))
		fmt.Fprintln(deps.Stdout, "No user or workspace is selected. Run `specgate user login`.")
	} else {
		fmt.Fprintln(deps.Stdout, notice(deps, output.StyleSuccess, "Local mode", "ready"))
	}
	fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Store:"), filepath.Dir(statePath))
	fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "User:"), selection.User.Username)
	fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Workspace:"), selection.Workspace.Slug)
	fmt.Fprintf(deps.Stdout, "%s not required\n", label(deps, "Network:"))
	for _, key := range []string{"repository", "shell", "plugins"} {
		check := diagnostics[key].(doctorCheck)
		checkLabel := map[string]string{"repository": "Repository:", "shell": "Shell:", "plugins": "Plugins:"}[key]
		fmt.Fprintf(deps.Stdout, "%s %s — %s\n", label(deps, checkLabel), check.Status, check.Message)
		if check.Command != "" {
			fmt.Fprintf(deps.Stdout, "  %s %s\n", label(deps, "next:"), check.Command)
		}
	}
	return nil
}

func buildLocalDoctorDiagnostics(ctx context.Context, deps *Deps, cfg config.Config, selectionErr error, workspaceSlug string) map[string]any {
	result := map[string]any{}
	root, found := config.FindProjectRoot(deps.WorkingDir)
	if !found {
		result["repository"] = doctorCheck{Status: "missing", Message: "No Git repository was found.", Command: "cd <repository-root> && specgate workspace bind"}
	} else if _, bound := cfg.Projects[root]; !bound {
		result["repository"] = doctorCheck{Status: "missing", Message: fmt.Sprintf("Repository %s has no workspace binding.", root), Command: "specgate workspace bind"}
	} else if selectionErr != nil {
		result["repository"] = doctorCheck{Status: "stale", Message: fmt.Sprintf("Repository workspace binding cannot be resolved: %v; rebind to the current Local workspace %s", selectionErr, workspaceSlug), Command: "specgate workspace bind " + workspaceSlug}
	} else {
		result["repository"] = doctorCheck{Status: "ok", Message: root}
	}
	if path, err := exec.LookPath("sh"); err != nil {
		result["shell"] = doctorCheck{Status: "missing", Message: "The required sh shell is unavailable.", Command: "install a POSIX sh shell and ensure it is on PATH"}
	} else {
		result["shell"] = doctorCheck{Status: "ok", Message: path}
	}
	result["plugins"] = localPluginDiagnostic(ctx, deps)
	return result
}

func localPluginDiagnostic(ctx context.Context, deps *Deps) doctorCheck {
	home, err := userHomeDir(deps)
	if err != nil {
		return doctorCheck{Status: "optional", Message: "IDE plugins are optional; their files could not be inspected.", Command: "specgate plugins doctor"}
	}
	pkg, _ := (embeddedLocalPlugin{}).PluginPackage(ctx)
	var installed []string
	var commands []string
	root, hasRoot := config.FindProjectRoot(deps.WorkingDir)
	for _, agent := range pluginAgentNames() {
		adapter, _ := pluginAgentAdapterFor(agent)
		if hasRoot && adapter.health(home, true, pkg, root).OK {
			installed = append(installed, agent+" (project)")
			commands = append(commands, "cd "+shellQuote(root)+" && specgate plugins doctor --agent "+agent+" --project-local")
		}
		if health, native := nativePluginHealth(agent, home); native && health.OK {
			installed = append(installed, agent+" (native)")
			commands = append(commands, "specgate plugins doctor --agent "+agent)
			continue
		}
		if adapter.health(home, false, pkg).OK {
			installed = append(installed, agent+" (global)")
			commands = append(commands, "specgate plugins doctor --agent "+agent)
		}
	}
	if len(installed) == 0 {
		return doctorCheck{Status: "optional", Message: "No complete global or project IDE plugin files were detected; CLI-only Local mode remains healthy.", Command: "specgate plugins install"}
	}
	return doctorCheck{Status: "ok", Message: "Plugin files detected for " + strings.Join(installed, ", ") + "; restart the IDE and verify in a new session.", Command: strings.Join(commands, " && ")}
}
