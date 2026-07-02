package command_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestSkillListPlain(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.skillsResult = []client.Skill{
		{ID: "skill-001-abcd", Name: "specgate-handoff", Description: "SpecGate handoff skill"},
		{ID: "skill-002-efgh", Name: "tdd-go", Description: "Go TDD patterns"},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "skill", "list")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "specgate-handoff") {
		t.Errorf("output missing specgate-handoff:\n%s", got)
	}
	if !strings.Contains(got, "tdd-go") {
		t.Errorf("output missing tdd-go:\n%s", got)
	}
}

func TestSkillListJSON(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.skillsResult = []client.Skill{
		{ID: "skill-001", Name: "my-skill", Description: "Short", Prompt: strings.Repeat("Long rubric. ", 100)},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "skill", "list")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.OK {
		t.Fatalf("ok = false: %s", out.String())
	}
	if len(env.Data.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(env.Data.Items))
	}
	if _, ok := env.Data.Items[0]["prompt"]; ok {
		t.Fatalf("skill list JSON should omit prompt by default: %s", out.String())
	}
}

func TestSkillListJSONCanIncludePrompts(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.skillsResult = []client.Skill{
		{ID: "skill-001", Name: "my-skill", Prompt: "Full rubric"},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "skill", "list", "--include-prompt")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "Full rubric") {
		t.Fatalf("output missing prompt with --include-prompt: %s", out.String())
	}
}

func TestSkillListWithNameFilter(t *testing.T) {
	t.Parallel()
	deps, fc, _, _ := newFakeDeps(t)
	command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "skill", "list", "--name", "tdd")
	if fc.lastSkillsFilter != "tdd" {
		t.Fatalf("lastSkillsFilter = %q, want tdd", fc.lastSkillsFilter)
	}
}

func TestSkillShowByID(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.skillResult = &client.Skill{ID: "skill-99", Name: "my-skill", Description: "A skill", Prompt: "Do the thing"}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "skill", "show", "skill-99")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastSkillID != "skill-99" {
		t.Errorf("lastSkillID = %q, want skill-99", fc.lastSkillID)
	}
	got := out.String()
	if !strings.Contains(got, "my-skill") {
		t.Errorf("output missing skill name:\n%s", got)
	}
	if !strings.Contains(got, "Do the thing") {
		t.Errorf("output missing prompt:\n%s", got)
	}
}

func TestSkillShowJSON(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.skillResult = &client.Skill{ID: "skill-1", Name: "foo"}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "skill", "show", "skill-1")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.OK {
		t.Fatalf("ok = false: %s", out.String())
	}
}
