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
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestPluginsInstallProjectLocalAndDoctor(t *testing.T) {
	srv := newPluginRegistry(t)
	workDir := t.TempDir()
	t.Chdir(workDir)
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	existingMarketplace := filepath.Join(workDir, ".agents", "plugins", "marketplace.json")
	if err := os.MkdirAll(filepath.Dir(existingMarketplace), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existingMarketplace, []byte(`{"name":"personal","plugins":[{"name":"other"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	deps, out := newPluginDeps()
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
		".cursor/skills/using-specgate/SKILL.md",
		".cursor/skills/setting-up-specgate-project/SKILL.md",
		".codex/plugins/specgate/.codex-plugin/plugin.json",
		".codex/plugins/specgate/hooks/session-start",
		".codex/plugins/specgate/skills/shaping-work/SKILL.md",
		".codex/plugins/specgate/skills/completing-delivery/SKILL.md",
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
	if !strings.Contains(string(configBody), `[plugins."specgate@personal"]`) ||
		!strings.Contains(string(configBody), "enabled = true") {
		t.Fatalf("codex config not enabled: %s", configBody)
	}

	deps, out = newPluginDeps()
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

func TestPluginsDoctorReportsMissing(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	t.Setenv("HOME", t.TempDir())
	deps, out := newPluginDeps()
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
}

func TestPluginsDoctorWarnsWhenCodexCacheIsStale(t *testing.T) {
	srv := newPluginRegistry(t)
	workDir := t.TempDir()
	t.Chdir(workDir)
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	deps, out := newPluginDeps()
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--server", srv.URL,
		"plugins", "install",
		"--agent", "codex",
	)
	if code != output.ExitOK {
		t.Fatalf("install exit = %d, output = %s", code, out.String())
	}

	cacheRoot := filepath.Join(homeDir, ".codex", "plugins", "cache", "personal", "specgate", "0.1.0")
	for _, skill := range []string{
		"using-specgate",
		"checking-spec-readiness",
		"picking-up-work",
		"implementing-work",
		"completing-delivery",
	} {
		path := filepath.Join(cacheRoot, "skills", skill, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("# "+skill+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	deps, out = newPluginDeps()
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
	for _, want := range []string{"Codex plugin cache is stale", "setting-up-specgate-project", "shaping-work", "restart Codex"} {
		if !strings.Contains(got, want) {
			t.Fatalf("warning missing %q: %s", want, out.String())
		}
	}
}

func TestPluginsInstallUsesRegistryFlag(t *testing.T) {
	srv := newPluginRegistry(t)
	workDir := t.TempDir()
	t.Chdir(workDir)
	t.Setenv("HOME", t.TempDir())

	deps, out := newPluginDeps()
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

func newPluginDeps() (*command.Deps, *bytes.Buffer) {
	var out bytes.Buffer
	return &command.Deps{
		Stdout: &out,
		Stderr: io.Discard,
		Stdin:  strings.NewReader(""),
		Opener: func(_ string) error { return nil },
	}, &out
}

func newPluginRegistry(t *testing.T) *httptest.Server {
	t.Helper()
	skills := []string{
		"using-specgate",
		"setting-up-specgate-project",
		"checking-spec-readiness",
		"shaping-work",
		"picking-up-work",
		"implementing-work",
		"completing-delivery",
	}
	files := map[string]string{
		"package.json": `{
  "schema": "specgate.plugin-package/v1",
  "name": "specgate",
  "display_name": "SpecGate",
  "version": "0.1.0",
  "description": "SpecGate plugin",
  "skills": ["using-specgate", "setting-up-specgate-project", "checking-spec-readiness", "shaping-work", "picking-up-work", "implementing-work", "completing-delivery"],
  "retired_skills": ["specgate-handoff"],
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
