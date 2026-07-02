package db

import (
	"testing"

	"github.com/specgate/doc-registry/internal/config"
	"gorm.io/gorm"
)

func TestOpenUnknownDriver(t *testing.T) {
	t.Parallel()
	_, err := Open(config.DatabaseConfig{Driver: "mystery"})
	if err == nil {
		t.Fatal("expected error for unknown driver")
	}
}

func TestMigrate_AllDrivers(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		// Re-running Migrate must be a no-op on both drivers (idempotency).
		if err := Migrate(gdb); err != nil {
			t.Fatalf("second Migrate failed on %s: %v", name, err)
		}
		// A canary table exists.
		var count int64
		if err := gdb.Raw("SELECT COUNT(*) FROM artifacts").Scan(&count).Error; err != nil {
			t.Fatalf("%s: count artifacts: %v", name, err)
		}
	})
}

func TestMigrate_AddsAttachmentGovernanceFileIDToExistingTable(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		if err := gdb.Exec("ALTER TABLE artifact_attachments DROP COLUMN IF EXISTS governance_file_id").Error; err != nil {
			t.Fatalf("%s: drop governance_file_id: %v", name, err)
		}
		if err := Migrate(gdb); err != nil {
			t.Fatalf("%s: migrate after old attachment schema: %v", name, err)
		}

		var count int64
		err := gdb.Raw(`
			SELECT COUNT(*)
			FROM information_schema.columns
			WHERE table_name = 'artifact_attachments'
			  AND column_name = 'governance_file_id'
		`).Scan(&count).Error
		if err != nil {
			t.Fatalf("%s: inspect artifact_attachments columns: %v", name, err)
		}
		if count != 1 {
			t.Fatalf("%s: governance_file_id column count = %d, want 1", name, count)
		}
	})
}
