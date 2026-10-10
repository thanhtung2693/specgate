package command_test

import (
	"context"
	"database/sql"
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

func TestFirstCheckpointDisclosesStoreWideUpgrade(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	if b, err := exec.Command("git", "-C", deps.WorkingDir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, b)
	}
	for _, args := range [][]string{{"config", "user.email", "fixture@example.test"}, {"config", "user.name", "Fixture"}, {"commit", "--allow-empty", "-qm", "baseline"}} {
		if b, err := exec.Command("git", append([]string{"-C", deps.WorkingDir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, b)
		}
	}
	dir, s, _, w := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, dir, s)
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "checkpoint", w.Key); code == 0 {
		t.Fatalf("unconfirmed upgrade succeeded: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "state.db.pre-enhanced.bak")); !os.IsNotExist(err) {
		t.Fatalf("unconfirmed upgrade created backup: %v", err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "work", "checkpoint", w.Key); code != 0 {
		t.Fatalf("checkpoint %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), `"store_upgrade"`) || !strings.Contains(out.String(), filepath.Join(dir, "state.db.pre-enhanced.bak")) || !strings.Contains(out.String(), `"requires_supporting_cli":true`) {
		t.Fatalf("missing upgrade disclosure: %s", out.String())
	}
}

func TestCheckpointRejectsUnavailableCheckoutWithoutPersisting(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	dir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, dir, store)
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "work", "checkpoint", work.Key); code != output.ExitUnavailable {
		t.Fatalf("unavailable checkpoint exit=%d: %s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "state.db.pre-enhanced.bak")); !os.IsNotExist(err) {
		t.Fatalf("unavailable checkpoint created backup: %v", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM work_checkpoints`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unavailable checkpoint persisted %d rows", count)
	}
}

func TestSelectedCheckpointComparisonIsVisibleAndBoundToDecision(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	git := func(args ...string) {
		t.Helper()
		if b, err := exec.Command("git", append([]string{"-C", deps.WorkingDir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, b)
		}
	}
	git("init", "-q")
	git("config", "user.email", "fixture@example.test")
	git("config", "user.name", "Fixture")
	git("commit", "--allow-empty", "-qm", "baseline")
	path := filepath.Join(deps.WorkingDir, "checkpoint.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	dir, store, selection, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "head")
	closeLocalChangeStore(t, deps, dir, store)
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "work", "checkpoint", work.Key); code != 0 {
		t.Fatalf("checkpoint %d: %s", code, out.String())
	}
	store, err := local.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	var created struct{ Data struct{ ID string } }
	if err := json.Unmarshal(out.Bytes(), &created); err != nil || created.Data.ID == "" {
		t.Fatalf("checkpoint command omitted identity: %v %s", err, out.String())
	}
	checkpoint, err := store.GetCheckpoint(t.Context(), selection.Workspace.ID, work.Key, created.Data.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "status", work.Key, "--checkpoint", checkpoint.ID, "--detail"); code != 0 {
		t.Fatalf("selected status %d: %s", code, out.String())
	}
	var status struct {
		Data struct {
			ReviewID string `json:"review_id"`
			Digest   string `json:"basis_digest"`
			Delta    struct {
				State    string   `json:"state"`
				Modified []string `json:"modified"`
			} `json:"selected_checkpoint_comparison"`
			Basis local.AcceptanceBasis `json:"acceptance_basis"`
			Risks []struct {
				Category string `json:"category"`
				Count    int    `json:"count"`
			} `json:"risks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Data.Digest == "" || status.Data.Delta.State != "changed" || len(status.Data.Delta.Modified) != 1 || status.Data.Delta.Modified[0] != "checkpoint.txt" {
		t.Fatalf("checkpoint comparison missing: %s", out.String())
	}
	if status.Data.Basis.CheckpointCompare == nil || status.Data.Basis.CheckpointCompare.State != "changed" || status.Data.Basis.CheckpointCompare.ModifiedCount != 1 {
		t.Fatalf("comparison omitted from acceptance basis: %s", out.String())
	}
	firstDigest := status.Data.Digest
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "status", work.Key, "--checkpoint", checkpoint.ID, "--detail"); code != 0 {
		t.Fatalf("repeated selected status %d: %s", code, out.String())
	}
	if err := json.Unmarshal(out.Bytes(), &status); err != nil || status.Data.Digest != firstDigest {
		t.Fatalf("repeated read changed basis: %s err=%v", out.String(), err)
	}
	deps.WorkingDir = t.TempDir()
	out.Reset()
	for _, args := range [][]string{
		{"--json", "change", "status", work.Key, "--checkpoint", checkpoint.ID},
		{"--json", "--yes", "change", "accept", work.Key, "--review-id", status.Data.ReviewID, "--basis-digest", firstDigest, "--checkpoint", checkpoint.ID},
	} {
		out.Reset()
		if code := command.ExecuteForCode(command.NewRootCommand(deps), args...); code != output.ExitUsage {
			t.Fatalf("noncomparable acceptance selection exit %d: %s", code, out.String())
		}
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "resume", work.Key, "--since", checkpoint.ID); code != 0 || !strings.Contains(out.String(), `"state":"unavailable"`) {
		t.Fatalf("resume must retain unavailable delta: %d %s", code, out.String())
	}
}

func TestDeliveryStatusOmitsEnhancedFieldsForLegacyReview(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	dir, store, sel, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, sel.Workspace.ID, work, "builder", "head")
	closeLocalChangeStore(t, deps, dir, store)
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "status", work.Key); code != 0 {
		t.Fatalf("status %d: %s", code, out.String())
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"basis_digest", "acceptance_basis", "recorded_acceptance_basis"} {
		if value, ok := envelope.Data[key]; ok {
			t.Errorf("legacy status includes %s=%#v", key, value)
		}
	}
}

func TestPinRejectsWatchedChangesDuringConfirmation(t *testing.T) {
	deps, _, prompt, out := newFakeDeps(t)
	dir, s, sel, _ := newLocalChangeWork(t, deps)
	w, err := s.CreateQuickWork(t.Context(), sel.Workspace.ID, local.QuickWorkInput{Title: "Pin", AcceptanceCriteria: []string{"Works @check:unit"}})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	watched := filepath.Join(root, "test.txt")
	if err := os.WriteFile(watched, []byte("reviewed"), 0600); err != nil {
		t.Fatal(err)
	}
	deps.WorkingDir = root
	deps.StdinIsTTY = func() bool { return true }
	prompt.confirmValue = true
	prompt.confirmObserver = func() {
		if err := os.WriteFile(watched, []byte("changed"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	file := writeDeliveryJSON(t, map[string]any{"context_digest": w.ContextDigest, "shell": "sh", "watched_paths": []string{"test.txt"}, "checks": []any{map[string]any{"name": "unit", "command": "true", "cwd": "."}}})
	closeLocalChangeStore(t, deps, dir, s)
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "work", "verification", w.Key, "--file", file); code != output.ExitConflict {
		t.Fatalf("code=%d: %s", code, out.String())
	}
}

func TestDeliveryStatusReturnsEnhancedDecisionBasis(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	dir, s, sel, _ := newLocalChangeWork(t, deps)
	w, err := s.CreateQuickWork(t.Context(), sel.Workspace.ID, local.QuickWorkInput{Title: "basis status", AcceptanceCriteria: []string{"Works @check:unit"}})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	deps.WorkingDir = root
	pin, err := s.PinVerificationContract(t.Context(), sel.Workspace.ID, w.Key, root, "human", local.VerificationContractInput{
		ContextDigest: w.ContextDigest,
		Shell:         "sh",
		Checks: []local.VerificationCheck{{
			Name: "unit", Command: "true", Cwd: ".",
			TestReport: &local.JUnitTestReport{Format: "junit", Selectors: map[string][]local.SelectedTestCase{
				"local-1": {{ClassName: "pkg", Name: "works"}},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{
		"event_type":                   "coding_agent.completed",
		"agent":                        map[string]any{"name": "builder"},
		"context_digest":               w.ContextDigest,
		"verification_contract_digest": pin.Digest,
		"criteria":                     []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "proof"}}},
		"checks":                       []any{map[string]any{"name": "unit", "command": "true", "cwd": ".", "status": "pass"}},
	}
	if _, err := s.SubmitDelivery(t.Context(), sel.Workspace.ID, w.Key, body, root); err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, dir, s)
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "status", w.Key); code != 0 {
		t.Fatalf("status %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), `"basis_digest"`) || !strings.Contains(out.String(), `"acceptance_basis"`) {
		t.Fatalf("missing enhanced decision material: %s", out.String())
	}
}

func TestDeliveryStatusPrintsCopyableBasisDecision(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	dir, s, sel, w := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, s, sel.Workspace.ID, w, "builder", "head")
	if _, err := s.CreateCheckpointWithFiles(t.Context(), sel.Workspace.ID, w.Key, "checkpoint", nil, ""); err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, dir, s)
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "delivery", "status", w.Key); code != 0 {
		t.Fatalf("status %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "--basis-digest") || !strings.Contains(out.String(), "--review-id") {
		t.Fatalf("missing copyable basis command: %s", out.String())
	}
}

func TestStatusShowsRecordedBasisAfterLegacyExplicitDecision(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	dir, store, selection, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "head")
	basis, err := store.AcceptanceBasis(t.Context(), selection.Workspace.ID, work.Key)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DecideDeliveryWithBasis(t.Context(), selection.Workspace.ID, work.Key, "reject", "human", "reviewed", basis.ReviewID, basis.Digest); err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, dir, store)
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "status", work.Key); code != 0 {
		t.Fatalf("status %d: %s", code, out.String())
	}
	var response struct {
		Data struct {
			Recorded *local.AcceptanceBasis `json:"recorded_acceptance_basis"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.Recorded == nil || response.Data.Recorded.Digest != basis.Digest {
		t.Fatalf("recorded legacy basis missing: %s", out.String())
	}
}

func TestSelectedLineageImpactRoundTripsThroughHumanDecision(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	dir := t.TempDir()
	store, err := local.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	sel, err := store.Initialize(t.Context(), local.InitInput{WorkspaceName: "Local", DisplayName: "Human", Username: "human"})
	if err != nil {
		t.Fatal(err)
	}
	base, err := store.PublishArtifact(t.Context(), sel.Workspace.ID, local.ArtifactInput{
		FeatureKey: "LINEAGE", RequestType: "new_feature",
		Documents:      []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("old")}, {Path: "plan.md", Role: "plan", Content: []byte("implement old")}},
		SourceCriteria: []local.SourceCriterion{{ID: "old", Text: "Preserve old", SourcePath: "spec.md"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RunReadiness(t.Context(), sel.Workspace.ID, base.ID); err != nil {
		t.Fatal(err)
	}
	tasks, err := store.ListGateTasks(t.Context(), sel.Workspace.ID, base.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		gate := local.GateResultInput{Gate: task.GateKey, GateDigest: task.GateDigest, InputDigest: task.ArtifactDigest, State: "pass", Summary: "reviewed"}
		gate.Evaluator.Executor = task.Executor
		if _, err := store.SubmitGateResult(t.Context(), sel.Workspace.ID, task.TaskID, gate); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ApproveArtifact(t.Context(), sel.Workspace.ID, base.ID, "human", "scope reviewed"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PromoteArtifact(t.Context(), sel.Workspace.ID, base.ID); err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(t.Context(), sel.Workspace.ID, local.WorkInput{FeatureRef: "LINEAGE", Title: "Old scope", AcceptanceCriteria: []string{"Preserve old @source:old"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SubmitDelivery(t.Context(), sel.Workspace.ID, work.Key, map[string]any{
		"context_digest": work.ContextDigest, "agent": map[string]any{"name": "builder"},
		"criteria": []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "proof"}}},
	}); err != nil {
		t.Fatal(err)
	}
	target, err := store.PublishArtifact(t.Context(), sel.Workspace.ID, local.ArtifactInput{
		FeatureKey: "LINEAGE", RequestType: "change_request", BaseVersion: "v1",
		Documents:      []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("new")}, {Path: "plan.md", Role: "plan", Content: []byte("implement old")}},
		SourceCriteria: []local.SourceCriterion{{ID: "new", Text: "Preserve old", SourcePath: "spec.md"}},
		SourceLineage: &local.SourceLineage{Version: 1, BaseArtifactID: base.ID, BaseDigest: base.SnapshotDigest,
			Rows: []local.LineageRow{{BaseID: "old", TargetIDs: []string{"new"}, Reason: "renamed"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, dir, store)
	statusArgs := []string{"--json", "change", "status", work.Key, "--impact-base", base.ID, "--impact-target", target.ID}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), statusArgs...); code != 0 {
		t.Fatalf("status %d: %s", code, out.String())
	}
	var result struct {
		Data struct {
			Basis  string               `json:"basis_digest"`
			Review string               `json:"review_id"`
			Impact local.ArtifactImpact `json:"selected_impact"`
			Next   string               `json:"next_command"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.Basis == "" || result.Data.Impact.RequirementImpact != "declared" || !strings.Contains(result.Data.Next, "--impact-target") {
		t.Fatalf("selection absent: %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "accept", work.Key, "--review-id", result.Data.Review, "--basis-digest", result.Data.Basis); code != output.ExitConflict {
		t.Fatalf("omitted selection code=%d: %s", code, out.String())
	}
	store, err = local.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateWork(t.Context(), sel.Workspace.ID, local.WorkInput{FeatureRef: "LINEAGE", Title: "Parallel old work", AcceptanceCriteria: []string{"Old contract @source:old"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "accept", work.Key, "--review-id", result.Data.Review, "--basis-digest", result.Data.Basis, "--impact-base", base.ID, "--impact-target", target.ID); code != output.ExitConflict {
		t.Fatalf("changed comparison accepted with old basis %d: %s", code, out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), statusArgs...); code != 0 {
		t.Fatalf("fresh status %d: %s", code, out.String())
	}
	var refreshed struct {
		Data struct {
			Basis string `json:"basis_digest"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &refreshed); err != nil {
		t.Fatal(err)
	}
	if refreshed.Data.Basis == result.Data.Basis {
		t.Fatalf("linked work did not change selected basis: %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "accept", work.Key, "--review-id", result.Data.Review, "--basis-digest", refreshed.Data.Basis, "--impact-base", base.ID, "--impact-target", target.ID); code != 0 {
		t.Fatalf("selected decision %d: %s", code, out.String())
	}
}

func TestFullRejectsLocalReportExtensionsBeforeExecution(t *testing.T) {
	for _, route := range []string{"submit", "report"} {
		for _, field := range []string{"test_report", "test_observation", "test_run"} {
			t.Run(route+"-"+field, func(t *testing.T) {
				deps, client, _, out := newFakeDeps(t)
				ran := false
				deps.RunCheckCommand = func(context.Context, string) (int, string) { ran = true; return 0, "" }
				file := writeDeliveryJSON(t, map[string]any{"event_type": "coding_agent.completed", "checks": []any{map[string]any{"name": "unit", "command": "true", "status": "pass", field: map[string]any{"format": "junit"}}}})
				args := []string{"--json", "--yes", "delivery", route, "CR-1", "--file", file, "--skip-evidence-check"}
				if route == "submit" {
					args = append(args, "--run-checks")
				}
				code := command.ExecuteForCode(command.NewRootCommand(deps), args...)
				if code != output.ExitIncompatible || ran || client.calls != 0 {
					t.Fatalf("code=%d ran=%t calls=%d: %s", code, ran, client.calls, out.String())
				}
			})
		}
	}
}

func TestFullRejectsAcceptanceSelectionsBeforeNetwork(t *testing.T) {
	for _, route := range [][]string{{"change", "status"}, {"change", "accept"}, {"delivery", "status"}, {"delivery", "approve"}, {"delivery", "reject"}} {
		for _, flag := range []string{"checkpoint", "impact-base", "impact-target"} {
			t.Run(route[0]+"-"+route[1]+"-"+flag, func(t *testing.T) {
				deps, client, _, out := newFakeDeps(t)
				args := append([]string{"--json", "--yes"}, route...)
				args = append(args, "CR-1", "--"+flag, "selected")
				code := command.ExecuteForCode(command.NewRootCommand(deps), args...)
				if code != output.ExitIncompatible || client.calls != 0 {
					t.Fatalf("code=%d calls=%d: %s", code, client.calls, out.String())
				}
			})
		}
	}
}

func TestEnhancedDecisionAliasesRejectWatchedDriftAndReadBackBasis(t *testing.T) {
	for _, verb := range []string{"approve", "reject"} {
		t.Run(verb, func(t *testing.T) {
			deps, _, _, out := newFakeDeps(t)
			dir, s, sel, w := newLocalChangeWork(t, deps)
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
				t.Fatal(err)
			}
			deps.WorkingDir = root
			path := filepath.Join(root, "watched.txt")
			if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
				t.Fatal(err)
			}
			w, err := s.CreateQuickWork(t.Context(), sel.Workspace.ID, local.QuickWorkInput{Title: "watched", AcceptanceCriteria: []string{"works @check:unit"}})
			if err != nil {
				t.Fatal(err)
			}
			pin, err := s.PinVerificationContract(t.Context(), sel.Workspace.ID, w.Key, root, "human", local.VerificationContractInput{ContextDigest: w.ContextDigest, Shell: "sh", WatchedPaths: []string{"watched.txt"}, Checks: []local.VerificationCheck{{Name: "unit", Command: "true", Cwd: "."}}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.SubmitDelivery(t.Context(), sel.Workspace.ID, w.Key, map[string]any{"agent": map[string]any{"name": "builder"}, "context_digest": w.ContextDigest, "verification_contract_digest": pin.Digest, "checks": []any{map[string]any{"name": "unit", "command": "true", "cwd": ".", "status": "pass"}}}, root)
			if err != nil {
				t.Fatal(err)
			}
			closeLocalChangeStore(t, deps, dir, s)
			read := func() (string, string) {
				out.Reset()
				if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "status", w.Key); code != 0 {
					t.Fatalf("status %d: %s", code, out.String())
				}
				var response struct {
					Data struct {
						Basis  string `json:"basis_digest"`
						Review string `json:"review_id"`
					}
				}
				if err := json.Unmarshal(out.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				return response.Data.Basis, response.Data.Review
			}
			basis, review := read()
			if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
				t.Fatal(err)
			}
			out.Reset()
			if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "delivery", verb, w.Key, "--review-id", review, "--basis-digest", basis); code != output.ExitConflict {
				t.Fatalf("stale decision %d: %s", code, out.String())
			}
			basis, review = read()
			out.Reset()
			if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "delivery", verb, w.Key, "--review-id", review, "--basis-digest", basis); code != 0 {
				t.Fatalf("fresh decision %d: %s", code, out.String())
			}
			read()
			var response struct {
				Data struct {
					Recorded *local.AcceptanceBasis `json:"recorded_acceptance_basis"`
				}
			}
			if err := json.Unmarshal(out.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Data.Recorded == nil || response.Data.Recorded.Digest != basis {
				t.Fatalf("recorded basis missing: %s", out.String())
			}
		})
	}
}

func TestResumeRejectsExplicitUnknownCheckpoint(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	dir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, dir, store)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "resume", work.Key, "--since", "missing")
	if code != output.ExitConflict {
		t.Fatalf("got %d: %s", code, out.String())
	}
}

func TestHandoffRefusesCheckpointBeforeWritingFile(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	dir, store, sel, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, sel.Workspace.ID, work, "builder", "head")
	if _, err := store.CreateCheckpointWithFiles(t.Context(), sel.Workspace.ID, work.Key, "fingerprint", nil, ""); err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, dir, store)
	dest := filepath.Join(t.TempDir(), "bundle.json")
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "handoff", "export", work.Key, "--file", dest)
	if code == 0 {
		t.Fatalf("lossy export accepted: %s", out.String())
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("export wrote output: %v", err)
	}
}
