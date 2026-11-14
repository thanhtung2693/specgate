package command_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestLocalInitBindsCurrentGitProject(t *testing.T) {
	t.Parallel()
	canonicalRepo, nested := commandGitRepo(t)
	deps, out := newTestDeps(t)
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

func commandGitRepo(t *testing.T) (string, string) {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	nested := filepath.Join(repo, "app")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	canonicalRepo, ok := config.FindProjectRoot(nested)
	if !ok {
		t.Fatal("project root not found")
	}
	return canonicalRepo, nested
}
