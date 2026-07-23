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

func TestWorkspaceHelpShowsProjectExamples(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	root := command.NewRootCommand(deps)
	root.SetOut(out)
	root.SetErr(out)

	code := command.ExecuteForCode(root, "workspace", "--help")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	for _, want := range []string{"workspace select", "workspace bind", "workspace current", "workspace unbind", "--workspace platform"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("help missing %q:\n%s", want, out.String())
		}
	}
}

func TestWorkspaceBindPersistsProjectWorkspace(t *testing.T) {
	t.Parallel()
	canonicalRepo, nested := commandGitRepo(t)

	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.WorkingDir = nested
	fc.workspaces = []client.IdentityWorkspace{{ID: "ws-1", Slug: "platform", Name: "Platform"}}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "workspace", "bind", "platform")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	cfg, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Workspace.ID != "" {
		t.Fatalf("global workspace = %#v, want empty", cfg.Workspace)
	}
	if cfg.Projects[canonicalRepo].Workspace.ID != "ws-1" || cfg.Projects[canonicalRepo].Workspace.Slug != "platform" {
		t.Fatalf("project workspace = %#v", cfg.Projects[canonicalRepo].Workspace)
	}
	if _, err := os.Stat(filepath.Join(canonicalRepo, ".specgate", ".gitignore")); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Project workspace set to platform", "Project: " + canonicalRepo} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestWorkspaceBindUsesCurrentWorkspaceWhenSlugOmitted(t *testing.T) {
	t.Parallel()
	canonicalRepo, nested := commandGitRepo(t)

	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.WorkingDir = nested
	if err := (config.Config{
		Workspace: config.CurrentWorkspace{ID: "ws-global", Slug: "platform", Name: "Platform"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "workspace", "bind")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("client calls = %d, want 0", fc.calls)
	}
	cfg, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Projects[canonicalRepo].Workspace.ID != "ws-global" || cfg.Projects[canonicalRepo].Workspace.Slug != "platform" {
		t.Fatalf("project workspace = %#v", cfg.Projects[canonicalRepo].Workspace)
	}
}

func TestWorkspaceBindPromptsForWorkspace(t *testing.T) {
	t.Parallel()
	canonicalRepo, nested := commandGitRepo(t)

	deps, fc, fp, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.WorkingDir = nested
	fp.selectedValue = "platform"
	fc.workspaces = []client.IdentityWorkspace{
		{ID: "ws-1", Slug: "platform", Name: "Platform"},
		{ID: "ws-2", Slug: "docs", Name: "Docs"},
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "workspace", "bind")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if len(fp.selectOptions) != 2 || fp.selectOptions[0].Value != "platform" {
		t.Fatalf("select options = %#v", fp.selectOptions)
	}
	cfg, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Projects[canonicalRepo].Workspace.ID != "ws-1" {
		t.Fatalf("project workspace = %#v", cfg.Projects[canonicalRepo].Workspace)
	}
}

func TestWorkspaceSelectCanPersistProjectWorkspace(t *testing.T) {
	t.Parallel()
	canonicalRepo, nested := commandGitRepo(t)

	deps, fc, fp, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.WorkingDir = nested
	fp.selectedValue = "project"
	fc.workspaces = []client.IdentityWorkspace{{ID: "ws-1", Slug: "platform", Name: "Platform"}}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "workspace", "select", "platform")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	cfg, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Workspace.ID != "" {
		t.Fatalf("global workspace = %#v, want empty", cfg.Workspace)
	}
	if cfg.Projects[canonicalRepo].Workspace.ID != "ws-1" || cfg.Projects[canonicalRepo].Workspace.Slug != "platform" {
		t.Fatalf("project workspace = %#v", cfg.Projects[canonicalRepo].Workspace)
	}
	if !strings.Contains(out.String(), "Project workspace set to platform") {
		t.Fatalf("output missing project scope: %s", out.String())
	}
}

func TestWorkspaceSelectGlobalClearsCurrentProjectBinding(t *testing.T) {
	t.Parallel()
	canonicalRepo, nested := commandGitRepo(t)
	otherRepo, _ := commandGitRepo(t)

	deps, fc, fp, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.WorkingDir = nested
	fp.selectedValue = "global"
	fc.workspaces = []client.IdentityWorkspace{{ID: "ws-global", Slug: "global", Name: "Global"}}
	cfg := config.Config{
		Projects: map[string]config.ProjectConfig{
			canonicalRepo: {Workspace: config.CurrentWorkspace{ID: "ws-old", Slug: "old"}},
			otherRepo:     {Workspace: config.CurrentWorkspace{ID: "ws-other", Slug: "other"}},
		},
	}
	if err := cfg.SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "workspace", "select", "global")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	loaded, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Workspace.ID != "ws-global" || loaded.Workspace.Slug != "global" {
		t.Fatalf("global workspace = %#v, want selected workspace", loaded.Workspace)
	}
	if _, ok := loaded.Projects[canonicalRepo]; ok {
		t.Fatalf("current project binding was not cleared: %#v", loaded.Projects)
	}
	if loaded.Projects[otherRepo].Workspace.ID != "ws-other" {
		t.Fatalf("other project binding changed: %#v", loaded.Projects)
	}
}

func TestWorkspaceSelectNoInputGlobalClearsCurrentProjectBinding(t *testing.T) {
	t.Parallel()
	canonicalRepo, nested := commandGitRepo(t)

	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.WorkingDir = nested
	fc.workspaces = []client.IdentityWorkspace{{ID: "ws-global", Slug: "global", Name: "Global"}}
	cfg := config.Config{}
	cfg.SetProjectWorkspace(canonicalRepo, config.CurrentWorkspace{ID: "ws-old", Slug: "old"})
	if err := cfg.SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "workspace", "select", "global")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	loaded, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Workspace.ID != "ws-global" || loaded.Workspace.Slug != "global" {
		t.Fatalf("global workspace = %#v, want selected workspace", loaded.Workspace)
	}
	if loaded.Projects != nil {
		t.Fatalf("project binding was not cleared: %#v", loaded.Projects)
	}
}

func TestWorkspaceCurrentShowsProjectSource(t *testing.T) {
	t.Parallel()
	canonicalRepo, nested := commandGitRepo(t)

	deps, _, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.WorkingDir = nested
	cfg := config.Config{}
	cfg.SetProjectWorkspace(canonicalRepo, config.CurrentWorkspace{ID: "ws-project", Slug: "platform", Name: "Platform"})
	if err := cfg.SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "current")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "workspace: platform (Platform)") {
		t.Fatalf("output missing workspace: %s", out.String())
	}
	if !strings.Contains(out.String(), "source: project") {
		t.Fatalf("output missing project source: %s", out.String())
	}
	if !strings.Contains(out.String(), "project: "+canonicalRepo) {
		t.Fatalf("output missing project root: %s", out.String())
	}
}

func TestWorkspaceCurrentShowsGlobalSourceAndProjectBindingHint(t *testing.T) {
	t.Parallel()
	canonicalRepo, nested := commandGitRepo(t)

	deps, _, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.WorkingDir = nested
	if err := (config.Config{
		Workspace: config.CurrentWorkspace{ID: "ws-global", Slug: "global", Name: "Global"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "current")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	for _, want := range []string{
		"workspace: global (Global)",
		"source: global default",
		"project: " + canonicalRepo + " (not bound)",
		"specgate workspace bind",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestWorkspaceCurrentShowsOverrideSource(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{
		Workspace: config.CurrentWorkspace{ID: "ws-global", Slug: "global", Name: "Global"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--workspace", "platform", "workspace", "current")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "workspace: platform") {
		t.Fatalf("output missing override workspace: %s", out.String())
	}
	if !strings.Contains(out.String(), "source: override") {
		t.Fatalf("output missing override source: %s", out.String())
	}
}

func TestWorkspaceMembersListsEffectiveWorkspaceAndMarksCurrentUser(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{
		CurrentUser: config.CurrentUser{ID: "user-2", Username: "grace", DisplayName: "Grace Hopper"},
		Workspace:   config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	fc.workspaceMembers = &client.WorkspaceMembersResult{
		Workspace:   client.IdentityWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"},
		CurrentUser: client.IdentityUser{ID: "user-2", Username: "grace"},
		Members: []client.WorkspaceMember{
			{UserID: "user-1", Username: "ada", DisplayName: "Ada Lovelace", Role: "owner"},
			{UserID: "user-2", Username: "grace", DisplayName: "Grace Hopper", Role: "owner", Current: true},
		},
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "members")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastWorkspaceMembersID != "ws-1" || fc.lastWorkspaceMembersUserID != "user-2" {
		t.Fatalf("members lookup workspace=%q user=%q", fc.lastWorkspaceMembersID, fc.lastWorkspaceMembersUserID)
	}
	for _, want := range []string{"workspace: platform", "ada", "grace", "(you)"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestDoctorShowsWorkspaceMembershipState(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{
		CurrentUser: config.CurrentUser{ID: "user-2", Username: "grace", DisplayName: "Grace Hopper"},
		Workspace:   config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	fc.workspaceMembers = &client.WorkspaceMembersResult{
		Workspace:   client.IdentityWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"},
		CurrentUser: client.IdentityUser{ID: "user-2", Username: "grace"},
		Members: []client.WorkspaceMember{
			{UserID: "user-1", Username: "ada", DisplayName: "Ada Lovelace", Role: "owner"},
		},
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "doctor")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "NOT_MEMBER") || !strings.Contains(out.String(), "workspace members") {
		t.Fatalf("doctor output missing membership state:\n%s", out.String())
	}
}

func TestDoctorMarksStaleConfiguredWorkspaceMissing(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{
		CurrentUser: config.CurrentUser{ID: "user-2", Username: "grace", DisplayName: "Grace Hopper"},
		Workspace:   config.CurrentWorkspace{ID: "stale-workspace", Slug: "platform", Name: "Platform"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	fc.getWorkspaceErr = &client.APIError{Kind: client.ErrorNotFound, Status: 404, Message: "workspace not found"}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "doctor")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "Workspace:   MISSING") || !strings.Contains(out.String(), "workspace select") {
		t.Fatalf("doctor output did not identify stale workspace:\n%s", out.String())
	}
}

func TestDoctorReportsMissingWorkspaceOverride(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.getWorkspaceErr = &client.APIError{Kind: client.ErrorNotFound, Status: 404, Message: "workspace not found"}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--workspace", "missing-workspace", "doctor")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "Workspace:   MISSING") || !strings.Contains(out.String(), "workspace select") {
		t.Fatalf("doctor output did not identify missing workspace override:\n%s", out.String())
	}
	if fc.lastStatusWorkspaceID != "" {
		t.Fatalf("board probe workspace = %q, want none for missing selection", fc.lastStatusWorkspaceID)
	}
}

func TestWorkspaceUnbindRemovesCurrentProjectBinding(t *testing.T) {
	t.Parallel()
	canonicalRepo, nested := commandGitRepo(t)

	deps, _, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.WorkingDir = nested
	cfg := config.Config{
		Workspace: config.CurrentWorkspace{ID: "ws-global", Slug: "global"},
	}
	cfg.SetProjectWorkspace(canonicalRepo, config.CurrentWorkspace{ID: "ws-project", Slug: "platform"})
	if err := cfg.SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "unbind")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	loaded, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Projects) != 0 {
		t.Fatalf("projects still set: %#v", loaded.Projects)
	}
	if loaded.Workspace.Slug != "global" {
		t.Fatalf("global workspace = %#v, want preserved", loaded.Workspace)
	}
	if !strings.Contains(out.String(), "Project workspace binding removed") {
		t.Fatalf("output missing unbind summary: %s", out.String())
	}
}

func TestWorkspaceSelectPromptsWhenSlugMissing(t *testing.T) {
	t.Parallel()
	deps, fc, fp, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.WorkingDir = t.TempDir()
	fp.selectedValue = "platform"
	fc.workspaces = []client.IdentityWorkspace{
		{ID: "ws-1", Slug: "platform", Name: "Platform"},
		{ID: "ws-2", Slug: "docs", Name: "Docs"},
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "workspace", "select")
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

func TestUserLoginBootstrapsIdentityFromFlags(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	fc.identitySelection = &client.IdentitySelection{
		User:      client.IdentityUser{ID: "user-1", Username: "thanhtung2693", DisplayName: "Thanh Tung", Email: "thanhtung2693@example.com"},
		Workspace: client.IdentityWorkspace{ID: "ws-1", Slug: "fresh-alpha-workspace", Name: "Fresh Alpha Workspace"},
	}

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--plain",
		"user",
		"login",
		"--workspace",
		"Fresh Alpha Workspace",
		"--display-name",
		"Thanh Tung",
		"--username",
		"ThanhTung2693",
		"--email",
		"thanhtung2693@example.com",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastBootstrapInput.WorkspaceName != "Fresh Alpha Workspace" ||
		fc.lastBootstrapInput.DisplayName != "Thanh Tung" ||
		fc.lastBootstrapInput.Username != "thanhtung2693" ||
		fc.lastBootstrapInput.Email != "thanhtung2693@example.com" {
		t.Fatalf("bootstrap input = %#v", fc.lastBootstrapInput)
	}
	cfg, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentUser.Username != "thanhtung2693" || cfg.Workspace.Slug != "fresh-alpha-workspace" {
		t.Fatalf("identity config = user %#v workspace %#v", cfg.CurrentUser, cfg.Workspace)
	}
	if !strings.Contains(out.String(), "Logged in as thanhtung2693") ||
		!strings.Contains(out.String(), "Workspace set to fresh-alpha-workspace") {
		t.Fatalf("output missing login summary: %s", out.String())
	}
}

func TestUserLoginClearsStaleProjectWorkspaceBindings(t *testing.T) {
	t.Parallel()
	canonicalRepo, _ := commandGitRepo(t)

	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{}
	cfg.SetProjectWorkspace(canonicalRepo, config.CurrentWorkspace{ID: "old-ws", Slug: "old"})
	if err := cfg.SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	fc.identitySelection = &client.IdentitySelection{
		User:      client.IdentityUser{ID: "user-1", Username: "thanhtung2693", DisplayName: "Thanh Tung"},
		Workspace: client.IdentityWorkspace{ID: "ws-1", Slug: "fresh-alpha-workspace", Name: "Fresh Alpha Workspace"},
	}

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--plain",
		"user",
		"login",
		"--workspace",
		"Fresh Alpha Workspace",
		"--display-name",
		"Thanh Tung",
		"--username",
		"thanhtung2693",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	loaded, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Projects != nil {
		t.Fatalf("project bindings = %#v, want nil after login", loaded.Projects)
	}
	if loaded.Workspace.Slug != "fresh-alpha-workspace" {
		t.Fatalf("workspace = %#v, want fresh login workspace", loaded.Workspace)
	}
}

func TestUserLoginUnknownFlagReportsPlainUsageError(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--plain",
		"user",
		"login",
		"--name",
		"New User",
	)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "Error (usage): unknown flag: --name") {
		t.Fatalf("output = %q, want readable usage error", out.String())
	}
	if strings.Contains(out.String(), "schema_version") {
		t.Fatalf("plain output leaked JSON envelope: %q", out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("bootstrap calls = %d, want 0", fc.calls)
	}
}

func TestUserLoginUnknownFlagHonorsTrailingJSONFlag(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"user",
		"login",
		"--name",
		"New User",
		"--json",
	)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), `"command":"user.login"`) ||
		!strings.Contains(out.String(), `"code":"usage"`) ||
		strings.Contains(out.String(), "Error (usage):") {
		t.Fatalf("output = %q, want JSON usage envelope", out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("bootstrap calls = %d, want 0", fc.calls)
	}
}

func TestUserLoginJSONRequiresRequiredFields(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "user", "login", "--username", "thanhtung2693")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("client calls = %d, want 0", fc.calls)
	}
	if !strings.Contains(out.String(), "workspace and display-name are required") {
		t.Fatalf("output missing validation message: %s", out.String())
	}
}

func TestUserLoginPlainDoesNotPromptForRequiredFields(t *testing.T) {
	t.Parallel()
	deps, fc, fp, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "user", "login")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("client calls = %d, want 0", fc.calls)
	}
	if fp.inputCalls != 0 {
		t.Fatalf("plain login prompted %d time(s)", fp.inputCalls)
	}
	if !strings.Contains(out.String(), "workspace, display-name, and username are required") {
		t.Fatalf("output missing validation message: %s", out.String())
	}
}

func TestUserLogoutClearsIdentitySelection(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	err := (config.Config{
		Server: "http://localhost:28080",
		CurrentUser: config.CurrentUser{
			ID:          "user-1",
			Username:    "thanhtung2693",
			DisplayName: "Thanh Tung",
		},
		Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "fresh-alpha-workspace", Name: "Fresh Alpha Workspace"},
	}).SaveTo(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.SetProjectWorkspace(t.TempDir(), config.CurrentWorkspace{ID: "old-ws", Slug: "old"})
	if err := cfg.SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "user", "logout")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	cfg, err = config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "http://localhost:28080" {
		t.Fatalf("server = %q, want preserved", cfg.Server)
	}
	if cfg.CurrentUser.Username != "" || cfg.Workspace.Slug != "" {
		t.Fatalf("identity not cleared: user %#v workspace %#v", cfg.CurrentUser, cfg.Workspace)
	}
	if cfg.Projects != nil {
		t.Fatalf("project bindings = %#v, want nil after logout", cfg.Projects)
	}
	if !strings.Contains(out.String(), `"ok":true`) {
		t.Fatalf("output missing success envelope: %s", out.String())
	}
}
