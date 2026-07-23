package command_test

import (
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestDeliverySubmitRejectsUntouchedPendingCheckBeforeNetwork(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "implemented",
		"agent":      map[string]any{"name": "builder"},
		"checks": []map[string]any{{
			"name": "tests", "command": "", "status": "pending",
			"detail": "TODO: set command and observed status to pass, fail, or skipped",
		}},
		"criteria": []map[string]any{},
	})

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "delivery", "submit", "CR-101", "--file", f,
	)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("network calls = %d, want 0", fc.calls)
	}
	for _, want := range []string{"tests", "still pending", "pass, fail, or skipped"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("error missing %q: %s", want, out.String())
		}
	}
}
