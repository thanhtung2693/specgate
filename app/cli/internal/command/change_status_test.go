package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestChangeApproveRequiresExplicitWorkContractBeforeMutation(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "approve", "artifact-1")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	for _, want := range []string{"--title", "--ac"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing actionable flag %q: %s", want, out.String())
		}
	}
}

func TestChangeSubmitRunChecksMarksLocalAssuranceAsReproduced(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)
	deps.RunCheckCommand = func(_ context.Context, command string) (int, string) {
		if command != "go test ./..." {
			t.Fatalf("executed command = %q", command)
		}
		return 0, "ok"
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type":     "coding_agent.completed",
		"summary":        "done",
		"context_digest": work.ContextDigest,
		"checks": []map[string]any{{
			"name": "tests", "command": "go test ./...", "status": "pass",
		}},
		"criteria": []map[string]any{{
			"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "verified"},
		}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "submit", work.Key, "--file", f, "--run-checks")
	if code != output.ExitOK {
		t.Fatalf("submit exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), `"assurance":"Agent-reported; locally reproduced"`) {
		t.Fatalf("submit assurance did not expose local reproduction: output=%s", out.String())
	}
	for _, field := range []string{`"context_digest":"` + work.ContextDigest + `"`, `"completion_id":"`, `"criterion_evidence":`, `"risks":`} {
		if !strings.Contains(out.String(), field) {
			t.Fatalf("submit omitted status material %s: %s", field, out.String())
		}
	}
	out.Reset()
	got := runChangeStatusJSON(t, deps, out, work.Key)
	if got.Assurance != "Agent-reported; locally reproduced" {
		t.Fatalf("stored assurance = %q", got.Assurance)
	}
}

// A claimed check status that re-execution contradicts must survive in the
// stored report so the delivery receipt reports what SpecGate corrected.
func TestChangeSubmitRunChecksRecordsCorrectedClaimInAssurance(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)
	deps.RunCheckCommand = func(_ context.Context, _ string) (int, string) {
		return 1, "FAIL: TestRetryStops"
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type":     "coding_agent.completed",
		"summary":        "done",
		"context_digest": work.ContextDigest,
		"checks": []map[string]any{{
			"name": "tests", "command": "go test ./...", "status": "pass",
		}},
		"criteria": []map[string]any{{
			"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "verified"},
		}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "submit", work.Key, "--file", f, "--run-checks")
	if code != output.ExitGovernanceFailed {
		t.Fatalf("submit exit = %d, want governance failure; output = %s", code, out.String())
	}
	out.Reset()
	got := runChangeStatusJSON(t, deps, out, work.Key)
	if !strings.Contains(got.Assurance, "corrected 1 agent-reported check result") {
		t.Fatalf("assurance = %q, want the corrected claim reported", got.Assurance)
	}
	if !strings.Contains(got.Assurance, "locally reproduced") {
		t.Fatalf("assurance = %q, want local reproduction retained", got.Assurance)
	}
}

type changeStatusData struct {
	Mode        string   `json:"mode"`
	Ref         string   `json:"ref"`
	Title       string   `json:"title"`
	State       string   `json:"state"`
	Evidence    string   `json:"evidence"`
	Assurance   string   `json:"assurance"`
	Decision    string   `json:"decision"`
	Receipt     string   `json:"receipt"`
	Freshness   string   `json:"freshness"`
	NextActor   string   `json:"next_actor"`
	Missing     []string `json:"missing"`
	Guidance    string   `json:"guidance"`
	Stale       bool     `json:"stale"`
	StaleReason string   `json:"stale_reason"`
	NextCommand string   `json:"next_command"`
}

type changeStatusEnvelope struct {
	SchemaVersion string           `json:"schema_version"`
	Command       string           `json:"command"`
	OK            bool             `json:"ok"`
	Data          changeStatusData `json:"data"`
}

func runChangeStatusJSON(t *testing.T, deps *command.Deps, out *bytes.Buffer, ref string) changeStatusData {
	t.Helper()
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "status", ref)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var envelope changeStatusEnvelope
	if err := json.NewDecoder(out).Decode(&envelope); err != nil {
		t.Fatalf("decode status output: %v", err)
	}
	if envelope.SchemaVersion != "specgate.cli/v1" || envelope.Command != "change.status" || !envelope.OK {
		t.Fatalf("unexpected success envelope: %#v", envelope)
	}
	return envelope.Data
}

func TestChangeStatusLocalWithoutReportShowsImplementationScaffold(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)

	got := runChangeStatusJSON(t, deps, out, work.Key)
	if got.State != "implementation" || got.NextActor != "implementing_agent" {
		t.Fatalf("status = %#v", got)
	}
	want := "specgate delivery report " + work.Key + " --init"
	if got.NextCommand != want {
		t.Fatalf("next command = %q, want %q", got.NextCommand, want)
	}
}

func TestChangeStatusLocalPassingEvidenceAwaitsAcceptance(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "receipt-1")
	reviewID := localReviewID(t, store, selection.Workspace.ID, work.Key)
	closeLocalChangeStore(t, deps, stateDir, store)

	got := runChangeStatusJSON(t, deps, out, work.Key)
	if got.State != "awaiting_acceptance" || got.NextActor != "human_reviewer" || got.Decision != "Awaiting human acceptance" {
		t.Fatalf("status = %#v", got)
	}
	if want := "specgate --yes change accept " + work.Key + " --review-id " + reviewID; got.NextCommand != want {
		t.Fatalf("next command = %q, want %q", got.NextCommand, want)
	}
}

func TestChangeStatusPlainLocalAwaitingAcceptanceShowsRunnableDecision(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "receipt-1")
	closeLocalChangeStore(t, deps, stateDir, store)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "change", "status", work.Key)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	want := "Next: specgate --yes change accept " + work.Key
	if !strings.Contains(out.String(), want) {
		t.Fatalf("output missing %q:\n%s", want, out.String())
	}
}

func TestChangeStatusLocalApprovedDeliveryIsAccepted(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "receipt-1")
	if err := store.DecideDeliveryWithBasis(t.Context(), selection.Workspace.ID, work.Key, "approve", "human", "accepted", localReviewID(t, store, selection.Workspace.ID, work.Key), ""); err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, stateDir, store)

	got := runChangeStatusJSON(t, deps, out, work.Key)
	if got.State != "accepted" || got.NextActor != "none" || len(got.Missing) != 0 || got.NextCommand != "specgate audit "+work.Key || got.Decision != "Accepted" {
		t.Fatalf("status = %#v", got)
	}
}

func TestChangeStatusLocalHumanCanAcceptEvidenceGapWithoutHidingIt(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	review, err := store.SubmitDelivery(t.Context(), selection.Workspace.ID, work.Key, map[string]any{
		"context_digest": work.ContextDigest,
		"agent":          map[string]any{"name": "builder"},
		"criteria": []any{map[string]any{
			"criterion_id": "local-1", "claim": "unsatisfied",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if review.Verdict != "failed" {
		t.Fatalf("review = %#v, want failed evidence", review)
	}
	if err := store.DecideDeliveryWithBasis(t.Context(), selection.Workspace.ID, work.Key, "approve", "human", "reviewed the false negative", localReviewID(t, store, selection.Workspace.ID, work.Key), ""); err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, stateDir, store)

	got := runChangeStatusJSON(t, deps, out, work.Key)
	if got.State != "accepted" ||
		got.Decision != "Accepted" ||
		got.Evidence != "Evidence gaps found" {
		t.Fatalf("status = %#v, want accepted decision with evidence gap disclosed", got)
	}
}

func TestChangeStatusLocalReceiptSurfacesAvailabilityWarnings(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	_, err := store.SubmitDelivery(t.Context(), selection.Workspace.ID, work.Key, map[string]any{
		"context_digest": work.ContextDigest,
		"agent":          map[string]any{"name": "builder"},
		"git_receipt": map[string]any{
			"availability": "unavailable",
			"warnings":     []any{"Git metadata could not be read"},
		},
		"criteria": []any{map[string]any{
			"criterion_id": "local-1",
			"claim":        "satisfied",
			"evidence":     map[string]any{"heading": "targeted test passed"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, stateDir, store)

	got := runChangeStatusJSON(t, deps, out, work.Key)
	if got.Receipt != "Git receipt unavailable; warning: Git metadata could not be read" ||
		!strings.Contains(got.Freshness, "No stored receipt") {
		t.Fatalf("status = %#v, want unavailable receipt kept out of freshness claim", got)
	}
}

func TestChangeStatusLocalHumanRejectionRequestsRework(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "receipt-1")
	if err := store.DecideDeliveryWithBasis(t.Context(), selection.Workspace.ID, work.Key, "reject", "human", "rework", localReviewID(t, store, selection.Workspace.ID, work.Key), ""); err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, stateDir, store)

	got := runChangeStatusJSON(t, deps, out, work.Key)
	if got.State != "rework_requested" || got.NextActor != "implementing_agent" ||
		got.NextCommand != "specgate --yes change submit "+work.Key+" --run-checks --json" ||
		len(got.Missing) != 1 || got.Missing[0] != "Revised completion addressing requested changes" ||
		got.Guidance != "rework" {
		t.Fatalf("rework status = %#v", got)
	}
}

func TestChangeStatusLocalStalePeerEvidenceIsSeparateFromAuthority(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "receipt-1")
	if _, err := store.PeerReviewDelivery(t.Context(), selection.Workspace.ID, work.Key, map[string]any{
		"agent": map[string]any{"name": "reviewer"},
		"peer_review_of": map[string]any{
			"completion_feedback_event_id": latestLocalReportID(t, store, selection.Workspace.ID, work.Key),
			"git_receipt":                  map[string]any{"head_revision": "receipt-1"},
		},
		"criteria": []any{map[string]any{
			"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "reviewed"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "receipt-2")
	closeLocalChangeStore(t, deps, stateDir, store)

	got := runChangeStatusJSON(t, deps, out, work.Key)
	if !got.Stale || got.Decision != "Awaiting human acceptance" || got.State != "awaiting_acceptance" {
		t.Fatalf("stale peer changed delivery authority: %#v", got)
	}
}
