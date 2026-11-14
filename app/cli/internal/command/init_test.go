package command_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestLocalInitSuggestsAgentNeutralPluginInstall(t *testing.T) {
	deps, out := newTestDeps(t)
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
	deps, out := newTestDeps(t)
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

func TestLocalInitIgnoresRepositoryContainedStateBeforeCreatingIt(t *testing.T) {
	deps, out := newTestDeps(t)
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

			runner := &fakeCommandRunner{}
			deps, out := newTestDeps(t)
			deps.ConfigPath = cfgPath
			deps.CommandRunner = runner
			args := []string{"--json", "--no-input", "init", "--mode", testCase.mode}
			if testCase.mode == "local" {
				args = append(args, "--local-dir", stateDir)
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

func TestInitLocalCreatesStateWithoutDocker(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	deps, out := newTestDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	runner := &fakeCommandRunner{}
	deps.CommandRunner = runner

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Offline dogfood", "--display-name", "Dogfood Human", "--username", "dogfood-human", "--email", "human@example.com")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if len(runner.Commands) != 0 {
		t.Fatalf("local init invoked docker: %#v", runner.Commands)
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
	deps, out := newTestDeps(t)
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
	deps, out := newTestDeps(t)
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

// TestInitCmdNoSeedByDefault verifies --no-input does not issue a seed command.

// TestUpCmdJSON verifies `up` exits 0 in --json mode.

// TestDownCmdJSON verifies `down` exits 0 in --json mode.
