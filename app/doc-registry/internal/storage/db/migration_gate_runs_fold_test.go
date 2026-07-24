package db

import (
	"reflect"
	"testing"

	migrationpostgres "github.com/specgate/doc-registry/migrations/postgres"
	"gorm.io/gorm"
)

func TestDevelopmentSchemaIsCollapsed(t *testing.T) {
	entries, err := migrationpostgres.FS.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var migrations []string
	for _, entry := range entries {
		if !entry.IsDir() {
			migrations = append(migrations, entry.Name())
		}
	}
	want := []string{"0001_init.migration"}
	if !reflect.DeepEqual(migrations, want) {
		t.Fatalf("migrations = %v, want collapsed schema %v", migrations, want)
	}
}

func TestGateRunsFreshSchema(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		cols := tableColumns(t, "gate_runs", gdb, name)
		for _, want := range []string{
			"id", "workspace_id", "subject_kind", "subject_id", "gate", "state", "hint",
			"executor", "evidence_json", "completion_feedback_event_id", "created_at",
		} {
			if !cols[want] {
				t.Fatalf("gate_runs missing column %q (have %v)", want, cols)
			}
		}
		for _, absent := range []string{"workboard_gate_runs", "artifact_readiness_runs", "gate_results"} {
			if cols := tableColumns(t, absent, gdb, name); len(cols) != 0 {
				t.Fatalf("obsolete table %q exists in fresh schema", absent)
			}
		}
		if !gdb.Migrator().HasIndex("gate_runs", "idx_gate_runs_delivery_cycle") {
			t.Fatalf("%s gate_runs missing delivery-cycle index", name)
		}
	})
}

func TestGateRunsMigrationRepairsMissingDeliveryCycleColumn(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		if err := gdb.Exec("DROP INDEX IF EXISTS idx_gate_runs_delivery_cycle").Error; err != nil {
			t.Fatalf("%s drop delivery-cycle index: %v", name, err)
		}
		if err := gdb.Exec("ALTER TABLE gate_runs DROP COLUMN completion_feedback_event_id").Error; err != nil {
			t.Fatalf("%s drop delivery-cycle column: %v", name, err)
		}

		if err := Migrate(gdb); err != nil {
			t.Fatalf("%s migrate schema without delivery-cycle column: %v", name, err)
		}
		if !tableColumns(t, "gate_runs", gdb, name)["completion_feedback_event_id"] {
			t.Fatalf("%s migration did not restore gate_runs.completion_feedback_event_id", name)
		}
		if !gdb.Migrator().HasIndex("gate_runs", "idx_gate_runs_delivery_cycle") {
			t.Fatalf("%s migration did not restore delivery-cycle index", name)
		}
	})
}
