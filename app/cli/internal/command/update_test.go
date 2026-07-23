package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/specgate/specgate/app/cli/internal/buildinfo"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

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

func TestUpdateWindowsUsesNativeSelfUpdaterWithoutShell(t *testing.T) {
	installerHits := 0
	cliSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		installerHits++
		http.Error(w, "shell installer must not run on Windows", http.StatusInternalServerError)
	}))
	defer cliSrv.Close()

	deps, out := newTestDeps(t, "")
	deps.CLIInstallURL = cliSrv.URL
	deps.RuntimeGOOS = "windows"
	deps.ExecutablePath = func() (string, error) {
		return `C:\Users\test\AppData\Local\bin\specgate.exe`, nil
	}
	var (
		updatedVersion string
		updatedPath    string
	)
	deps.SelfUpdateCLI = func(_ context.Context, version, executablePath string) error {
		updatedVersion = version
		updatedPath = executablePath
		return nil
	}
	deps.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	if err := (config.Config{
		Mode:  config.ModeLocal,
		Local: config.LocalStore{Path: t.TempDir()},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "update", "--version", "v9.9.9",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if installerHits != 0 {
		t.Fatalf("Windows update fetched shell installer %d time(s)", installerHits)
	}
	if updatedVersion != "v9.9.9" {
		t.Fatalf("updated version = %q, want v9.9.9", updatedVersion)
	}
	if updatedPath != `C:\Users\test\AppData\Local\bin\specgate.exe` {
		t.Fatalf("updated path = %q", updatedPath)
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

// fakeDeployRunner implements deploy.CommandRunner for command-layer tests.
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
