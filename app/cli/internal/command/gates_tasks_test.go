package command_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestGatesTasksPreviewCmd_CallsEndpoint(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.gatePreviewResult = map[string]any{
		"artifact_id":   "art-42",
		"preview_tasks": []any{},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "gates", "tasks", "preview", "art-42")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastGatePreviewID != "art-42" {
		t.Errorf("lastGatePreviewID = %q, want art-42", fc.lastGatePreviewID)
	}
	if !strings.Contains(out.String(), "art-42") {
		t.Errorf("expected stdout to contain artifact ID art-42, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "No expected gate tasks") {
		t.Errorf("expected 'No expected gate tasks' in output when preview_tasks is empty, got: %s", out.String())
	}
}

func TestGatesTasksPreviewCmd_WithTasks(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	deps.Client.(*fakeClient).gatePreviewResult = map[string]any{
		"artifact_id": "art-43",
		"preview_tasks": []any{
			map[string]any{
				"gate_key": "canonical_spec",
				"executor": "model_judge",
				"note":     "Verify spec completeness",
			},
			map[string]any{
				"gate_key": "rollback_plan",
				"executor": "model_judge",
				"note":     "Check rollback section",
			},
		},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "gates", "tasks", "preview", "art-43")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "canonical_spec") {
		t.Errorf("output missing canonical_spec:\n%s", got)
	}
	if !strings.Contains(got, "rollback_plan") {
		t.Errorf("output missing rollback_plan:\n%s", got)
	}
	if !strings.Contains(got, "model_judge") {
		t.Errorf("output missing executor:\n%s", got)
	}
}

func TestGatesTasksPreviewCmd_JSONOutput(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	deps.Client.(*fakeClient).gatePreviewResult = map[string]any{
		"artifact_id":   "art-44",
		"preview_tasks": []any{},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "gates", "tasks", "preview", "art-44")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "art-44") {
		t.Errorf("output missing artifact_id in JSON:\n%s", out.String())
	}
}

func TestGatesTasksDispatchCmd_CallsEndpoint(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
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
