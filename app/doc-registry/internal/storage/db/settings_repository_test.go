package db

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/settings"
)

func TestSettingsRepository_PutBatchAndGetAll(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		sr := NewSettingsRepository(gdb)
		ctx := context.Background()

		items := []settings.Setting{
			{Key: settings.KeyMCPEnabled, Value: "true"},
			{Key: settings.KeyMCPAddr, Value: ":9090"},
		}
		if err := sr.PutBatch(ctx, items); err != nil {
			t.Fatal(err)
		}

		all, err := sr.GetAll(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 2 {
			t.Fatalf("got %d settings, want 2", len(all))
		}

		if err := sr.PutBatch(ctx, []settings.Setting{
			{Key: settings.KeyMCPAddr, Value: ":8082"},
		}); err != nil {
			t.Fatal(err)
		}
		s, err := sr.Get(ctx, settings.KeyMCPAddr)
		if err != nil {
			t.Fatal(err)
		}
		if s == nil || s.Value != ":8082" {
			t.Fatalf("upsert failed: got %v", s)
		}
	})
}

func TestSettingsRepository_GetNotFound(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		sr := NewSettingsRepository(gdb)
		ctx := context.Background()

		s, err := sr.Get(ctx, "nonexistent")
		if err != nil {
			t.Fatal(err)
		}
		if s != nil {
			t.Fatal("expected nil for missing key")
		}
	})
}

func TestSettingsRepository_Count(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		sr := NewSettingsRepository(gdb)
		ctx := context.Background()

		count, err := sr.Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("expected 0 seeded rows, got %d", count)
		}

		_ = sr.PutBatch(ctx, []settings.Setting{
			{Key: settings.KeyMCPEnabled, Value: "true", UpdatedAt: time.Now()},
		})
		count, err = sr.Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("expected 1, got %d", count)
		}
	})
}
