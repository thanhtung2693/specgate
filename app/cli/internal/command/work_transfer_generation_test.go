package command_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/local"
)

func TestReceivedWorkKeepsPickupAndReviewHistory(t *testing.T) {
	deps, out := newTestDeps(t)
	sourceDir, source, selection, work := newLocalChangeWork(t, deps)
	root := deps.WorkingDir
	if err := os.WriteFile(filepath.Join(root, "tracked"), []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "tracked"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-qm", "fixture"}} {
		if result, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, result)
		}
	}
	pending, err := source.CreateQuickWork(t.Context(), selection.Workspace.ID, local.QuickWorkInput{Title: "Pending review", AcceptanceCriteria: []string{"Keep pending"}})
	if err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, sourceDir, source)
	type state struct {
		Work       local.WorkItem   `json:"work"`
		Checkpoint local.Checkpoint `json:"checkpoint"`
		Delta      struct {
			State string `json:"state"`
			Limit string `json:"limit"`
		} `json:"delta"`
	}
	type status struct {
		State      string                 `json:"state"`
		Freshness  string                 `json:"freshness"`
		Historical bool                   `json:"historical_verification"`
		Assurance  string                 `json:"assurance"`
		Actor      string                 `json:"next_actor"`
		Next       string                 `json:"next_command"`
		Basis      string                 `json:"basis_digest"`
		Review     string                 `json:"review_id"`
		Completion string                 `json:"completion_id"`
		Recorded   *local.AcceptanceBasis `json:"recorded_acceptance_basis"`
	}
	run := func(want int, data any, args ...string) string {
		t.Helper()
		out.Reset()
		if code := command.ExecuteForCode(command.NewRootCommand(deps), args...); code != want {
			t.Fatalf("%v: got exit %d, want %d: %s", args, code, want, out.String())
		}
		if data != nil {
			var envelope struct {
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(envelope.Data, data); err != nil {
				t.Fatal(err)
			}
		}
		return out.String()
	}
	deps.RunCheckCommand = func(context.Context, string) (int, string) { return 0, "source test output" }
	for _, item := range []local.WorkItem{work, pending} {
		report := writeDeliveryJSON(t, map[string]any{
			"event_type": "coding_agent.completed", "agent": map[string]any{"name": "builder"},
			"context_digest": item.ContextDigest,
			"checks":         []any{map[string]any{"name": "unit", "command": "exit 99", "status": "pass"}},
			"criteria":       []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "Fixture evidence"}}},
		})
		run(0, nil, "--json", "--yes", "change", "submit", item.Key, "--file", report, "--run-checks")
	}
	var pickup local.Checkpoint
	run(0, &pickup, "--json", "--yes", "work", "checkpoint", work.Key, "--note", "Continue with receipt review")
	var before state
	run(0, &before, "--json", "work", "resume", work.Key)
	if before.Checkpoint.ID != pickup.ID || before.Delta.State != "unchanged" {
		t.Fatalf("legacy checkpoint no longer compares: %+v", before)
	}
	var accepted status
	run(0, &accepted, "--json", "change", "status", work.Key, "--checkpoint", pickup.ID)
	run(0, nil, "--json", "--yes", "change", "accept", work.Key, "--review-id", accepted.Review, "--basis-digest", accepted.Basis, "--checkpoint", pickup.ID, "--note", "Source decision")
	run(0, &accepted, "--json", "change", "status", work.Key)
	if accepted.Recorded == nil {
		t.Fatal("missing source recorded acceptance")
	}
	var sourcePending status
	run(0, &sourcePending, "--json", "change", "status", pending.Key)
	if sourcePending.Basis != "" || sourcePending.Actor != "human_reviewer" {
		t.Fatalf("want legacy review-ID-only source baseline: %+v", sourcePending)
	}

	sourceBytes, err := os.ReadFile(filepath.Join(sourceDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	receivedDir := t.TempDir()
	receivedPath := filepath.Join(receivedDir, "state.db")
	if err := os.WriteFile(receivedPath, sourceBytes, 0600); err != nil {
		t.Fatal(err)
	}
	received, err := local.Open(receivedPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := received.InitializeTransferGeneration(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := received.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPECGATE_LOCAL_DIR", receivedDir)
	deps.RunCheckCommand = func(_ context.Context, _ string) (int, string) {
		t.Fatal("pickup or review ran a check")
		return 0, ""
	}
	var defaultResume, explicit state
	run(0, &defaultResume, "--json", "work", "resume", work.Key)
	if defaultResume.Checkpoint.ID != "" || defaultResume.Delta.State != "no_baseline" {
		t.Fatalf("default resume chose a source observation: %+v", defaultResume)
	}
	run(0, &explicit, "--json", "work", "resume", work.Key, "--since", pickup.ID)
	if explicit.Checkpoint.ID != pickup.ID || explicit.Checkpoint.Note != pickup.Note || explicit.Work.ContextDigest != work.ContextDigest ||
		explicit.Delta.State != "noncomparable" || !strings.Contains(explicit.Delta.Limit, "generation") {
		t.Fatalf("historical resume lost scope, note, or comparison limit: %+v", explicit)
	}
	plain := run(0, nil, "--plain", "work", "resume", work.Key, "--since", pickup.ID)
	if !strings.Contains(plain, pickup.Note) {
		t.Fatalf("plain pickup lost note: %s", plain)
	}
	run(2, nil, "--json", "change", "status", work.Key, "--checkpoint", pickup.ID)
	var receivedAccepted status
	run(0, &receivedAccepted, "--json", "change", "status", work.Key)
	if accepted.State != receivedAccepted.State || accepted.Review != receivedAccepted.Review || accepted.Completion != receivedAccepted.Completion ||
		!reflect.DeepEqual(accepted.Recorded, receivedAccepted.Recorded) || !receivedAccepted.Historical || !strings.Contains(receivedAccepted.Freshness, "generation") ||
		!strings.Contains(receivedAccepted.Assurance, "reproduced by the source SpecGate CLI") {
		t.Fatalf("recorded acceptance changed: before %+v after %+v", accepted, receivedAccepted)
	}
	var receivedPending status
	run(0, &receivedPending, "--json", "change", "status", pending.Key)
	if receivedPending.Basis == "" || receivedPending.Actor != sourcePending.Actor || receivedPending.State != sourcePending.State ||
		receivedPending.Review != sourcePending.Review || receivedPending.Completion != sourcePending.Completion ||
		!strings.Contains(receivedPending.Next, receivedPending.Basis) || !strings.Contains(receivedPending.Freshness, "generation") {
		t.Fatalf("pending review changed or has no receiving basis: %+v", receivedPending)
	}
	run(4, nil, "--json", "--yes", "change", "accept", pending.Key, "--review-id", sourcePending.Review)
	var pendingResume struct {
		Status status `json:"status"`
	}
	run(0, &pendingResume, "--json", "work", "resume", pending.Key)
	if pendingResume.Status.Basis != receivedPending.Basis || pendingResume.Status.Next != receivedPending.Next {
		t.Fatalf("pending resume diverged from status: %+v", pendingResume.Status)
	}
	var newPickup local.Checkpoint
	run(0, &newPickup, "--json", "--yes", "work", "checkpoint", pending.Key, "--note", "Receiving observation")
	run(0, &defaultResume, "--json", "work", "resume", pending.Key)
	if defaultResume.Checkpoint.ID != newPickup.ID || defaultResume.Delta.State != "unchanged" {
		t.Fatalf("receiving observation unavailable: %+v", defaultResume)
	}
	run(0, &receivedPending, "--json", "change", "status", pending.Key, "--checkpoint", newPickup.ID)
	if receivedPending.Actor != "human_reviewer" || receivedPending.Recorded != nil {
		t.Fatalf("pickup or review decided pending work: %+v", receivedPending)
	}

	otherRoot := t.TempDir()
	if result, err := exec.Command("git", "clone", "-q", root, otherRoot).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v %s", err, result)
	}
	deps.WorkingDir = otherRoot
	var other state
	run(0, &other, "--json", "work", "resume", pending.Key, "--since", newPickup.ID)
	if other.Checkpoint.ID != newPickup.ID || other.Delta.State != "noncomparable" ||
		!strings.Contains(other.Delta.Limit, "checkout identities") {
		t.Fatalf("different path was treated as the same receiving checkout: %+v", other)
	}
}
