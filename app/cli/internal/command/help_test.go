package command_test

import (
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

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
			deps, out := newTestDeps(t)
			deps.StdoutIsTTY = func() bool { return tc.tty }
			if code := command.ExecuteForCode(command.NewRootCommand(deps), "--help"); code != output.ExitOK {
				t.Fatalf("exit = %d, output = %s", code, out.String())
			}
			got := out.String()
			for _, want := range []string{"Usage:", "Start here", "Additional Commands:"} {
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

func TestRootHelpExplainsDefaultLocalWorkflow(t *testing.T) {
	deps, out := newTestDeps(t)
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--help"); code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	for _, want := range []string{
		"specgate init sets up Local mode by default",
		"without Docker, a model API key, or a server",
		"Review the evidence and follow the next command",
		"specgate init",
		"specgate doctor",
		"specgate work resume LOCAL-123 --json",
		"specgate change status LOCAL-123",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("root help missing %q:\n%s", want, out.String())
		}
	}
	examples := strings.SplitN(out.String(), "Examples:", 2)
	if len(examples) != 2 {
		t.Fatalf("root help is missing examples:\n%s", out.String())
	}
	workflow := strings.SplitN(examples[1], "Start here", 2)[0]
	for _, optional := range []string{"specgate ui", "specgate open", "--mode full", "work verification"} {
		if strings.Contains(workflow, optional) {
			t.Errorf("default examples include optional command %q:\n%s", optional, workflow)
		}
	}
}

func TestRootHelpKeepsAdvancedLocalCommandsCallable(t *testing.T) {
	for _, path := range [][]string{{"work", "verification"}, {"artifact", "impact"}, {"open"}} {
		deps, out := newTestDeps(t)
		if code := command.ExecuteForCode(command.NewRootCommand(deps), append(path, "--help")...); code != output.ExitOK {
			t.Errorf("%v --help exit = %d, output = %s", path, code, out.String())
		}
		if !strings.Contains(out.String(), "specgate "+strings.Join(path, " ")) {
			t.Errorf("%v help missing command usage:\n%s", path, out.String())
		}
	}
}
