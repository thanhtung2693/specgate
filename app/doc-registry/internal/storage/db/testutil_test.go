package db

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	gpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	dbOnce    sync.Once
	dbBaseURL string
	dbErr     error
)

func startSharedPostgres() (string, error) {
	dbOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		container, err := tcpostgres.Run(
			ctx,
			"postgres:18-alpine",
			tcpostgres.WithDatabase("postgres"),
			tcpostgres.WithUsername("docreg"),
			tcpostgres.WithPassword("docreg"),
			tcpostgres.BasicWaitStrategies(),
		)
		if err != nil {
			dbErr = err
			return
		}
		dsn, err := container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			dbErr = err
			return
		}
		dbBaseURL = dsn
	})
	return dbBaseURL, dbErr
}

var (
	dbMu    sync.Mutex
	dbCount int
)

// forEachDriver yields (name, *gorm.DB) for Postgres via a shared testcontainer.
// Each call creates a uniquely-named database on the shared container for isolation.
// The subtest is skipped on hosts where testcontainers can't reach a Docker daemon.
func forEachDriver(t *testing.T, fn func(t *testing.T, name string, gdb *gorm.DB)) {
	t.Helper()

	t.Run("postgres", func(t *testing.T) {
		baseURL, err := startSharedPostgres()
		if err != nil {
			t.Skipf("skipping postgres subtest (no docker?): %v", err)
		}

		dbMu.Lock()
		dbCount++
		n := dbCount
		dbMu.Unlock()
		dbName := fmt.Sprintf("testdb_%d", n)

		adminDB, err := gorm.Open(gpostgres.Open(baseURL), &gorm.Config{Logger: logger.Discard})
		if err != nil {
			t.Fatalf("admin connect: %v", err)
		}
		if err := adminDB.Exec("CREATE DATABASE " + dbName).Error; err != nil {
			t.Fatalf("CREATE DATABASE %s: %v", dbName, err)
		}
		adminSQLDB, _ := adminDB.DB()
		_ = adminSQLDB.Close()

		testDSN := replaceDBInDSN(baseURL, dbName)
		gdb, err := gorm.Open(gpostgres.Open(testDSN), &gorm.Config{Logger: logger.Discard})
		if err != nil {
			t.Fatalf("open test postgres %s: %v", dbName, err)
		}
		sqlDB, err := gdb.DB()
		if err != nil {
			t.Fatalf("test postgres underlying db %s: %v", dbName, err)
		}
		if err := sqlDB.Ping(); err != nil {
			t.Fatalf("ping test postgres %s: %v", dbName, err)
		}
		if err := migratePostgres(gdb); err != nil {
			t.Fatalf("migratePostgres %s: %v", dbName, err)
		}
		t.Cleanup(func() {
			_ = sqlDB.Close()

			adminDB2, err := gorm.Open(gpostgres.Open(baseURL), &gorm.Config{Logger: logger.Discard})
			if err == nil {
				_ = adminDB2.Exec("DROP DATABASE IF EXISTS " + dbName + " WITH (FORCE)").Error
				adminSQLDB2, _ := adminDB2.DB()
				_ = adminSQLDB2.Close()
			}
		})
		fn(t, "postgres", gdb)
	})
}

// replaceDBInDSN replaces the database component of a DSN string.
func replaceDBInDSN(dsn, newDB string) string {
	qIdx := -1
	for i, c := range dsn {
		if c == '?' {
			qIdx = i
			break
		}
	}
	var suffix string
	base := dsn
	if qIdx >= 0 {
		suffix = dsn[qIdx:]
		base = dsn[:qIdx]
	}
	slashIdx := -1
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == '/' {
			slashIdx = i
			break
		}
	}
	if slashIdx < 0 {
		return dsn
	}
	return base[:slashIdx+1] + newDB + suffix
}
