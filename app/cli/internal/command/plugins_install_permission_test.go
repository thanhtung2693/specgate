package command_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// The installed skills instruct the agent to drive the `specgate` CLI. Without a
// matching permission rule the agent's first governed command is denied and the
// work stalls before any record exists, so the install has to grant what it asks
// the agent to run.
func TestPluginsInstallProjectLocalClaudeAllowsTheSpecgateCLI(t *testing.T) {
	srv := newPluginRegistry(t)
	workDir := t.TempDir()
	t.Chdir(workDir)
	deps, out := newPluginDeps(t.TempDir())

	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--server", srv.URL, "plugins", "install", "--project-local", "--agent", "claude")
	if code != output.ExitOK {
		t.Fatalf("install exit = %d, output = %s", code, out.String())
	}

	raw, err := os.ReadFile(filepath.Join(workDir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("install did not grant the agent permission to run specgate: %v", err)
	}
	if !strings.Contains(string(raw), "Bash(specgate:*)") {
		t.Fatalf("settings.json does not allow the specgate CLI:\n%s", raw)
	}
}

// Settings are the user's file. Installing must add its rule without discarding
// anything already there.
func TestPluginsInstallProjectLocalClaudePreservesExistingSettings(t *testing.T) {
	srv := newPluginRegistry(t)
	workDir := t.TempDir()
	t.Chdir(workDir)
	settings := filepath.Join(workDir, ".claude", "settings.json")
	writeTestFile(t, settings, `{"model":"opus","permissions":{"allow":["Bash(go test:*)"],"deny":["Bash(rm:*)"]}}`)
	if err := os.Chmod(settings, 0o600); err != nil {
		t.Fatal(err)
	}
	deps, out := newPluginDeps(t.TempDir())

	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--server", srv.URL, "plugins", "install", "--project-local", "--agent", "claude")
	if code != output.ExitOK {
		t.Fatalf("install exit = %d, output = %s", code, out.String())
	}

	raw, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("install corrupted settings.json: %v\n%s", err, raw)
	}
	if got["model"] != "opus" {
		t.Fatalf("install dropped an unrelated setting: %s", raw)
	}
	perms, _ := got["permissions"].(map[string]any)
	deny, _ := perms["deny"].([]any)
	if len(deny) != 1 || deny[0] != "Bash(rm:*)" {
		t.Fatalf("install dropped a deny rule: %s", raw)
	}
	allow, _ := perms["allow"].([]any)
	var haveExisting, haveSpecgate bool
	for _, a := range allow {
		switch a {
		case "Bash(go test:*)":
			haveExisting = true
		case "Bash(specgate:*)":
			haveSpecgate = true
		}
	}
	if !haveExisting || !haveSpecgate {
		t.Fatalf("allow list = %v, want both the existing rule and specgate", allow)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(settings)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("settings mode = %o, want preserved 600", got)
		}
	}
}

func TestPluginsInstallProjectLocalClaudeRejectsMalformedSettingsBeforeWriting(t *testing.T) {
	srv := newPluginRegistry(t)
	workDir := t.TempDir()
	t.Chdir(workDir)
	settings := filepath.Join(workDir, ".claude", "settings.json")
	writeTestFile(t, settings, `{"broken"`)
	deps, out := newPluginDeps(t.TempDir())

	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--json", "--server", srv.URL, "plugins", "install", "--project-local", "--agent", "claude")
	if code != output.ExitUsage {
		t.Fatalf("install exit = %d, want usage failure; output = %s", code, out.String())
	}
	for _, want := range []string{`"code":"validation_failed"`, "settings.json", "parse"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("structured error missing %q: %s", want, out.String())
		}
	}
	for _, path := range []string{
		filepath.Join(workDir, ".claude", "skills"),
		filepath.Join(workDir, ".claude", "specgate-hooks"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("install wrote %s before validating settings: %v", path, err)
		}
	}
	raw, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"broken"` {
		t.Fatalf("malformed settings changed: %q", raw)
	}
}

// The governed-repo routing lives in the SessionStart hook. A project-local
// install that ships skills but no hook leaves the agent with no instruction to
// load a phase, which is the whole point of installing into a governed repo.
func TestPluginsInstallProjectLocalClaudeRegistersTheSessionHook(t *testing.T) {
	srv := newPluginRegistry(t)
	workDir := t.TempDir()
	t.Chdir(workDir)
	deps, out := newPluginDeps(t.TempDir())

	code := command.ExecuteForCode(command.NewRootCommand(deps),
		"--plain", "--server", srv.URL, "plugins", "install", "--project-local", "--agent", "claude")
	if code != output.ExitOK {
		t.Fatalf("install exit = %d, output = %s", code, out.String())
	}

	for _, path := range []string{
		filepath.Join(".claude", "specgate-hooks", "session-start"),
		filepath.Join(".claude", "specgate-hooks", "run-hook.cmd"),
	} {
		info, err := os.Stat(filepath.Join(workDir, path))
		if err != nil {
			t.Fatalf("hook asset missing: %v", err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("%s is not executable: %v", path, info.Mode())
		}
	}

	raw, err := os.ReadFile(filepath.Join(workDir, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	hooks, _ := got["hooks"].(map[string]any)
	sessionStart, _ := hooks["SessionStart"].([]any)
	if len(sessionStart) == 0 {
		t.Fatalf("install registered no SessionStart hook:\n%s", raw)
	}
	if !strings.Contains(string(raw), "specgate-hooks") {
		t.Fatalf("SessionStart hook does not point at the installed script:\n%s", raw)
	}
}
