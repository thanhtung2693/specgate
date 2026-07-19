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
	"reflect"
	"strconv"
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
	Commands  []string
	Err       error
	OnCommand func()
	// OutputData, when non-nil, is returned by Output instead of "[]".
	OutputData      []byte
	OutputByCommand map[string][]byte
}

func (f *fakeDeployRunner) Run(_ context.Context, name string, args ...string) error {
	f.runHook()
	all := append([]string{name}, args...)
	f.Commands = append(f.Commands, strings.Join(all, " "))
	return f.Err
}

func (f *fakeDeployRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	f.runHook()
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

func (f *fakeDeployRunner) OutputToFile(ctx context.Context, path, name string, args ...string) error {
	data, err := f.Output(ctx, name, args...)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func (f *fakeDeployRunner) runHook() {
	if f.OnCommand == nil {
		return
	}
	hook := f.OnCommand
	f.OnCommand = nil
	hook()
}

var _ deploy.CommandRunner = (*fakeDeployRunner)(nil)

// setupTestBundle creates a minimal compose bundle in dir so Init can proceed.
func setupTestBundle(t *testing.T, dir string) {
	t.Helper()
	os.WriteFile(filepath.Join(dir, "compose.yml"), []byte("# placeholder\n"), 0644)
	os.WriteFile(filepath.Join(dir, "specgate.env.example"), []byte("SETTINGS_ENCRYPTION_KEY=\n"), 0644)
	if err := deploy.MarkManagedDirectory(dir); err != nil {
		t.Fatal(err)
	}
}

// fakeServer wires HTTP handlers into a test httptest.Server.
type fakeServer struct {
	metaHandler      http.HandlerFunc
	statusHandler    http.HandlerFunc
	settingsHandler  http.HandlerFunc
	schemaHandler    http.HandlerFunc
	workspaceHandler http.HandlerFunc
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
	if f.settingsHandler != nil {
		mux.HandleFunc("/settings", f.settingsHandler)
	}
	if f.schemaHandler != nil {
		mux.HandleFunc("/api/v1/schema/status", f.schemaHandler)
	}
	if f.workspaceHandler != nil {
		mux.HandleFunc("/api/v1/workspaces/", f.workspaceHandler)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func jsonMeta(apiVersion string, capabilities map[string]bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		details := make(map[string]map[string]string, len(capabilities))
		for name, available := range capabilities {
			state := "unavailable"
			if available {
				state = "available"
			}
			details[name] = map[string]string{"state": state}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body": map[string]any{
				"api_version":        apiVersion,
				"server_version":     "dev",
				"capability_details": details,
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
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{Mode: config.ModeLocal}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

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

func TestCommandsRejectMalformedConfigBeforeCallingServer(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = cfgPath

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "status", "--all-workspaces")
	if code != output.ExitUnavailable {
		t.Fatalf("exit = %d, want unavailable; output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("server called %d time(s) with malformed config", fc.calls)
	}
	if !strings.Contains(out.String(), "config") {
		t.Fatalf("output lacks config recovery context: %s", out.String())
	}
}

func TestCommandsRejectUnknownConfigModeBeforeCallingServer(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mode":"locla"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = cfgPath

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "status", "--all-workspaces")
	if code != output.ExitUnavailable {
		t.Fatalf("exit = %d, want unavailable; output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("server called %d time(s) with unknown config mode", fc.calls)
	}
	if !strings.Contains(out.String(), "local or full") {
		t.Fatalf("output lacks mode recovery context: %s", out.String())
	}
}

func TestVersionStillWorksWithMalformedConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath

	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "version"); code != output.ExitOK {
		t.Fatalf("exit = %d, want ok; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), `"command":"version"`) {
		t.Fatalf("unexpected version output: %s", out.String())
	}
}

func TestFlagErrorDoesNotColorRedirectedStderr(t *testing.T) {
	deps, _ := newTestDeps(t, "")
	var stderr bytes.Buffer
	deps.Stderr = &stderr
	deps.StdoutIsTTY = func() bool { return true }
	deps.StderrIsTTY = func() bool { return false }
	t.Setenv("CI", "")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--unknown")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "\x1b[") {
		t.Fatalf("redirected stderr contains ANSI: %q", stderr.String())
	}
}

func TestPositionalArgumentErrorJSONIsMachineReadable(t *testing.T) {
	deps, out := newTestDeps(t, "")
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "audit")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want %d; output = %s", code, output.ExitUsage, out.String())
	}
	var envelope struct {
		Command string              `json:"command"`
		Error   output.ErrorPayload `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal positional argument error: %v; output = %q", err, out.String())
	}
	if envelope.Command != "audit" || envelope.Error.Code != "usage" || !strings.Contains(envelope.Error.Message, "specgate audit --help") {
		t.Fatalf("unexpected positional argument error: %+v", envelope)
	}
}

func TestJSONProgressRequiresJSONOutput(t *testing.T) {
	deps, out := newTestDeps(t, "")
	deps.Stderr = out
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--json-progress", "version")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want %d; output = %s", code, output.ExitUsage, out.String())
	}
	if !strings.Contains(out.String(), "--json-progress requires --json") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestNoArgCommandRejectsUnexpectedArgument(t *testing.T) {
	for _, args := range [][]string{
		{"version", "extra"},
		{"feature", "extra"},
	} {
		deps, out := newTestDeps(t, "")
		deps.Stderr = out
		code := command.ExecuteForCode(command.NewRootCommand(deps), append([]string{"--plain"}, args...)...)
		if code != output.ExitUsage {
			t.Fatalf("%v: exit = %d, want %d; output = %s", args, code, output.ExitUsage, out.String())
		}
	}
}

func TestModelSetIgnoresRepositoryServerForEnvironmentSecrets(t *testing.T) {
	t.Setenv("SPECGATE_NO_UPDATE_CHECK", "1")
	t.Setenv("SPECGATE_SERVER", "")
	t.Setenv("GOOGLE_API_KEY", "environment-secret")

	attackerCalled := false
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attackerCalled = true
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer attacker.Close()

	var trustedSettings map[string]string
	trusted := (&fakeServer{settingsHandler: func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Settings map[string]string `json:"settings"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		trustedSettings = body.Settings
		_ = json.NewEncoder(w).Encode(map[string]any{"settings": body.Settings})
	}}).build(t)

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".specgate"), 0o755); err != nil {
		t.Fatal(err)
	}
	repoConfig, err := json.Marshal(config.RepoConfig{Server: attacker.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".specgate", "config"), repoConfig, 0o644); err != nil {
		t.Fatal(err)
	}

	deps, out := newTestDeps(t, "")
	deps.WorkingDir = repo
	if err := (config.Config{Mode: config.ModeFull, Server: trusted.URL}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input",
		"model", "set", "--provider", "google", "--model", "gemini-3.1-flash-lite")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if attackerCalled {
		t.Fatal("model setup sent a request to the repository-configured server")
	}
	if trustedSettings["google.api_key"] != "environment-secret" {
		t.Fatalf("trusted server settings = %#v", trustedSettings)
	}
}

func TestLocalWorkspaceCreateSelectAndCurrentNeedNoHTTP(t *testing.T) {
	deps, out := newTestDeps(t, "")
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "create", "Beta workspace"); code != output.ExitOK {
		t.Fatalf("create exit = %d; output=%s", code, out.String())
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "select", "beta-workspace"); code != output.ExitOK {
		t.Fatalf("select exit = %d; output=%s", code, out.String())
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "current"); code != output.ExitOK {
		t.Fatalf("current exit = %d; output=%s", code, out.String())
	}
	if deps.Client != nil {
		t.Fatal("Local workspace commands created an HTTP client")
	}
	if !strings.Contains(out.String(), "workspace: beta-workspace (Beta workspace)") {
		t.Fatalf("output = %s", out.String())
	}
}

func TestLocalUserLoginCreatesWorkspaceAndPersistsSelection(t *testing.T) {
	deps, out := newTestDeps(t, "")
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "user", "login", "--workspace", "Beta", "--display-name", "Second Human", "--username", "second-human"); code != output.ExitOK {
		t.Fatalf("login exit = %d; output=%s", code, out.String())
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "user", "current"); code != output.ExitOK {
		t.Fatalf("current exit = %d; output=%s", code, out.String())
	}
	if deps.Client != nil {
		t.Fatal("Local user commands created an HTTP client")
	}
	if !strings.Contains(out.String(), "user: second-human (Second Human)") {
		t.Fatalf("output = %s", out.String())
	}
	if !strings.Contains(out.String(), "Workspace set to beta") {
		t.Fatalf("output = %s", out.String())
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

func TestLocalWorkspaceBindPersistsProjectStoreAndSelection(t *testing.T) {
	deps, out := newTestDeps(t, "")
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	deps.WorkingDir = repo
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "workspace", "bind"); code != output.ExitOK {
		t.Fatalf("bind exit = %d; output=%s", code, out.String())
	}
	cfg, err := config.LoadFrom(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRepo, ok := config.FindProjectRoot(repo)
	if !ok {
		t.Fatal("project root not found")
	}
	project, ok := cfg.Projects[canonicalRepo]
	if !ok || project.Local.Path != stateDir || project.Local.ID != cfg.Local.ID || project.Workspace.Slug != "alpha" {
		t.Fatalf("project = %#v", project)
	}
	if !strings.Contains(out.String(), "Project workspace set to alpha") {
		t.Fatalf("output = %s", out.String())
	}
	ignore, err := os.ReadFile(filepath.Join(canonicalRepo, ".specgate", ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ignore), "*") || !strings.Contains(string(ignore), "!config") {
		t.Fatalf(".specgate/.gitignore = %q", ignore)
	}
}

func TestLocalProjectWorkspaceBindingOverridesGlobalSelection(t *testing.T) {
	deps, out := newTestDeps(t, "")
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	deps.WorkingDir = repo
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "create", "Beta"); code != output.ExitOK {
		t.Fatalf("workspace create exit = %d; output=%s", code, out.String())
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "bind", "alpha"); code != output.ExitOK {
		t.Fatalf("workspace bind exit = %d; output=%s", code, out.String())
	}
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "workspace", "select", "beta"); code != output.ExitOK {
		t.Fatalf("workspace select exit = %d; output=%s", code, out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "status"); code != output.ExitOK || !strings.Contains(out.String(), `"slug":"alpha"`) {
		t.Fatalf("project binding did not scope Local status: exit=%d output=%s", code, out.String())
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

func TestLocalArtifactPublishListAndShowNeedNoHTTP(t *testing.T) {
	deps, out := newTestDeps(t, "")
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	artifactPath := filepath.Join(t.TempDir(), "artifact.json")
	if err := os.WriteFile(artifactPath, []byte(`{"feature_key":"LOCAL-ARTIFACTS","request_type":"new_feature","documents":[{"path":"spec.md","role":"spec","content":"immutable body"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", artifactPath); code != output.ExitOK {
		t.Fatalf("publish exit = %d; output=%s", code, out.String())
	}
	var published struct {
		Data struct {
			ArtifactID string `json:"artifact_id"`
			Version    int    `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	if published.Data.ArtifactID == "" || published.Data.Version != 1 {
		t.Fatalf("published = %#v", published)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "show", published.Data.ArtifactID); code != output.ExitOK {
		t.Fatalf("show exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), "immutable body") {
		t.Fatalf("show output = %s", out.String())
	}
	if deps.Client != nil {
		t.Fatal("Local artifact commands created an HTTP client")
	}
}

func TestLocalWorkspaceOverrideScopesArtifactWithoutChangingSelection(t *testing.T) {
	deps, out := newTestDeps(t, "")
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	artifactPath := filepath.Join(t.TempDir(), "artifact.json")
	if err := os.WriteFile(artifactPath, []byte(`{"feature_key":"LOCAL-ISOLATION","request_type":"new_feature","documents":[{"path":"spec.md","role":"spec","content":"alpha only"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", artifactPath); code != output.ExitOK {
		t.Fatalf("publish exit = %d; output=%s", code, out.String())
	}
	var published struct {
		Data struct {
			ArtifactID string `json:"artifact_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "workspace", "create", "Beta"); code != output.ExitOK {
		t.Fatalf("workspace create exit = %d; output=%s", code, out.String())
	}
	out.Reset()
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--workspace", "beta", "artifact", "show", published.Data.ArtifactID)
	if code != output.ExitNotFound || !strings.Contains(out.String(), `"code":"not_found"`) {
		t.Fatalf("override show exit = %d; output=%s", code, out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "workspace", "current"); code != output.ExitOK || !strings.Contains(out.String(), `"slug":"alpha"`) {
		t.Fatalf("override changed selection: exit=%d output=%s", code, out.String())
	}
	if deps.Client != nil {
		t.Fatal("Local workspace override created an HTTP client")
	}
}

func TestLocalReadinessThenHumanApprovalNeedNoHTTP(t *testing.T) {
	deps, out := newTestDeps(t, "")
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	artifactPath := filepath.Join(t.TempDir(), "artifact.json")
	if err := os.WriteFile(artifactPath, []byte(`{"feature_key":"LOCAL-READINESS","request_type":"new_feature","documents":[{"path":"spec.md","role":"spec","content":"acceptance criteria"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", artifactPath); code != output.ExitOK {
		t.Fatalf("publish exit = %d; output=%s", code, out.String())
	}
	var published struct {
		Data struct {
			ArtifactID string `json:"artifact_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "gates", "check", published.Data.ArtifactID); code != output.ExitOK {
		t.Fatalf("readiness exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"aggregate":"not_run"`) {
		t.Fatalf("readiness output = %s", out.String())
	}
	submitLocalGateResults(t, deps, out, published.Data.ArtifactID)
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "approve", published.Data.ArtifactID); code != output.ExitUsage {
		t.Fatalf("unconfirmed change approve exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"code":"confirmation_required"`) {
		t.Fatalf("unconfirmed change approval output = %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "approve", published.Data.ArtifactID); code != output.ExitOK {
		t.Fatalf("change approve exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"state":"approved_and_canonical"`) || deps.Client != nil {
		t.Fatalf("change approval output = %s client=%#v", out.String(), deps.Client)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "create", "--feature", "LOCAL-READINESS", "--title", "Implement local flow", "--ac", "Context Pack is followed"); code != output.ExitOK {
		t.Fatalf("work create exit = %d; output=%s", code, out.String())
	}
	var work struct {
		Data struct {
			Key string `json:"key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &work); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "context", work.Data.Key); code != output.ExitOK {
		t.Fatalf("context exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"context_digest":`) || !strings.Contains(out.String(), "acceptance criteria") {
		t.Fatalf("context output = %s", out.String())
	}
	var contextPack struct {
		Data struct {
			Digest string `json:"context_digest"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &contextPack); err != nil {
		t.Fatal(err)
	}
	scaffoldPath := filepath.Join(t.TempDir(), "completion.json")
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "report", work.Data.Key, "--init", scaffoldPath); code != output.ExitOK {
		t.Fatalf("delivery scaffold exit = %d; output=%s", code, out.String())
	}
	if _, err := os.Stat(scaffoldPath); err != nil {
		t.Fatalf("Local space-separated --init path was not written: %v; output=%s", err, out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "status", work.Data.Key, "--detail"); code != output.ExitOK {
		t.Fatalf("empty delivery status exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"found":false`) || !strings.Contains(out.String(), "delivery report") {
		t.Fatalf("empty delivery status output = %s", out.String())
	}
	completionPath := filepath.Join(t.TempDir(), "completion.json")
	completion := fmt.Sprintf(`{"event_type":"coding_agent.completed","context_digest":%q,"agent":{"name":"builder"},"criteria":[{"criterion_id":"local-1","claim":"satisfied","evidence":{"summary":"implemented"}}],"checks":[{"name":"unit","status":"skipped"}]}`, contextPack.Data.Digest)
	if err := os.WriteFile(completionPath, []byte(completion), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", work.Data.Key, "--file", completionPath); code != output.ExitOK {
		t.Fatalf("delivery submit exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"verdict":"passed"`) {
		t.Fatalf("delivery submit output = %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "status", work.Data.Key, "--detail"); code != output.ExitOK {
		t.Fatalf("plain delivery status exit = %d; output=%s", code, out.String())
	}
	for _, want := range []string{
		"Evidence: Ready for human review",
		"Assurance: Agent-reported",
		"Decision: Awaiting human acceptance",
		"Receipt: commit ",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("plain delivery status missing %q: %s", want, out.String())
		}
	}
	peerPath := filepath.Join(t.TempDir(), "peer.json")
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "peer-review", work.Data.Key, "--init="+peerPath); code != output.ExitOK {
		t.Fatalf("peer review init exit = %d; output=%s", code, out.String())
	}
	var peer map[string]any
	data, err := os.ReadFile(peerPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &peer); err != nil {
		t.Fatal(err)
	}
	peer["agent"] = map[string]any{"name": "reviewer"}
	criteria, _ := peer["criteria"].([]any)
	criteria[0].(map[string]any)["claim"] = "satisfied"
	criteria[0].(map[string]any)["evidence"] = map[string]any{"summary": "reviewed implementation"}
	data, err = json.Marshal(peer)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(peerPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "peer-review", work.Data.Key, "--file", peerPath); code != output.ExitOK {
		t.Fatalf("peer review exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"agent_name":"reviewer"`) || deps.Client != nil {
		t.Fatalf("peer review output = %s client=%#v", out.String(), deps.Client)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "status", work.Data.Key, "--detail"); code != output.ExitOK {
		t.Fatalf("delivery status exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"peer_review":{"state":"passed"`) {
		t.Fatalf("delivery status missing peer review state: %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "delivery", "approve", work.Data.Key); code != output.ExitOK {
		t.Fatalf("delivery approve exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"human_decision":"approve"`) {
		t.Fatalf("delivery approval output = %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", work.Data.Key, "--file", completionPath); code != output.ExitConflict || !strings.Contains(out.String(), `"code":"conflict"`) {
		t.Fatalf("post-approval delivery submit exit=%d output=%s", code, out.String())
	}
}

func TestLocalPluginRequiresExplicitQuickWorkCriteria(t *testing.T) {
	body, err := os.ReadFile("local_plugin_assets/skills/specgate-work-preparation/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "### Quick work") ||
		!strings.Contains(string(body), "Local mode has no platform model to draft missing criteria") {
		t.Fatalf("Local plugin does not explain Local quick-work criteria:\n%s", body)
	}
}

func TestLocalPluginRequiresIDEAgentConfirmedAcceptanceCriteria(t *testing.T) {
	body, err := os.ReadFile("local_plugin_assets/skills/specgate-work-preparation/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "at least one explicit `--ac`") {
		t.Fatalf("Local plugin does not document explicit IDE-agent criteria:\n%s", body)
	}
}

func submitLocalGateResults(t *testing.T, deps *command.Deps, out *bytes.Buffer, artifactID string) {
	t.Helper()
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "gates", "tasks", "list", artifactID); code != output.ExitOK {
		t.Fatalf("list exit=%d output=%s", code, out.String())
	}
	var listed struct {
		Data struct {
			Tasks []client.GateTask `json:"tasks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	for _, task := range listed.Data.Tasks {
		resultPath := filepath.Join(t.TempDir(), task.TaskID+".json")
		body := fmt.Sprintf(`{"gate":%q,"gate_digest":%q,"input_digest":%q,"state":"pass","evaluator":{"executor":"ide_agent"}}`, task.GateKey, task.GateDigest, task.ArtifactDigest)
		if err := os.WriteFile(resultPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		out.Reset()
		if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "gates", "tasks", "submit-result", task.TaskID, "--file", resultPath); code != output.ExitOK {
			t.Fatalf("submit exit=%d output=%s", code, out.String())
		}
	}
}

func TestLocalGateTaskLoopNeedsNoHTTP(t *testing.T) {
	deps, out := newTestDeps(t, "")
	stateDir := filepath.Join(t.TempDir(), "local")
	execute := func(args ...string) int {
		out.Reset()
		return command.ExecuteForCode(command.NewRootCommand(deps), args...)
	}
	if code := execute("--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit=%d output=%s", code, out.String())
	}
	artifactPath := filepath.Join(t.TempDir(), "artifact.json")
	if err := os.WriteFile(artifactPath, []byte(`{"feature_key":"LOCAL-GATES","request_type":"new_feature","documents":[{"path":"spec.md","role":"spec","content":"# Local gates\n\nAcceptance criteria: observable"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := execute("--json", "artifact", "publish", "--file", artifactPath); code != output.ExitOK {
		t.Fatalf("publish exit=%d output=%s", code, out.String())
	}
	var published struct {
		Data struct {
			ArtifactID string `json:"artifact_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	if code := execute("--json", "gates", "check", published.Data.ArtifactID, "--summary"); code != output.ExitOK {
		t.Fatalf("check exit=%d output=%s", code, out.String())
	}
	for _, want := range []string{`"aggregate":"not_run"`, `"dispatched_to_ide_agent"`, `"pending_task_ids"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("check output missing %s: %s", want, out.String())
		}
	}
	if code := execute("--json", "gates", "tasks", "list", published.Data.ArtifactID); code != output.ExitOK {
		t.Fatalf("list exit=%d output=%s", code, out.String())
	}
	if deps.Client != nil {
		t.Fatal("Local gate tasks created an HTTP client")
	}
	var listed struct {
		Data struct {
			Tasks []client.GateTask `json:"tasks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Data.Tasks) != 4 {
		t.Fatalf("tasks = %#v", listed.Data.Tasks)
	}
	for _, task := range listed.Data.Tasks {
		resultPath := filepath.Join(t.TempDir(), task.TaskID+".json")
		body := fmt.Sprintf(`{"gate":%q,"gate_digest":%q,"input_digest":%q,"state":"pass","evaluator":{"executor":"ide_agent"}}`, task.GateKey, task.GateDigest, task.ArtifactDigest)
		if err := os.WriteFile(resultPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if code := execute("--json", "gates", "tasks", "submit-result", task.TaskID, "--file", resultPath); code != output.ExitOK {
			t.Fatalf("submit exit=%d output=%s", code, out.String())
		}
	}
	if code := execute("--json", "gates", "results", published.Data.ArtifactID); code != output.ExitOK {
		t.Fatalf("results exit=%d output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"trust":"agent_attested"`) {
		t.Fatalf("results = %s", out.String())
	}
	if code := execute("--json", "gates", "check", published.Data.ArtifactID, "--summary"); code != output.ExitOK {
		t.Fatalf("second check exit=%d output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"aggregate":"pass"`) {
		t.Fatalf("second check = %s", out.String())
	}
	if code := execute("--json", "--yes", "artifact", "approve", published.Data.ArtifactID); code != output.ExitOK {
		t.Fatalf("approve exit=%d output=%s", code, out.String())
	}
}

func TestLocalWorkCreatePersistsIDEAgentConfirmedAcceptanceCriteria(t *testing.T) {
	deps, out := newTestDeps(t, "")
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	artifactPath := filepath.Join(t.TempDir(), "artifact.json")
	artifact := `{"feature_key":"LOCAL-EXPLICIT-AC","request_type":"new_feature","documents":[
		{"path":"spec.md","role":"spec","content":"# Contract\n\nThe source format is intentionally not interpreted by the CLI."},
		{"path":"notes.md","role":"design","content":"Acceptance criteria are confirmed by the IDE agent."}
	]}`
	if err := os.WriteFile(artifactPath, []byte(artifact), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", artifactPath); code != output.ExitOK {
		t.Fatalf("publish exit = %d; output=%s", code, out.String())
	}
	var published struct {
		Data struct {
			ArtifactID string `json:"artifact_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "gates", "check", published.Data.ArtifactID); code != output.ExitOK {
		t.Fatalf("readiness exit = %d; output=%s", code, out.String())
	}
	submitLocalGateResults(t, deps, out, published.Data.ArtifactID)
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "artifact", "approve", published.Data.ArtifactID); code != output.ExitOK {
		t.Fatalf("approve exit = %d; output=%s", code, out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "artifact", "promote", published.Data.ArtifactID); code != output.ExitOK {
		t.Fatalf("promote exit = %d; output=%s", code, out.String())
	}
	var promoted struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &promoted); err != nil {
		t.Fatal(err)
	}
	if promoted.Data.ID == "" {
		t.Fatalf("promote omitted feature id: %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "feature", "list", "--search", "explicit",
	); code != output.ExitOK {
		t.Fatalf("Local feature list exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"key":"LOCAL-EXPLICIT-AC"`) {
		t.Fatalf("Local feature list omitted promoted feature: %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "feature", "show", promoted.Data.ID,
	); code != output.ExitOK {
		t.Fatalf("Local feature show exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"canonical_artifact_id":"`+published.Data.ArtifactID+`"`) {
		t.Fatalf("Local feature show omitted canonical artifact: %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "feature", "show", "missing-feature",
	); code != output.ExitNotFound {
		t.Fatalf("missing Local feature exit = %d, want %d; output=%s", code, output.ExitNotFound, out.String())
	}
	if !strings.Contains(out.String(), "specgate feature list --all") {
		t.Fatalf("missing Local feature recovery is not actionable: %s", out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "work", "create",
		"--feature", promoted.Data.ID,
		"--title", "Persist confirmed criteria",
		"--ac", "First criterion",
		"--ac", "Shared criterion",
		"--ac", "Final criterion",
	); code != output.ExitOK {
		t.Fatalf("work create exit = %d; output=%s", code, out.String())
	}
	var work struct {
		Data struct {
			ID                 string   `json:"id"`
			Key                string   `json:"key"`
			ChangeRequestID    string   `json:"change_request_id"`
			ChangeRequestKey   string   `json:"change_request_key"`
			FeatureKey         string   `json:"feature_key"`
			ArtifactID         string   `json:"artifact_id"`
			LeadArtifactID     string   `json:"lead_artifact_id"`
			AcceptanceCriteria []string `json:"acceptance_criteria"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &work); err != nil {
		t.Fatal(err)
	}
	want := []string{"First criterion", "Shared criterion", "Final criterion"}
	if !reflect.DeepEqual(work.Data.AcceptanceCriteria, want) {
		t.Fatalf("criteria = %#v, want %#v", work.Data.AcceptanceCriteria, want)
	}
	if work.Data.ChangeRequestID != work.Data.ID ||
		work.Data.ChangeRequestKey != work.Data.Key ||
		work.Data.FeatureKey != "LOCAL-EXPLICIT-AC" ||
		work.Data.LeadArtifactID != work.Data.ArtifactID {
		t.Fatalf("Local work JSON aliases do not match Full mode: %+v", work.Data)
	}
	if deps.Client != nil {
		t.Fatal("Local work create created an HTTP client")
	}
}

func TestLocalPluginInstallUsesEmbeddedPackageWithoutHTTP(t *testing.T) {
	deps, out := newTestDeps(t, "")
	stateDir := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "init", "--mode", "local", "--local-dir", stateDir, "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != output.ExitOK {
		t.Fatalf("init exit = %d; output=%s", code, out.String())
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "plugins", "install", "--agent", "codex"); code != output.ExitOK {
		t.Fatalf("plugin install exit = %d; output=%s", code, out.String())
	}
	if !strings.Contains(out.String(), `"agents":["codex"]`) || deps.Client != nil {
		t.Fatalf("plugin output = %s client=%#v", out.String(), deps.Client)
	}
	home, err := deps.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	var installedSkills string
	for _, skill := range []string{"specgate-router", "specgate-project-setup", "specgate-work-preparation", "specgate-work-delivery"} {
		body, err := os.ReadFile(filepath.Join(home, ".codex", "plugins", "specgate", "skills", skill, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		installedSkills += string(body)
	}
	for _, want := range []string{"data.mode", "gates tasks list", "gates tasks submit-result", "different review-only agent"} {
		if !strings.Contains(installedSkills, want) {
			t.Fatalf("installed Local skills missing %q", want)
		}
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "plugins", "install", "--agent", "cursor,claude"); code != output.ExitOK {
		t.Fatalf("Local Cursor/Claude install exit=%d output=%s", code, out.String())
	}
	if deps.Client != nil {
		t.Fatal("Local plugin install created an HTTP client")
	}
	for _, path := range []string{
		filepath.Join(home, ".cursor", "rules", "using-specgate.mdc"),
		filepath.Join(home, ".claude", "skills", "specgate", ".claude-plugin", "plugin.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("embedded Local plugin missing %s: %v", path, err)
		}
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "plugins", "doctor", "--agent", "all"); code != output.ExitOK {
		t.Fatalf("Local all-agent plugin doctor exit=%d output=%s", code, out.String())
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
	homeDir := t.TempDir()
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
		UserHomeDir: func() (string, error) {
			return homeDir, nil
		},
	}
	_ = srvURL // deps.Client left nil → PersistentPreRunE constructs it
	return deps, &out
}

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

func TestRootHelpIsStyledOnlyOnTTY(t *testing.T) {
	for _, tc := range []struct {
		name     string
		tty      bool
		wantANSI bool
	}{
		{name: "rich", tty: true, wantANSI: true},
		{name: "portable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CI", "")
			t.Setenv("NO_COLOR", "")
			t.Setenv("TERM", "xterm-256color")
			deps, out := newTestDeps(t, "")
			deps.StdoutIsTTY = func() bool { return tc.tty }
			if code := command.ExecuteForCode(command.NewRootCommand(deps), "--help"); code != output.ExitOK {
				t.Fatalf("exit = %d, output = %s", code, out.String())
			}
			got := out.String()
			for _, want := range []string{"Usage:", "Core workflow commands", "Additional Commands:"} {
				if !strings.Contains(got, want) {
					t.Fatalf("help missing %q: %q", want, got)
				}
			}
			if strings.Contains(got, "\x1b[") != tc.wantANSI {
				t.Fatalf("ANSI = %t, want %t: %q", strings.Contains(got, "\x1b["), tc.wantANSI, got)
			}
		})
	}
}

func TestRootHelpStartsWithChangeFacade(t *testing.T) {
	deps, out := newTestDeps(t, "")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--help"); code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	help := out.String()
	start := strings.Index(help, "Start here")
	core := strings.Index(help, "Core workflow commands")
	if start == -1 || core == -1 || start >= core {
		t.Fatalf("root help should put the start-here facade before core workflow commands:\n%s", help)
	}
	if !strings.Contains(help[start:core], "\n  change") {
		t.Fatalf("start-here help should contain change:\n%s", help)
	}
}

func TestLocalParentCommandsShowHelp(t *testing.T) {
	for _, family := range []string{"artifact", "delivery", "gates", "plugins", "portable"} {
		t.Run(family, func(t *testing.T) {
			deps, out := newTestDeps(t, "")
			deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
			if err := (config.Config{Mode: config.ModeLocal}).SaveTo(deps.ConfigPath); err != nil {
				t.Fatal(err)
			}

			code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", family)
			if code != output.ExitOK {
				t.Fatalf("bare Local command exit = %d, output = %s", code, out.String())
			}
			if !strings.Contains(out.String(), "Usage:") {
				t.Fatalf("bare Local command did not show help: %s", out.String())
			}
			if strings.Contains(out.String(), "requires Full mode") {
				t.Fatalf("bare Local command was rejected as Full-only: %s", out.String())
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

// TestDoctorGreenExitsOK verifies doctor exits 0 when server is healthy.
func TestDoctorFixRejectsLocalMode(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{
		Mode:  config.ModeLocal,
		Local: config.LocalStore{Path: filepath.Join(t.TempDir(), "local")},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "doctor", "--fix")
	if code != output.ExitIncompatible {
		t.Fatalf("exit = %d, want %d, output = %s", code, output.ExitIncompatible, out.String())
	}
	if !strings.Contains(out.String(), "Full appliance") || !strings.Contains(out.String(), "specgate doctor") {
		t.Fatalf("output should explain the mode boundary and recovery command: %s", out.String())
	}
}

func TestDoctorReportsKnowledgeEmbeddingsMissing(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
		settingsHandler: func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"settings": map[string]string{
					"governance.model_provider": "google_genai",
					"governance.model":          "gemini-3.1-flash-lite",
					"google.api_key":            "***",
					"embedding.model_provider":  "",
					"embedding.model":           "",
				},
			})
		},
	}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "doctor")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		Data struct {
			KnowledgeEmbeddings struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Command string `json:"command"`
			} `json:"knowledge_embeddings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("doctor JSON decode failed: %v; output=%s", err, out.String())
	}
	if env.Data.KnowledgeEmbeddings.Status != "missing" {
		t.Fatalf("knowledge_embeddings = %+v, want missing", env.Data.KnowledgeEmbeddings)
	}
	if !strings.Contains(env.Data.KnowledgeEmbeddings.Message, "Knowledge embeddings are not configured") {
		t.Fatalf("unexpected embedding message: %+v", env.Data.KnowledgeEmbeddings)
	}
	if env.Data.KnowledgeEmbeddings.Command != "specgate model set" {
		t.Fatalf("embedding command = %q", env.Data.KnowledgeEmbeddings.Command)
	}
}

// TestDoctorReportsKnowledgeEmbeddingsConfigured proves the key-presence
// normalization: a provider + model + non-empty (redacted) api_key reads as ok,
// using the same rule as the model check — not a comparison against "set".
func TestDoctorReportsKnowledgeEmbeddingsConfigured(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
		settingsHandler: func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"settings": map[string]string{
					"governance.model_provider": "google_genai",
					"governance.model":          "gemini-3.1-flash-lite",
					"google.api_key":            "***",
					"embedding.model_provider":  "openai",
					"embedding.model":           "text-embedding-3-small",
					"openai.api_key":            "***",
				},
			})
		},
	}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "doctor")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		Data struct {
			KnowledgeEmbeddings struct {
				Status  string `json:"status"`
				Message string `json:"message"`
			} `json:"knowledge_embeddings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("doctor JSON decode failed: %v; output=%s", err, out.String())
	}
	if env.Data.KnowledgeEmbeddings.Status != "ok" {
		t.Fatalf("knowledge_embeddings = %+v, want ok", env.Data.KnowledgeEmbeddings)
	}
	if !strings.Contains(env.Data.KnowledgeEmbeddings.Message, "openai") || !strings.Contains(env.Data.KnowledgeEmbeddings.Message, "text-embedding-3-small") {
		t.Fatalf("embedding message should name provider+model: %+v", env.Data.KnowledgeEmbeddings)
	}
}

func TestDoctorGreenExitsOK(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
	}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "doctor")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Mode   string `json:"mode"`
			Server struct {
				Status string `json:"status"`
			} `json:"server"`
			Plugins struct {
				Command string `json:"command"`
			} `json:"plugins"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("doctor JSON decode failed: %v; output=%s", err, out.String())
	}
	if !env.OK || env.Data.Mode != "full" || env.Data.Server.Status != "ok" || env.Data.Plugins.Command != "specgate plugins doctor" {
		t.Fatalf("doctor JSON should expose setup summary, got: %s", out.String())
	}
}

func TestDoctorReportsIncompatibleDatabaseSchema(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
		schemaHandler: func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("workspace_id"); got != "ws-1" {
				http.Error(w, "workspace_id is required", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":  "incompatible",
				"message": "database schema is missing required artifact columns: artifacts.policy_snapshot_json",
			})
		},
	}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	if err := (config.Config{Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "doctor")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		Data struct {
			DatabaseSchema struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Command string `json:"command"`
			} `json:"database_schema"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("doctor JSON decode failed: %v; output=%s", err, out.String())
	}
	if env.Data.DatabaseSchema.Status != "incompatible" {
		t.Fatalf("database_schema = %+v, want incompatible", env.Data.DatabaseSchema)
	}
	if !strings.Contains(env.Data.DatabaseSchema.Message, "artifacts.policy_snapshot_json") {
		t.Fatalf("database schema message missing required column: %+v", env.Data.DatabaseSchema)
	}
	if env.Data.DatabaseSchema.Command != "specgate doctor" {
		t.Fatalf("database schema command = %q", env.Data.DatabaseSchema.Command)
	}
}

func TestDoctorShowsActionableSetupSummary(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
		statusHandler: func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("workspace_id"); got != "ws-1" {
				http.Error(w, "workspace_id is required", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"counts":{},"attention":[]}`))
		},
		settingsHandler: func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"settings": map[string]string{
					"governance.model_provider":         "google_genai",
					"governance.model":                  "gemini-3.1-flash-lite",
					"governance.default_thinking_level": "medium",
					"google.api_key":                    "***",
				},
			})
		},
	}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	if err := (config.Config{
		CurrentUser: config.CurrentUser{ID: "user-1", Username: "thanhtung2693", DisplayName: "Thanh Tung"},
		Workspace:   config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "doctor")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Server:",
		"Identity:",
		"Workspace:",
		"Model:",
		// The Model line surfaces the thinking-level lever so operators can see
		// and tune the governance judgment budget.
		"thinking: medium",
		"IDE plugins:",
		"Next:",
		"specgate plugins doctor",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, got)
		}
	}
}

// TestDoctorWarnsSelfAttestedWhenModelKeyMissing: with a provider/model set
// but no API key, the Model line must state the governance consequence —
// self-attested delivery verdicts and no server-side model review — not just
// that a key is missing.
func TestDoctorWarnsSelfAttestedWhenModelKeyMissing(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
		settingsHandler: func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"settings": map[string]string{
					"governance.model_provider": "google_genai",
					"governance.model":          "gemini-3.1-flash-lite",
					"google.api_key":            "",
				},
			})
		},
	}).build(t)

	deps, out := newTestDeps(t, srv.URL)
	if err := (config.Config{
		CurrentUser: config.CurrentUser{ID: "user-1", Username: "thanhtung2693", DisplayName: "Thanh Tung"},
		Workspace:   config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "doctor") //nolint:errcheck
	got := out.String()
	for _, want := range []string{"self-attested", "server-side model review", "specgate model set", "specgate gates tasks dispatch <artifact-id>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, got)
		}
	}
}

// TestDoctorShowsFullApplianceSection verifies doctor renders a "Full appliance"
// section (same data as `local-status`) when a CLI-managed deployment exists.
func TestDoctorShowsFullApplianceSection(t *testing.T) {
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
	for _, want := range []string{"Full appliance", dir, "doc-registry", "running (healthy)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

// TestDoctorSkipsFullApplianceWithoutDeployment verifies doctor omits the Full
// appliance section when no CLI-managed deployment exists.
func TestDoctorSkipsFullApplianceWithoutDeployment(t *testing.T) {
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
	if strings.Contains(out.String(), "Full appliance") {
		t.Fatalf("output should not contain a Full appliance section:\n%s", out.String())
	}
}

func TestDoctorFixOffersFullApplianceRepairChecklist(t *testing.T) {
	var statusCalls int
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
		statusHandler: func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("workspace_id"); got != "ws-1" {
				http.Error(w, "workspace_id is required", http.StatusBadRequest)
				return
			}
			statusCalls++
			if statusCalls == 1 {
				http.Error(w, "starting", http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(`{"counts":{},"attention":[]}`))
		},
	}).build(t)

	dir := t.TempDir()
	setupTestBundle(t, dir)

	runner := &fakeDeployRunner{}
	prompter := &fakePrompter{multiValues: []string{"full-appliance"}}
	deps, out := newTestDeps(t, srv.URL)
	deps.DeployRunner = runner
	deps.Prompter = prompter
	if err := (config.Config{DeploymentDir: dir, Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--server", srv.URL, "doctor", "--fix")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if prompter.multiTitle != "Repair SpecGate environment" {
		t.Fatalf("multi-select title = %q", prompter.multiTitle)
	}
	gotCommands := strings.Join(runner.Commands, "\n")
	if !strings.Contains(gotCommands, "docker compose -f "+filepath.Join(dir, "compose.yml")+" up -d --wait") {
		t.Fatalf("missing compose up repair command in:\n%s", gotCommands)
	}
	if statusCalls < 2 {
		t.Fatalf("doctor should re-run checks after repair; status calls = %d", statusCalls)
	}
	if !strings.Contains(out.String(), "Environment repaired successfully.") {
		t.Fatalf("output missing repaired message:\n%s", out.String())
	}
}

func TestDoctorFixYesRepairsWithoutPrompt(t *testing.T) {
	var statusCalls int
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
		statusHandler: func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("workspace_id"); got != "ws-1" {
				http.Error(w, "workspace_id is required", http.StatusBadRequest)
				return
			}
			statusCalls++
			if statusCalls == 1 {
				http.Error(w, "starting", http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(`{"counts":{},"attention":[]}`))
		},
	}).build(t)

	dir := t.TempDir()
	setupTestBundle(t, dir)

	runner := &fakeDeployRunner{}
	prompter := &fakePrompter{}
	deps, out := newTestDeps(t, srv.URL)
	deps.DeployRunner = runner
	deps.Prompter = prompter
	if err := (config.Config{DeploymentDir: dir, Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "--server", srv.URL, "doctor", "--fix")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if prompter.multiTitle != "" {
		t.Fatalf("--yes should not prompt, got title %q", prompter.multiTitle)
	}
	gotCommands := strings.Join(runner.Commands, "\n")
	if !strings.Contains(gotCommands, "docker compose -f "+filepath.Join(dir, "compose.yml")+" up -d --wait") {
		t.Fatalf("missing compose up repair command in:\n%s", gotCommands)
	}
	if statusCalls < 2 {
		t.Fatalf("doctor should re-run checks after repair; status calls = %d", statusCalls)
	}
}

func TestServerCommandWarnsWhenCLIBehindRecommendedVersion(t *testing.T) {
	oldVersion := buildinfo.Version
	buildinfo.Version = "v9.9.0-rc.1"
	t.Cleanup(func() { buildinfo.Version = oldVersion })

	var stderr bytes.Buffer
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/meta", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body": map[string]any{
				"api_version":             "specgate.api/v1",
				"version":                 "v9.9.0-rc.2",
				"recommended_cli_version": "v9.9.0-rc.2",
				"capabilities":            map[string]bool{"agents": true},
			},
		})
	})
	mux.HandleFunc("/api/v1/status", jsonStatus(1, 0))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	deps, _ := newTestDeps(t, srv.URL)
	deps.Stderr = &stderr
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "status", "--all-workspaces")
	if code != output.ExitOK {
		t.Fatalf("exit = %d", code)
	}
	got := stderr.String()
	if !strings.Contains(got, "specgate CLI v9.9.0-rc.1") ||
		!strings.Contains(got, "v9.9.0-rc.2") ||
		!strings.Contains(got, "specgate update") {
		t.Fatalf("missing update warning, got %q", got)
	}
}

func TestJSONModeSuppressesCLIUpdateWarning(t *testing.T) {
	oldVersion := buildinfo.Version
	buildinfo.Version = "v9.9.0-rc.1"
	t.Cleanup(func() { buildinfo.Version = oldVersion })

	var stderr bytes.Buffer
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/meta", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"body": map[string]any{
				"api_version":             "specgate.api/v1",
				"recommended_cli_version": "v9.9.0-rc.2",
				"capabilities":            map[string]bool{"agents": true},
			},
		})
	})
	mux.HandleFunc("/api/v1/status", jsonStatus(1, 0))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	deps, _ := newTestDeps(t, srv.URL)
	deps.Stderr = &stderr
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "status", "--all-workspaces")
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
	buildinfo.Version = "v9.9.0-rc.1"
	t.Cleanup(func() { buildinfo.Version = oldVersion })

	var stderr bytes.Buffer
	srv := (&fakeServer{
		metaHandler:   jsonMeta("specgate.api/v1", map[string]bool{"agents": true}),
		statusHandler: jsonStatus(1, 0),
	}).build(t)

	deps, _ := newTestDeps(t, srv.URL)
	deps.Stderr = &stderr
	deps.CheckLatestRelease = func(context.Context, time.Duration, string) (string, error) {
		return "v9.9.0-rc.2", nil
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "status", "--all-workspaces")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := stderr.String()
	if !strings.Contains(got, "latest GitHub release v9.9.0-rc.2") ||
		!strings.Contains(got, "scripts/install-cli.sh") {
		t.Fatalf("missing GitHub release warning, got %q", got)
	}
}

func TestGitHubReleaseWarningCanBeDisabled(t *testing.T) {
	t.Setenv("CI", "")
	oldVersion := buildinfo.Version
	buildinfo.Version = "v9.9.0-rc.1"
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
		return "v9.9.0-rc.2", nil
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "status", "--all-workspaces")
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

// TestDoctorAgentsUnavailableExits5 verifies agents.state=unavailable → exit 5.
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

func TestDoctorMissingAgentsCapabilityExits6(t *testing.T) {
	t.Parallel()
	srv := (&fakeServer{
		metaHandler: jsonMeta("specgate.api/v1", nil),
	}).build(t)

	deps, _ := newTestDeps(t, srv.URL)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL, "doctor")
	if code != output.ExitIncompatible {
		t.Fatalf("exit = %d, want %d (ExitIncompatible)", code, output.ExitIncompatible)
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

func TestDoctorUsesInContainerDiagnosticsWhenGatewayProbeFails(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/doc-registry/healthz/components" {
			http.Error(w, "gateway unavailable", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	deployDir := t.TempDir()
	setupTestBundle(t, deployDir)
	runner := &fakeDeployRunner{OutputByCommand: map[string][]byte{}}
	commandLine := "docker compose -f " + filepath.Join(deployDir, "compose.yml") + " exec -T specgate curl --fail --silent --show-error --max-time 5 http://127.0.0.1:9090/healthz/components"
	runner.OutputByCommand[commandLine] = []byte(`{"status":"degraded","components":{"nginx":{"status":"fail","state":"failed"}}}`)
	deps, out := newTestDeps(t, srv.URL+"/api/doc-registry")
	deps.DeployRunner = runner
	if err := (config.Config{DeploymentDir: deployDir}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", srv.URL+"/api/doc-registry", "doctor")
	if code != output.ExitUnavailable {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "nginx=fail") {
		t.Fatalf("doctor output does not identify nginx: %s", out.String())
	}
}

// TestConfigSetServerPersists verifies config server writes the URL to the config file.
func TestConfigSetServerPersists(t *testing.T) {
	t.Parallel()
	cfgPath := filepath.Join(t.TempDir(), "config.json")

	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath
	// config server doesn't call the API, so we don't need a real server.
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", "http://localhost:8080", "config", "server", "https://my.specgate.example")
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

func TestConfigSetServerRejectsMalformedConfigWithoutReplacingIt(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "config", "server", "https://example.test")
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
}

func TestConfigSetServerPathIsRemoved(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "config", "set", "server", "https://example.test")
	if code == output.ExitOK {
		t.Fatalf("removed config set server path still succeeds: %s", out.String())
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
	if _, err := os.Stat(filepath.Join(homeDir, ".codex", "plugins", "specgate", "skills", "specgate-router", "SKILL.md")); err != nil {
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
		".cursor/skills/specgate-router/SKILL.md",
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
			_, _ = io.WriteString(w, `{"name":"specgate","version":"0.1.0","skills":["specgate-router"]}`)
		case "/api/doc-registry/plugins/rules/using-specgate.mdc":
			_, _ = io.WriteString(w, "use specgate\n")
		case "/api/doc-registry/plugins/skills/specgate-router/SKILL.md":
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

func TestUninstallKeepsDataByDefaultAndRemovesUserFiles(t *testing.T) {
	dir := t.TempDir()
	setupTestBundle(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "data.txt"), []byte("keep me"), 0644); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, ".cursor", "rules", "using-specgate.mdc"), "rule")
	writeTestFile(t, filepath.Join(home, ".cursor", "rules", "using-specgate.mdc.specgate-owned"), "specgate-plugin-v1\n")
	writeTestFile(t, filepath.Join(home, ".cursor", "skills", "specgate-router", "SKILL.md"), "skill")
	writeTestFile(t, filepath.Join(home, ".cursor", "skills", "specgate-router", ".specgate-owned"), "specgate-plugin-v1\n")
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
		filepath.Join(home, ".cursor", "skills", "specgate-router"),
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

func TestUninstallRemovesEmptyPluginConfigFiles(t *testing.T) {
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

func TestUpdateJSONProgressEmitsEventsAndFinalEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cli/install.sh", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "#!/usr/bin/env sh\nexit 0\n")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	plugins := newPluginRegistry(t)

	deps, out := newTestDeps(t, srv.URL)
	deps.CLIInstallURL = srv.URL + "/cli/install.sh"
	deps.PublicRegistryURL = plugins.URL
	deps.PluginRegistryURL = srv.URL
	home := t.TempDir()
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--json-progress", "--server", srv.URL, "update", "--version", "v9.9.9")
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

func TestLocalUpdateRefreshesCLIWithoutInspectingOrUpdatingAppliance(t *testing.T) {
	cliSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "#!/usr/bin/env sh\nexit 0\n")
	}))
	defer cliSrv.Close()
	deployDir := t.TempDir()
	setupTestBundle(t, deployDir)
	runner := &fakeDeployRunner{}
	deps, out := newTestDeps(t, "")
	deps.CLIInstallURL = cliSrv.URL
	deps.DeployRunner = runner
	deps.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: t.TempDir()}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "update", "--version", "v9.9.9", "--dir", deployDir,
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if len(runner.Commands) != 0 {
		t.Fatalf("Local update ran appliance commands: %v", runner.Commands)
	}
	var envelope struct {
		Data struct {
			Steps []struct {
				ID      string `json:"id"`
				Status  string `json:"status"`
				Message string `json:"message"`
			} `json:"steps"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v; output=%s", err, out.String())
	}
	stack := envelope.Data.Steps[3]
	if stack.ID != "update_stack" || stack.Status != "skipped" || !strings.Contains(stack.Message, "Local mode") {
		t.Fatalf("stack step = %#v", stack)
	}
}

func TestUpdateVersionResolutionFailurePrecedesMutationInEveryMode(t *testing.T) {
	for _, mode := range []config.Mode{config.ModeLocal, config.ModeFull} {
		t.Run(string(mode), func(t *testing.T) {
			installerHits := 0
			cliSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				installerHits++
				_, _ = io.WriteString(w, "#!/usr/bin/env sh\nexit 0\n")
			}))
			defer cliSrv.Close()

			deployDir := t.TempDir()
			setupTestBundle(t, deployDir)
			cfg := config.Config{Mode: mode, DeploymentDir: deployDir}
			if mode == config.ModeLocal {
				cfg.Local.Path = t.TempDir()
				cfg.DeploymentDir = ""
			}
			deps, out := newTestDeps(t, "")
			if err := cfg.SaveTo(deps.ConfigPath); err != nil {
				t.Fatal(err)
			}
			runner := &fakeDeployRunner{}
			deps.CLIInstallURL = cliSrv.URL
			deps.DeployRunner = runner
			deps.CheckLatestRelease = func(context.Context, time.Duration, string) (string, error) {
				return "", errors.New("release lookup failed")
			}

			code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "update")
			if code != output.ExitUnavailable {
				t.Fatalf("exit = %d, want unavailable; output = %s", code, out.String())
			}
			if installerHits != 0 {
				t.Fatalf("CLI installer ran %d time(s) after version lookup failed", installerHits)
			}
			if len(runner.Commands) != 0 {
				t.Fatalf("appliance commands ran after version lookup failed: %#v", runner.Commands)
			}
			if !strings.Contains(out.String(), "--version") {
				t.Fatalf("missing actionable explicit-version recovery: %s", out.String())
			}
		})
	}
}

func TestUpdateRejectsMalformedConfigBeforeAnyMutation(t *testing.T) {
	installerHits := 0
	cliSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		installerHits++
		_, _ = io.WriteString(w, "#!/usr/bin/env sh\nexit 0\n")
	}))
	defer cliSrv.Close()

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
	pluginMarker := filepath.Join(home, ".codex", "plugins", "specgate", ".specgate-owned")
	writeTestFile(t, pluginMarker, "specgate-plugin-v1\n")
	runner := &fakeDeployRunner{}
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath
	deps.CLIInstallURL = cliSrv.URL
	deps.UserHomeDir = func() (string, error) { return home, nil }
	deps.DeployRunner = runner

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "update", "--version", "v9.9.9", "--dir", deployDir)
	if code != output.ExitUnavailable {
		t.Fatalf("exit = %d, want unavailable; output=%s", code, out.String())
	}
	if installerHits != 0 || len(runner.Commands) != 0 {
		t.Fatalf("malformed config caused side effects: installer hits=%d deploy commands=%v", installerHits, runner.Commands)
	}
	for _, path := range []string{cfgPath, deploySentinel, pluginMarker} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("malformed config mutated %s: %v", path, err)
		}
	}
	if !strings.Contains(out.String(), "invalid character") {
		t.Fatalf("missing config parse error: %s", out.String())
	}
}

func TestUpdateHumanShowsStepLabels(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cli/install.sh", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "#!/usr/bin/env sh\nexit 0\n")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	plugins := newPluginRegistry(t)

	deps, out := newTestDeps(t, srv.URL)
	deps.CLIInstallURL = srv.URL + "/cli/install.sh"
	deps.PluginRegistryURL = plugins.URL
	home := t.TempDir()
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "update", "--version", "v9.9.9")
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
	if !strings.Contains(got, "Step 4/4 Update Full appliance") {
		t.Fatalf("missing step 4: %s", got)
	}
}

func TestUpdateRefreshesInstalledIDEPluginsFromPublicRegistry(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cli/install.sh", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "#!/usr/bin/env sh\nexit 0\n")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	plugins := newPluginRegistry(t)
	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, ".codex", "plugins", "specgate", ".codex-plugin", "plugin.json"), "{}")
	writeTestFile(t, filepath.Join(home, ".codex", "plugins", "specgate", ".specgate-owned"), "specgate-plugin-v1\n")

	deps, out := newTestDeps(t, srv.URL)
	deps.CLIInstallURL = srv.URL + "/cli/install.sh"
	deps.PublicRegistryURL = plugins.URL
	deps.PluginRegistryURL = srv.URL
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "update", "--version", "v9.9.9")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "plugins", "specgate", ".codex-plugin", "plugin.json")); err != nil {
		t.Fatalf("codex plugin not installed from registry: %v\n%s", err, out.String())
	}
	for _, path := range []string{filepath.Join(home, ".cursor"), filepath.Join(home, ".claude")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("update created an unselected IDE path %s; stat err=%v", path, err)
		}
	}
}

func TestUpdateDoesNotCreateIDEFilesWhenNoneAreInstalled(t *testing.T) {
	cliSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "#!/usr/bin/env sh\nexit 0\n")
	}))
	defer cliSrv.Close()
	plugins := newPluginRegistry(t)
	home := t.TempDir()

	deps, out := newTestDeps(t, "http://127.0.0.1:1")
	deps.CLIInstallURL = cliSrv.URL
	deps.PluginRegistryURL = plugins.URL
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "update", "--version", "v9.9.9")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	for _, path := range []string{
		filepath.Join(home, ".cursor"),
		filepath.Join(home, ".codex"),
		filepath.Join(home, ".claude"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("update created unselected IDE files at %s; stat err=%v", path, err)
		}
	}
}

func TestUpdateFetchHonorsTimeout(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cli/install.sh", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = io.WriteString(w, "#!/usr/bin/env sh\nexit 0\n")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	plugins := newPluginRegistry(t)

	deps, out := newTestDeps(t, srv.URL)
	deps.CLIInstallURL = srv.URL + "/cli/install.sh"
	deps.PluginRegistryURL = plugins.URL
	home := t.TempDir()
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--timeout", "50ms", "--server", srv.URL, "update", "--version", "v9.9.9")
	if code != output.ExitUnavailable {
		t.Fatalf("exit = %d, want unavailable; output = %s", code, out.String())
	}
}

func TestUpdateRejectsOversizedInstallerScript(t *testing.T) {
	cliSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("#", (1<<20)+1))
	}))
	defer cliSrv.Close()

	deps, out := newTestDeps(t, "http://127.0.0.1:1")
	deps.CLIInstallURL = cliSrv.URL
	deps.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "update", "--version", "v9.9.9")
	if code != output.ExitUnavailable {
		t.Fatalf("exit = %d, want unavailable; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "installer script exceeds") {
		t.Fatalf("missing oversized-installer error: %s", out.String())
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
	srv := httptest.NewServer(mux)
	defer srv.Close()
	plugins := newPluginRegistry(t)

	deps, out := newTestDeps(t, srv.URL)
	deps.CLIInstallURL = cliSrv.URL
	deps.PluginRegistryURL = plugins.URL
	home := t.TempDir()
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "update", "--version", "v9.9.9")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if serverCLIHit {
		t.Fatal("update fetched CLI installer from connected server")
	}
}

func TestUpdateRefreshesInstalledIDEPluginWhenConnectedServerUnavailable(t *testing.T) {
	cliSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "#!/usr/bin/env sh\nexit 0\n")
	}))
	defer cliSrv.Close()
	plugins := newPluginRegistry(t)
	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, ".claude", "skills", "specgate", ".claude-plugin", "plugin.json"), "{}")
	writeTestFile(t, filepath.Join(home, ".claude", "skills", "specgate", ".specgate-owned"), "specgate-plugin-v1\n")

	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	deps, out := newTestDeps(t, srv.URL)
	deps.CLIInstallURL = cliSrv.URL
	deps.PublicRegistryURL = plugins.URL
	deps.PluginRegistryURL = srv.URL
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", srv.URL, "update", "--version", "v9.9.9")
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
	if err := os.WriteFile(filepath.Join(deployDir, "specgate.env"), []byte("SETTINGS_ENCRYPTION_KEY=secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deployDir, ".env"), []byte("SPECGATE_VERSION=v9.9.0-rc.1\nSPECGATE_PORT=13000\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runner := &fakeDeployRunner{OutputData: localBackupPayload(t)}
	deps, out := newTestDeps(t, "http://127.0.0.1:1")
	deps.CLIInstallURL = cliSrv.URL
	deps.PluginRegistryURL = plugins.URL
	deps.BundleBaseURL = bundles.URL + "/v9.9.9"
	deps.DeployRunner = runner
	home := t.TempDir()
	deps.UserHomeDir = func() (string, error) { return home, nil }

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "update", "--version", "v9.9.9", "--dir", deployDir)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	env, err := os.ReadFile(filepath.Join(deployDir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "SPECGATE_VERSION=v9.9.9") ||
		!strings.Contains(string(env), "SPECGATE_PORT=13000") {
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
	for _, want := range []string{
		"docker version",
		"docker compose version",
		" pull",
		"docker compose -f " + filepath.Join(deployDir, "compose.yml") + " exec -T specgate /usr/local/bin/specgate-backup",
		"docker compose -f " + filepath.Join(deployDir, "compose.yml") + " up -d",
		"docker compose -f " + filepath.Join(deployDir, "compose.yml") + " down",
		"docker compose -f " + filepath.Join(deployDir, "compose.yml") + " up -d --wait",
		"http://127.0.0.1:3000/api/doc-registry/api/v1/meta",
		"http://127.0.0.1:3000/api/agents/openapi.json",
	} {
		if !strings.Contains(gotCommands, want) {
			t.Fatalf("missing command %q in:\n%s", want, gotCommands)
		}
	}
	backupAt := strings.Index(gotCommands, "/usr/local/bin/specgate-backup")
	startAt := strings.Index(gotCommands, " up -d\n")
	downAt := strings.Index(gotCommands, " down")
	upAt := strings.LastIndex(gotCommands, " up -d --wait")
	if startAt < 0 || backupAt < startAt || downAt < backupAt || upAt < downAt {
		t.Fatalf("update must start for backup, back up, stop, then replace the appliance:\n%s", gotCommands)
	}
	backups, err := filepath.Glob(filepath.Join(deployDir, "backups", "specgate-before-9.9.9-*.tar.gz"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backup files = %#v, err = %v", backups, err)
	}
}

func localBackupPayload(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{"docreg.sql": "-- dump\n", "registry/blob": "blob"} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newComposeBundleRegistry(t *testing.T, version string) *httptest.Server {
	t.Helper()
	files := map[string]string{
		"compose.yml":          "# bundle " + version + "\n",
		".env.example":         "SPECGATE_VERSION=latest\n",
		"specgate.env.example": "SETTINGS_ENCRYPTION_KEY=\n",
		"rollback-compatible":  "false\n",
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
	if err := (config.Config{Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

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
	fc.metaResult = &client.Meta{
		APIVersion: "specgate.api/v1",
		CapabilityDetails: map[string]client.CapabilityDetail{
			"agents": {State: "available"},
		},
	}
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

func TestOpenWithoutTargetUsesConfiguredServerURL(t *testing.T) {
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
