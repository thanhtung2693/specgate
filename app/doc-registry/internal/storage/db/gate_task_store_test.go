package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
		ArtifactID:     artifactID,
		GateKey:        "test/quality@v1",
		GateVersion:    "v1",
		GateDigest:     "sha256:aaaa",
		ArtifactDigest: "sha256:bbbb",
		ProfileDigest:  "sha256:cccc",
		Executor:       policy.ExecutorIDEAgent,
		SkillContent:   "run tests",
		ExpiresAt:      time.Date(2026, 6, 26, 7, 52, 9, 338137085, time.UTC),
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
		if got.ProfileDigest != in.ProfileDigest {
			t.Fatalf("ProfileDigest: got %q want %q", got.ProfileDigest, in.ProfileDigest)
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
			GateDigest: created.GateDigest,
			Executor:   string(created.Executor),
			State:      "pass",
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

// TestPGGateTaskStore_BehaviorMatchesInMem asserts that the Postgres store
// produces the same validation outcomes as the in-memory implementation.
func TestPGGateTaskStore_BehaviorMatchesInMem(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		pgStore := NewPGGateTaskStore(gdb)
		memStore := policy.NewInMemGateTaskStore()

		artID := uuid.NewString()
		in := newGateTask(artID)

		pgTask, err := pgStore.CreateTask(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		memTask, err := memStore.CreateTask(ctx, in)
		if err != nil {
			t.Fatal(err)
		}

		// Fix 4: submit with nil EvaluatorJSON/FindingsJSON to exercise the nil→default path.
		nilResult := policy.GateResultRecord{
			GateDigest:    "sha256:aaaa",
			Executor:      string(policy.ExecutorIDEAgent),
			State:         "pass",
			EvaluatorJSON: nil,
			FindingsJSON:  nil,
		}
		pgResult, pgErr := pgStore.SubmitResult(ctx, pgTask.ID, nilResult)
		memResult, memErr := memStore.SubmitResult(ctx, memTask.ID, nilResult)

		if (pgErr != nil) != (memErr != nil) {
			t.Fatalf("error mismatch: pg=%v mem=%v", pgErr, memErr)
		}
		if pgErr == nil {
			if pgResult.Trust != memResult.Trust {
				t.Fatalf("Trust mismatch: pg=%q mem=%q", pgResult.Trust, memResult.Trust)
			}
			if pgResult.State != memResult.State {
				t.Fatalf("State mismatch: pg=%q mem=%q", pgResult.State, memResult.State)
			}
			// Fix 4: both stores must return identical non-nil defaults for JSON fields.
			if !bytes.Equal(pgResult.EvaluatorJSON, memResult.EvaluatorJSON) {
				t.Fatalf("EvaluatorJSON mismatch: pg=%s mem=%s", pgResult.EvaluatorJSON, memResult.EvaluatorJSON)
			}
			if !bytes.Equal(pgResult.FindingsJSON, memResult.FindingsJSON) {
				t.Fatalf("FindingsJSON mismatch: pg=%s mem=%s", pgResult.FindingsJSON, memResult.FindingsJSON)
			}
		}
	})
}

// TestPGGateTaskStore_BehaviorMatchesInMem_ErrorPaths asserts that the Postgres
// store returns the same domain errors as the in-memory implementation for
// mismatched gate_digest and mismatched executor (Fix 6).
func TestPGGateTaskStore_BehaviorMatchesInMem_ErrorPaths(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		pgStore := NewPGGateTaskStore(gdb)
		memStore := policy.NewInMemGateTaskStore()

		in := newGateTask(uuid.NewString())

		pgTask, err := pgStore.CreateTask(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		memTask, err := memStore.CreateTask(ctx, in)
		if err != nil {
			t.Fatal(err)
		}

		t.Run("stale_digest", func(t *testing.T) {
			staleResult := policy.GateResultRecord{
				GateDigest: "sha256:WRONG",
				Executor:   string(policy.ExecutorIDEAgent),
				State:      "pass",
			}
			_, pgErr := pgStore.SubmitResult(ctx, pgTask.ID, staleResult)
			_, memErr := memStore.SubmitResult(ctx, memTask.ID, staleResult)
			if !errors.Is(pgErr, policy.ErrStaleDigest) {
				t.Fatalf("pg: expected ErrStaleDigest, got %v", pgErr)
			}
			if !errors.Is(memErr, policy.ErrStaleDigest) {
				t.Fatalf("mem: expected ErrStaleDigest, got %v", memErr)
			}
		})

		t.Run("executor_mismatch", func(t *testing.T) {
			mismatchResult := policy.GateResultRecord{
				GateDigest: "sha256:aaaa",                // correct digest
				Executor:   string(policy.ExecutorHuman), // wrong executor (task assigned ide_agent)
				State:      "pass",
			}
			_, pgErr := pgStore.SubmitResult(ctx, pgTask.ID, mismatchResult)
			_, memErr := memStore.SubmitResult(ctx, memTask.ID, mismatchResult)
			if !errors.Is(pgErr, policy.ErrExecutorMismatch) {
				t.Fatalf("pg: expected ErrExecutorMismatch, got %v", pgErr)
			}
			if !errors.Is(memErr, policy.ErrExecutorMismatch) {
				t.Fatalf("mem: expected ErrExecutorMismatch, got %v", memErr)
			}
		})
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
