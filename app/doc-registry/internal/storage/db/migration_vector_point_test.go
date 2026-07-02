package db

import (
	"testing"

	"gorm.io/gorm"
)

func TestDocumentChunksDoesNotPersistVectorBackendMetadata(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		var columns []struct {
			ColumnName string `gorm:"column:column_name"`
		}
		if err := gdb.Raw(
			"SELECT column_name FROM information_schema.columns WHERE table_name = $1",
			"document_chunks",
		).Scan(&columns).Error; err != nil {
			t.Fatalf("list document_chunks columns: %v", err)
		}

		for _, column := range columns {
			switch column.ColumnName {
			case "qdrant_point_id", "vector_point_id":
				t.Fatalf("obsolete vector backend column %q still exists", column.ColumnName)
			}
		}
	})
}
