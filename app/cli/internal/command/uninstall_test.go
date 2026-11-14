package command_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

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
	runner := &fakeCommandRunner{}
	deps, out := newTestDeps(t)
	deps.ConfigPath = cfgPath
	deps.CommandRunner = runner
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
	deps, out := newTestDeps(t)
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

func TestUninstallRejectsMalformedConfigBeforeAnyMutation(t *testing.T) {
	for _, mode := range []string{"json", "plain"} {
		t.Run(mode, func(t *testing.T) {
			cfgPath := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(cfgPath, []byte("{broken"), 0o600); err != nil {
				t.Fatal(err)
			}
			deployDir := t.TempDir()

			deploySentinel := filepath.Join(deployDir, "keep.txt")
			if err := os.WriteFile(deploySentinel, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			home := t.TempDir()
			pluginPath := filepath.Join(home, ".codex", "plugins", "specgate")
			writeTestFile(t, filepath.Join(pluginPath, ".codex-plugin", "plugin.json"), "{}")
			writeTestFile(t, filepath.Join(pluginPath, ".specgate-owned"), "specgate-plugin-v1\n")

			runner := &fakeCommandRunner{}
			deps, out := newTestDeps(t)
			deps.ConfigPath = cfgPath
			deps.CommandRunner = runner
			deps.UserHomeDir = func() (string, error) { return home, nil }
			var stderr bytes.Buffer
			deps.Stderr = &stderr

			code := command.ExecuteForCode(
				command.NewRootCommand(deps),
				"--"+mode, "--no-input", "--yes",
				"uninstall", "--purge-data",
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
	deps, out := newTestDeps(t)
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
	deps, out := newTestDeps(t)
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
	deps, out := newTestDeps(t)
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
	deps, out := newTestDeps(t)
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
	deps, _ := newTestDeps(t)
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

func TestLocalUninstallPurgeRejectsConflictingSelections(t *testing.T) {
	for _, source := range []string{"SPECGATE_LOCAL_DIR", "current project"} {
		t.Run(source, func(t *testing.T) {
			deps, out := newTestDeps(t)
			home, err := deps.UserHomeDir()
			if err != nil {
				t.Fatal(err)
			}
			globalStore := t.TempDir()
			otherStore := t.TempDir()
			globalDB := filepath.Join(globalStore, "state.db")
			otherDB := filepath.Join(otherStore, "state.db")
			marker := filepath.Join(home, ".codex", "plugins", "specgate", ".specgate-owned")
			writeTestFile(t, globalDB, "global")
			writeTestFile(t, otherDB, "other")
			writeTestFile(t, marker, "specgate-plugin-v1\n")
			cfg := config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: globalStore}}
			t.Setenv("SPECGATE_LOCAL_DIR", "")
			switch source {
			case "SPECGATE_LOCAL_DIR":
				t.Setenv(source, otherStore)
			case "current project":
				if err := os.Mkdir(filepath.Join(deps.WorkingDir, ".git"), 0o700); err != nil {
					t.Fatal(err)
				}
				root, ok := config.FindProjectRoot(deps.WorkingDir)
				if !ok {
					t.Fatal("test project root missing")
				}
				cfg.Projects = map[string]config.ProjectConfig{root: {Local: config.LocalStore{Path: otherStore}}}
				// A matching environment override cannot hide a different project binding.
				t.Setenv("SPECGATE_LOCAL_DIR", globalStore)
			}
			if err := cfg.SaveTo(deps.ConfigPath); err != nil {
				t.Fatal(err)
			}

			code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "uninstall", "--purge-data")
			if code != output.ExitConflict {
				t.Errorf("exit = %d, want conflicting store refusal: %s", code, out.String())
			}
			var envelope struct {
				Error output.ErrorPayload `json:"error"`
			}
			if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{source, strconv.Quote(globalStore), strconv.Quote(otherStore)} {
				if !strings.Contains(envelope.Error.Message, want) {
					t.Errorf("refusal missing %q: %s", want, envelope.Error.Message)
				}
			}
			for _, path := range []string{globalDB, otherDB, marker, deps.ConfigPath} {
				if _, err := os.Stat(path); err != nil {
					t.Errorf("ambiguous purge changed %s: %v", path, err)
				}
			}
		})
	}
}

func TestLocalUninstallPurgeRefusesEnvironmentOnlySQLiteStore(t *testing.T) {
	deps, out := newTestDeps(t)
	home, err := deps.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	store := t.TempDir()
	t.Setenv("SPECGATE_LOCAL_DIR", store)
	dbPath := filepath.Join(store, "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("CREATE TABLE account_backup (name TEXT); INSERT INTO account_backup VALUES ('keep')")
	closeErr := db.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("create SQLite fixture: exec=%v close=%v", err, closeErr)
	}
	data, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(store, "unrelated.txt")
	marker := filepath.Join(home, ".codex", "plugins", "specgate", ".specgate-owned")
	writeTestFile(t, sentinel, "user data")
	writeTestFile(t, marker, "specgate-plugin-v1\n")

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "uninstall", "--purge-data")
	if code != output.ExitUsage || !strings.Contains(out.String(), "saved") {
		t.Errorf("expected an actionable usage error for the environment-only store: exit=%d output=%s", code, out)
	}
	if info, err := os.Stat(store); err != nil || !info.IsDir() {
		t.Errorf("store directory changed: %v", err)
	}
	for path, want := range map[string][]byte{dbPath: data, sentinel: []byte("user data"), marker: []byte("specgate-plugin-v1\n")} {
		if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, want) {
			t.Errorf("environment-only purge modified %s: %v", path, err)
		}
	}
	if _, err := os.Stat(deps.ConfigPath); !os.IsNotExist(err) {
		t.Errorf("unexpected config after refusal: %v", err)
	}
}

func TestLocalUninstallPurgeRequiresExplicitConsent(t *testing.T) {
	for _, tc := range []struct {
		name      string
		tty       bool
		confirm   bool
		flags     []string
		wantCode  int
		wantPurge bool
	}{
		{name: "redirected stdin", wantCode: output.ExitUsage},
		{name: "terminal decline", tty: true, wantCode: output.ExitOK},
		{name: "terminal confirm", tty: true, confirm: true, wantCode: output.ExitOK, wantPurge: true},
		{name: "terminal plus yes", tty: true, flags: []string{"--yes"}, wantCode: output.ExitOK, wantPurge: true},
		{name: "JSON plus no yes", tty: true, flags: []string{"--json"}, wantCode: output.ExitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps, prompt, out := newWorkflowTestDeps(t)
			deps.StdinIsTTY = func() bool { return tc.tty }
			prompt.confirmValue = tc.confirm
			store := t.TempDir()
			stateDB := filepath.Join(store, "state.db")
			writeTestFile(t, stateDB, "sqlite fixture")
			// Lexical aliases of one store do not create a conflict.
			t.Setenv("SPECGATE_LOCAL_DIR", store+string(os.PathSeparator)+".")
			cfg := config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: store}}
			if err := cfg.SaveTo(deps.ConfigPath); err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			deps.Stderr = &stderr

			args := append([]string{"uninstall", "--purge-data"}, tc.flags...)
			code := command.ExecuteForCode(command.NewRootCommand(deps), args...)
			if code != tc.wantCode {
				t.Errorf("exit = %d, want %d, stdout = %s, stderr = %s", code, tc.wantCode, out, &stderr)
			}
			if tc.tty && len(tc.flags) == 0 {
				if !strings.Contains(prompt.confirmTitle, store) {
					t.Errorf("confirmation must show the SQLite directory, got %q", prompt.confirmTitle)
				}
			} else if prompt.confirmTitle != "" {
				t.Errorf("unexpected confirmation: %q", prompt.confirmTitle)
			}
			for _, path := range []string{stateDB, deps.ConfigPath} {
				_, err := os.Stat(path)
				if (tc.wantPurge && !os.IsNotExist(err)) || (!tc.wantPurge && err != nil) {
					t.Errorf("path %s, purged = %t, stat err = %v", path, tc.wantPurge, err)
				}
			}
		})
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

	deps, out := newTestDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "uninstall")
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

	deps, out := newTestDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "uninstall")
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

	deps, out := newTestDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "uninstall")
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

	deps, out := newTestDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "uninstall")
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

	deps, out := newTestDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "uninstall")
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
