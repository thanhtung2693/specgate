package command_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestUninstallKeepsDataByDefaultAndRemovesUserFiles(t *testing.T) {
	dir := t.TempDir()
	setupTestBundle(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "data.txt"), []byte("keep me"), 0644); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, ".cursor", "rules", "using-specgate.mdc"), "rule")
	writeTestFile(t, filepath.Join(home, ".cursor", "rules", "using-specgate.mdc.specgate-owned"), "specgate-plugin-v1\n")
	writeTestFile(t, filepath.Join(home, ".cursor", "skills", "specgate", "SKILL.md"), "skill")
	writeTestFile(t, filepath.Join(home, ".cursor", "skills", "specgate", ".specgate-owned"), "specgate-plugin-v1\n")
	writeTestFile(t, filepath.Join(home, ".cursor", "skills", "delivering-work", "SKILL.md"), "retired skill")
	writeTestFile(t, filepath.Join(home, ".cursor", "skills", "delivering-work", ".specgate-owned"), "specgate-plugin-v1\n")
	writeTestFile(t, filepath.Join(home, ".codex", "plugins", "specgate", ".codex-plugin", "plugin.json"), "{}")
	writeTestFile(t, filepath.Join(home, ".codex", "plugins", "specgate", ".specgate-owned"), "specgate-plugin-v1\n")
	writeTestFile(t, filepath.Join(home, ".codex", "plugins", "cache", "personal", "specgate", "v0.1.0", ".codex-plugin", "plugin.json"), "{}")
	writeTestFile(t, filepath.Join(home, ".codex", "plugins", "cache", "personal", "specgate", "v0.1.0", ".specgate-owned"), "specgate-plugin-v1\n")
	writeTestFile(t, filepath.Join(home, ".claude", "skills", "specgate", ".claude-plugin", "plugin.json"), "{}")
	writeTestFile(t, filepath.Join(home, ".claude", "skills", "specgate", ".specgate-owned"), "specgate-plugin-v1\n")
	writeTestFile(t, filepath.Join(home, ".codex", "config.toml"), "[plugins.\"specgate@personal\"]\nenabled = true\n\n[marketplaces.personal]\nsource_type = \"local\"\nsource = "+strconv.Quote(home)+"\n\n[tools]\nkeep = true\n")
	writeTestFile(t, filepath.Join(home, ".agents", "plugins", "marketplace.json"), `{
  "name": "personal",
  "plugins": [
    {"name": "specgate", "source": {"source": "local", "path": "./.codex/plugins/specgate"}},
    {"name": "other"}
  ]
}
`)

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{
		Server:        "http://localhost:18080",
		DeploymentDir: dir,
		CurrentUser:   config.CurrentUser{Username: "alpha"},
		Workspace:     config.CurrentWorkspace{Slug: "alpha"},
	}).SaveTo(cfgPath); err != nil {
		t.Fatal(err)
	}

	runner := &fakeDeployRunner{}
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath
	deps.UserHomeDir = func() (string, error) { return home, nil }
	deps.DeployRunner = runner
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "uninstall", "--dir", dir)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	gotCommands := strings.Join(runner.Commands, "\n")
	if !strings.Contains(gotCommands, "docker compose -f "+filepath.Join(dir, "compose.yml")+" down") {
		t.Fatalf("missing compose down command in:\n%s", gotCommands)
	}
	if strings.Contains(gotCommands, " down -v") {
		t.Fatalf("default uninstall must not remove volumes:\n%s", gotCommands)
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatalf("config file should be removed; stat err=%v", err)
	}
	for _, path := range []string{
		filepath.Join(home, ".cursor", "rules", "using-specgate.mdc"),
		filepath.Join(home, ".cursor", "skills", "specgate"),
		filepath.Join(home, ".cursor", "skills", "delivering-work"),
		filepath.Join(home, ".codex", "plugins", "specgate"),
		filepath.Join(home, ".codex", "plugins", "cache", "personal", "specgate"),
		filepath.Join(home, ".claude", "skills", "specgate"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s should be removed; stat err=%v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "data.txt")); err != nil {
		t.Fatalf("deployment data should be kept by default: %v", err)
	}
	configText, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(configText), "specgate@personal") ||
		!strings.Contains(string(configText), "[marketplaces.personal]") ||
		!strings.Contains(string(configText), "[tools]") {
		t.Fatalf("codex config not cleaned safely:\n%s", configText)
	}
	marketplace, err := os.ReadFile(filepath.Join(home, ".agents", "plugins", "marketplace.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(marketplace), `"name": "specgate"`) || !strings.Contains(string(marketplace), `"name": "other"`) {
		t.Fatalf("marketplace not cleaned safely:\n%s", marketplace)
	}
}

func TestUninstallRejectsUnmanagedComposeDirectoryBeforeDown(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yml")
	if err := os.WriteFile(composePath, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeDeployRunner{}
	deps, out := newTestDeps(t, "")
	deps.DeployRunner = runner

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "uninstall", "--dir", dir)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if len(runner.Commands) != 0 {
		t.Fatalf("Docker commands ran for unmanaged directory: %#v", runner.Commands)
	}
	if _, err := os.Stat(composePath); err != nil {
		t.Fatalf("unmanaged compose file changed: %v", err)
	}
}

func TestLocalUninstallPurgeDataRemovesSQLiteFilesWithoutDocker(t *testing.T) {
	stateDir := t.TempDir()
	for _, name := range []string{"state.db", "state.db-wal", "state.db-shm", "state.db-journal"} {
		if err := os.WriteFile(filepath.Join(stateDir, name), []byte("local state"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	userSpec := filepath.Join(stateDir, "workspace-spec.md")
	if err := os.WriteFile(userSpec, []byte("# User-owned specification\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(cfgPath); err != nil {
		t.Fatal(err)
	}
	runner := &fakeDeployRunner{}
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath
	deps.DeployRunner = runner
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "--yes", "uninstall", "--purge-data")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	for _, name := range []string{"state.db", "state.db-wal", "state.db-shm", "state.db-journal"} {
		if _, err := os.Stat(filepath.Join(stateDir, name)); !os.IsNotExist(err) {
			t.Fatalf("SpecGate SQLite file %s should be removed; stat err=%v", name, err)
		}
	}
	if _, err := os.Stat(userSpec); err != nil {
		t.Fatalf("user-owned file must be preserved; stat err=%v", err)
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatalf("config file should be removed; stat err=%v", err)
	}
	if len(runner.Commands) != 0 {
		t.Fatalf("Local uninstall must not run Docker: %#v", runner.Commands)
	}
}

func TestLocalUninstallPreflightsEverySQLitePathBeforePurge(t *testing.T) {
	stateDir := t.TempDir()
	statePath := filepath.Join(stateDir, "state.db")
	if err := os.WriteFile(statePath, []byte("local state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(stateDir, "state.db-wal"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(cfgPath); err != nil {
		t.Fatal(err)
	}
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "--yes", "uninstall", "--purge-data")
	if code == output.ExitOK {
		t.Fatalf("unsafe journal path was accepted: %s", out.String())
	}
	if body, err := os.ReadFile(statePath); err != nil || string(body) != "local state" {
		t.Fatalf("state.db changed before purge preflight completed: body=%q err=%v", body, err)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("config was removed after failed purge preflight: %v", err)
	}
}

// fakeDeployRunner implements deploy.CommandRunner for command-layer tests.
func TestLocalUninstallRejectsFullOnlyDirWithoutRemovingAnything(t *testing.T) {
	for _, mode := range []string{"json", "plain"} {
		t.Run(mode, func(t *testing.T) {
			stateDir := t.TempDir()
			statePath := filepath.Join(stateDir, "state.db")
			if err := os.WriteFile(statePath, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			cfgPath := filepath.Join(t.TempDir(), "config.json")
			if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(cfgPath); err != nil {
				t.Fatal(err)
			}
			deps, out := newTestDeps(t, "")
			deps.ConfigPath = cfgPath
			var stderr bytes.Buffer
			deps.Stderr = &stderr
			args := []string{"--" + mode, "--no-input", "uninstall", "--dir", filepath.Join(t.TempDir(), "full")}

			code := command.ExecuteForCode(command.NewRootCommand(deps), args...)
			if code != output.ExitUsage {
				t.Fatalf("exit = %d, want usage; stdout=%s stderr=%s", code, out.String(), stderr.String())
			}
			message := out.String() + stderr.String()
			if !strings.Contains(message, "--dir") || !strings.Contains(message, "Full mode") {
				t.Fatalf("missing actionable mode error: %s", message)
			}
			if _, err := os.Stat(statePath); err != nil {
				t.Fatalf("Local state changed: %v", err)
			}
			if _, err := os.Stat(cfgPath); err != nil {
				t.Fatalf("config changed: %v", err)
			}
		})
	}
}

func TestUninstallRejectsMalformedConfigBeforeAnyMutation(t *testing.T) {
	for _, mode := range []string{"json", "plain"} {
		t.Run(mode, func(t *testing.T) {
			cfgPath := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(cfgPath, []byte("{broken"), 0o600); err != nil {
				t.Fatal(err)
			}
			deployDir := t.TempDir()
			setupTestBundle(t, deployDir)
			deploySentinel := filepath.Join(deployDir, "keep.txt")
			if err := os.WriteFile(deploySentinel, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			home := t.TempDir()
			pluginPath := filepath.Join(home, ".codex", "plugins", "specgate")
			writeTestFile(t, filepath.Join(pluginPath, ".codex-plugin", "plugin.json"), "{}")
			writeTestFile(t, filepath.Join(pluginPath, ".specgate-owned"), "specgate-plugin-v1\n")

			runner := &fakeDeployRunner{}
			deps, out := newTestDeps(t, "")
			deps.ConfigPath = cfgPath
			deps.DeployRunner = runner
			deps.UserHomeDir = func() (string, error) { return home, nil }
			var stderr bytes.Buffer
			deps.Stderr = &stderr

			code := command.ExecuteForCode(
				command.NewRootCommand(deps),
				"--"+mode, "--no-input", "--yes",
				"uninstall", "--dir", deployDir, "--purge-data",
			)
			if code != output.ExitUnavailable {
				t.Fatalf("exit = %d, want unavailable; stdout=%s stderr=%s", code, out.String(), stderr.String())
			}
			if len(runner.Commands) != 0 {
				t.Fatalf("malformed config reached deployment routing: %v", runner.Commands)
			}
			for _, path := range []string{cfgPath, deploySentinel, filepath.Join(pluginPath, ".specgate-owned")} {
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("malformed config mutated %s: %v", path, err)
				}
			}
			if !strings.Contains(out.String()+stderr.String(), "invalid character") {
				t.Fatalf("missing config parse error: stdout=%s stderr=%s", out.String(), stderr.String())
			}
		})
	}
}

func TestLocalUninstallPurgeDataRemovesEmptySpecGateParent(t *testing.T) {
	repo := t.TempDir()
	stateDir := filepath.Join(repo, ".specgate", "local")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "state.db"), []byte("local state"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(cfgPath); err != nil {
		t.Fatal(err)
	}
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "--yes", "uninstall", "--purge-data")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(repo, ".specgate")); !os.IsNotExist(err) {
		t.Fatalf("empty .specgate parent should be removed; stat err=%v", err)
	}
}

func TestLocalUninstallPurgeDataRejectsSymlinkedStateDirectory(t *testing.T) {
	external := t.TempDir()
	stateFile := filepath.Join(external, "state.db")
	if err := os.WriteFile(stateFile, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(t.TempDir(), "local-state")
	if err := os.Symlink(external, stateDir); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(cfgPath); err != nil {
		t.Fatal(err)
	}
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "--yes", "uninstall", "--purge-data")
	if code == output.ExitOK {
		t.Fatalf("symlinked state directory was purged: %s", out.String())
	}
	if body, err := os.ReadFile(stateFile); err != nil || string(body) != "keep" {
		t.Fatalf("symlink target changed: body=%q err=%v", body, err)
	}
}

func TestLocalUninstallPurgeDataRejectsSymlinkedStateAncestor(t *testing.T) {
	externalParent := t.TempDir()
	externalState := filepath.Join(externalParent, "local")
	if err := os.MkdirAll(externalState, 0o700); err != nil {
		t.Fatal(err)
	}
	stateFile := filepath.Join(externalState, "state.db")
	if err := os.WriteFile(stateFile, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkParent := filepath.Join(t.TempDir(), "linked-state")
	if err := os.Symlink(externalParent, linkParent); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(linkParent, "local")
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(cfgPath); err != nil {
		t.Fatal(err)
	}
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "--yes", "uninstall", "--purge-data")
	if code == output.ExitOK {
		t.Fatalf("state behind a symlinked ancestor was purged: %s", out.String())
	}
	if body, err := os.ReadFile(stateFile); err != nil || string(body) != "keep" {
		t.Fatalf("symlink target changed: body=%q err=%v", body, err)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("config was removed after unsafe state path: %v", err)
	}
}

func TestLocalUninstallRejectsSymlinkedConfigAncestorBeforePluginRemoval(t *testing.T) {
	stateDir := t.TempDir()
	realConfigParent := t.TempDir()
	realConfig := filepath.Join(realConfigParent, "config.json")
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(realConfig); err != nil {
		t.Fatal(err)
	}
	linkParent := filepath.Join(t.TempDir(), "linked-config")
	if err := os.Symlink(realConfigParent, linkParent); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	pluginMarker := filepath.Join(home, ".codex", "plugins", "specgate", ".specgate-owned")
	writeTestFile(t, pluginMarker, "specgate-plugin-v1\n")
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = filepath.Join(linkParent, "config.json")
	deps.UserHomeDir = func() (string, error) { return home, nil }

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "uninstall")
	if code == output.ExitOK {
		t.Fatalf("uninstall accepted config behind symlinked ancestor: %s", out.String())
	}
	for _, path := range []string{realConfig, pluginMarker} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unsafe config path caused mutation of %s: %v", path, err)
		}
	}
}

func TestLocalUninstallPurgeDataWarnsBeforeDeletingSQLiteFiles(t *testing.T) {
	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, "state.db"), []byte("local state"), 0600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(cfgPath); err != nil {
		t.Fatal(err)
	}
	deps, _ := newTestDeps(t, "")
	deps.ConfigPath = cfgPath
	var stderr bytes.Buffer
	deps.Stderr = &stderr

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "--yes", "uninstall", "--purge-data")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Warning: Local purge removes only SpecGate SQLite files") || !strings.Contains(stderr.String(), stateDir) {
		t.Fatalf("missing Local purge warning:\n%s", stderr.String())
	}
}

func TestUninstallRemovesSpecGateOnlyPluginConfigFiles(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, ".codex", "config.toml")
	marketplacePath := filepath.Join(home, ".agents", "plugins", "marketplace.json")
	writeTestFile(t, configPath, "[plugins.\"specgate@personal\"]\nenabled = true\n\n[marketplaces.personal]\nsource_type = \"local\"\nsource = "+strconv.Quote(home)+"\n")
	writeTestFile(t, marketplacePath, `{
  "name": "personal",
  "plugins": [
    {"name": "specgate", "source": {"source": "local", "path": "./.codex/plugins/specgate"}}
  ]
}
`)
	writeTestFile(t, filepath.Join(home, ".codex", "plugins", "specgate", ".codex-plugin", "plugin.json"), "{}")
	writeTestFile(t, filepath.Join(home, ".codex", "plugins", "specgate", ".specgate-owned"), "specgate-plugin-v1\n")

	deps, out := newTestDeps(t, "")
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "uninstall", "--dir", filepath.Join(t.TempDir(), "deploy"))
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	for _, path := range []string{
		configPath,
		marketplacePath,
		filepath.Join(home, ".codex", "plugins"),
		filepath.Join(home, ".agents", "plugins"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s should be removed; stat err=%v", path, err)
		}
	}
}

func TestUninstallReportsUnownedFilesPreservedInsideManagedPluginDirectory(t *testing.T) {
	home := t.TempDir()
	pluginRoot := filepath.Join(home, ".codex", "plugins", "specgate")
	manifest := filepath.Join(pluginRoot, ".codex-plugin", "plugin.json")
	userFile := filepath.Join(pluginRoot, "notes.txt")
	writeTestFile(t, manifest, "{}")
	writeTestFile(t, filepath.Join(pluginRoot, ".specgate-owned"), "specgate-plugin-v1\n")
	writeTestFile(t, userFile, "user-owned")

	deps, out := newTestDeps(t, "")
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "uninstall", "--dir", filepath.Join(t.TempDir(), "deploy"))
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if body, err := os.ReadFile(userFile); err != nil || string(body) != "user-owned" {
		t.Fatalf("unowned plugin file changed: body=%q err=%v", body, err)
	}
	if _, err := os.Stat(manifest); !os.IsNotExist(err) {
		t.Fatalf("managed manifest remains: %v", err)
	}
	if !strings.Contains(out.String(), `"preserved_paths":["`+pluginRoot+`"]`) {
		t.Fatalf("unowned plugin directory was not reported: %s", out.String())
	}
}

func TestUninstallCleansManagedCodexConfigWhenPluginDirectoryIsMissing(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, ".codex", "config.toml")
	marketplacePath := filepath.Join(home, ".agents", "plugins", "marketplace.json")
	writeTestFile(t, configPath, "[plugins.\"specgate@personal\"]\nenabled = true\n\n[marketplaces.personal]\nsource_type = \"local\"\nsource = "+strconv.Quote(home)+"\n\n[tools]\nkeep = true\n")
	writeTestFile(t, marketplacePath, `{
  "name": "personal",
  "plugins": [
    {"name": "specgate", "source": {"source": "local", "path": "./.codex/plugins/specgate"}}
  ]
}
`)

	deps, out := newTestDeps(t, "")
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "uninstall", "--dir", filepath.Join(t.TempDir(), "deploy"))
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	configBody, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(configBody), "specgate@personal") ||
		strings.Contains(string(configBody), "[marketplaces.personal]") ||
		!strings.Contains(string(configBody), "[tools]") {
		t.Fatalf("managed Codex config was not cleaned safely:\n%s", configBody)
	}
	if _, err := os.Stat(marketplacePath); !os.IsNotExist(err) {
		t.Fatalf("managed marketplace should be removed; stat err=%v", err)
	}
}

func TestUninstallKeepsUnownedCodexMarketplaceAndConfig(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, ".codex", "config.toml")
	marketplacePath := filepath.Join(home, ".agents", "plugins", "marketplace.json")
	configBody := "[plugins.\"specgate@personal\"]\nenabled = true\n"
	marketplaceBody := `{"name":"personal","plugins":[{"name":"specgate","source":{"source":"local","path":"/custom/specgate"}}]}`
	writeTestFile(t, configPath, configBody)
	writeTestFile(t, marketplacePath, marketplaceBody)
	writeTestFile(t, filepath.Join(home, ".codex", "plugins", "specgate", ".codex-plugin", "plugin.json"), "{}")
	writeTestFile(t, filepath.Join(home, ".codex", "plugins", "specgate", ".specgate-owned"), "specgate-plugin-v1\n")

	deps, out := newTestDeps(t, "")
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "uninstall", "--dir", filepath.Join(t.TempDir(), "deploy"))
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	for path, want := range map[string]string{configPath: configBody, marketplacePath: marketplaceBody} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != want {
			t.Fatalf("unowned shared file %s changed:\n%s", path, body)
		}
	}
}

func TestUninstallPreservesEmptySharedMarketplaceFile(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, ".codex", "config.toml")
	marketplacePath := filepath.Join(home, ".agents", "plugins", "marketplace.json")
	writeTestFile(t, configPath, "")
	writeTestFile(t, marketplacePath, `{"name":"personal","plugins":[]}`)
	writeTestFile(t, filepath.Join(home, ".codex", "plugins", "specgate", ".specgate-owned"), "specgate-plugin-v1\n")

	deps, out := newTestDeps(t, "")
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "uninstall", "--dir", filepath.Join(t.TempDir(), "deploy"))
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("empty managed config should be removed; stat err=%v", err)
	}
	body, err := os.ReadFile(marketplacePath)
	if err != nil {
		t.Fatalf("empty shared marketplace should be preserved: %v", err)
	}
	if string(body) != `{"name":"personal","plugins":[]}` {
		t.Fatalf("empty shared marketplace changed: %s", body)
	}
}

func TestUninstallPurgeDataRequiresConfirmation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)

	deps, out := newTestDeps(t, "")
	deps.DeployRunner = &fakeDeployRunner{}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "uninstall", "--dir", dir, "--purge-data")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want %d; output = %s", code, output.ExitUsage, out.String())
	}
}

func TestUninstallPurgeDataRejectsUnmanagedDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	userSpec := filepath.Join(dir, "user-spec.md")
	if err := os.WriteFile(userSpec, []byte("# Keep me\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeDeployRunner{}
	deps, out := newTestDeps(t, "")
	deps.DeployRunner = runner

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "uninstall", "--dir", dir, "--purge-data")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if body, err := os.ReadFile(userSpec); err != nil || string(body) != "# Keep me\n" {
		t.Fatalf("user file changed: body=%q err=%v", body, err)
	}
	if len(runner.Commands) != 0 {
		t.Fatalf("Docker commands ran for unmanaged directory: %#v", runner.Commands)
	}
}

func TestUninstallPlainPurgeDataRequiresYes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)

	runner := &fakeDeployRunner{}
	deps, out := newTestDeps(t, "")
	deps.DeployRunner = runner
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "uninstall", "--dir", dir, "--purge-data")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want %d; output = %s", code, output.ExitUsage, out.String())
	}
	if len(runner.Commands) != 0 {
		t.Fatalf("commands ran despite missing --yes: %#v", runner.Commands)
	}
}

func TestUninstallPurgeDataRemovesDeploymentDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "data.txt"), []byte("delete me"), 0644); err != nil {
		t.Fatal(err)
	}

	runner := &fakeDeployRunner{}
	deps, out := newTestDeps(t, "")
	deps.DeployRunner = runner
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "uninstall", "--dir", dir, "--purge-data")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	gotCommands := strings.Join(runner.Commands, "\n")
	if !strings.Contains(gotCommands, "docker compose -f "+filepath.Join(dir, "compose.yml")+" down -v") {
		t.Fatalf("missing compose down -v command in:\n%s", gotCommands)
	}
	if strings.Contains(gotCommands, "config --images") || strings.Contains(gotCommands, "docker image rm") {
		t.Fatalf("--purge-data must not inspect or remove images:\n%s", gotCommands)
	}
	for _, want := range []string{
		"docker container ls -q --filter label=org.specgate.managed=true --filter label=org.specgate.project=specgate",
		"docker volume ls -q --filter label=org.specgate.managed=true --filter label=org.specgate.project=specgate",
		"docker network ls -q --filter label=org.specgate.managed=true --filter label=org.specgate.project=specgate",
	} {
		if !strings.Contains(gotCommands, want) {
			t.Fatalf("missing labeled cleanup command %q in:\n%s", want, gotCommands)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("deployment dir should be removed with --purge-data; stat err=%v", err)
	}
}

func TestUninstallPurgeDataScopesLabeledCleanupToComposeProject(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SPECGATE_COMPOSE_PROJECT=alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runner := &fakeDeployRunner{OutputByCommand: map[string][]byte{
		"docker compose -f " + filepath.Join(dir, "compose.yml") + " config --images": []byte("ghcr.io/thanhtung2693/agents:v9.9.0-rc.1\n"),
	}}
	deps, out := newTestDeps(t, "")
	deps.DeployRunner = runner
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "uninstall", "--dir", dir, "--purge-data")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	gotCommands := strings.Join(runner.Commands, "\n")
	for _, want := range []string{
		"docker container ls -q --filter label=org.specgate.managed=true --filter label=org.specgate.project=alpha",
		"docker volume ls -q --filter label=org.specgate.managed=true --filter label=org.specgate.project=alpha",
		"docker network ls -q --filter label=org.specgate.managed=true --filter label=org.specgate.project=alpha",
	} {
		if !strings.Contains(gotCommands, want) {
			t.Fatalf("missing project-scoped cleanup command %q in:\n%s", want, gotCommands)
		}
	}
}

func TestUninstallPlainPurgeDataKeepsImages(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)

	runner := &fakeDeployRunner{}
	deps, out := newTestDeps(t, "")
	deps.DeployRunner = runner
	var stderr bytes.Buffer
	deps.Stderr = &stderr
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--yes", "uninstall", "--dir", dir, "--purge-data")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(stderr.String(), "Warning: Full purge permanently removes SpecGate-managed Docker volumes") ||
		!strings.Contains(stderr.String(), dir) {
		t.Fatalf("missing Full purge warning:\n%s", stderr.String())
	}
	gotCommands := strings.Join(runner.Commands, "\n")
	if strings.Contains(gotCommands, "docker image rm") || strings.Contains(gotCommands, "config --images") {
		t.Fatalf("--purge-data must not inspect or remove images:\n%s", gotCommands)
	}
}

func TestUninstallChecklistCanKeepPluginsAndPurgeDataWithoutRemovingImages(t *testing.T) {
	dir := t.TempDir()
	setupTestBundle(t, dir)
	home := t.TempDir()
	pluginPath := filepath.Join(home, ".cursor", "rules", "using-specgate.mdc")
	writeTestFile(t, pluginPath, "rule")

	runner := &fakeDeployRunner{}
	deps, _, prompter, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.UserHomeDir = func() (string, error) { return home, nil }
	deps.DeployRunner = runner
	prompter.multiValues = []string{"data"}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "uninstall", "--dir", dir)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if prompter.multiTitle != "Remove SpecGate setup" {
		t.Fatalf("multi-select title = %q", prompter.multiTitle)
	}
	if _, err := os.Stat(pluginPath); err != nil {
		t.Fatalf("plugin file should be kept when unchecked: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("deployment dir should be removed when data checked; stat err=%v", err)
	}
	gotCommands := strings.Join(runner.Commands, "\n")
	if !strings.Contains(gotCommands, "docker compose -f "+filepath.Join(dir, "compose.yml")+" down -v") {
		t.Fatalf("missing compose down -v command in:\n%s", gotCommands)
	}
	if strings.Contains(gotCommands, "config --images") || strings.Contains(gotCommands, "docker image rm") {
		t.Fatalf("uninstall must not inspect or remove shared Docker images:\n%s", gotCommands)
	}
}
