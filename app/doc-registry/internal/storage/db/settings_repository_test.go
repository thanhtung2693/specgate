package db

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/settings"
)

func TestSettingsRepository_PutBatchAndGetAll(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		sr := NewSettingsRepository(gdb)
		ctx := context.Background()

		items := []settings.Setting{
			{Key: settings.KeyGovernanceModelProvider, Value: "openai"},
			{Key: settings.KeyGovernanceModel, Value: "gpt-5"},
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
			{Key: settings.KeyGovernanceModel, Value: "gpt-5-mini"},
		}); err != nil {
			t.Fatal(err)
		}
		all, err = sr.GetAll(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 2 {
			t.Fatalf("got %d settings after upsert, want 2", len(all))
		}
		values := make(map[string]string, len(all))
		for _, item := range all {
			values[item.Key] = item.Value
		}
		if values[settings.KeyGovernanceModel] != "gpt-5-mini" {
			t.Fatalf("upsert failed: got %q", values[settings.KeyGovernanceModel])
		}
	})
}
