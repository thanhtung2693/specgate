package command_test

import (
	"path/filepath"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// Signing in must not unbind other repositories. Workspace-scoped commands now
// refuse to run in an unbound project, so discarding the bindings would leave
// every checkout on the machine broken until each one is bound again.
func TestUserLoginKeepsExistingProjectWorkspaceBindings(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	stateDir := t.TempDir()
	otherProject := filepath.Join(t.TempDir(), "other-repo")

	cfg := config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}
	cfg.SetProjectWorkspace(otherProject, config.CurrentWorkspace{
		ID: "ws-other", Slug: "other", Name: "Other",
	})
	if err := cfg.SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json",
		"user", "login", "--username", "dev", "--display-name", "Dev", "--workspace", "Fresh")
	if code != output.ExitOK {
		t.Fatalf("login exit = %d, output = %s", code, out.String())
	}

	saved, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Projects) == 0 {
		t.Fatal("login discarded every project workspace binding on this machine")
	}
	if _, ok := saved.Projects[filepath.Clean(otherProject)]; !ok {
		t.Fatalf("login discarded the unrelated project binding: %#v", saved.Projects)
	}
	if saved.CurrentUser.Username != "dev" {
		t.Fatalf("login did not record the user: %#v", saved.CurrentUser)
	}
}
