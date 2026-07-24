package command_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestWorkspaceBoundaryRejectsGlobalFallbackInGitProject(t *testing.T) {
	t.Parallel()
	canonicalRepo, nested := commandGitRepo(t)
	deps, fc, _, out := newFakeDeps(t)
	deps.WorkingDir = nested
	if err := (config.Config{
		Mode:      config.ModeFull,
		Workspace: config.CurrentWorkspace{ID: "ws-global", Slug: "global"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "status")
	if code != output.ExitUnavailable {
		t.Fatalf("exit = %d, want unavailable; output = %s", code, out.String())
	}
	var envelope struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v; output = %s", err, out.String())
	}
	if envelope.Error.Code != "missing_workspace_binding" {
		t.Fatalf("error code = %q", envelope.Error.Code)
	}
	if envelope.Error.Details["project_root"] != canonicalRepo {
		t.Fatalf("project_root = %#v, want %q", envelope.Error.Details["project_root"], canonicalRepo)
	}
	if !strings.Contains(envelope.Error.Message, "specgate workspace bind") {
		t.Fatalf("error is not actionable: %s", envelope.Error.Message)
	}
	if fc.lastStatusWorkspaceID != "" {
		t.Fatalf("status API called with workspace %q", fc.lastStatusWorkspaceID)
	}
}

func TestWorkspaceBoundaryRejectsLocalFallbackBeforeOpeningStore(t *testing.T) {
	t.Parallel()
	_, nested := commandGitRepo(t)
	deps, _, _, out := newFakeDeps(t)
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

func TestWorkspaceBoundaryRejectsUnboundWorkspaceWrites(t *testing.T) {
	t.Parallel()
	for name, args := range map[string][]string{
		"quick work":       {"work", "create-quick", "Fix it", "--description", "Details", "--ac", "It works"},
		"artifact publish": {"artifact", "publish", "--file", "missing.json"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, nested := commandGitRepo(t)
			deps, _, _, out := newFakeDeps(t)
			deps.WorkingDir = nested
			if err := (config.Config{
				Mode:      config.ModeFull,
				Workspace: config.CurrentWorkspace{ID: "ws-global", Slug: "global"},
			}).SaveTo(deps.ConfigPath); err != nil {
				t.Fatal(err)
			}

			code := command.ExecuteForCode(command.NewRootCommand(deps), append([]string{"--json"}, args...)...)
			if code != output.ExitUnavailable {
				t.Fatalf("exit = %d, want unavailable; output = %s", code, out.String())
			}
			if !strings.Contains(out.String(), `"code":"missing_workspace_binding"`) {
				t.Fatalf("output = %s", out.String())
			}
		})
	}
}

func TestWorkspaceBoundaryAllowsExplicitOverrideInUnboundProject(t *testing.T) {
	t.Parallel()
	_, nested := commandGitRepo(t)
	deps, fc, _, out := newFakeDeps(t)
	deps.WorkingDir = nested
	if err := (config.Config{
		Mode:      config.ModeFull,
		Workspace: config.CurrentWorkspace{ID: "ws-global", Slug: "global"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--workspace", "platform", "status")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastStatusWorkspaceID != "platform" {
		t.Fatalf("status workspace = %q, want explicit override", fc.lastStatusWorkspaceID)
	}
}

func TestWorkspaceBoundaryAllowsRepoDefaultInUnboundProject(t *testing.T) {
	t.Parallel()
	canonicalRepo, nested := commandGitRepo(t)
	if err := os.MkdirAll(filepath.Join(canonicalRepo, ".specgate"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(canonicalRepo, ".specgate", "config"),
		[]byte(`{"workspace":"platform"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	deps, fc, _, out := newFakeDeps(t)
	deps.WorkingDir = nested
	if err := (config.Config{
		Mode:      config.ModeFull,
		Workspace: config.CurrentWorkspace{ID: "ws-global", Slug: "global"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "status")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastStatusWorkspaceID != "platform" {
		t.Fatalf("status workspace = %q, want repo default", fc.lastStatusWorkspaceID)
	}
}

func TestWorkspaceBoundaryAllowsGlobalDefaultOutsideGit(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	if err := (config.Config{
		Mode:      config.ModeFull,
		Workspace: config.CurrentWorkspace{ID: "ws-global", Slug: "global"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "status")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastStatusWorkspaceID != "ws-global" {
		t.Fatalf("status workspace = %q, want global default", fc.lastStatusWorkspaceID)
	}
}

func TestLocalWorkspaceUsesRepoDefault(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t, "")
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
	deps, out := newTestDeps(t, "")
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
