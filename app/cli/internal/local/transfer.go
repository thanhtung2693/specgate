package local

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// TransferUser is the identity needed to describe the scope of a private
// archive. Email and other user content stay in the database payload.
type TransferUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type TransferBookmark struct {
	Workspace     Workspace `json:"workspace"`
	WorkID        string    `json:"work_id"`
	WorkKey       string    `json:"work_key"`
	ContextDigest string    `json:"context_digest"`
	CheckpointID  string    `json:"checkpoint_id,omitempty"`
}

type TransferInventory struct {
	StoreID        string           `json:"store_id"`
	StoreVersion   string           `json:"store_version"`
	Generation     string           `json:"checkout_generation"`
	WorkspaceCount int              `json:"workspace_count"`
	UserCount      int              `json:"user_count"`
	WorkCount      int              `json:"work_count"`
	Workspaces     []Workspace      `json:"workspaces"`
	Users          []TransferUser   `json:"users"`
	Bookmark       TransferBookmark `json:"bookmark"`
}

func (s *Store) vacuumSnapshot(ctx context.Context, dest string) error {
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, dest); err != nil {
		return err
	}
	return os.Chmod(dest, 0o600)
}

// CreateTransferSnapshot copies committed SQLite state, including WAL data,
// without opening the source for migration. The caller owns the private stage.
func CreateTransferSnapshot(ctx context.Context, source, dest string) error {
	store, err := openTransferReadOnly(source, false)
	if err != nil {
		return err
	}
	defer store.Close()
	if _, _, err := enhancedStoreFormat(ctx, store.db); err != nil {
		return err
	}
	return store.vacuumSnapshot(ctx, dest)
}

func openTransferReadOnly(path string, immutable bool) (*Store, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	path = canonicalizeDarwinRootAlias(absolute)
	if err := rejectSymlinkedPathComponents(path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("the Local database must be a regular file")
	}
	if !immutable {
		for _, suffix := range []string{"-wal", "-shm", "-journal"} {
			if sidecar, err := os.Lstat(path + suffix); err == nil && !sidecar.Mode().IsRegular() {
				return nil, fmt.Errorf("the Local database sidecars must be regular files")
			} else if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
		}
	}
	uri, err := url.Parse(sqliteDSN(path))
	if err != nil {
		return nil, err
	}
	query := uri.Query()
	query.Del("_txlock")
	query.Set("mode", "ro")
	query.Add("_pragma", "trusted_schema(OFF)")
	if immutable {
		query.Set("immutable", "1")
	}
	uri.RawQuery = query.Encode()
	if err := registerEnhancedSQLiteDriver(); err != nil {
		return nil, err
	}
	db, err := sql.Open(enhancedSQLiteDriverName, uri.String())
	if err != nil {
		return nil, err
	}
	return &Store{db: db, path: path}, nil
}

// ReadTransferBinding reads an existing store without migrations. A blank
// workspace ref resolves to that database's current workspace selection.
func ReadTransferBinding(ctx context.Context, path, workspaceRef string) (TransferInventory, error) {
	s, err := openTransferReadOnly(path, false)
	if err != nil {
		return TransferInventory{}, err
	}
	defer s.Close()
	return s.transferInventory(ctx, workspaceRef, "", "", false, false)
}

// ValidateTransferSnapshot is used before writable SQLite opens a private
// archive payload. Immutable mode verifies the database file on its own.
func ValidateTransferSnapshot(ctx context.Context, path, workspaceRef, workRef, checkpointID string) (TransferInventory, error) {
	s, err := openTransferReadOnly(path, true)
	if err != nil {
		return TransferInventory{}, err
	}
	defer s.Close()
	legacyArtifact, err := s.validateTransferSchema(ctx)
	if err != nil {
		return TransferInventory{}, err
	}
	var status string
	if err := s.db.QueryRowContext(ctx, `PRAGMA integrity_check(1)`).Scan(&status); err != nil {
		return TransferInventory{}, err
	}
	if status != "ok" {
		return TransferInventory{}, fmt.Errorf("SQLite integrity check failed")
	}
	var violatedTable string
	err = s.db.QueryRowContext(ctx, `SELECT "table" FROM pragma_foreign_key_check LIMIT 1`).Scan(&violatedTable)
	if !errors.Is(err, sql.ErrNoRows) {
		if err == nil {
			err = fmt.Errorf("SQLite foreign-key check failed")
		}
		return TransferInventory{}, err
	}
	return s.transferInventory(ctx, workspaceRef, workRef, checkpointID, true, legacyArtifact)
}

// ReadReceivedTransferBookmark checks the selected relations without requiring
// the mutable inventory to stay frozen after the store has been received.
func ReadReceivedTransferBookmark(ctx context.Context, path string, bookmark TransferBookmark) (TransferInventory, error) {
	s, err := openTransferReadOnly(path, false)
	if err != nil {
		return TransferInventory{}, err
	}
	defer s.Close()
	return s.transferInventory(ctx, bookmark.Workspace.ID, bookmark.WorkID, bookmark.CheckpointID, false, false)
}

func (s *Store) validateTransferSchema(ctx context.Context) (legacyArtifact bool, err error) {
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return false, err
	}
	if version != 0 {
		return false, fmt.Errorf("%w: SQLite schema version %d", ErrStoreIncompatible, version)
	}
	versionMarker, _, err := enhancedStoreFormat(ctx, s.db)
	if err != nil {
		return false, err
	}

	expected, err := transferCurrentSchema()
	if err != nil {
		return false, err
	}
	actual, err := readTransferColumns(ctx, s.db)
	if err != nil {
		return false, err
	}
	for table, columns := range actual {
		if expected[table] == nil {
			return false, fmt.Errorf("%w: unsupported SQLite schema object", ErrStoreIncompatible)
		}
		for name, column := range columns {
			want, exists := expected[table][name]
			if versionMarker == "" {
				// These two historical schema changes are handled by Open in scratch.
				if table == "work_items" && (name == "feature_id" || name == "artifact_id") {
					column.NotNull = want.NotNull
				}
				if table == "artifact_documents" && name == "role" && column.PrimaryKey == 0 {
					column.PrimaryKey = want.PrimaryKey
				}
			}
			if !exists || column != want {
				return false, fmt.Errorf("%w: unsupported SQLite column shape for %s.%s", ErrStoreIncompatible, table, name)
			}
		}
	}
	for table, columns := range expected {
		if actual[table] == nil && versionMarker == "" {
			switch table {
			case "local_gate_tasks", "verification_contracts", "work_checkpoints", "acceptance_bases", "delivery_peer_reviews":
				continue // Open creates optional pre-enhancement tables in scratch.
			}
		}
		for name := range columns {
			if _, ok := actual[table][name]; ok {
				continue
			}
			if versionMarker == "" && table == "artifacts" && (name == "source_criteria_json" || name == "source_lineage_json") {
				legacyArtifact = true // Existing Open migrations add these fields.
				continue
			}
			if versionMarker == "" && table == "delivery_reviews" && name == "report_id" {
				continue // Existing Open migration adds this field.
			}
			return false, fmt.Errorf("%w: missing Local column %s.%s", ErrStoreIncompatible, table, name)
		}
	}
	return legacyArtifact, s.validateTransferGuards(ctx, versionMarker)
}

type transferColumn struct {
	Type       string
	NotNull    bool
	PrimaryKey int
	Hidden     int
}

var transferCurrentSchema = sync.OnceValues(func() (map[string]map[string]transferColumn, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	for _, statement := range localSchemaStatements {
		if _, err := db.Exec(statement); err != nil {
			return nil, err
		}
	}
	return readTransferColumns(context.Background(), db)
})

func readTransferColumns(ctx context.Context, db *sql.DB) (map[string]map[string]transferColumn, error) {
	rows, err := db.QueryContext(ctx, `SELECT tbl.name, tbl.type, col.name, col.type, col."notnull", col.pk, col.hidden
		FROM pragma_table_list AS tbl, pragma_table_xinfo(tbl.name, 'main') AS col
		WHERE tbl.schema='main' AND substr(tbl.name, 1, 7) <> 'sqlite_'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tables := make(map[string]map[string]transferColumn)
	for rows.Next() {
		var table, tableType, name string
		var column transferColumn
		if err := rows.Scan(&table, &tableType, &name, &column.Type, &column.NotNull, &column.PrimaryKey, &column.Hidden); err != nil {
			return nil, err
		}
		if tableType != "table" {
			return nil, fmt.Errorf("%w: unsupported SQLite schema object", ErrStoreIncompatible)
		}
		if tables[table] == nil {
			tables[table] = make(map[string]transferColumn)
		}
		tables[table][name] = column
	}
	return tables, rows.Err()
}

func (s *Store) transferInventory(ctx context.Context, workspaceRef, workRef, checkpointID string, withScope, legacyArtifact bool) (TransferInventory, error) {
	var inventory TransferInventory
	version, generation, err := enhancedStoreFormat(ctx, s.db)
	if err != nil {
		return inventory, err
	}
	selection, err := s.Current(ctx)
	if err != nil {
		return inventory, err
	}
	inventory.StoreID, inventory.StoreVersion, inventory.Generation = selection.StoreID, version, generation
	inventory.Bookmark.Workspace = selection.Workspace
	if workspaceRef != "" {
		inventory.Bookmark.Workspace, err = s.Workspace(ctx, workspaceRef)
		if err != nil {
			return inventory, fmt.Errorf("selected workspace: %w", err)
		}
	}
	if workRef != "" {
		work, err := s.GetWork(ctx, inventory.Bookmark.Workspace.ID, workRef)
		if err != nil {
			return inventory, fmt.Errorf("pickup work in selected workspace: %w", err)
		}
		if err := s.validateTransferWork(ctx, work); err != nil {
			return inventory, err
		}
		pack, err := s.transferContextPack(ctx, work, legacyArtifact)
		if err != nil {
			return inventory, err
		}
		inventory.Bookmark.WorkID, inventory.Bookmark.WorkKey, inventory.Bookmark.ContextDigest = pack.WorkID, pack.WorkKey, pack.Digest
		if checkpointID != "" {
			checkpoint, err := s.GetCheckpoint(ctx, work.WorkspaceID, work.ID, checkpointID)
			if err != nil {
				return inventory, fmt.Errorf("pickup checkpoint in selected work: %w", err)
			}
			if checkpoint.ID != checkpointID || checkpoint.WorkspaceID != work.WorkspaceID ||
				checkpoint.WorkID != work.ID || checkpoint.ContextDigest != work.ContextDigest {
				return inventory, fmt.Errorf("pickup checkpoint identities or Context Pack digest do not match")
			}
			inventory.Bookmark.CheckpointID = checkpoint.ID
		}
	}
	if withScope {
		if err := s.readTransferScope(ctx, &inventory); err != nil {
			return TransferInventory{}, err
		}
	}
	return inventory, nil
}

func (s *Store) transferContextPack(ctx context.Context, work WorkItem, legacyArtifact bool) (ContextPack, error) {
	if !legacyArtifact || work.ArtifactID == "" {
		return s.ContextPack(ctx, work.WorkspaceID, work.ID)
	}
	// Older SQLite artifacts lack the newer source inventory and lineage
	// columns, which do not participate in the Context Pack's rendering.
	artifact := Artifact{ID: work.ArtifactID}
	err := s.db.QueryRowContext(ctx, `SELECT version, snapshot_digest FROM artifacts WHERE id=? AND workspace_id=?`,
		work.ArtifactID, work.WorkspaceID).Scan(&artifact.Version, &artifact.SnapshotDigest)
	if err != nil {
		return ContextPack{}, err
	}
	artifact.Documents, err = getArtifactDocuments(ctx, s.db, artifact.ID)
	if err != nil {
		return ContextPack{}, err
	}
	return checkedContextPack(work, contextMarkdown(work.Key, work.Title, artifact, work.AcceptanceCriteria))
}

func (s *Store) validateTransferWork(ctx context.Context, work WorkItem) error {
	if work.ArtifactID == "" && work.FeatureID == "" {
		return nil
	}
	var sameWorkspace int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM artifacts AS a JOIN features AS f
		ON f.workspace_id=a.workspace_id AND f.key=a.feature_key
		JOIN artifact_approvals AS approval ON approval.artifact_id=a.id AND approval.workspace_id=a.workspace_id
		WHERE a.id=? AND a.workspace_id=? AND a.status='approved' AND f.id=?)`,
		work.ArtifactID, work.WorkspaceID, work.FeatureID).Scan(&sameWorkspace)
	if err != nil {
		return err
	}
	if sameWorkspace != 1 {
		return fmt.Errorf("pickup work's feature or approved artifact does not match the selected workspace")
	}
	return nil
}

func (s *Store) readTransferScope(ctx context.Context, inventory *TransferInventory) error {
	var identityBytes int64
	if err := s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM workspaces),
		(SELECT COUNT(*) FROM users), (SELECT COUNT(*) FROM work_items),
		(SELECT COALESCE(SUM(length(CAST(id||slug||name AS BLOB))),0) FROM workspaces) +
		(SELECT COALESCE(SUM(length(CAST(id||username AS BLOB))),0) FROM users)`).
		Scan(&inventory.WorkspaceCount, &inventory.UserCount, &inventory.WorkCount, &identityBytes); err != nil {
		return err
	}
	if identityBytes > 1<<20 {
		return fmt.Errorf("the Local identity inventory exceeds the 1 MiB manifest limit")
	}
	var err error
	inventory.Workspaces, err = s.ListWorkspaces(ctx)
	if err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, username FROM users ORDER BY username`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var user TransferUser
		if err := rows.Scan(&user.ID, &user.Username); err != nil {
			return err
		}
		inventory.Users = append(inventory.Users, user)
	}
	return rows.Err()
}

func (s *Store) validateTransferGuards(ctx context.Context, version string) error {
	expected := make(map[string]string)
	var writer string
	switch version {
	case enhancedStoreVersion:
		writer = enhancedWriterFunction
	case transferStoreVersion:
		writer = transferWriterFunction
	}
	if writer != "" {
		for _, table := range enhancedStoreGuardedTables {
			for _, operation := range []string{"INSERT", "UPDATE", "DELETE"} {
				name, statement := storeWriterGuard(table, operation, writer)
				expected[name] = statement
			}
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT name, sql FROM sqlite_schema WHERE type='trigger'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name, statement string
		if err := rows.Scan(&name, &statement); err != nil {
			return err
		}
		if want, ok := expected[name]; !ok || strings.Join(strings.Fields(statement), " ") != strings.Join(strings.Fields(want), " ") {
			return fmt.Errorf("%w: unsupported Local store writer guard", ErrStoreIncompatible)
		}
		delete(expected, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(expected) != 0 {
		return fmt.Errorf("%w: incomplete Local store writer guards", ErrStoreIncompatible)
	}
	return nil
}

// ReceiveTransferGeneration migrates only an already-validated private stage,
// then makes the rotated generation self-contained in the main SQLite file.
func ReceiveTransferGeneration(ctx context.Context, path string) (generation string, err error) {
	s, err := Open(path)
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, s.Close()) }()
	generation, err = s.InitializeTransferGeneration(ctx)
	if err != nil {
		return "", err
	}
	var mode string
	if err := s.db.QueryRowContext(ctx, `PRAGMA journal_mode=DELETE`).Scan(&mode); err != nil {
		return "", err
	}
	if mode != "delete" {
		return "", fmt.Errorf("could not checkpoint all receiving SQLite WAL data")
	}
	return generation, nil
}
