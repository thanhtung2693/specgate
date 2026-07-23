package command_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestLocalInitSuggestsAgentNeutralPluginInstall(t *testing.T) {
	deps, out := newTestDeps(t, "")
	stateDir := filepath.Join(t.TempDir(), "local")
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir,
		"--workspace-name", "Alpha", "--display-name", "Human", "--username", "human",
	)
	if code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), "specgate plugins install") {
		t.Fatalf("init output lacks generic plugin installer: %s", out.String())
	}
	if strings.Contains(out.String(), "Codex") || strings.Contains(out.String(), "--agent codex") {
		t.Fatalf("init output assumes a specific IDE: %s", out.String())
	}
}

func TestLocalInitJSONSuggestsAgentNeutralPluginInstall(t *testing.T) {
	deps, out := newTestDeps(t, "")
	stateDir := filepath.Join(t.TempDir(), "local")
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "--no-input", "init", "--mode", "local", "--local-dir", stateDir,
		"--workspace-name", "Alpha", "--display-name", "Human", "--username", "human",
	)
	if code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	var envelope struct {
		Data struct {
			Next string `json:"next"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Next != "specgate plugins install" {
		t.Fatalf("next = %q, want agent-neutral plugin installer; output=%s", envelope.Data.Next, out.String())
	}
}

func TestInteractiveInitPromptsForSetupMode(t *testing.T) {
	deps, _ := newTestDeps(t, "")
	t.Setenv("CI", "")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("COLUMNS", "80")
	var stderr bytes.Buffer
	deps.Stderr = &stderr
	deps.StdinIsTTY = func() bool { return true }
	deps.StdoutIsTTY = func() bool { return true }
	deps.StderrIsTTY = func() bool { return true }
	stateDir := filepath.Join(t.TempDir(), "local")
	deps.Prompter = &fakePrompter{
		selectedValue: "local",
		inputValues:   []string{"Alpha", "Human", "human", ""},
		selectObserver: func() {
			if !strings.Contains(stderr.String(), " ____  ____") {
				t.Fatalf("welcome was not written before setup-mode prompt: %q", stderr.String())
			}
		},
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "init", "--local-dir", stateDir); code != output.ExitOK {
		t.Fatalf("exit = %d", code)
	}
	fake := deps.Prompter.(*fakePrompter)
	if len(fake.selectOptions) != 2 || fake.selectOptions[0].Value != "local" || fake.selectOptions[1].Value != "full" {
		t.Fatalf("mode options = %#v", fake.selectOptions)
	}
	if !strings.Contains(fake.selectOptions[0].Label, "no Docker") || !strings.Contains(fake.selectOptions[1].Label, "team") {
		t.Fatalf("mode labels = %#v", fake.selectOptions)
	}
}

func TestLocalInitIgnoresRepositoryContainedStateBeforeCreatingIt(t *testing.T) {
	deps, out := newTestDeps(t, "")
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	deps.WorkingDir = repo
	canonicalRepo, ok := config.FindProjectRoot(repo)
	if !ok {
		t.Fatal("project root not found")
	}
	stateDir := filepath.Join(canonicalRepo, ".specgate", "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	ignore, err := os.ReadFile(filepath.Join(canonicalRepo, ".specgate", ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ignore), "*") || !strings.Contains(string(ignore), "!config") {
		t.Fatalf(".specgate/.gitignore = %q", ignore)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "state.db")); err != nil {
		t.Fatal(err)
	}
}

// fakeDeployRunner implements deploy.CommandRunner for command-layer tests.
func TestInitCmdJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath
	deps.DeployRunner = &fakeDeployRunner{}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "init", "--mode", "full", "--dir", dir)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	// Config should have the deployment dir persisted.
	cfg, err := config.LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DeploymentDir != dir {
		t.Errorf("deployment_dir = %q, want %q", cfg.DeploymentDir, dir)
	}
}

func TestInitRejectsMalformedConfigBeforeMutation(t *testing.T) {
	tests := []struct {
		name string
		mode string
		args []string
	}{
		{
			name: "local",
			mode: "local",
			args: []string{"--workspace-name", "Local", "--display-name", "Human", "--username", "human"},
		},
		{name: "full", mode: "full"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			cfgPath := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(cfgPath, []byte("{broken"), 0o600); err != nil {
				t.Fatal(err)
			}
			stateDir := filepath.Join(t.TempDir(), "local")
			deployDir := t.TempDir()
			setupTestBundle(t, deployDir)
			runner := &fakeDeployRunner{}
			deps, out := newTestDeps(t, "")
			deps.ConfigPath = cfgPath
			deps.DeployRunner = runner
			args := []string{"--json", "--no-input", "init", "--mode", testCase.mode}
			if testCase.mode == "local" {
				args = append(args, "--local-dir", stateDir)
			} else {
				args = append(args, "--dir", deployDir)
			}
			args = append(args, testCase.args...)

			code := command.ExecuteForCode(command.NewRootCommand(deps), args...)
			if code != output.ExitUnavailable {
				t.Fatalf("exit = %d, want unavailable; output = %s", code, out.String())
			}
			body, err := os.ReadFile(cfgPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != "{broken" {
				t.Fatalf("malformed config was replaced: %s", body)
			}
			if len(runner.Commands) != 0 {
				t.Fatalf("init mutated deployment before config validation: %v", runner.Commands)
			}
			if _, err := os.Stat(filepath.Join(stateDir, "state.db")); !os.IsNotExist(err) {
				t.Fatalf("Local database created before config validation; stat err=%v", err)
			}
		})
	}
}

func TestFullInitReportsConfigSaveFailure(t *testing.T) {
	deployDir := t.TempDir()
	setupTestBundle(t, deployDir)
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{}).SaveTo(cfgPath); err != nil {
		t.Fatal(err)
	}
	runner := &fakeDeployRunner{OnCommand: func() {
		if err := os.Remove(cfgPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(cfgPath, 0o700); err != nil {
			t.Fatal(err)
		}
	}}
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath
	deps.DeployRunner = runner

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "init", "--mode", "full", "--dir", deployDir)
	if code != output.ExitUnavailable {
		t.Fatalf("exit = %d, want unavailable; output = %s", code, out.String())
	}
}

func TestInitRejectsFlagsFromTheOtherModeBeforeMutation(t *testing.T) {
	localCases := [][]string{
		{"--dir", filepath.Join(t.TempDir(), "full")},
		{"--seed"},
		{"--no-seed"},
		{"--bundle-version", "v9.9.9"},
	}
	for _, extra := range localCases {
		t.Run("local_"+strings.TrimPrefix(extra[0], "--"), func(t *testing.T) {
			stateDir := filepath.Join(t.TempDir(), "local")
			deps, out := newTestDeps(t, "")
			runner := &fakeDeployRunner{}
			deps.DeployRunner = runner
			args := []string{
				"--json", "--no-input", "init", "--mode", "local",
				"--local-dir", stateDir,
				"--workspace-name", "Local",
				"--display-name", "Human",
				"--username", "human",
			}
			args = append(args, extra...)
			code := command.ExecuteForCode(command.NewRootCommand(deps), args...)
			if code != output.ExitUsage {
				t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
			}
			if _, err := os.Stat(filepath.Join(stateDir, "state.db")); !os.IsNotExist(err) {
				t.Fatalf("Local database created for incompatible flags; stat err=%v", err)
			}
			if len(runner.Commands) != 0 {
				t.Fatalf("incompatible Local flags ran deployment commands: %v", runner.Commands)
			}
		})
	}

	t.Run("full_local-dir", func(t *testing.T) {
		deployDir := t.TempDir()
		setupTestBundle(t, deployDir)
		deps, out := newTestDeps(t, "")
		runner := &fakeDeployRunner{}
		deps.DeployRunner = runner
		code := command.ExecuteForCode(
			command.NewRootCommand(deps),
			"--json", "--no-input", "init", "--mode", "full",
			"--dir", deployDir,
			"--local-dir", filepath.Join(t.TempDir(), "local"),
		)
		if code != output.ExitUsage {
			t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
		}
		if len(runner.Commands) != 0 {
			t.Fatalf("incompatible Full flags ran deployment commands: %v", runner.Commands)
		}
	})
}

func TestFullInitReplacesExistingLocalTopology(t *testing.T) {
	dir := t.TempDir()
	setupTestBundle(t, dir)
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.DeployRunner = &fakeDeployRunner{}
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: filepath.Join(t.TempDir(), "local")}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "init", "--mode", "full", "--dir", dir); code != output.ExitOK {
		t.Fatalf("full init exit = %d, output = %s", code, out.String())
	}
	cfg, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != config.ModeFull || cfg.Local != (config.LocalStore{}) || cfg.CurrentUser != (config.CurrentUser{}) || cfg.Workspace != (config.CurrentWorkspace{}) || len(cfg.Projects) != 0 {
		t.Fatalf("Full config retained Local state: %#v", cfg)
	}
}

func TestLocalInitReplacesExistingFullTopology(t *testing.T) {
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{
		Mode:          config.ModeFull,
		Server:        "https://specgate.example/api/doc-registry",
		DeploymentDir: filepath.Join(t.TempDir(), "deploy"),
		CurrentUser:   config.CurrentUser{ID: "full-user", Username: "full-user"},
		Workspace:     config.CurrentWorkspace{ID: "full-workspace", Slug: "full"},
		Projects:      map[string]config.ProjectConfig{t.TempDir(): {Workspace: config.CurrentWorkspace{ID: "full-workspace", Slug: "full"}}},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Local", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("Local init exit = %d, output = %s", code, out.String())
	}
	cfg, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != config.ModeLocal || cfg.Server != "" || cfg.DeploymentDir != "" || len(cfg.Projects) != 0 {
		t.Fatalf("Local config retained Full state: %#v", cfg)
	}
}

func TestInitLocalCreatesStateWithoutDocker(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	runner := &fakeDeployRunner{}
	deps.DeployRunner = runner

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Offline dogfood", "--display-name", "Dogfood Human", "--username", "dogfood-human", "--email", "human@example.com")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if len(runner.Commands) != 0 {
		t.Fatalf("local init invoked docker: %#v", runner.Commands)
	}
	if deps.Client != nil {
		t.Fatal("local init constructed an HTTP client")
	}
	cfg, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != config.ModeLocal || cfg.Local.Path != stateDir || cfg.Local.ID == "" {
		t.Fatalf("config = %#v", cfg)
	}
	if cfg.CurrentUser.Email != "human@example.com" {
		t.Fatalf("email = %q", cfg.CurrentUser.Email)
	}
}

func TestInitLocalCanInstallEmbeddedCodexPlugin(t *testing.T) {
	deps, out := newTestDeps(t, "")
	homeDir := t.TempDir()
	deps.UserHomeDir = func() (string, error) { return homeDir, nil }
	stateDir := filepath.Join(t.TempDir(), "local")

	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "--no-input",
		"init", "--mode", "local", "--local-dir", stateDir,
		"--workspace-name", "Offline dogfood", "--display-name", "Dogfood Human", "--username", "dogfood-human",
		"--install-plugins", "--plugin-agent", "codex",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".codex", "plugins", "specgate", "skills", "specgate", "SKILL.md")); err != nil {
		t.Fatalf("embedded Codex plugin missing after Local init: %v", err)
	}
	var envelope struct {
		Data struct {
			Plugins struct {
				Agents []string `json:"agents"`
				Scope  string   `json:"scope"`
			} `json:"plugins"`
			Next string `json:"next"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if strings.Join(envelope.Data.Plugins.Agents, ",") != "codex" || envelope.Data.Plugins.Scope != "global" {
		t.Fatalf("unexpected plugin result: %s", out.String())
	}
	if !strings.Contains(envelope.Data.Next, "plugins doctor") {
		t.Fatalf("next command should verify installed plugin: %s", out.String())
	}
}

func TestInitLocalNamesSelectedPluginInNextStep(t *testing.T) {
	deps, out := newTestDeps(t, "")
	homeDir := t.TempDir()
	deps.UserHomeDir = func() (string, error) { return homeDir, nil }

	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--no-input",
		"init", "--mode", "local", "--local-dir", filepath.Join(t.TempDir(), "local"),
		"--workspace-name", "Offline dogfood", "--display-name", "Dogfood Human", "--username", "dogfood-human",
		"--install-plugins", "--plugin-agent", "claude",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "specgate plugins doctor --agent claude") {
		t.Fatalf("next step does not name selected plugin: %s", out.String())
	}
}

func TestInitPersistsServerFromDeploymentPort(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SPECGATE_PORT=13000\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath
	deps.DeployRunner = &fakeDeployRunner{}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "init", "--mode", "full", "--dir", dir)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	cfg, err := config.LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "http://localhost:13000/api/doc-registry" {
		t.Fatalf("server = %q, want http://localhost:13000/api/doc-registry", cfg.Server)
	}
}

func TestUpRefreshesPersistedServerFromDeploymentPort(t *testing.T) {
	dir := t.TempDir()
	setupTestBundle(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SPECGATE_PORT=13991\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{Mode: config.ModeFull, Server: config.DefaultServerURL}).SaveTo(cfgPath); err != nil {
		t.Fatal(err)
	}
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath
	deps.DeployRunner = &fakeDeployRunner{}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "up", "--dir", dir)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	cfg, err := config.LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "http://localhost:13991/api/doc-registry" || cfg.DeploymentDir != dir {
		t.Fatalf("config after up = %#v", cfg)
	}
}

func TestInitPlainShowsLocalWebURL(t *testing.T) {
	dir := t.TempDir()
	setupTestBundle(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SPECGATE_PORT=13000\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deps, out := newTestDeps(t, "")
	deps.DeployRunner = &fakeDeployRunner{}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "init", "--mode", "full", "--dir", dir)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "Web UI:") || !strings.Contains(out.String(), "http://localhost:13000") {
		t.Fatalf("plain init must show the local web URL: %s", out.String())
	}
}

func TestInitPrefersEnvironmentPortOverDeploymentPort(t *testing.T) {
	dir := t.TempDir()
	setupTestBundle(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SPECGATE_PORT=13000\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPECGATE_PORT", "13001")

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath
	deps.DeployRunner = &fakeDeployRunner{}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "init", "--mode", "full", "--dir", dir)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	cfg, err := config.LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "http://localhost:13001/api/doc-registry" {
		t.Fatalf("server = %q, want http://localhost:13001/api/doc-registry", cfg.Server)
	}
}

func TestInitRefreshesPersistedDefaultServerForCustomPort(t *testing.T) {
	dir := t.TempDir()
	setupTestBundle(t, dir)
	t.Setenv("SPECGATE_PORT", "13001")

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{Server: config.DefaultServerURL}).SaveTo(cfgPath); err != nil {
		t.Fatal(err)
	}
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath
	deps.DeployRunner = &fakeDeployRunner{}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "init", "--mode", "full", "--dir", dir)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	cfg, err := config.LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "http://localhost:13001/api/doc-registry" {
		t.Fatalf("server = %q, want http://localhost:13001/api/doc-registry", cfg.Server)
	}
}

func TestInitKeepsExplicitServer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SPECGATE_PORT=13000\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath
	deps.DeployRunner = &fakeDeployRunner{}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", "https://specgate.example", "--no-input", "init", "--mode", "full", "--dir", dir)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	cfg, err := config.LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "" {
		t.Fatalf("server = %q, want empty because --server is an override", cfg.Server)
	}
}

func TestInitCanInstallSelectedPlugins(t *testing.T) {
	srv := newPluginRegistry(t)
	dir := t.TempDir()
	setupTestBundle(t, dir)

	homeDir := t.TempDir()

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	deps, out := newTestDeps(t, srv.URL)
	deps.ConfigPath = cfgPath
	deps.UserHomeDir = func() (string, error) { return homeDir, nil }
	deps.DeployRunner = &fakeDeployRunner{}
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "--server", srv.URL, "--no-input",
		"init", "--mode", "full", "--dir", dir,
		"--install-plugins", "--plugin-agent", "cursor",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	for _, path := range []string{
		".cursor/rules/using-specgate.mdc",
		".cursor/skills/specgate/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(homeDir, path)); err != nil {
			t.Fatalf("%s missing after init plugin install: %v\n%s", path, err, out.String())
		}
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".codex", "plugins", "specgate")); !os.IsNotExist(err) {
		t.Fatalf("codex plugin should not be installed for --plugin-agent cursor; stat err=%v", err)
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Plugins struct {
				Agents []string `json:"agents"`
				Scope  string   `json:"scope"`
			} `json:"plugins"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v, output = %s", err, out.String())
	}
	if !env.OK || env.Data.Plugins.Scope != "global" || strings.Join(env.Data.Plugins.Agents, ",") != "cursor" {
		t.Fatalf("unexpected init plugin payload: %s", out.String())
	}
}

func TestInitInstallsPluginsFromInferredLocalServer(t *testing.T) {
	var pluginRequests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pluginRequests = append(pluginRequests, r.URL.Path)
		switch r.URL.Path {
		case "/api/doc-registry/api/v1/identity/bootstrap":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"user":      map[string]any{"id": "user-1", "username": "dogfood", "display_name": "Dogfood User"},
				"workspace": map[string]any{"id": "ws-1", "slug": "dogfood", "name": "Dogfood workspace"},
			})
		case "/api/doc-registry/plugins/package.json":
			_, _ = io.WriteString(w, `{"name":"specgate","version":"0.1.0","skills":["specgate"]}`)
		case "/api/doc-registry/plugins/rules/using-specgate.mdc":
			_, _ = io.WriteString(w, "use specgate\n")
		case "/api/doc-registry/plugins/skills/specgate/SKILL.md":
			_, _ = io.WriteString(w, "# using specgate\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	port := strings.TrimPrefix(srv.URL, "http://127.0.0.1:")
	dir := t.TempDir()
	setupTestBundle(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SPECGATE_PORT="+port+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	homeDir := t.TempDir()
	deps, out := newTestDeps(t, "")
	deps.DeployRunner = &fakeDeployRunner{}
	deps.UserHomeDir = func() (string, error) { return homeDir, nil }

	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "--no-input", "init", "--mode", "full", "--dir", dir,
		"--workspace-name", "Dogfood workspace", "--display-name", "Dogfood User", "--username", "dogfood",
		"--install-plugins", "--plugin-agent", "cursor",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".cursor", "rules", "using-specgate.mdc")); err != nil {
		t.Fatalf("cursor plugin missing after init: %v", err)
	}
	if !strings.Contains(strings.Join(pluginRequests, "\n"), "/api/doc-registry/plugins/package.json") {
		t.Fatalf("plugin registry requests = %#v", pluginRequests)
	}
}

// TestInitCmdNoSeedByDefault verifies --no-input does not issue a seed command.
func TestInitCmdNoSeedByDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)

	fr := &fakeDeployRunner{}
	deps, _ := newTestDeps(t, "")
	deps.DeployRunner = fr
	command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "init", "--mode", "full", "--dir", dir)
	for _, cmd := range fr.Commands {
		if strings.Contains(cmd, "seed-demo") {
			t.Fatalf("unexpected seed command: %s", cmd)
		}
	}
}

// TestUpCmdJSON verifies `up` exits 0 in --json mode.
func TestUpCmdJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)

	runner := &fakeDeployRunner{}
	deps, out := newTestDeps(t, "")
	deps.DeployRunner = runner
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "up", "--dir", dir)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
}

// TestDownCmdJSON verifies `down` exits 0 in --json mode.
func TestDownCmdJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)

	deps, out := newTestDeps(t, "")
	deps.DeployRunner = &fakeDeployRunner{}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "down", "--dir", dir)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
}
