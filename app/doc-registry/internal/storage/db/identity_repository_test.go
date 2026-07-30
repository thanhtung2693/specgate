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

// The access-change trail is only worth writing if something reads it back, and
// the member list is where an operator asks who has access. Exercised against a
// real database because the lateral join and the events table's foreign keys are
// what this is actually testing.
func TestIdentityRepository_MemberListReportsCredentialStateAndItsLastChange(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewIdentityRepository(gdb)
		ctx := context.Background()

		owner, err := repo.Bootstrap(ctx, identity.BootstrapInput{
			WorkspaceName: "Credential trail", DisplayName: "Tung", Username: "tung",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Bootstrap(ctx, identity.BootstrapInput{
			WorkspaceName: "Credential trail", DisplayName: "Mai", Username: "mai",
		}); err != nil {
			t.Fatal(err)
		}

		byName := func() map[string]identity.WorkspaceMemberDetail {
			members, err := repo.ListWorkspaceMembers(ctx, owner.Workspace.ID)
			if err != nil {
				t.Fatal(err)
			}
			out := map[string]identity.WorkspaceMemberDetail{}
			for _, m := range members {
				out[m.Username] = m
			}
			return out
		}

		// Nobody holds a credential yet, and nothing has been recorded.
		before := byName()
		if before["mai"].CredentialSet || before["mai"].CredentialChangedAt != nil {
			t.Fatalf("%s: fresh member = %#v, want no credential and no change record", name, before["mai"])
		}

		if err := repo.SetUserCredential(ctx, "mai", "$2a$10$fakehashfakehashfakehashfakehashfakehashfakehashfakeha"); err != nil {
			t.Fatal(err)
		}
		if err := repo.RecordCredentialEvent(ctx, identity.CredentialEventInput{
			Username:    "mai",
			EventType:   identity.EventCredentialIssued,
			Actor:       "tung",
			WorkspaceID: owner.Workspace.ID,
		}); err != nil {
			t.Fatal(err)
		}

		issued := byName()["mai"]
		if !issued.CredentialSet {
			t.Fatalf("%s: member cannot authenticate after a credential was set", name)
		}
		if issued.CredentialChangedBy != "tung" || issued.CredentialChangedAt == nil {
			t.Fatalf("%s: member = %#v, want the change attributed to tung", name, issued)
		}

		// Revoking appends rather than replacing: the newest record wins the readback
		// and the earlier one is still there.
		if err := repo.SetUserCredential(ctx, "mai", ""); err != nil {
			t.Fatal(err)
		}
		if err := repo.RecordCredentialEvent(ctx, identity.CredentialEventInput{
			Username: "mai", EventType: identity.EventCredentialRevoked, Actor: "mai",
		}); err != nil {
			t.Fatal(err)
		}
		revoked := byName()["mai"]
		if revoked.CredentialSet {
			t.Fatalf("%s: member still holds a credential after revoke", name)
		}
		if revoked.CredentialChangedBy != "mai" {
			t.Fatalf("%s: latest change attributed to %q, want mai", name, revoked.CredentialChangedBy)
		}
		latest, err := repo.LatestCredentialEvent(ctx, "mai")
		if err != nil {
			t.Fatal(err)
		}
		if latest == nil || latest.EventType != identity.EventCredentialRevoked {
			t.Fatalf("%s: latest event = %#v, want the revoke", name, latest)
		}
		// An unknown member has no trail rather than an error.
		missing, err := repo.LatestCredentialEvent(ctx, "ghost")
		if err != nil || missing != nil {
			t.Fatalf("%s: unknown member gave (%#v, %v), want (nil, nil)", name, missing, err)
		}
	})
}
