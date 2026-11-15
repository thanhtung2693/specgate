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
	deps, out := newTestDeps(t)
	deps.ConfigPath = cfgPath

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "status")
	if code != output.ExitUnavailable {
		t.Fatalf("exit = %d, want unavailable; output = %s", code, out.String())
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
	deps, out := newTestDeps(t)
	deps.ConfigPath = cfgPath

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "status")
	if code != output.ExitUnavailable {
		t.Fatalf("exit = %d, want unavailable; output = %s", code, out.String())
	}

	if !strings.Contains(out.String(), "expected local") {
		t.Fatalf("output lacks mode recovery context: %s", out.String())
	}
}

func TestVersionStillWorksWithMalformedConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps, out := newTestDeps(t)
	deps.ConfigPath = cfgPath

	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "version"); code != output.ExitOK {
		t.Fatalf("exit = %d, want ok; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), `"command":"version"`) {
		t.Fatalf("unexpected version output: %s", out.String())
	}
}

func TestFlagErrorDoesNotColorRedirectedStderr(t *testing.T) {
	deps, _ := newTestDeps(t)
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
	deps, out := newTestDeps(t)
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
	deps, out := newTestDeps(t)
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
		deps, out := newTestDeps(t)
		deps.Stderr = out
		code := command.ExecuteForCode(command.NewRootCommand(deps), append([]string{"--plain"}, args...)...)
		if code != output.ExitUsage {
			t.Fatalf("%v: exit = %d, want %d; output = %s", args, code, output.ExitUsage, out.String())
		}
	}
}

func TestBareCommandGroupsReturnJSONHelpWithoutConfiguration(t *testing.T) {
	for _, tc := range []struct {
		args []string
		name string
	}{
		{nil, "specgate"},
		{[]string{"work"}, "work"},
		{[]string{"delivery", "handoff"}, "delivery.handoff"},
		{[]string{"completion"}, "completion"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps, out := newTestDeps(t)
			if err := os.WriteFile(deps.ConfigPath, []byte("{broken"), 0600); err != nil {
				t.Fatal(err)
			}
			if code := command.ExecuteForCode(command.NewRootCommand(deps), append(tc.args, "--json")...); code != output.ExitOK {
				t.Fatalf("exit = %d, output = %s", code, out)
			}
			var envelope struct {
				OK      bool   `json:"ok"`
				Command string `json:"command"`
				Data    struct {
					Help string `json:"help"`
				} `json:"data"`
			}
			if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
				t.Fatalf("invalid envelope: %v; %s", err, out)
			}
			if !envelope.OK || envelope.Command != tc.name || !strings.Contains(envelope.Data.Help, "Usage:") {
				t.Fatalf("unexpected help envelope: %s", out)
			}
		})
	}
}

func TestGeneratedCompletionUsageAndRawUtilities(t *testing.T) {
	for _, tc := range []struct {
		name         string
		args         []string
		usageCommand string
		raw          string
	}{
		{"extra argument", []string{"completion", "bash", "unexpected"}, "completion.bash", ""},
		{"unknown shell", []string{"completion", "nonesuch"}, "completion", ""},
		{"raw completion", []string{"completion", "bash"}, "", "# bash completion"},
		{"raw help", []string{"work", "--help"}, "", "Usage:"},
		{"raw version", []string{"--version"}, "", "specgate "},
		{"help command", []string{"help", "work"}, "", "Usage:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps, out := newTestDeps(t)
			if err := os.WriteFile(deps.ConfigPath, []byte("{broken"), 0600); err != nil {
				t.Fatal(err)
			}
			code := command.ExecuteForCode(command.NewRootCommand(deps), append(tc.args, "--json")...)
			if tc.raw != "" {
				if code != output.ExitOK || !strings.Contains(out.String(), tc.raw) || json.Valid(out.Bytes()) {
					t.Fatalf("expected raw utility output with exit 0; exit = %d, output = %.80q", code, out)
				}
				return
			}
			var envelope struct {
				Command string              `json:"command"`
				Error   output.ErrorPayload `json:"error"`
			}
			if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
				t.Fatalf("exit %d and invalid envelope: %v; %s", code, err, out)
			}
			if code != output.ExitUsage || envelope.Command != tc.usageCommand || envelope.Error.Code != "usage" || !strings.Contains(envelope.Error.Message, " --help") {
				t.Fatalf("exit = %d, output = %s", code, out)
			}
		})
	}
}

func TestUIValidationAndRuntimeExitContracts(t *testing.T) {
	for _, tc := range []struct {
		port, errorCode, message string
		exit                     int
	}{
		{"-1", "usage", "--port must be between 0 and 65535", output.ExitUsage},
		{"65536", "usage", "--port must be between 0 and 65535", output.ExitUsage},
		{"0", "unavailable", "local UI requires a repository", output.ExitUnavailable},
	} {
		t.Run(tc.port, func(t *testing.T) {
			deps, out := newTestDeps(t)
			var stderr bytes.Buffer
			deps.Stderr = &stderr
			code := command.ExecuteForCode(command.NewRootCommand(deps), "ui", "--port", tc.port, "--json")
			var envelope struct {
				Error output.ErrorPayload `json:"error"`
			}
			if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
				t.Fatalf("exit %d and invalid envelope: %v; %s", code, err, out)
			}
			if code != tc.exit || envelope.Error.Code != tc.errorCode || !strings.Contains(envelope.Error.Message, tc.message) || stderr.Len() != 0 {
				t.Fatalf("exit = %d, stdout = %s, stderr = %s", code, out, &stderr)
			}
			out.Reset()
			if code := command.ExecuteForCode(command.NewRootCommand(deps), "ui", "--port", tc.port); code != tc.exit || !strings.Contains(stderr.String(), tc.message) || out.Len() != 0 {
				t.Fatalf("human exit = %d, stdout = %s, stderr = %s", code, out, &stderr)
			}
		})
	}
}

func TestWizardDoesNotPromptWithRedirectedInputOrCI(t *testing.T) {
	for _, tc := range []struct {
		name                string
		stdinTTY, stderrTTY bool
		ci                  string
	}{
		{"pipe", false, true, ""},
		{"CI with TTY", true, true, "true"},
		{"redirected prompts", true, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps, prompt, out := newWorkflowTestDeps(t)
			t.Setenv("CI", tc.ci)
			input, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			defer writer.Close()
			deps.Stdin = input
			deps.StdinIsTTY = func() bool { return tc.stdinTTY }
			deps.StderrIsTTY = func() bool { return tc.stderrTTY }
			command.ExecuteForCode(command.NewRootCommand(deps), "plugins", "doctor")
			if prompt.multiTitle != "" {
				t.Fatalf("unexpected interactive prompt %q; output = %s", prompt.multiTitle, out)
			}
		})
	}
}
