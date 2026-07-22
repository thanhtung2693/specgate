package command_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestPluginsRejectAndReportNativeOwnership(t *testing.T) {
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/plugins/package.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"version":"1.0.0","skills":["using-specgate"]}`)
	}))
	t.Cleanup(registry.Close)

	tests := []struct {
		agent string
		setup func(*testing.T, string)
	}{
		{"codex", writeNativeCodexInstall},
		{"claude", writeNativeClaudeInstall},
	}
	for _, test := range tests {
		t.Run(test.agent, func(t *testing.T) {
			home, work := t.TempDir(), t.TempDir()
			t.Chdir(work)
			test.setup(t, home)

			deps, out := newPluginDeps(home)
			code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--no-input", "--server", "http://127.0.0.1:1", "plugins", "install", "--agent", "cursor,"+test.agent, "--project-local")
			if code != output.ExitConflict || !strings.Contains(out.String(), `"owner":"native"`) || !strings.Contains(out.String(), `"marketplace":"team"`) {
				t.Fatalf("native conflict missing: exit=%d output=%s", code, out.String())
			}
			if _, err := os.Stat(filepath.Join(work, ".cursor")); !os.IsNotExist(err) {
				t.Fatalf("install wrote before conflict: %v", err)
			}

			deps, out = newPluginDeps(home)
			code = command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--server", registry.URL, "plugins", "doctor", "--agent", test.agent)
			if code != output.ExitOK || !strings.Contains(out.String(), "native marketplace team") {
				t.Fatalf("native doctor missing: exit=%d output=%s", code, out.String())
			}
		})
	}
}

func writeNativeCodexInstall(t *testing.T, home string) {
	t.Helper()
	writeTestFile(t, filepath.Join(home, ".codex", "config.toml"), "[plugins.\"specgate@team\"]\nenabled = true\n")
}

func writeNativeClaudeInstall(t *testing.T, home string) {
	t.Helper()
	writeTestFile(t, filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), `{"plugins":{"specgate@team":[]}}`)
}
