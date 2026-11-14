package command_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestChangeSubmitValidatesCompletionBeforeWriting(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.docs_updated",
		"summary":    "Updated docs",
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "submit", "CR-101", "--file", f)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d output = %s", code, out.String())
	}
	var envelope struct {
		Command string `json:"command"`
		OK      bool   `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Command != "change.submit" || envelope.OK {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestChangeSubmitRunChecksRequiresYesWithFacadeEnvelope(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type":     "coding_agent.completed",
		"summary":        "All done",
		"context_digest": work.ContextDigest,
		"checks": []map[string]any{{
			"name": "tests", "command": "go test ./...", "status": "pass",
		}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "submit", work.Key, "--file", f, "--run-checks")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	var envelope struct {
		Command string `json:"command"`
		OK      bool   `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Command != "change.submit" || envelope.OK {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestChangeSubmitRequiresFileForUnsafeRef(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "submit", "../CR-101")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "--file") {
		t.Fatalf("missing file flag guidance: %s", out.String())
	}
}

func TestChangeDecisionsLocalRequireYes(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "receipt-1")
	closeLocalChangeStore(t, deps, stateDir, store)

	for _, verb := range []string{"approve", "accept", "request-changes"} {
		out.Reset()
		code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", verb, work.Key)
		if code != output.ExitUsage || !strings.Contains(out.String(), "--yes") {
			t.Fatalf("%s: exit = %d output = %s", verb, code, out.String())
		}
	}
}

func TestChangeAcceptLocalWithYesStoresDecisionWithoutHTTP(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "receipt-1")
	reviewID := localReviewID(t, store, selection.Workspace.ID, work.Key)
	closeLocalChangeStore(t, deps, stateDir, store)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "accept", work.Key, "--review-id", reviewID, "--note", "accepted locally")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var envelope struct {
		Command string `json:"command"`
		OK      bool   `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Command != "change.accept" || !envelope.OK {
		t.Fatalf("envelope = %#v", envelope)
	}
	verified, err := local.Open(stateDir + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer verified.Close()
	review, err := verified.DeliveryStatus(t.Context(), selection.Workspace.ID, work.Key)
	if err != nil {
		t.Fatal(err)
	}
	if review.HumanDecision != "approve" {
		t.Fatalf("stored decision = %q, want approve", review.HumanDecision)
	}
}

func TestChangeAcceptLocalRejectsCompletionReporterWithoutHTTP(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "local", "receipt-1")
	reviewID := localReviewID(t, store, selection.Workspace.ID, work.Key)
	closeLocalChangeStore(t, deps, stateDir, store)

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json",
		"--yes",
		"change",
		"accept",
		work.Key,
		"--review-id", reviewID,
	)
	if code == output.ExitOK ||
		!strings.Contains(out.String(), "cannot approve its own delivery") {
		t.Fatalf("exit = %d output = %s", code, out.String())
	}
}

func TestChangeSubmitLocalUsesStoreWithoutHTTP(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type":     "coding_agent.completed",
		"summary":        "All done",
		"context_digest": work.ContextDigest,
		"criteria": []map[string]any{{
			"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "targeted test passed"},
		}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "submit", work.Key, "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var envelope struct {
		Command string `json:"command"`
		OK      bool   `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Command != "change.submit" || !envelope.OK {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func newLocalChangeWork(t *testing.T, deps *command.Deps) (string, *local.Store, local.Selection, local.WorkItem) {
	t.Helper()
	stateDir := t.TempDir()
	store, err := local.Open(stateDir + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := store.Initialize(t.Context(), local.InitInput{WorkspaceName: "Local workspace", DisplayName: "Local developer", Username: "local"})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.PublishArtifact(t.Context(), selection.Workspace.ID, local.ArtifactInput{
		FeatureKey: "CHANGE", RequestType: "new_feature",
		Documents: []local.ArtifactDocumentInput{
			{Path: "spec.md", Role: "spec", Content: []byte("# Change\n\n## Acceptance criteria\n\n1. Status is actionable.")},
			{Path: "plan.md", Role: "plan", Content: []byte("# Plan\n\nImplement and verify.")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RunReadiness(t.Context(), selection.Workspace.ID, artifact.ID); err != nil {
		t.Fatal(err)
	}
	tasks, err := store.ListGateTasks(t.Context(), selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		result := local.GateResultInput{Gate: task.GateKey, GateDigest: task.GateDigest, InputDigest: task.ArtifactDigest, State: "pass", Summary: "reviewed"}
		result.Evaluator.Executor = task.Executor
		if _, err := store.SubmitGateResult(t.Context(), selection.Workspace.ID, task.TaskID, result); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ApproveArtifact(t.Context(), selection.Workspace.ID, artifact.ID, "human", "approved"); err != nil {
		t.Fatal(err)
	}
	feature, err := store.PromoteArtifact(t.Context(), selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(t.Context(), selection.Workspace.ID, local.WorkInput{FeatureRef: feature.Key, Title: "Add change status", AcceptanceCriteria: []string{"Status is actionable."}})
	if err != nil {
		t.Fatal(err)
	}
	return stateDir, store, selection, work
}

func submitLocalChangeDelivery(t *testing.T, store *local.Store, workspaceID string, work local.WorkItem, agent, receipt string) {
	t.Helper()
	_, err := store.SubmitDelivery(t.Context(), workspaceID, work.Key, map[string]any{
		"context_digest": work.ContextDigest,
		"agent":          map[string]any{"name": agent},
		"git_receipt":    map[string]any{"head_revision": receipt},
		"criteria": []any{map[string]any{
			"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "targeted test passed"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func latestLocalReportID(t *testing.T, store *local.Store, workspaceID, ref string) string {
	t.Helper()
	report, err := store.LatestDeliveryReport(t.Context(), workspaceID, ref)
	if err != nil {
		t.Fatal(err)
	}
	return report.ID
}

func closeLocalChangeStore(t *testing.T, deps *command.Deps, stateDir string, store *local.Store) {
	t.Helper()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
}
