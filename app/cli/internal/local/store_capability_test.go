package local

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// EnableEnhancedStore is a test fixture for a barrier-only store. Production
// writes install the barrier and their first enhanced record in one transaction.
func (s *Store) EnableEnhancedStore(ctx context.Context) error {
	return s.WithEnhancedWrite(ctx, func(*sql.Tx) error { return nil })
}

func TestLineageFirstWriteInstallsCompatibilityBarrier(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sel, err := s.Initialize(t.Context(), InitInput{WorkspaceName: "Lineage", Username: "human", DisplayName: "Human"})
	if err != nil {
		t.Fatal(err)
	}
	in := ArtifactInput{FeatureKey: "F", RequestType: "new_feature", Documents: []ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("base")}}, SourceCriteria: []SourceCriterion{{ID: "r", Text: "Requirement", SourcePath: "spec.md"}}}
	base, err := s.PublishArtifact(t.Context(), sel.Workspace.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	in.BaseVersion = "v1"
	in.SourceLineage = &SourceLineage{Version: 1, BaseArtifactID: base.ID, BaseDigest: base.SnapshotDigest, Rows: []LineageRow{{BaseID: "r", TargetIDs: []string{"r"}}}}
	if _, err := s.PublishArtifact(t.Context(), sel.Workspace.ID, in); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".pre-enhanced.bak"); err != nil {
		t.Fatal("missing pre-enhanced backup:", err)
	}
	legacy, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	if _, err := legacy.Exec(`UPDATE workspaces SET name='old writer'`); err == nil {
		t.Fatal("legacy writer changed lineage store")
	}
}

func TestOpenRejectsUnknownEnhancedStoreVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO metadata(key,value) VALUES('enhanced_store_version','999')`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Open(path)
	if err == nil {
		got.Close()
		t.Fatal("opened unsupported store version")
	}
	info, statErr := os.Stat(path)
	if statErr != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("unsupported store was modified: mode=%v err=%v", info.Mode(), statErr)
	}
}

func TestEnhancedStoreRejectsConnectionWithoutCapability(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	selection, err := store.Initialize(ctx, InitInput{WorkspaceName: "Guard", Username: "human", DisplayName: "Human"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnableEnhancedStore(ctx); err != nil {
		t.Fatal(err)
	}

	legacy, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	if _, err := legacy.ExecContext(ctx, `INSERT INTO workspaces(id, slug, name) VALUES ('legacy-workspace', 'legacy', 'Legacy')`); err == nil {
		t.Fatal("legacy connection inserted a workspace into an enhanced store")
	}
	if _, err := legacy.ExecContext(ctx, `UPDATE workspaces SET name = 'Mutated' WHERE id = ?`, selection.Workspace.ID); err == nil {
		t.Fatal("legacy connection updated an enhanced store")
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspaces WHERE id = 'legacy-workspace'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("legacy workspace count = %d, want 0", count)
	}
	var name string
	if err := store.db.QueryRowContext(ctx, `SELECT name FROM workspaces WHERE id = ?`, selection.Workspace.ID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Guard" {
		t.Fatalf("workspace name = %q, want Guard", name)
	}
	if _, err := store.CreateWorkspace(ctx, "Supporting client"); err != nil {
		t.Fatalf("supporting client write = %v", err)
	}
}

func TestEnhancedStoreUpgradeRollsBackWhenFirstWriteFails(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Initialize(ctx, InitInput{WorkspaceName: "Legacy", Username: "human", DisplayName: "Human"}); err != nil {
		t.Fatal(err)
	}

	if err := store.WithEnhancedWrite(ctx, func(tx *sql.Tx) error {
		return errors.New("first enhanced write failed")
	}); err == nil {
		t.Fatal("WithEnhancedWrite succeeded after callback failure")
	}
	var markerCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM metadata WHERE key = 'enhanced_store_version'`).Scan(&markerCount); err != nil {
		t.Fatal(err)
	}
	if markerCount != 0 {
		t.Fatalf("enhanced marker count = %d, want 0 after rollback", markerCount)
	}
	var triggerCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name LIKE 'specgate_enhanced_%'`).Scan(&triggerCount); err != nil {
		t.Fatal(err)
	}
	if triggerCount != 0 {
		t.Fatalf("enhanced trigger count = %d, want 0 after rollback", triggerCount)
	}
	if _, err := store.CreateWorkspace(ctx, "Still legacy"); err != nil {
		t.Fatalf("legacy store write after failed upgrade = %v", err)
	}
	if _, err := os.Stat(store.enhancedStoreBackupPath()); !os.IsNotExist(err) {
		t.Fatalf("failed first write retained its backup: %v", err)
	}
	if err := store.EnableEnhancedStore(ctx); err != nil {
		t.Fatalf("retry after failed first write: %v", err)
	}
	if _, err := os.Stat(store.enhancedStoreBackupPath()); err != nil {
		t.Fatalf("successful retry did not retain backup: %v", err)
	}
}

func TestEnhancedWritePreservesPreexistingBackup(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backup := store.enhancedStoreBackupPath()
	want := []byte("user-owned prior backup")
	if err := os.WriteFile(backup, want, 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	err = store.WithEnhancedWrite(t.Context(), func(*sql.Tx) error {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatalf("existing backup did not block upgrade: called=%v err=%v", called, err)
	}
	got, err := os.ReadFile(backup)
	if err != nil || string(got) != string(want) {
		t.Fatalf("prior backup changed: bytes=%q err=%v", got, err)
	}
}

func TestEnhancedBackupParentIsPrivateBeforeSnapshot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "specgate")
	s, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.EnableEnhancedStore(t.Context()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("created state parent mode = %o, want 0700", info.Mode().Perm())
	}
}

func TestEnhancedBackupNeverAppearsPublicDuringSnapshot(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec(`CREATE TABLE backup_fixture(data BLOB)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO backup_fixture(data) VALUES (zeroblob(67108864))`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.createEnhancedStoreBackup(t.Context()) }()
	tick := time.NewTicker(100 * time.Microsecond)
	defer tick.Stop()
	backup := s.enhancedStoreBackupPath()
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(backup)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("final backup mode: %v %v", info, err)
			}
			return
		case <-tick.C:
			info, err := os.Lstat(backup)
			if err == nil && info.Mode().Perm()&0o077 != 0 {
				t.Fatalf("backup exposed during VACUUM: mode %o", info.Mode().Perm())
			}
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
	}
}

func TestOpenPreservesExistingStateDirectoryPermissions(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("existing directory mode changed to %o", info.Mode().Perm())
	}
}
