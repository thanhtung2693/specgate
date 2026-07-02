package skills

import (
	"context"
	"testing"

	storagedb "github.com/specgate/doc-registry/internal/storage/db"
)

func TestService_Create_validation(t *testing.T) {
	t.Parallel()
	db := newSkillsTestGormDB(t)
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	svc := NewService(storagedb.NewSkillRepository(db))
	ctx := context.Background()

	_, err := svc.Create(ctx, CreateInput{Name: "", Prompt: "x"})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	_, err = svc.Create(ctx, CreateInput{Name: "n", Prompt: "  "})
	if err == nil {
		t.Fatal("expected error for empty prompt")
	}

	created, err := svc.Create(ctx, CreateInput{
		Name:        "g",
		Description: "d",
		Prompt:      "body",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "g" || got.Prompt != "body" {
		t.Fatalf("Get = %#v", got)
	}
	_, err = svc.Get(ctx, "00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Fatal("expected not found")
	}
	if !IsNotFound(err) {
		t.Fatalf("expected IsNotFound, got %v", err)
	}
}

func TestService_Create_normalizesTextFields(t *testing.T) {
	t.Parallel()
	db := newSkillsTestGormDB(t)
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	svc := NewService(storagedb.NewSkillRepository(db))
	ctx := context.Background()

	created, err := svc.Create(ctx, CreateInput{
		Name:        "  spaced name  ",
		Description: "  spaced description  ",
		Prompt:      "  keep prompt spacing  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "spaced name" {
		t.Fatalf("Name = %q", created.Name)
	}
	if created.Description != "spaced description" {
		t.Fatalf("Description = %q", created.Description)
	}
	if created.Prompt != "  keep prompt spacing  " {
		t.Fatalf("Prompt = %q", created.Prompt)
	}

	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "spaced name" || got.Description != "spaced description" {
		t.Fatalf("Get = %#v", got)
	}
}
