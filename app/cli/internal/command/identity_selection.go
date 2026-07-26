package command

import (
	"context"
	"fmt"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/interactive"
)

func validateWorkspaceSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("workspace slug is required")
	}
	if isUUIDLike(slug) {
		return fmt.Errorf("workspace slug must use lowercase letters, numbers, and hyphens; internal workspace IDs are not accepted")
	}
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return fmt.Errorf("workspace slug must use lowercase letters, numbers, and hyphens")
	}
	return nil
}

func isUUIDLike(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !((r >= 'a' && r <= 'f') || (r >= '0' && r <= '9')) {
				return false
			}
		}
	}
	return true
}

func saveIdentitySelection(deps *Deps, selection client.IdentitySelection) error {
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

func saveWorkspaceSelection(deps *Deps, workspace client.IdentityWorkspace, scope string, projectRoot string) error {
	cfg, _ := config.LoadFrom(deps.ConfigPath)
	selected := config.CurrentWorkspace{
		ID:   workspace.ID,
		Slug: workspace.Slug,
		Name: workspace.Name,
	}
	if scope == workspaceSaveScopeProject {
		cfg.SetProjectWorkspace(projectRoot, selected)
	} else {
		cfg.Workspace = selected
		if projectRoot != "" {
			cfg.RemoveProjectWorkspace(projectRoot)
		}
	}
	return saveConfig(deps, cfg)
}

func selectWorkspaceSaveScope(deps *Deps) (string, string, error) {
	projectRoot, ok := config.FindProjectRoot(deps.WorkingDir)
	if !ok {
		return workspaceSaveScopeGlobal, "", nil
	}
	if !canPrompt(deps) {
		return workspaceSaveScopeGlobal, projectRoot, nil
	}
	scope, err := deps.Prompter.Select("Save workspace selection", []interactive.Option{
		{Label: "This project", Value: workspaceSaveScopeProject},
		{Label: "Global default", Value: workspaceSaveScopeGlobal},
	})
	if err != nil {
		return "", "", err
	}
	if scope == workspaceSaveScopeProject {
		return workspaceSaveScopeProject, projectRoot, nil
	}
	return workspaceSaveScopeGlobal, projectRoot, nil
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

func currentWorkspaceID(ctx context.Context, deps *Deps) (string, error) {
	return workspaceIDForSelection(ctx, deps, currentWorkspaceSelection(deps))
}

func workspaceIDForSelection(ctx context.Context, deps *Deps, selection config.ResolvedWorkspace) (string, error) {
	if selection.Workspace.ID != "" {
		return selection.Workspace.ID, nil
	}
	if selection.Workspace.Slug == "" {
		return "", nil
	}
	workspace, err := deps.Client.GetWorkspace(ctx, selection.Workspace.Slug)
	if err != nil {
		return "", err
	}
	return workspace.ID, nil
}

func currentActor(deps *Deps) string {
	cfg, _ := config.LoadFrom(deps.ConfigPath)
	if cfg.CurrentUser.Username != "" {
		return cfg.CurrentUser.Username
	}
	return cfg.CurrentUser.DisplayName
}

func annotateBodyWithCurrentSelection(ctx context.Context, deps *Deps, body map[string]any) error {
	if body == nil {
		return nil
	}
	cfg, _ := config.LoadFrom(deps.ConfigPath)
	if cfg.CurrentUser.Username != "" {
		if _, exists := body["created_by"]; !exists {
			body["created_by"] = cfg.CurrentUser.Username
		}
	}
	if _, exists := body["workspace_id"]; !exists {
		selection := resolveWorkspaceSelection(deps, cfg)
		workspaceID, err := workspaceIDForSelection(ctx, deps, selection)
		if err != nil {
			return err
		}
		if workspaceID != "" {
			body["workspace_id"] = workspaceID
		}
	}
	return nil
}

func requestContextForBody(ctx context.Context, body map[string]any) context.Context {
	workspaceID, _ := body["workspace_id"].(string)
	if workspaceID == "" {
		return ctx
	}
	return client.WithWorkspace(ctx, workspaceID)
}
