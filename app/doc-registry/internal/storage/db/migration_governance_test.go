package db

import (
	"strings"
	"testing"

	"gorm.io/gorm"
)

// tableColumns returns a set of column names present in the given table.
func tableColumns(t *testing.T, name string, gdb *gorm.DB, driver string) map[string]bool {
	t.Helper()
	cols := map[string]bool{}
	var rows []struct {
		ColumnName string `gorm:"column:column_name"`
	}
	if err := gdb.Raw(
		"SELECT column_name FROM information_schema.columns WHERE table_name = $1",
		name,
	).Scan(&rows).Error; err != nil {
		t.Fatalf("information_schema.columns(%s): %v", name, err)
	}
	for _, r := range rows {
		cols[strings.ToLower(r.ColumnName)] = true
	}
	return cols
}

// TestGovernanceEnvelopeSchema verifies the open (artifact_id, path)
// artifact_files shape and the governance envelope columns on artifacts.
// Runs on both drivers via forEachDriver.
// Per spec §Migration: artifact_files keyed by (artifact_id, path) with a role
// column; artifacts carries the envelope columns.
func TestGovernanceEnvelopeSchema(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		// artifact_files columns
		cols := tableColumns(t, "artifact_files", gdb, name)
		for _, want := range []string{"artifact_id", "path", "role", "s3_path", "size_bytes"} {
			if !cols[want] {
				t.Fatalf("artifact_files missing column %q (have %v)", want, cols)
			}
		}
		// artifacts envelope columns
		acols := tableColumns(t, "artifacts", gdb, name)
		for _, want := range []string{"source_kind", "source_id", "source_revision", "authority", "gates_profile"} {
			if !acols[want] {
				t.Fatalf("artifacts missing envelope column %q (have %v)", want, acols)
			}
		}
	})
}

func TestPostgresFreshSchemaIncludesGovernanceTables(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		for _, table := range []string{
			"gate_tasks",
			"gate_runs",
			"users",
			"workspaces",
			"workspace_members",
		} {
			if cols := tableColumns(t, table, gdb, name); len(cols) == 0 {
				t.Fatalf("%s table missing in fresh schema on %s", table, name)
			}
		}

		cols := tableColumns(t, "change_requests", gdb, name)
		for _, want := range []string{"governance_thread_id", "workspace_id"} {
			if !cols[want] {
				t.Fatalf("change_requests missing column %q in fresh schema on %s", want, name)
			}
		}
	})
}
