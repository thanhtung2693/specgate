package command_test

import (
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// A human accepting or rejecting a delivery decides from `change status`, and the
// delivery skill forbids re-reading the completion file. So the per-criterion
// verdict — and the check that decided it — has to reach that payload, or the
// human approves without knowing which criterion is weak.

func TestChangeStatusExposesPerCriterionVerdictAndDecidingCheck(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	// A bound criterion, so the verdict is decided by a named check rather than
	// by the agent's claim.
	_, work := handoffWork(t, deps, "Retries stop @check:unit", nil)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type":     "coding_agent.completed",
		"summary":        "done",
		"context_digest": work.ContextDigest,
		"checks": []map[string]any{{
			"name": "unit", "command": "go test ./...", "status": "skipped", "detail": "no runner",
		}},
		"criteria": []map[string]any{{
			"criterion_id": "local-1", "claim": "satisfied",
			"evidence": map[string]any{"heading": "checked by hand"},
		}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "submit", work.Key, "--file", f)
	if code != output.ExitGovernanceFailed {
		t.Fatalf("submit exit = %d, want governance failure; output = %s", code, out.String())
	}
	out.Reset()

	code = command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "status", work.Key)
	if code != output.ExitOK {
		t.Fatalf("status exit = %d, output = %s", code, out.String())
	}
	payload := out.String()
	for _, want := range []string{`"criteria"`, `"verdict"`, `"why"`} {
		if !strings.Contains(payload, want) {
			t.Fatalf("change status payload missing %q:\n%s", want, payload)
		}
	}
	// The deciding check must be nameable by the human, not just the criterion.
	if !strings.Contains(payload, "unit") {
		t.Fatalf("per-criterion detail does not name the deciding check:\n%s", payload)
	}
}

// The human-readable receipt must show the same per-criterion outcome, since
// that is what the delivery skill renders verbatim.
func TestChangeStatusHumanOutputListsCriteriaVerdicts(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type":     "coding_agent.completed",
		"summary":        "done",
		"context_digest": work.ContextDigest,
		"checks": []map[string]any{{
			"name": "tests", "command": "go test ./...", "status": "pass",
		}},
		"criteria": []map[string]any{{
			"criterion_id": "local-1", "claim": "satisfied",
			"evidence": map[string]any{"heading": "verified"},
		}},
	})
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "submit", work.Key, "--file", f); code != output.ExitOK {
		t.Fatalf("submit exit = %d, output = %s", code, out.String())
	}
	out.Reset()

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "change", "status", work.Key)
	if code != output.ExitOK {
		t.Fatalf("status exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "Criteria:") {
		t.Fatalf("human receipt omits the per-criterion block:\n%s", out.String())
	}
}
