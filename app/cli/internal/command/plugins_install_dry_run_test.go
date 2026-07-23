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
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

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
