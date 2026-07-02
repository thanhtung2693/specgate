package command_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// --- delivery review ---

func TestDeliveryReviewWithYesFlag(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--yes", "delivery", "review", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastGatesID != "cr-1" {
		t.Errorf("lastGatesID = %q, want cr-1", fc.lastGatesID)
	}
}

func TestDeliveryReviewDeclinedNoConfirmation(t *testing.T) {
	t.Parallel()
	deps, fc, _, _ := newFakeDeps(t)
	deps.StdinIsTTY = func() bool { return true } // interactive terminal session
	// fakePrompter.confirmValue defaults to false → declined
	command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "review", "CR-101") //nolint:errcheck
	if fc.lastGatesID != "" {
		t.Errorf("TriggerDeliveryReview was called despite declined confirmation")
	}
}

// TestDeliveryReviewNonTTYProceedsWithoutConfirm verifies the confirm prompt is
// TTY-only: a non-interactive session (piped stdin) without --yes proceeds.
func TestDeliveryReviewNonTTYProceedsWithoutConfirm(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t) // Stdin is a strings.Reader → not a TTY
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "review", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastGatesID != "cr-1" {
		t.Errorf("lastGatesID = %q, want cr-1 (review should trigger without prompting)", fc.lastGatesID)
	}
}

func TestDeliveryReviewJSON(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "delivery", "review", "CR-101")
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

// TestDeliveryReviewJSONWithoutYesProceeds mirrors gates run: `delivery review
// --json` without --yes proceeds with a success envelope (confirms are
// TTY-only ceremony; --yes stays accepted for compatibility).
func TestDeliveryReviewJSONWithoutYesProceeds(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "review", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("expected a JSON envelope, got unparseable output %q: %v", out.String(), err)
	}
	if !env.OK {
		t.Fatalf("ok = false: %s", out.String())
	}
	if fc.lastGatesID != "cr-1" {
		t.Errorf("lastGatesID = %q, want cr-1 (review should trigger without --yes)", fc.lastGatesID)
	}
}

// --- delivery report ---

func TestDeliveryReportFromFile(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	body := map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "All tests pass",
	}
	f := writeTempJSON(t, body)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "report", "CR-101", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastFeedbackBody == nil {
		t.Fatal("feedback body not recorded")
	}
	if fc.lastFeedbackBody["event_type"] != "coding_agent.completed" {
		t.Errorf("event_type = %v, want coding_agent.completed", fc.lastFeedbackBody["event_type"])
	}
}

func TestDeliveryReportRejectsInvalidJSONBeforeHTTP(t *testing.T) {
	t.Parallel()
	deps, fc, _, _ := newFakeDeps(t)
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{broken"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := command.NewRootCommand(deps)
	cmd.SetArgs([]string{"delivery", "report", "CR-101", "--file", bad})
	cmd.Execute() //nolint:errcheck
	if fc.calls != 0 {
		t.Fatalf("calls = %d after invalid JSON, want 0", fc.calls)
	}
}

func TestDeliveryReportRequiresFile(t *testing.T) {
	t.Parallel()
	deps, _, _, _ := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--no-input", "delivery", "report", "CR-101")
	if code == output.ExitOK {
		t.Fatal("expected non-zero exit when --file is missing in --no-input mode")
	}
}

// --- delivery status ---

func TestDeliveryStatusPlain(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		ChangeRequestID: "cr-1",
		Found:           true,
		Verdict:         "pass",
		Hint:            "All criteria satisfied",
		ReviewedAt:      "2026-06-20T10:00:00Z",
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "status", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "pass") {
		t.Errorf("output missing verdict:\n%s", got)
	}
}

func TestDeliveryStatusHumanDetailUsesCriterionCheckboxes(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		ChangeRequestID: "cr-1",
		Found:           true,
		Verdict:         "needs_human_review",
		Hint:            "2 met, 1 unclear after merged delivery",
		ReviewedAt:      "2026-06-20T10:00:00Z",
		PerCriterion: []client.CriterionReview{
			{CriterionID: "demo-ac-1", Verdict: "met", Why: "Verified in merged implementation."},
			{CriterionID: "demo-ac-2", Verdict: "unclear", Why: "Needs human review."},
		},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "delivery", "status", "CR-101", "--detail")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Delivery Review",
		"needs_human_review",
		"Criteria:",
		"█",
		"☑",
		"☐",
		"demo-ac-1",
		"Needs human review.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

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
	deps, _, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "status", "CR-101")
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
	f := writeTempJSON(t, map[string]any{
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
	for _, want := range []string{"1/4", "2/4", "3/4", "4/4", "pass", "ac-1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestDeliverySubmitJSONEnvelopeHasAllSections(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	f := writeTempJSON(t, map[string]any{
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
	f := writeTempJSON(t, map[string]any{
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

// --- delivery report --init ---

func TestDeliveryReportInitScaffoldsCompletionTemplate(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.acceptanceCriteria = []client.AcceptanceCriterion{
		{ID: "ac-1", Text: "Cannot over-redeem"},
		{ID: "ac-2", Text: "Audit log exists"},
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
		EventType     string   `json:"event_type"`
		Summary       string   `json:"summary"`
		AffectedFiles []string `json:"affected_files"`
		Checks        []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"checks"`
		Criteria []struct {
			CriterionID string            `json:"criterion_id"`
			Text        string            `json:"text"`
			Claim       string            `json:"claim"`
			Evidence    map[string]string `json:"evidence"`
		} `json:"criteria"`
	}
	if err := json.Unmarshal(data, &tpl); err != nil {
		t.Fatalf("template is not valid JSON: %v\n%s", err, data)
	}
	if tpl.EventType != "coding_agent.completed" {
		t.Fatalf("event_type = %q", tpl.EventType)
	}
	if len(tpl.Criteria) != 2 || tpl.Criteria[0].CriterionID != "ac-1" ||
		tpl.Criteria[0].Text != "Cannot over-redeem" || tpl.Criteria[0].Claim != "satisfied" {
		t.Fatalf("criteria = %+v", tpl.Criteria)
	}
	if tpl.Criteria[0].Evidence == nil {
		t.Fatalf("criteria[0].evidence missing:\n%s", data)
	}
	if len(tpl.AffectedFiles) != 1 || len(tpl.Checks) != 1 {
		t.Fatalf("expected one empty example entry in affected_files and checks:\n%s", data)
	}
	if !strings.Contains(out.String(), path) {
		t.Fatalf("output should mention where the template was written:\n%s", out.String())
	}
	// No feedback event must be posted in --init mode.
	if fc.lastFeedbackBody != nil {
		t.Fatalf("ReportFeedback was called in --init mode")
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
}

func TestDeliveryReportInjectsRequiredEnvelopeFields(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	dir := t.TempDir()
	path := dir + "/completion.json"
	// Scaffold-shaped body: no change_request_id or severity, like --init emits.
	if err := os.WriteFile(path, []byte(`{"event_type":"coding_agent.completed","summary":"done","criteria":[]}`), 0o644); err != nil {
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
	if err := os.WriteFile(path, []byte(`{"event_type":"coding_agent.completed","summary":"done","criteria":[]}`), 0o644); err != nil {
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

func TestDeliveryReportRejectsNullBodyFile(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	dir := t.TempDir()
	path := dir + "/completion.json"
	if err := os.WriteFile(path, []byte("null"), 0o644); err != nil {
		t.Fatal(err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "report", "CR-1", "--file", path)
	if code == output.ExitOK {
		t.Fatalf("null body accepted: %s", out.String())
	}
	if !strings.Contains(out.String(), "JSON object") {
		t.Fatalf("error should explain the expected shape: %s", out.String())
	}
}
