package command_test

import (
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
)

func TestAgentWorkflowHelpIncludesRunnableExamples(t *testing.T) {
	t.Parallel()
	deps, _, _, _ := newFakeDeps(t)
	root := command.NewRootCommand(deps)

	tests := []struct {
		path []string
		want string
	}{
		{[]string{"artifact", "publish"}, "--preview --compare"},
		{[]string{"gates", "check"}, "--summary"},
		{[]string{"work", "create"}, "--ac"},
		{[]string{"work", "context"}, "--json"},
		{[]string{"delivery", "submit"}, "--file"},
	}

	for _, tt := range tests {
		cmd, _, err := root.Find(tt.path)
		if err != nil {
			t.Fatalf("find %v: %v", tt.path, err)
		}
		help := cmd.Long + "\n" + cmd.Example
		if !strings.Contains(help, tt.want) {
			t.Errorf("%s help missing %q:\n%s", strings.Join(tt.path, " "), tt.want, help)
		}
	}
}

func TestRootHelpDescribesGovernedDelivery(t *testing.T) {
	t.Parallel()
	deps, _, _, _ := newFakeDeps(t)
	root := command.NewRootCommand(deps)

	if !strings.Contains(root.Short, "govern") || !strings.Contains(root.Short, "delivery") {
		t.Fatalf("root summary = %q, want current governed-delivery scope", root.Short)
	}
}

func TestChangeHelpShowsTheCompleteRunnableFacade(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	root := command.NewRootCommand(deps)
	root.SetOut(out)
	root.SetErr(out)

	code := command.ExecuteForCode(root, "change", "--help")
	if code != 0 {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	for _, want := range []string{
		"accept",
		"request-changes",
		"status",
		"submit",
		"specgate change status CR-123",
		"\n  specgate change submit CR-123\n",
		"specgate change submit CR-123 --file .specgate/completion-CR-123.json",
		"specgate --yes change accept CR-123",
		"specgate --yes change request-changes CR-123",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("change help missing runnable facade entry %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	code = command.ExecuteForCode(command.NewRootCommand(deps), "change", "submit", "--help")
	if code != 0 {
		t.Fatalf("submit help exit = %d, output = %s", code, out.String())
	}
	for _, want := range []string{
		"Default completion file: .specgate/completion-<ref>.json",
		"letters, digits, hyphens (-), and underscores (_)",
		"--file is required when the ref is not file-safe",
		"specgate change submit CR-123",
		"specgate change submit CR-123 --file .specgate/completion-CR-123.json",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("change submit help missing %q:\n%s", want, out.String())
		}
	}
}

func TestInitHelpShowsRunnableOnboardingExamples(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	root := command.NewRootCommand(deps)
	root.SetOut(out)
	root.SetErr(out)

	code := command.ExecuteForCode(root, "init", "--help")
	if code != 0 {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	for _, want := range []string{
		"specgate init",
		"specgate init --mode local --no-input",
		"specgate init --mode full",
		"--workspace-name",
		"--display-name",
		"--username",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("help missing %q:\n%s", want, out.String())
		}
	}
}

func TestInitHelpLabelsFullOnlyFlags(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	root := command.NewRootCommand(deps)
	root.SetOut(out)
	root.SetErr(out)

	code := command.ExecuteForCode(root, "init", "--help")
	if code != 0 {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	for _, flag := range []string{"--dir", "--seed", "--no-seed", "--bundle-version"} {
		line := ""
		for _, candidate := range strings.Split(out.String(), "\n") {
			if strings.Contains(candidate, flag) {
				line = candidate
				break
			}
		}
		if !strings.Contains(line, "Full mode only") {
			t.Errorf("%s help line = %q, want Full mode scope", flag, line)
		}
	}
}
