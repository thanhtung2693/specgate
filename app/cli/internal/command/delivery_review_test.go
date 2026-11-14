package command_test

import (
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func writeDeliveryJSON(t *testing.T, data any) string {
	t.Helper()
	if body, ok := data.(map[string]any); ok && body["event_type"] == "coding_agent.completed" {
		if _, exists := body["agent"]; !exists {
			body["agent"] = map[string]any{"name": "builder"}
		}
	}
	return writeTempJSON(t, data)
}

func TestLocalDeliveryReportFromFileReturnsActionableError(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type":     "coding_agent.completed",
		"summary":        "done",
		"context_digest": work.ContextDigest,
		"agent":          map[string]any{"name": "Codex"},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "report", work.Key, "--file", f)
	if code != output.ExitIncompatible {
		t.Fatalf("exit = %d, want incompatible; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "specgate change submit") || !strings.Contains(out.String(), "--file") {
		t.Fatalf("error does not route Local completion to change submit: %s", out.String())
	}
}

func TestDeliveryPeerReviewRejectsMissingEvidenceBeforeNetworkCall(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	deps.WorkingDir = t.TempDir()
	path := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.peer_reviewed",
		"agent":      map[string]any{"name": "reviewer"},
		"criteria": []any{map[string]any{
			"criterion_id": "ac-1",
			"claim":        "satisfied",
			"evidence":     map[string]any{"kind": "test", "path": "missing-proof.txt", "line": 1},
		}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "peer-review", "CR-101", "--file", path)
	if code == output.ExitOK {
		t.Fatalf("missing peer evidence was accepted: %s", out.String())
	}
	if !strings.Contains(out.String(), "missing-proof.txt") {
		t.Fatalf("error does not identify missing evidence: %s", out.String())
	}
}

func deliveryGitOutput(dir string, args ...string) string {
	return strings.Join(append([]string{"git", "-C", dir}, args...), " ")
}

func deliveryGitRunner(dir string, status []byte) *fakeCommandRunner {
	return &fakeCommandRunner{OutputByCommand: map[string][]byte{
		deliveryGitOutput(dir, "rev-parse", "--show-toplevel"):                            []byte(dir + "\n"),
		deliveryGitOutput(dir, "remote", "get-url", "origin"):                             []byte("https://github.com/acme/project.git\n"),
		deliveryGitOutput(dir, "branch", "--show-current"):                                []byte("main\n"),
		deliveryGitOutput(dir, "rev-parse", "HEAD"):                                       []byte("head-revision\n"),
		deliveryGitOutput(dir, "merge-base", "HEAD", "origin/main"):                       []byte("base-revision\n"),
		deliveryGitOutput(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all"): status,
	}}
}
