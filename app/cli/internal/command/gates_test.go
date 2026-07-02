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
	command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "gates", "run", "CR-101") //nolint:errcheck
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
	for _, want := range []string{"run", "status", "history", "check", "tasks"} {
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
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
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
