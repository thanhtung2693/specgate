package command_test

import (
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestWorkCreateRequiresFeatureAndTitle(t *testing.T) {
	t.Parallel()
	deps, _ := newTestDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "create", "--title", "T")
	if code == output.ExitOK {
		t.Fatal("missing --feature must fail")
	}
}
