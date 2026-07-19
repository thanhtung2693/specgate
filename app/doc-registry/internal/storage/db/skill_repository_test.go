package db

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

func TestSkillRepository_CreateListGetUpdateDelete(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewSkillRepository(gdb)
		ctx := context.Background()

		row := &SkillRow{
			ID:          "skill-1",
			WorkspaceID: "ws-a",
			Name:        "schema-design",
			Description: "designs schemas",
			Prompt:      "# Schema Design\nYou are a schema designer.",
		}
		if err := repo.Create(ctx, row); err != nil {
			t.Fatalf("create: %v", err)
		}

		// List returns the created row.
		rows, err := repo.List(ctx, "ws-a")
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].ID != "skill-1" {
			t.Fatalf("list: %+v", rows)
		}

		// Get returns the row by id.
		got, err := repo.Get(ctx, "ws-a", "skill-1")
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "schema-design" || got.Prompt == "" {
			t.Fatalf("get: %+v", got)
		}

		// Update changes mutable fields.
		got.Name = "schema-design-v2"
		got.Description = "updated desc"
		got.Prompt = "# Updated"
		if err := repo.Update(ctx, "ws-a", got); err != nil {
			t.Fatalf("update: %v", err)
		}
		after, err := repo.Get(ctx, "ws-a", "skill-1")
		if err != nil {
			t.Fatal(err)
		}
		if after.Name != "schema-design-v2" || after.Description != "updated desc" {
			t.Fatalf("after update: %+v", after)
		}

		// Delete removes the row.
		if err := repo.Delete(ctx, "ws-a", "skill-1"); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if _, err := repo.Get(ctx, "ws-a", "skill-1"); err != gorm.ErrRecordNotFound {
			t.Fatalf("after delete, expected ErrRecordNotFound, got %v", err)
		}
	})
}

func TestSkillRepository_Delete_NotFound(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewSkillRepository(gdb)
		if err := repo.Delete(context.Background(), "ws-a", "nonexistent"); err != gorm.ErrRecordNotFound {
			t.Fatalf("expected ErrRecordNotFound, got %v", err)
		}
	})
}
