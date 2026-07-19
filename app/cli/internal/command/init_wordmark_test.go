package command

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestWriteInitWelcome(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mode     output.Mode
		color    bool
		stdinTTY bool
		noInput  bool
		env      map[string]string
		want     string
	}{
		{name: "rich", mode: output.ModeHuman, color: true, stdinTTY: true, want: " ____  ____"},
		{name: "narrow", mode: output.ModeHuman, color: true, stdinTTY: true, env: map[string]string{"COLUMNS": "40"}, want: "SpecGate"},
		{name: "plain", mode: output.ModePlain, color: false, stdinTTY: true},
		{name: "json", mode: output.ModeJSON, color: false, stdinTTY: true},
		{name: "no input", mode: output.ModeHuman, color: true, stdinTTY: true, noInput: true},
		{name: "non tty", mode: output.ModeHuman, color: true},
		{name: "ci", mode: output.ModeHuman, color: true, stdinTTY: true, env: map[string]string{"CI": "true"}},
		{name: "no color", mode: output.ModeHuman, color: true, stdinTTY: true, env: map[string]string{"NO_COLOR": "1"}},
		{name: "dumb terminal", mode: output.ModeHuman, color: true, stdinTTY: true, env: map[string]string{"TERM": "dumb"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CI", "")
			t.Setenv("NO_COLOR", "")
			t.Setenv("TERM", "xterm-256color")
			t.Setenv("COLUMNS", "80")
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			var stderr bytes.Buffer
			deps := &Deps{
				Stdout:      io.Discard,
				Stderr:      &stderr,
				NoInput:     tc.noInput,
				StdinIsTTY:  func() bool { return tc.stdinTTY },
				StdoutIsTTY: func() bool { return tc.color },
				StderrIsTTY: func() bool { return tc.color },
				Printer:     output.NewWithColor(io.Discard, &stderr, tc.mode, tc.color),
			}

			writeInitWelcome(deps)
			if tc.want == "" {
				if stderr.Len() != 0 {
					t.Fatalf("unexpected welcome: %q", stderr.String())
				}
				return
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("welcome = %q, want %q", stderr.String(), tc.want)
			}
		})
	}
}

func TestWriteInitWelcomeRequiresStderrTerminal(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	var stderr bytes.Buffer
	deps := &Deps{
		Stderr:      &stderr,
		StdinIsTTY:  func() bool { return true },
		StdoutIsTTY: func() bool { return true },
		StderrIsTTY: func() bool { return false },
		Printer:     output.NewWithCapabilities(io.Discard, &stderr, output.ModeHuman, true, false),
	}

	writeInitWelcome(deps)
	if stderr.Len() != 0 {
		t.Fatalf("wordmark written to redirected stderr: %q", stderr.String())
	}
}
