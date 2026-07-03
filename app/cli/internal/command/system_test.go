package command_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/specgate/specgate/app/cli/internal/buildinfo"
	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/deploy"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// fakeDeployRunner implements deploy.CommandRunner for command-layer tests.
type fakeDeployRunner struct {
	Commands []string
	Err      error
	// OutputData, when non-nil, is returned by Output instead of "[]".
	OutputData      []byte
	OutputByCommand map[string][]byte
}

func (f *fakeDeployRunner) Run(_ context.Context, name string, args ...string) error {
	all := append([]string{name}, args...)
	f.Commands = append(f.Commands, strings.Join(all, " "))
	return f.Err
}

func (f *fakeDeployRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	all := append([]string{name}, args...)
	cmd := strings.Join(all, " ")
	f.Commands = append(f.Commands, cmd)
	if f.OutputByCommand != nil {
		if data, ok := f.OutputByCommand[cmd]; ok {
			return data, f.Err
		}
	}
	if f.OutputData != nil {
		return f.OutputData, f.Err
	}
	return []byte("[]"), f.Err
}

var _ deploy.CommandRunner = (*fakeDeployRunner)(nil)

// setupTestBundle creates a minimal compose bundle in dir so Init can proceed.
func setupTestBundle(t *testing.T, dir string) {
	t.Helper()
	os.WriteFile(filepath.Join(dir, "compose.yml"), []byte("# placeholder\n"), 0644)
	os.WriteFile(filepath.Join(dir, "doc-registry.env.example"), []byte("SETTINGS_ENCRYPTION_KEY=\n"), 0644)
	os.WriteFile(filepath.Join(dir, "agents.env.example"), []byte("LANGSMITH_API_KEY=\n"), 0644)
}

// fakeServer wires HTTP handlers into a test httptest.Server.
type fakeServer struct {
	metaHandler   http.HandlerFunc
	statusHandler http.HandlerFunc
}

func (f *fakeServer) build(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	if f.metaHandler != nil {
		mux.HandleFunc("/api/v1/meta", f.metaHandler)
	}
	if f.statusHandler != nil {
		mux.HandleFunc("/api/v1/status", f.statusHandler)
	} else {
		// Doctor probes the board endpoint; default to an empty healthy board.
		mux.HandleFunc("/api/v1/status", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"counts":{},"attention":[]}`))
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func jsonMeta(apiVersion string, capabilities map[string]bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body": map[string]any{
				"api_version":    apiVersion,
				"server_version": "dev",
				"capabilities":   capabilities,
			},
		})
	}
}

func TestVersionCommandPrintsBuildVersion(t *testing.T) {
	t.Setenv("SPECGATE_NO_UPDATE_CHECK", "1")
	oldVersion := buildinfo.Version
	buildinfo.Version = "v9.9.9-test"
	t.Cleanup(func() { buildinfo.Version = oldVersion })

	var stdout, stderr bytes.Buffer
	deps := command.DefaultDeps()
	deps.Stdout = &stdout
	deps.Stderr = &stderr

	code := command.ExecuteForCode(command.NewRootCommand(deps), "version")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, want %d; stderr=%q", code, output.ExitOK, stderr.String())
	}
	if got, want := stdout.String(), "specgate v9.9.9-test\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func jsonStatus(total, ready int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body": map[string]any{
				"counts":          map[string]any{"total": total, "ready": ready},
				"needs_attention": []any{},
			},
		})
	}
}

func newTestDeps(t *testing.T, srvURL string) (*command.Deps, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	deps := &command.Deps{
		Stdout: &out,
		Stderr: io.Discard,
		Stdin:  strings.NewReader(""),
		Opener: func(_ string) error { return nil },
		// Isolate config to a temp file so commands that persist config (init,
		// identity, config set) never write to the developer's real
		// ~/.config/specgate/config.json when the suite runs. Tests that need a
		// specific path may override deps.ConfigPath after this call.
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
	}
	_ = srvURL // deps.Client left nil → PersistentPreRunE constructs it
	return deps, &out
}

// TestStatusJSONEnvelope verifies --json outputs a valid envelope with ok=true.
func TestStatusJSONEnvelope(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{statusHandler: jsonStatus(5, 2)}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "status")
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
	command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "status")

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
	if rawQuery != "" {
		t.Fatalf("query = %q, want empty", rawQuery)
	}
}

func TestStatusPlainShowsScopeAndNextAction(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{statusHandler: func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body": map[string]any{
				"counts": map[string]any{
					"total":   3,
					"ready":   0,
					"handoff": 3,
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
		`Scope: selected workspace "platform"`,
		"Work: 3 total",
		"handoff 3",
		"Needs attention: 0",
		"Next: no work needs attention right now.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestStatusHumanUsesDashboardVisuals(t *testing.T) {
	t.Parallel()
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
						"phase":             "handoff",
						"issues":            []string{"tracker_status_conflict"},
					},
				},
			},
		})
	}}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--server", srv.URL, "status")
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

// TestDoctorGreenExitsOK verifies doctor exits 0 when server is healthy.
func TestDoctorGreenExitsOK(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
	}).build(t)

	deps, _ := newTestDeps(t, srv.URL)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "doctor")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
}

// TestDoctorShowsLocalStackSection verifies doctor renders a "Local stack"
// section (same data as `local-status`) when a CLI-managed deployment exists.
func TestDoctorShowsLocalStackSection(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
	}).build(t)

	dir := t.TempDir()
	setupTestBundle(t, dir)

	deps, out := newTestDeps(t, srv.URL)
	if err := (config.Config{DeploymentDir: dir}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	deps.DeployRunner = &fakeDeployRunner{
		OutputData: []byte(`[{"Name":"doc-registry","Status":"running (healthy)"}]`),
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "doctor")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{"Local stack", dir, "doc-registry", "running (healthy)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

// TestDoctorSkipsLocalStackWithoutDeployment verifies doctor omits the local
// stack section when no CLI-managed deployment exists.
func TestDoctorSkipsLocalStackWithoutDeployment(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
	}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	// DeploymentDir points at an empty dir: no compose.yml → no section.
	if err := (config.Config{DeploymentDir: t.TempDir()}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	deps.DeployRunner = &fakeDeployRunner{}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "doctor")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if strings.Contains(out.String(), "Local stack") {
		t.Fatalf("output should not contain a local stack section:\n%s", out.String())
	}
}

func TestServerCommandWarnsWhenCLIBehindRecommendedVersion(t *testing.T) {
	oldVersion := buildinfo.Version
	buildinfo.Version = "v0.1.0-alpha.1"
	t.Cleanup(func() { buildinfo.Version = oldVersion })

	var stderr bytes.Buffer
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/meta", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body": map[string]any{
				"api_version":             "specgate.api/v1",
				"version":                 "v0.1.0-alpha.2",
				"recommended_cli_version": "v0.1.0-alpha.2",
				"capabilities":            map[string]bool{"agents": true},
			},
		})
	})
	mux.HandleFunc("/api/v1/status", jsonStatus(1, 0))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	deps, _ := newTestDeps(t, srv.URL)
	deps.Stderr = &stderr
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "status")
	if code != output.ExitOK {
		t.Fatalf("exit = %d", code)
	}
	got := stderr.String()
	if !strings.Contains(got, "specgate CLI v0.1.0-alpha.1") ||
		!strings.Contains(got, "v0.1.0-alpha.2") ||
		!strings.Contains(got, "specgate update") {
		t.Fatalf("missing update warning, got %q", got)
	}
}

func TestJSONModeSuppressesCLIUpdateWarning(t *testing.T) {
	oldVersion := buildinfo.Version
	buildinfo.Version = "v0.1.0-alpha.1"
	t.Cleanup(func() { buildinfo.Version = oldVersion })

	var stderr bytes.Buffer
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/meta", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body": map[string]any{
				"api_version":             "specgate.api/v1",
				"recommended_cli_version": "v0.1.0-alpha.2",
				"capabilities":            map[string]bool{"agents": true},
			},
		})
	})
	mux.HandleFunc("/api/v1/status", jsonStatus(1, 0))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	deps, _ := newTestDeps(t, srv.URL)
	deps.Stderr = &stderr
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "status")
	if code != output.ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("json mode should not emit warning, got %q", got)
	}
}

func TestCommandWarnsWhenGitHubReleaseIsNewer(t *testing.T) {
	t.Setenv("CI", "")
	oldVersion := buildinfo.Version
	buildinfo.Version = "v0.1.0-alpha.1"
	t.Cleanup(func() { buildinfo.Version = oldVersion })

	var stderr bytes.Buffer
	srv := (&fakeServer{
		metaHandler:   jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
		statusHandler: jsonStatus(1, 0),
	}).build(t)

	deps, _ := newTestDeps(t, srv.URL)
	deps.Stderr = &stderr
	deps.CheckLatestRelease = func(context.Context, time.Duration, string) (string, error) {
		return "v0.1.0-alpha.2", nil
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "status")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := stderr.String()
	if !strings.Contains(got, "latest GitHub release v0.1.0-alpha.2") ||
		!strings.Contains(got, "scripts/install-cli.sh") {
		t.Fatalf("missing GitHub release warning, got %q", got)
	}
}

func TestGitHubReleaseWarningCanBeDisabled(t *testing.T) {
	t.Setenv("CI", "")
	oldVersion := buildinfo.Version
	buildinfo.Version = "v0.1.0-alpha.1"
	t.Cleanup(func() { buildinfo.Version = oldVersion })
	t.Setenv("SPECGATE_NO_UPDATE_CHECK", "1")

	var stderr bytes.Buffer
	called := false
	srv := (&fakeServer{
		metaHandler:   jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
		statusHandler: jsonStatus(1, 0),
	}).build(t)

	deps, _ := newTestDeps(t, srv.URL)
	deps.Stderr = &stderr
	deps.CheckLatestRelease = func(context.Context, time.Duration, string) (string, error) {
		called = true
		return "v0.1.0-alpha.2", nil
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "status")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	if called {
		t.Fatal("GitHub release check should be skipped when SPECGATE_NO_UPDATE_CHECK=1")
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("disabled update check should be silent, got %q", got)
	}
}

// TestDoctorAgentsUnavailableExits5 verifies capabilities["agents"]=false → exit 5.
func TestDoctorAgentsUnavailableExits5(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": false}),
	}).build(t)

	deps, _ := newTestDeps(t, srv.URL)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "doctor")
	if code != output.ExitUnavailable {
		t.Fatalf("exit = %d, want %d (ExitUnavailable)", code, output.ExitUnavailable)
	}
}

// TestDoctorBadAPIVersionExits6 verifies a mismatched api_version → exit 6.
func TestDoctorBadAPIVersionExits6(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v0", nil),
	}).build(t)

	deps, _ := newTestDeps(t, srv.URL)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "doctor")
	if code != output.ExitIncompatible {
		t.Fatalf("exit = %d, want %d (ExitIncompatible)", code, output.ExitIncompatible)
	}
}

// TestDoctorServerDownExits5 verifies unreachable server → exit 5.
func TestDoctorServerDownExits5(t *testing.T) {
	t.Parallel()
	deps, _ := newTestDeps(t, "")
	// Point at an address that refuses connections.
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", "http://127.0.0.1:1", "doctor")
	if code != output.ExitUnavailable {
		t.Fatalf("exit = %d, want %d (ExitUnavailable)", code, output.ExitUnavailable)
	}
}

// TestConfigSetServerPersists verifies config set server writes the URL to the config file.
func TestConfigSetServerPersists(t *testing.T) {
	t.Parallel()
	cfgPath := filepath.Join(t.TempDir(), "config.json")

	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath
	// config set server doesn't call the API, so we don't need a real server.
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", "http://localhost:8080", "config", "set", "server", "https://my.specgate.example")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}

	got, err := config.LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Server != "https://my.specgate.example" {
		t.Fatalf("Server = %q, want https://my.specgate.example", got.Server)
	}
}

// TestInitCmdJSON verifies init exits 0 in --json --no-input mode.
func TestInitCmdJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath
	deps.DeployRunner = &fakeDeployRunner{}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "init", "--dir", dir)
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

func TestInitPersistsServerFromDeploymentPort(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DOC_REGISTRY_PORT=18080\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath
	deps.DeployRunner = &fakeDeployRunner{}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "init", "--dir", dir)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	cfg, err := config.LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "http://localhost:18080" {
		t.Fatalf("server = %q, want http://localhost:18080", cfg.Server)
	}
}

func TestInitKeepsExplicitServer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DOC_REGISTRY_PORT=18080\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath
	deps.DeployRunner = &fakeDeployRunner{}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", "https://specgate.example", "--no-input", "init", "--dir", dir)
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
	t.Setenv("HOME", homeDir)

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	deps, out := newTestDeps(t, srv.URL)
	deps.ConfigPath = cfgPath
	deps.DeployRunner = &fakeDeployRunner{}
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "--server", srv.URL, "--no-input",
		"init", "--dir", dir,
		"--install-plugins", "--plugin-agent", "cursor",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	for _, path := range []string{
		".cursor/rules/using-specgate.mdc",
		".cursor/skills/using-specgate/SKILL.md",
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

// TestInitCmdNoSeedByDefault verifies --no-input does not issue a seed command.
func TestInitCmdNoSeedByDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)

	fr := &fakeDeployRunner{}
	deps, _ := newTestDeps(t, "")
	deps.DeployRunner = fr
	command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "init", "--dir", dir)
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

	deps, out := newTestDeps(t, "")
	deps.DeployRunner = &fakeDeployRunner{}
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

func TestUninstallKeepsDataByDefaultAndRemovesUserFiles(t *testing.T) {
	dir := t.TempDir()
	setupTestBundle(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "data.txt"), []byte("keep me"), 0644); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestFile(t, filepath.Join(home, ".cursor", "rules", "using-specgate.mdc"), "rule")
	writeTestFile(t, filepath.Join(home, ".cursor", "skills", "using-specgate", "SKILL.md"), "skill")
	writeTestFile(t, filepath.Join(home, ".codex", "plugins", "specgate", ".codex-plugin", "plugin.json"), "{}")
	writeTestFile(t, filepath.Join(home, ".codex", "plugins", "cache", "personal", "specgate", "v0.1.0", ".codex-plugin", "plugin.json"), "{}")
	writeTestFile(t, filepath.Join(home, ".codex", "skills", "specgate", "SKILL.md"), "legacy")
	writeTestFile(t, filepath.Join(home, ".codex", "skills", "using-specgate", "SKILL.md"), "legacy")
	writeTestFile(t, filepath.Join(home, ".claude", "skills", "specgate", ".claude-plugin", "plugin.json"), "{}")
	writeTestFile(t, filepath.Join(home, ".codex", "config.toml"), "[plugins.\"specgate@personal\"]\nenabled = true\n\n[tools]\nkeep = true\n")
	writeTestFile(t, filepath.Join(home, ".agents", "plugins", "marketplace.json"), `{
  "name": "personal",
  "plugins": [
    {"name": "specgate"},
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
		filepath.Join(home, ".cursor", "skills", "using-specgate"),
		filepath.Join(home, ".codex", "plugins", "specgate"),
		filepath.Join(home, ".codex", "plugins", "cache", "personal", "specgate"),
		filepath.Join(home, ".codex", "skills", "specgate"),
		filepath.Join(home, ".codex", "skills", "using-specgate"),
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
	if strings.Contains(string(configText), "specgate@personal") || !strings.Contains(string(configText), "[tools]") {
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

func TestUninstallRemovesSpecGateOnlyPluginConfigFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, ".codex", "config.toml")
	marketplacePath := filepath.Join(home, ".agents", "plugins", "marketplace.json")
	writeTestFile(t, configPath, "[plugins.\"specgate@personal\"]\nenabled = true\n")
	writeTestFile(t, marketplacePath, `{
  "name": "personal",
  "plugins": [
    {"name": "specgate"}
  ]
}
`)
	writeTestFile(t, filepath.Join(home, ".codex", "plugins", "specgate", ".codex-plugin", "plugin.json"), "{}")

	deps, out := newTestDeps(t, "")
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
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

func TestUninstallRemovesEmptyPluginConfigFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, ".codex", "config.toml")
	marketplacePath := filepath.Join(home, ".agents", "plugins", "marketplace.json")
	writeTestFile(t, configPath, "")
	writeTestFile(t, marketplacePath, `{"name":"personal","plugins":[]}`)

	deps, out := newTestDeps(t, "")
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "uninstall", "--dir", filepath.Join(t.TempDir(), "deploy"))
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	for _, path := range []string{configPath, marketplacePath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s should be removed; stat err=%v", path, err)
		}
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

func TestUninstallPurgeDataRemovesDeploymentDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "data.txt"), []byte("delete me"), 0644); err != nil {
		t.Fatal(err)
	}

	runner := &fakeDeployRunner{OutputByCommand: map[string][]byte{
		"docker compose -f " + filepath.Join(dir, "compose.yml") + " config --images": []byte("ghcr.io/thanhtung2693/doc-registry:v0.1.0-alpha.1\npgvector/pgvector:0.8.3-pg18\nghcr.io/thanhtung2693/ui:v0.1.0-alpha.1\n"),
	}}
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
	if !strings.Contains(gotCommands, "docker compose -f "+filepath.Join(dir, "compose.yml")+" config --images") {
		t.Fatalf("missing compose image discovery command in:\n%s", gotCommands)
	}
	if !strings.Contains(gotCommands, "docker image rm ghcr.io/thanhtung2693/doc-registry:v0.1.0-alpha.1 ghcr.io/thanhtung2693/ui:v0.1.0-alpha.1") {
		t.Fatalf("missing filtered image rm command in:\n%s", gotCommands)
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
	if strings.Contains(gotCommands, "pgvector/pgvector") {
		t.Fatalf("shared base image should not be removed:\n%s", gotCommands)
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
		"docker compose -f " + filepath.Join(dir, "compose.yml") + " config --images": []byte("ghcr.io/thanhtung2693/agents:v0.1.0-alpha.1\n"),
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

func TestUninstallPlainPurgeDataAlsoRemovesImages(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setupTestBundle(t, dir)

	runner := &fakeDeployRunner{OutputByCommand: map[string][]byte{
		"docker compose -f " + filepath.Join(dir, "compose.yml") + " config --images": []byte("ghcr.io/thanhtung2693/agents:v0.1.0-alpha.1\n"),
	}}
	deps, out := newTestDeps(t, "")
	deps.DeployRunner = runner
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--yes", "uninstall", "--dir", dir, "--purge-data")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	gotCommands := strings.Join(runner.Commands, "\n")
	if !strings.Contains(gotCommands, "docker image rm ghcr.io/thanhtung2693/agents:v0.1.0-alpha.1") {
		t.Fatalf("missing image rm command in:\n%s", gotCommands)
	}
}

func TestUninstallChecklistCanKeepPluginsAndPurgeData(t *testing.T) {
	dir := t.TempDir()
	setupTestBundle(t, dir)
	home := t.TempDir()
	t.Setenv("HOME", home)
	pluginPath := filepath.Join(home, ".cursor", "rules", "using-specgate.mdc")
	writeTestFile(t, pluginPath, "rule")

	runner := &fakeDeployRunner{}
	deps, _, prompter, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	deps.DeployRunner = runner
	runner.OutputByCommand = map[string][]byte{
		"docker compose -f " + filepath.Join(dir, "compose.yml") + " config --images": []byte("ghcr.io/thanhtung2693/agents:v0.1.0-alpha.1\nredis:8-alpine\n"),
	}
	prompter.multiValues = []string{"data", "images"}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "uninstall", "--dir", dir)
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
	if !strings.Contains(gotCommands, "docker image rm ghcr.io/thanhtung2693/agents:v0.1.0-alpha.1") {
		t.Fatalf("missing image rm command in:\n%s", gotCommands)
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
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

func TestUpdateJSONProgressEmitsEventsAndFinalEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cli/install.sh", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "#!/usr/bin/env sh\nexit 0\n")
	})
	mux.HandleFunc("/plugins/install.sh", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "#!/usr/bin/env sh\nexit 0\n")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	plugins := newPluginRegistry(t)

	deps, out := newTestDeps(t, srv.URL)
	deps.CLIInstallURL = srv.URL + "/cli/install.sh"
	deps.PluginRegistryURL = plugins.URL
	t.Setenv("HOME", t.TempDir())
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--json-progress", "--server", srv.URL, "update")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected progress lines plus final envelope, got %d lines: %s", len(lines), out.String())
	}

	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("first line unmarshal: %v", err)
	}
	if first["event"] == nil {
		t.Fatalf("first line missing event: %s", lines[0])
	}

	var final struct {
		OK   bool `json:"ok"`
		Data struct {
			Steps []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"steps"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &final); err != nil {
		t.Fatalf("final line unmarshal: %v", err)
	}
	if !final.OK {
		t.Fatalf("final envelope not ok: %s", lines[len(lines)-1])
	}
	if len(final.Data.Steps) != 4 {
		t.Fatalf("steps = %d, want 4", len(final.Data.Steps))
	}
}

func TestUpdateHumanShowsStepLabels(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cli/install.sh", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "#!/usr/bin/env sh\nexit 0\n")
	})
	mux.HandleFunc("/plugins/install.sh", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "#!/usr/bin/env sh\nexit 0\n")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	plugins := newPluginRegistry(t)

	deps, out := newTestDeps(t, srv.URL)
	deps.CLIInstallURL = srv.URL + "/cli/install.sh"
	deps.PluginRegistryURL = plugins.URL
	t.Setenv("HOME", t.TempDir())
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "update")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Step 1/4 Detect install target") {
		t.Fatalf("missing step 1: %s", got)
	}
	if !strings.Contains(got, "Step 2/4 Install CLI") {
		t.Fatalf("missing step 2: %s", got)
	}
	if !strings.Contains(got, "Step 3/4 Update IDE setup") {
		t.Fatalf("missing step 3: %s", got)
	}
	if !strings.Contains(got, "Step 4/4 Update local stack") {
		t.Fatalf("missing step 4: %s", got)
	}
}

func TestUpdateRefreshesAllIDEPluginsFromPublicRegistry(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cli/install.sh", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "#!/usr/bin/env sh\nexit 0\n")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	plugins := newPluginRegistry(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	deps, out := newTestDeps(t, srv.URL)
	deps.CLIInstallURL = srv.URL + "/cli/install.sh"
	deps.PluginRegistryURL = plugins.URL
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "update")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "plugins", "specgate", ".codex-plugin", "plugin.json")); err != nil {
		t.Fatalf("codex plugin not installed from registry: %v\n%s", err, out.String())
	}
}

func TestUpdateFetchHonorsTimeout(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cli/install.sh", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = io.WriteString(w, "#!/usr/bin/env sh\nexit 0\n")
	})
	mux.HandleFunc("/plugins/install.sh", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "#!/usr/bin/env sh\nexit 0\n")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	plugins := newPluginRegistry(t)

	deps, out := newTestDeps(t, srv.URL)
	deps.CLIInstallURL = srv.URL + "/cli/install.sh"
	deps.PluginRegistryURL = plugins.URL
	t.Setenv("HOME", t.TempDir())
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--timeout", "50ms", "--server", srv.URL, "update")
	if code != output.ExitUnavailable {
		t.Fatalf("exit = %d, want unavailable; output = %s", code, out.String())
	}
}

func TestUpdateUsesPublicCLIInstallerInsteadOfConnectedServer(t *testing.T) {
	cliSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "#!/usr/bin/env sh\nexit 0\n")
	}))
	defer cliSrv.Close()

	serverCLIHit := false
	mux := http.NewServeMux()
	mux.HandleFunc("/cli/install.sh", func(w http.ResponseWriter, _ *http.Request) {
		serverCLIHit = true
		http.Error(w, "server installer should not be used", http.StatusInternalServerError)
	})
	mux.HandleFunc("/plugins/install.sh", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "#!/usr/bin/env sh\nexit 0\n")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	plugins := newPluginRegistry(t)

	deps, out := newTestDeps(t, srv.URL)
	deps.CLIInstallURL = cliSrv.URL
	deps.PluginRegistryURL = plugins.URL
	t.Setenv("HOME", t.TempDir())
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "update")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if serverCLIHit {
		t.Fatal("update fetched CLI installer from connected server")
	}
}

func TestUpdateInstallsIDEPluginsWhenConnectedServerUnavailable(t *testing.T) {
	cliSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "#!/usr/bin/env sh\nexit 0\n")
	}))
	defer cliSrv.Close()
	plugins := newPluginRegistry(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	deps, out := newTestDeps(t, srv.URL)
	deps.CLIInstallURL = cliSrv.URL
	deps.PluginRegistryURL = plugins.URL
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "update")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, want OK; output = %s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "specgate", ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("claude plugin not installed from public registry: %v\n%s", err, out.String())
	}
}

func TestUpdateRefreshesLocalDeploymentBundleAndImages(t *testing.T) {
	cliSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "#!/usr/bin/env sh\nexit 0\n")
	}))
	defer cliSrv.Close()
	plugins := newPluginRegistry(t)
	bundles := newComposeBundleRegistry(t, "v9.9.9")
	deployDir := t.TempDir()
	setupTestBundle(t, deployDir)
	if err := os.WriteFile(filepath.Join(deployDir, ".env"), []byte("SPECGATE_VERSION=v1.0.0\nDOC_REGISTRY_PORT=18080\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runner := &fakeDeployRunner{}
	deps, out := newTestDeps(t, "http://127.0.0.1:1")
	deps.CLIInstallURL = cliSrv.URL
	deps.PluginRegistryURL = plugins.URL
	deps.BundleBaseURL = bundles.URL + "/v9.9.9"
	deps.DeployRunner = runner
	t.Setenv("HOME", t.TempDir())

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "update", "--version", "v9.9.9", "--dir", deployDir)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	env, err := os.ReadFile(filepath.Join(deployDir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "SPECGATE_VERSION=v9.9.9") ||
		!strings.Contains(string(env), "DOC_REGISTRY_PORT=18080") {
		t.Fatalf(".env not updated/preserved: %s", env)
	}
	compose, err := os.ReadFile(filepath.Join(deployDir, "compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compose), "# bundle v9.9.9") {
		t.Fatalf("compose bundle not refreshed: %s", compose)
	}
	gotCommands := strings.Join(runner.Commands, "\n")
	for _, want := range []string{"docker version", "docker compose version", "docker compose -f " + filepath.Join(deployDir, "compose.yml") + " pull", "docker compose -f " + filepath.Join(deployDir, "compose.yml") + " up -d --wait"} {
		if !strings.Contains(gotCommands, want) {
			t.Fatalf("missing command %q in:\n%s", want, gotCommands)
		}
	}
}

func newComposeBundleRegistry(t *testing.T, version string) *httptest.Server {
	t.Helper()
	files := map[string]string{
		"compose.yml":              "# bundle " + version + "\n",
		"doc-registry.env.example": "SETTINGS_ENCRYPTION_KEY=\n",
		"agents.env.example":       "LANGSMITH_API_KEY=\n",
		"postgres-init/init.sql":   "-- init\n",
	}
	var tarBuf bytes.Buffer
	gz := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gz)
	for path, body := range files {
		hdr := &tar.Header{Name: path, Mode: 0644, Size: int64(len(body))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	tarName := fmt.Sprintf("specgate-compose_%s.tar.gz", version)
	sum := sha256.Sum256(tarBuf.Bytes())
	checksums := fmt.Sprintf("%x  %s\n", sum[:], tarName)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + version + "/" + tarName:
			_, _ = w.Write(tarBuf.Bytes())
		case "/" + version + "/" + fmt.Sprintf("specgate-compose_%s_checksums.txt", version):
			_, _ = io.WriteString(w, checksums)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDoctorFailsWhenBoardEndpointIsDown(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.statusErr = errors.New("Internal Server Error: governance-status")

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "doctor")

	if code == output.ExitOK {
		t.Fatalf("doctor reported OK while the board endpoint is failing:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "board") {
		t.Fatalf("output should name the board probe:\n%s", out.String())
	}
}

func TestOpenDeepLinkRequiresAdvertisedWebURL(t *testing.T) {
	t.Parallel()
	// A server that advertises no web_url: a deep link must error clearly
	// instead of pointing the browser at the API server.
	deps, fc, _, out := newFakeDeps(t)
	fc.metaResult = &client.Meta{APIVersion: "specgate.api/v1", Capabilities: map[string]bool{"agents": true}}
	opened := ""
	deps.Opener = func(u string) error { opened = u; return nil }

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "open", "CR-1")

	if code == output.ExitOK {
		t.Fatalf("deep link without web_url should fail, opened %q: %s", opened, out.String())
	}
	if opened != "" {
		t.Fatalf("browser should not open, got %q", opened)
	}
	if !strings.Contains(out.String(), "web UI URL") {
		t.Fatalf("error should explain the missing web UI URL: %s", out.String())
	}
}

func TestOpenBaseURLKeepsLegacyFallback(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	opened := ""
	deps.Opener = func(u string) error { opened = u; return nil }

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "open")

	if code != output.ExitOK {
		t.Fatalf("bare open should keep working: %s", out.String())
	}
	if opened == "" {
		t.Fatalf("bare open should still open the configured URL")
	}
}
