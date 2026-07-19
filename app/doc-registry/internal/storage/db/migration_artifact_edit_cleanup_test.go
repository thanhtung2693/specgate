package db

import (
	"testing"

	"gorm.io/gorm"
)

func TestArtifactEditMutationSchemaRemoved(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		for _, table := range []string{
			"artifact_edit_hunk_decisions",
			"artifact_edit_sessions",
			"artifact_edit_session_files",
			"artifact_edit_revisions",
		} {
			if gdb.Migrator().HasTable(table) {
				t.Fatalf("%s must not exist", table)
			}
		}
	})
}
