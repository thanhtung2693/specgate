package local

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestPortableExportUsesOneSnapshotDuringConcurrentWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.db.SetMaxOpenConns(1)
	sel, err := s.Initialize(t.Context(), InitInput{WorkspaceName: "Snapshot", Username: "human", DisplayName: "Human"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := s.CreateQuickWork(t.Context(), sel.Workspace.ID, QuickWorkInput{Title: "0", AcceptanceCriteria: []string{"Keep data consistent"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(t.Context(), `INSERT INTO delivery_reports(id,workspace_id,work_id,context_digest,body,created_at) VALUES('report',?,?,?,'{"generation":"0"}','2026-10-04T00:00:00Z')`, sel.Workspace.ID, work.ID, work.ContextDigest); err != nil {
		t.Fatal(err)
	}
	writer, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	done := make(chan error, 1)
	ready := make(chan struct{})
	go func() {
		for generation := 1; ; generation++ {
			if ctx.Err() != nil {
				done <- nil
				return
			}
			tx, err := writer.db.BeginTx(ctx, nil)
			if err == nil {
				_, err = tx.ExecContext(ctx, `UPDATE work_items SET title=? WHERE id=?`, fmt.Sprint(generation), work.ID)
				if err == nil {
					_, err = tx.ExecContext(ctx, `UPDATE delivery_reports SET body=? WHERE id='report'`, fmt.Sprintf(`{"generation":"%d"}`, generation))
				}
				if err == nil {
					err = tx.Commit()
				} else {
					_ = tx.Rollback()
				}
			}
			if err != nil {
				if errors.Is(err, sql.ErrTxDone) && ctx.Err() != nil {
					err = ctx.Err()
				}
				done <- err
				return
			}
			if generation == 1 {
				close(ready)
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("concurrent writer: %v", err)
		}
	})
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatalf("writer did not become ready: %v", ctx.Err())
	case err := <-done:
		done <- err
		t.Fatalf("writer stopped before export: %v", err)
	}
	for attempt := 0; attempt < 100; attempt++ {
		bundle, err := s.ExportWorkspace(ctx, sel.Workspace.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(bundle.Work) != 1 || len(bundle.Delivery) != 1 {
			t.Fatalf("export lost work or report: %+v", bundle)
		}
		if got := bundle.Delivery[0].Report["generation"]; got != bundle.Work[0].Title {
			t.Fatalf("export mixed committed generations: work=%q report=%v", bundle.Work[0].Title, got)
		}
	}
}
