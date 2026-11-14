package command_test

import (
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
)

func TestGatesHelpShowsUnifiedFamily(t *testing.T) {
	t.Parallel()
	deps, _ := newTestDeps(t)
	root := command.NewRootCommand(deps)
	gatesCmd, _, err := root.Find([]string{"gates"})
	if err != nil {
		t.Fatalf("find gates command: %v", err)
	}
	var subcommands []string
	for _, sub := range gatesCmd.Commands() {
		subcommands = append(subcommands, sub.Name())
	}
	joined := strings.Join(subcommands, ",")
	for _, want := range []string{"check", "results", "tasks"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("gates subcommand %q not registered: %v", want, subcommands)
		}
	}
}
