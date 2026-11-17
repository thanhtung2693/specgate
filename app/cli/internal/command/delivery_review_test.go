package command_test

import (
	"encoding/json"
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

func TestDeliveryPeerReviewDiscardsCopiedGrounding(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "peer-grounding-head")
	reportID := latestLocalReportID(t, store, selection.Workspace.ID, work.Key)
	closeLocalChangeStore(t, deps, stateDir, store)

	var reviewID string
	for _, tc := range []struct {
		digest, heading string
		exit            int
	}{
		{"sha256:forged-only", "", output.ExitUsage},
		{"sha256:first", "Independent review note", output.ExitOK},
		{"sha256:second", "Independent review note", output.ExitOK},
	} {
		evidence := map[string]any{"grounding": map[string]any{"status": "grounded", "digest": tc.digest}}
		if tc.heading != "" {
			evidence["heading"] = tc.heading
		}
		f := writeDeliveryJSON(t, map[string]any{
			"event_type": "coding_agent.peer_reviewed",
			"agent":      map[string]any{"name": "reviewer"},
			"peer_review_of": map[string]any{
				"completion_feedback_event_id": reportID, "git_receipt": map[string]any{"head_revision": "peer-grounding-head"},
			},
			"criteria": []map[string]any{{"criterion_id": "local-1", "claim": "satisfied", "evidence": evidence}},
		})
		out.Reset()
		if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "peer-review", work.Key, "--file", f); code != tc.exit {
			t.Errorf("exit = %d, want %d: %s", code, tc.exit, out.String())
			continue
		}
		if tc.exit != output.ExitOK {
			if !strings.Contains(out.String(), "claims satisfied but has no evidence") {
				t.Errorf("expected evidence validation: %s", out.String())
			}
			continue
		}
		var response struct {
			Data struct{ ID string } `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Data.ID == "" || reviewID != "" && response.Data.ID != reviewID {
			t.Errorf("a changed copied digest created a different review: previous ID %q, current ID %q", reviewID, response.Data.ID)
		}
		reviewID = response.Data.ID
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
