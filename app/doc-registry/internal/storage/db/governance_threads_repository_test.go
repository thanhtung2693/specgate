package db

import (
	"context"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/governancethreads"
	"gorm.io/gorm"
)

func TestGovernanceThreadsRepository_ListUpsertAndArchive(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		t.Helper()
		repo := NewGovernanceThreadsRepository(gdb)
		ctx := context.Background()
		base := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)

		_, err := repo.Upsert(ctx, governancethreads.Thread{
			ThreadID:  "thread-old",
			Title:     "Old thread",
			Preview:   "old preview",
			CreatedAt: base,
			UpdatedAt: base,
		})
		if err != nil {
			t.Fatalf("upsert old: %v", err)
		}
		_, err = repo.Upsert(ctx, governancethreads.Thread{
			ThreadID:  "thread-new",
			Title:     "New thread",
			Preview:   "new preview",
			CreatedAt: base.Add(time.Minute),
			UpdatedAt: base.Add(time.Minute),
		})
		if err != nil {
			t.Fatalf("upsert new: %v", err)
		}

		items, total, err := repo.List(ctx, governancethreads.ListFilter{Limit: 1})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if total != 2 || len(items) != 1 || items[0].ThreadID != "thread-new" {
			t.Fatalf("unexpected first page total=%d items=%+v", total, items)
		}

		_, err = repo.Upsert(ctx, governancethreads.Thread{
			ThreadID:  "thread-old",
			Title:     "Renamed thread",
			Preview:   "renamed preview",
			CreatedAt: base,
			UpdatedAt: base.Add(2 * time.Minute),
		})
		if err != nil {
			t.Fatalf("upsert existing: %v", err)
		}
		items, total, err = repo.List(ctx, governancethreads.ListFilter{Limit: 10})
		if err != nil {
			t.Fatalf("list after rename: %v", err)
		}
		if total != 2 || len(items) != 2 || items[0].ThreadID != "thread-old" || items[0].Title != "Renamed thread" {
			t.Fatalf("unexpected renamed order total=%d items=%+v", total, items)
		}

		if err := repo.Delete(ctx, "thread-old", base.Add(3*time.Minute)); err != nil {
			t.Fatalf("delete: %v", err)
		}
		items, total, err = repo.List(ctx, governancethreads.ListFilter{Limit: 10})
		if err != nil {
			t.Fatalf("list after archive: %v", err)
		}
		if total != 1 || len(items) != 1 || items[0].ThreadID != "thread-new" {
			t.Fatalf("archived thread still visible total=%d items=%+v", total, items)
		}
	})
}
