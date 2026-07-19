package db

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/identity"
)

func TestIdentityRepository_BootstrapCreatesSelection(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewIdentityRepository(gdb)
		ctx := context.Background()

		selection, err := repo.Bootstrap(ctx, identity.BootstrapInput{
			WorkspaceName: "SpecGate Platform",
			DisplayName:   "Thanh Tung",
			Username:      "ThanhTung2693",
			Email:         "thanhtung2693@example.com",
		})
		if err != nil {
			t.Fatal(err)
		}
		if selection.User.Username != "thanhtung2693" {
			t.Fatalf("username = %q, want normalized thanhtung2693", selection.User.Username)
		}
		if selection.Workspace.Slug != "specgate-platform" {
			t.Fatalf("workspace slug = %q, want specgate-platform", selection.Workspace.Slug)
		}

		again, err := repo.Bootstrap(ctx, identity.BootstrapInput{
			WorkspaceName: "SpecGate Platform",
			DisplayName:   "Thanh Tung",
			Username:      "thanhtung2693",
		})
		if err != nil {
			t.Fatal(err)
		}
		if again.User.ID != selection.User.ID {
			t.Fatalf("idempotent user id = %q, want %q", again.User.ID, selection.User.ID)
		}
		if again.Workspace.ID != selection.Workspace.ID {
			t.Fatalf("idempotent workspace id = %q, want %q", again.Workspace.ID, selection.Workspace.ID)
		}

		users, err := repo.ListUsers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(users) != 1 {
			t.Fatalf("users = %d, want 1", len(users))
		}
		workspaces, err := repo.ListWorkspaces(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(workspaces) != 1 {
			t.Fatalf("workspaces = %d, want 1", len(workspaces))
		}

		userByName, err := repo.GetUser(ctx, "thanhtung2693")
		if err != nil {
			t.Fatal(err)
		}
		if userByName == nil || userByName.ID != selection.User.ID {
			t.Fatalf("user by username = %#v, want id %s", userByName, selection.User.ID)
		}
		workspaceBySlug, err := repo.GetWorkspace(ctx, "specgate-platform")
		if err != nil {
			t.Fatal(err)
		}
		if workspaceBySlug == nil || workspaceBySlug.ID != selection.Workspace.ID {
			t.Fatalf("workspace by slug = %#v, want id %s", workspaceBySlug, selection.Workspace.ID)
		}
	})
}

func TestIdentityRepository_ListWorkspaceMembersJoinsUsers(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewIdentityRepository(gdb)
		ctx := context.Background()

		first, err := repo.Bootstrap(ctx, identity.BootstrapInput{
			WorkspaceName: "SpecGate Platform",
			DisplayName:   "Ada Lovelace",
			Username:      "ada",
		})
		if err != nil {
			t.Fatal(err)
		}
		second, err := repo.Bootstrap(ctx, identity.BootstrapInput{
			WorkspaceName: "SpecGate Platform",
			DisplayName:   "Grace Hopper",
			Username:      "grace",
			Email:         "grace@example.com",
		})
		if err != nil {
			t.Fatal(err)
		}

		members, err := repo.ListWorkspaceMembers(ctx, first.Workspace.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(members) != 2 {
			t.Fatalf("members = %d, want 2", len(members))
		}
		if members[0].Username != "ada" || members[1].Username != "grace" {
			t.Fatalf("members order = %#v, want username order", members)
		}
		if members[1].UserID != second.User.ID || members[1].Email != "grace@example.com" {
			t.Fatalf("joined user fields = %#v", members[1])
		}
	})
}
