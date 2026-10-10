package command_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
)

func TestLocalWorkspaceSelectMissingSlugIsUsageWithoutMutation(t *testing.T) {
	deps, out := newTestDeps(t)
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "init", "--mode", "local", "--local-dir", t.TempDir(), "--workspace-name", "Alpha", "--display-name", "Human", "--username", "human"); code != 0 {
		t.Fatalf("init exit=%d: %s", code, out.String())
	}
	before, err := os.ReadFile(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--json", "workspace", "select"},
		{"--json", "workspace", "select", " "},
		{"--plain", "--no-input", "workspace", "select"},
	} {
		out.Reset()
		if code := command.ExecuteForCode(command.NewRootCommand(deps), args...); code != 2 {
			t.Fatalf("missing slug exit=%d, want usage: %s", code, out.String())
		}
	}
	after, err := os.ReadFile(deps.ConfigPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("invalid selection changed config: %v", err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "workspace", "current"); code != 0 || !bytes.Contains(out.Bytes(), []byte(`"slug":"alpha"`)) {
		t.Fatalf("invalid selection changed workspace: exit=%d %s", code, out.String())
	}
	if deps.Client != nil {
		t.Fatal("Local selection created an HTTP client")
	}
}
