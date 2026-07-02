package db

import (
	"fmt"

	"github.com/specgate/doc-registry/internal/config"
	"gorm.io/gorm"
)

// Open returns a GORM DB for the configured Postgres driver.
func Open(cfg config.DatabaseConfig) (*gorm.DB, error) {
	switch cfg.Driver {
	case "postgres":
		return openPostgres(cfg.PostgresDSN)
	default:
		return nil, fmt.Errorf("unknown database driver %q", cfg.Driver)
	}
}

// Migrate applies the Postgres migration set.
func Migrate(gdb *gorm.DB) error {
	return migratePostgres(gdb)
}
