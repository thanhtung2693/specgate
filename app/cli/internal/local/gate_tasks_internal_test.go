package local

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLocalGateTaskRejectsExpiryAndReplacesItsResult(t *testing.T) {
	store, selection, artifact := internalGateFixture(t)
	ctx := context.Background()
	dispatch, err := store.DispatchGateTasks(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.GetGateTask(ctx, selection.Workspace.ID, dispatch.CreatedTaskIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE local_gate_tasks SET expires_at = ? WHERE id = ?`, time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano), task.TaskID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SubmitGateResult(ctx, selection.Workspace.ID, task.TaskID, internalGateResult(task)); !errors.Is(err, ErrGateTaskExpired) {
		t.Fatalf("expiry error = %v", err)
	}

	redispatch, err := store.DispatchGateTasks(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(redispatch.CreatedTaskIDs) != 1 || len(redispatch.PendingTaskIDs) != 4 {
		t.Fatalf("redispatch = %#v", redispatch)
	}
	replacement, err := store.GetGateTask(ctx, selection.Workspace.ID, redispatch.CreatedTaskIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.SubmitGateResult(ctx, selection.Workspace.ID, replacement.TaskID, internalGateResult(replacement))
	if err != nil {
		t.Fatal(err)
	}
	secondInput := internalGateResult(replacement)
	secondInput.State = "warn"
	second, err := store.SubmitGateResult(ctx, selection.Workspace.ID, replacement.TaskID, secondInput)
	if err != nil {
		t.Fatal(err)
	}
	if first.ResultID == second.ResultID || second.State != "warn" {
		t.Fatalf("resubmission = %#v", second)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM local_gate_tasks WHERE id = ? AND result_id IS NOT NULL`, replacement.TaskID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("stored result rows = %d", count)
	}
}

func TestLocalDriftGateDerivesStateFromAttestedFindings(t *testing.T) {
	store, selection, artifact := internalGateFixture(t)
	ctx := context.Background()
	if _, err := store.RunReadiness(ctx, selection.Workspace.ID, artifact.ID); err != nil {
		t.Fatal(err)
	}
	tasks, err := store.ListGateTasks(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if _, err := store.SubmitGateResult(ctx, selection.Workspace.ID, task.TaskID, internalGateResult(task)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ApproveArtifact(ctx, selection.Workspace.ID, artifact.ID, "human", "approved"); err != nil {
		t.Fatal(err)
	}
	dispatch, err := store.DispatchGateTasks(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	drift, err := store.GetGateTask(ctx, selection.Workspace.ID, dispatch.CreatedTaskIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	input := internalGateResult(drift)
	if _, err := store.SubmitGateResult(ctx, selection.Workspace.ID, drift.TaskID, input); err == nil ||
		!strings.Contains(err.Error(), "evidence.examined_docs") ||
		!strings.Contains(err.Error(), "evidence.repo_commit") {
		t.Fatalf("missing drift evidence error = %v, want exact required field paths", err)
	}
	input.State = "fail"
	input.Evidence.ExaminedDocs = []string{"app/cli/AGENTS.md"}
	input.Evidence.RepoCommit = "abc123"
	result, err := store.SubmitGateResult(ctx, selection.Workspace.ID, drift.TaskID, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "pass" {
		t.Fatalf("zero-finding drift = %#v", result)
	}
	input.Findings = []json.RawMessage{json.RawMessage(`{"summary":"doc drift"}`)}
	result, err = store.SubmitGateResult(ctx, selection.Workspace.ID, drift.TaskID, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "warn" {
		t.Fatalf("finding drift = %#v", result)
	}
}

func TestLocalConcurrentGateResultSubmissionsRetryBusy(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	seed, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer seed.Close()
	selection, err := seed.Initialize(ctx, InitInput{WorkspaceName: "Alpha", DisplayName: "Human", Username: "human"})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := seed.PublishArtifact(ctx, selection.Workspace.ID, ArtifactInput{FeatureKey: "LOCAL-CONCURRENT", RequestType: "new_feature", Documents: []ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("# Local")}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.DispatchGateTasks(ctx, selection.Workspace.ID, artifact.ID); err != nil {
		t.Fatal(err)
	}
	tasks, err := seed.ListGateTasks(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, len(tasks))
	var wg sync.WaitGroup
	for _, task := range tasks {
		wg.Add(1)
		go func(task GateTask) {
			defer wg.Done()
			<-start
			store, err := Open(path)
			if err == nil {
				defer store.Close()
				_, err = store.SubmitGateResult(ctx, selection.Workspace.ID, task.TaskID, internalGateResult(task))
			}
			errs <- err
		}(task)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent submission failed: %v", err)
		}
	}
}

func internalGateFixture(t *testing.T) (*Store, Selection, Artifact) {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	selection, err := store.Initialize(ctx, InitInput{WorkspaceName: "Alpha", DisplayName: "Human", Username: "human"})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.PublishArtifact(ctx, selection.Workspace.ID, ArtifactInput{FeatureKey: "LOCAL-GATE", RequestType: "new_feature", Documents: []ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("# Local gate")}}})
	if err != nil {
		t.Fatal(err)
	}
	return store, selection, artifact
}

func internalGateResult(task GateTask) GateResultInput {
	input := GateResultInput{Gate: task.GateKey, GateDigest: task.GateDigest, InputDigest: task.ArtifactDigest, State: "pass"}
	input.Evaluator.Executor = task.Executor
	return input
}
