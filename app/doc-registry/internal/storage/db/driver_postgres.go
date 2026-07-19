package db

import (
	"fmt"
	"io"
	"log"
	"os"

	migrationpostgres "github.com/specgate/doc-registry/migrations/postgres"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// openPostgres opens a GORM DB against Postgres using the libpq-style DSN.
// Foreign keys are enforced by the database engine by default.
func openPostgres(dsn string) (*gorm.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("postgres: empty DSN")
	}
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: postgresLogger(os.Stderr)})
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("underlying db: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return gdb, nil
}

func postgresLogger(out io.Writer) logger.Interface {
	return logger.New(log.New(out, "", log.LstdFlags), logger.Config{
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: true,
	})
}

// migratePostgres applies the embedded Postgres schema. Every file is
// idempotent (CREATE ... IF NOT EXISTS) and applied in full on every start;
// there is no migration-tracking table.
func migratePostgres(gdb *gorm.DB) error {
	entries, err := migrationpostgres.FS.ReadDir(".")
	if err != nil {
		return fmt.Errorf("read pg migrations: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := migrationpostgres.FS.ReadFile(e.Name())
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		if err := gdb.Exec(string(b)).Error; err != nil {
			return fmt.Errorf("apply %s: %w", e.Name(), err)
		}
	}
	return nil
}
