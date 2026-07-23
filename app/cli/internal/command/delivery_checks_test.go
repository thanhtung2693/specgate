package command_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestDeliverySubmitSkipEvidenceCheckFlag(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	missing := filepath.Join(t.TempDir(), "gone.go")
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"criteria": []map[string]any{
			{
				"criterion_id": "ac-0",
				"claim":        "satisfied",
				"evidence":     map[string]any{"kind": "file", "path": missing},
			},
		},
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", "CR-101", "--file", f, "--skip-evidence-check")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.calls != 4 {
		t.Fatalf("calls = %d, want 4", fc.calls)
	}
}

func TestDeliverySubmitWarnsOnMissingAffectedFiles(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.deliveryStatusResult = &client.DeliveryStatusResult{ChangeRequestID: "cr-1", Found: true, Verdict: "pass"}
	missing := filepath.Join(t.TempDir(), "deleted_file.go")
	f := writeDeliveryJSON(t, map[string]any{
		"event_type":     "coding_agent.completed",
		"summary":        "done",
		"affected_files": []string{missing},
		"criteria":       []map[string]any{},
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "submit", "CR-101", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d (affected_files may legitimately include deleted files), output = %s", code, out.String())
	}
	if fc.calls != 4 {
		t.Fatalf("calls = %d, want 4", fc.calls)
	}
	got := out.String()
	if !strings.Contains(got, "deleted_file.go") || !strings.Contains(got, "affected_files") {
		t.Fatalf("expected a warning naming the missing affected file:\n%s", got)
	}
}

func TestDeliveryReportRejectsMissingEvidencePaths(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	missing := filepath.Join(t.TempDir(), "phantom_test.go")
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"criteria": []map[string]any{
			{
				"criterion_id": "ac-0",
				"claim":        "satisfied",
				"evidence":     map[string]any{"kind": "test", "path": missing},
			},
		},
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "report", "CR-101", "--file", f)
	if code == output.ExitOK {
		t.Fatalf("expected non-zero exit for missing evidence path; output = %s", out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("calls = %d, want 0 (must fail before any network call)", fc.calls)
	}
	if !strings.Contains(out.String(), "phantom_test.go") {
		t.Fatalf("error should name the missing path:\n%s", out.String())
	}
}

// --- executed checks (--run-checks) ---

func TestDeliverySubmitRunChecksRequiresYesOutsideInteractiveTerminal(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	executed := false
	deps.RunCheckCommand = func(_ context.Context, _ string) (int, string) {
		executed = true
		return 0, ""
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"checks":     []map[string]any{{"name": "tests", "command": "go test ./...", "status": "pass"}},
		"criteria":   []map[string]any{},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "submit", "CR-101", "--file", f, "--run-checks")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if executed || fc.lastFeedbackBody != nil {
		t.Fatal("check or submission ran without explicit --yes")
	}
	if !strings.Contains(out.String(), "--yes") {
		t.Fatalf("error must explain automation confirmation: %s", out.String())
	}
}

func TestDeliverySubmitRunChecksShowsCommandsBeforeInteractiveConfirmation(t *testing.T) {
	deps, _, fp, out := newFakeDeps(t)
	deps.StdinIsTTY = func() bool { return true }
	fp.confirmValue = false
	var stderr bytes.Buffer
	deps.Stderr = &stderr
	executed := false
	deps.RunCheckCommand = func(_ context.Context, _ string) (int, string) {
		executed = true
		return 0, ""
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"checks":     []map[string]any{{"name": "tests", "command": "go test ./...", "status": "pass"}},
		"criteria":   []map[string]any{},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "delivery", "submit", "CR-101", "--file", f, "--run-checks")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if executed {
		t.Fatal("check ran after user declined confirmation")
	}
	if !strings.Contains(stderr.String(), "go test ./...") || !strings.Contains(fp.confirmTitle, "Run 1") {
		t.Fatalf("command preview/confirmation missing: stderr=%q confirm=%q", stderr.String(), fp.confirmTitle)
	}
}

func TestDeliverySubmitRunChecksReplacesClaimedStatuses(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	var stderr bytes.Buffer
	deps.Stderr = &stderr
	deps.RunCheckCommand = func(_ context.Context, cmd string) (int, string) {
		if strings.Contains(cmd, "lint") {
			return 0, "lint clean"
		}
		return 1, "--- FAIL: TestX (0.01s)"
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"checks": []map[string]any{
			{"name": "go test ./...", "command": "go test ./...", "status": "pass"},
			{"name": "make lint", "status": "fail"},
			{"name": "manual smoke", "status": "skipped", "detail": "no device available"},
		},
		"criteria": []map[string]any{},
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--yes", "delivery", "submit", "CR-101", "--file", f, "--run-checks")
	if code != output.ExitOK {
		t.Fatalf("exit = %d (honest statuses are submitted, review decides), output = %s", code, out.String())
	}
	checks, _ := fc.lastFeedbackBody["checks"].([]any)
	if len(checks) != 3 {
		t.Fatalf("checks = %#v", fc.lastFeedbackBody["checks"])
	}
	c0 := checks[0].(map[string]any)
	if c0["status"] != "fail" || !strings.Contains(c0["detail"].(string), "exit 1") {
		t.Fatalf("claimed pass must be corrected to observed fail: %#v", c0)
	}
	if c0["source"] != "specgate_cli" {
		t.Fatalf("executed check must carry its observation source: %#v", c0)
	}
	c1 := checks[1].(map[string]any)
	if c1["status"] != "fail" {
		t.Fatalf("check without explicit command must be untouched: %#v", c1)
	}
	c2 := checks[2].(map[string]any)
	if c2["status"] != "skipped" || c2["detail"] != "no device available" {
		t.Fatalf("skipped check must be untouched: %#v", c2)
	}
	if strings.Contains(out.String(), "Executed check") || strings.Contains(out.String(), "1/4") {
		t.Fatalf("stdout contains progress output:\n%s", out.String())
	}
	if !strings.Contains(stderr.String(), "go test ./...") || !strings.Contains(stderr.String(), "1/4") {
		t.Fatalf("stderr should report the corrected check:\n%s", stderr.String())
	}
}

func TestDeliverySubmitRunChecksUsesCommandFieldAsExecutable(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	var executed []string
	deps.RunCheckCommand = func(_ context.Context, cmd string) (int, string) {
		executed = append(executed, cmd)
		return 0, "OK"
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"checks": []map[string]any{
			{"name": "tests", "command": "python3 -m unittest", "status": "pass"},
		},
		"criteria": []map[string]any{},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--yes", "delivery", "submit", "CR-101", "--file", f, "--run-checks")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if len(executed) != 1 || executed[0] != "python3 -m unittest" {
		t.Fatalf("executed = %#v, want python3 -m unittest", executed)
	}
	checks, _ := fc.lastFeedbackBody["checks"].([]any)
	c0 := checks[0].(map[string]any)
	if c0["name"] != "tests" || c0["status"] != "pass" {
		t.Fatalf("submitted check = %#v", c0)
	}
	if c0["command"] != "python3 -m unittest" {
		t.Fatalf("command not retained for independent review: %#v", c0)
	}
	if !strings.Contains(c0["detail"].(string), "exit 0") {
		t.Fatalf("detail missing observed execution: %#v", c0)
	}
}

func TestDeliverySubmitChecksNotExecutedWithoutFlag(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	executed := false
	deps.RunCheckCommand = func(_ context.Context, _ string) (int, string) {
		executed = true
		return 0, ""
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"checks":     []map[string]any{{"name": "go test ./...", "command": "go test ./...", "status": "pass"}},
		"criteria":   []map[string]any{},
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", "CR-101", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if executed {
		t.Fatal("checks were executed without --run-checks")
	}
	checks, _ := fc.lastFeedbackBody["checks"].([]any)
	if checks[0].(map[string]any)["status"] != "pass" {
		t.Fatalf("claimed status must be untouched without --run-checks: %#v", checks[0])
	}
}

// TestDeliverySubmitRunChecksAfterRefResolution: an invalid work ref must fail
// before any check command executes — checks can take minutes.
func TestDeliverySubmitRunChecksAfterRefResolution(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolveErr = errors.New("work item not found")
	executed := false
	deps.RunCheckCommand = func(_ context.Context, _ string) (int, string) {
		executed = true
		return 0, ""
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"checks":     []map[string]any{{"name": "go test ./...", "command": "go test ./...", "status": "pass"}},
		"criteria":   []map[string]any{},
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--yes", "delivery", "submit", "BAD-REF", "--file", f, "--run-checks")
	if code == output.ExitOK {
		t.Fatalf("expected non-zero exit for unresolved ref; output = %s", out.String())
	}
	if executed {
		t.Fatal("check commands ran before the work ref resolved")
	}
}

// TestDeliverySubmitRunChecksTruncatesOutputRuneSafe: detail truncation must
// not split a multibyte character.
func TestDeliverySubmitRunChecksTruncatesOutputRuneSafe(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.RunCheckCommand = func(_ context.Context, _ string) (int, string) {
		return 0, strings.Repeat("é", 400)
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"checks":     []map[string]any{{"name": "echo unicode", "command": "echo unicode", "status": "pass"}},
		"criteria":   []map[string]any{},
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "delivery", "submit", "CR-101", "--file", f, "--run-checks")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	checks, _ := fc.lastFeedbackBody["checks"].([]any)
	detail := checks[0].(map[string]any)["detail"].(string)
	if !utf8.ValidString(detail) {
		t.Fatalf("detail is not valid UTF-8 after truncation: %q", detail)
	}
	if len([]rune(detail)) > 220 {
		t.Fatalf("detail not truncated: %d runes", len([]rune(detail)))
	}
}

// --- agent-reported evidence visibility ---

func TestDeliveryStatusWarnsWhenAgentAttested(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		ChangeRequestID: "cr-1",
		Found:           true,
		Verdict:         "pass",
		JudgeModel:      "agent_attested",
		EvalSuite:       "delivery-review-v1",
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "status", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Agent-reported evidence") {
		t.Fatalf("plain output missing agent-reported evidence warning:\n%s", got)
	}
	if strings.Contains(got, "specgate model set") {
		t.Fatalf("warning must not imply model-backed review verifies the code:\n%s", got)
	}
}

func TestDeliveryStatusDashboardWarnsWhenAgentAttested(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	deps, fc, _, out := newFakeDeps(t)
	deps.StdoutIsTTY = func() bool { return true }
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		ChangeRequestID: "cr-1",
		Found:           true,
		Verdict:         "pass",
		JudgeModel:      "agent_attested",
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "delivery", "status", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "Agent-reported evidence") {
		t.Fatalf("dashboard output missing agent-reported evidence warning:\n%s", out.String())
	}
}

func TestDeliveryStatusNoAttestedWarningForModelJudge(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		ChangeRequestID: "cr-1",
		Found:           true,
		Verdict:         "pass",
		JudgeModel:      "governance-gate-judge",
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "status", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if strings.Contains(out.String(), "Agent-reported evidence") {
		t.Fatalf("model-judged verdict must not carry the agent-reported warning:\n%s", out.String())
	}
}

func TestDeliveryReportRejectsNullBodyFile(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	dir := t.TempDir()
	path := dir + "/completion.json"
	if err := os.WriteFile(path, []byte("null"), 0o644); err != nil {
		t.Fatal(err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "report", "CR-1", "--file", path)
	if code == output.ExitOK {
		t.Fatalf("null body accepted: %s", out.String())
	}
	if !strings.Contains(out.String(), "JSON object") {
		t.Fatalf("error should explain the expected shape: %s", out.String())
	}
}
