package command_test

import (
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
