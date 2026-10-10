package command_test

import (
	"context"
	"encoding/json"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalVerificationPinScaffoldAndRejectChangedCommand(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	stateDir, store, sel, _ := newLocalChangeWork(t, deps)
	w, err := store.CreateQuickWork(t.Context(), sel.Workspace.ID, local.QuickWorkInput{Title: "Pinned", AcceptanceCriteria: []string{"Works @check:unit"}})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	deps.WorkingDir = root
	closeLocalChangeStore(t, deps, stateDir, store)
	file := writeDeliveryJSON(t, map[string]any{"context_digest": w.ContextDigest, "shell": "sh", "checks": []map[string]any{{"name": "unit", "command": "printf checked", "cwd": "."}}})
	args := []string{"--json", "--yes", "work", "verification", w.Key, "--file", file}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), args...); code != 0 {
		t.Fatalf("pin %d: %s", code, out.String())
	}
	var pin struct {
		Data local.VerificationContract `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &pin); err != nil {
		t.Fatal(err)
	}
	if pin.Data.Status != "pinned" || pin.Data.Digest == "" {
		t.Fatalf("bad pin: %s", out.String())
	}
	out.Reset()
	reportPath := filepath.Join(t.TempDir(), "completion.json")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "report", w.Key, "--init", reportPath); code != 0 {
		t.Fatalf("scaffold %d: %s", code, out.String())
	}
	raw, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), pin.Data.Digest) || !strings.Contains(string(raw), "printf checked") {
		t.Fatalf("pin missing in scaffold: %s", raw)
	}
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	deps.WorkingDir = nested
	for _, cmdText := range []string{"true", "printf checked"} {
		out.Reset()
		calls := 0
		deps.RunCheckCommand = func(_ context.Context, cmd string) (int, string) {
			calls++
			if !strings.Contains(cmd, "printf checked") {
				t.Errorf("runner command: %s", cmd)
			}
			if !strings.Contains(cmd, "cd '"+realRoot+"' &&") {
				t.Errorf("pinned command did not run at repository root: %s", cmd)
			}
			return 0, "checked"
		}
		body := map[string]any{"event_type": "coding_agent.completed", "context_digest": w.ContextDigest, "verification_contract_digest": pin.Data.Digest, "criteria": []map[string]any{{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "proof"}}}, "checks": []map[string]any{{"name": "unit", "command": cmdText, "cwd": ".", "status": "pass"}}}
		f := writeDeliveryJSON(t, body)
		code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "submit", w.Key, "--file", f, "--run-checks")
		if cmdText == "true" {
			if code == 0 || calls != 0 {
				t.Fatalf("mismatch executed: %d %d %s", code, calls, out.String())
			}
		} else if code != 0 || calls != 1 {
			t.Fatalf("valid submit: %d %d %s", code, calls, out.String())
		}
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "status", w.Key); code != 0 {
		t.Fatalf("status %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), `"verification_contract":"pinned"`) {
		t.Fatalf("status omitted contract: %s", out.String())
	}
}

func TestLocalSubmitPreflightBeforeRunner(t *testing.T) {
	for _, kind := range []string{"unknown", "context", "workspace", "delivered"} {
		t.Run(kind, func(t *testing.T) {
			deps, _, _, out := newFakeDeps(t)
			stateDir, store, selection, work := newLocalChangeWork(t, deps)
			ref, digest, want := work.Key, work.ContextDigest, output.ExitUsage
			if kind == "unknown" {
				ref = "BAD-REF"
				want = output.ExitNotFound
			}
			if kind == "context" {
				digest = "wrong"
			}
			if kind == "delivered" {
				submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "head-a")
				if err := store.DecideDeliveryWithBasis(t.Context(), selection.Workspace.ID, work.Key, "approve", "reviewer", "", localReviewID(t, store, selection.Workspace.ID, work.Key), ""); err != nil {
					t.Fatal(err)
				}
				want = output.ExitConflict
			}
			closeLocalChangeStore(t, deps, stateDir, store)
			calls := 0
			deps.RunCheckCommand = func(context.Context, string) (int, string) { calls++; return 0, "ok" }
			file := writeDeliveryJSON(t, map[string]any{"event_type": "coding_agent.completed", "context_digest": digest,
				"criteria": []map[string]any{{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "verified"}}},
				"checks":   []map[string]any{{"name": "unit", "command": "true", "status": "pass"}}})
			args := []string{"--json", "--yes", "change", "submit", ref, "--file", file, "--run-checks"}
			if kind == "workspace" {
				args = append(args, "--workspace", "missing-workspace")
				want = output.ExitNotFound
			}
			code := command.ExecuteForCode(command.NewRootCommand(deps), args...)
			if calls != 0 {
				t.Fatalf("invalid %s executed %d commands (exit %d): %s", kind, calls, code, out.String())
			}
			if code != want {
				t.Fatalf("exit %d want %d: %s", code, want, out.String())
			}
		})
	}
}

func TestLocalReportEnabledCheckRequiresFreshSelectedJUnitEvidence(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	var err error
	work, err = store.CreateQuickWork(t.Context(), selection.Workspace.ID, local.QuickWorkInput{Title: "JUnit", AcceptanceCriteria: []string{"selected test passes @check:unit"}})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if b, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, b)
	}
	deps.WorkingDir = root
	literal := `printf '%s' '<testsuite><testcase classname="pkg" name="works"/></testsuite>' > "$SPECGATE_TEST_REPORT"`
	input := local.VerificationContractInput{ContextDigest: work.ContextDigest, Shell: "sh", Checks: []local.VerificationCheck{{
		Name: "unit", Command: literal, Cwd: ".", TestReport: &local.JUnitTestReport{Format: "junit", Selectors: map[string][]local.SelectedTestCase{
			"local-1": {{ClassName: "pkg", Name: "works"}},
		}},
	}}}
	contract, err := store.PinVerificationContract(t.Context(), selection.Workspace.ID, work.Key, root, "human", input)
	if err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, stateDir, store)
	body := map[string]any{"event_type": "coding_agent.completed", "agent": map[string]any{"name": "builder"}, "context_digest": work.ContextDigest, "verification_contract_digest": contract.Digest,
		"criteria": []map[string]any{{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "proof"}}},
		"checks":   []map[string]any{{"name": "unit", "command": literal, "cwd": ".", "status": "pass"}}}
	file := writeDeliveryJSON(t, body)
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "submit", work.Key, "--file", file, "--run-checks"); code != output.ExitOK {
		t.Fatalf("submit %d: %s", code, out.String())
	}
	stored, err := local.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stored.Close()
	report, err := stored.LatestDeliveryReport(t.Context(), selection.Workspace.ID, work.Key)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := report.Body["checks"].([]any)[0].(map[string]any)["test_observation"]; !ok {
		t.Fatalf("selected observation not retained: %#v", report.Body)
	}
	// Replaying a genuine stored receipt through the untrusted input file
	// without a new executor run must not reproduce its observed pass.
	body["checks"] = report.Body["checks"]
	replayed := writeDeliveryJSON(t, body)
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "submit", work.Key, "--file", replayed); code != output.ExitUsage {
		t.Fatalf("replayed receipt passed: %d %s", code, out.String())
	}
	// A new exit-zero invocation that produces no XML cannot reuse the old
	// observation either. It may record a fresh failed attempt, never the pass.
	deps.RunCheckCommand = func(context.Context, string) (int, string) { return 0, "" }
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "submit", work.Key, "--file", replayed, "--run-checks"); code != output.ExitGovernanceFailed {
		t.Fatalf("empty new run reused a receipt: %d %s", code, out.String())
	}
	latest, err := stored.LatestDeliveryReport(t.Context(), selection.Workspace.ID, work.Key)
	if err != nil {
		t.Fatal(err)
	}
	check := latest.Body["checks"].([]any)[0].(map[string]any)
	oldRun := report.Body["checks"].([]any)[0].(map[string]any)["test_run"].(map[string]any)
	newRun := check["test_run"].(map[string]any)
	if check["test_observation"] != nil || newRun["id"] == oldRun["id"] || check["status"] != "fail" {
		t.Fatalf("replayed metadata retained observed assurance: %#v", check)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "status", work.Key, "--detail"); code != output.ExitOK {
		t.Fatalf("status %d: %s", code, out.String())
	}
	var status struct {
		Data struct {
			Criteria []struct {
				Why string `json:"why"`
			} `json:"criteria"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if len(status.Data.Criteria) != 1 || !strings.Contains(status.Data.Criteria[0].Why, "test report must be a regular non-symlink file") {
		t.Fatalf("missing fresh report reason lost after persistence: %s", out.String())
	}
}
