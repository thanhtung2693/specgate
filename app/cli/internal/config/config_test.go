package config_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/config"
)

func TestServerPrecedence(t *testing.T) {
	t.Setenv("SPECGATE_SERVER", "https://env.example")
	got := config.ResolveServer("https://flag.example", config.RepoConfig{Server: "https://repo.example"}, config.Config{Server: "https://file.example"})
	if got != "https://flag.example" {
		t.Fatalf("server = %q, want flag value", got)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	t.Setenv("SPECGATE_SERVER", "https://env.example")
	got := config.ResolveServer("", config.RepoConfig{Server: "https://repo.example"}, config.Config{Server: "https://file.example"})
	if got != "https://env.example" {
		t.Fatalf("server = %q, want env value", got)
	}
}

// TestRepoConfigOverridesGlobalFile: with no flag and no env, the repo
// .specgate/config server outranks the global user config server.
func TestRepoConfigOverridesGlobalFile(t *testing.T) {
	t.Setenv("SPECGATE_SERVER", "")
	got := config.ResolveServer("", config.RepoConfig{Server: "https://repo.example"}, config.Config{Server: "https://file.example"})
	if got != "https://repo.example" {
		t.Fatalf("server = %q, want repo value", got)
	}
}

func TestResolvePluginRegistryIgnoresRepoServer(t *testing.T) {
	t.Setenv("SPECGATE_SERVER", "")
	got := config.ResolvePluginRegistry("", config.Config{Server: "https://global.example"})
	if got != "https://global.example" {
		t.Fatalf("plugin registry = %q, want global server", got)
	}
}

func TestFileOverridesDefault(t *testing.T) {
	t.Setenv("SPECGATE_SERVER", "")
	got := config.ResolveServer("", config.RepoConfig{}, config.Config{Server: "https://file.example"})
	if got != "https://file.example" {
		t.Fatalf("server = %q, want file value", got)
	}
}

func TestDefaultFallback(t *testing.T) {
	t.Setenv("SPECGATE_SERVER", "")
	got := config.ResolveServer("", config.RepoConfig{}, config.Config{})
	if got != "http://localhost:3000/api/doc-registry" {
		t.Fatalf("server = %q, want http://localhost:3000/api/doc-registry", got)
	}
}

func TestResolveModeDefaultsUnsetConfigToFullAndPreservesExplicitLocal(t *testing.T) {
	if got := config.ResolveMode(config.Config{}); got != config.ModeFull {
		t.Fatalf("unset mode = %q, want %q", got, config.ModeFull)
	}
	if got := config.ResolveMode(config.Config{Server: "https://server.example"}); got != config.ModeFull {
		t.Fatalf("server-only config mode = %q, want %q", got, config.ModeFull)
	}
	if got := config.ResolveMode(config.Config{Mode: config.ModeLocal}); got != config.ModeLocal {
		t.Fatalf("explicit mode = %q, want %q", got, config.ModeLocal)
	}
}

func TestLoadFromRejectsUnknownMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"mode":"locla"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := config.LoadFrom(path); err == nil || !strings.Contains(err.Error(), "local or full") {
		t.Fatalf("LoadFrom error = %v, want actionable invalid-mode error", err)
	}
}

func TestResolveLocalDirPrecedence(t *testing.T) {
	project := config.ProjectConfig{Local: config.LocalStore{Path: "/project/local", ID: "project-store"}}
	cfg := config.Config{Local: config.LocalStore{Path: "/global/local", ID: "global-store"}}
	got := config.ResolveLocalDir("/flag/local", "/env/local", project, cfg, "/default/local")
	if got != "/flag/local" {
		t.Fatalf("flag path = %q", got)
	}
	got = config.ResolveLocalDir("", "/env/local", project, cfg, "/default/local")
	if got != "/env/local" {
		t.Fatalf("env path = %q", got)
	}
	got = config.ResolveLocalDir("", "", project, cfg, "/default/local")
	if got != "/project/local" {
		t.Fatalf("project path = %q", got)
	}
	got = config.ResolveLocalDir("", "", config.ProjectConfig{}, cfg, "/default/local")
	if got != "/global/local" {
		t.Fatalf("global path = %q", got)
	}
}

func TestDefaultPathHonorsSpecgateConfigPath(t *testing.T) {
	want := filepath.Join(t.TempDir(), "isolated.json")
	t.Setenv("SPECGATE_CONFIG_PATH", want)
	got, err := config.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := config.Config{Server: "https://saved.example", DeploymentDir: "/tmp/deploy"}
	if err := original.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	got, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Server != original.Server {
		t.Fatalf("Server = %q, want %q", got.Server, original.Server)
	}
	if got.DeploymentDir != original.DeploymentDir {
		t.Fatalf("DeploymentDir = %q, want %q", got.DeploymentDir, original.DeploymentDir)
	}
}

func TestSaveIsAtomicAndRestrictedMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := config.Config{Server: "https://atomic.example"}
	if err := c.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestSaveDoesNotFollowPredictableTempSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	external := filepath.Join(dir, "external")
	if err := os.WriteFile(external, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, path+".tmp"); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if err := (config.Config{Server: "https://safe.example"}).SaveTo(path); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(external)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "keep" {
		t.Fatalf("predictable temp symlink target changed: %q", body)
	}
}

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.json")
	got, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}
	if got.Server != "" {
		t.Fatalf("Server = %q, want empty", got.Server)
	}
}

func TestFindProjectRootUsesNearestGitRoot(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "repo")
	nested := filepath.Join(root, "app", "cli")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}

	got, ok := config.FindProjectRoot(nested)
	if !ok {
		t.Fatal("project root not found")
	}
	want, ok := config.FindProjectRoot(root)
	if !ok {
		t.Fatal("root not found from root path")
	}
	if got != want {
		t.Fatalf("root = %q, want %q", got, want)
	}
}

func TestResolveWorkspaceSourceOverrideWins(t *testing.T) {
	canonicalRoot, nested := tempGitRepo(t)
	cfg := config.Config{
		Workspace: config.CurrentWorkspace{ID: "global-ws", Slug: "global"},
		Projects: map[string]config.ProjectConfig{
			canonicalRoot: {Workspace: config.CurrentWorkspace{ID: "project-ws", Slug: "project"}},
		},
	}

	got := config.ResolveWorkspaceSource(cfg, config.RepoConfig{}, nested, "  override-slug  ")
	if got.Source != config.WorkspaceSourceOverride {
		t.Fatalf("source = %q, want override", got.Source)
	}
	if got.Workspace.Slug != "override-slug" {
		t.Fatalf("workspace = %#v, want override slug", got.Workspace)
	}
	if got.ProjectRoot != "" {
		t.Fatalf("project root = %q, want empty", got.ProjectRoot)
	}
}

func TestResolveWorkspaceSourceProjectWinsOverGlobal(t *testing.T) {
	canonicalRoot, nested := tempGitRepo(t)
	cfg := config.Config{
		Workspace: config.CurrentWorkspace{ID: "global-ws", Slug: "global"},
		Projects: map[string]config.ProjectConfig{
			canonicalRoot: {Workspace: config.CurrentWorkspace{ID: "project-ws", Slug: "project"}},
		},
	}

	got := config.ResolveWorkspaceSource(cfg, config.RepoConfig{}, nested, "")
	if got.Source != config.WorkspaceSourceProject {
		t.Fatalf("source = %q, want project", got.Source)
	}
	if got.Workspace.ID != "project-ws" || got.Workspace.Slug != "project" {
		t.Fatalf("workspace = %#v, want project workspace", got.Workspace)
	}
	if got.ProjectRoot != canonicalRoot {
		t.Fatalf("project root = %q, want %q", got.ProjectRoot, canonicalRoot)
	}
}

func TestResolveWorkspaceSourceProjectSlugWinsOverGlobal(t *testing.T) {
	canonicalRoot, nested := tempGitRepo(t)
	cfg := config.Config{
		Workspace: config.CurrentWorkspace{ID: "global-ws", Slug: "global"},
		Projects: map[string]config.ProjectConfig{
			canonicalRoot: {Workspace: config.CurrentWorkspace{Slug: "platform"}},
		},
	}

	got := config.ResolveWorkspaceSource(cfg, config.RepoConfig{}, nested, "")
	if got.Source != config.WorkspaceSourceProject {
		t.Fatalf("source = %q, want project", got.Source)
	}
	if got.Workspace.Slug != "platform" {
		t.Fatalf("workspace = %#v, want project slug workspace", got.Workspace)
	}
	if got.ProjectRoot != canonicalRoot {
		t.Fatalf("project root = %q, want %q", got.ProjectRoot, canonicalRoot)
	}
}

func TestResolveWorkspaceSourceUsesGlobalWhenNoProjectBinding(t *testing.T) {
	cfg := config.Config{
		Workspace: config.CurrentWorkspace{ID: "global-ws", Slug: "global"},
	}

	got := config.ResolveWorkspaceSource(cfg, config.RepoConfig{}, t.TempDir(), "")
	if got.Source != config.WorkspaceSourceGlobal {
		t.Fatalf("source = %q, want global", got.Source)
	}
	if got.Workspace.ID != "global-ws" || got.Workspace.Slug != "global" {
		t.Fatalf("workspace = %#v, want global workspace", got.Workspace)
	}
}

func TestResolveWorkspaceSourceNoneWhenUnconfigured(t *testing.T) {
	got := config.ResolveWorkspaceSource(config.Config{}, config.RepoConfig{}, t.TempDir(), "")
	if got.Source != config.WorkspaceSourceNone {
		t.Fatalf("source = %q, want none", got.Source)
	}
	if got.Workspace != (config.CurrentWorkspace{}) {
		t.Fatalf("workspace = %#v, want empty", got.Workspace)
	}
}

func TestRemoveProjectWorkspaceDeletesBindingAndNilsEmptyMap(t *testing.T) {
	canonicalRoot, _ := tempGitRepo(t)
	cfg := config.Config{
		Projects: map[string]config.ProjectConfig{
			canonicalRoot: {Workspace: config.CurrentWorkspace{ID: "project-ws", Slug: "project"}},
		},
	}

	if !cfg.RemoveProjectWorkspace(canonicalRoot) {
		t.Fatal("RemoveProjectWorkspace returned false, want true")
	}
	if cfg.Projects != nil {
		t.Fatalf("Projects = %#v, want nil", cfg.Projects)
	}
	if cfg.RemoveProjectWorkspace(canonicalRoot) {
		t.Fatal("RemoveProjectWorkspace returned true for missing binding")
	}
}

func TestProjectWorkspaceAPICanonicalizesNestedPathToGitRoot(t *testing.T) {
	canonicalRoot, nested := tempGitRepo(t)
	cfg := config.Config{}

	cfg.SetProjectWorkspace(nested, config.CurrentWorkspace{ID: "project-ws", Slug: "project"})
	if _, ok := cfg.Projects[canonicalRoot]; !ok {
		t.Fatalf("Projects missing canonical root key %q: %#v", canonicalRoot, cfg.Projects)
	}
	if _, ok := cfg.Projects[nested]; ok {
		t.Fatalf("Projects unexpectedly kept nested key %q: %#v", nested, cfg.Projects)
	}
	if !cfg.RemoveProjectWorkspace(nested) {
		t.Fatal("RemoveProjectWorkspace returned false for nested path")
	}
	if cfg.Projects != nil {
		t.Fatalf("Projects = %#v, want nil after removing canonical binding", cfg.Projects)
	}
}

func TestProjectWorkspaceAPICanonicalizesSymlinkPath(t *testing.T) {
	canonicalRoot, _ := tempGitRepo(t)
	symlinkRoot := filepath.Join(t.TempDir(), "repo-link")
	if err := os.Symlink(canonicalRoot, symlinkRoot); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	cfg := config.Config{}
	cfg.SetProjectWorkspace(symlinkRoot, config.CurrentWorkspace{ID: "project-ws", Slug: "project"})
	if _, ok := cfg.Projects[canonicalRoot]; !ok {
		t.Fatalf("Projects missing canonical root key %q: %#v", canonicalRoot, cfg.Projects)
	}
	if _, ok := cfg.Projects[symlinkRoot]; ok {
		t.Fatalf("Projects unexpectedly kept symlink key %q: %#v", symlinkRoot, cfg.Projects)
	}

	resolved := config.ResolveWorkspaceSource(cfg, config.RepoConfig{}, filepath.Join(symlinkRoot, "app", "cli"), "")
	if resolved.Source != config.WorkspaceSourceProject {
		t.Fatalf("source = %q, want project", resolved.Source)
	}
	if resolved.ProjectRoot != canonicalRoot {
		t.Fatalf("project root = %q, want %q", resolved.ProjectRoot, canonicalRoot)
	}
	if resolved.Workspace.ID != "project-ws" || resolved.Workspace.Slug != "project" {
		t.Fatalf("workspace = %#v, want project workspace", resolved.Workspace)
	}

	if !cfg.RemoveProjectWorkspace(symlinkRoot) {
		t.Fatal("RemoveProjectWorkspace returned false for symlink path")
	}
	if cfg.Projects != nil {
		t.Fatalf("Projects = %#v, want nil after removing canonical binding", cfg.Projects)
	}
}

// TestResolveWorkspaceSourceRepoWinsOverGlobal: with no override and no per-user
// project binding, the repo .specgate/config default workspace is used and
// outranks the global user workspace.
func TestResolveWorkspaceSourceRepoWinsOverGlobal(t *testing.T) {
	_, nested := tempGitRepo(t)
	cfg := config.Config{
		Workspace: config.CurrentWorkspace{ID: "global-ws", Slug: "global"},
	}
	repo := config.RepoConfig{Workspace: "team-default"}

	got := config.ResolveWorkspaceSource(cfg, repo, nested, "")
	if got.Source != config.WorkspaceSourceRepo {
		t.Fatalf("source = %q, want repo", got.Source)
	}
	if got.Workspace.Slug != "team-default" {
		t.Fatalf("workspace = %#v, want repo default slug", got.Workspace)
	}
}

// TestResolveWorkspaceSourceProjectBindingOutranksRepo: the per-user project
// binding always wins over the repo .specgate/config default workspace — the
// repo default is honored only when no binding exists.
func TestResolveWorkspaceSourceProjectBindingOutranksRepo(t *testing.T) {
	canonicalRoot, nested := tempGitRepo(t)
	cfg := config.Config{
		Projects: map[string]config.ProjectConfig{
			canonicalRoot: {Workspace: config.CurrentWorkspace{ID: "user-ws", Slug: "user-bound"}},
		},
	}
	repo := config.RepoConfig{Workspace: "team-default"}

	got := config.ResolveWorkspaceSource(cfg, repo, nested, "")
	if got.Source != config.WorkspaceSourceProject {
		t.Fatalf("source = %q, want project (binding outranks repo)", got.Source)
	}
	if got.Workspace.Slug != "user-bound" {
		t.Fatalf("workspace = %#v, want user-bound", got.Workspace)
	}
}

// TestLoadRepoConfigReadsAllowedFieldsOnly: LoadRepoConfig reuses FindProjectRoot
// to locate .specgate/config and parses only server and workspace.
func TestLoadRepoConfigReadsAllowedFieldsOnly(t *testing.T) {
	canonicalRoot, nested := tempGitRepo(t)
	body := `{
		"server": "https://repo.example",
		"workspace": "team-default",
		"governance_profile": "strict-v1",
		"api_key": "ignored-test-key",
		"token": "jwt-should-be-ignored",
		"current_user": {"username": "someone-else"}
	}`
	writeRepoConfig(t, canonicalRoot, body)

	rc := config.LoadRepoConfig(nested)
	if rc.Server != "https://repo.example" {
		t.Fatalf("Server = %q, want https://repo.example", rc.Server)
	}
	if rc.Workspace != "team-default" {
		t.Fatalf("Workspace = %q, want team-default", rc.Workspace)
	}
	encoded, _ := json.Marshal(rc)
	if strings.Contains(string(encoded), "governance_profile") {
		t.Fatalf("RepoConfig should ignore governance_profile: %s", encoded)
	}
	// The RepoConfig type has no secret/identity fields, so credentials in the
	// file cannot be read. Guard against accidental leakage via the round-trip
	// string form.
	if got := fmt.Sprintf("%+v", rc); strings.Contains(got, "ignored-test-key") ||
		strings.Contains(got, "jwt-should-be-ignored") || strings.Contains(got, "someone-else") {
		t.Fatalf("RepoConfig leaked a secret/identity field: %s", got)
	}
}

func TestLoadRepoConfigMissingFileReturnsEmpty(t *testing.T) {
	_, nested := tempGitRepo(t)
	if got := config.LoadRepoConfig(nested); got != (config.RepoConfig{}) {
		t.Fatalf("LoadRepoConfig = %#v, want empty for missing file", got)
	}
}

func writeRepoConfig(t *testing.T, repoRoot, body string) {
	t.Helper()
	dir := filepath.Join(repoRoot, ".specgate")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func tempGitRepo(t *testing.T) (string, string) {
	t.Helper()

	root := filepath.Join(t.TempDir(), "repo")
	nested := filepath.Join(root, "app", "cli")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}

	canonicalRoot, ok := config.FindProjectRoot(nested)
	if !ok {
		t.Fatal("project root not found")
	}
	return canonicalRoot, nested
}

func TestEnsureSpecgateDirGitignore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".specgate")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gi := filepath.Join(dir, ".gitignore")

	if err := config.EnsureSpecgateDirGitignore(dir); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(gi)
	if err != nil {
		t.Fatalf("gitignore not written: %v", err)
	}
	// Scaffolds ignored (*), with the config and the .gitignore itself kept.
	s := string(body)
	if !strings.Contains(s, "*") || !strings.Contains(s, "!config") || !strings.Contains(s, "!.gitignore") {
		t.Fatalf("unexpected content:\n%s", s)
	}

	// Idempotent: an existing .gitignore is left untouched (no clobber).
	if err := os.WriteFile(gi, []byte("custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.EnsureSpecgateDirGitignore(dir); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(gi)
	if string(again) != "custom\n" {
		t.Fatalf("existing .gitignore must be left untouched, got:\n%s", again)
	}
}

func TestEnsureSpecgateDirGitignoreRejectsSymlinkedDirectory(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	dir := filepath.Join(root, ".specgate")
	if err := os.Symlink(target, dir); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if err := config.EnsureSpecgateDirGitignore(dir); err == nil {
		t.Fatal("symlinked .specgate directory was accepted")
	}
	if _, err := os.Stat(filepath.Join(target, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("external target was changed: %v", err)
	}
}

func TestEnsureSpecgateDirGitignorePreservesDanglingSymlink(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".specgate")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "missing")
	path := filepath.Join(dir, ".gitignore")
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if err := config.EnsureSpecgateDirGitignore(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("existing symlink was not preserved: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("dangling symlink target was created: %v", err)
	}
}
