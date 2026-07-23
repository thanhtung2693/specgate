package command_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/buildinfo"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

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
