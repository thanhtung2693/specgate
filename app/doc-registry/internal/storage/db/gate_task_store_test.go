package db

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/policy"
	"github.com/specgate/doc-registry/internal/workboard"
)

// newGateTask builds a minimal valid GateTaskRecord for tests.
func newGateTask(artifactID string) policy.GateTaskRecord {
	return policy.GateTaskRecord{
		WorkspaceID:    "ws-test",
		ArtifactID:     artifactID,
		GateKey:        "test/quality@v1",
		GateVersion:    "v1",
		GateDigest:     "sha256:aaaa",
		ArtifactDigest: "sha256:bbbb",
		PolicyDigest:   "sha256:cccc",
		Executor:       policy.ExecutorIDEAgent,
		SkillContent:   "run tests",
		ExpiresAt:      time.Now().UTC().Add(time.Hour),
	}
}

func TestPGGateTaskStore_CreateGetRoundTrip(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		store := NewPGGateTaskStore(gdb)
		artID := uuid.NewString()
		in := newGateTask(artID)

		created, err := store.CreateTask(ctx, in)
		if err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
		if created.ID == "" {
			t.Fatal("created.ID must be assigned")
		}
		if created.ArtifactID != artID {
			t.Fatalf("ArtifactID mismatch: got %q want %q", created.ArtifactID, artID)
		}

		got, err := store.GetTask(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetTask: %v", err)
		}
		if got.ID != created.ID {
			t.Fatalf("ID: got %q want %q", got.ID, created.ID)
		}
		if got.GateDigest != in.GateDigest {
			t.Fatalf("GateDigest: got %q want %q", got.GateDigest, in.GateDigest)
		}
		if got.Executor != in.Executor {
			t.Fatalf("Executor: got %q want %q", got.Executor, in.Executor)
		}
		if got.SkillContent != in.SkillContent {
			t.Fatalf("SkillContent: got %q want %q", got.SkillContent, in.SkillContent)
		}
		// Fix 3: assert all remaining fields round-trip correctly.
		if got.GateKey != in.GateKey {
			t.Fatalf("GateKey: got %q want %q", got.GateKey, in.GateKey)
		}
		if got.GateVersion != in.GateVersion {
			t.Fatalf("GateVersion: got %q want %q", got.GateVersion, in.GateVersion)
		}
		if got.ArtifactDigest != in.ArtifactDigest {
			t.Fatalf("ArtifactDigest: got %q want %q", got.ArtifactDigest, in.ArtifactDigest)
		}
		if got.PolicyDigest != in.PolicyDigest {
			t.Fatalf("PolicyDigest: got %q want %q", got.PolicyDigest, in.PolicyDigest)
		}
		// Postgres TIMESTAMPTZ stores microsecond precision; the store normalizes
		// returned records to that contract so created/get round-trips agree.
		wantExpiresAt := in.ExpiresAt.UTC().Truncate(time.Microsecond)
		if !created.ExpiresAt.UTC().Equal(wantExpiresAt) {
			t.Fatalf("created ExpiresAt: got %v want %v", created.ExpiresAt.UTC(), wantExpiresAt)
		}
		if !got.ExpiresAt.UTC().Equal(wantExpiresAt) {
			t.Fatalf("ExpiresAt: got %v want %v", got.ExpiresAt.UTC(), wantExpiresAt)
		}
	})
}

func TestPGGateTaskStore_CreateTaskIfCurrentMissingIsAtomic(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		store := NewPGGateTaskStore(gdb)
		task := newGateTask(uuid.NewString())
		ctx := policy.WithWorkspace(context.Background(), task.WorkspaceID)
		var created atomic.Int32
		var wg sync.WaitGroup
		for range 12 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, wasCreated, err := store.CreateTaskIfCurrentMissing(ctx, task, time.Now().UTC())
				if err != nil {
					t.Errorf("CreateTaskIfCurrentMissing: %v", err)
					return
				}
				if wasCreated {
					created.Add(1)
				}
			}()
		}
		wg.Wait()

		if created.Load() != 1 {
			t.Fatalf("created = %d, want 1", created.Load())
		}
		tasks, err := store.ListTasksForArtifact(ctx, task.ArtifactID)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 1 {
			t.Fatalf("tasks = %#v, want one current task", tasks)
		}
	})
}

func TestPGGateTaskStore_CreateTaskIfCurrentMissingIdentityIncludesWorkspaceWithoutScopedContext(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		store := NewPGGateTaskStore(gdb)
		taskA := newGateTask(uuid.NewString())
		taskA.WorkspaceID = "ws-a"
		taskB := taskA
		taskB.WorkspaceID = "ws-b"

		if _, created, err := store.CreateTaskIfCurrentMissing(context.Background(), taskA, time.Now().UTC()); err != nil || !created {
			t.Fatalf("workspace A create = %v, %v; want true, nil", created, err)
		}
		if _, created, err := store.CreateTaskIfCurrentMissing(context.Background(), taskB, time.Now().UTC()); err != nil || !created {
			t.Fatalf("workspace B create = %v, %v; want true, nil", created, err)
		}
		tasks, err := store.ListTasksForArtifact(context.Background(), taskA.ArtifactID)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 2 {
			t.Fatalf("tasks = %#v, want one per workspace", tasks)
		}
	})
}

func TestPGGateTaskStore_GetTask_NotFound(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		store := NewPGGateTaskStore(gdb)

		_, err := store.GetTask(ctx, uuid.NewString())
		if !errors.Is(err, policy.ErrGateTaskNotFound) {
			t.Fatalf("expected ErrGateTaskNotFound, got %v", err)
		}
	})
}

func TestPGGateTaskStore_WorkspaceScope(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		store := NewPGGateTaskStore(gdb)
		artifactID := uuid.NewString()
		ctxA := policy.WithWorkspace(context.Background(), "workspace-a")
		ctxB := policy.WithWorkspace(context.Background(), "workspace-b")

		taskA := newGateTask(artifactID)
		taskA.WorkspaceID = "workspace-a"
		_, err := store.CreateTask(ctxA, taskA)
		if err != nil {
			t.Fatal(err)
		}
		taskB := newGateTask(artifactID)
		taskB.WorkspaceID = "workspace-b"
		createdB, err := store.CreateTask(ctxB, taskB)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := store.GetTask(ctxA, createdB.ID); !errors.Is(err, policy.ErrGateTaskNotFound) {
			t.Fatalf("cross-workspace get error = %v, want ErrGateTaskNotFound", err)
		}
		pending, err := store.ListTasksForArtifact(ctxA, artifactID)
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) != 1 || pending[0].WorkspaceID != "workspace-a" {
			t.Fatalf("workspace-a pending tasks = %+v", pending)
		}
		_, err = store.SubmitResult(ctxA, createdB.ID, policy.GateResultRecord{
			GateDigest:  createdB.GateDigest,
			InputDigest: createdB.ArtifactDigest,
			Executor:    string(policy.ExecutorIDEAgent),
			State:       "pass",
		})
		if !errors.Is(err, policy.ErrGateTaskNotFound) {
			t.Fatalf("cross-workspace submit error = %v, want ErrGateTaskNotFound", err)
		}
	})
}

func TestPGGateTaskStore_ListTasksForArtifact(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		store := NewPGGateTaskStore(gdb)
		artA := uuid.NewString()
		artB := uuid.NewString()

		// Create two tasks for artA and one for artB.
		_, err := store.CreateTask(ctx, newGateTask(artA))
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.CreateTask(ctx, newGateTask(artA))
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.CreateTask(ctx, newGateTask(artB))
		if err != nil {
			t.Fatal(err)
		}

		listA, err := store.ListTasksForArtifact(ctx, artA)
		if err != nil {
			t.Fatalf("ListTasksForArtifact: %v", err)
		}
		if len(listA) != 2 {
			t.Fatalf("expected 2 tasks for artA, got %d", len(listA))
		}
		for _, task := range listA {
			if task.ArtifactID != artA {
				t.Fatalf("unexpected ArtifactID %q in artA list", task.ArtifactID)
			}
		}

		listB, err := store.ListTasksForArtifact(ctx, artB)
		if err != nil {
			t.Fatalf("ListTasksForArtifact artB: %v", err)
		}
		if len(listB) != 1 {
			t.Fatalf("expected 1 task for artB, got %d", len(listB))
		}

		// Non-existent artifact returns empty slice (not error).
		listNone, err := store.ListTasksForArtifact(ctx, uuid.NewString())
		if err != nil {
			t.Fatalf("ListTasksForArtifact missing: %v", err)
		}
		if len(listNone) != 0 {
			t.Fatalf("expected 0 tasks for unknown artifact, got %d", len(listNone))
		}
	})
}

func TestPGGateTaskStore_ListTasksForArtifactExcludesSubmittedTasks(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		store := NewPGGateTaskStore(gdb)
		artID := uuid.NewString()

		created, err := store.CreateTask(ctx, newGateTask(artID))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.SubmitResult(ctx, created.ID, policy.GateResultRecord{
			GateDigest:  created.GateDigest,
			InputDigest: created.ArtifactDigest,
			Executor:    string(created.Executor),
			State:       "pass",
		}); err != nil {
			t.Fatal(err)
		}

		tasks, err := store.ListTasksForArtifact(ctx, artID)
		if err != nil {
			t.Fatalf("ListTasksForArtifact: %v", err)
		}
		if len(tasks) != 0 {
			t.Fatalf("len(tasks) = %d, want 0 pending tasks", len(tasks))
		}
		if _, err := store.GetTask(ctx, created.ID); err != nil {
			t.Fatalf("GetTask should still load submitted task: %v", err)
		}
	})
}

func TestPGGateTaskStore_ExpiredUnsubmittedTaskIsNotCurrent(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		store := NewPGGateTaskStore(gdb)
		task := newGateTask(uuid.NewString())
		task.ExpiresAt = time.Now().UTC().Add(-time.Second)
		created, err := store.CreateTask(context.Background(), task)
		if err != nil {
			t.Fatal(err)
		}
		pending, err := store.ListTasksForArtifact(context.Background(), created.ArtifactID)
		if err != nil || len(pending) != 0 {
			t.Fatalf("pending = %+v, err = %v; want expired task omitted", pending, err)
		}
	})
}

func TestPGGateTaskStore_SubmitResult_DigestMismatch(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		store := NewPGGateTaskStore(gdb)

		created, err := store.CreateTask(ctx, newGateTask(uuid.NewString()))
		if err != nil {
			t.Fatal(err)
		}

		_, err = store.SubmitResult(ctx, created.ID, policy.GateResultRecord{
			GateDigest:    "sha256:wrong",
			InputDigest:   created.ArtifactDigest,
			Executor:      string(policy.ExecutorIDEAgent),
			State:         "pass",
			EvaluatorJSON: json.RawMessage(`{}`),
			FindingsJSON:  json.RawMessage(`[]`),
		})
		if !errors.Is(err, policy.ErrStaleDigest) {
			t.Fatalf("expected ErrStaleDigest, got %v", err)
		}
	})
}

func TestPGGateTaskStore_SubmitResult_ExecutorMismatch(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		store := NewPGGateTaskStore(gdb)

		task := newGateTask(uuid.NewString())
		task.Executor = policy.ExecutorIDEAgent
		created, err := store.CreateTask(ctx, task)
		if err != nil {
			t.Fatal(err)
		}

		_, err = store.SubmitResult(ctx, created.ID, policy.GateResultRecord{
			GateDigest:    "sha256:aaaa",
			InputDigest:   created.ArtifactDigest,
			Executor:      string(policy.ExecutorHuman), // wrong executor
			State:         "pass",
			EvaluatorJSON: json.RawMessage(`{}`),
			FindingsJSON:  json.RawMessage(`[]`),
		})
		if !errors.Is(err, policy.ErrExecutorMismatch) {
			t.Fatalf("expected ErrExecutorMismatch, got %v", err)
		}
	})
}

func TestPGGateTaskStore_SubmitResult_TrustStamping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		executor  policy.Executor
		wantTrust policy.Trust
	}{
		{policy.ExecutorIDEAgent, policy.TrustAgentAttested},
		{policy.ExecutorPlatformLLM, policy.TrustPlatformEvaluated},
		{policy.ExecutorHuman, policy.TrustHumanDecision},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.executor), func(t *testing.T) {
			t.Parallel()
			forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
				ctx := context.Background()
				store := NewPGGateTaskStore(gdb)

				task := newGateTask(uuid.NewString())
				task.Executor = tc.executor
				created, err := store.CreateTask(ctx, task)
				if err != nil {
					t.Fatal(err)
				}

				result, err := store.SubmitResult(ctx, created.ID, policy.GateResultRecord{
					GateDigest:    "sha256:aaaa",
					InputDigest:   created.ArtifactDigest,
					Executor:      string(tc.executor),
					State:         "pass",
					EvaluatorJSON: json.RawMessage(`{}`),
					FindingsJSON:  json.RawMessage(`[]`),
				})
				if err != nil {
					t.Fatalf("SubmitResult: %v", err)
				}
				if result.Trust != tc.wantTrust {
					t.Fatalf("Trust: got %q want %q", result.Trust, tc.wantTrust)
				}
				if result.ID == "" {
					t.Fatal("result.ID must be assigned")
				}
				if result.TaskID != created.ID {
					t.Fatalf("TaskID: got %q want %q", result.TaskID, created.ID)
				}
				if result.SubmittedAt.IsZero() {
					t.Fatal("SubmittedAt must be set")
				}
			})
		})
	}
}

func TestPGGateTaskStore_SubmitResult_TaskNotFound(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		store := NewPGGateTaskStore(gdb)

		_, err := store.SubmitResult(ctx, uuid.NewString(), policy.GateResultRecord{
			GateDigest:    "sha256:aaaa",
			Executor:      string(policy.ExecutorIDEAgent),
			State:         "pass",
			EvaluatorJSON: json.RawMessage(`{}`),
			FindingsJSON:  json.RawMessage(`[]`),
		})
		if !errors.Is(err, policy.ErrGateTaskNotFound) {
			t.Fatalf("expected ErrGateTaskNotFound, got %v", err)
		}
	})
}

// TestPGGateTaskStore_SubmitResult_ReplacesPriorResult verifies one-result-per-task
// semantics: a resubmission replaces the prior result rather than accumulating rows.
func TestPGGateTaskStore_SubmitResult_ReplacesPriorResult(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		store := NewPGGateTaskStore(gdb)

		created, err := store.CreateTask(ctx, newGateTask(uuid.NewString()))
		if err != nil {
			t.Fatal(err)
		}
		mk := func(state string) policy.GateResultRecord {
			return policy.GateResultRecord{
				GateDigest:    "sha256:aaaa",
				InputDigest:   created.ArtifactDigest,
				Executor:      string(policy.ExecutorIDEAgent),
				State:         state,
				EvaluatorJSON: json.RawMessage(`{}`),
				FindingsJSON:  json.RawMessage(`[]`),
			}
		}
		if _, err := store.SubmitResult(ctx, created.ID, mk("pass")); err != nil {
			t.Fatalf("first submit: %v", err)
		}
		latest, err := store.SubmitResult(ctx, created.ID, mk("fail"))
		if err != nil {
			t.Fatalf("second submit: %v", err)
		}
		if latest.State != "fail" {
			t.Fatalf("latest State: got %q want fail", latest.State)
		}

		// Result runs are keyed by the task id in gate_runs, so exactly one row
		// exists per task and it carries the latest verdict.
		var run workboard.GateRun
		if err := gdb.First(&run, "id = ?", created.ID).Error; err != nil {
			t.Fatalf("load result run: %v", err)
		}
		if run.State != workboard.NextActionStateFail {
			t.Fatalf("result run state = %q, want fail (last-wins)", run.State)
		}
		var count int64
		if err := gdb.Model(&workboard.GateRun{}).
			Where("subject_kind = ? AND subject_id = ?", workboard.GateRunSubjectArtifact, created.ArtifactID).
			Count(&count).Error; err != nil {
			t.Fatalf("count: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected exactly 1 result row per task, got %d", count)
		}
	})
}

// TestPGGateTaskStore_CreateTask_RejectsZeroExpiresAt covers the ExpiresAt guard.
func TestPGGateTaskStore_CreateTask_RejectsZeroExpiresAt(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		store := NewPGGateTaskStore(gdb)
		task := newGateTask(uuid.NewString())
		task.ExpiresAt = time.Time{}
		if _, err := store.CreateTask(ctx, task); !errors.Is(err, policy.ErrExpiresAtRequired) {
			t.Fatalf("expected ErrExpiresAtRequired, got %v", err)
		}
	})
}
