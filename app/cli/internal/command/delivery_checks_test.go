package command_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestDeliverySubmitRunChecksRequiresYesOutsideInteractiveTerminal(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)
	executed := false
	deps.RunCheckCommand = func(_ context.Context, _ string) (int, string) {
		executed = true
		return 0, ""
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type":     "coding_agent.completed",
		"summary":        "done",
		"context_digest": work.ContextDigest,
		"checks":         []map[string]any{{"name": "tests", "command": "go test ./...", "status": "pass"}},
		"criteria":       []map[string]any{{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "verified"}}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", work.Key, "--file", f, "--run-checks")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if executed {
		t.Fatal("check or submission ran without explicit --yes")
	}
	if !strings.Contains(out.String(), "--yes") {
		t.Fatalf("error must explain automation confirmation: %s", out.String())
	}
}

func TestDeliverySubmitRunChecksNeverExecutesPaddedSkippedStatus(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)
	calls := 0
	deps.RunCheckCommand = func(context.Context, string) (int, string) {
		calls++
		return 0, ""
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type":     "coding_agent.completed",
		"summary":        "done",
		"context_digest": work.ContextDigest,
		"checks":         []map[string]any{{"name": "tests", "command": "go test ./...", "status": " skipped ", "detail": "manual check only"}},
		"criteria":       []map[string]any{{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "verified"}}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", work.Key, "--file", f, "--run-checks")
	if code != output.ExitOK || calls != 0 {
		t.Fatalf("exit = %d, ran %d skipped commands, output = %s", code, calls, out.String())
	}
}

func TestDeliverySubmitRunChecksShowsCommandsBeforeInteractiveConfirmation(t *testing.T) {
	deps, fp, out := newWorkflowTestDeps(t)
	deps.StdinIsTTY = func() bool { return true }
	fp.confirmValue = false
	var stderr bytes.Buffer
	deps.Stderr = &stderr
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)
	executed := false
	deps.RunCheckCommand = func(_ context.Context, _ string) (int, string) {
		executed = true
		return 0, ""
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type":     "coding_agent.completed",
		"summary":        "done",
		"context_digest": work.ContextDigest,
		"checks":         []map[string]any{{"name": "tests", "command": "go test ./...", "status": "pass"}},
		"criteria":       []map[string]any{{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "verified"}}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "delivery", "submit", work.Key, "--file", f, "--run-checks")
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

// TestDeliverySubmitRunChecksTruncatesOutputRuneSafe: detail truncation must
// not split a multibyte character.
func TestDeliverySubmitRunChecksTruncatesOutputRuneSafe(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)
	deps.RunCheckCommand = func(_ context.Context, _ string) (int, string) {
		return 0, strings.Repeat("é", 400)
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type":     "coding_agent.completed",
		"summary":        "done",
		"context_digest": work.ContextDigest,
		"checks":         []map[string]any{{"name": "echo unicode", "command": "echo unicode", "status": "pass"}},
		"criteria":       []map[string]any{{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "verified"}}},
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "delivery", "submit", work.Key, "--file", f, "--run-checks")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	readStore, err := local.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer readStore.Close()
	report, err := readStore.LatestDeliveryReport(t.Context(), work.WorkspaceID, work.Key)
	if err != nil {
		t.Fatal(err)
	}
	checks, _ := report.Body["checks"].([]any)
	detail := checks[0].(map[string]any)["detail"].(string)
	if !utf8.ValidString(detail) {
		t.Fatalf("detail is not valid UTF-8 after truncation: %q", detail)
	}
	if len([]rune(detail)) > 220 {
		t.Fatalf("detail not truncated: %d runes", len([]rune(detail)))
	}
}
