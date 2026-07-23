package command_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestDeliveryStatusNotFound(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		ChangeRequestID: "cr-1",
		Found:           false,
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "status", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "No delivery review") {
		t.Errorf("output missing 'No delivery review' message:\n%s", got)
	}
}

func TestDeliveryReportHelpHasNoControlCharacters(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "delivery", "report", "--help")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if strings.ContainsRune(out.String(), '\x00') {
		t.Fatalf("help contains NUL: %q", out.String())
	}
}

func TestDeliveryStatusDetailFlag(t *testing.T) {
	t.Parallel()
	deps, fc, _, _ := newFakeDeps(t)
	command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "status", "CR-101", "--detail") //nolint:errcheck
	if !fc.lastDetailFlag {
		t.Fatalf("lastDetailFlag = false, want true")
	}
}

func TestDeliveryStatusJSON(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		ChangeRequestID: "cr-1",
		Found:           true,
		Verdict:         "pass",
		JudgeModel:      "agent_attested",
		EvalSuite:       "delivery-review-v1",
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "status", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			JudgeModel string `json:"judge_model"`
			EvalSuite  string `json:"eval_suite_version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.OK {
		t.Fatalf("ok = false: %s", out.String())
	}
	if env.Data.JudgeModel != "agent_attested" || env.Data.EvalSuite != "delivery-review-v1" {
		t.Fatalf("metadata = %#v", env.Data)
	}
}

// --- delivery submit ---

func TestDeliverySubmitChainsAllStages(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		ChangeRequestID: "cr-1",
		Found:           true,
		Verdict:         "pass",
		PerCriterion: []client.CriterionReview{
			{CriterionID: "ac-1", Verdict: "met", Why: "Verified."},
		},
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "All done",
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "submit", "CR-101", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastFeedbackBody == nil || fc.lastFeedbackBody["event_type"] != "coding_agent.completed" {
		t.Fatalf("feedback body = %v", fc.lastFeedbackBody)
	}
	// report + gates + review + status
	if fc.calls != 4 {
		t.Fatalf("calls = %d, want 4", fc.calls)
	}
	if !fc.lastDetailFlag {
		t.Fatalf("DeliveryStatus detail = false, want true")
	}
	got := out.String()
	for _, want := range []string{"1/4", "2/4", "3/4", "4/4", "Ready for human review", "ac-1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestDeliverySubmitJSONEnvelopeHasAllSections(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "All done",
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", "CR-101", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Report map[string]any `json:"report"`
			Gates  map[string]any `json:"gates"`
			Review map[string]any `json:"review"`
			Status map[string]any `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v, output: %s", err, out.String())
	}
	if !env.OK {
		t.Fatalf("ok = false: %s", out.String())
	}
	if env.Data.Report == nil || env.Data.Gates == nil || env.Data.Review == nil || env.Data.Status == nil {
		t.Fatalf("envelope missing sections: %s", out.String())
	}
}

func TestDeliverySubmitStopsOnStageFailure(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.gatesRunErr = errors.New("gates backend down")
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "All done",
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", "CR-101", "--file", f)
	if code == output.ExitOK {
		t.Fatalf("expected non-zero exit, got OK; output=%q", out.String())
	}
	// report + gates only; review and status never run
	if fc.calls != 2 {
		t.Fatalf("calls = %d, want 2 (stop after failing stage)", fc.calls)
	}
	var env struct {
		OK    bool `json:"ok"`
		Error *struct {
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v, output: %s", err, out.String())
	}
	if env.OK || env.Error == nil {
		t.Fatalf("expected error envelope: %s", out.String())
	}
	if env.Error.Details["stage"] != "gates" {
		t.Fatalf("details.stage = %v, want gates: %s", env.Error.Details["stage"], out.String())
	}
}

func TestDeliverySubmitRequiresFile(t *testing.T) {
	t.Parallel()
	deps, fc, _, _ := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", "CR-101")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want %d (ExitUsage)", code, output.ExitUsage)
	}
	if fc.calls != 0 {
		t.Fatalf("calls = %d, want 0", fc.calls)
	}
}

func TestDeliverySubmitRejectsMissingCompletionAgentBeforeNetwork(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"agent":      map[string]any{"name": ""},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "submit", "CR-101", "--file", f)
	if code != output.ExitUsage || fc.calls != 0 {
		t.Fatalf("exit = %d, calls = %d, output = %s", code, fc.calls, out.String())
	}
	if !strings.Contains(out.String(), "agent.name is required") {
		t.Fatalf("error should identify the missing completion agent:\n%s", out.String())
	}
}

func TestDeliverySubmitRejectsNonCompletionEventBeforeNetwork(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.docs_updated",
		"summary":    "updated docs",
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "submit", "CR-101", "--file", f)
	if code != output.ExitUsage || fc.calls != 0 {
		t.Fatalf("exit = %d, calls = %d, output = %s", code, fc.calls, out.String())
	}
	if !strings.Contains(out.String(), "event_type must be coding_agent.completed") {
		t.Fatalf("error should identify the required submit event type:\n%s", out.String())
	}
}

// --- delivery report --init ---

func TestDeliveryReportInitScaffoldsCompletionTemplate(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.acceptanceCriteria = []client.AcceptanceCriterion{
		{ID: "ac-1", Text: "Cannot over-redeem", VerificationBinding: "integration"},
		{ID: "ac-2", Text: "Audit log exists", VerificationBinding: "audit"},
		{ID: "ac-3", Text: "Retry remains safe", VerificationBinding: "integration"},
	}
	path := filepath.Join(t.TempDir(), "completion.json")
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "report", "CR-101", "--init="+path)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastACWorkID != "cr-1" {
		t.Fatalf("lastACWorkID = %q, want cr-1", fc.lastACWorkID)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var tpl struct {
		EventType string `json:"event_type"`
		Summary   string `json:"summary"`
		Agent     struct {
			Name string `json:"name"`
		} `json:"agent"`
		AffectedFiles []string `json:"affected_files"`
		Checks        []struct {
			Name    string `json:"name"`
			Command string `json:"command"`
			Status  string `json:"status"`
			Detail  string `json:"detail"`
		} `json:"checks"`
		Criteria []struct {
			CriterionID         string            `json:"criterion_id"`
			Text                string            `json:"text"`
			Claim               string            `json:"claim"`
			VerificationBinding string            `json:"verification_binding"`
			Evidence            map[string]string `json:"evidence"`
		} `json:"criteria"`
	}
	if err := json.Unmarshal(data, &tpl); err != nil {
		t.Fatalf("template is not valid JSON: %v\n%s", err, data)
	}
	if tpl.EventType != "coding_agent.completed" {
		t.Fatalf("event_type = %q", tpl.EventType)
	}
	if tpl.Agent.Name != "" {
		t.Fatalf("agent.name = %q, want blank scaffold value", tpl.Agent.Name)
	}
	if len(tpl.Criteria) != 3 || tpl.Criteria[0].CriterionID != "ac-1" ||
		tpl.Criteria[0].Text != "Cannot over-redeem" || tpl.Criteria[0].Claim != "not_done" {
		t.Fatalf("criteria = %+v", tpl.Criteria)
	}
	// The declared check-binding is scaffolded so the agent keeps it in the report
	//; an unbound criterion omits it.
	if tpl.Criteria[0].VerificationBinding != "integration" {
		t.Fatalf("criteria[0].verification_binding = %q, want integration", tpl.Criteria[0].VerificationBinding)
	}
	if tpl.Criteria[1].VerificationBinding != "audit" {
		t.Fatalf("criteria[1].verification_binding = %q, want audit", tpl.Criteria[1].VerificationBinding)
	}
	if tpl.Criteria[0].Evidence == nil {
		t.Fatalf("criteria[0].evidence missing:\n%s", data)
	}
	if len(tpl.AffectedFiles) != 0 || len(tpl.Checks) != 2 {
		t.Fatalf("expected empty affected_files and one check per unique binding:\n%s", data)
	}
	if tpl.Checks[0].Name != "integration" || tpl.Checks[1].Name != "audit" ||
		tpl.Checks[0].Command != "" || tpl.Checks[0].Status != "pending" ||
		!strings.Contains(tpl.Checks[0].Detail, "pass, fail, or skipped") {
		t.Fatalf("check examples = %+v, want an explicit pending placeholder with next-action guidance", tpl.Checks)
	}
	if !strings.Contains(out.String(), path) {
		t.Fatalf("output should mention where the template was written:\n%s", out.String())
	}
	// No feedback event must be posted in --init mode.
	if fc.lastFeedbackBody != nil {
		t.Fatalf("ReportFeedback was called in --init mode")
	}
}

func TestLocalDeliveryReportInitCapturesGitReceiptAndAllCheckBindings(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
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
	deps.DeployRunner = deliveryGitRunner(dir, nil)
	path := filepath.Join(t.TempDir(), "completion.json")

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "report", work.Key, "--init="+path)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("Local scaffold made %d HTTP calls", fc.calls)
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
	deps, _, _, out := newFakeDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)
	deps.Client = nil
	dir := t.TempDir()
	deps.WorkingDir = dir
	deps.DeployRunner = deliveryGitRunner(dir, nil)
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

func TestDeliveryReportInitAcceptsSpaceSeparatedPath(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.acceptanceCriteria = []client.AcceptanceCriterion{{ID: "ac-1", Text: "AC"}}
	path := filepath.Join(t.TempDir(), "completion.json")

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "report", "CR-101", "--init", path)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected template at space-separated --init path: %v", err)
	}
	if fc.lastACWorkID != "cr-1" {
		t.Fatalf("lastACWorkID = %q, want cr-1", fc.lastACWorkID)
	}
}

// TestDeliveryReportInitBareWritesUnderSpecgateDir: bare `--init` (no explicit
// path) writes the per-work-item scaffold into the project-local `.specgate/`
// working directory as completion-<ref>.json, not the repo root.
func TestDeliveryReportInitBareWritesUnderSpecgateDir(t *testing.T) {
	// No t.Parallel: t.Chdir is incompatible with parallel tests.
	deps, fc, _, out := newFakeDeps(t)
	fc.acceptanceCriteria = []client.AcceptanceCriterion{{ID: "ac-1", Text: "AC"}}
	dir := t.TempDir()
	t.Chdir(dir)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "report", "CR-101", "--init")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	want := filepath.Join(dir, ".specgate", "completion-CR-101.json")
	info, err := os.Stat(want)
	if err != nil {
		t.Fatalf("expected scaffold under .specgate/: %v\noutput: %s", err, out.String())
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("completion scaffold mode = %04o, want 0600", got)
	}
	dirInfo, err := os.Stat(filepath.Join(dir, ".specgate"))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf(".specgate mode = %04o, want 0700", got)
	}
	// Bare --init must NOT write into the repo root / cwd.
	if _, err := os.Stat(filepath.Join(dir, "completion-CR-101.json")); err == nil {
		t.Fatalf("bare --init wrote to cwd root instead of .specgate/")
	}
	if !strings.Contains(out.String(), filepath.Join(".specgate", "completion-CR-101.json")) {
		t.Fatalf("output should name the .specgate/ scaffold path:\n%s", out.String())
	}
}

// TestSpecgateWorkingDirIsGitignored: the .specgate/ working directory (which
// holds the delivery-report scaffold) must be gitignored so transient files are
// never committed.
func TestSpecgateWorkingDirIsGitignored(t *testing.T) {
	// No t.Parallel: t.Chdir is incompatible with parallel tests.
	deps, fc, _, outBuf := newFakeDeps(t)
	fc.acceptanceCriteria = []client.AcceptanceCriterion{{ID: "ac-1", Text: "AC"}}
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}
	t.Chdir(dir)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "report", "CR-101", "--init")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, outBuf.String())
	}

	// git check-ignore exits 0 when the scaffold is ignored by the nested
	// .specgate/.gitignore that delivery report --init writes.
	out, err := exec.Command("git", "check-ignore", ".specgate/completion-CR-101.json").CombinedOutput()
	if err != nil {
		t.Fatalf(".specgate scaffold is not gitignored (git check-ignore failed): %v\n%s", err, out)
	}
}

func TestDeliveryReportInitRefusesOverwriteWithoutForce(t *testing.T) {
	t.Parallel()
	deps, fc, _, _ := newFakeDeps(t)
	fc.acceptanceCriteria = []client.AcceptanceCriterion{{ID: "ac-1", Text: "AC"}}
	path := filepath.Join(t.TempDir(), "completion.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "report", "CR-101", "--init="+path)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want %d (ExitUsage)", code, output.ExitUsage)
	}
	if got, _ := os.ReadFile(path); string(got) != "{}" {
		t.Fatalf("file was overwritten without --force: %s", got)
	}

	code = command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "report", "CR-101", "--init="+path, "--force")
	if code != output.ExitOK {
		t.Fatalf("exit with --force = %d, want 0", code)
	}
	if got, _ := os.ReadFile(path); string(got) == "{}" {
		t.Fatalf("file was not overwritten with --force")
	}
}

func TestDeliveryReportInitJSONReturnsExistingScaffoldPathWithoutOverwriting(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.acceptanceCriteria = []client.AcceptanceCriterion{{ID: "ac-1", Text: "AC"}}
	path := filepath.Join(t.TempDir(), "completion.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "report", "CR-101", "--init="+path)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want %d; output=%s", code, output.ExitUsage, out.String())
	}
	var env struct {
		Error struct {
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v; output=%s", err, out.String())
	}
	if got := env.Error.Details["path"]; got != path {
		t.Fatalf("error.details.path = %#v, want %q; output=%s", got, path, out.String())
	}
	if got, _ := os.ReadFile(path); string(got) != "{}" {
		t.Fatalf("existing scaffold was overwritten: %s", got)
	}
}

func TestDeliveryReportInitJSONReportsPathAndCount(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.acceptanceCriteria = []client.AcceptanceCriterion{{ID: "ac-1", Text: "AC"}}
	path := filepath.Join(t.TempDir(), "completion.json")
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "report", "CR-101", "--init="+path)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Path     string `json:"path"`
			Criteria int    `json:"criteria"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.OK || env.Data.Path != path || env.Data.Criteria != 1 {
		t.Fatalf("unexpected envelope: %s", out.String())
	}
	var scaffold struct {
		ChangeRequestID string `json:"change_request_id"`
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &scaffold); err != nil {
		t.Fatal(err)
	}
	if scaffold.ChangeRequestID != "cr-1" {
		t.Fatalf("change_request_id = %q, want cr-1; scaffold=%s", scaffold.ChangeRequestID, body)
	}
}

func TestDeliveryReportInjectsRequiredEnvelopeFields(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	dir := t.TempDir()
	path := dir + "/completion.json"
	// Scaffold-shaped body: no change_request_id or severity, like --init emits.
	if err := os.WriteFile(path, []byte(`{"event_type":"coding_agent.completed","summary":"done","agent":{"name":"builder"},"criteria":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "report", "CR-1", "--file", path)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if got := fc.lastFeedbackBody["change_request_id"]; got != "cr-1" {
		t.Errorf("change_request_id = %v, want cr-1", got)
	}
	if got := fc.lastFeedbackBody["severity"]; got != "info" {
		t.Errorf("severity = %v, want info", got)
	}
}

func TestDeliverySubmitInjectsRequiredEnvelopeFields(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	dir := t.TempDir()
	path := dir + "/completion.json"
	if err := os.WriteFile(path, []byte(`{"event_type":"coding_agent.completed","summary":"done","agent":{"name":"builder"},"criteria":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "submit", "CR-1", "--file", path)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if got := fc.lastFeedbackBody["change_request_id"]; got != "cr-1" {
		t.Errorf("change_request_id = %v, want cr-1", got)
	}
	if got := fc.lastFeedbackBody["severity"]; got != "info" {
		t.Errorf("severity = %v, want info", got)
	}
}

// --- completion evidence verification ---

func TestDeliverySubmitRejectsSatisfiedClaimWithoutEvidence(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"criteria":   []map[string]any{{"criterion_id": "ac-0", "claim": "satisfied"}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "submit", "CR-101", "--file", f)
	if code != output.ExitUsage || fc.calls != 0 {
		t.Fatalf("exit = %d, calls = %d, output = %s", code, fc.calls, out.String())
	}
	if !strings.Contains(out.String(), "ac-0") || !strings.Contains(out.String(), "evidence") {
		t.Fatalf("error should identify criterion and evidence requirement:\n%s", out.String())
	}
}

func TestDeliverySubmitRejectsNonCanonicalClaim(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"criteria": []map[string]any{{
			"criterion_id": "ac-0",
			"claim":        "satisfied: implemented",
			"evidence":     map[string]any{"kind": "file", "path": "internal/command/delivery.go"},
		}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "submit", "CR-101", "--file", f)
	if code != output.ExitUsage || fc.calls != 0 {
		t.Fatalf("exit = %d, calls = %d, output = %s", code, fc.calls, out.String())
	}
	if !strings.Contains(out.String(), "satisfied, partial, or not_done") {
		t.Fatalf("error should name exact claim enum:\n%s", out.String())
	}
}

func TestDeliverySubmitRejectsPassingCheckWithoutCommand(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"checks":     []map[string]any{{"name": "tests", "status": "pass"}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "submit", "CR-101", "--file", f)
	if code != output.ExitUsage || fc.calls != 0 {
		t.Fatalf("exit = %d, calls = %d, output = %s", code, fc.calls, out.String())
	}
	if !strings.Contains(out.String(), "tests") || !strings.Contains(out.String(), "command") {
		t.Fatalf("error should identify check and command requirement:\n%s", out.String())
	}
}

func TestDeliverySubmitRejectsNonCanonicalCheckStatus(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"checks":     []map[string]any{{"name": "tests", "command": "go test ./...", "status": "passed"}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "submit", "CR-101", "--file", f)
	if code != output.ExitUsage || fc.calls != 0 {
		t.Fatalf("exit = %d, calls = %d, output = %s", code, fc.calls, out.String())
	}
	if !strings.Contains(out.String(), "pass, fail, or skipped") {
		t.Fatalf("error should name exact check status enum:\n%s", out.String())
	}
}

func TestDeliverySubmitRejectsMissingEvidencePaths(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
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
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "submit", "CR-101", "--file", f)
	if code == output.ExitOK {
		t.Fatalf("expected non-zero exit for missing evidence path; output = %s", out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("calls = %d, want 0 (must fail before any network call)", fc.calls)
	}
	got := out.String()
	if !strings.Contains(got, "fabricated_handler.go") {
		t.Fatalf("error should name the missing path:\n%s", got)
	}
	if !strings.Contains(got, "--skip-evidence-check") {
		t.Fatalf("error should mention the escape hatch:\n%s", got)
	}
}

func TestDeliverySubmitAcceptsExistingEvidencePathsAndSkipsEmpty(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	existing := filepath.Join(t.TempDir(), "handler.go")
	if err := os.WriteFile(existing, []byte("package api\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"criteria": []map[string]any{
			{
				"criterion_id": "ac-0",
				"claim":        "satisfied",
				"evidence":     map[string]any{"kind": "file", "path": existing},
			},
			{
				// Scaffold-shaped empty evidence must not trip verification.
				"criterion_id": "ac-1",
				"claim":        "not_done",
				"evidence":     map[string]any{"kind": "", "path": ""},
			},
		},
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", "CR-101", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.calls != 4 {
		t.Fatalf("calls = %d, want 4", fc.calls)
	}
}

func TestDeliverySubmitGroundsCriterionEvidenceFromCitedFile(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	dir := t.TempDir()
	existing := filepath.Join(dir, "handler.go")
	if err := os.WriteFile(existing, []byte("package api\n\nfunc Handler() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"criteria": []map[string]any{
			{
				"criterion_id": "ac-0",
				"claim":        "satisfied",
				"evidence":     map[string]any{"kind": "file", "path": existing, "line": 3},
			},
		},
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", "CR-101", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	criteria, _ := fc.lastFeedbackBody["criteria"].([]any)
	if len(criteria) != 1 {
		t.Fatalf("criteria = %#v", fc.lastFeedbackBody["criteria"])
	}
	entry, _ := criteria[0].(map[string]any)
	evidence, _ := entry["evidence"].(map[string]any)
	grounding, _ := evidence["grounding"].(map[string]any)
	if grounding["status"] != "grounded" {
		t.Fatalf("grounding = %#v, want grounded", grounding)
	}
	if got := grounding["excerpt"]; !strings.Contains(fmt.Sprint(got), "func Handler") {
		t.Fatalf("excerpt = %#v, want cited line", got)
	}
	if got := fmt.Sprint(grounding["digest"]); !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("digest = %q, want sha256 digest", got)
	}
}
