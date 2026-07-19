package local

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenMigratesExistingWorkItemsForQuickRouteWithoutLosingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE workspaces (id TEXT PRIMARY KEY, slug TEXT UNIQUE NOT NULL, name TEXT NOT NULL)`,
		`CREATE TABLE artifacts (id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id), feature_key TEXT NOT NULL, request_type TEXT NOT NULL, version INTEGER NOT NULL, status TEXT NOT NULL, snapshot_digest TEXT NOT NULL, policy_digest TEXT NOT NULL DEFAULT '', policy_snapshot_json TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, UNIQUE(workspace_id, feature_key, version))`,
		`CREATE TABLE features (id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id), key TEXT NOT NULL, canonical_artifact_id TEXT NOT NULL REFERENCES artifacts(id), version INTEGER NOT NULL, created_at TEXT NOT NULL, UNIQUE(workspace_id, key))`,
		`CREATE TABLE work_items (id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id), key TEXT NOT NULL, feature_id TEXT NOT NULL REFERENCES features(id), artifact_id TEXT NOT NULL REFERENCES artifacts(id), title TEXT NOT NULL, description TEXT NOT NULL, phase TEXT NOT NULL, context_digest TEXT NOT NULL, acceptance_criteria TEXT NOT NULL, created_at TEXT NOT NULL, UNIQUE(workspace_id, key))`,
		`CREATE TABLE delivery_reports (id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id), work_id TEXT NOT NULL REFERENCES work_items(id), context_digest TEXT NOT NULL, body TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`INSERT INTO workspaces VALUES ('ws-1', 'alpha', 'Alpha')`,
		`INSERT INTO artifacts VALUES ('artifact-1', 'ws-1', 'LOGIN', 'bugfix', 1, 'approved', 'digest', '', '', '2026-07-19T00:00:00Z')`,
		`INSERT INTO features VALUES ('feature-1', 'ws-1', 'LOGIN', 'artifact-1', 1, '2026-07-19T00:00:00Z')`,
		`INSERT INTO work_items VALUES ('work-1', 'ws-1', 'LOCAL-OLD', 'feature-1', 'artifact-1', 'Existing work', '', 'ready', 'context', '["Works"]', '2026-07-19T00:00:00Z')`,
		`INSERT INTO delivery_reports VALUES ('report-1', 'ws-1', 'work-1', 'context', '{"summary":"preserved"}', '2026-07-19T00:01:00Z')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	existing, err := store.GetWork(context.Background(), "ws-1", "LOCAL-OLD")
	if err != nil {
		t.Fatal(err)
	}
	if existing.ArtifactID != "artifact-1" || existing.Title != "Existing work" {
		t.Fatalf("existing work changed: %#v", existing)
	}
	report, err := store.LatestDeliveryReport(context.Background(), "ws-1", "LOCAL-OLD")
	if err != nil || report.ID != "report-1" {
		t.Fatalf("existing delivery report changed: %#v, %v", report, err)
	}
	if _, err := store.CreateQuickWork(context.Background(), "ws-1", QuickWorkInput{
		Title: "Quick work", AcceptanceCriteria: []string{"Works"},
	}); err != nil {
		t.Fatalf("create quick work after migration: %v", err)
	}
	foreignKeyRows, err := store.db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer foreignKeyRows.Close()
	if foreignKeyRows.Next() {
		t.Fatal("migration left a broken foreign-key relationship")
	}
	if err := foreignKeyRows.Err(); err != nil {
		t.Fatal(err)
	}
}
