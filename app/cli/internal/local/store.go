package local

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email,omitempty"`
}

type Workspace struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type Selection struct {
	StoreID   string    `json:"store_id"`
	User      User      `json:"user"`
	Workspace Workspace `json:"workspace"`
}

type InitInput struct {
	WorkspaceName string
	DisplayName   string
	Username      string
	Email         string
}

func Open(path string) (*Store, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	path = canonicalizeDarwinRootAlias(absolutePath)
	if err := ensureRealDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	if err := prepareDatabaseFile(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		return nil, err
	}
	for _, statement := range []string{
		"PRAGMA journal_mode = WAL",
		`CREATE TABLE IF NOT EXISTS metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, username TEXT UNIQUE NOT NULL, display_name TEXT NOT NULL, email TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS workspaces (id TEXT PRIMARY KEY, slug TEXT UNIQUE NOT NULL, name TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS selection (id INTEGER PRIMARY KEY CHECK (id = 1), user_id TEXT NOT NULL, workspace_id TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS artifacts (id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id), feature_key TEXT NOT NULL, request_type TEXT NOT NULL, version INTEGER NOT NULL, status TEXT NOT NULL, snapshot_digest TEXT NOT NULL, policy_digest TEXT NOT NULL DEFAULT '', policy_snapshot_json TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, UNIQUE(workspace_id, feature_key, version))`,
		`CREATE TABLE IF NOT EXISTS artifact_documents (artifact_id TEXT NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE, path TEXT NOT NULL, role TEXT NOT NULL, content BLOB NOT NULL, digest TEXT NOT NULL, PRIMARY KEY(artifact_id, path))`,
		`CREATE TABLE IF NOT EXISTS artifact_readiness_runs (id TEXT PRIMARY KEY, artifact_id TEXT NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE, workspace_id TEXT NOT NULL REFERENCES workspaces(id), aggregate TEXT NOT NULL, evidence TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS local_gate_tasks (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id),
  artifact_id TEXT NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
  gate_key TEXT NOT NULL,
  gate_version TEXT NOT NULL,
  gate_digest TEXT NOT NULL,
  artifact_digest TEXT NOT NULL,
  policy_digest TEXT NOT NULL,
  executor TEXT NOT NULL,
  skill_content TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  result_id TEXT,
  result_state TEXT,
  result_summary TEXT,
  evaluator_json TEXT,
  evidence_json TEXT,
  findings_json TEXT,
  submitted_at TEXT
)`,
		`CREATE TABLE IF NOT EXISTS artifact_approvals (artifact_id TEXT PRIMARY KEY REFERENCES artifacts(id) ON DELETE CASCADE, workspace_id TEXT NOT NULL REFERENCES workspaces(id), actor TEXT NOT NULL, note TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS features (id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id), key TEXT NOT NULL, canonical_artifact_id TEXT NOT NULL REFERENCES artifacts(id), version INTEGER NOT NULL, created_at TEXT NOT NULL, UNIQUE(workspace_id, key))`,
		`CREATE TABLE IF NOT EXISTS work_items (id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id), key TEXT NOT NULL, feature_id TEXT REFERENCES features(id), artifact_id TEXT REFERENCES artifacts(id), title TEXT NOT NULL, description TEXT NOT NULL, phase TEXT NOT NULL, context_digest TEXT NOT NULL, acceptance_criteria TEXT NOT NULL, created_at TEXT NOT NULL, UNIQUE(workspace_id, key))`,
		`CREATE TABLE IF NOT EXISTS delivery_reports (id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id), work_id TEXT NOT NULL REFERENCES work_items(id), context_digest TEXT NOT NULL, body TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS delivery_reviews (id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id), work_id TEXT NOT NULL REFERENCES work_items(id), report_id TEXT NOT NULL DEFAULT '', verdict TEXT NOT NULL, summary TEXT NOT NULL, human_decision TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS delivery_peer_reviews (id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id), work_id TEXT NOT NULL REFERENCES work_items(id), agent_name TEXT NOT NULL, body TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS audit_events (id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id), work_id TEXT NOT NULL REFERENCES work_items(id), action TEXT NOT NULL, detail TEXT NOT NULL, created_at TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err := ensureQuickWorkSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureDeliveryReviewReportBinding(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func ensureQuickWorkSchema(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(work_items)`)
	if err != nil {
		return err
	}
	requiresMigration := false
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if (name == "feature_id" || name == "artifact_id") && notNull != 0 {
			requiresMigration = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !requiresMigration {
		return nil
	}

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`) //nolint:errcheck
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS work_items_quick_migration`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE work_items_quick_migration (
		id TEXT PRIMARY KEY,
		workspace_id TEXT NOT NULL REFERENCES workspaces(id),
		key TEXT NOT NULL,
		feature_id TEXT REFERENCES features(id),
		artifact_id TEXT REFERENCES artifacts(id),
		title TEXT NOT NULL,
		description TEXT NOT NULL,
		phase TEXT NOT NULL,
		context_digest TEXT NOT NULL,
		acceptance_criteria TEXT NOT NULL,
		created_at TEXT NOT NULL,
		UNIQUE(workspace_id, key)
	)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO work_items_quick_migration
		(id, workspace_id, key, feature_id, artifact_id, title, description, phase, context_digest, acceptance_criteria, created_at)
		SELECT id, workspace_id, key, feature_id, artifact_id, title, description, phase, context_digest, acceptance_criteria, created_at
		FROM work_items`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE work_items`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE work_items_quick_migration RENAME TO work_items`); err != nil {
		return err
	}
	return tx.Commit()
}

func ensureDeliveryReviewReportBinding(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(delivery_reviews)`)
	if err != nil {
		return err
	}
	hasReportID := false
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		hasReportID = hasReportID || name == "report_id"
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !hasReportID {
		if _, err := db.Exec(`ALTER TABLE delivery_reviews ADD COLUMN report_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if _, err := db.Exec(`
		UPDATE delivery_reviews
		SET report_id = COALESCE((
			SELECT reports.id
			FROM delivery_reports AS reports
			WHERE reports.workspace_id = delivery_reviews.workspace_id
			  AND reports.work_id = delivery_reviews.work_id
			  AND reports.created_at <= delivery_reviews.created_at
			ORDER BY reports.created_at DESC, reports.id DESC
			LIMIT 1
		), '')
		WHERE report_id = ''`); err != nil {
		return err
	}
	_, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_delivery_reviews_report ON delivery_reviews(report_id) WHERE report_id <> ''`)
	return err
}

func canonicalizeDarwinRootAlias(path string) string {
	if runtime.GOOS != "darwin" {
		return path
	}
	for _, alias := range []string{"/tmp", "/var"} {
		if path == alias || strings.HasPrefix(path, alias+"/") {
			return "/private" + path
		}
	}
	return path
}

func ensureRealDirectory(path string) error {
	if err := rejectSymlinkedPathComponents(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
		if err := rejectSymlinkedPathComponents(path); err != nil {
			return err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("local state directory %s must be a real directory", path)
	}
	return nil
}

func rejectSymlinkedPathComponents(path string) error {
	for current := filepath.Clean(path); ; {
		info, err := os.Lstat(current)
		switch {
		case err == nil && info.Mode()&os.ModeSymlink != 0:
			return fmt.Errorf("local state path %s contains symlink %s", path, current)
		case err != nil && !os.IsNotExist(err):
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

func prepareDatabaseFile(path string) error {
	info, err := os.Lstat(path)
	switch {
	case os.IsNotExist(err):
		file, createErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if createErr != nil {
			return createErr
		}
		if closeErr := file.Close(); closeErr != nil {
			return closeErr
		}
	case err != nil:
		return err
	case info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular():
		return fmt.Errorf("local database %s must be a regular file, not a symlink", path)
	}
	return os.Chmod(path, 0o600)
}

func sqliteDSN(path string) string {
	slashPath := filepath.ToSlash(path)
	if isWindowsDriveAbsolute(slashPath) {
		slashPath = "/" + slashPath
	}
	uri := url.URL{Scheme: "file", Path: slashPath}
	query := uri.Query()
	query.Set("_txlock", "immediate")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(ON)")
	uri.RawQuery = query.Encode()
	return uri.String()
}

func isWindowsDriveAbsolute(path string) bool {
	if len(path) < 3 || path[1] != ':' || path[2] != '/' {
		return false
	}
	return path[0] >= 'A' && path[0] <= 'Z' || path[0] >= 'a' && path[0] <= 'z'
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Initialize(ctx context.Context, in InitInput) (Selection, error) {
	if strings.TrimSpace(in.WorkspaceName) == "" || strings.TrimSpace(in.DisplayName) == "" || strings.TrimSpace(in.Username) == "" {
		return Selection{}, fmt.Errorf("workspace name, display name, and username are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Selection{}, err
	}
	defer tx.Rollback()
	storeID, err := metadata(ctx, tx, "store_id")
	if err == sql.ErrNoRows {
		storeID, err = newID()
		if err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO metadata(key, value) VALUES ('store_id', ?)`, storeID)
		}
	}
	if err != nil {
		return Selection{}, err
	}
	username := strings.ToLower(strings.TrimSpace(in.Username))
	user := User{Username: username, DisplayName: strings.TrimSpace(in.DisplayName), Email: strings.TrimSpace(in.Email)}
	if err := tx.QueryRowContext(ctx, `SELECT id, username, display_name, email FROM users WHERE username = ?`, username).Scan(&user.ID, &user.Username, &user.DisplayName, &user.Email); err == sql.ErrNoRows {
		user.ID, err = newID()
		if err != nil {
			return Selection{}, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO users(id, username, display_name, email) VALUES (?, ?, ?, ?)`, user.ID, user.Username, user.DisplayName, user.Email); err != nil {
			return Selection{}, err
		}
	} else if err != nil {
		return Selection{}, err
	}
	workspace := Workspace{Slug: slug(in.WorkspaceName), Name: strings.TrimSpace(in.WorkspaceName)}
	if err := tx.QueryRowContext(ctx, `SELECT id, slug, name FROM workspaces WHERE slug = ?`, workspace.Slug).Scan(&workspace.ID, &workspace.Slug, &workspace.Name); err == sql.ErrNoRows {
		workspace.ID, err = newID()
		if err != nil {
			return Selection{}, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO workspaces(id, slug, name) VALUES (?, ?, ?)`, workspace.ID, workspace.Slug, workspace.Name); err != nil {
			return Selection{}, err
		}
	} else if err != nil {
		return Selection{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO selection(id, user_id, workspace_id) VALUES (1, ?, ?) ON CONFLICT(id) DO UPDATE SET user_id = excluded.user_id, workspace_id = excluded.workspace_id`, user.ID, workspace.ID); err != nil {
		return Selection{}, err
	}
	if err = tx.Commit(); err != nil {
		return Selection{}, err
	}
	return Selection{StoreID: storeID, User: user, Workspace: workspace}, nil
}

func (s *Store) Current(ctx context.Context) (Selection, error) {
	var result Selection
	err := s.db.QueryRowContext(ctx, `SELECT m.value, u.id, u.username, u.display_name, u.email, w.id, w.slug, w.name FROM metadata m JOIN selection s ON s.id = 1 JOIN users u ON u.id = s.user_id JOIN workspaces w ON w.id = s.workspace_id WHERE m.key = 'store_id'`).Scan(&result.StoreID, &result.User.ID, &result.User.Username, &result.User.DisplayName, &result.User.Email, &result.Workspace.ID, &result.Workspace.Slug, &result.Workspace.Name)
	return result, err
}

func (s *Store) CreateWorkspace(ctx context.Context, name string) (Workspace, error) {
	workspace := Workspace{Slug: slug(name), Name: strings.TrimSpace(name)}
	if workspace.Slug == "" {
		return Workspace{}, fmt.Errorf("workspace name is required")
	}
	if err := s.db.QueryRowContext(ctx, `SELECT id, slug, name FROM workspaces WHERE slug = ?`, workspace.Slug).Scan(&workspace.ID, &workspace.Slug, &workspace.Name); err == nil {
		return workspace, nil
	} else if err != sql.ErrNoRows {
		return Workspace{}, err
	}
	var err error
	workspace.ID, err = newID()
	if err != nil {
		return Workspace{}, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO workspaces(id, slug, name) VALUES (?, ?, ?)`, workspace.ID, workspace.Slug, workspace.Name); err != nil {
		return Workspace{}, err
	}
	return workspace, nil
}

func (s *Store) SelectWorkspace(ctx context.Context, ref string) error {
	workspace, err := s.Workspace(ctx, ref)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE selection SET workspace_id = ? WHERE id = 1`, workspace.ID)
	return err
}

// Workspace returns one Local workspace by immutable id or slug.
func (s *Store) Workspace(ctx context.Context, ref string) (Workspace, error) {
	var workspace Workspace
	err := s.db.QueryRowContext(ctx, `SELECT id, slug, name FROM workspaces WHERE id = ? OR slug = ?`, ref, ref).Scan(&workspace.ID, &workspace.Slug, &workspace.Name)
	return workspace, err
}

func (s *Store) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, slug, name FROM workspaces ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var workspaces []Workspace
	for rows.Next() {
		var workspace Workspace
		if err := rows.Scan(&workspace.ID, &workspace.Slug, &workspace.Name); err != nil {
			return nil, err
		}
		workspaces = append(workspaces, workspace)
	}
	return workspaces, rows.Err()
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, username, display_name, email FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.Username, &user.DisplayName, &user.Email); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Store) ClearSelection(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM selection WHERE id = 1`)
	return err
}

func metadata(ctx context.Context, tx *sql.Tx, key string) (string, error) {
	var value string
	err := tx.QueryRowContext(ctx, `SELECT value FROM metadata WHERE key = ?`, key).Scan(&value)
	return value, err
}

func newID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func slug(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
