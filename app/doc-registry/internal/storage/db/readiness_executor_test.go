package db

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/workboard"
)

// TestListReadinessRunsIncludesAgentAttestedRuns verifies the readiness
// history shows every executor with its origin.
func TestListReadinessRunsIncludesAgentAttestedRuns(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		runs := []workboard.GateRun{
			{ID: "run-platform", WorkspaceID: "ws-test", SubjectKind: workboard.GateRunSubjectArtifact, SubjectID: "art-x", Gate: "scope_clear", State: workboard.NextActionStatePass, Hint: "bounded", Executor: workboard.GateRunExecutorPlatform, CreatedAt: now.Add(-2 * time.Hour)},
			{ID: "run-agent", WorkspaceID: "ws-test", SubjectKind: workboard.GateRunSubjectArtifact, SubjectID: "art-x", Gate: "spec_completeness", State: workboard.NextActionStateWarn, Hint: "thin risks", Executor: workboard.GateRunExecutorIDEAgent, CreatedAt: now.Add(-1 * time.Hour)},
		}
		for i := range runs {
			if err := gdb.Create(&runs[i]).Error; err != nil {
				t.Fatal(err)
			}
		}

		rows, err := repo.ListReadinessRuns(ctx, "art-x", 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Fatalf("rows = %d, want 2 (agent-attested run missing from history) — %#v", len(rows), rows)
		}
		byID := map[string]string{}
		for _, row := range rows {
			byID[row.ID] = row.Executor
		}
		if byID["run-agent"] != "ide_agent" {
			t.Errorf("run-agent executor = %q, want ide_agent", byID["run-agent"])
		}
		if byID["run-platform"] != "platform" {
			t.Errorf("run-platform executor = %q, want platform", byID["run-platform"])
		}
	})
}
