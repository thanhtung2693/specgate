package api

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	storagedb "github.com/specgate/doc-registry/internal/storage/db"
)

// sharedPostgres holds the single package-level testcontainer started lazily on
// first call to newTestGormDB.
var (
	sharedPostgresOnce    sync.Once
	sharedPostgresBaseURL string // e.g. "postgres://docreg:docreg@localhost:PORT/postgres?sslmode=disable"
	sharedPostgresErr     error
)

// startSharedPostgres starts the testcontainer exactly once per test binary run.
func startSharedPostgres() (string, error) {
	sharedPostgresOnce.Do(func() {
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
			sharedPostgresErr = err
			return
		}
		dsn, err := container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			sharedPostgresErr = err
			return
		}
		sharedPostgresBaseURL = dsn
	})
	return sharedPostgresBaseURL, sharedPostgresErr
}

var dbMu sync.Mutex
var dbCount int

// testDBSem bounds how many DB-backed tests provision + migrate a database at
// once. Without it, a high -parallel run migrates many fresh databases
// simultaneously (each migration plus its gorm pool holds real memory) and OOMs
// the testcontainer host (observed as exit 137 in CI). The cap keeps peak memory
// modest while still overlapping most of the suite.
var testDBSem = make(chan struct{}, 4)

// newTestGormDB opens a fresh migrated Postgres DB for the test. Each call
// creates a uniquely-named database on the shared container so tests are fully
// isolated. The DB is closed and dropped in t.Cleanup.
func newTestGormDB(t *testing.T) *gorm.DB {
	t.Helper()

	baseURL, err := startSharedPostgres()
	if err != nil {
		t.Skipf("skipping (no docker?): %v", err)
	}

	// Hold a slot for the lifetime of the test so concurrent DB-backed tests
	// (and their migrations) stay memory-bounded regardless of -parallel.
	testDBSem <- struct{}{}
	t.Cleanup(func() { <-testDBSem })

	// Generate a unique database name.
	dbMu.Lock()
	dbCount++
	n := dbCount
	dbMu.Unlock()
	dbName := fmt.Sprintf("testdb_%d", n)

	// Connect to the "postgres" maintenance DB to create the test DB.
	adminDB, err := gorm.Open(postgres.Open(baseURL), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("newTestGormDB: admin connect: %v", err)
	}
	if err := adminDB.Exec("CREATE DATABASE " + dbName).Error; err != nil {
		t.Fatalf("newTestGormDB: CREATE DATABASE %s: %v", dbName, err)
	}
	adminSQLDB, _ := adminDB.DB()
	_ = adminSQLDB.Close()

	// Build DSN pointing at the new database.
	// Replace "/postgres?" with "/<dbname>?" in the base URL.
	testDSN := replaceDBInDSN(baseURL, dbName)

	gdb, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("newTestGormDB: open %s: %v", dbName, err)
	}
	if err := storagedb.Migrate(gdb); err != nil {
		t.Fatalf("newTestGormDB: migrate %s: %v", dbName, err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gdb.DB()
		_ = sqlDB.Close()

		// Drop the database after the test.
		adminDB2, err := gorm.Open(postgres.Open(baseURL), &gorm.Config{Logger: logger.Discard})
		if err == nil {
			_ = adminDB2.Exec("DROP DATABASE IF EXISTS " + dbName + " WITH (FORCE)").Error
			adminSQLDB2, _ := adminDB2.DB()
			_ = adminSQLDB2.Close()
		}
	})
	return gdb
}

// replaceDBInDSN replaces the database component of a DSN string.
// Input DSN ends with "/<olddb>?..." — we replace <olddb> with newDB.
func replaceDBInDSN(dsn, newDB string) string {
	// DSN format from testcontainers: "postgres://user:pass@host:port/dbname?sslmode=disable"
	// Find last "/" before "?" and replace.
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
	// Find last "/" in base.
	slashIdx := -1
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == '/' {
			slashIdx = i
			break
		}
	}
	if slashIdx < 0 {
		return dsn // malformed, return as-is
	}
	return base[:slashIdx+1] + newDB + suffix
}
