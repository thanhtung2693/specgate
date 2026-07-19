package db

import (
	"context"
	"errors"
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
			ThreadID:    "thread-old",
			WorkspaceID: "ws-a",
			Title:       "Old thread",
			Preview:     "old preview",
			CreatedAt:   base,
			UpdatedAt:   base,
		})
		if err != nil {
			t.Fatalf("upsert old: %v", err)
		}
		_, err = repo.Upsert(ctx, governancethreads.Thread{
			ThreadID:    "thread-new",
			WorkspaceID: "ws-a",
			Title:       "New thread",
			Preview:     "new preview",
			CreatedAt:   base.Add(time.Minute),
			UpdatedAt:   base.Add(time.Minute),
		})
		if err != nil {
			t.Fatalf("upsert new: %v", err)
		}

		items, total, err := repo.List(ctx, governancethreads.ListFilter{WorkspaceID: "ws-a", Limit: 1})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if total != 2 || len(items) != 1 || items[0].ThreadID != "thread-new" {
			t.Fatalf("unexpected first page total=%d items=%+v", total, items)
		}

		_, err = repo.Upsert(ctx, governancethreads.Thread{
			ThreadID:    "thread-old",
			WorkspaceID: "ws-a",
			Title:       "Renamed thread",
			Preview:     "renamed preview",
			CreatedAt:   base,
			UpdatedAt:   base.Add(2 * time.Minute),
		})
		if err != nil {
			t.Fatalf("upsert existing: %v", err)
		}
		items, total, err = repo.List(ctx, governancethreads.ListFilter{WorkspaceID: "ws-a", Limit: 10})
		if err != nil {
			t.Fatalf("list after rename: %v", err)
		}
		if total != 2 || len(items) != 2 || items[0].ThreadID != "thread-old" || items[0].Title != "Renamed thread" {
			t.Fatalf("unexpected renamed order total=%d items=%+v", total, items)
		}

		if err := repo.Delete(ctx, "ws-a", "thread-old", base.Add(3*time.Minute)); err != nil {
			t.Fatalf("delete: %v", err)
		}
		items, total, err = repo.List(ctx, governancethreads.ListFilter{WorkspaceID: "ws-a", Limit: 10})
		if err != nil {
			t.Fatalf("list after archive: %v", err)
		}
		if total != 1 || len(items) != 1 || items[0].ThreadID != "thread-new" {
			t.Fatalf("archived thread still visible total=%d items=%+v", total, items)
		}
	})
}

func TestGovernanceThreadsRepositoryRejectsCrossWorkspaceAccess(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewGovernanceThreadsRepository(gdb)
		ctx := context.Background()
		now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
		if _, err := repo.Upsert(ctx, governancethreads.Thread{ThreadID: "same-id", WorkspaceID: "ws-a", Title: "A", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if _, err := repo.Upsert(ctx, governancethreads.Thread{ThreadID: "same-id", WorkspaceID: "ws-b", Title: "B", CreatedAt: now, UpdatedAt: now}); !errors.Is(err, governancethreads.ErrNotFound) {
			t.Fatalf("cross-workspace upsert err=%v, want not found", err)
		}
		items, total, err := repo.List(ctx, governancethreads.ListFilter{WorkspaceID: "ws-b", Limit: 10})
		if err != nil || total != 0 || len(items) != 0 {
			t.Fatalf("cross-workspace list total=%d items=%v err=%v", total, items, err)
		}
		if err := repo.Delete(ctx, "ws-b", "same-id", now); !errors.Is(err, governancethreads.ErrNotFound) {
			t.Fatalf("cross-workspace delete err=%v, want not found", err)
		}
	})
}
