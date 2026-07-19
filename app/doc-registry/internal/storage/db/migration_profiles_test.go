package db

import (
	"testing"

	"gorm.io/gorm"
)

// TestPolicySnapshotSchema verifies the fresh schema keeps only automatic-policy
// snapshot fields on artifacts and does not create a governance_profiles table.
func TestPolicySnapshotSchema(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		if cols := tableColumns(t, "governance_profiles", gdb, name); len(cols) != 0 {
			t.Fatalf("governance_profiles table exists in fresh schema: %v", cols)
		}

		acols := tableColumns(t, "artifacts", gdb, name)
		for _, want := range []string{
			"policy_version", "policy_digest", "policy_snapshot_json",
		} {
			if !acols[want] {
				t.Fatalf("artifacts missing policy snapshot column %q (have %v)", want, acols)
			}
		}
		for _, legacy := range []string{
			"gates_profile",
			"gates_profile_version",
			"gates_profile_digest",
			"gates_profile_snapshot_json",
		} {
			if acols[legacy] {
				t.Fatalf("artifacts should not expose legacy column %q (have %v)", legacy, acols)
			}
		}

		// Readiness runs live in the unified gate_runs table (subject_kind =
		// 'artifact'); its shape is asserted by TestGateRunsFreshSchema.
	})
}
