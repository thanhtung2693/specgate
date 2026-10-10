package local

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

type interruptedExportQueryer struct {
	artifactQueryer
	stage int
	calls int
	query string
	rows  *sql.Rows
}

func (q *interruptedExportQueryer) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	q.calls++
	if q.calls != q.stage {
		return q.artifactQueryer.QueryContext(ctx, query, args...)
	}
	rows, err := q.artifactQueryer.QueryContext(ctx, q.query)
	q.rows = rows
	return rows, err
}

func TestPortableDeliveryExportRefusesInterruptedRows(t *testing.T) {
	for _, test := range []struct {
		name  string
		stage int
		query string
	}{
		{"reports", 1, `SELECT 'report', 'work', '{}' UNION ALL SELECT 'later', 'work', json('invalid')`},
		{"reviews", 2, `SELECT 'review', 'work', '', 'passed', 'reviewed', '', '', '{}' UNION ALL SELECT 'later', 'work', '', 'passed', '', '', '', json('invalid')`},
		{"peers", 3, `SELECT 'peer', 'work', 'reviewer', '{}' UNION ALL SELECT 'later', 'work', 'reviewer', json('invalid')`},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := Open(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			// Actual SQLite yields one valid row, then an iteration error.
			probe, err := store.db.QueryContext(t.Context(), test.query)
			if err != nil {
				t.Fatal(err)
			}
			if !probe.Next() || probe.Next() || probe.Err() == nil {
				t.Fatal("fixture did not yield a valid row followed by an iteration error")
			}
			probe.Close()
			q := &interruptedExportQueryer{artifactQueryer: store.db, stage: test.stage, query: test.query}
			got, err := exportDeliveryEvidence(t.Context(), q, "workspace")
			if q.rows == nil || q.rows.Err() == nil {
				t.Fatal("export did not encounter the actual SQLite iteration error")
			}
			if err == nil || got != nil {
				t.Fatalf("interrupted export succeeded or leaked partial evidence: rows=%#v err=%v", got, err)
			}
		})
	}
}
