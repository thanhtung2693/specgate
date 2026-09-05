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

func TestLocalInitRejectsInvalidPluginScopeBeforeStateMutation(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.UserHomeDir = func() (string, error) { return t.TempDir(), nil }

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "init",
		"--mode", "local", "--local-dir", stateDir,
		"--workspace-name", "Alpha", "--display-name", "Human", "--username", "human",
		"--install-plugins", "--plugin-agent", "codex", "--plugin-scope", "somewhere")
	if code != output.ExitUsage || !strings.Contains(out.String(), "global or project") {
		t.Fatalf("exit=%d output=%s", code, out.String())
	}
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("state directory was mutated before validation: %v", err)
	}
}

func TestLocalInitProjectPluginScopeRejectsNestedInvocationBeforeStateMutation(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(repo, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(t.TempDir(), "state")
	deps, out := newTestDeps(t, "")
	deps.WorkingDir = nested
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	canonicalRepo, ok := config.FindProjectRoot(nested)
	if !ok {
		t.Fatal("project root not found")
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "init",
		"--mode", "local", "--local-dir", stateDir,
		"--workspace-name", "Alpha", "--display-name", "Human", "--username", "human",
		"--install-plugins", "--plugin-agent", "codex", "--plugin-scope", "project")
	if code != output.ExitUsage || !strings.Contains(out.String(), "cd "+canonicalRepo) {
		t.Fatalf("exit=%d output=%s", code, out.String())
	}
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("state directory was mutated before project-root validation: %v", err)
	}
}

func TestLocalDoctorReportsRepositoryShellAndOptionalPlugins(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(t.TempDir(), "state")
	home := t.TempDir()
	deps, out := newTestDeps(t, "")
	deps.WorkingDir = repo
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.UserHomeDir = func() (string, error) { return home, nil }
	args := []string{"--json", "--no-input", "init", "--mode", "local", "--local-dir", stateDir,
		"--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), args...); code != output.ExitOK {
		t.Fatalf("init exit=%d output=%s", code, out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "doctor"); code != output.ExitOK {
		t.Fatalf("doctor exit=%d output=%s", code, out.String())
	}
	var envelope struct {
		Data struct {
			Repository map[string]any `json:"repository"`
			Shell      map[string]any `json:"shell"`
			Plugins    map[string]any `json:"plugins"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Repository["status"] != "ok" || envelope.Data.Shell["status"] != "ok" {
		t.Fatalf("diagnostics=%s", out.String())
	}
	if envelope.Data.Plugins["status"] != "optional" || strings.Contains(strings.ToLower(out.String()), "loaded") {
		t.Fatalf("plugins must be optional and not claim session loading: %s", out.String())
	}
	if cfg, err := config.LoadFrom(deps.ConfigPath); err != nil || len(cfg.Projects) != 1 {
		t.Fatalf("project binding missing: cfg=%#v err=%v", cfg, err)
	}
}

func TestLocalDoctorSeparatesMissingRepositoryAndShellDiagnostics(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	deps, out := newTestDeps(t, "")
	deps.WorkingDir = t.TempDir()
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "init", "--mode", "local", "--local-dir", stateDir,
		"--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit=%d output=%s", code, out.String())
	}
	t.Setenv("PATH", "")
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "doctor"); code != output.ExitOK {
		t.Fatalf("doctor exit=%d output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"repository":{"status":"missing"`) || !strings.Contains(out.String(), `"shell":{"status":"missing"`) {
		t.Fatalf("missing diagnostics are not distinct: %s", out.String())
	}
}

func TestLocalDoctorMarksMismatchedProjectBindingStale(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	deps, out := newTestDeps(t, "")
	deps.WorkingDir = repo
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	stateDir := filepath.Join(t.TempDir(), "state")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "init", "--mode", "local", "--local-dir", stateDir,
		"--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit=%d output=%s", code, out.String())
	}
	cfg, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	root, ok := config.FindProjectRoot(repo)
	if !ok {
		t.Fatal("missing project root")
	}
	project := cfg.Projects[root]
	project.Workspace = config.CurrentWorkspace{ID: "wrong", Slug: "wrong", Name: "Wrong"}
	cfg.Projects[root] = project
	if err := cfg.SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "doctor"); code != output.ExitOK {
		t.Fatalf("doctor exit=%d output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"repository":{"status":"stale"`) || !strings.Contains(out.String(), "workspace bind") {
		t.Fatalf("mismatched binding was not actionable: %s", out.String())
	}
}
