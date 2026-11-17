package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
	"github.com/specgate/specgate/app/cli/internal/workflow"
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

func TestChangeSubmitRunChecksDistinguishesInitialResultsAndCorrections(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, claim, wantAssurance string
		runnerExit, wantExit       int
	}{
		{"pending to pass", "pending", "Agent-reported; locally reproduced", 0, output.ExitOK},
		{"pending to fail", "pending", "Agent-reported; locally reproduced", 1, output.ExitGovernanceFailed},
		{"pass to fail", "pass", "Agent-reported; locally reproduced; corrected 1 agent-reported check result", 1, output.ExitGovernanceFailed},
		{"fail to pass", "fail", "Agent-reported; locally reproduced; corrected 1 agent-reported check result", 0, output.ExitOK},
		{"pass agrees", "pass", "Agent-reported; locally reproduced", 0, output.ExitOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			deps, out := newTestDeps(t)
			stateDir, store, _, work := newLocalChangeWork(t, deps)
			closeLocalChangeStore(t, deps, stateDir, store)
			deps.RunCheckCommand = func(_ context.Context, command string) (int, string) {
				if command != "go test ./..." {
					t.Fatalf("executed command = %q", command)
				}
				return tc.runnerExit, ""
			}
			f := writeDeliveryJSON(t, map[string]any{
				"event_type": "coding_agent.completed", "summary": "done", "context_digest": work.ContextDigest,
				"checks": []map[string]any{{"name": "tests", "command": "go test ./...", "status": tc.claim}},
				"criteria": []map[string]any{{
					"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "verified"},
				}},
			})

			code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "submit", work.Key, "--file", f, "--run-checks")
			if code != tc.wantExit {
				t.Fatalf("submit exit = %d, want %d; output = %s", code, tc.wantExit, out.String())
			}
			for _, field := range []string{`"context_digest":"` + work.ContextDigest + `"`, `"completion_id":"`, `"criterion_evidence":`, `"risks":`} {
				if !strings.Contains(out.String(), field) {
					t.Fatalf("submit omitted status material %s: %s", field, out.String())
				}
			}
			var submitted changeStatusEnvelope
			if err := json.Unmarshal(out.Bytes(), &submitted); err != nil {
				t.Fatal(err)
			}
			out.Reset()
			stored := runChangeStatusJSON(t, deps, out, work.Key)
			if submitted.Data.Assurance != tc.wantAssurance || stored.Assurance != tc.wantAssurance {
				t.Fatalf("submit assurance = %q; status assurance = %q; both want %q", submitted.Data.Assurance, stored.Assurance, tc.wantAssurance)
			}
		})
	}
}

func TestChangeStatusLocalHistoricalPendingObservationIsNotCorrection(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	_, err := store.SubmitDelivery(t.Context(), selection.Workspace.ID, work.Key, map[string]any{
		"context_digest": work.ContextDigest,
		"agent":          map[string]any{"name": "builder"},
		"checks": []any{map[string]any{
			"name": "tests", "command": "go test ./...", "status": "pass", "source": "specgate_cli", "claimed_status": "pending",
		}},
		"criteria": []any{map[string]any{
			"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "verified"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, stateDir, store)
	if got := runChangeStatusJSON(t, deps, out, work.Key); got.Assurance != "Agent-reported; locally reproduced" {
		t.Fatalf("stored pending observation assurance = %q", got.Assurance)
	}
}

func TestLocalDeliveryShowsPartialCriterionDespitePassingCheck(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, selection, _ := newLocalChangeWork(t, deps)
	work, err := store.CreateQuickWork(t.Context(), selection.Workspace.ID, local.QuickWorkInput{
		Title: "Reject empty names", AcceptanceCriteria: []string{"Reject empty names @check:legacy-suite"},
	})
	if err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, stateDir, store)
	file := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed", "context_digest": work.ContextDigest,
		"checks":   []map[string]any{{"name": "legacy-suite", "command": "true", "status": "pass"}},
		"criteria": []map[string]any{{"criterion_id": "local-1", "claim": "partial"}},
	})
	for _, tc := range []struct {
		args []string
		exit int
	}{
		{[]string{"change", "submit", work.Key, "--file", file}, output.ExitGovernanceFailed},
		{[]string{"change", "status", work.Key}, output.ExitOK},
		{[]string{"work", "resume", work.Key}, output.ExitOK},
	} {
		out.Reset()
		if code := command.ExecuteForCode(command.NewRootCommand(deps), append([]string{"--json", "--yes"}, tc.args...)...); code != tc.exit {
			t.Fatalf("%s %s exit = %d, want %d: %s", tc.args[0], tc.args[1], code, tc.exit, out.String())
		}
		var envelope struct {
			Data struct {
				changeStatusData
				Status changeStatusData `json:"status"`
			} `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		status := envelope.Data.changeStatusData
		if tc.args[0] == "work" {
			status = envelope.Data.Status
		}
		if status.Evidence != "Evidence gaps found" || len(status.Criteria) != 1 || status.Criteria[0].Verdict != "unmet" ||
			!strings.Contains(status.Criteria[0].Why, `criterion claim is "partial"`) ||
			!strings.Contains(status.Criteria[0].Why, `check "legacy-suite" passed`) {
			t.Errorf("%s %s hid the incomplete criterion or the passing check: %s", tc.args[0], tc.args[1], out.String())
		}
	}
}

type changeStatusData struct {
	Mode        string                     `json:"mode"`
	Ref         string                     `json:"ref"`
	Title       string                     `json:"title"`
	State       string                     `json:"state"`
	Evidence    string                     `json:"evidence"`
	Assurance   string                     `json:"assurance"`
	Criteria    []workflow.CriterionReview `json:"criteria"`
	Decision    string                     `json:"decision"`
	Receipt     string                     `json:"receipt"`
	Freshness   string                     `json:"freshness"`
	NextActor   string                     `json:"next_actor"`
	Missing     []string                   `json:"missing"`
	Guidance    string                     `json:"guidance"`
	Stale       bool                       `json:"stale"`
	StaleReason string                     `json:"stale_reason"`
	NextCommand string                     `json:"next_command"`
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

func TestChangeSubmitRetryKeepsCustomCompletionAndReadbackRequiresItsPath(t *testing.T) {
	t.Chdir(t.TempDir())
	deps, out := newTestDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed", "summary": "custom",
		"context_digest": work.ContextDigest, "custom_completion_file": false,
		"checks":   []map[string]any{{"name": "tests", "command": "go test ./...", "status": "fail"}},
		"criteria": []map[string]any{{"criterion_id": "local-1", "claim": "not_done"}},
	})
	customPath := filepath.Join(t.TempDir(), "a draft with 'quotes'.json")
	if err := os.Rename(f, customPath); err != nil {
		t.Fatal(err)
	}
	deps.RunCheckCommand = func(context.Context, string) (int, string) { return 1, "fail" }
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "change", "submit", work.Key, "--file", customPath, "--run-checks", "--yes", "--json"); code != output.ExitGovernanceFailed {
		t.Fatalf("exit = %d, output = %s", code, out)
	}
	var envelope changeStatusEnvelope
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	prefix := "specgate --yes change submit " + work.Key + " --run-checks --json --file"
	quoted := "'" + strings.ReplaceAll(customPath, "'", "'\"'\"'") + "'"
	if envelope.Data.NextCommand != prefix+" "+quoted {
		t.Fatalf("submit next command = %q", envelope.Data.NextCommand)
	}
	out.Reset()
	status := runChangeStatusJSON(t, deps, out, work.Key)
	if status.NextCommand != prefix || !strings.Contains(strings.Join(status.Missing, " "), "--file") || strings.Contains(out.String(), customPath) {
		t.Fatalf("status should require a path without retaining the draft location: %#v", status)
	}

	defaultPath := filepath.Join(".specgate", "completion-"+work.Key+".json")
	data, err := os.ReadFile(customPath)
	if err != nil {
		t.Fatal(err)
	}
	// A stale default exists. The incomplete custom retry must stop before opening it.
	writeTestFile(t, defaultPath, string(data))
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), strings.Fields(strings.TrimPrefix(status.NextCommand, "specgate "))...); code != output.ExitUsage || !strings.Contains(out.String(), "flag needs an argument: --file") {
		t.Fatalf("readback retry selected a file without an explicit path: exit = %d, output = %s", code, out)
	}

	// A real canonical draft remains eligible for the established default retry,
	// even if the input report tries to supply its own origin marker.
	writeTestFile(t, defaultPath, strings.Replace(string(data), `"custom_completion_file":false`, `"custom_completion_file":true`, 1))
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "change", "submit", work.Key, "--file", defaultPath, "--run-checks", "--yes", "--json"); code != output.ExitGovernanceFailed {
		t.Fatalf("canonical submit exit = %d, output = %s", code, out)
	}
	out.Reset()
	status = runChangeStatusJSON(t, deps, out, work.Key)
	if want := strings.TrimSuffix(prefix, " --file"); status.NextCommand != want {
		t.Fatalf("canonical retry = %q, want %q", status.NextCommand, want)
	}

	t.Run("parent traversal through symlink cannot select canonical draft", func(t *testing.T) {
		other := t.TempDir()
		if err := os.Mkdir(filepath.Join(other, "nested"), 0700); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(other, defaultPath), string(data))
		if err := os.Symlink(filepath.Join(other, "nested"), "jump"); err != nil {
			if runtime.GOOS == "windows" {
				t.Skipf("symlink unavailable: %v", err)
			}
			t.Fatal(err)
		}
		rawPath := "jump" + string(filepath.Separator) + ".." + string(filepath.Separator) + defaultPath
		out.Reset()
		if code := command.ExecuteForCode(command.NewRootCommand(deps), "change", "submit", work.Key, "--file", rawPath, "--run-checks", "--yes", "--json"); code != output.ExitGovernanceFailed {
			t.Fatalf("submit exit = %d, output = %s", code, out)
		}
		out.Reset()
		status := runChangeStatusJSON(t, deps, out, work.Key)
		if status.NextCommand != prefix {
			t.Fatalf("noncanonical retry must require --file, got %q", status.NextCommand)
		}
	})
}
