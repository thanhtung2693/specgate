package command_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestWorkspaceSelectPersistsCurrentWorkspace(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	fc.workspaces = []client.IdentityWorkspace{{ID: "ws-1", Slug: "platform", Name: "Platform"}}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "select", "platform")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastWorkspaceID != "platform" {
		t.Fatalf("workspace lookup = %q, want platform", fc.lastWorkspaceID)
	}
	cfg, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Workspace.ID != "ws-1" || cfg.Workspace.Slug != "platform" {
		t.Fatalf("workspace config = %#v", cfg.Workspace)
	}
}

func TestWorkspaceSelectPromptsWhenSlugMissing(t *testing.T) {
	t.Parallel()
	deps, fc, fp, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	fp.selectedValue = "platform"
	fc.workspaces = []client.IdentityWorkspace{
		{ID: "ws-1", Slug: "platform", Name: "Platform"},
		{ID: "ws-2", Slug: "docs", Name: "Docs"},
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "select")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastWorkspaceID != "" {
		t.Fatalf("workspace lookup = %q, want no direct lookup", fc.lastWorkspaceID)
	}
	if len(fp.selectOptions) != 2 || fp.selectOptions[0].Value != "platform" {
		t.Fatalf("select options = %#v", fp.selectOptions)
	}
	cfg, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Workspace.ID != "ws-1" || cfg.Workspace.Slug != "platform" {
		t.Fatalf("workspace config = %#v", cfg.Workspace)
	}
}

func TestWorkspaceSelectNoInputRequiresSlug(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	fc.workspaces = []client.IdentityWorkspace{{ID: "ws-1", Slug: "platform", Name: "Platform"}}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "workspace", "select")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("client calls = %d, want 0", fc.calls)
	}
	if !strings.Contains(out.String(), "workspace slug is required") {
		t.Fatalf("output missing validation message: %s", out.String())
	}
}

func TestWorkspaceSelectRejectsInternalIDShape(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "select", "5367ce6c-53cd-4891-a56a-229bb25d3f41")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("client calls = %d, want 0", fc.calls)
	}
	if !strings.Contains(out.String(), "workspace slug must use lowercase letters") {
		t.Fatalf("output missing validation message: %s", out.String())
	}
}

func TestUserCurrentReadsConfig(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	err := (config.Config{CurrentUser: config.CurrentUser{
		ID:          "user-1",
		Username:    "thanhtung2693",
		DisplayName: "Thanh Tung",
	}}).SaveTo(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "user", "current")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "thanhtung2693") {
		t.Fatalf("output missing username: %s", out.String())
	}
}

func TestInitInteractiveBootstrapsIdentity(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)

	deps, fc, fp, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.DeployRunner = &fakeDeployRunner{}
	fp.inputValues = []string{"SpecGate", "Thanh Tung", "ThanhTung2693", "thanhtung2693@example.com"}
	fc.identitySelection = &client.IdentitySelection{
		User:      client.IdentityUser{ID: "user-1", Username: "thanhtung2693", DisplayName: "Thanh Tung", Email: "thanhtung2693@example.com"},
		Workspace: client.IdentityWorkspace{ID: "ws-1", Slug: "specgate", Name: "SpecGate"},
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "init", "--dir", dir)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastBootstrapInput.Username != "thanhtung2693" {
		t.Fatalf("username = %q, want normalized thanhtung2693", fc.lastBootstrapInput.Username)
	}
	cfg, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentUser.ID != "user-1" || cfg.Workspace.ID != "ws-1" {
		t.Fatalf("identity config = user %#v workspace %#v", cfg.CurrentUser, cfg.Workspace)
	}
}

func TestInitInteractiveSeedUsesSelectedIdentity(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)

	deps, fc, fp, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	fr := &fakeDeployRunner{}
	deps.DeployRunner = fr
	fp.inputValues = []string{"SpecGate", "Thanh Tung", "ThanhTung2693", "thanhtung2693@example.com"}
	fc.identitySelection = &client.IdentitySelection{
		User:      client.IdentityUser{ID: "user-1", Username: "thanhtung2693", DisplayName: "Thanh Tung", Email: "thanhtung2693@example.com"},
		Workspace: client.IdentityWorkspace{ID: "ws-1", Slug: "specgate", Name: "SpecGate"},
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "init", "--dir", dir, "--seed")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	for _, cmd := range fr.Commands {
		if strings.Contains(cmd, "--seed-demo") {
			if !strings.Contains(cmd, "--seed-demo-workspace-id ws-1") {
				t.Fatalf("seed command missing workspace: %s", cmd)
			}
			if !strings.Contains(cmd, "--seed-demo-created-by thanhtung2693") {
				t.Fatalf("seed command missing actor: %s", cmd)
			}
			return
		}
	}
	t.Fatalf("seed command not found: %#v", fr.Commands)
}
