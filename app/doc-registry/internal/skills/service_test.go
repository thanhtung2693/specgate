package skills

import (
	"context"
	"errors"
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
	ctx := WithWorkspace(context.Background(), "ws-a")

	_, err := svc.Create(ctx, CreateInput{Name: "", Prompt: "x"})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	_, err = svc.Create(ctx, CreateInput{Name: "n", Prompt: "  "})
	if err == nil {
		t.Fatal("expected error for empty prompt")
	}

	created, err := svc.Create(ctx, CreateInput{
		WorkspaceID: "ws-a",
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
	ctx := WithWorkspace(context.Background(), "ws-a")

	created, err := svc.Create(ctx, CreateInput{
		WorkspaceID: "ws-a",
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

func TestService_WorkspaceScopesSkills(t *testing.T) {
	t.Parallel()
	db := newSkillsTestGormDB(t)
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	svc := NewService(storagedb.NewSkillRepository(db))

	created, err := svc.Create(WithWorkspace(context.Background(), "ws-a"), CreateInput{
		WorkspaceID: "ws-a", Name: "shared", Prompt: "a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(WithWorkspace(context.Background(), "ws-b"), CreateInput{
		WorkspaceID: "ws-b", Name: "shared", Prompt: "b",
	}); err != nil {
		t.Fatal("same skill name should work in another workspace:", err)
	}
	if _, err := svc.Get(WithWorkspace(context.Background(), "ws-b"), created.ID); !IsNotFound(err) {
		t.Fatalf("cross-workspace get = %v, want not found", err)
	}
	items, err := svc.List(WithWorkspace(context.Background(), "ws-b"))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Prompt != "b" {
		t.Fatalf("workspace B skills = %+v", items)
	}
}

func TestService_RequiresWorkspace(t *testing.T) {
	t.Parallel()
	db := newSkillsTestGormDB(t)
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	svc := NewService(storagedb.NewSkillRepository(db))
	if _, err := svc.List(context.Background()); !errors.Is(err, ErrWorkspaceRequired) {
		t.Fatalf("List without workspace = %v", err)
	}
}
