package command_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestPluginsUseEmbeddedPackageBeforeInitialization(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	homeDir := t.TempDir()

	var requests atomic.Int64
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "registry should not be contacted", http.StatusServiceUnavailable)
	}))
	t.Cleanup(registry.Close)

	deps, out := newPluginDeps(homeDir)

	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "plugins", "install", "--project-local", "--agent", "codex",
	)
	if code != output.ExitOK {
		t.Fatalf("install exit = %d, output = %s", code, out.String())
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("uninitialized install contacted plugin registry %d time(s)", got)
	}

	deps, out = newPluginDeps(homeDir)

	code = command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "plugins", "doctor", "--project-local", "--agent", "codex",
	)
	if code != output.ExitOK {
		t.Fatalf("doctor exit = %d, output = %s", code, out.String())
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("uninitialized doctor contacted plugin registry %d time(s)", got)
	}
}
