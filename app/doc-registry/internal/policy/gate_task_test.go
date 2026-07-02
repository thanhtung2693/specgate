package policy_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/policy"
)

func TestCreateTask_AssignsID(t *testing.T) {
	t.Parallel()
	store := policy.NewInMemGateTaskStore()
	task, err := store.CreateTask(context.Background(), policy.GateTaskRecord{
		ArtifactID:     "art-1",
		GateKey:        "acme/check@1",
		GateDigest:     "sha256:abc",
		ArtifactDigest: "sha256:def",
		ProfileDigest:  "sha256:ghi",
		Executor:       policy.ExecutorIDEAgent,
		ExpiresAt:      time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.ID == "" {
		t.Error("task ID must be assigned")
	}
}

func TestGetTask_NotFound(t *testing.T) {
	t.Parallel()
	store := policy.NewInMemGateTaskStore()
	_, err := store.GetTask(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for unknown task")
	}
}

func TestListTasksForArtifact_Empty(t *testing.T) {
	t.Parallel()
	store := policy.NewInMemGateTaskStore()
	tasks, err := store.ListTasksForArtifact(context.Background(), "art-none")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestSubmitResult_StaleDigest_Rejected(t *testing.T) {
	t.Parallel()
	store := policy.NewInMemGateTaskStore()
	task, err := store.CreateTask(context.Background(), policy.GateTaskRecord{
		ArtifactID:     "art-1",
		GateKey:        "acme/check@1",
		GateDigest:     "sha256:correct",
		ArtifactDigest: "sha256:def",
		ProfileDigest:  "sha256:ghi",
		Executor:       policy.ExecutorIDEAgent,
		ExpiresAt:      time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SubmitResult(context.Background(), task.ID, policy.GateResultRecord{
		GateDigest: "sha256:STALE",
		Executor:   string(policy.ExecutorIDEAgent),
		State:      "pass",
	})
	if err == nil {
		t.Fatal("expected error for stale gate digest")
	}
}

func TestSubmitResult_IDEAgent_StampsAgentAttested(t *testing.T) {
	t.Parallel()
	store := policy.NewInMemGateTaskStore()
	task, err := store.CreateTask(context.Background(), policy.GateTaskRecord{
		ArtifactID:     "art-1",
		GateKey:        "acme/check@1",
		GateDigest:     "sha256:correct",
		ArtifactDigest: "sha256:def",
		ProfileDigest:  "sha256:ghi",
		Executor:       policy.ExecutorIDEAgent,
		ExpiresAt:      time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.SubmitResult(context.Background(), task.ID, policy.GateResultRecord{
		GateDigest: "sha256:correct",
		Executor:   string(policy.ExecutorIDEAgent),
		State:      "pass",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Trust != policy.TrustAgentAttested {
		t.Errorf("trust %q, want agent_attested", result.Trust)
	}
}

func TestListTasksForArtifact_ExcludesSubmittedTasks(t *testing.T) {
	t.Parallel()
	store := policy.NewInMemGateTaskStore()
	task, err := store.CreateTask(context.Background(), policy.GateTaskRecord{
		ArtifactID:     "art-1",
		GateKey:        "acme/check@1",
		GateDigest:     "sha256:correct",
		ArtifactDigest: "sha256:def",
		ProfileDigest:  "sha256:ghi",
		Executor:       policy.ExecutorIDEAgent,
		ExpiresAt:      time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SubmitResult(context.Background(), task.ID, policy.GateResultRecord{
		GateDigest: "sha256:correct",
		Executor:   string(policy.ExecutorIDEAgent),
		State:      "pass",
	}); err != nil {
		t.Fatal(err)
	}
	tasks, err := store.ListTasksForArtifact(context.Background(), "art-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("len(tasks) = %d, want 0 pending tasks", len(tasks))
	}
	if _, err := store.GetTask(context.Background(), task.ID); err != nil {
		t.Fatalf("GetTask should still load submitted task: %v", err)
	}
}

func TestSubmitResult_PlatformLLM_StampsPlatformEvaluated(t *testing.T) {
	t.Parallel()
	store := policy.NewInMemGateTaskStore()
	task, err := store.CreateTask(context.Background(), policy.GateTaskRecord{
		ArtifactID:     "art-1",
		GateKey:        "acme/check@1",
		GateDigest:     "sha256:correct",
		ArtifactDigest: "sha256:def",
		ProfileDigest:  "sha256:ghi",
		Executor:       policy.ExecutorPlatformLLM,
		ExpiresAt:      time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.SubmitResult(context.Background(), task.ID, policy.GateResultRecord{
		GateDigest: "sha256:correct",
		Executor:   string(policy.ExecutorPlatformLLM),
		State:      "pass",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Trust != policy.TrustPlatformEvaluated {
		t.Errorf("trust %q, want platform_evaluated", result.Trust)
	}
}

func TestSubmitResult_Human_StampsHumanDecision(t *testing.T) {
	t.Parallel()
	store := policy.NewInMemGateTaskStore()
	task, err := store.CreateTask(context.Background(), policy.GateTaskRecord{
		ArtifactID:     "art-1",
		GateKey:        "acme/check@1",
		GateDigest:     "sha256:correct",
		ArtifactDigest: "sha256:def",
		ProfileDigest:  "sha256:ghi",
		Executor:       policy.ExecutorHuman,
		ExpiresAt:      time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.SubmitResult(context.Background(), task.ID, policy.GateResultRecord{
		GateDigest: "sha256:correct",
		Executor:   string(policy.ExecutorHuman),
		State:      "pass",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Trust != policy.TrustHumanDecision {
		t.Errorf("trust %q, want human_decision", result.Trust)
	}
}

func TestSubmitResult_ExecutorMismatch_Rejected(t *testing.T) {
	t.Parallel()
	store := policy.NewInMemGateTaskStore()
	task, err := store.CreateTask(context.Background(), policy.GateTaskRecord{
		ArtifactID:     "a1",
		GateKey:        "k@1",
		GateVersion:    "1",
		GateDigest:     "sha256:d1",
		ArtifactDigest: "sha256:a1",
		ProfileDigest:  "sha256:p1",
		Executor:       policy.ExecutorIDEAgent,
		ExpiresAt:      time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SubmitResult(context.Background(), task.ID, policy.GateResultRecord{
		GateDigest: "sha256:d1",
		State:      "pass",
		Executor:   string(policy.ExecutorHuman), // mismatched
	})
	if err == nil || !strings.Contains(err.Error(), "executor mismatch") {
		t.Fatalf("expected executor mismatch error, got %v", err)
	}
}
