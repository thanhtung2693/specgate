package command_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestDeliveryReportHelpHasNoControlCharacters(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "delivery", "report", "--help")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if strings.ContainsRune(out.String(), '\x00') {
		t.Fatalf("help contains NUL: %q", out.String())
	}
}

func TestDeliverySubmitRequiresFile(t *testing.T) {
	t.Parallel()
	deps, _ := newTestDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", "CR-101")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want %d (ExitUsage)", code, output.ExitUsage)
	}
}

func TestDeliverySubmitRejectsMissingCompletionAgentBeforeNetwork(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"agent":      map[string]any{"name": ""},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", "CR-101", "--file", f)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "agent.name is required") {
		t.Fatalf("error should identify the missing completion agent:\n%s", out.String())
	}
}

func TestDeliverySubmitRejectsNonCompletionEventBeforeNetwork(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.docs_updated",
		"summary":    "updated docs",
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", "CR-101", "--file", f)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "event_type must be coding_agent.completed") {
		t.Fatalf("error should identify the required submit event type:\n%s", out.String())
	}
}

func TestLocalDeliveryReportInitCapturesGitReceiptAndAllCheckBindings(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir := t.TempDir()
	store, err := local.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	selection, err := store.Initialize(t.Context(), local.InitInput{
		WorkspaceName: "Local", DisplayName: "Developer", Username: "developer",
	})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateQuickWork(t.Context(), selection.Workspace.ID, local.QuickWorkInput{
		Title: "Bound local checks",
		AcceptanceCriteria: []string{
			"Unit behavior passes @check:unit",
			"Integration behavior passes @check:integration",
			"Unit behavior remains stable @check:unit",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	deps.WorkingDir = dir
	deps.CommandRunner = deliveryGitRunner(dir, nil)
	path := filepath.Join(t.TempDir(), "completion.json")

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "report", work.Key, "--init="+path)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var scaffold struct {
		GitReceipt map[string]any `json:"git_receipt"`
		Checks     []struct {
			Name string `json:"name"`
		} `json:"checks"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &scaffold); err != nil {
		t.Fatal(err)
	}
	if scaffold.GitReceipt["head_revision"] != "head-revision" {
		t.Fatalf("git receipt = %#v", scaffold.GitReceipt)
	}
	if len(scaffold.Checks) != 2 || scaffold.Checks[0].Name != "unit" || scaffold.Checks[1].Name != "integration" {
		t.Fatalf("checks = %#v, want unique declared bindings", scaffold.Checks)
	}
}

func TestLocalDeliveryReportInitPlainRoutesToChangeSubmit(t *testing.T) {
	deps, out := newTestDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)
	dir := t.TempDir()
	deps.WorkingDir = dir
	deps.CommandRunner = deliveryGitRunner(dir, nil)
	path := filepath.Join(t.TempDir(), "completion.json")

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "report", work.Key, "--init="+path)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	want := "specgate change submit " + work.Key + " --file " + path
	if !strings.Contains(out.String(), want) {
		t.Fatalf("Local scaffold handoff = %q, want command %q", out.String(), want)
	}
	if strings.Contains(out.String(), "specgate delivery submit") {
		t.Fatalf("Local scaffold advertised the diagnostic delivery facade: %s", out.String())
	}
}

func TestDeliverySubmitRejectsSatisfiedClaimWithoutEvidence(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"criteria":   []map[string]any{{"criterion_id": "ac-0", "claim": "satisfied"}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", "CR-101", "--file", f)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "ac-0") || !strings.Contains(out.String(), "evidence") {
		t.Fatalf("error should identify criterion and evidence requirement:\n%s", out.String())
	}
}

func TestDeliverySubmitRejectsNonCanonicalClaim(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"criteria": []map[string]any{{
			"criterion_id": "ac-0",
			"claim":        "satisfied: implemented",
			"evidence":     map[string]any{"kind": "file", "path": "internal/command/delivery.go"},
		}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", "CR-101", "--file", f)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "satisfied, partial, or not_done") {
		t.Fatalf("error should name exact claim enum:\n%s", out.String())
	}
}

func TestDeliverySubmitRejectsPassingCheckWithoutCommand(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"checks":     []map[string]any{{"name": "tests", "status": "pass"}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", "CR-101", "--file", f)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "tests") || !strings.Contains(out.String(), "command") {
		t.Fatalf("error should identify check and command requirement:\n%s", out.String())
	}
}

func TestDeliverySubmitRejectsNonCanonicalCheckStatus(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"checks":     []map[string]any{{"name": "tests", "command": "go test ./...", "status": "passed"}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", "CR-101", "--file", f)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "pass, fail, or skipped") {
		t.Fatalf("error should name exact check status enum:\n%s", out.String())
	}
}

func TestDeliverySubmitRejectsMissingEvidencePaths(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	missing := filepath.Join(t.TempDir(), "internal", "api", "fabricated_handler.go")
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"criteria": []map[string]any{
			{
				"criterion_id": "ac-0",
				"claim":        "satisfied",
				"evidence":     map[string]any{"kind": "file", "path": missing},
			},
		},
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", "CR-101", "--file", f)
	if code == output.ExitOK {
		t.Fatalf("expected non-zero exit for missing evidence path; output = %s", out.String())
	}
	got := out.String()
	if !strings.Contains(got, "fabricated_handler.go") {
		t.Fatalf("error should name the missing path:\n%s", got)
	}
	if !strings.Contains(got, "--skip-evidence-check") {
		t.Fatalf("error should mention the escape hatch:\n%s", got)
	}
}

func TestLocalScaffoldDefaultPathConflictAndSubmission(t *testing.T) {
	deps, out := newTestDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)
	t.Chdir(deps.WorkingDir)
	execute := func(args ...string) int {
		out.Reset()
		return command.ExecuteForCode(command.NewRootCommand(deps), append([]string{"--json"}, args...)...)
	}
	path := filepath.Join(".specgate", "completion-"+work.Key+".json")
	if code := execute("delivery", "report", work.Key, "--init"); code != output.ExitOK || !strings.Contains(out.String(), `"criteria":1`) {
		t.Fatalf("scaffold = %d: %s", code, out.String())
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if code := execute("delivery", "report", work.Key, "--init"); code != output.ExitUsage || !strings.Contains(out.String(), filepath.Base(path)) {
		t.Fatalf("scaffold conflict = %d: %s", code, out.String())
	}
	if unchanged, err := os.ReadFile(path); err != nil || string(unchanged) != string(original) {
		t.Fatalf("existing scaffold changed: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(original, &body); err != nil {
		t.Fatal(err)
	}
	body["agent"] = map[string]any{"name": "builder"}
	body["criteria"].([]any)[0].(map[string]any)["claim"] = "satisfied"
	body["criteria"].([]any)[0].(map[string]any)["evidence"] = map[string]any{"heading": "A verified result"}
	body["checks"] = []any{map[string]any{"name": "unit", "status": "pass", "command": "true"}}
	completed, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, completed, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := execute("change", "submit", work.Key); code != output.ExitOK || !strings.Contains(out.String(), `"state":"awaiting_acceptance"`) {
		t.Fatalf("submit default file = %d: %s", code, out.String())
	}
}
