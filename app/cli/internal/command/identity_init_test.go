package command_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

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

	code := command.ExecuteForCode(command.NewRootCommand(deps), "init", "--mode", "full", "--dir", dir)
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

	code := command.ExecuteForCode(command.NewRootCommand(deps), "init", "--mode", "full", "--dir", dir, "--seed")
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

func TestInitSeedNoInputRequiresIdentityFlagsBeforeDeployment(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)

	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	fr := &fakeDeployRunner{}
	deps.DeployRunner = fr

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "init", "--mode", "full", "--dir", dir, "--seed")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastBootstrapInput != (client.IdentityBootstrapInput{}) {
		t.Fatalf("bootstrap input = %#v, want none", fc.lastBootstrapInput)
	}
	if len(fr.Commands) != 0 {
		t.Fatalf("deployment ran before validation: %#v", fr.Commands)
	}
	if !strings.Contains(out.String(), "workspace-name, display-name, and username are required when using --seed with --no-input") {
		t.Fatalf("output missing actionable error: %s", out.String())
	}
}

func TestInitNoInputRejectsPartialIdentityFlagsBeforeDeployment(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)

	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	fr := &fakeDeployRunner{}
	deps.DeployRunner = fr

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "--no-input", "init", "--mode", "full", "--dir", dir,
		"--workspace-name", "SpecGate",
		"--display-name", "Thanh Tung",
	)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastBootstrapInput != (client.IdentityBootstrapInput{}) {
		t.Fatalf("bootstrap input = %#v, want none", fc.lastBootstrapInput)
	}
	if len(fr.Commands) != 0 {
		t.Fatalf("deployment ran before validation: %#v", fr.Commands)
	}
	if !strings.Contains(out.String(), "workspace-name, display-name, and username must be provided together when setting init identity flags") {
		t.Fatalf("output missing partial-flag validation: %s", out.String())
	}
}

func TestInitSeedNoInputBootstrapsIdentityFromFlags(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)

	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	fr := &fakeDeployRunner{}
	deps.DeployRunner = fr
	fc.identitySelection = &client.IdentitySelection{
		User:      client.IdentityUser{ID: "user-1", Username: "thanhtung2693", DisplayName: "Thanh Tung", Email: "thanhtung2693@example.com"},
		Workspace: client.IdentityWorkspace{ID: "ws-1", Slug: "specgate", Name: "SpecGate"},
	}

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "--no-input", "init", "--mode", "full", "--dir", dir, "--seed",
		"--workspace-name", "SpecGate",
		"--display-name", "Thanh Tung",
		"--username", "ThanhTung2693",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastBootstrapInput.WorkspaceName != "SpecGate" ||
		fc.lastBootstrapInput.DisplayName != "Thanh Tung" ||
		fc.lastBootstrapInput.Username != "thanhtung2693" ||
		fc.lastBootstrapInput.Email != "" {
		t.Fatalf("bootstrap input = %#v", fc.lastBootstrapInput)
	}
	cfg, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentUser.ID != "user-1" || cfg.Workspace.ID != "ws-1" {
		t.Fatalf("identity config = user %#v workspace %#v", cfg.CurrentUser, cfg.Workspace)
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

func TestInitSeedNoInputForwardsOptionalEmail(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)

	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.DeployRunner = &fakeDeployRunner{}
	fc.identitySelection = &client.IdentitySelection{
		User:      client.IdentityUser{ID: "user-1", Username: "thanhtung2693", DisplayName: "Thanh Tung", Email: "thanhtung2693@example.com"},
		Workspace: client.IdentityWorkspace{ID: "ws-1", Slug: "specgate", Name: "SpecGate"},
	}

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "--no-input", "init", "--mode", "full", "--dir", dir, "--seed",
		"--workspace-name", "SpecGate",
		"--display-name", "Thanh Tung",
		"--username", "ThanhTung2693",
		"--email", "thanhtung2693@example.com",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastBootstrapInput.Email != "thanhtung2693@example.com" {
		t.Fatalf("email = %q, want forwarded optional email", fc.lastBootstrapInput.Email)
	}
}

func commandGitRepo(t *testing.T) (string, string) {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	nested := filepath.Join(repo, "app")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	canonicalRepo, ok := config.FindProjectRoot(nested)
	if !ok {
		t.Fatal("project root not found")
	}
	return canonicalRepo, nested
}
