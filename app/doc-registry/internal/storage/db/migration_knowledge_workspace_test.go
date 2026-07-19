package db

import (
	"testing"

	"gorm.io/gorm"
)

// TestMigrationKnowledgeWorkspaceScope proves the fresh schema includes
// documents.workspace_id.
func TestMigrationKnowledgeWorkspaceScope(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		cols := tableColumns(t, "documents", gdb, name)
		if !cols["workspace_id"] {
			t.Fatalf("documents.workspace_id missing; columns=%v", cols)
		}
	})
}
