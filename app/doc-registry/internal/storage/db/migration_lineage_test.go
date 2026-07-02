package db

import (
	"testing"

	"gorm.io/gorm"
)

// TestArtifactLineageColumns verifies that parent_artifact_id and lineage_root_id
// exist on the artifacts table. Runs on both drivers via forEachDriver.
// Per spec §Migration (SP3 artifact lineage).
func TestArtifactLineageColumns(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		cols := tableColumns(t, "artifacts", gdb, name)
		for _, want := range []string{"parent_artifact_id", "lineage_root_id"} {
			if !cols[want] {
				t.Fatalf("artifacts missing lineage column %q (have %v)", want, cols)
			}
		}
	})
}
