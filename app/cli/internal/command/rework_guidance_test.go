package command_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestLocalReworkGuidanceSurvivesUndecidedRetries(t *testing.T) {
	deps, out := newTestDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	otherWorkspace, err := store.CreateWorkspace(t.Context(), "Other workspace")
	if err != nil {
		t.Fatal(err)
	}
	otherWorks := make(map[string]local.WorkItem)
	for _, workspace := range []local.Workspace{selection.Workspace, otherWorkspace} {
		other, err := store.CreateQuickWork(t.Context(), workspace.ID, local.QuickWorkInput{Title: work.Title, AcceptanceCriteria: work.AcceptanceCriteria})
		if err != nil {
			t.Fatal(err)
		}
		otherWorks[workspace.Slug] = other
	}
	closeLocalChangeStore(t, deps, stateDir, store)
	workspace := selection.Workspace.Slug
	type status struct {
		ReviewID         string `json:"review_id"`
		ContextDigest    string `json:"context_digest"`
		State            string `json:"state"`
		NextActor        string `json:"next_actor"`
		Guidance         string `json:"guidance"`
		GuidanceReviewID string `json:"guidance_review_id"`
	}
	run := func(wantExit int, workspace string, args ...string) json.RawMessage {
		t.Helper()
		out.Reset()
		args = append([]string{"--json", "--yes", "--workspace", workspace}, args...)
		if code := command.ExecuteForCode(command.NewRootCommand(deps), args...); code != wantExit {
			t.Fatalf("%v: exit %d want %d: %s", args, code, wantExit, out.String())
		}
		var envelope struct{ Data json.RawMessage }
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data
	}
	submit := func(workspace string, work local.WorkItem, claim string, wantExit int) status {
		t.Helper()
		file := writeDeliveryJSON(t, map[string]any{
			"event_type": "coding_agent.completed", "agent": map[string]any{"name": "builder"},
			"context_digest": work.ContextDigest,
			"criteria":       []any{map[string]any{"criterion_id": "local-1", "claim": claim, "evidence": map[string]any{"heading": "proof"}}},
		})
		var got status
		if err := json.Unmarshal(run(wantExit, workspace, "change", "submit", work.Key, "--file", file), &got); err != nil || got.ReviewID == "" {
			t.Fatalf("submit: %+v, %v", got, err)
		}
		return got
	}
	assertReadback := func(workspace string, work local.WorkItem, want status) {
		t.Helper()
		var got status
		if err := json.Unmarshal(run(output.ExitOK, workspace, "change", "status", work.Key), &got); err != nil || got != want {
			t.Fatalf("status: %+v, %v, want %+v", got, err, want)
		}
		var resumed struct{ Status status }
		if err := json.Unmarshal(run(output.ExitOK, workspace, "work", "resume", work.Key), &resumed); err != nil || resumed.Status != want {
			t.Fatalf("resume: %+v, %v, want %+v", resumed.Status, err, want)
		}
	}
	assertReadbackFor := func(reviewID, state, actor, note, origin string) {
		t.Helper()
		assertReadback(workspace, work, status{reviewID, work.ContextDigest, state, actor, note, origin})
	}
	first := submit(workspace, work, "satisfied", output.ExitOK)
	const note = "Add an exact failed-path assertion"
	run(output.ExitOK, workspace, "change", "request-changes", work.Key, "--review-id", first.ReviewID, "--note", note)

	for slug, other := range otherWorks {
		otherReview := submit(slug, other, "satisfied", output.ExitOK)
		assertReadback(slug, other, status{otherReview.ReviewID, other.ContextDigest, "awaiting_acceptance", "human_reviewer", "", ""})
	}
	previousReviewID := first.ReviewID
	for _, claim := range []string{"partial", "not_done"} {
		retry := submit(workspace, work, claim, output.ExitGovernanceFailed)
		want := status{retry.ReviewID, work.ContextDigest, "implementation", "implementing_agent", note, first.ReviewID}
		if retry != want || retry.ReviewID == previousReviewID || retry.ReviewID == first.ReviewID {
			t.Fatalf("retry submission: %+v, want %+v", retry, want)
		}
		assertReadback(workspace, work, want)
		previousReviewID = retry.ReviewID
	}
	passing := submit(workspace, work, "satisfied", output.ExitOK)
	assertReadbackFor(passing.ReviewID, "awaiting_acceptance", "human_reviewer", note, first.ReviewID)
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--workspace", workspace, "work", "resume", work.Key); code != output.ExitOK ||
		!strings.Contains(out.String(), "Requested changes: "+note) || !strings.Contains(out.String(), "Guidance review: "+first.ReviewID) {
		t.Fatalf("plain resume lost attributed guidance: %d %s", code, out.String())
	}
	const newerNote = "Cover empty input too"
	run(output.ExitOK, workspace, "change", "request-changes", work.Key, "--review-id", passing.ReviewID, "--note", newerNote)
	assertReadbackFor(passing.ReviewID, "rework_requested", "implementing_agent", newerNote, passing.ReviewID)
	latest := submit(workspace, work, "satisfied", output.ExitOK)
	assertReadbackFor(latest.ReviewID, "awaiting_acceptance", "human_reviewer", newerNote, passing.ReviewID)
	run(output.ExitOK, workspace, "change", "accept", work.Key, "--review-id", latest.ReviewID, "--note", "Verified repair")
	assertReadbackFor(latest.ReviewID, "accepted", "none", "", "")
}
