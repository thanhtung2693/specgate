package db

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/governancefiles"
)

func TestGovernanceFilesRepository_CreateAndCommit(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewGovernanceFilesRepository(gdb)
		ctx := governancefiles.WithWorkspace(context.Background(), "ws-default")

		now := time.Now().UTC().Truncate(time.Second)
		created, err := repo.Create(ctx, governancefiles.File{
			ID:         "f1",
			Name:       "hello.png",
			Mime:       "image/png",
			SizeBytes:  1234,
			ObjectKey:  "governance/resources/uploads/f1.png",
			Status:     governancefiles.StatusPending,
			CreatedAt:  now,
			LastUsedAt: now,
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if created.Status != governancefiles.StatusPending {
			t.Fatalf("status = %q, want pending", created.Status)
		}

		later := now.Add(time.Minute)
		committed, err := repo.Commit(ctx, "f1", later)
		if err != nil {
			t.Fatalf("Commit: %v", err)
		}
		if committed.Status != governancefiles.StatusReady {
			t.Fatalf("status = %q, want ready", committed.Status)
		}
		if !committed.LastUsedAt.Equal(later) {
			t.Fatalf("last_used_at = %v, want %v", committed.LastUsedAt, later)
		}
	})
}

func TestGovernanceFilesRepository_WorkspaceScopesFiles(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewGovernanceFilesRepository(gdb)
		now := time.Now().UTC().Truncate(time.Second)
		for _, f := range []governancefiles.File{
			{ID: "ws-a-file", WorkspaceID: "ws-a", Name: "same.md", Mime: "text/markdown", SizeBytes: 1, ObjectKey: "k/ws-a.md", Status: governancefiles.StatusReady, CreatedAt: now, LastUsedAt: now},
			{ID: "ws-b-file", WorkspaceID: "ws-b", Name: "same.md", Mime: "text/markdown", SizeBytes: 1, ObjectKey: "k/ws-b.md", Status: governancefiles.StatusReady, CreatedAt: now, LastUsedAt: now},
		} {
			if _, err := repo.Create(governancefiles.WithWorkspace(context.Background(), f.WorkspaceID), f); err != nil {
				t.Fatalf("seed %s: %v", f.ID, err)
			}
		}

		ctxA := governancefiles.WithWorkspace(context.Background(), "ws-a")
		items, total, err := repo.List(ctxA, governancefiles.ListFilter{Limit: 10})
		if err != nil {
			t.Fatalf("List ws-a: %v", err)
		}
		if total != 1 || len(items) != 1 || items[0].ID != "ws-a-file" {
			t.Fatalf("ws-a list = %+v / total=%d, want ws-a only", items, total)
		}
		if _, err := repo.Get(ctxA, "ws-b-file"); err != governancefiles.ErrNotFound {
			t.Fatalf("cross-workspace Get error = %v, want ErrNotFound", err)
		}
		if _, err := repo.Delete(ctxA, "ws-b-file"); err != governancefiles.ErrNotFound {
			t.Fatalf("cross-workspace Delete error = %v, want ErrNotFound", err)
		}
	})
}

func TestGovernanceFilesRepository_ListOrdersByLastUsedDesc(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewGovernanceFilesRepository(gdb)
		ctx := governancefiles.WithWorkspace(context.Background(), "ws-default")
		now := time.Now().UTC().Truncate(time.Second)

		seed := []governancefiles.File{
			{ID: "a", Name: "a.png", Mime: "image/png", SizeBytes: 1, ObjectKey: "k/a.png",
				Status: governancefiles.StatusReady, CreatedAt: now.Add(-3 * time.Hour), LastUsedAt: now.Add(-3 * time.Hour)},
			{ID: "b", Name: "B picture.PNG", Mime: "image/png", SizeBytes: 2, ObjectKey: "k/b.png",
				Status: governancefiles.StatusReady, CreatedAt: now.Add(-2 * time.Hour), LastUsedAt: now.Add(-1 * time.Hour)},
			{ID: "c", Name: "c.md", Mime: "text/markdown", SizeBytes: 3, ObjectKey: "k/c.md",
				Status: governancefiles.StatusPending, CreatedAt: now, LastUsedAt: now},
		}
		for _, f := range seed {
			if _, err := repo.Create(ctx, f); err != nil {
				t.Fatalf("seed %s: %v", f.ID, err)
			}
		}

		items, total, err := repo.List(ctx, governancefiles.ListFilter{Limit: 10})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if total != 2 {
			t.Fatalf("total = %d, want 2 (ready only)", total)
		}
		if len(items) != 2 || items[0].ID != "b" || items[1].ID != "a" {
			t.Fatalf("order = %+v, want [b,a]", items)
		}

		// q matches case-insensitively on name
		items, total, err = repo.List(ctx, governancefiles.ListFilter{Q: "picture", Limit: 10})
		if err != nil {
			t.Fatalf("List q: %v", err)
		}
		if total != 1 || len(items) != 1 || items[0].ID != "b" {
			t.Fatalf("q=picture got %+v / total=%d", items, total)
		}
	})
}

func TestGovernanceFilesRepository_Touch(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewGovernanceFilesRepository(gdb)
		ctx := governancefiles.WithWorkspace(context.Background(), "ws-default")
		now := time.Now().UTC().Truncate(time.Second)
		_, _ = repo.Create(ctx, governancefiles.File{
			ID: "t1", Name: "x.png", Mime: "image/png", SizeBytes: 1,
			ObjectKey: "k/t1.png", Status: governancefiles.StatusReady,
			CreatedAt: now.Add(-time.Hour), LastUsedAt: now.Add(-time.Hour),
		})

		later := now.Add(2 * time.Hour)
		got, err := repo.Touch(ctx, "t1", later)
		if err != nil {
			t.Fatalf("Touch: %v", err)
		}
		if !got.LastUsedAt.Equal(later) {
			t.Fatalf("last_used_at = %v, want %v", got.LastUsedAt, later)
		}

		if _, err := repo.Touch(ctx, "missing", later); err == nil {
			t.Fatalf("Touch missing: want error")
		}
	})
}

func TestGovernanceFilesRepository_DeleteStaleReady(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewGovernanceFilesRepository(gdb)
		ctx := governancefiles.WithWorkspace(context.Background(), "ws-default")
		now := time.Now().UTC().Truncate(time.Second)
		fresh := now.Add(-30 * 24 * time.Hour)
		stale := now.Add(-90 * 24 * time.Hour)

		_, _ = repo.Create(ctx, governancefiles.File{
			ID: "fresh", Name: "f.png", Mime: "image/png", SizeBytes: 1,
			ObjectKey: "k/fresh.png", Status: governancefiles.StatusReady,
			CreatedAt: fresh, LastUsedAt: fresh,
		})
		_, _ = repo.Create(ctx, governancefiles.File{
			ID: "stale", Name: "s.png", Mime: "image/png", SizeBytes: 1,
			ObjectKey: "k/stale.png", Status: governancefiles.StatusReady,
			CreatedAt: stale, LastUsedAt: stale,
		})

		cutoff := now.Add(-60 * 24 * time.Hour)
		keys, err := repo.DeleteStaleReady(ctx, cutoff)
		if err != nil {
			t.Fatalf("DeleteStaleReady: %v", err)
		}
		if len(keys) != 1 || keys[0] != "k/stale.png" {
			t.Fatalf("returned keys = %v, want [k/stale.png]", keys)
		}
		if _, err := repo.Get(ctx, "fresh"); err != nil {
			t.Fatalf("Get fresh: %v", err)
		}
	})
}

func TestGovernanceFilesRepository_DeleteStaleReadyKeepsArtifactFileRefs(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewGovernanceFilesRepository(gdb)
		ctx := governancefiles.WithWorkspace(context.Background(), "ws-default")
		now := time.Now().UTC().Truncate(time.Second)
		stale := now.Add(-90 * 24 * time.Hour)

		_, _ = repo.Create(ctx, governancefiles.File{
			ID: "pinned", Name: "tasks-be.md", Mime: "text/markdown", SizeBytes: 42,
			ObjectKey: "uploads/pinned.md", Status: governancefiles.StatusReady,
			CreatedAt: stale, LastUsedAt: stale,
		})
		if err := gdb.WithContext(ctx).Exec(
			`INSERT INTO artifacts
			 (id, workspace_id, feature_id, version, status, request_type, impact_level, created_by, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"artifact-1", "ws-default", "feature-1", "v0.1", "approved", "new_feature", "low", "governance-ops", now, now,
		).Error; err != nil {
			t.Fatalf("insert artifact: %v", err)
		}
		if err := gdb.WithContext(ctx).Exec(
			`INSERT INTO artifact_files (artifact_id, path, role, object_key, size_bytes, content_sha256)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			"artifact-1", "tasks-be.md", "plan", "uploads/pinned.md", 42, "sha256:5b40d99a1a9f5b96d548b6f0f3c0c1df86e93c11c545a16a0bb1dcf7a48c7d99",
		).Error; err != nil {
			t.Fatalf("insert artifact file ref: %v", err)
		}

		keys, err := repo.DeleteStaleReady(ctx, now.Add(-60*24*time.Hour))
		if err != nil {
			t.Fatalf("DeleteStaleReady: %v", err)
		}
		if len(keys) != 0 {
			t.Fatalf("returned keys = %v, want none", keys)
		}
		if _, err := repo.Get(ctx, "pinned"); err != nil {
			t.Fatalf("Get pinned governance file: %v", err)
		}
	})
}

func TestGovernanceFilesRepository_DeleteKeepsArtifactFileRefs(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewGovernanceFilesRepository(gdb)
		ctx := governancefiles.WithWorkspace(context.Background(), "ws-default")
		now := time.Now().UTC().Truncate(time.Second)
		const key = "workspaces/ws-default/governance/resources/uploads/pinned.md"

		if _, err := repo.Create(ctx, governancefiles.File{
			ID: "pinned", Name: "tasks-be.md", Mime: "text/markdown", SizeBytes: 42,
			ObjectKey: key, Status: governancefiles.StatusReady, CreatedAt: now, LastUsedAt: now,
		}); err != nil {
			t.Fatalf("Create governance file: %v", err)
		}
		if err := gdb.WithContext(ctx).Exec(
			`INSERT INTO artifacts
			 (id, workspace_id, feature_id, version, status, request_type, impact_level, created_by, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"artifact-1", "ws-default", "feature-1", "v0.1", "approved", "new_feature", "low", "governance-ops", now, now,
		).Error; err != nil {
			t.Fatalf("insert artifact: %v", err)
		}
		if err := gdb.WithContext(ctx).Exec(
			`INSERT INTO artifact_files (artifact_id, path, role, object_key, size_bytes, content_sha256)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			"artifact-1", "tasks-be.md", "plan", key, 42, "sha256:5b40d99a1a9f5b96d548b6f0f3c0c1df86e93c11c545a16a0bb1dcf7a48c7d99",
		).Error; err != nil {
			t.Fatalf("insert artifact file ref: %v", err)
		}

		if _, err := repo.Delete(ctx, "pinned"); err != governancefiles.ErrReferenced {
			t.Fatalf("Delete error = %v, want ErrReferenced", err)
		}
		if _, err := repo.Get(ctx, "pinned"); err != nil {
			t.Fatalf("Get pinned governance file after rejected delete: %v", err)
		}
	})
}
