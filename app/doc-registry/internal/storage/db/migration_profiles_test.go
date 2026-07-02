package db

import (
	"testing"

	"gorm.io/gorm"
)

// TestProfilesSchema verifies the SP1 profile registry table plus the artifact
// profile snapshot columns. Runs on both drivers via forEachDriver.
func TestProfilesSchema(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		pcols := tableColumns(t, "governance_profiles", gdb, name)
		for _, want := range []string{
			"id", "namespace", "key", "version", "display_name", "change_type",
			"definition_json", "digest", "source", "source_repo", "source_path",
			"status", "imported_at", "created_at", "updated_at",
		} {
			if !pcols[want] {
				t.Fatalf("governance_profiles missing column %q (have %v)", want, pcols)
			}
		}

		acols := tableColumns(t, "artifacts", gdb, name)
		for _, want := range []string{
			"gates_profile_version", "gates_profile_digest", "gates_profile_snapshot_json",
		} {
			if !acols[want] {
				t.Fatalf("artifacts missing profile snapshot column %q (have %v)", want, acols)
			}
		}

		// Readiness runs live in the unified gate_runs table (subject_kind =
		// 'artifact'); its shape is asserted by TestGateRunsFreshSchema.
	})
}
