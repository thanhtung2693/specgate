package command

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
)

func openLocalStore(deps *Deps) (*local.Store, error) {
	path, err := localStatePath(deps)
	if err != nil {
		return nil, err
	}
	return local.Open(path)
}

func localStatePath(deps *Deps) (string, error) {
	cfg, _ := config.LoadFrom(deps.ConfigPath)
	project := config.ProjectConfig{}
	if root, ok := config.FindProjectRoot(deps.WorkingDir); ok {
		project = cfg.Projects[root]
	}
	dir := config.ResolveLocalDir("", os.Getenv("SPECGATE_LOCAL_DIR"), project, cfg, "")
	if dir == "" {
		return "", fmt.Errorf("local mode is not initialized; run `specgate init`")
	}
	return filepath.Join(dir, "state.db"), nil
}

// localSelection applies the documented one-command override and project
// binding without changing the persisted Local workspace selection.
func localSelection(ctx context.Context, deps *Deps, store *local.Store) (local.Selection, error) {
	selection, err := store.Current(ctx)
	if err != nil {
		return selection, err
	}
	cfg, _ := config.LoadFrom(deps.ConfigPath)
	resolved := resolveWorkspaceSelection(deps, cfg)
	if resolved.Source == config.WorkspaceSourceNone ||
		resolved.Source == config.WorkspaceSourceGlobal {
		return selection, nil
	}
	ref := strings.TrimSpace(resolved.Workspace.ID)
	if ref == "" {
		ref = strings.TrimSpace(resolved.Workspace.Slug)
	}
	workspace, err := store.Workspace(ctx, ref)
	if err != nil {
		return local.Selection{}, err
	}
	selection.Workspace = workspace
	return selection, nil
}
