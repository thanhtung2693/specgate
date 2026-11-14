package command_test

import (
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestLocalWorkflowCommandSurfaceRetiresStatsAndPortable(t *testing.T) {
	deps, _ := newTestDeps(t)
	root := command.NewRootCommand(deps)
	for _, args := range [][]string{{"stats"}, {"portable"}, {"work", "archive"}, {"work", "policy"}, {"gates", "run"}, {"gates", "status"}, {"gates", "history"}} {
		cmd, _, err := root.Find(args)
		if err == nil && cmd.Name() == args[len(args)-1] {
			t.Errorf("retired command is registered: %v", args)
		}
	}
	for _, args := range [][]string{{"status"}, {"work", "list"}, {"audit"}, {"delivery", "handoff", "export"}, {"delivery", "handoff", "show"}, {"gates", "tasks", "dispatch"}} {
		cmd, _, err := root.Find(args)
		if err != nil || cmd.Name() != args[len(args)-1] {
			t.Errorf("Local workflow command missing: %v (%v)", args, err)
		}
	}
}

func TestRetiredRemoteFlagsReturnUsage(t *testing.T) {
	for _, removed := range [][]string{{"audit", "LOCAL-123", "--verify"}, {"status", "--all-workspaces"}, {"work", "list", "--all-workspaces"}} {
		deps, out := newTestDeps(t)
		flag := removed[len(removed)-1]
		if code := command.ExecuteForCode(command.NewRootCommand(deps), append([]string{"--json"}, removed...)...); code != output.ExitUsage || !strings.Contains(out.String(), "unknown flag: "+flag) {
			t.Errorf("retired command option %v returned %d: %s", removed, code, out.String())
		}
	}
}
