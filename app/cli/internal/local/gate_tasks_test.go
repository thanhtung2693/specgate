package local_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/local"
)

func TestLocalGateTasksArePersistedIdempotentAndWorkspaceScoped(t *testing.T) {
	t.Parallel()
	store, selection, artifact := localGateFixture(t)
	ctx := context.Background()

	first, err := store.DispatchGateTasks(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.CreatedTaskIDs) != 3 || len(first.PendingTaskIDs) != 3 {
		t.Fatalf("first dispatch = %#v", first)
	}

	second, err := store.DispatchGateTasks(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.CreatedTaskIDs) != 0 || len(second.SkippedGateKeys) != 3 || len(second.PendingTaskIDs) != 3 {
		t.Fatalf("second dispatch = %#v", second)
	}

	tasks, err := store.ListGateTasks(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("tasks = %#v", tasks)
	}
	for _, task := range tasks {
		if task.Executor != "ide_agent" || task.ArtifactDigest != artifact.SnapshotDigest || task.PolicyDigest != artifact.PolicyDigest || task.GateDigest == "" || task.SkillContent == "" {
			t.Fatalf("unbound task = %#v", task)
		}
	}

	other, err := store.CreateWorkspace(ctx, "Beta")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetGateTask(ctx, other.ID, first.CreatedTaskIDs[0]); !errors.Is(err, local.ErrGateTaskNotFound) {
		t.Fatalf("cross-workspace task error = %v", err)
	}
}

func TestLocalGateResultRejectsUntrustedBindings(t *testing.T) {
	t.Parallel()
	store, selection, artifact := localGateFixture(t)
	ctx := context.Background()
	dispatched, err := store.DispatchGateTasks(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.GetGateTask(ctx, selection.Workspace.ID, dispatched.CreatedTaskIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	valid := validLocalGateResult(task)

	// Rejection must name the offending field. "gate task result is invalid"
	// leaves an agent with four candidates and no way to choose between them.
	cases := []struct {
		name string
		edit func(*local.GateResultInput)
		says string
	}{
		{"wrong gate", func(in *local.GateResultInput) { in.Gate = "scope_clear" }, "gate"},
		{"wrong gate digest", func(in *local.GateResultInput) { in.GateDigest = "sha256:wrong" }, "gate_digest"},
		{"wrong input digest", func(in *local.GateResultInput) { in.InputDigest = "sha256:wrong" }, "input_digest"},
		{"wrong executor", func(in *local.GateResultInput) { in.Evaluator.Executor = "platform_llm" }, "evaluator.executor"},
		{"invalid state", func(in *local.GateResultInput) { in.State = "passed" }, "state"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := valid
			tc.edit(&input)
			_, err := store.SubmitGateResult(ctx, selection.Workspace.ID, task.TaskID, input)
			if !errors.Is(err, local.ErrGateTaskInvalid) {
				t.Fatalf("error = %v", err)
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("error %q does not name the offending field %q", err, tc.says)
			}
			if tc.name != "invalid state" {
				want := "specgate gates tasks list " + task.ArtifactID
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q lacks exact repair command %q", err, want)
				}
			}
		})
	}

	result, err := store.SubmitGateResult(ctx, selection.Workspace.ID, task.TaskID, valid)
	if err != nil {
		t.Fatal(err)
	}
	if result.Trust != "agent_attested" || result.State != "pass" || result.ResultID == "" {
		t.Fatalf("stamped result = %#v", result)
	}
}

func TestLocalReadinessBlocksApprovalUntilSemanticResultsExist(t *testing.T) {
	t.Parallel()
	store, selection, artifact := localGateFixture(t)
	ctx := context.Background()

	run, err := store.RunReadiness(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Aggregate != "not_run" {
		t.Fatalf("aggregate = %q, want not_run", run.Aggregate)
	}
	if err := store.ApproveArtifact(ctx, selection.Workspace.ID, artifact.ID, "human", ""); err == nil {
		t.Fatal("approval succeeded with pending semantic tasks")
	}

	tasks, err := store.ListGateTasks(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if _, err := store.SubmitGateResult(ctx, selection.Workspace.ID, task.TaskID, validLocalGateResult(task)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ApproveArtifact(ctx, selection.Workspace.ID, artifact.ID, "human", "approved"); err != nil {
		t.Fatal(err)
	}

	dispatched, err := store.DispatchGateTasks(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(dispatched.CreatedTaskIDs) != 1 || len(dispatched.PendingTaskIDs) != 1 {
		t.Fatalf("approved dispatch = %#v", dispatched)
	}
	drift, err := store.GetGateTask(ctx, selection.Workspace.ID, dispatched.CreatedTaskIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if drift.GateKey != "spec_repo_drift" {
		t.Fatalf("pickup gate = %#v", drift)
	}

	input := validLocalGateResult(drift)
	input.Evidence.ExaminedDocs = []string{"spec.md", "plan.md"}
	input.Evidence.RepoCommit = "checkout-before-new-work"
	if _, err := store.SubmitGateResult(ctx, selection.Workspace.ID, drift.TaskID, input); err != nil {
		t.Fatal(err)
	}
	// Dispatch has no checkout input. A completed drift verdict therefore cannot
	// establish that the current checkout (including uncommitted work) is clean.
	refreshed, err := store.DispatchGateTasks(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(refreshed.CreatedTaskIDs) != 1 || len(refreshed.PendingTaskIDs) != 1 || len(refreshed.SkippedGateKeys) != 3 {
		t.Fatalf("dispatch after completed drift = %#v, want fresh drift and reused document gates", refreshed)
	}
	freshID := refreshed.CreatedTaskIDs[0]
	if freshID == drift.TaskID {
		t.Fatal("completed drift task was reused")
	}
	pending, err := store.ListGateTasks(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].TaskID != freshID || pending[0].GateKey != "spec_repo_drift" {
		t.Fatalf("pending tasks = %#v, want only fresh drift task", pending)
	}
	repeated, err := store.DispatchGateTasks(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(repeated.CreatedTaskIDs) != 0 || len(repeated.PendingTaskIDs) != 1 || repeated.PendingTaskIDs[0] != freshID {
		t.Fatalf("pending drift redispatch = %#v, want same pending task", repeated)
	}
	run, err = store.RunReadiness(ctx, selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	check := run.Checks["spec_repo_drift"].(map[string]any)
	if check["state"] != "not_run" || check["task_id"] != freshID {
		t.Fatalf("drift check = %#v, want fresh pending task instead of old pass", check)
	}
}

func localGateFixture(t *testing.T) (*local.Store, local.Selection, local.Artifact) {
	t.Helper()
	store, err := local.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	selection, err := store.Initialize(ctx, local.InitInput{WorkspaceName: "Alpha", DisplayName: "Human", Username: "human"})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.PublishArtifact(ctx, selection.Workspace.ID, local.ArtifactInput{
		FeatureKey: "LOCAL-GATE", RequestType: "new_feature",
		Documents: localGovernedDocuments("# Local gate\n\nAcceptance criteria: works"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return store, selection, artifact
}

func localGovernedDocuments(spec string) []local.ArtifactDocumentInput {
	return []local.ArtifactDocumentInput{
		{Path: "spec.md", Role: "spec", Content: []byte(spec)},
		{Path: "plan.md", Role: "plan", Content: []byte("# Plan\n\nImplement and verify the approved criteria.")},
	}
}

func validLocalGateResult(task local.GateTask) local.GateResultInput {
	result := local.GateResultInput{Gate: task.GateKey, GateDigest: task.GateDigest, InputDigest: task.ArtifactDigest, State: "pass", Summary: "reviewed"}
	result.Evaluator.Executor = task.Executor
	return result
}

func completeLocalGateTasks(t *testing.T, store *local.Store, selection local.Selection, artifactID string) {
	t.Helper()
	tasks, err := store.ListGateTasks(context.Background(), selection.Workspace.ID, artifactID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if _, err := store.SubmitGateResult(context.Background(), selection.Workspace.ID, task.TaskID, validLocalGateResult(task)); err != nil {
			t.Fatal(err)
		}
	}
}
