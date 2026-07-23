package command_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// TestConfigSetServerPersists verifies config server writes the URL to the config file.
func TestConfigSetServerPersists(t *testing.T) {
	t.Parallel()
	cfgPath := filepath.Join(t.TempDir(), "config.json")

	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath
	// config server doesn't call the API, so we don't need a real server.
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--server", "http://localhost:8080", "config", "server", "https://my.specgate.example")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}

	got, err := config.LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Server != "https://my.specgate.example" {
		t.Fatalf("Server = %q, want https://my.specgate.example", got.Server)
	}
}

func TestConfigSetServerRejectsMalformedConfigWithoutReplacingIt(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps, out := newTestDeps(t, "")
	deps.ConfigPath = cfgPath

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "config", "server", "https://example.test")
	if code != output.ExitUnavailable {
		t.Fatalf("exit = %d, want unavailable; output = %s", code, out.String())
	}
	body, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "{broken" {
		t.Fatalf("malformed config was replaced: %s", body)
	}
}
