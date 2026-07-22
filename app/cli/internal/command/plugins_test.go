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
	existingMarketplaceBody := []byte(`{"name":"personal","plugins":[{"name":"other"}]}`)
	if err := os.MkdirAll(filepath.Dir(existingMarketplace), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existingMarketplace, existingMarketplaceBody, 0o644); err != nil {
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
		".cursor/skills/specgate/SKILL.md",
		".cursor/skills/specgate-project-setup/SKILL.md",
		".agents/skills/specgate/SKILL.md",
		".agents/skills/specgate-work-preparation/SKILL.md",
		".agents/skills/specgate-work-delivery/SKILL.md",
		".claude/skills/specgate/SKILL.md",
		".claude/skills/specgate-project-setup/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(workDir, path)); err != nil {
			t.Fatalf("%s missing after install: %v\n%s", path, err, out.String())
		}
	}
	marketplaceBody, err := os.ReadFile(existingMarketplace)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(marketplaceBody, existingMarketplaceBody) {
		t.Fatalf("project-local install changed unrelated marketplace config: %s", marketplaceBody)
	}
	for _, path := range []string{
		".codex/plugins/specgate",
		".codex/config.toml",
	} {
		if _, err := os.Stat(filepath.Join(workDir, path)); !os.IsNotExist(err) {
			t.Fatalf("obsolete project-local path %s was created: %v", path, err)
		}
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

func TestPluginsInstallProjectLocalCodexMigratesOnlyOwnedLegacyFiles(t *testing.T) {
	srv := newPluginRegistry(t)
	workDir := t.TempDir()
	t.Chdir(workDir)
	homeDir := t.TempDir()

	legacyRoot := filepath.Join(workDir, ".codex", "plugins", "specgate")
	writeTestFile(t, filepath.Join(legacyRoot, ".specgate-owned"), "specgate-plugin-v1\n")
	writeTestFile(t, filepath.Join(legacyRoot, ".codex-plugin", "plugin.json"), `{}`)
	marketplacePath := filepath.Join(workDir, ".agents", "plugins", "marketplace.json")
	writeTestFile(t, marketplacePath, `{"name":"personal","plugins":[{"name":"specgate","source":{"source":"local","path":"./.codex/plugins/specgate"}},{"name":"other"}]}`)
	configPath := filepath.Join(workDir, ".codex", "config.toml")
	writeTestFile(t, configPath, "[plugins.\"specgate@personal\"]\nenabled = true\n\n[marketplaces.personal]\nsource_type = \"local\"\nsource = "+strconv.Quote(workDir)+"\n\n[tools]\nkeep = true\n")

	deps, out := newPluginDeps(homeDir)
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--server", srv.URL,
		"plugins", "install", "--project-local", "--agent", "codex",
	)
	if code != output.ExitOK {
		t.Fatalf("install exit = %d, output = %s", code, out.String())
	}
	if _, err := os.Stat(legacyRoot); !os.IsNotExist(err) {
		t.Fatalf("owned legacy Codex bundle remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".agents", "skills", "specgate", "SKILL.md")); err != nil {
		t.Fatalf("focused Codex skill missing: %v", err)
	}
	marketplaceBody, err := os.ReadFile(marketplacePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(marketplaceBody, []byte(`"name": "specgate"`)) || !bytes.Contains(marketplaceBody, []byte(`"name": "other"`)) {
		t.Fatalf("legacy marketplace entry was not removed safely: %s", marketplaceBody)
	}
	configBody, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(configBody, []byte("specgate@personal")) || !bytes.Contains(configBody, []byte("[tools]")) {
		t.Fatalf("legacy Codex config was not cleaned safely: %s", configBody)
	}
}

func TestPluginsInstallProjectLocalCodexRemovesEmptyLegacyDirectory(t *testing.T) {
	srv := newPluginRegistry(t)
	workDir := t.TempDir()
	t.Chdir(workDir)

	legacyRoot := filepath.Join(workDir, ".codex", "plugins", "specgate")
	writeTestFile(t, filepath.Join(legacyRoot, ".specgate-owned"), "specgate-plugin-v1\n")
	writeTestFile(t, filepath.Join(legacyRoot, ".codex-plugin", "plugin.json"), `{}`)
	writeTestFile(t, filepath.Join(workDir, ".agents", "plugins", "marketplace.json"), `{"name":"personal","plugins":[{"name":"specgate","source":{"source":"local","path":"./.codex/plugins/specgate"}}]}`)
	writeTestFile(t, filepath.Join(workDir, ".codex", "config.toml"), "[plugins.\"specgate@personal\"]\nenabled = true\n\n[marketplaces.personal]\nsource_type = \"local\"\nsource = "+strconv.Quote(workDir)+"\n")

	deps, out := newPluginDeps(t.TempDir())
	if code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--server", srv.URL,
		"plugins", "install", "--project-local", "--agent", "codex",
	); code != output.ExitOK {
		t.Fatalf("install exit = %d, output = %s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(workDir, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("empty legacy Codex directory remains: %v", err)
	}
}

func TestPluginsInstallProjectLocalCodexRejectsSymlinkedLegacyAncestors(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		linkPath      func(string) string
		externalSetup func(*testing.T, string)
		sentinel      func(string) string
	}{
		{
			name:     "codex directory",
			linkPath: func(workDir string) string { return filepath.Join(workDir, ".codex") },
			externalSetup: func(t *testing.T, external string) {
				writeTestFile(t, filepath.Join(external, "plugins", "specgate", ".specgate-owned"), "specgate-plugin-v1\n")
				writeTestFile(t, filepath.Join(external, "plugins", "specgate", ".codex-plugin", "plugin.json"), `{}`)
			},
			sentinel: func(external string) string {
				return filepath.Join(external, "plugins", "specgate", ".codex-plugin", "plugin.json")
			},
		},
		{
			name:     "agents plugins directory",
			linkPath: func(workDir string) string { return filepath.Join(workDir, ".agents", "plugins") },
			externalSetup: func(t *testing.T, external string) {
				writeTestFile(t, filepath.Join(external, "marketplace.json"), `{"name":"personal","plugins":[{"name":"specgate","source":{"source":"local","path":"./.codex/plugins/specgate"}}]}`)
			},
			sentinel: func(external string) string { return filepath.Join(external, "marketplace.json") },
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			srv := newPluginRegistry(t)
			workDir := t.TempDir()
			t.Chdir(workDir)
			external := t.TempDir()
			testCase.externalSetup(t, external)
			before, err := os.ReadFile(testCase.sentinel(external))
			if err != nil {
				t.Fatal(err)
			}
			if testCase.name == "agents plugins directory" {
				writeTestFile(t, filepath.Join(workDir, ".codex", "plugins", "specgate", ".specgate-owned"), "specgate-plugin-v1\n")
				writeTestFile(t, filepath.Join(workDir, ".codex", "plugins", "specgate", ".codex-plugin", "plugin.json"), `{}`)
			}
			if err := os.MkdirAll(filepath.Dir(testCase.linkPath(workDir)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(external, testCase.linkPath(workDir)); err != nil {
				t.Skipf("symlink unsupported: %v", err)
			}

			deps, out := newPluginDeps(t.TempDir())
			code := command.ExecuteForCode(command.NewRootCommand(deps),
				"--plain", "--server", srv.URL,
				"plugins", "install", "--project-local", "--agent", "codex",
			)
			if code == output.ExitOK {
				t.Fatalf("project-local install accepted symlinked ancestor: %s", out.String())
			}
			after, err := os.ReadFile(testCase.sentinel(external))
			if err != nil {
				t.Fatalf("external sentinel was removed: %v", err)
			}
			if !bytes.Equal(after, before) {
				t.Fatalf("external sentinel changed:\n%s", after)
			}
			if _, err := os.Stat(filepath.Join(workDir, ".agents", "skills", "specgate", "SKILL.md")); !os.IsNotExist(err) {
				t.Fatalf("installer wrote project skills before rejecting unsafe ancestry: %v", err)
			}
		})
	}
}

func TestPluginsInstallProjectLocalCodexIgnoresUnrelatedSymlinkedIDEPaths(t *testing.T) {
	for _, link := range []string{".codex", filepath.Join(".agents", "plugins")} {
		t.Run(link, func(t *testing.T) {
			workDir := t.TempDir()
			t.Chdir(workDir)
			external := t.TempDir()
			writeTestFile(t, filepath.Join(external, "keep.txt"), "untouched")
			if err := os.MkdirAll(filepath.Dir(filepath.Join(workDir, link)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(external, filepath.Join(workDir, link)); err != nil {
				t.Skipf("symlink unsupported: %v", err)
			}

			deps, out := newPluginDeps(t.TempDir())
			code := command.ExecuteForCode(command.NewRootCommand(deps),
				"--plain", "--server", newPluginRegistry(t).URL,
				"plugins", "install", "--project-local", "--agent", "codex",
			)
			if code != output.ExitOK {
				t.Fatalf("install read unrelated %s path: %s", link, out.String())
			}
			if body, err := os.ReadFile(filepath.Join(external, "keep.txt")); err != nil || string(body) != "untouched" {
				t.Fatalf("external path changed: body=%q err=%v", body, err)
			}
		})
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
	if _, err := os.Stat(filepath.Join(workDir, ".agents", "skills", "specgate", "SKILL.md")); err != nil {
		t.Fatalf("Codex project skill missing after install: %v", err)
	}
}

func TestPluginsInstallRefusesToOverwriteUnownedCursorSkill(t *testing.T) {
	srv := newPluginRegistry(t)
	home := t.TempDir()
	foreign := filepath.Join(home, ".cursor", "skills", "specgate", "SKILL.md")
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

func TestPluginsInstallReportsProjectSkillsSHBootstrapConflictForEachIDE(t *testing.T) {
	for _, testCase := range []struct {
		agent     string
		skillPath string
	}{
		{agent: "codex", skillPath: filepath.Join(".agents", "skills", "specgate", "SKILL.md")},
		{agent: "claude", skillPath: filepath.Join(".claude", "skills", "specgate", "SKILL.md")},
		{agent: "cursor", skillPath: filepath.Join(".agents", "skills", "specgate", "SKILL.md")},
	} {
		t.Run(testCase.agent, func(t *testing.T) {
			workDir := t.TempDir()
			t.Chdir(workDir)
			home := t.TempDir()
			writeSkillsSHBootstrap(t, workDir, testCase.skillPath, false, "thanhtung2693/specgate")

			deps, out := newPluginDeps(home)
			code := command.ExecuteForCode(command.NewRootCommand(deps),
				"--json", "--no-input", "--server", newPluginRegistry(t).URL,
				"plugins", "install", "--agent", testCase.agent, "--project-local",
			)
			if code != output.ExitConflict {
				t.Fatalf("install exit = %d, want conflict; output=%s", code, out.String())
			}

			var env struct {
				OK    bool `json:"ok"`
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
					Details struct {
						Conflicts []struct {
							Scope         string `json:"scope"`
							Path          string `json:"path"`
							RemoveCommand string `json:"remove_command"`
						} `json:"conflicts"`
						RetryCommand string `json:"retry_command"`
					} `json:"details"`
				} `json:"error"`
			}
			if err := json.Unmarshal(out.Bytes(), &env); err != nil {
				t.Fatalf("unmarshal: %v, output=%s", err, out.String())
			}
			if env.OK || env.Error.Code != "conflict" || len(env.Error.Details.Conflicts) != 1 {
				t.Fatalf("unexpected conflict envelope: %s", out.String())
			}
			conflict := env.Error.Details.Conflicts[0]
			if conflict.Scope != "project" || conflict.Path != testCase.skillPath || conflict.RemoveCommand != "npx skills remove specgate -y" {
				t.Fatalf("unexpected conflict details: %+v", conflict)
			}
			wantRetry := "specgate plugins install --agent " + testCase.agent + " --project-local --no-input"
			if env.Error.Details.RetryCommand != wantRetry {
				t.Fatalf("retry command = %q, want %q", env.Error.Details.RetryCommand, wantRetry)
			}
			if _, err := os.Stat(filepath.Join(workDir, filepath.Dir(testCase.skillPath), ".specgate-owned")); !os.IsNotExist(err) {
				t.Fatalf("installer claimed the skills.sh directory before failing: %v", err)
			}
			if _, err := os.Stat(filepath.Join(home, ".codex", "plugins", "specgate")); !os.IsNotExist(err) {
				t.Fatalf("installer wrote global files before failing: %v", err)
			}
		})
	}
}

func TestPluginsInstallReportsGlobalSkillsSHBootstrapConflictBeforeWriting(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	home := t.TempDir()
	writeSkillsSHBootstrap(t, home, filepath.Join(".agents", "skills", "specgate", "SKILL.md"), true, "thanhtung2693/specgate")

	deps, out := newPluginDeps(home)
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "--no-input", "--server", newPluginRegistry(t).URL,
		"plugins", "install", "--agent", "codex",
	)
	if code != output.ExitConflict {
		t.Fatalf("install exit = %d, want conflict; output=%s", code, out.String())
	}
	for _, want := range []string{
		`"scope":"global"`,
		`"remove_command":"npx skills remove specgate -g -y"`,
		`"retry_command":"specgate plugins install --agent codex --no-input"`,
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("conflict output missing %q: %s", want, out.String())
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "plugins", "specgate")); !os.IsNotExist(err) {
		t.Fatalf("installer wrote plugin before failing: %v", err)
	}
}

func TestPluginsInstallReportsCrossScopeSkillsSHBootstrapConflict(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	home := t.TempDir()
	writeSkillsSHBootstrap(t, workDir, filepath.Join(".agents", "skills", "specgate", "SKILL.md"), false, "thanhtung2693/specgate")

	deps, out := newPluginDeps(home)
	var stderr bytes.Buffer
	deps.Stderr = &stderr
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--server", newPluginRegistry(t).URL,
		"plugins", "install", "--agent", "codex",
	)
	if code != output.ExitConflict {
		t.Fatalf("install exit = %d, want conflict; output=%s", code, out.String())
	}
	if !strings.Contains(stderr.String(), "npx skills remove specgate -y") || !strings.Contains(stderr.String(), "specgate plugins install --agent codex --no-input") {
		t.Fatalf("plain conflict lacks exact recovery commands: stdout=%q stderr=%q", out.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "plugins", "specgate")); !os.IsNotExist(err) {
		t.Fatalf("cross-scope conflict wrote plugin before failing: %v", err)
	}
}

func TestPluginsInstallDoesNotTrustForeignSkillsSHLock(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	foreign := filepath.Join(".agents", "skills", "specgate", "SKILL.md")
	writeSkillsSHBootstrap(t, workDir, foreign, false, "someone-else/specgate")

	deps, out := newPluginDeps(t.TempDir())
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "--no-input", "--server", newPluginRegistry(t).URL,
		"plugins", "install", "--agent", "codex", "--project-local",
	)
	if code != output.ExitUsage {
		t.Fatalf("install exit = %d, want validation failure; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"code":"validation_failed"`) || strings.Contains(out.String(), "npx skills remove") {
		t.Fatalf("foreign lock received trusted migration advice: %s", out.String())
	}
	body, err := os.ReadFile(foreign)
	if err != nil || string(body) != "# skills.sh SpecGate bootstrap\n" {
		t.Fatalf("foreign skill changed: body=%q err=%v", body, err)
	}
}

func TestPluginsInstallSucceedsAfterSkillsSHBootstrapIsRemoved(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	home := t.TempDir()
	skillPath := filepath.Join(".agents", "skills", "specgate")
	writeSkillsSHBootstrap(t, workDir, filepath.Join(skillPath, "SKILL.md"), false, "thanhtung2693/specgate")
	if err := os.RemoveAll(skillPath); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(workDir, "skills-lock.json"), `{"version":1,"skills":{}}`)

	deps, out := newPluginDeps(home)
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--server", newPluginRegistry(t).URL,
		"plugins", "install", "--agent", "codex", "--project-local",
	)
	if code != output.ExitOK {
		t.Fatalf("install after bootstrap removal exit = %d; output=%s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(workDir, skillPath, ".specgate-owned")); err != nil {
		t.Fatalf("CLI-managed root skill missing after migration: %v", err)
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

func TestPluginsDoctorDoesNotWarnWhenSelectedAgentExecutableIsUnavailable(t *testing.T) {
	for _, agentName := range []string{"codex", "claude", "cursor"} {
		t.Run(agentName, func(t *testing.T) {
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
				"--json", "plugins", "install", "--project-local", "--agent", agentName,
			)
			if code != output.ExitOK {
				t.Fatalf("install exit = %d, output = %s", code, out.String())
			}

			deps, out = newPluginDeps(homeDir)
			deps.ConfigPath = configPath
			code = command.ExecuteForCode(command.NewRootCommand(deps),
				"--json", "plugins", "doctor", "--project-local", "--agent", agentName,
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
			if agent.InstalledVersion != agent.LatestVersion {
				t.Fatalf("plugin versions do not match: %+v", agent)
			}
			if agent.NeedsUpdate {
				t.Fatalf("missing executable incorrectly requires a plugin update: %s", out.String())
			}
			if len(agent.Warnings) != 0 {
				t.Fatalf("selected agent should not require a host executable: %s", out.String())
			}
		})
	}
}

func TestPluginsDoctorDetectsStaleDirectFileInstalls(t *testing.T) {
	for _, agentName := range []string{"codex", "claude", "cursor"} {
		t.Run(agentName, func(t *testing.T) {
			srv := newPluginRegistry(t)
			workDir := t.TempDir()
			t.Chdir(workDir)
			homeDir := t.TempDir()

			deps, out := newPluginDeps(homeDir)
			if code := command.ExecuteForCode(command.NewRootCommand(deps),
				"--json", "--server", srv.URL,
				"plugins", "install", "--project-local", "--agent", agentName,
			); code != output.ExitOK {
				t.Fatalf("install exit = %d, output = %s", code, out.String())
			}

			var markerRoot string
			switch agentName {
			case "codex":
				markerRoot = filepath.Join(workDir, ".agents", "skills")
			case "claude":
				markerRoot = filepath.Join(workDir, ".claude", "skills")
			case "cursor":
				markerRoot = filepath.Join(workDir, ".cursor", "skills")
			}
			for _, skill := range []string{"specgate", "specgate-project-setup", "specgate-work-preparation", "specgate-work-delivery"} {
				marker := filepath.Join(markerRoot, skill, ".specgate-owned")
				if err := os.WriteFile(marker, []byte("specgate-plugin-v1\nversion=0.0.9\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if agentName == "cursor" {
				marker := filepath.Join(workDir, ".cursor", "rules", "using-specgate.mdc.specgate-owned")
				if err := os.WriteFile(marker, []byte("specgate-plugin-v1\nversion=0.0.9\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			deps, out = newPluginDeps(homeDir)
			code := command.ExecuteForCode(command.NewRootCommand(deps),
				"--json", "--server", srv.URL,
				"plugins", "doctor", "--project-local", "--agent", agentName,
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
			if agent.InstalledVersion != "0.0.9" || agent.LatestVersion != "0.1.0" || !agent.NeedsUpdate {
				t.Fatalf("stale direct install was not detected: %+v", agent)
			}
			warnings := strings.Join(agent.Warnings, "\n")
			if !strings.Contains(warnings, "does not match latest 0.1.0") {
				t.Fatalf("version mismatch warning is inaccurate: %s", out.String())
			}
			if !strings.Contains(warnings, "plugins install --agent "+agentName+" --project-local") {
				t.Fatalf("stale direct install warning lacks repair command: %s", out.String())
			}
		})
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

func TestPluginsDoctorProjectLocalRequiresFocusedCodexSkill(t *testing.T) {
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
	skill := filepath.Join(workDir, ".agents", "skills", "specgate", "SKILL.md")
	if err := os.Remove(skill); err != nil {
		t.Fatal(err)
	}

	deps, out = newPluginDeps(homeDir)
	code = command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "--server", srv.URL,
		"plugins", "doctor",
		"--project-local",
		"--agent", "codex",
	)
	if code != output.ExitUnavailable {
		t.Fatalf("doctor exit = %d, want unavailable; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), ".agents/skills/specgate/SKILL.md") || !strings.Contains(out.String(), "specgate plugins install --agent codex --project-local") {
		t.Fatalf("doctor output missing focused skill or repair command: %s", out.String())
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
		"specgate",
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
	if _, err := os.Stat(filepath.Join(workDir, ".claude", "skills", "specgate", "SKILL.md")); err != nil {
		t.Fatalf("Claude project skill missing after registry install: %v\n%s", err, out.String())
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
  "skills": ["specgate", "../../outside"],
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
		filepath.Join(home, ".codex", "plugins", "cache", "personal", "specgate", "0.1.0", "skills", "specgate", "SKILL.md"),
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
	if _, err := os.Stat(filepath.Join(workDir, ".agents", "skills", "specgate", "SKILL.md")); err != nil {
		t.Fatalf("project-local Codex skill missing: %v", err)
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

func TestPluginsInstallJSONDryRunReportsPlannedOperations(t *testing.T) {
	srv := newPluginRegistry(t)
	workDir := t.TempDir()
	t.Chdir(workDir)

	deps, out := newPluginDeps(t.TempDir())
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "--server", srv.URL,
		"plugins", "install",
		"--agent", "codex",
		"--project-local",
		"--dry-run",
	)
	if code != output.ExitOK {
		t.Fatalf("dry-run exit = %d, output = %s", code, out.String())
	}
	var env struct {
		Data struct {
			DryRun            bool     `json:"dry_run"`
			WrittenCount      int      `json:"written_count"`
			PlannedOperations []string `json:"planned_operations"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v, output=%s", err, out.String())
	}
	if !env.Data.DryRun || env.Data.WrittenCount != 0 {
		t.Fatalf("unexpected dry-run result: %+v", env.Data)
	}
	if len(env.Data.PlannedOperations) == 0 {
		t.Fatalf("JSON dry-run omitted planned operations: %s", out.String())
	}
	want := "write " + filepath.Join(".agents", "skills", "specgate", "SKILL.md")
	if !strings.Contains(strings.Join(env.Data.PlannedOperations, "\n"), want) {
		t.Fatalf("planned operations missing %q: %#v", want, env.Data.PlannedOperations)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".agents")); !os.IsNotExist(err) {
		t.Fatalf("JSON dry-run wrote project files: %v", err)
	}
}

func TestPluginsInstallProjectLocalLeavesUnrelatedMalformedCodexConfigUntouched(t *testing.T) {
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
			if code != output.ExitOK {
				t.Fatalf("project-local skill install read unrelated Codex config: %s", out.String())
			}
			if _, err := os.Stat(filepath.Join(workDir, ".agents", "skills", "specgate", "SKILL.md")); dryRun && !os.IsNotExist(err) {
				t.Fatalf("dry-run wrote focused skill: %v", err)
			} else if !dryRun && err != nil {
				t.Fatalf("focused skill missing: %v", err)
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

func writeSkillsSHBootstrap(t *testing.T, root, skillPath string, global bool, source string) {
	t.Helper()
	writeTestFile(t, filepath.Join(root, skillPath), "# skills.sh SpecGate bootstrap\n")
	lockPath := filepath.Join(root, "skills-lock.json")
	if global {
		lockPath = filepath.Join(root, ".agents", ".skill-lock.json")
	}
	body := fmt.Sprintf(`{
  "version": 1,
  "skills": {
    "specgate": {
      "source": %q,
      "sourceType": "github",
      "skillPath": "plugins/skills/specgate/SKILL.md"
    }
  }
}`, source)
	writeTestFile(t, lockPath, body)
}

func newPluginRegistry(t *testing.T) *httptest.Server {
	t.Helper()
	skills := []string{
		"specgate",
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
  "skills": ["specgate", "specgate-project-setup", "specgate-work-preparation", "specgate-work-delivery"],
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
