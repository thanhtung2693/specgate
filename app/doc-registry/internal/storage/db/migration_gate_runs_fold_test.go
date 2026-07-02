package db

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/google/uuid"

	"github.com/specgate/doc-registry/internal/policy"
	"github.com/specgate/doc-registry/internal/workboard"
)

// TestGateRunsFreshSchema verifies the unified gate_runs table exists with the
// expected columns and that the legacy per-concern stores are gone on a fresh
// install. Per spec §3.2 "Table: gate_runs".
func TestGateRunsFreshSchema(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		cols := tableColumns(t, "gate_runs", gdb, name)
		for _, want := range []string{
			"id", "subject_kind", "subject_id", "gate", "state", "hint",
			"executor", "proposal_ref", "evidence_json", "created_at",
		} {
			if !cols[want] {
				t.Fatalf("gate_runs missing column %q (have %v)", want, cols)
			}
		}
		for _, legacy := range []string{"workboard_gate_runs", "artifact_readiness_runs", "gate_results"} {
			if cols := tableColumns(t, legacy, gdb, name); len(cols) != 0 {
				t.Fatalf("legacy table %q must not exist in fresh schema", legacy)
			}
		}
		// gate_tasks stays: dispatch/queue state, not run history.
		if cols := tableColumns(t, "gate_tasks", gdb, name); len(cols) == 0 {
			t.Fatal("gate_tasks table must still exist")
		}
	})
}

// TestGateRunsFoldFromLegacyTables simulates a live dev DB that still has the
// pre-unification tables: recreate them with their legacy DDL, seed rows, then
// re-run the migration (the runner re-applies the file on every boot) and
// assert the rows folded into gate_runs and the legacy tables were dropped.
// Re-running the migration once more must be a no-op (idempotent fold).
func TestGateRunsFoldFromLegacyTables(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Microsecond)

		// Recreate the legacy tables exactly as 0001_init used to define them.
		legacyDDL := []string{
			`CREATE TABLE workboard_gate_runs (
				id TEXT PRIMARY KEY,
				change_request_id TEXT NOT NULL,
				gate TEXT NOT NULL,
				state TEXT NOT NULL,
				hint TEXT NOT NULL,
				proposal_ref TEXT,
				evidence_json TEXT NOT NULL DEFAULT '{}',
				created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)`,
			`CREATE TABLE artifact_readiness_runs (
				id TEXT PRIMARY KEY,
				artifact_id TEXT NOT NULL,
				gate TEXT NOT NULL,
				state TEXT NOT NULL,
				hint TEXT NOT NULL DEFAULT '',
				evidence_json TEXT NOT NULL DEFAULT '{}',
				created_at TIMESTAMPTZ NOT NULL
			)`,
			`CREATE TABLE gate_results (
				id UUID PRIMARY KEY,
				task_id UUID NOT NULL REFERENCES gate_tasks(id),
				gate_digest TEXT NOT NULL,
				executor TEXT NOT NULL,
				state TEXT NOT NULL,
				trust TEXT NOT NULL,
				evaluator JSONB NOT NULL DEFAULT '{}',
				findings JSONB NOT NULL DEFAULT '[]',
				submitted_at TIMESTAMPTZ NOT NULL DEFAULT now()
			)`,
		}
		for _, ddl := range legacyDDL {
			if err := gdb.Exec(ddl).Error; err != nil {
				t.Fatalf("recreate legacy table: %v", err)
			}
		}

		// Seed one row per legacy store. The gate result needs a real task row.
		store := NewPGGateTaskStore(gdb)
		artID := uuid.NewString()
		task, err := store.CreateTask(ctx, policy.GateTaskRecord{
			ArtifactID:     artID,
			GateKey:        "spec_completeness",
			GateVersion:    "v1",
			GateDigest:     "sha256:fold",
			ArtifactDigest: "sha256:art",
			ProfileDigest:  "sha256:prof",
			Executor:       policy.ExecutorIDEAgent,
			ExpiresAt:      now.Add(time.Hour),
		})
		if err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
		seeds := []string{
			`INSERT INTO workboard_gate_runs (id, change_request_id, gate, state, hint, proposal_ref, evidence_json, created_at)
			 VALUES ('wgr-1', 'cr-fold', 'scope_clear', 'pass', 'ok', NULL, '{"k":1}', NOW())`,
			`INSERT INTO artifact_readiness_runs (id, artifact_id, gate, state, hint, evidence_json, created_at)
			 VALUES ('arr-1', '` + artID + `', 'spec_completeness', 'warn', 'thin', 'free text evidence', NOW())`,
			`INSERT INTO gate_results (id, task_id, gate_digest, executor, state, trust, evaluator, findings, submitted_at)
			 VALUES (gen_random_uuid(), '` + task.ID + `', 'sha256:fold', 'ide_agent', 'fail', 'agent_attested', '{"executor":"ide_agent"}', '[]', NOW())`,
		}
		for _, ins := range seeds {
			if err := gdb.Exec(ins).Error; err != nil {
				t.Fatalf("seed legacy row: %v", err)
			}
		}

		// Re-apply the migration file: the fold must move rows and drop tables.
		if err := migratePostgres(gdb); err != nil {
			t.Fatalf("migratePostgres (fold): %v", err)
		}
		for _, legacy := range []string{"workboard_gate_runs", "artifact_readiness_runs", "gate_results"} {
			if cols := tableColumns(t, legacy, gdb, name); len(cols) != 0 {
				t.Fatalf("legacy table %q must be dropped after fold", legacy)
			}
		}

		var total int64
		if err := gdb.Model(&workboard.GateRun{}).Count(&total).Error; err != nil {
			t.Fatalf("count gate_runs: %v", err)
		}
		if total != 3 {
			t.Fatalf("gate_runs count = %d, want 3 folded rows", total)
		}

		// Workboard row: change_request subject, platform executor.
		repo := NewWorkBoardRepository(gdb)
		crRuns, err := repo.ListGateRuns(ctx, "cr-fold", 10)
		if err != nil {
			t.Fatalf("ListGateRuns: %v", err)
		}
		if len(crRuns) != 1 || crRuns[0].ID != "wgr-1" || crRuns[0].Gate != "scope_clear" {
			t.Fatalf("folded workboard run mismatch: %+v", crRuns)
		}

		// Readiness row: artifact subject, platform executor; the ide_agent
		// result row for the same artifact must not leak into the readiness list.
		artRepo := NewRepository(gdb)
		readiness, err := artRepo.ListReadinessRuns(ctx, artID, 10)
		if err != nil {
			t.Fatalf("ListReadinessRuns: %v", err)
		}
		if len(readiness) != 1 || readiness[0].ID != "arr-1" {
			t.Fatalf("folded readiness rows mismatch: %+v", readiness)
		}
		if readiness[0].EvidenceJSON != "free text evidence" {
			t.Fatalf("readiness evidence_json = %q, want free text preserved", readiness[0].EvidenceJSON)
		}

		// Gate result row: keyed by task id so the task no longer lists as pending.
		var resultRun workboard.GateRun
		if err := gdb.First(&resultRun, "id = ?", task.ID).Error; err != nil {
			t.Fatalf("folded gate result row (id = task id): %v", err)
		}
		if resultRun.SubjectKind != workboard.GateRunSubjectArtifact ||
			resultRun.SubjectID != artID ||
			resultRun.Executor != "ide_agent" ||
			resultRun.State != workboard.NextActionStateFail {
			t.Fatalf("folded gate result mismatch: %+v", resultRun)
		}
		pending, err := store.ListTasksForArtifact(ctx, artID)
		if err != nil {
			t.Fatalf("ListTasksForArtifact: %v", err)
		}
		if len(pending) != 0 {
			t.Fatalf("task with folded result must not be pending, got %d", len(pending))
		}

		// Idempotent: a second migration pass changes nothing.
		if err := migratePostgres(gdb); err != nil {
			t.Fatalf("migratePostgres (re-run): %v", err)
		}
		var again int64
		if err := gdb.Model(&workboard.GateRun{}).Count(&again).Error; err != nil {
			t.Fatalf("recount gate_runs: %v", err)
		}
		if again != total {
			t.Fatalf("gate_runs count changed on re-run: %d -> %d", total, again)
		}
	})
}
