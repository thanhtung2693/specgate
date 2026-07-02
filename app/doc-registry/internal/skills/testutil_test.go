package skills

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

var (
	skillsPostgresOnce    sync.Once
	skillsPostgresBaseURL string
	skillsPostgresErr     error
)

var skillsDBMu sync.Mutex
var skillsDBCount int

func startSkillsPostgres() (string, error) {
	skillsPostgresOnce.Do(func() {
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
			skillsPostgresErr = err
			return
		}
		dsn, err := container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			skillsPostgresErr = err
			return
		}
		skillsPostgresBaseURL = dsn
	})
	return skillsPostgresBaseURL, skillsPostgresErr
}

// newSkillsTestGormDB opens a fresh migrated Postgres DB for skills tests.
func newSkillsTestGormDB(t *testing.T) *gorm.DB {
	t.Helper()

	baseURL, err := startSkillsPostgres()
	if err != nil {
		t.Skipf("skipping (no docker?): %v", err)
	}

	skillsDBMu.Lock()
	skillsDBCount++
	n := skillsDBCount
	skillsDBMu.Unlock()
	dbName := fmt.Sprintf("skills_testdb_%d", n)

	adminDB, err := gorm.Open(postgres.Open(baseURL), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("newSkillsTestGormDB: admin connect: %v", err)
	}
	if err := adminDB.Exec("CREATE DATABASE " + dbName).Error; err != nil {
		t.Fatalf("newSkillsTestGormDB: CREATE DATABASE %s: %v", dbName, err)
	}
	adminSQLDB, _ := adminDB.DB()
	_ = adminSQLDB.Close()

	testDSN := replaceSkillsDBInDSN(baseURL, dbName)

	gdb, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("newSkillsTestGormDB: open %s: %v", dbName, err)
	}
	if err := storagedb.Migrate(gdb); err != nil {
		t.Fatalf("newSkillsTestGormDB: migrate %s: %v", dbName, err)
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

func replaceSkillsDBInDSN(dsn, newDB string) string {
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
