package command_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/local"
)

func TestPreservationEvidenceSurvivesHumanDecisionAndLaterCheckoutChange(t *testing.T) {
	for _, tc := range []struct{ verb, data, outcome, decision string }{
		{"accept", "broken", "failed", "approve"},
		{"request-changes", "stable", "passed", "reject"},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			deps, _, _, out := newFakeDeps(t)
			dir, store, sel, _ := newLocalChangeWork(t, deps)
			root := deps.WorkingDir
			path := filepath.Join(root, "data.txt")
			if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"init", "-q"}, {"add", "data.txt"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-qm", "fixture"}} {
				if b, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
					t.Fatalf("git: %v %s", err, b)
				}
			}
			work, err := store.CreateQuickWork(t.Context(), sel.Workspace.ID, local.QuickWorkInput{Title: "new and preserved behavior", AcceptanceCriteria: []string{"New behavior @check:new", "Stored data remains stable @check:preserve"}})
			if err != nil {
				t.Fatal(err)
			}
			newCommand := `if test -f data.txt; then result=''; else result='<failure/>'; fi; printf '<testsuite><testcase classname="fixture" name="new">%s</testcase></testsuite>' "$result" > "$SPECGATE_TEST_REPORT"`
			preserveCommand := `if test "$(cat data.txt)" = stable; then result=''; else result='<failure/>'; fi; printf '<testsuite><testcase classname="fixture" name="preserve">%s</testcase></testsuite>' "$result" > "$SPECGATE_TEST_REPORT"`
			checks := []local.VerificationCheck{
				{Name: "new", Command: newCommand, Cwd: ".", TestReport: &local.JUnitTestReport{Format: "junit", Selectors: map[string][]local.SelectedTestCase{"local-1": {{ClassName: "fixture", Name: "new"}}}}},
				{Name: "preserve", Command: preserveCommand, Cwd: ".", TestReport: &local.JUnitTestReport{Format: "junit", Selectors: map[string][]local.SelectedTestCase{"local-2": {{ClassName: "fixture", Name: "preserve"}}}}},
			}
			pin, err := store.PinVerificationContract(t.Context(), sel.Workspace.ID, work.Key, root, "human", local.VerificationContractInput{ContextDigest: work.ContextDigest, Shell: "sh", Checks: checks})
			if err != nil {
				t.Fatal(err)
			}
			closeLocalChangeStore(t, deps, dir, store)
			body := map[string]any{"event_type": "coding_agent.completed", "agent": map[string]any{"name": "builder"}, "context_digest": work.ContextDigest, "verification_contract_digest": pin.Digest,
				"checks":   []any{map[string]any{"name": "new", "command": newCommand, "cwd": ".", "status": "pass"}, map[string]any{"name": "preserve", "command": preserveCommand, "cwd": ".", "status": "pass"}},
				"criteria": []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "new check"}}, map[string]any{"criterion_id": "local-2", "claim": "satisfied", "evidence": map[string]any{"heading": "preserve check"}}}}
			file := writeDeliveryJSON(t, body)
			wantExit := 0
			if tc.outcome == "failed" {
				wantExit = 1
			}
			if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "submit", work.Key, "--file", file, "--run-checks"); code != wantExit {
				t.Fatalf("submit: %d %s", code, out.String())
			}
			out.Reset()
			if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "work", "checkpoint", work.Key); code != 0 {
				t.Fatalf("checkpoint: %d %s", code, out.String())
			}
			var cp struct {
				Data struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if err := json.Unmarshal(out.Bytes(), &cp); err != nil || cp.Data.ID == "" {
				t.Fatalf("checkpoint output: %s %v", out.String(), err)
			}
			read := func() (local.AcceptanceBasis, *local.AcceptanceBasis) {
				out.Reset()
				if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "status", work.Key, "--detail", "--checkpoint", cp.Data.ID); code != 0 {
					t.Fatalf("status: %d %s", code, out.String())
				}
				var got struct {
					Data struct {
						Basis    local.AcceptanceBasis  `json:"acceptance_basis"`
						Recorded *local.AcceptanceBasis `json:"recorded_acceptance_basis"`
					} `json:"data"`
				}
				if err := json.Unmarshal(out.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				return got.Data.Basis, got.Data.Recorded
			}
			assertEvidence := func(b local.AcceptanceBasis) {
				t.Helper()
				if len(b.CriterionEvidence) != 2 {
					t.Fatalf("lost criteria: %+v", b)
				}
				for i, outcome := range []string{"passed", tc.outcome} {
					row := b.CriterionEvidence[i]
					if len(row.Selected) != 1 || row.Selected[0].Outcome != outcome || row.CheckProvenance != "selected_test_observed" {
						t.Fatalf("criterion %d evidence: %+v", i, row)
					}
				}
			}
			basis, _ := read()
			assertEvidence(basis)
			if basis.Freshness != "matching_endpoints" {
				t.Fatalf("selected checkpoint falsely stale: %+v", basis)
			}
			// A human may accept residual risk, but the exact historical basis
			// must retain the stale checkout and peer-review limitations.
			if err := os.WriteFile(path, []byte("changed before decision"), 0600); err != nil {
				t.Fatal(err)
			}
			basis, _ = read()
			encodedBasis, err := json.Marshal(basis)
			if err != nil {
				t.Fatal(err)
			}
			var observed struct {
				Freshness string `json:"freshness"`
				Peer      struct {
					State string `json:"state"`
				} `json:"peer_review"`
				Gaps []string `json:"gaps"`
			}
			if err := json.Unmarshal(encodedBasis, &observed); err != nil {
				t.Fatal(err)
			}
			if observed.Freshness != "stale" || observed.Peer.State != "not_run" || len(observed.Gaps) == 0 {
				t.Fatalf("basis hides current limitations: %s", encodedBasis)
			}
			out.Reset()
			if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", tc.verb, work.Key, "--review-id", basis.ReviewID, "--basis-digest", basis.Digest, "--checkpoint", cp.Data.ID, "--note", "fixture human decision"); code != 0 {
				t.Fatalf("decision: %d %s", code, out.String())
			}
			_, recorded := read()
			if recorded == nil || recorded.Decision != tc.decision {
				t.Fatalf("missing decision: %+v", recorded)
			}
			assertEvidence(*recorded)
			if raw, err := json.Marshal(recorded); err != nil || !strings.Contains(string(raw), `"freshness":"stale"`) {
				t.Fatalf("recorded basis lost stale evidence: %s %v", raw, err)
			}
			before, err := json.Marshal(recorded)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("later checkout"), 0600); err != nil {
				t.Fatal(err)
			}
			current, recorded := read()
			after, err := json.Marshal(recorded)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) || current.Digest == basis.Digest {
				t.Fatal("historical basis changed or current checkout change was hidden")
			}
		})
	}
}
