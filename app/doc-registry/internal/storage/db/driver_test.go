package db

import (
	"testing"

	"gorm.io/gorm"
)

func TestMigrate_Postgres(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		// Re-running Migrate must be a no-op (idempotency).
		if err := Migrate(gdb); err != nil {
			t.Fatalf("second Migrate failed on %s: %v", name, err)
		}
		// A canary table exists.
		var count int64
		if err := gdb.Raw("SELECT COUNT(*) FROM artifacts").Scan(&count).Error; err != nil {
			t.Fatalf("%s: count artifacts: %v", name, err)
		}
		// Delivery provenance keeps the submitted source head distinct from a
		// provider-created merge commit.
		var headSHA string
		if err := gdb.Raw("SELECT head_sha FROM integration_delivery_links LIMIT 0").Scan(&headSHA).Error; err != nil {
			t.Fatalf("%s: delivery head_sha column: %v", name, err)
		}
	})
}
