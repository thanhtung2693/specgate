package command_test

import (
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// Local review counted any non-empty evidence key as evidence while the Full
// server rejects unknown properties, so a completion that passed Local review
// was refused by the appliance. The CLI validates the same evidence contract in
// both modes, before any network call.
func TestCompletionRejectsUnknownEvidenceFieldInLocalMode(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	_, work := handoffWork(t, deps, "Retries stop", nil)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type":     "coding_agent.completed",
		"summary":        "done",
		"context_digest": work.ContextDigest,
		"checks":         []map[string]any{{"name": "unit", "command": "true", "status": "pass"}},
		"criteria": []map[string]any{{
			"criterion_id": "local-1", "claim": "satisfied",
			// `summary` is not part of the evidence contract; the appliance
			// answers "unexpected property" for exactly this payload.
			"evidence": map[string]any{"kind": "file", "path": "retry.md", "summary": "capped"},
		}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "submit", work.Key, "--file", f)
	if code == output.ExitOK {
		t.Fatalf("Local accepted evidence the appliance rejects:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "summary") {
		t.Fatalf("error does not name the offending evidence field:\n%s", out.String())
	}
}

func TestCompletionAcceptsEveryDocumentedEvidenceField(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	_, work := handoffWork(t, deps, "Retries stop", nil)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type":     "coding_agent.completed",
		"summary":        "done",
		"context_digest": work.ContextDigest,
		"checks":         []map[string]any{{"name": "unit", "command": "true", "status": "pass"}},
		"criteria": []map[string]any{{
			"criterion_id": "local-1", "claim": "satisfied",
			"evidence": map[string]any{
				"kind": "file", "path": "retry.md", "line": 1,
				"heading": "Retries", "url": "https://example.com/pr/1", "file_key": "gov-1",
			},
		}},
	})

	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "submit", work.Key, "--file", f, "--skip-evidence-check"); code != output.ExitOK {
		t.Fatalf("documented evidence fields were rejected: exit %d\n%s", code, out.String())
	}
}
