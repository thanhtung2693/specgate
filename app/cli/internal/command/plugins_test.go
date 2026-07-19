package command_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestSingularPluginAliasIsRemoved(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "plugin", "doctor")
	if code == output.ExitOK {
		t.Fatalf("removed plugin alias still succeeds: %s", out.String())
	}
}

func TestPluginsInstallProjectLocalAndDoctor(t *testing.T) {
	srv := newPluginRegistry(t)
	workDir := t.TempDir()
	t.Chdir(workDir)
	homeDir := t.TempDir()

	existingMarketplace := filepath.Join(workDir, ".agents", "plugins", "marketplace.json")
	if err := os.MkdirAll(filepath.Dir(existingMarketplace), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existingMarketplace, []byte(`{"name":"personal","plugins":[{"name":"other"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	deps, out := newPluginDeps(homeDir)
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--server", srv.URL,
		"plugins", "install",
		"--project-local",
		"--agent", "cursor, codex, claude",
	)
	if code != output.ExitOK {
		t.Fatalf("install exit = %d, output = %s", code, out.String())
	}

	for _, path := range []string{
		".cursor/rules/using-specgate.mdc",
		".cursor/skills/specgate-router/SKILL.md",
		".cursor/skills/specgate-project-setup/SKILL.md",
		".codex/plugins/specgate/.codex-plugin/plugin.json",
		".codex/plugins/specgate/hooks/session-start",
		".codex/plugins/specgate/skills/specgate-work-preparation/SKILL.md",
		".codex/plugins/specgate/skills/specgate-work-delivery/SKILL.md",
		".claude/skills/specgate/.claude-plugin/plugin.json",
		".claude/skills/specgate/hooks/run-hook.cmd",
	} {
		if _, err := os.Stat(filepath.Join(workDir, path)); err != nil {
			t.Fatalf("%s missing after install: %v\n%s", path, err, out.String())
		}
	}
	marketplaceBody, err := os.ReadFile(existingMarketplace)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(marketplaceBody, []byte(`"name": "other"`)) {
		t.Fatalf("existing marketplace plugin was not preserved: %s", marketplaceBody)
	}
	if !bytes.Contains(marketplaceBody, []byte(`"path": "./.codex/plugins/specgate"`)) {
		t.Fatalf("specgate marketplace path missing: %s", marketplaceBody)
	}
	configBody, err := os.ReadFile(filepath.Join(workDir, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var codexConfig struct {
		Plugins map[string]struct {
			Enabled bool `toml:"enabled"`
		} `toml:"plugins"`
		Marketplaces map[string]struct {
			SourceType string `toml:"source_type"`
			Source     string `toml:"source"`
		} `toml:"marketplaces"`
	}
	if err := toml.Unmarshal(configBody, &codexConfig); err != nil {
		t.Fatalf("parse Codex config: %v", err)
	}
	if !codexConfig.Plugins["specgate@personal"].Enabled {
		t.Fatalf("codex config not enabled: %s", configBody)
	}
	personal := codexConfig.Marketplaces["personal"]
	if personal.SourceType != "local" || personal.Source != workDir {
		t.Fatalf("codex marketplace not registered: %s", configBody)
	}

	deps, out = newPluginDeps(homeDir)
	code = command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--server", srv.URL,
		"plugins", "doctor",
		"--project-local",
		"--agent", "all",
	)
	if code != output.ExitOK {
		t.Fatalf("doctor exit = %d, output = %s", code, out.String())
	}
}

func TestPluginsInstallGlobalRejectsProjectOnlyMarketplacePointer(t *testing.T) {
	srv := newPluginRegistry(t)
	homeDir := t.TempDir()
	marketplace := filepath.Join(homeDir, ".agents", "plugins", "marketplace.json")
	original := `{"name":"personal","plugins":[{"name":"specgate","source":{"source":"local","path":"./plugins"}}]}`
	writeTestFile(t, marketplace, original)
	deps, out := newPluginDeps(homeDir)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")

	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--server", srv.URL,
		"plugins", "install", "--agent", "codex",
	)
	if code == output.ExitOK {
		t.Fatalf("global install accepted project-only marketplace pointer: %s", out.String())
	}
	body, err := os.ReadFile(marketplace)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != original {
		t.Fatalf("global install changed unowned marketplace:\n%s", body)
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".codex", "plugins", "specgate")); !os.IsNotExist(err) {
		t.Fatalf("global install wrote plugin before rejecting marketplace; stat err=%v", err)
	}
}

func TestLocalPluginsInstallLeavesProjectInstructionsUntouched(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	original := []byte("# Project rules\n\nKeep this repository-specific guidance intact.\n")
	if err := os.WriteFile("AGENTS.md", original, 0o644); err != nil {
		t.Fatal(err)
	}

	deps, out := newPluginDeps(t.TempDir())
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{Mode: config.ModeLocal}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	args := []string{"--plain", "plugins", "install", "--project-local", "--agent", "codex"}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), args...); code != output.ExitOK {
		t.Fatalf("install exit = %d, output = %s", code, out.String())
	}
	after, err := os.ReadFile(filepath.Join(workDir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, original) {
		t.Fatalf("project instructions changed during plugin install:\n%s", after)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".codex", "plugins", "specgate", "skills", "specgate-router", "SKILL.md")); err != nil {
		t.Fatalf("Codex plugin skill missing after install: %v", err)
	}
}

func TestPluginsInstallRefusesToOverwriteUnownedCursorSkill(t *testing.T) {
	srv := newPluginRegistry(t)
	home := t.TempDir()
	foreign := filepath.Join(home, ".cursor", "skills", "specgate-router", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreign, []byte("# User-owned skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	deps, out := newPluginDeps(home)
	var stderr bytes.Buffer
	deps.Stderr = &stderr
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--server", srv.URL, "plugins", "install", "--agent", "cursor",
	)
	if code == output.ExitOK {
		t.Fatalf("install unexpectedly overwrote unowned skill: %s", out.String())
	}
	body, err := os.ReadFile(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "# User-owned skill\n" {
		t.Fatalf("foreign skill changed: %q", body)
	}
	if !strings.Contains(stderr.String(), "not owned by SpecGate") {
		t.Fatalf("missing actionable ownership error: stdout=%q stderr=%q", out.String(), stderr.String())
	}
}

func TestPluginsInstallRefusesSymlinkedOwnedTarget(t *testing.T) {
	t.Parallel()
	srv := newPluginRegistry(t)
	defer srv.Close()
	home := t.TempDir()
	external := t.TempDir()
	writeTestFile(t, filepath.Join(external, ".specgate-owned"), "specgate-plugin-v1\n")
	manifest := filepath.Join(external, ".codex-plugin", "plugin.json")
	writeTestFile(t, manifest, "keep")
	pluginRoot := filepath.Join(home, ".codex", "plugins")
	if err := os.MkdirAll(pluginRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(pluginRoot, "specgate")); err != nil {
		t.Fatal(err)
	}

	deps, _, _, out := newFakeDeps(t)
	deps.PluginRegistryURL = srv.URL
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "plugins", "install", "--agent", "codex")
	if code == output.ExitOK {
		t.Fatalf("symlinked plugin target was accepted: %s", out.String())
	}
	if body, err := os.ReadFile(manifest); err != nil || string(body) != "keep" {
		t.Fatalf("symlink target changed: body=%q err=%v", body, err)
	}
}

func TestPluginsInstallRefusesNestedSymlinkInOwnedTarget(t *testing.T) {
	t.Parallel()
	srv := newPluginRegistry(t)
	defer srv.Close()
	home := t.TempDir()
	external := t.TempDir()
	externalSkill := filepath.Join(external, "SKILL.md")
	writeTestFile(t, externalSkill, "keep")
	pluginRoot := filepath.Join(home, ".codex", "plugins", "specgate")
	writeTestFile(t, filepath.Join(pluginRoot, ".specgate-owned"), "specgate-plugin-v1\n")
	if err := os.MkdirAll(filepath.Join(pluginRoot, "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(pluginRoot, "skills", "specgate-work-preparation")); err != nil {
		t.Fatal(err)
	}

	deps, _, _, out := newFakeDeps(t)
	deps.PluginRegistryURL = srv.URL
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "plugins", "install", "--agent", "codex")
	if code == output.ExitOK {
		t.Fatalf("nested symlink was accepted: %s", out.String())
	}
	if body, err := os.ReadFile(externalSkill); err != nil || string(body) != "keep" {
		t.Fatalf("external skill changed: body=%q err=%v", body, err)
	}
}

func TestPluginsInstallRefusesSymlinkedCodexConfig(t *testing.T) {
	t.Parallel()
	srv := newPluginRegistry(t)
	defer srv.Close()
	home := t.TempDir()
	external := filepath.Join(t.TempDir(), "config.toml")
	writeTestFile(t, external, "[tools]\nkeep = true\n")
	configDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDir, "config.toml")
	if err := os.Symlink(external, configPath); err != nil {
		t.Fatal(err)
	}

	deps, _, _, out := newFakeDeps(t)
	deps.PluginRegistryURL = srv.URL
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "plugins", "install", "--agent", "codex")
	if code == output.ExitOK {
		t.Fatalf("symlinked Codex config was accepted: %s", out.String())
	}
	if info, err := os.Lstat(configPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("Codex config symlink was replaced: info=%v err=%v", info, err)
	}
}

func TestPluginsInstallPreservesUnrelatedCodexTOMLCommentsAndOrder(t *testing.T) {
	srv := newPluginRegistry(t)
	home := t.TempDir()
	configPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	originalPrefix := "# User comment\nmodel = \"gpt-test\"\n\n[tools]\n# Keep this comment\nkeep = true\n"
	if err := os.WriteFile(configPath, []byte(originalPrefix), 0o644); err != nil {
		t.Fatal(err)
	}

	deps, out := newPluginDeps(home)
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--server", srv.URL, "plugins", "install", "--agent", "codex",
	)
	if code != output.ExitOK {
		t.Fatalf("install exit = %d, output = %s", code, out.String())
	}
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(body), originalPrefix) {
		t.Fatalf("unrelated TOML text was rewritten:\n%s", body)
	}
}

func TestPluginsInstallPreservesExistingCodexConfigMode(t *testing.T) {
	srv := newPluginRegistry(t)
	home := t.TempDir()
	configPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("[tools]\nkeep = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	deps, out := newPluginDeps(home)
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--server", srv.URL, "plugins", "install", "--agent", "codex",
	)
	if code != output.ExitOK {
		t.Fatalf("install exit = %d, output = %s", code, out.String())
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("Codex config mode = %o, want 600", info.Mode().Perm())
	}
}

func TestPluginsInstallRefusesUnownedSpecGateMarketplaceEntry(t *testing.T) {
	srv := newPluginRegistry(t)
	defer srv.Close()
	home := t.TempDir()
	marketplacePath := filepath.Join(home, ".agents", "plugins", "marketplace.json")
	if err := os.MkdirAll(filepath.Dir(marketplacePath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{"name":"personal","plugins":[{"name":"specgate","source":{"source":"local","path":"/custom/specgate"}},{"name":"keep"}]}`
	if err := os.WriteFile(marketplacePath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	deps, out := newPluginDeps(home)
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "--server", srv.URL, "plugins", "install", "--agent", "codex",
	)
	if code == output.ExitOK {
		t.Fatalf("unowned marketplace entry was overwritten: %s", out.String())
	}
	if !strings.Contains(out.String(), "/custom/specgate") || !strings.Contains(out.String(), "not managed by SpecGate") {
		t.Fatalf("missing actionable ownership conflict: %s", out.String())
	}
	body, err := os.ReadFile(marketplacePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != original {
		t.Fatalf("unowned marketplace changed:\n%s", body)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "plugins", "specgate")); !os.IsNotExist(err) {
		t.Fatalf("plugin files were written before ownership validation: %v", err)
	}
}

func TestPluginsInstallRefusesInlineSpecGateCodexConfig(t *testing.T) {
	srv := newPluginRegistry(t)
	home := t.TempDir()
	configPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "plugins = { \"specgate@personal\" = { enabled = true } }\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	deps, out := newPluginDeps(home)
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--server", srv.URL, "plugins", "install", "--agent", "codex",
	)
	if code == output.ExitOK {
		t.Fatalf("inline config was rewritten: %s", out.String())
	}
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != original {
		t.Fatalf("user Codex config changed:\n%s", body)
	}
}

func TestPluginsDoctorExecutableWarningDoesNotRequirePluginUpdate(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	t.Setenv("PATH", t.TempDir())
	homeDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{Mode: config.ModeLocal}).SaveTo(configPath); err != nil {
		t.Fatal(err)
	}

	deps, out := newPluginDeps(homeDir)
	deps.ConfigPath = configPath
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "plugins", "install", "--project-local", "--agent", "codex",
	)
	if code != output.ExitOK {
		t.Fatalf("install exit = %d, output = %s", code, out.String())
	}

	deps, out = newPluginDeps(homeDir)
	deps.ConfigPath = configPath
	code = command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "plugins", "doctor", "--project-local", "--agent", "codex",
	)
	if code != output.ExitOK {
		t.Fatalf("doctor exit = %d, output = %s", code, out.String())
	}
	var env struct {
		Data struct {
			Agents []struct {
				InstalledVersion string   `json:"installed_version"`
				LatestVersion    string   `json:"latest_version"`
				NeedsUpdate      bool     `json:"needs_update"`
				Warnings         []string `json:"warnings"`
			} `json:"agents"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v, output = %s", err, out.String())
	}
	if len(env.Data.Agents) != 1 {
		t.Fatalf("unexpected doctor envelope: %s", out.String())
	}
	agent := env.Data.Agents[0]
	if agent.InstalledVersion == "" || agent.InstalledVersion != agent.LatestVersion {
		t.Fatalf("plugin versions do not match: %+v", agent)
	}
	if agent.NeedsUpdate {
		t.Fatalf("missing Codex executable incorrectly requires a plugin update: %s", out.String())
	}
	if !strings.Contains(strings.Join(agent.Warnings, "\n"), "Codex CLI executable was not found") {
		t.Fatalf("doctor warning missing unavailable Codex executable: %s", out.String())
	}
}

func TestPluginsDoctorRequiresCodexMarketplaceRegistration(t *testing.T) {
	srv := newPluginRegistry(t)
	workDir := t.TempDir()
	t.Chdir(workDir)
	homeDir := t.TempDir()

	deps, out := newPluginDeps(homeDir)
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "--server", srv.URL,
		"plugins", "install",
		"--agent", "codex",
	)
	if code != output.ExitOK {
		t.Fatalf("install exit = %d, output = %s", code, out.String())
	}
	configPath := filepath.Join(homeDir, ".codex", "config.toml")
	if err := os.WriteFile(configPath, []byte("[plugins.\"specgate@personal\"]\nenabled = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	deps, out = newPluginDeps(homeDir)
	code = command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "--server", srv.URL,
		"plugins", "doctor",
		"--agent", "codex",
	)
	if code != output.ExitUnavailable {
		t.Fatalf("doctor exit = %d, want unavailable; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "registered personal marketplace") {
		t.Fatalf("doctor output missing marketplace registration failure: %s", out.String())
	}
}

func TestPluginsDoctorRejectsUnsafeOrUnownedExpectedContent(t *testing.T) {
	tests := []struct {
		name   string
		agent  string
		mutate func(t *testing.T, home string)
	}{
		{
			name:  "symlink",
			agent: "codex",
			mutate: func(t *testing.T, home string) {
				t.Helper()
				path := filepath.Join(home, ".codex", "plugins", "specgate", ".codex-plugin", "plugin.json")
				external := filepath.Join(t.TempDir(), "plugin.json")
				if err := os.WriteFile(external, []byte(`{"name":"specgate","version":"0.1.0"}`), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, path); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:  "directory",
			agent: "claude",
			mutate: func(t *testing.T, home string) {
				t.Helper()
				path := filepath.Join(home, ".claude", "skills", "specgate", "hooks", "hooks-claude.json")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:  "unowned",
			agent: "cursor",
			mutate: func(t *testing.T, home string) {
				t.Helper()
				marker := filepath.Join(home, ".cursor", "rules", "using-specgate.mdc.specgate-owned")
				if err := os.Remove(marker); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:  "unowned_codex_config",
			agent: "codex",
			mutate: func(t *testing.T, home string) {
				t.Helper()
				configPath := filepath.Join(home, ".codex", "config.toml")
				body := "[plugins.\"specgate@personal\"]\nenabled = true\n\n[marketplaces.personal]\nsource_type = \"local\"\nsource = " + strconv.Quote(filepath.Join(t.TempDir(), "foreign")) + "\n"
				if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:  "unowned_codex_marketplace",
			agent: "codex",
			mutate: func(t *testing.T, home string) {
				t.Helper()
				marketplacePath := filepath.Join(home, ".agents", "plugins", "marketplace.json")
				body := `{"name":"personal","plugins":[{"name":"specgate","source":{"source":"local","path":"/custom/specgate"}}]}`
				if err := os.WriteFile(marketplacePath, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newPluginRegistry(t)
			home := t.TempDir()
			deps, out := newPluginDeps(home)
			if code := command.ExecuteForCode(command.NewRootCommand(deps),
				"--plain", "--server", srv.URL, "plugins", "install", "--agent", tt.agent,
			); code != output.ExitOK {
				t.Fatalf("install exit = %d, output = %s", code, out.String())
			}

			tt.mutate(t, home)
			deps, out = newPluginDeps(home)
			code := command.ExecuteForCode(command.NewRootCommand(deps),
				"--json", "--server", srv.URL, "plugins", "doctor", "--agent", tt.agent,
			)
			if code != output.ExitUnavailable {
				t.Fatalf("doctor exit = %d, want unavailable; output = %s", code, out.String())
			}
			if !strings.Contains(out.String(), "specgate plugins install --agent "+tt.agent) {
				t.Fatalf("doctor output missing repair command: %s", out.String())
			}
		})
	}
}

func TestPluginsDoctorReportsMissing(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	homeDir := t.TempDir()
	deps, out := newPluginDeps(homeDir)
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "--server", "http://127.0.0.1:1",
		"plugins", "doctor",
		"--project-local",
		"--agent", "codex",
	)
	if code != output.ExitUnavailable {
		t.Fatalf("doctor exit = %d, want unavailable; output = %s", code, out.String())
	}
	var env struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v, output = %s", err, out.String())
	}
	if env.OK || env.Error.Code != "unavailable" {
		t.Fatalf("unexpected doctor envelope: %s", out.String())
	}
	if !strings.Contains(out.String(), "specgate plugins install --agent codex --project-local") {
		t.Fatalf("doctor output missing repair command: %s", out.String())
	}
}

func TestPluginsDoctorProjectLocalVersionWarningUsesProjectRepairCommand(t *testing.T) {
	srv := newPluginRegistry(t)
	workDir := t.TempDir()
	t.Chdir(workDir)
	homeDir := t.TempDir()

	deps, out := newPluginDeps(homeDir)
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "--server", srv.URL,
		"plugins", "install",
		"--project-local",
		"--agent", "codex",
	)
	if code != output.ExitOK {
		t.Fatalf("install exit = %d, output = %s", code, out.String())
	}
	manifest := filepath.Join(workDir, ".codex", "plugins", "specgate", ".codex-plugin", "plugin.json")
	if err := os.WriteFile(manifest, []byte(`{"name":"specgate","version":"0.0.1"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	deps, out = newPluginDeps(homeDir)
	code = command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "--server", srv.URL,
		"plugins", "doctor",
		"--project-local",
		"--agent", "codex",
	)
	if code != output.ExitOK {
		t.Fatalf("doctor exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "run 'specgate plugins install --agent codex --project-local'") {
		t.Fatalf("doctor warning missing project-local repair command: %s", out.String())
	}
}

func TestPluginsDoctorWarnsWhenCodexCacheIsStale(t *testing.T) {
	srv := newPluginRegistry(t)
	workDir := t.TempDir()
	t.Chdir(workDir)
	homeDir := t.TempDir()

	deps, out := newPluginDeps(homeDir)
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--server", srv.URL,
		"plugins", "install",
		"--agent", "codex",
	)
	if code != output.ExitOK {
		t.Fatalf("install exit = %d, output = %s", code, out.String())
	}

	cacheRoot := filepath.Join(homeDir, ".codex", "plugins", "cache", "personal", "specgate", "0.1.0")
	for _, skill := range []string{"specgate-project-setup", "specgate-work-preparation"} {
		if err := os.Remove(filepath.Join(cacheRoot, "skills", skill, "SKILL.md")); err != nil {
			t.Fatal(err)
		}
	}
	for _, skill := range []string{
		"specgate-router",
		"specgate-work-delivery",
	} {
		path := filepath.Join(cacheRoot, "skills", skill, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("# "+skill+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	deps, out = newPluginDeps(homeDir)
	code = command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "--server", srv.URL,
		"plugins", "doctor",
		"--agent", "codex",
	)
	if code != output.ExitOK {
		t.Fatalf("doctor exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			LatestVersion string `json:"latest_version"`
			Agents        []struct {
				Agent       string   `json:"agent"`
				OK          bool     `json:"ok"`
				NeedsUpdate bool     `json:"needs_update"`
				Warnings    []string `json:"warnings"`
			} `json:"agents"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v, output = %s", err, out.String())
	}
	if !env.OK || env.Data.LatestVersion != "0.1.0" || len(env.Data.Agents) != 1 {
		t.Fatalf("unexpected doctor envelope: %s", out.String())
	}
	agent := env.Data.Agents[0]
	if agent.Agent != "codex" || !agent.OK || !agent.NeedsUpdate {
		t.Fatalf("unexpected codex health: %+v", agent)
	}
	got := strings.Join(agent.Warnings, "\n")
	for _, want := range []string{"Codex plugin cache is stale", "specgate-project-setup", "specgate-work-preparation", "restart Codex"} {
		if !strings.Contains(got, want) {
			t.Fatalf("warning missing %q: %s", want, out.String())
		}
	}
}

func TestPluginsInstallRemovesOnlyObsoleteOwnedCodexCacheVersions(t *testing.T) {
	srv := newPluginRegistry(t)
	home := t.TempDir()
	cacheRoot := filepath.Join(home, ".codex", "plugins", "cache", "personal", "specgate")
	obsolete := filepath.Join(cacheRoot, "0.0.9")
	writeTestFile(t, filepath.Join(obsolete, ".specgate-owned"), "specgate-plugin-v1\n")
	writeTestFile(t, filepath.Join(obsolete, ".codex-plugin", "plugin.json"), `{}`)
	unowned := filepath.Join(cacheRoot, "custom", "keep.txt")
	writeTestFile(t, unowned, "keep")

	deps, out := newPluginDeps(home)
	if code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--server", srv.URL,
		"plugins", "install", "--agent", "codex",
	); code != output.ExitOK {
		t.Fatalf("install exit = %d, output = %s", code, out.String())
	}

	if _, err := os.Stat(obsolete); !os.IsNotExist(err) {
		t.Fatalf("obsolete owned cache remains: %v", err)
	}
	if body, err := os.ReadFile(unowned); err != nil || string(body) != "keep" {
		t.Fatalf("unowned cache changed: body=%q err=%v", body, err)
	}
	if _, err := os.Stat(filepath.Join(cacheRoot, "0.1.0", ".codex-plugin", "plugin.json")); err != nil {
		t.Fatalf("current cache missing: %v", err)
	}
}

func TestPluginsInstallUsesRegistryFlag(t *testing.T) {
	srv := newPluginRegistry(t)
	workDir := t.TempDir()
	t.Chdir(workDir)
	homeDir := t.TempDir()

	deps, out := newPluginDeps(homeDir)
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--server", "http://127.0.0.1:1",
		"plugins", "install",
		"--project-local",
		"--agent", "claude",
		"--registry", srv.URL,
	)
	if code != output.ExitOK {
		t.Fatalf("install exit = %d, output = %s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(workDir, ".claude", "skills", "specgate", ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("claude plugin missing after registry install: %v\n%s", err, out.String())
	}
}

func TestPluginsInstallRejectsUnsafePackageSkillNames(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	homeDir := t.TempDir()
	outside := filepath.Join(workDir, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/plugins/")
		switch path {
		case "package.json":
			_, _ = io.WriteString(w, `{
  "schema": "specgate.plugin-package/v1",
  "name": "specgate",
  "display_name": "SpecGate",
  "version": "0.1.0",
  "description": "SpecGate plugin",
  "skills": ["specgate-router", "../../outside"],
  "served_files": []
}`)
		case "rules/using-specgate.mdc":
			_, _ = io.WriteString(w, "use specgate\n")
		default:
			_, _ = io.WriteString(w, "malicious\n")
		}
	}))
	t.Cleanup(srv.Close)

	deps, out := newPluginDeps(homeDir)
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "--server", srv.URL,
		"plugins", "install",
		"--project-local",
		"--agent", "cursor",
	)
	if code != output.ExitUsage {
		t.Fatalf("install exit = %d, want usage; output = %s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatalf("unsafe package skill removed outside directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".cursor", "rules", "using-specgate.mdc")); !os.IsNotExist(err) {
		t.Fatalf("installer wrote files after unsafe package validation failed; stat err=%v", err)
	}
}

func TestPluginsInstallPromptsForAgentSelection(t *testing.T) {
	srv := newPluginRegistry(t)
	home := t.TempDir()

	deps, out := newPluginDeps(home)
	prompter := &fakePrompter{multiValues: []string{"cursor", "codex"}}
	deps.Prompter = prompter
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--server", srv.URL,
		"plugins", "install",
	)
	if code != output.ExitOK {
		t.Fatalf("install exit = %d, output = %s", code, out.String())
	}
	if prompter.multiTitle != "Select IDE plugins" {
		t.Fatalf("multi-select title = %q", prompter.multiTitle)
	}
	for _, path := range []string{
		filepath.Join(home, ".cursor", "rules", "using-specgate.mdc"),
		filepath.Join(home, ".codex", "plugins", "specgate", ".codex-plugin", "plugin.json"),
		filepath.Join(home, ".codex", "plugins", "cache", "personal", "specgate", "0.1.0", ".codex-plugin", "plugin.json"),
		filepath.Join(home, ".codex", "plugins", "cache", "personal", "specgate", "0.1.0", "skills", "specgate-router", "SKILL.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s missing after prompted install: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "specgate")); !os.IsNotExist(err) {
		t.Fatalf("claude should not be installed when unchecked; stat err=%v", err)
	}
}

func TestPluginsInstallUsesDetectedAgentDefaults(t *testing.T) {
	srv := newPluginRegistry(t)
	home := t.TempDir()

	deps, out := newPluginDeps(home)
	deps.PluginAgentDefaults = func() []string { return []string{"codex"} }
	prompter := &fakePrompter{}
	deps.Prompter = prompter
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--server", srv.URL,
		"plugins", "install",
	)
	if code != output.ExitOK {
		t.Fatalf("install exit = %d, output = %s", code, out.String())
	}
	if strings.Join(prompter.multiDefaults, ",") != "codex" {
		t.Fatalf("multi-select defaults = %#v, want codex", prompter.multiDefaults)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "plugins", "specgate", ".codex-plugin", "plugin.json")); err != nil {
		t.Fatalf("codex plugin missing after default install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".cursor", "rules", "using-specgate.mdc")); !os.IsNotExist(err) {
		t.Fatalf("cursor should not be installed when not selected; stat err=%v", err)
	}
}

func TestPluginsInstallPlainModeDoesNotPromptForAgent(t *testing.T) {
	srv := newPluginRegistry(t)
	home := t.TempDir()

	deps, out := newPluginDeps(home)
	prompter := &fakePrompter{multiValues: []string{"codex"}}
	deps.Prompter = prompter
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--server", srv.URL,
		"plugins", "install",
	)
	if code != output.ExitOK {
		t.Fatalf("install exit = %d, output = %s", code, out.String())
	}
	if prompter.multiTitle != "" {
		t.Fatalf("plain install prompted for agent: %q", prompter.multiTitle)
	}
	for _, path := range []string{
		filepath.Join(home, ".cursor", "rules", "using-specgate.mdc"),
		filepath.Join(home, ".codex", "plugins", "specgate", ".codex-plugin", "plugin.json"),
		filepath.Join(home, ".claude", "skills", "specgate", ".claude-plugin", "plugin.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s missing after plain install: %v", path, err)
		}
	}
}

func TestPluginsInstallPromptsForScope(t *testing.T) {
	srv := newPluginRegistry(t)
	workDir := t.TempDir()
	t.Chdir(workDir)
	home := t.TempDir()

	deps, out := newPluginDeps(home)
	prompter := &fakePrompter{multiValues: []string{"codex"}, selectedValue: "project"}
	deps.Prompter = prompter
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--server", srv.URL,
		"plugins", "install",
	)
	if code != output.ExitOK {
		t.Fatalf("install exit = %d, output = %s", code, out.String())
	}
	if len(prompter.selectOptions) != 2 || prompter.selectOptions[0].Value != "global" || prompter.selectOptions[1].Value != "project" {
		t.Fatalf("scope options = %#v", prompter.selectOptions)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".codex", "plugins", "specgate", ".codex-plugin", "plugin.json")); err != nil {
		t.Fatalf("project-local codex plugin missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "plugins", "specgate")); !os.IsNotExist(err) {
		t.Fatalf("global codex plugin should not be installed for project scope; stat err=%v", err)
	}
	if !strings.Contains(out.String(), "Scope: project-local") {
		t.Fatalf("output missing project-local scope: %s", out.String())
	}
}

func TestPluginsInstallDryRunSaysNoFilesWereWritten(t *testing.T) {
	srv := newPluginRegistry(t)
	workDir := t.TempDir()
	t.Chdir(workDir)
	home := t.TempDir()

	deps, out := newPluginDeps(home)
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--server", srv.URL,
		"plugins", "install",
		"--agent", "codex",
		"--project-local",
		"--dry-run",
	)
	if code != output.ExitOK {
		t.Fatalf("dry-run exit = %d, output = %s", code, out.String())
	}
	text := out.String()
	for _, want := range []string{"Plugin setup plan for:", "No files were written."} {
		if !strings.Contains(text, want) {
			t.Fatalf("dry-run output missing %q:\n%s", want, text)
		}
	}
	for _, misleading := range []string{"setup installed", "Restart the selected IDE", "plugins doctor"} {
		if strings.Contains(text, misleading) {
			t.Fatalf("dry-run output claims a side effect via %q:\n%s", misleading, text)
		}
	}
	if _, err := os.Stat(filepath.Join(workDir, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote project files: %v", err)
	}
}

func TestPluginsInstallRejectsMalformedCodexConfigBeforeAnyWrite(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry_run_%t", dryRun), func(t *testing.T) {
			srv := newPluginRegistry(t)
			workDir := t.TempDir()
			t.Chdir(workDir)
			configPath := filepath.Join(workDir, ".codex", "config.toml")
			writeTestFile(t, configPath, "[broken")

			deps, out := newPluginDeps(t.TempDir())
			args := []string{
				"--json", "--server", srv.URL,
				"plugins", "install",
				"--agent", "codex",
				"--project-local",
			}
			if dryRun {
				args = append(args, "--dry-run")
			}
			code := command.ExecuteForCode(command.NewRootCommand(deps), args...)
			if code == output.ExitOK {
				t.Fatalf("install accepted malformed Codex config: %s", out.String())
			}
			if _, err := os.Stat(filepath.Join(workDir, ".codex", "plugins", "specgate")); !os.IsNotExist(err) {
				t.Fatalf("install wrote plugin files before config validation: %v", err)
			}
			body, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != "[broken" {
				t.Fatalf("malformed Codex config changed: %q", body)
			}
		})
	}
}

func newPluginDeps(homeDir string) (*command.Deps, *bytes.Buffer) {
	var out bytes.Buffer
	return &command.Deps{
		Stdout:     &out,
		Stderr:     io.Discard,
		Stdin:      strings.NewReader(""),
		Opener:     func(_ string) error { return nil },
		ConfigPath: filepath.Join(homeDir, ".config", "specgate", "config.json"),
		UserHomeDir: func() (string, error) {
			return homeDir, nil
		},
	}, &out
}

func newPluginRegistry(t *testing.T) *httptest.Server {
	t.Helper()
	skills := []string{
		"specgate-router",
		"specgate-project-setup",
		"specgate-work-preparation",
		"specgate-work-delivery",
	}
	files := map[string]string{
		"package.json": `{
  "schema": "specgate.plugin-package/v1",
  "name": "specgate",
  "display_name": "SpecGate",
  "version": "0.1.0",
  "description": "SpecGate plugin",
  "skills": ["specgate-router", "specgate-project-setup", "specgate-work-preparation", "specgate-work-delivery"],
  "served_files": []
}`,
		"rules/using-specgate.mdc":   "use specgate\n",
		".codex-plugin/plugin.json":  `{"name":"specgate"}`,
		".claude-plugin/plugin.json": `{"name":"specgate"}`,
		"assets/logo.svg":            "<svg></svg>\n",
		"hooks/hooks.json":           `{"hooks":{}}`,
		"hooks/hooks-claude.json":    `{"hooks":{}}`,
		"hooks/run-hook.cmd":         "#!/usr/bin/env sh\n",
		"hooks/session-start":        "#!/usr/bin/env sh\n",
		".agents/plugins/personal-marketplace.json": `{
  "name": "personal",
  "interface": {"displayName": "Personal"},
  "plugins": [{
    "name": "specgate",
    "source": {"source": "local", "path": "__SPECGATE_PLUGIN_PATH__"},
    "policy": {"installation": "AVAILABLE", "authentication": "ON_INSTALL"},
    "category": "Productivity"
  }]
}`,
	}
	for _, skill := range skills {
		files["skills/"+skill+"/SKILL.md"] = "# " + skill + "\n"
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/plugins/")
		body, ok := files[path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}
