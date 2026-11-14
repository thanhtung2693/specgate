package command_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// The Local trail itself still reads, and its envelope must not claim a
// verification happened.
func TestAuditLocalTrailReportsNoChain(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir := t.TempDir()
	store, err := local.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Initialize(t.Context(), local.InitInput{
		WorkspaceName: "Local workspace", DisplayName: "Local developer", Username: "local",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "audit", "CR-1")
	if code != output.ExitOK && code != output.ExitNotFound {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if strings.Contains(out.String(), `"verified":true`) {
		t.Fatalf("Local audit must not claim a verified chain, got %s", out.String())
	}
}
