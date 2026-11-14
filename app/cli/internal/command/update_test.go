package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestUpdateUsesResolvedExecutableDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "install target")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "specgate")
	if err := os.WriteFile(exe, []byte("original binary"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "specgate")
	if err := os.Symlink(exe, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "#!/bin/sh\nprintf 'INSTALL_ARG:%s\\n' \"$@\"\n")
	}))
	defer srv.Close()
	deps, out := newTestDeps(t)
	deps.RuntimeGOOS = "linux"
	deps.CLIInstallURL = srv.URL
	deps.ExecutablePath = func() (string, error) { return link, nil }
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: t.TempDir()}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "update", "--version", "v9.9.9")
	if code != 0 {
		t.Fatalf("update exit=%d: %s", code, out.String())
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := "INSTALL_ARG:--install-dir\nINSTALL_ARG:" + resolved + "\nINSTALL_ARG:--version\nINSTALL_ARG:v9.9.9\n"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("installer did not receive exact resolved target/version: %s", out.String())
	}
	if data, err := os.ReadFile(exe); err != nil || string(data) != "original binary" {
		t.Fatalf("test installer changed the executable: %q %v", data, err)
	}
}

func TestUpdateWindowsUsesNativeSelfUpdaterWithoutShell(t *testing.T) {
	installerHits := 0
	cliSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		installerHits++
		http.Error(w, "shell installer must not run on Windows", http.StatusInternalServerError)
	}))
	defer cliSrv.Close()

	deps, out := newTestDeps(t)
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
		_, _ = io.WriteString(w, "#!/usr/bin/env sh\necho 'installer progress'\n")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	plugins := newPluginRegistry(t)

	deps, out := newTestDeps(t)
	var stderr bytes.Buffer
	deps.Stderr = &stderr
	deps.CLIInstallURL = srv.URL + "/cli/install.sh"
	deps.PublicRegistryURL = plugins.URL

	home := t.TempDir()
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--json-progress", "update", "--version", "v9.9.9")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected progress lines plus final envelope, got %d lines: %s", len(lines), out.String())
	}

	for _, line := range lines[:len(lines)-1] {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("progress line %q is not JSON: %v", line, err)
		}
		if event["event"] == nil {
			t.Fatalf("progress line missing event: %s", line)
		}
	}
	if !strings.Contains(stderr.String(), "installer progress") {
		t.Fatalf("installer output missing from stderr: %s", &stderr)
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
		_, _ = io.WriteString(w, "#!/usr/bin/env sh\necho 'installer progress'\n")
	}))
	defer cliSrv.Close()

	runner := &fakeCommandRunner{}
	deps, out := newTestDeps(t)
	deps.CLIInstallURL = cliSrv.URL
	deps.CommandRunner = runner
	deps.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: t.TempDir()}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "update", "--version", "v9.9.9",
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

	deploySentinel := filepath.Join(deployDir, "keep.txt")
	if err := os.WriteFile(deploySentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	pluginMarker := filepath.Join(home, ".codex", "plugins", "specgate", ".specgate-owned")
	writeTestFile(t, pluginMarker, "specgate-plugin-v1\n")
	runner := &fakeCommandRunner{}
	deps, out := newTestDeps(t)
	deps.ConfigPath = cfgPath
	deps.CLIInstallURL = cliSrv.URL
	deps.UserHomeDir = func() (string, error) { return home, nil }
	deps.CommandRunner = runner

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "update", "--version", "v9.9.9")
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

	deps, out := newTestDeps(t)
	deps.CLIInstallURL = srv.URL + "/cli/install.sh"

	home := t.TempDir()
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "update", "--version", "v9.9.9")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Step 1/3 Detect install target") {
		t.Fatalf("missing step 1: %s", got)
	}
	if !strings.Contains(got, "Step 2/3 Install CLI") {
		t.Fatalf("missing step 2: %s", got)
	}
	if !strings.Contains(got, "Step 3/3 Update IDE setup") {
		t.Fatalf("missing step 3: %s", got)
	}
	if strings.Contains(got, "Update Full appliance") {
		t.Fatalf("retired appliance step: %s", got)
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

	deps, out := newTestDeps(t)
	deps.CLIInstallURL = srv.URL + "/cli/install.sh"
	deps.PublicRegistryURL = plugins.URL

	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "update", "--version", "v9.9.9")
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

	home := t.TempDir()

	deps, out := newTestDeps(t)
	deps.CLIInstallURL = cliSrv.URL

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

	for _, mode := range []config.Mode{config.ModeLocal} {
		t.Run(string(mode), func(t *testing.T) {
			deps, out := newTestDeps(t)
			var stderr bytes.Buffer
			deps.Stderr = &stderr
			deps.CLIInstallURL = srv.URL + "/cli/install.sh"

			home := t.TempDir()
			deps.UserHomeDir = func() (string, error) { return home, nil }
			if err := (config.Config{Mode: mode, Server: srv.URL}).SaveTo(deps.ConfigPath); err != nil {
				t.Fatal(err)
			}
			code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--timeout", "50ms", "update", "--version", "v9.9.9")
			if code != output.ExitUnavailable || !strings.Contains(stderr.String(), "Client.Timeout") {
				t.Fatalf("exit = %d, want timeout failure; output = %s; stderr = %s", code, out.String(), stderr.String())
			}
		})
	}
}

func TestUpdateRejectsOversizedInstallerScript(t *testing.T) {
	cliSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("#", (1<<20)+1))
	}))
	defer cliSrv.Close()

	deps, out := newTestDeps(t)
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

	deps, out := newTestDeps(t)
	deps.CLIInstallURL = cliSrv.URL
	if err := (config.Config{Mode: config.ModeLocal, Server: srv.URL}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	deps.UserHomeDir = func() (string, error) { return home, nil }
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "update", "--version", "v9.9.9")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if serverCLIHit {
		t.Fatal("update fetched CLI installer from connected server")
	}
}
