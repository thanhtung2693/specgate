package db

import (
	"gorm.io/gorm"
)

// Open returns the Postgres-backed GORM database.
func Open(dsn string) (*gorm.DB, error) { return openPostgres(dsn) }

// Migrate applies the Postgres migration set.
func Migrate(gdb *gorm.DB) error {
	return migratePostgres(gdb)
}
