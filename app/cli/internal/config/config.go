package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/specgate/specgate/app/cli/internal/fsutil"
)

const DefaultServerURL = "http://localhost:3000/api/doc-registry"

type Mode string

const (
	ModeLocal Mode = "local"
	ModeFull  Mode = "full"
)

// LocalStore identifies a Local CLI SQLite store without conflating it with a
// Full appliance deployment.
type LocalStore struct {
	Path string `json:"path,omitempty"`
	ID   string `json:"id,omitempty"`
}

// Config holds user-persisted CLI settings.
type Config struct {
	Mode          Mode                     `json:"mode,omitempty"`
	Local         LocalStore               `json:"local,omitempty"`
	Server        string                   `json:"server,omitempty"`
	DeploymentDir string                   `json:"deployment_dir,omitempty"`
	CurrentUser   CurrentUser              `json:"current_user,omitempty"`
	Workspace     CurrentWorkspace         `json:"workspace,omitempty"`
	Projects      map[string]ProjectConfig `json:"projects,omitempty"`
	// Credentials holds one gateway credential per server URL. Keyed by server so
	// switching servers can never send a credential to a host it was not issued
	// for; the whole file is written at mode 0600.
	Credentials map[string]ServerCredential `json:"credentials,omitempty"`
}

// ServerCredential is one appliance's gateway credential. The secret lives here
// because the gateway verifies it per request; status output reports only whether
// it is set, and no command prints the secret.
type ServerCredential struct {
	Username string `json:"username"`
	Secret   string `json:"secret"`
}

// NormalizeServerURL is the credential map key: a trimmed, lowercased URL with no
// trailing slash, so the same appliance written three ways resolves to one entry.
func NormalizeServerURL(raw string) string {
	return strings.TrimRight(strings.ToLower(strings.TrimSpace(raw)), "/")
}

// CredentialFor returns the credential stored for a server URL.
func (c Config) CredentialFor(serverURL string) (ServerCredential, bool) {
	credential, ok := c.Credentials[NormalizeServerURL(serverURL)]
	if !ok || credential.Username == "" || credential.Secret == "" {
		return ServerCredential{}, false
	}
	return credential, true
}

// CurrentUser stores the CLI-selected local user.
type CurrentUser struct {
	ID          string `json:"id,omitempty"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Email       string `json:"email,omitempty"`
}

// CurrentWorkspace stores the CLI-selected workspace.
type CurrentWorkspace struct {
	ID   string `json:"id,omitempty"`
	Slug string `json:"slug,omitempty"`
	Name string `json:"name,omitempty"`
}

// ProjectConfig stores CLI defaults that should apply only when commands run
// from a specific project checkout.
type ProjectConfig struct {
	Workspace CurrentWorkspace `json:"workspace,omitempty"`
	Local     LocalStore       `json:"local,omitempty"`
}

// RepoConfig holds optional committed `.specgate/config` shared team defaults.
// Secrets, API keys, governance policy state, and per-user identity are never
// sourced here. Those stay in env, deployment .env, or the global user config.
type RepoConfig struct {
	Server    string `json:"server,omitempty"`
	Workspace string `json:"workspace,omitempty"` // repo default workspace slug
}

// WorkspaceSource identifies where the effective workspace came from.
type WorkspaceSource string

const (
	WorkspaceSourceNone     WorkspaceSource = "none"
	WorkspaceSourceOverride WorkspaceSource = "override"
	WorkspaceSourceProject  WorkspaceSource = "project"
	WorkspaceSourceRepo     WorkspaceSource = "repo"
	WorkspaceSourceGlobal   WorkspaceSource = "global"
)

// ResolvedWorkspace captures the selected workspace and its source.
type ResolvedWorkspace struct {
	Workspace   CurrentWorkspace
	Source      WorkspaceSource
	ProjectRoot string
}

// ResolveServer returns the effective server URL using the precedence chain:
// flag > SPECGATE_SERVER env > repo .specgate/config > global config file >
// the local appliance gateway.
func ResolveServer(flag string, repo RepoConfig, cfg Config) string {
	if flag != "" {
		return flag
	}
	if env := os.Getenv("SPECGATE_SERVER"); env != "" {
		return env
	}
	if repo.Server != "" {
		return repo.Server
	}
	if cfg.Server != "" {
		return cfg.Server
	}
	return DefaultServerURL
}

// ResolvePluginRegistry excludes repository configuration because plugin
// packages can install executable IDE hooks into a user's global environment.
func ResolvePluginRegistry(flag string, cfg Config) string {
	return ResolveServer(flag, RepoConfig{}, cfg)
}

// ResolveMode defaults commands to Full until setup persists a mode.
// `specgate init` owns the Local-first choice for a fresh installation.
func ResolveMode(cfg Config) Mode {
	if cfg.Mode == ModeLocal || cfg.Mode == ModeFull {
		return cfg.Mode
	}
	return ModeFull
}

// ResolveLocalDir uses command flag > environment > project binding > selected
// global Local store > caller-provided OS default.
func ResolveLocalDir(flag, env string, project ProjectConfig, cfg Config, fallback string) string {
	for _, path := range []string{flag, env, project.Local.Path, cfg.Local.Path, fallback} {
		if trimmed := strings.TrimSpace(path); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// ResolveWorkspaceSource returns the effective workspace using the precedence
// chain override > per-user project binding > repo .specgate/config default >
// global config > none. The repo config default is honored only when no
// per-user project binding exists — the binding always outranks it.
func ResolveWorkspaceSource(cfg Config, repo RepoConfig, cwd, override string) ResolvedWorkspace {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		return ResolvedWorkspace{
			Workspace: CurrentWorkspace{Slug: trimmed},
			Source:    WorkspaceSourceOverride,
		}
	}

	root, hasProjectRoot := FindProjectRoot(cwd)
	if hasProjectRoot && cfg.Projects != nil {
		if project, ok := cfg.Projects[root]; ok && project.Workspace != (CurrentWorkspace{}) {
			return ResolvedWorkspace{
				Workspace:   project.Workspace,
				Source:      WorkspaceSourceProject,
				ProjectRoot: root,
			}
		}
	}

	if slug := strings.TrimSpace(repo.Workspace); slug != "" {
		return ResolvedWorkspace{
			Workspace:   CurrentWorkspace{Slug: slug},
			Source:      WorkspaceSourceRepo,
			ProjectRoot: root,
		}
	}

	if cfg.Workspace != (CurrentWorkspace{}) {
		return ResolvedWorkspace{
			Workspace:   cfg.Workspace,
			Source:      WorkspaceSourceGlobal,
			ProjectRoot: root,
		}
	}

	return ResolvedWorkspace{Source: WorkspaceSourceNone}
}

// LoadRepoConfig reads the optional committed `.specgate/config` (JSON) from the
// repo root nearest to cwd, reusing FindProjectRoot for root detection. A
// missing, unreadable, or malformed file yields an empty RepoConfig — the repo
// layer is optional and never fatal. Only server URL and default workspace slug
// are parsed; secrets and identity are ignored by construction (RepoConfig has
// no such fields).
func LoadRepoConfig(cwd string) RepoConfig {
	root, ok := FindProjectRoot(cwd)
	if !ok {
		return RepoConfig{}
	}
	data, err := os.ReadFile(filepath.Join(root, ".specgate", "config"))
	if err != nil {
		return RepoConfig{}
	}
	var rc RepoConfig
	if json.Unmarshal(data, &rc) != nil {
		return RepoConfig{}
	}
	return rc
}

// DefaultPath returns os.UserConfigDir()/specgate/config.json.
func DefaultPath() (string, error) {
	if path := strings.TrimSpace(os.Getenv("SPECGATE_CONFIG_PATH")); path != "" {
		return path, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "specgate", "config.json"), nil
}

// LoadFrom reads the config from path. Empty path uses DefaultPath.
func LoadFrom(path string) (Config, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return Config{}, err
		}
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, err
	}
	if c.Mode != "" && c.Mode != ModeLocal && c.Mode != ModeFull {
		return Config{}, fmt.Errorf("invalid SpecGate mode %q; expected local or full", c.Mode)
	}
	return c, nil
}

// FindProjectRoot returns the nearest ancestor containing .git. The returned
// path is absolute and symlink-resolved where possible.
func FindProjectRoot(start string) (string, bool) {
	if start == "" {
		var err error
		start, err = os.Getwd()
		if err != nil {
			return "", false
		}
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", false
	}
	if !info.IsDir() {
		abs = filepath.Dir(abs)
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, ".git")); err == nil {
			return abs, true
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", false
		}
		abs = parent
	}
}

// EnsureSpecgateDirGitignore writes a .gitignore inside the .specgate working
// directory so its transient delivery scaffolds (completion-<ref>.json) and the
// generated ignore file stay private, while config and review handoffs can be
// committed deliberately. A nested .gitignore keeps this self-contained: git
// honors it even when it ignores itself, so the repo's root .gitignore is never
// touched. Exact generated content from older releases is upgraded; user-edited
// files are left untouched.
func EnsureSpecgateDirGitignore(dir string) error {
	info, err := os.Lstat(dir)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%s must be a real directory", dir)
		}
	case os.IsNotExist(err):
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	default:
		return err
	}

	const legacyContent = "# SpecGate working dir: ignore transient delivery scaffolds,\n" +
		"# keep the committed config, review handoffs, and this file.\n" +
		"*\n!.gitignore\n!config\n!handoffs/\n!handoffs/**\n"
	const initialLegacyContent = "# SpecGate working dir: ignore transient delivery scaffolds,\n" +
		"# keep the committed config and this file.\n" +
		"*\n!.gitignore\n!config\n"
	const content = "# SpecGate working dir: ignore generated files and transient delivery scaffolds.\n" +
		"*\n!config\n!handoffs/\n!handoffs/**\n"

	path := filepath.Join(dir, ".gitignore")
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil || (string(body) != legacyContent && string(body) != initialLegacyContent) {
			return nil
		}
		return fsutil.AtomicWriteFile(path, []byte(content), info.Mode().Perm())
	} else if !os.IsNotExist(err) {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if os.IsExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	return file.Close()
}

// SetProjectWorkspace records a workspace binding for projectRoot.
func (c *Config) SetProjectWorkspace(projectRoot string, workspace CurrentWorkspace) {
	projectRoot = canonicalProjectRoot(projectRoot)
	if projectRoot == "" {
		return
	}
	if c.Projects == nil {
		c.Projects = map[string]ProjectConfig{}
	}
	project := c.Projects[projectRoot]
	project.Workspace = workspace
	if c.Mode == ModeLocal && project.Local.Path == "" {
		project.Local = c.Local
	}
	c.Projects[projectRoot] = project
}

// RemoveProjectWorkspace deletes the workspace binding for projectRoot.
func (c *Config) RemoveProjectWorkspace(projectRoot string) bool {
	if c == nil || c.Projects == nil {
		return false
	}
	projectRoot = canonicalProjectRoot(projectRoot)
	if projectRoot == "" {
		return false
	}
	if _, ok := c.Projects[projectRoot]; !ok {
		return false
	}
	delete(c.Projects, projectRoot)
	if len(c.Projects) == 0 {
		c.Projects = nil
	}
	return true
}

func canonicalProjectRoot(projectRoot string) string {
	if projectRoot == "" {
		return ""
	}
	if root, ok := FindProjectRoot(projectRoot); ok {
		return root
	}
	abs, err := filepath.Abs(projectRoot)
	if err != nil {
		return projectRoot
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return abs
}

// Save writes the config to DefaultPath atomically with mode 0600.
func (c Config) Save() error {
	return c.SaveTo("")
}

// SaveTo writes the config to path atomically with mode 0600.
// Empty path uses DefaultPath.
func (c Config) SaveTo(path string) error {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return fsutil.AtomicWriteFile(path, data, 0o600)
}
