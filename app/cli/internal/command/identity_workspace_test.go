package command_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestWorkspaceHelpShowsProjectExamples(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	root := command.NewRootCommand(deps)
	root.SetOut(out)
	root.SetErr(out)

	code := command.ExecuteForCode(root, "workspace", "--help")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	for _, want := range []string{"workspace select", "workspace bind", "workspace current", "workspace unbind", "--workspace platform"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("help missing %q:\n%s", want, out.String())
		}
	}
}

func TestWorkspaceSelectNoInputRequiresSlug(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	deps.Stderr = out
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "workspace", "select")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}

	if !strings.Contains(out.String(), "workspace slug is required") {
		t.Fatalf("output missing validation message: %s", out.String())
	}
}

// Signing in records the new user and global workspace without unbinding other
// checkouts. Only `user logout` clears project bindings, which is the behavior
// the configuration reference documents.

func TestUserLoginUnknownFlagReportsPlainUsageError(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	deps.Stderr = out

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--plain",
		"user",
		"login",
		"--name",
		"New User",
	)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "Error (usage): unknown flag: --name") {
		t.Fatalf("output = %q, want readable usage error", out.String())
	}
	if strings.Contains(out.String(), "schema_version") {
		t.Fatalf("plain output leaked JSON envelope: %q", out.String())
	}

}

func TestUserLoginUnknownFlagHonorsTrailingJSONFlag(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"user",
		"login",
		"--name",
		"New User",
		"--json",
	)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), `"command":"user.login"`) ||
		!strings.Contains(out.String(), `"code":"usage"`) ||
		strings.Contains(out.String(), "Error (usage):") {
		t.Fatalf("output = %q, want JSON usage envelope", out.String())
	}

}

func TestUserLoginJSONRequiresRequiredFields(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "user", "login", "--username", "thanhtung2693")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}

	if !strings.Contains(out.String(), "--workspace and --display-name are required") {
		t.Fatalf("output missing validation message: %s", out.String())
	}
}

func TestUserLoginPlainDoesNotPromptForRequiredFields(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	deps.Stderr = out
	fp := &fakePrompter{}
	deps.Prompter = fp
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "user", "login")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}

	if fp.inputCalls != 0 {
		t.Fatalf("plain login prompted %d time(s)", fp.inputCalls)
	}
	if !strings.Contains(out.String(), "--workspace, --display-name, and --username are required") {
		t.Fatalf("output missing validation message: %s", out.String())
	}
}
