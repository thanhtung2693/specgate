package command_test

import (
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// The message a user sees when login flags are missing must be a sentence. It
// used to append " are required" to a bare noun, producing "workspace are
// required", and it named nouns rather than the flags to type.
func TestUserLoginNamesMissingFlagsAsASentence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{"one missing", []string{"--display-name", "D", "--username", "u"}, []string{"--workspace is required"}},
		{"two missing", []string{"--username", "u"}, []string{"--workspace and --display-name are required"}},
		{"all missing", nil, []string{"--workspace, --display-name, and --username are required"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			deps, _, _, out := newFakeDeps(t)
			stateDir := t.TempDir()
			if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(deps.ConfigPath); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"--json", "user", "login"}, tc.args...)
			if code := command.ExecuteForCode(command.NewRootCommand(deps), args...); code != output.ExitUsage {
				t.Fatalf("exit = %d, want ExitUsage; output = %s", code, out.String())
			}
			for _, want := range tc.want {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("output = %q, want %q", out.String(), want)
				}
			}
		})
	}
}
