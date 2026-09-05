package command

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/specgate/specgate/app/cli/internal/config"
)

func buildLocalDoctorDiagnostics(ctx context.Context, deps *Deps, cfg config.Config, selectionErr error) map[string]any {
	result := map[string]any{}
	root, found := config.FindProjectRoot(deps.WorkingDir)
	if !found {
		result["repository"] = doctorCheck{Status: "missing", Message: "No Git repository was found.", Command: "cd <repository-root> && specgate workspace bind"}
	} else if _, bound := cfg.Projects[root]; !bound {
		result["repository"] = doctorCheck{Status: "missing", Message: fmt.Sprintf("Repository %s has no workspace binding.", root), Command: "specgate workspace bind"}
	} else if selectionErr != nil {
		result["repository"] = doctorCheck{Status: "stale", Message: fmt.Sprintf("Repository workspace binding cannot be resolved: %v", selectionErr), Command: "specgate workspace bind"}
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
	for _, agent := range pluginAgentNames() {
		if health, native := nativePluginHealth(agent, home); native && health.OK {
			installed = append(installed, agent+" (native)")
			continue
		}
		adapter, _ := pluginAgentAdapterFor(agent)
		if adapter.health(home, false, pkg).OK {
			installed = append(installed, agent)
		}
	}
	if len(installed) == 0 {
		return doctorCheck{Status: "optional", Message: "No global IDE plugin files were detected; CLI-only Local mode remains healthy.", Command: "specgate plugins install"}
	}
	return doctorCheck{Status: "ok", Message: "Plugin files detected for " + strings.Join(installed, ", ") + "; restart the IDE and verify in a new session.", Command: "specgate plugins doctor"}
}
