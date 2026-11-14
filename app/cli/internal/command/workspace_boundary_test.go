package command_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestWorkspaceBoundaryRejectsLocalFallbackBeforeOpeningStore(t *testing.T) {
	t.Parallel()
	_, nested := commandGitRepo(t)
	deps, out := newTestDeps(t)
	deps.Stderr = out
	deps.WorkingDir = nested
	stateDir := filepath.Join(t.TempDir(), "local")
	if err := (config.Config{
		Mode:      config.ModeLocal,
		Local:     config.LocalStore{Path: stateDir, ID: "store-1"},
		Workspace: config.CurrentWorkspace{ID: "ws-global", Slug: "global"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "status")
	if code != output.ExitUnavailable {
		t.Fatalf("exit = %d, want unavailable; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "missing_workspace_binding") ||
		!strings.Contains(out.String(), "specgate workspace bind") {
		t.Fatalf("output is not actionable: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(stateDir, "state.db")); !os.IsNotExist(err) {
		t.Fatalf("Local store was opened before the boundary check: %v", err)
	}
}

func TestLocalWorkspaceUsesRepoDefault(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--plain", "--no-input", "init", "--mode", "local",
		"--local-dir", stateDir,
		"--workspace-name", "Alpha",
		"--display-name", "Human",
		"--username", "human",
	); code != output.ExitOK {
		t.Fatalf("init exit = %d, output = %s", code, out.String())
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "create", "Beta"); code != output.ExitOK {
		t.Fatalf("create exit = %d, output = %s", code, out.String())
	}

	canonicalRepo, nested := commandGitRepo(t)
	if err := os.MkdirAll(filepath.Join(canonicalRepo, ".specgate"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(canonicalRepo, ".specgate", "config"),
		[]byte(`{"workspace":"beta"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	deps.WorkingDir = nested
	out.Reset()

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "status")
	if code != output.ExitOK {
		t.Fatalf("status exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), `"slug":"beta"`) {
		t.Fatalf("Local status ignored repo workspace default: %s", out.String())
	}
}

func TestLocalWorkspaceCurrentExplainsUnboundProject(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--plain", "--no-input", "init", "--mode", "local",
		"--local-dir", stateDir,
		"--workspace-name", "Alpha",
		"--display-name", "Human",
		"--username", "human",
	); code != output.ExitOK {
		t.Fatalf("init exit = %d, output = %s", code, out.String())
	}
	canonicalRepo, nested := commandGitRepo(t)
	deps.WorkingDir = nested
	out.Reset()

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "current")
	if code != output.ExitOK {
		t.Fatalf("current exit = %d, output = %s", code, out.String())
	}
	for _, want := range []string{
		"scope: global default",
		"project: " + canonicalRepo + " (not bound)",
		"specgate workspace bind",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}
