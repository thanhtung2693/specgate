package command_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestLocalInitBindsCurrentGitProject(t *testing.T) {
	t.Parallel()
	canonicalRepo, nested := commandGitRepo(t)
	deps, _, _, out := newFakeDeps(t)
	deps.WorkingDir = nested
	stateDir := filepath.Join(t.TempDir(), "local")

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "--no-input", "init", "--mode", "local",
		"--local-dir", stateDir,
		"--workspace-name", "Alpha",
		"--display-name", "Human",
		"--username", "human",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	cfg, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	binding, ok := cfg.Projects[canonicalRepo]
	if !ok {
		t.Fatalf("projects = %#v, want binding for %q", cfg.Projects, canonicalRepo)
	}
	if binding.Workspace.ID != cfg.Workspace.ID || binding.Workspace.Slug != "alpha" {
		t.Fatalf("binding = %#v, global = %#v", binding.Workspace, cfg.Workspace)
	}
	if binding.Local.Path != stateDir {
		t.Fatalf("binding local path = %q, want %q", binding.Local.Path, stateDir)
	}
	if _, err := os.Stat(filepath.Join(canonicalRepo, ".specgate", ".gitignore")); err != nil {
		t.Fatalf("project working directory not prepared: %v", err)
	}
}

func TestFullInitBindsCurrentGitProject(t *testing.T) {
	t.Parallel()
	canonicalRepo, nested := commandGitRepo(t)
	deployDir := t.TempDir()
	setupTestBundle(t, deployDir)
	deps, fc, _, out := newFakeDeps(t)
	deps.WorkingDir = nested
	deps.DeployRunner = &fakeDeployRunner{}
	fc.identitySelection = &client.IdentitySelection{
		User:      client.IdentityUser{ID: "user-1", Username: "human", DisplayName: "Human"},
		Workspace: client.IdentityWorkspace{ID: "ws-1", Slug: "alpha", Name: "Alpha"},
	}

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "--no-input", "init", "--mode", "full", "--dir", deployDir,
		"--workspace-name", "Alpha",
		"--display-name", "Human",
		"--username", "human",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	cfg, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	binding, ok := cfg.Projects[canonicalRepo]
	if !ok {
		t.Fatalf("projects = %#v, want binding for %q", cfg.Projects, canonicalRepo)
	}
	if binding.Workspace.ID != "ws-1" || binding.Workspace.Slug != "alpha" {
		t.Fatalf("binding = %#v", binding.Workspace)
	}
	if _, err := os.Stat(filepath.Join(canonicalRepo, ".specgate", ".gitignore")); err != nil {
		t.Fatalf("project working directory not prepared: %v", err)
	}
}
