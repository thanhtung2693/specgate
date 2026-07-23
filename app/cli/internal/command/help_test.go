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
