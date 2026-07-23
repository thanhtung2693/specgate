package command_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// --- fakes ---

func TestWorkArchiveDeclinePrintsCancelled(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t) // fakePrompter confirmValue defaults to false
	deps.StdinIsTTY = func() bool { return true }

	code := command.ExecuteForCode(command.NewRootCommand(deps), "work", "archive", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("calls = %d, want 0 (declined confirm must not archive)", fc.calls)
	}
	if !strings.Contains(out.String(), "Cancelled.") {
		t.Fatalf("output = %q, want Cancelled.", out.String())
	}
}

func TestWorkCreateSendsFeatureBodyAndRendersResult(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.createWorkItemResult = map[string]any{
		"change_request_key": "CR-77", "feature_key": "my-feat",
		"lead_artifact_id": "art-1", "acceptance_criteria": []any{"a", "b"},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain",
		"work", "create", "--feature", "my-feat", "--title", "Do it", "--ac", "a")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, out = %s", code, out.String())
	}
	if fc.lastCreateWorkItem["feature"] != "my-feat" {
		t.Fatalf("body = %+v", fc.lastCreateWorkItem)
	}
	if !strings.Contains(out.String(), "Created CR-77") || !strings.Contains(out.String(), "2 acceptance criteria") {
		t.Fatalf("render = %s", out.String())
	}
}

func TestWorkListMissingWorkspaceOverridePrintsJSONError(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.getWorkspaceErr = &client.APIError{Kind: client.ErrorNotFound, Status: 404, Message: "workspace not found"}

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "--workspace", "missing-workspace", "work", "list",
	)
	if code != output.ExitNotFound {
		t.Fatalf("exit = %d, want not found; output = %q", code, out.String())
	}
	var envelope struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("workspace selection error is not a JSON envelope: %q: %v", out.String(), err)
	}
	if envelope.OK || envelope.Error.Code != "not_found" {
		t.Fatalf("workspace selection envelope = %+v; output = %s", envelope, out.String())
	}
}

func TestWorkCreateRequiresFeatureAndTitle(t *testing.T) {
	t.Parallel()
	deps, _, _, _ := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "create", "--title", "T")
	if code == output.ExitOK {
		t.Fatal("missing --feature must fail")
	}
}

// Coding agents consume work show via --json; the envelope must carry the
// acceptance criteria (the JSON path used to return bare ResolvedWork and agents
// saw none even when normalized rows existed).
func TestWorkShowJSONIncludesAcceptanceCriteria(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.acceptanceCriteria = []client.AcceptanceCriterion{{ID: "ac-1", Text: "First AC"}}
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		Found:        true,
		PerCriterion: []client.CriterionReview{{CriterionID: "ac-1", Verdict: "met"}},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "show", "CR-1")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, out = %s", code, out.String())
	}
	var env struct {
		Data struct {
			AcceptanceCriteria []client.AcceptanceCriterion `json:"acceptance_criteria"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data.AcceptanceCriteria) != 1 ||
		env.Data.AcceptanceCriteria[0].Text != "First AC" ||
		!env.Data.AcceptanceCriteria[0].Done {
		t.Fatalf("acceptance_criteria missing from JSON envelope: %s", out.String())
	}
}
