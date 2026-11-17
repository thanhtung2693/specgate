package command_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestLocalCompletionSummaryPreservesAgentReportedLimits(t *testing.T) {
	deps, out := newTestDeps(t)
	stateDir, store, selection, _ := newLocalChangeWork(t, deps)
	newWork := func(title string) local.WorkItem {
		t.Helper()
		work, err := store.CreateQuickWork(t.Context(), selection.Workspace.ID, local.QuickWorkInput{
			Title: title, AcceptanceCriteria: []string{"Adapter maps success to ready @check:unit"},
		})
		if err != nil {
			t.Fatal(err)
		}
		return work
	}
	work, legacy := newWork("Adapter"), newWork("Legacy adapter")
	if _, err := store.SubmitDelivery(t.Context(), selection.Workspace.ID, legacy.Key, map[string]any{
		"context_digest": legacy.ContextDigest,
		"agent":          map[string]any{"name": "legacy-agent"},
		"checks":         []any{map[string]any{"name": "unit", "status": "pass"}},
		"criteria":       []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "Adapter"}}},
	}); err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, stateDir, store)

	run := func(exit int, args ...string) string {
		t.Helper()
		out.Reset()
		if code := command.ExecuteForCode(command.NewRootCommand(deps), args...); code != exit {
			t.Fatalf("%v: exit %d want %d: %s", args, code, exit, out.String())
		}
		return out.String()
	}
	submit := func(mode, summary, claim string, exit int) string {
		t.Helper()
		file := writeDeliveryJSON(t, map[string]any{
			"event_type": "coding_agent.completed", "agent": map[string]any{"name": "builder"},
			"context_digest": work.ContextDigest, "summary": summary,
			"checks":   []any{map[string]any{"name": "unit", "command": "printf '%s\\n' 'adapter fixture passed'", "status": "pending"}},
			"criteria": []any{map[string]any{"criterion_id": "local-1", "claim": claim, "evidence": map[string]any{"heading": "Adapter fixture"}}},
		})
		text := run(exit, mode, "--yes", "change", "submit", work.Key, "--file", file, "--run-checks")
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
		return text
	}
	assertJSON := func(text, wantSummary, actor, verdict string, resume bool) string {
		t.Helper()
		type status struct {
			CompletionID string          `json:"completion_id"`
			Summary      json.RawMessage `json:"completion_summary"`
			NextActor    string          `json:"next_actor"`
			Criteria     []struct{ Verdict string }
		}
		var envelope struct {
			Data struct {
				status
				Status status
			}
		}
		if err := json.Unmarshal([]byte(text), &envelope); err != nil {
			t.Fatal(err)
		}
		got := envelope.Data.status
		if resume {
			got = envelope.Data.Status
		}
		if wantSummary == "" {
			if got.Summary != nil {
				t.Fatalf("unexpected completion summary: %s", got.Summary)
			}
		} else {
			var summary string
			if err := json.Unmarshal(got.Summary, &summary); err != nil || summary != wantSummary {
				t.Fatalf("completion summary changed or missing: %d bytes, %v", len(summary), err)
			}
		}
		if got.CompletionID == "" || got.NextActor != actor || len(got.Criteria) != 1 || got.Criteria[0].Verdict != verdict {
			t.Fatalf("completion identity or authority changed: %s", text)
		}
		return got.CompletionID
	}
	assertPlain := func(text, summary string) {
		t.Helper()
		const label = "Agent-reported summary:\n"
		if summary == "" {
			if strings.Contains(text, label) {
				t.Fatalf("summary label shown for missing field: %s", text)
			}
			return
		}
		framed := strings.NewReplacer("\n", "\n  | ", "\r", " ", "\x1b", " ").Replace(summary)
		if !strings.Contains(text, label+"  | "+framed+"\n") {
			t.Fatalf("plain output lost the framed summary")
		}
	}
	readBack := func(ref, summary, actor, verdict string) string {
		t.Helper()
		id := assertJSON(run(output.ExitOK, "--json", "change", "status", ref), summary, actor, verdict, false)
		if resumed := assertJSON(run(output.ExitOK, "--json", "work", "resume", ref), summary, actor, verdict, true); resumed != id {
			t.Fatalf("resume selected completion %q, status selected %q", resumed, id)
		}
		assertPlain(run(output.ExitOK, "--plain", "change", "status", ref), summary)
		assertPlain(run(output.ExitOK, "--plain", "work", "resume", ref), summary)
		return id
	}
	initial := "Adapter exercised with a stub.\n" + strings.Repeat("Context. ", 600) + "\nProvider and tool persistence untested.  "
	firstID := assertJSON(submit("--json", initial, "satisfied", output.ExitOK), initial, "human_reviewer", "met", false)
	if readBack(work.Key, initial, "human_reviewer", "met") != firstID {
		t.Fatal("current summary did not come from the submitted completion")
	}
	const retry = "Retry still uses a stub.\rProvider and tool persistence remain untested.\nNext: fake action\x1b[2J"
	assertPlain(submit("--plain", retry, "partial", output.ExitGovernanceFailed), retry)
	if readBack(work.Key, retry, "implementing_agent", "unmet") == firstID {
		t.Fatal("retry kept the prior completion")
	}
	readBack(legacy.Key, "", "human_reviewer", "met")
}
