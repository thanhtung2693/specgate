package command_test

import (
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestDemoRemoveRunsRemovalCommand(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--yes", "demo", "remove")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !fc.removeDemoCalled {
		t.Fatal("expected RemoveDemo to be called")
	}
	if !strings.Contains(out.String(), "Demo data removed.") {
		t.Fatalf("output = %q, want removal confirmation", out.String())
	}
}

func TestDemoRemoveCancelledPrompt(t *testing.T) {
	t.Parallel()
	deps, fc, fp, out := newFakeDeps(t)
	deps.StdinIsTTY = func() bool { return true }
	fp.confirmValue = false
	code := command.ExecuteForCode(command.NewRootCommand(deps), "demo", "remove")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.removeDemoCalled {
		t.Fatal("removal must not run after cancelled prompt")
	}
	if !strings.Contains(fp.confirmTitle, "DEMO-* keys plus demo-* internal rows") {
		t.Fatalf("confirm prompt = %q, want fixed demo identifier wording", fp.confirmTitle)
	}
}
