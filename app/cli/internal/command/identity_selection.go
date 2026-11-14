package command

import (
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
)

type identitySelection struct {
	User      local.User      `json:"user"`
	Workspace local.Workspace `json:"workspace"`
}

func saveIdentitySelection(deps *Deps, selection identitySelection) error {
	cfg, _ := config.LoadFrom(deps.ConfigPath)
	cfg.CurrentUser = config.CurrentUser{
		ID:          selection.User.ID,
		Username:    selection.User.Username,
		DisplayName: selection.User.DisplayName,
		Email:       selection.User.Email,
	}
	cfg.Workspace = config.CurrentWorkspace{
		ID:   selection.Workspace.ID,
		Slug: selection.Workspace.Slug,
		Name: selection.Workspace.Name,
	}
	// Existing per-project bindings survive sign-in. Workspace-scoped commands
	// refuse to run in an unbound project, so discarding them would break every
	// checkout on the machine; a binding that no longer resolves fails loudly on
	// its own project instead.
	return saveConfig(deps, cfg)
}

func saveWorkspaceSelection(deps *Deps, workspace local.Workspace) error {
	cfg, _ := config.LoadFrom(deps.ConfigPath)
	cfg.Workspace = localWorkspaceConfig(workspace)
	return saveConfig(deps, cfg)
}

func saveConfig(deps *Deps, cfg config.Config) error {
	if deps.ConfigPath != "" {
		return cfg.SaveTo(deps.ConfigPath)
	}
	return cfg.Save()
}

func currentWorkspaceSelection(deps *Deps) config.ResolvedWorkspace {
	cfg, _ := config.LoadFrom(deps.ConfigPath)
	return resolveWorkspaceSelection(deps, cfg)
}

func resolveWorkspaceSelection(deps *Deps, cfg config.Config) config.ResolvedWorkspace {
	repoCfg := config.LoadRepoConfig(deps.WorkingDir)
	return config.ResolveWorkspaceSource(cfg, repoCfg, deps.WorkingDir, deps.WorkspaceOverride)
}
