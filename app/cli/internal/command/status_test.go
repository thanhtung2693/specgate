package command_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// TestStatusJSONEnvelope verifies --json outputs a valid envelope with ok=true.
func TestStatusJSONEnvelope(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{statusHandler: jsonStatus(5, 2)}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "status", "--all-workspaces")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Counts struct {
				Total int `json:"total"`
			} `json:"counts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v, output: %s", err, out.String())
	}
	if !env.OK {
		t.Fatalf("ok = false, output: %s", out.String())
	}
	if env.Data.Counts.Total != 5 {
		t.Fatalf("total = %d, want 5", env.Data.Counts.Total)
	}
}

// TestStatusJSONHasNoSpinnerOrProse verifies JSON mode emits only a single JSON line.
func TestStatusJSONHasNoSpinnerOrProse(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{statusHandler: jsonStatus(1, 0)}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "status", "--all-workspaces")

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 JSON line, got %d: %s", len(lines), out.String())
	}
	if !json.Valid([]byte(lines[0])) {
		t.Fatalf("not valid JSON: %s", lines[0])
	}
}

func TestStatusUsesSelectedWorkspaceByDefault(t *testing.T) {
	t.Parallel()
	var gotWorkspace string
	srv := (&fakeServer{statusHandler: func(w http.ResponseWriter, r *http.Request) {
		gotWorkspace = r.URL.Query().Get("workspace_id")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body": map[string]any{
				"counts":          map[string]any{"total": 1, "ready": 0},
				"needs_attention": []any{},
			},
		})
	}}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{
		Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "specgate"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "status")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if gotWorkspace != "ws-1" {
		t.Fatalf("workspace_id query = %q, want ws-1", gotWorkspace)
	}
}

func TestStatusUsesProjectWorkspaceWhenBound(t *testing.T) {
	t.Parallel()
	repo := filepath.Join(t.TempDir(), "repo")
	nested := filepath.Join(repo, "service")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	canonicalRepo, ok := config.FindProjectRoot(nested)
	if !ok {
		t.Fatal("project root not found")
	}

	var gotWorkspace string
	srv := (&fakeServer{statusHandler: func(w http.ResponseWriter, r *http.Request) {
		gotWorkspace = r.URL.Query().Get("workspace_id")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body": map[string]any{
				"counts":          map[string]any{"total": 1, "ready": 0},
				"needs_attention": []any{},
			},
		})
	}}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	deps.WorkingDir = nested
	if err := (config.Config{
		Workspace: config.CurrentWorkspace{ID: "global-ws", Slug: "global"},
		Projects: map[string]config.ProjectConfig{
			canonicalRepo: {Workspace: config.CurrentWorkspace{ID: "project-ws", Slug: "platform"}},
		},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "status")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if gotWorkspace != "project-ws" {
		t.Fatalf("workspace_id query = %q, want project-ws", gotWorkspace)
	}
}

func TestStatusUsesWorkspaceOverride(t *testing.T) {
	t.Parallel()
	var gotWorkspace string
	srv := (&fakeServer{
		statusHandler: func(w http.ResponseWriter, r *http.Request) {
			gotWorkspace = r.URL.Query().Get("workspace_id")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"body": map[string]any{
					"counts":          map[string]any{"total": 1, "ready": 0},
					"needs_attention": []any{},
				},
			})
		},
		workspaceHandler: func(w http.ResponseWriter, r *http.Request) {
			if got := strings.TrimPrefix(r.URL.Path, "/api/v1/workspaces/"); got != "platform" {
				t.Fatalf("workspace lookup path = %q, want platform", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"body": map[string]any{"id": "ws-platform", "slug": "platform", "name": "Platform"},
			})
		},
	}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	if err := (config.Config{
		Workspace: config.CurrentWorkspace{ID: "global-ws", Slug: "global"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "--workspace", " platform ", "status")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if gotWorkspace != "ws-platform" {
		t.Fatalf("workspace_id query = %q, want ws-platform", gotWorkspace)
	}
}

func TestStatusUsesWorkspaceEnvOverride(t *testing.T) {
	t.Setenv("SPECGATE_WORKSPACE", " platform ")

	var gotWorkspace string
	srv := (&fakeServer{
		statusHandler: func(w http.ResponseWriter, r *http.Request) {
			gotWorkspace = r.URL.Query().Get("workspace_id")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"body": map[string]any{
					"counts":          map[string]any{"total": 1, "ready": 0},
					"needs_attention": []any{},
				},
			})
		},
		workspaceHandler: func(w http.ResponseWriter, r *http.Request) {
			if got := strings.TrimPrefix(r.URL.Path, "/api/v1/workspaces/"); got != "platform" {
				t.Fatalf("workspace lookup path = %q, want platform", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"body": map[string]any{"id": "ws-platform", "slug": "platform", "name": "Platform"},
			})
		},
	}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	if err := (config.Config{
		Workspace: config.CurrentWorkspace{ID: "global-ws", Slug: "global"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "status")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if gotWorkspace != "ws-platform" {
		t.Fatalf("workspace_id query = %q, want ws-platform", gotWorkspace)
	}
}

func TestStatusAllWorkspacesOmitsWorkspaceFilter(t *testing.T) {
	t.Parallel()
	var rawQuery string
	srv := (&fakeServer{statusHandler: func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body": map[string]any{
				"counts":          map[string]any{"total": 1, "ready": 0},
				"needs_attention": []any{},
			},
		})
	}}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{
		Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "specgate"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "status", "--all-workspaces")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if rawQuery != "all_workspaces=true" {
		t.Fatalf("query = %q, want all_workspaces=true", rawQuery)
	}
}

func TestStatusPlainShowsScopeAndNextAction(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{statusHandler: func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body": map[string]any{
				"counts": map[string]any{
					"total": 3,
					"ready": 3,
				},
				"needs_attention": []any{},
			},
		})
	}}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{
		Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "status")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{
		`Scope: global workspace "platform"`,
		"Work: 3 total",
		"ready 3",
		"Needs attention: 0",
		"Next: no work needs attention right now.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestStatusColorRequiresCapableTerminal(t *testing.T) {
	srv := (&fakeServer{statusHandler: func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body": map[string]any{
				"counts":          map[string]any{"total": 3, "ready": 3},
				"needs_attention": []any{},
			},
		})
	}}).build(t)

	for _, tc := range []struct {
		name     string
		args     []string
		tty      bool
		env      map[string]string
		wantANSI bool
	}{
		{name: "rich tty", args: []string{"--server", srv.URL, "status"}, tty: true, wantANSI: true},
		{name: "non tty", args: []string{"--server", srv.URL, "status"}},
		{name: "plain", args: []string{"--plain", "--server", srv.URL, "status"}, tty: true},
		{name: "json", args: []string{"--json", "--server", srv.URL, "status"}, tty: true},
		{name: "ci", args: []string{"--server", srv.URL, "status"}, tty: true, env: map[string]string{"CI": "true"}},
		{name: "no color", args: []string{"--server", srv.URL, "status"}, tty: true, env: map[string]string{"NO_COLOR": "1"}},
		{name: "dumb terminal", args: []string{"--server", srv.URL, "status"}, tty: true, env: map[string]string{"TERM": "dumb"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CI", "")
			t.Setenv("NO_COLOR", "")
			t.Setenv("TERM", "xterm-256color")
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			deps, out := newTestDeps(t, srv.URL)
			deps.StdoutIsTTY = func() bool { return tc.tty }
			deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
			if err := (config.Config{Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform"}}).SaveTo(deps.ConfigPath); err != nil {
				t.Fatal(err)
			}

			code := command.ExecuteForCode(command.NewRootCommand(deps), tc.args...)
			if code != output.ExitOK {
				t.Fatalf("exit = %d, output = %s", code, out.String())
			}
			got := out.String()
			if strings.Contains(got, "\x1b[") != tc.wantANSI {
				t.Fatalf("ANSI = %t, want %t: %q", strings.Contains(got, "\x1b["), tc.wantANSI, got)
			}
			if !tc.wantANSI && !strings.Contains(strings.Join(tc.args, " "), "--json") && strings.Contains(got, "█") {
				t.Fatalf("portable output contains dashboard glyph: %q", got)
			}
		})
	}
}

func TestStatusPlainShowsProjectWorkspaceScope(t *testing.T) {
	t.Parallel()
	canonicalRepo, nested := commandGitRepo(t)
	srv := (&fakeServer{statusHandler: func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body": map[string]any{
				"counts":          map[string]any{"total": 1, "ready": 1},
				"needs_attention": []any{},
			},
		})
	}}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.WorkingDir = nested
	cfg := config.Config{}
	cfg.SetProjectWorkspace(canonicalRepo, config.CurrentWorkspace{ID: "ws-project", Slug: "platform"})
	if err := cfg.SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "status")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), `Scope: project workspace "platform"`) {
		t.Fatalf("output missing project workspace scope:\n%s", out.String())
	}
}

// fakeDeployRunner implements deploy.CommandRunner for command-layer tests.
func TestStatusWithoutWorkspaceRequiresExplicitAllWorkspaces(t *testing.T) {
	t.Parallel()
	called := false
	srv := (&fakeServer{statusHandler: func(w http.ResponseWriter, r *http.Request) {
		called = true
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body": map[string]any{
				"counts":          map[string]any{"total": 2, "ready": 2},
				"needs_attention": []any{},
			},
		})
	}}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "status")
	if code == output.ExitOK {
		t.Fatalf("status unexpectedly succeeded without workspace: %s", out.String())
	}
	if called {
		t.Fatal("status requested a global view without --all-workspaces")
	}
}

func TestStatusHumanUsesDashboardVisuals(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	srv := (&fakeServer{statusHandler: func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body": map[string]any{
				"counts": map[string]any{
					"total":  5,
					"ready":  1,
					"review": 2,
				},
				"attention": []any{
					map[string]any{
						"change_request_id": "cr-1",
						"key":               "DEMO-301",
						"title":             "Close the delivery evidence loop",
						"phase":             "ready",
						"issues":            []string{"tracker_status_conflict"},
					},
				},
			},
		})
	}}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	deps.StdoutIsTTY = func() bool { return true }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--server", srv.URL, "status", "--all-workspaces")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"SpecGate Board",
		"Summary:",
		"Ready work:",
		"█",
		"Needs Attention",
		"DEMO-301",
		"tracker_status_conflict",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

// TestLocalStatusCmdJSON verifies `local-status` exits 0 in --json mode.
func TestLocalStatusCmdJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)

	deps, out := newTestDeps(t, "")
	deps.DeployRunner = &fakeDeployRunner{}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "local-status", "--dir", dir)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v, output: %s", err, out.String())
	}
	if !env.OK {
		t.Fatalf("ok = false: %s", out.String())
	}
}

func TestLocalStatusCmdPlainUsesFullApplianceTerminology(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)

	deps, out := newTestDeps(t, "")
	deps.DeployRunner = &fakeDeployRunner{
		OutputData: []byte(`[{"Name":"doc-registry","Status":"running (healthy)"}]`),
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "local-status", "--dir", dir)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "Full appliance") {
		t.Fatalf("output should identify the Full appliance:\n%s", out.String())
	}
	if strings.Contains(out.String(), "Local stack") {
		t.Fatalf("output should not use retired Local stack terminology:\n%s", out.String())
	}
}
