package command_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// --- gates run ---

func TestGatesRunWithYesFlag(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--yes", "gates", "run", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastGatesID != "cr-1" { // fakeClient.ResolveWorkRef returns "cr-1"
		t.Errorf("lastGatesID = %q, want cr-1", fc.lastGatesID)
	}
}

func TestGatesRunDeclinedNoConfirmation(t *testing.T) {
	t.Parallel()
	deps, fc, _, _ := newFakeDeps(t)
	deps.StdinIsTTY = func() bool { return true } // interactive terminal session
	// fakePrompter.confirmValue defaults to false → declined
	command.ExecuteForCode(command.NewRootCommand(deps), "gates", "run", "CR-101") //nolint:errcheck
	// RunLLMGates should NOT have been called
	if fc.lastGatesID != "" {
		t.Errorf("RunLLMGates was called despite declined confirmation")
	}
}

// TestGatesRunNonTTYProceedsWithoutConfirm verifies the confirm prompt is
// TTY-only: a non-interactive session (piped stdin) without --yes proceeds
// instead of prompting or erroring — the confirm is ceremony, not protection.
func TestGatesRunNonTTYProceedsWithoutConfirm(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t) // Stdin is a strings.Reader → not a TTY
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "gates", "run", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastGatesID != "cr-1" {
		t.Errorf("lastGatesID = %q, want cr-1 (gates should run without prompting)", fc.lastGatesID)
	}
}

func TestGatesRunJSONOutput(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "gates", "run", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.OK {
		t.Fatalf("ok = false: %s", out.String())
	}
}

// TestGatesRunJSONWithoutYesProceeds verifies `gates run --json` (which implies
// --no-input) without --yes proceeds and emits a success envelope: confirms are
// TTY-only ceremony, and --yes stays accepted for compatibility.
func TestGatesRunJSONWithoutYesProceeds(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "gates", "run", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("expected a JSON envelope, got unparseable output %q: %v", out.String(), err)
	}
	if !env.OK {
		t.Fatalf("ok = false: %s", out.String())
	}
	if fc.lastGatesID != "cr-1" {
		t.Errorf("lastGatesID = %q, want cr-1 (gates should run without --yes)", fc.lastGatesID)
	}
}

// --- gates check ---

func TestGatesCheck(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "gates", "check", "art-7")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastArtifactID != "art-7" {
		t.Errorf("lastArtifactID = %q, want art-7", fc.lastArtifactID)
	}
	if !strings.Contains(out.String(), "Spec quality: pass") {
		t.Errorf("output missing spec quality label:\n%s", out.String())
	}
}

func TestGatesCheckSummaryJSONOmitsHeavyEvidence(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.readinessResult = map[string]any{
		"artifact_id":        "art-7",
		"aggregate":          "warn",
		"governance_level":   "standard",
		"evaluations_posted": float64(2),
		"readiness_runs": []any{
			map[string]any{"gate": "spec_completeness", "state": "pass", "hint": "ok", "executor": "platform", "evidence_json": strings.Repeat("x", 4_000), "created_at": "2026-07-10T00:00:00Z"},
			map[string]any{"gate": "ac_verifiable", "state": "warn", "hint": "clarify rollback", "executor": "ide_agent"},
		},
		"dispatched_to_ide_agent": map[string]any{
			"artifact_id":      "art-7",
			"created_task_ids": []any{"task-1"},
		},
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "gates", "check", "art-7", "--summary")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if strings.Contains(out.String(), "evidence_json") || strings.Contains(out.String(), "created_at") {
		t.Fatalf("summary leaked detailed fields: %s", out.String())
	}
	if env.Data["artifact_id"] != "art-7" || env.Data["aggregate"] != "warn" {
		t.Fatalf("summary identity = %#v", env.Data)
	}
	gates, ok := env.Data["gates"].([]any)
	if !ok || len(gates) != 2 {
		t.Fatalf("summary gates = %#v", env.Data["gates"])
	}
	if executor := gates[1].(map[string]any)["executor"]; executor != "ide_agent" {
		t.Fatalf("summary gate executor = %#v, want ide_agent", executor)
	}
	dispatch, ok := env.Data["dispatched_to_ide_agent"].(map[string]any)
	if !ok || len(dispatch["created_task_ids"].([]any)) != 1 {
		t.Fatalf("summary dispatch = %#v", env.Data["dispatched_to_ide_agent"])
	}
}

func TestGatesCheckDefaultJSONKeepsDetailedEvidence(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.readinessResult = map[string]any{
		"artifact_id": "art-7",
		"aggregate":   "pass",
		"readiness_runs": []any{map[string]any{
			"gate": "spec_completeness", "state": "pass", "evidence_json": `{"scope":"complete"}`,
		}},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "gates", "check", "art-7")
	if code != output.ExitOK || !strings.Contains(out.String(), "evidence_json") {
		t.Fatalf("default JSON lost detailed evidence: exit=%d output=%s", code, out.String())
	}
}

func TestGatesResultsReturnsStoredDetailWithoutRunningChecks(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "gates", "results", "art-7")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.calls != 1 {
		t.Fatalf("client calls = %d, want one read", fc.calls)
	}
	for _, want := range []string{`"command":"gates.results"`, `"id":"run-1"`, `"evidence_json"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("detail output missing %s: %s", want, out.String())
		}
	}
}

func TestGatesResultsPlainHandlesMissingHint(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "gates", "results", "art-7")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if strings.Contains(out.String(), "%!") || !strings.Contains(out.String(), "spec_completeness\tpass") {
		t.Fatalf("bad plain result: %s", out.String())
	}
}

// TestGatesCheckShowsIDEAgentDispatch: when the server has no platform model
// it dispatches the gates as ide_agent tasks; the human output must say so and
// name the follow-up command instead of ending at "not_run".
func TestGatesCheckShowsIDEAgentDispatch(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.readinessResult = map[string]any{
		"aggregate": "not_run",
		"dispatched_to_ide_agent": map[string]any{
			"artifact_id":      "art-9",
			"created_task_ids": []any{"t-1", "t-2", "t-3"},
		},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "gates", "check", "art-9")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{"3 gate task", "IDE agent", "specgate gates tasks list art-9"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

// TestGatesCheckNoDispatchLineWhenModelRan: a platform-judged run must not
// print the IDE-agent dispatch hint.
func TestGatesCheckNoDispatchLineWhenModelRan(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.readinessResult = map[string]any{"aggregate": "warn"}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "gates", "check", "art-9")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if strings.Contains(out.String(), "IDE agent") {
		t.Fatalf("unexpected dispatch hint:\n%s", out.String())
	}
}

func TestGatesHelpShowsUnifiedFamily(t *testing.T) {
	t.Parallel()
	deps, _, _, _ := newFakeDeps(t)
	root := command.NewRootCommand(deps)
	gatesCmd, _, err := root.Find([]string{"gates"})
	if err != nil {
		t.Fatalf("find gates command: %v", err)
	}
	var subcommands []string
	for _, sub := range gatesCmd.Commands() {
		subcommands = append(subcommands, sub.Name())
	}
	joined := strings.Join(subcommands, ",")
	for _, want := range []string{"run", "status", "history", "check", "results", "tasks"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("gates subcommand %q not registered: %v", want, subcommands)
		}
	}
}

// --- gates status ---

func TestGatesStatusPlain(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.gatesStatusResult = &client.GatesStatusResult{
		ChangeRequestID: "cr-1",
		Gates: []client.GateSummary{
			{Gate: "canonical_spec", State: "pass", Hint: ""},
			{Gate: "rollback_plan", State: "fail", Hint: "Missing rollback section"},
		},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "gates", "status", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "canonical_spec") {
		t.Errorf("output missing canonical_spec:\n%s", got)
	}
	if !strings.Contains(got, "pass") {
		t.Errorf("output missing pass state:\n%s", got)
	}
	if !strings.Contains(got, "rollback_plan") {
		t.Errorf("output missing rollback_plan:\n%s", got)
	}
}

func TestGatesStatusHumanUsesProgressAndIcons(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	deps, fc, _, out := newFakeDeps(t)
	deps.StdoutIsTTY = func() bool { return true }
	fc.gatesStatusResult = &client.GatesStatusResult{
		ChangeRequestID: "cr-1",
		Gates: []client.GateSummary{
			{Gate: "canonical_spec", State: "pass", Hint: ""},
			{Gate: "rollback_plan", State: "fail", Hint: "Missing rollback section"},
		},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "gates", "status", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Quality Gates",
		"Progress:",
		"█",
		"✓",
		"✕",
		"canonical_spec",
		"Missing rollback section",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}

	out.Reset()
	deps.StdoutIsTTY = func() bool { return false }
	code = command.ExecuteForCode(command.NewRootCommand(deps), "gates", "status", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("portable exit = %d, output = %s", code, out.String())
	}
	if strings.Contains(out.String(), "\x1b[") || strings.ContainsAny(out.String(), "█✓✕") {
		t.Fatalf("portable gates status contains rich terminal output: %q", out.String())
	}
}

func TestGatesStatusJSON(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.gatesStatusResult = &client.GatesStatusResult{
		ChangeRequestID: "cr-1",
		Gates:           []client.GateSummary{{Gate: "canonical_spec", State: "pass"}},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "gates", "status", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.OK {
		t.Fatalf("ok = false: %s", out.String())
	}
}

// --- gates history ---

func TestGatesHistoryPlain(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.gateHistoryResult = &client.GateHistoryResult{
		ChangeRequestID: "cr-1",
		Runs: []client.GateRun{
			{GateRunID: "run-1", Gate: "canonical_spec", State: "pass", CreatedAt: "2026-06-20T10:00:00Z"},
			{GateRunID: "run-2", Gate: "rollback_plan", State: "fail", Hint: "Missing", CreatedAt: "2026-06-20T09:00:00Z"},
		},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "gates", "history", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "canonical_spec") {
		t.Errorf("output missing canonical_spec:\n%s", got)
	}
	if !strings.Contains(got, "run-1") {
		t.Errorf("output missing run ID:\n%s", got)
	}
}

func TestGatesHistoryJSON(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "gates", "history", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.OK {
		t.Fatalf("ok = false: %s", out.String())
	}
}

func TestGatesHistoryGateFilter(t *testing.T) {
	t.Parallel()
	deps, fc, _, _ := newFakeDeps(t)
	command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "gates", "history", "CR-101", "--gate", "canonical_spec") //nolint:errcheck
	if fc.lastGateFilter != "canonical_spec" {
		t.Fatalf("lastGateFilter = %q, want canonical_spec", fc.lastGateFilter)
	}
}

func TestGatesHistoryLimitFlag(t *testing.T) {
	t.Parallel()
	deps, fc, _, _ := newFakeDeps(t)
	command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "gates", "history", "CR-101", "--limit", "5") //nolint:errcheck
	if fc.lastGateLimit != 5 {
		t.Fatalf("lastGateLimit = %d, want 5", fc.lastGateLimit)
	}
}

func TestGateTaskShowJSONUsesStableEnvelope(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	setGateTaskWorkspace(t, deps)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "gates", "tasks", "show", "task-1")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		SchemaVersion string          `json:"schema_version"`
		Command       string          `json:"command"`
		OK            bool            `json:"ok"`
		Data          json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.SchemaVersion != "specgate.cli/v1" || env.Command != "gates.tasks.show" || !env.OK || len(env.Data) == 0 {
		t.Fatalf("unexpected envelope: %s", out.String())
	}
}
