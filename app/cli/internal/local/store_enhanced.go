package local

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"modernc.org/sqlite"
)

var (
	enhancedSQLiteDriverOnce sync.Once
	enhancedSQLiteDriverErr  error
)

func registerEnhancedSQLiteDriver() error {
	enhancedSQLiteDriverOnce.Do(func() {
		driverInstance := &sqlite.Driver{}
		enhancedSQLiteDriverErr = driverInstance.RegisterDeterministicScalarFunction(
			"specgate_enhanced_writer",
			0,
			func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) {
				return int64(1), nil
			},
		)
		if enhancedSQLiteDriverErr != nil {
			return
		}
		sql.Register(enhancedSQLiteDriverName, driverInstance)
	})
	return enhancedSQLiteDriverErr
}

// WithEnhancedWrite executes an enhanced-record write behind the store-wide
// compatibility barrier. The first use creates one mode-0600 snapshot beside
// the database, then atomically installs the marker and SQLite mutation guards
// with the caller's write. A failed first write rolls back database changes and
// removes only the newly-created owned snapshot.
func (s *Store) WithEnhancedWrite(ctx context.Context, write func(*sql.Tx) error) error {
	if write == nil {
		return fmt.Errorf("enhanced store write is required")
	}
	enabled, err := s.enhancedStoreEnabled(ctx)
	if err != nil {
		return err
	}
	createdBackup := false
	if !enabled {
		if err := s.createEnhancedStoreBackup(ctx); err != nil {
			return err
		}
		createdBackup = true
	}
	committed := false
	defer func() {
		if createdBackup && !committed {
			_ = os.Remove(s.enhancedStoreBackupPath())
		}
	}()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if !enabled {
		if err := installEnhancedStoreBarrier(ctx, tx); err != nil {
			return err
		}
	}
	if err := write(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *Store) enhancedStoreEnabled(ctx context.Context) (bool, error) {
	var version string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM metadata WHERE key = ?`, enhancedStoreMetadataKey).Scan(&version)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if version != enhancedStoreVersion {
		return false, fmt.Errorf("%w: enhanced Local store version %q", ErrStoreIncompatible, version)
	}
	return true, nil
}

func (s *Store) enhancedStoreBackupPath() string { return s.path + ".pre-enhanced.bak" }

func (s *Store) createEnhancedStoreBackup(ctx context.Context) error {
	backup := s.enhancedStoreBackupPath()
	if err := ensureRealDirectory(filepath.Dir(backup)); err != nil {
		return err
	}
	if _, err := os.Lstat(backup); err == nil {
		return fmt.Errorf("enhanced Local store backup already exists: %s", backup)
	} else if !os.IsNotExist(err) {
		return err
	}
	privateDir, err := os.MkdirTemp(filepath.Dir(backup), ".specgate-backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(privateDir)
	privateBackup := filepath.Join(privateDir, "state.db")
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, privateBackup); err != nil {
		return fmt.Errorf("create enhanced Local store backup: %w", err)
	}
	if err := os.Chmod(privateBackup, 0o600); err != nil {
		return err
	}
	if err := os.Link(privateBackup, backup); err != nil {
		return fmt.Errorf("publish enhanced Local store backup: %w", err)
	}
	return nil
}

func installEnhancedStoreBarrier(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO metadata(key, value) VALUES (?, ?)`, enhancedStoreMetadataKey, enhancedStoreVersion); err != nil {
		return err
	}
	for _, table := range enhancedStoreGuardedTables {
		for _, operation := range []string{"INSERT", "UPDATE", "DELETE"} {
			name := "specgate_enhanced_" + table + "_" + strings.ToLower(operation)
			statement := fmt.Sprintf(`CREATE TRIGGER %s BEFORE %s ON %s BEGIN
				SELECT CASE WHEN specgate_enhanced_writer() <> 1 THEN RAISE(ABORT, 'enhanced Local store requires a supporting SpecGate CLI') END;
			END`, name, operation, table)
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
	}
	return nil
}

var enhancedStoreGuardedTables = []string{
	"metadata",
	"users",
	"workspaces",
	"selection",
	"artifacts",
	"artifact_documents",
	"artifact_readiness_runs",
	"local_gate_tasks",
	"artifact_approvals",
	"features",
	"work_items",
	"delivery_reports",
	"verification_contracts",
	"work_checkpoints",
	"acceptance_bases",
	"delivery_reviews",
	"delivery_peer_reviews",
	"audit_events",
}
