package local

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenBackfillsLegacyDeliveryReviewReportBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE delivery_reports (id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, work_id TEXT NOT NULL, context_digest TEXT NOT NULL, body TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE delivery_reviews (id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, work_id TEXT NOT NULL, verdict TEXT NOT NULL, summary TEXT NOT NULL, human_decision TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
		`INSERT INTO delivery_reports(id, workspace_id, work_id, context_digest, body, created_at) VALUES ('report-1', 'ws-1', 'work-1', 'digest', '{}', '2026-07-19T00:00:00Z')`,
		`INSERT INTO delivery_reviews(id, workspace_id, work_id, verdict, summary, created_at) VALUES ('review-1', 'ws-1', 'work-1', 'passed', 'ready', '2026-07-19T00:00:01Z')`,
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
	var reportID string
	if err := store.db.QueryRow(`SELECT report_id FROM delivery_reviews WHERE id = 'review-1'`).Scan(&reportID); err != nil {
		t.Fatal(err)
	}
	if reportID != "report-1" {
		t.Fatalf("report_id = %q, want report-1", reportID)
	}
}
