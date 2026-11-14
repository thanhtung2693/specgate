package command

import (
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/config"
)

func TestTransferPickupCommandQuotesNativePowerShellPaths(t *testing.T) {
	activation := transferActivation{
		Repo:       `C:\repos\Bob's %work% $test`,
		ConfigPath: `C:\profiles\Bob's %work% $test\config.json`,
		New: transferBinding{
			LocalDir:  `C:\stores\Bob's %work% $test`,
			Workspace: config.CurrentWorkspace{ID: "workspace-id"},
		},
	}
	command := transferReadCommand("windows", activation, "work resume", "LOCAL-work", "--since", "abc", "--json")
	for _, expected := range []string{
		`Push-Location -LiteralPath 'C:\repos\Bob''s %work% $test' -ErrorAction Stop`,
		`$env:SPECGATE_CONFIG_PATH = 'C:\profiles\Bob''s %work% $test\config.json'`,
		`$env:SPECGATE_LOCAL_DIR = 'C:\stores\Bob''s %work% $test'`,
		`& specgate --workspace 'workspace-id' work resume 'LOCAL-work' --since 'abc' --json`,
		`finally`,
	} {
		if !strings.Contains(command, expected) {
			t.Errorf("missing literal %q in %q", expected, command)
		}
	}
}

func TestTransferRepositoryDoesNotConflateRemoteHomeAndAbsolutePaths(t *testing.T) {
	home := transferRepository("git@Host.Example:org/repo.git")
	absolute := transferRepository("git@Host.Example:/org/repo.git")
	if home == "" || absolute == "" || home == absolute ||
		transferRepository(home) != home || transferRepository(absolute) != absolute ||
		strings.Contains(home, "git@") || strings.Contains(absolute, "git@") {
		t.Fatalf("distinct credential-free, stable identities required: %q %q", home, absolute)
	}
}
