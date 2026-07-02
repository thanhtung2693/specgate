package seeding_test

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

	"github.com/specgate/doc-registry/internal/settings"
	"github.com/specgate/doc-registry/internal/skills"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
)

var (
	seedingPostgresOnce    sync.Once
	seedingPostgresBaseURL string
	seedingPostgresErr     error
)

var seedingDBMu sync.Mutex
var seedingDBCount int

func startSeedingPostgres() (string, error) {
	seedingPostgresOnce.Do(func() {
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
			seedingPostgresErr = err
			return
		}
		dsn, err := container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			seedingPostgresErr = err
			return
		}
		seedingPostgresBaseURL = dsn
	})
	return seedingPostgresBaseURL, seedingPostgresErr
}

// newSeedingTestGormDB creates a fresh migrated Postgres DB for the test.
func newSeedingTestGormDB(t *testing.T) *gorm.DB {
	t.Helper()

	baseURL, err := startSeedingPostgres()
	if err != nil {
		t.Skipf("skipping (no docker?): %v", err)
	}

	seedingDBMu.Lock()
	seedingDBCount++
	n := seedingDBCount
	seedingDBMu.Unlock()
	dbName := fmt.Sprintf("seeding_testdb_%d", n)

	adminDB, err := gorm.Open(postgres.Open(baseURL), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("newSeedingTestGormDB: admin connect: %v", err)
	}
	if err := adminDB.Exec("CREATE DATABASE " + dbName).Error; err != nil {
		t.Fatalf("newSeedingTestGormDB: CREATE DATABASE %s: %v", dbName, err)
	}
	adminSQLDB, _ := adminDB.DB()
	_ = adminSQLDB.Close()

	testDSN := replaceSeedingDBInDSN(baseURL, dbName)

	gdb, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("newSeedingTestGormDB: open %s: %v", dbName, err)
	}
	if err := storagedb.Migrate(gdb); err != nil {
		t.Fatalf("newSeedingTestGormDB: migrate %s: %v", dbName, err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gdb.DB()
		_ = sqlDB.Close()

		adminDB2, err := gorm.Open(postgres.Open(baseURL), &gorm.Config{Logger: logger.Discard})
		if err == nil {
			_ = adminDB2.Exec("DROP DATABASE IF EXISTS " + dbName + " WITH (FORCE)").Error
			adminSQLDB2, _ := adminDB2.DB()
			_ = adminSQLDB2.Close()
		}
	})
	return gdb
}

// newDemoTestGormDB is an alias used by demo_test.go.
func newDemoTestGormDB(t *testing.T) *gorm.DB {
	t.Helper()
	return newSeedingTestGormDB(t)
}

func replaceSeedingDBInDSN(dsn, newDB string) string {
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

// forEachDriver yields (name, skillsSvc, settingsSvc) for Postgres via testcontainers.
func forEachDriver(t *testing.T, fn func(t *testing.T, name string, skillsSvc *skills.Service, settingsSvc *settings.Service)) {
	t.Helper()

	t.Run("postgres", func(t *testing.T) {
		gdb := newSeedingTestGormDB(t)
		skillsSvc, settingsSvc := openMigratedDB(t, gdb)
		fn(t, "postgres", skillsSvc, settingsSvc)
	})
}

// openMigratedDB sets up skills and settings services on an already-open,
// already-migrated Postgres DB.
func openMigratedDB(t *testing.T, gdb *gorm.DB) (*skills.Service, *settings.Service) {
	t.Helper()

	skillsSvc := skills.NewService(storagedb.NewSkillRepository(gdb))

	// Use a deterministic 32-byte hex key for tests (not a real secret).
	const testKey = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	crypto, err := settings.NewCrypto(testKey)
	if err != nil {
		t.Fatalf("settings.NewCrypto: %v", err)
	}
	settingsSvc, err := settings.NewServiceWithTTL(
		storagedb.NewSettingsRepository(gdb),
		crypto,
		time.Hour, // large TTL — background refresh won't fire during test
	)
	if err != nil {
		// On a fresh DB, initial load may warn; that is acceptable.
		t.Logf("settings.NewService warning (ok on fresh DB): %v", err)
	}
	t.Cleanup(settingsSvc.Stop)

	return skillsSvc, settingsSvc
}

// mustCreateSkill inserts a skill directly through the service and fails the
// test if creation fails.
func mustCreateSkill(t *testing.T, svc *skills.Service, name string) *skills.Skill {
	t.Helper()
	sk, err := svc.Create(context.Background(), skills.CreateInput{
		Name:        name,
		Description: "test description",
		Prompt:      "test prompt text",
	})
	if err != nil {
		t.Fatalf("mustCreateSkill(%q): %v", name, err)
	}
	return sk
}
