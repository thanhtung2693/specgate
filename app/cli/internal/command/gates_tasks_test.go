package command_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func setGateTaskWorkspace(t *testing.T, deps *command.Deps) {
	t.Helper()
	if err := (config.Config{Workspace: config.CurrentWorkspace{ID: "workspace-gates"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
}

func TestGatesTasksPreviewCommandIsRemoved(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "gates", "tasks", "preview", "art-44")
	if code == output.ExitOK {
		t.Fatalf("removed preview command still succeeds: %s", out.String())
	}
}

func TestGatesTasksDispatchCmd_CallsEndpoint(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	setGateTaskWorkspace(t, deps)
	fc.dispatchGateTasksResult = &client.DispatchGateTasksResult{
		ArtifactID:      "art-42",
		CreatedTaskIDs:  []string{"t1", "t2"},
		SkippedGateKeys: []string{},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "gates", "tasks", "dispatch", "art-42")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastDispatchGateTasksID != "art-42" {
		t.Errorf("lastDispatchGateTasksID = %q, want art-42", fc.lastDispatchGateTasksID)
	}
	if !strings.Contains(out.String(), "Dispatched 2 gate task(s)") {
		t.Errorf("unexpected output: %s", out.String())
	}
	if !strings.Contains(out.String(), "art-42") {
		t.Errorf("output should contain artifact id, got: %s", out.String())
	}
}

func TestGatesTasksSubmitResultCmd(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	setGateTaskWorkspace(t, deps)
	file := filepath.Join(t.TempDir(), "result.json")
	if err := os.WriteFile(file, []byte(`{"state":"pass"}`), 0o600); err != nil {
		t.Fatalf("write result file: %v", err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "gates", "tasks", "submit-result", "task-1", "--file", file)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "submitted") {
		t.Errorf("expected submission confirmation, got: %s", out.String())
	}
}
