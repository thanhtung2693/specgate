package command_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
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
	deps.PluginRegistryURL = registry.URL
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
	deps.PluginRegistryURL = registry.URL
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

func TestPluginsUseRegistryAfterFullInitialization(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	homeDir := t.TempDir()

	var requests atomic.Int64
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "registry unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(registry.Close)

	deps, out := newPluginDeps(homeDir)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{Mode: config.ModeFull}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	deps.PluginRegistryURL = registry.URL
	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "plugins", "install", "--project-local", "--agent", "codex",
	)
	if code == output.ExitOK {
		t.Fatalf("configured Full install bypassed registry: %s", out.String())
	}
	if got := requests.Load(); got == 0 {
		t.Fatal("configured Full install did not contact plugin registry")
	}
}
