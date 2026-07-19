package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func writeDeliveryJSON(t *testing.T, data any) string {
	t.Helper()
	if body, ok := data.(map[string]any); ok && body["event_type"] == "coding_agent.completed" {
		if _, exists := body["agent"]; !exists {
			body["agent"] = map[string]any{"name": "builder"}
		}
	}
	return writeTempJSON(t, data)
}

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
	command.ExecuteForCode(command.NewRootCommand(deps), "delivery", "review", "CR-101") //nolint:errcheck
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

func TestDeliveryApprovePostsHumanDecisionWithCurrentActor(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	if err := (config.Config{CurrentUser: config.CurrentUser{Username: "lead"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--yes", "delivery", "approve", "CR-101", "--note", "false failure reviewed")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastDeliveryDecisionID != "cr-1" {
		t.Fatalf("decision id = %q, want cr-1", fc.lastDeliveryDecisionID)
	}
	if fc.lastDeliveryDecision.Decision != "approve" ||
		fc.lastDeliveryDecision.Actor != "lead" ||
		fc.lastDeliveryDecision.Note != "false failure reviewed" {
		t.Fatalf("decision body = %+v", fc.lastDeliveryDecision)
	}
}

func TestDeliveryRejectPostsHumanDecision(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	if err := (config.Config{CurrentUser: config.CurrentUser{Username: "reviewer"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "reject", "CR-101", "--note", "criterion two is still missing")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastDeliveryDecision.Decision != "reject" ||
		fc.lastDeliveryDecision.Actor != "reviewer" ||
		fc.lastDeliveryDecision.Note != "criterion two is still missing" {
		t.Fatalf("decision body = %+v", fc.lastDeliveryDecision)
	}
}

// --- delivery report ---

func TestDeliveryReportInteractiveIncludesAgentName(t *testing.T) {
	t.Parallel()
	deps, fc, prompter, out := newFakeDeps(t)
	prompter.inputValues = []string{"Codex", "Implemented and verified."}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "delivery", "report", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if got := completionAgentNameForTest(fc.lastFeedbackBody); got != "Codex" {
		t.Fatalf("agent.name = %q, want Codex; body = %#v", got, fc.lastFeedbackBody)
	}
	if len(prompter.inputTitles) != 2 || prompter.inputTitles[0] != "Coding agent name" || prompter.inputTitles[1] != "Feedback summary" {
		t.Fatalf("input prompts = %#v", prompter.inputTitles)
	}
}

func completionAgentNameForTest(body map[string]any) string {
	agent, _ := body["agent"].(map[string]any)
	name, _ := agent["name"].(string)
	return name
}

func TestDeliveryPeerReviewInitBindsLatestCompletion(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.acceptanceCriteria = []client.AcceptanceCriterion{{ID: "ac-1", Text: "Works"}}
	fc.feedbackEvents = []client.GovernanceFeedbackEvent{
		{
			ID: "feedback-a", EventType: "coding_agent.completed", CreatedAt: "2026-07-12T00:00:00Z",
			PayloadJSON: `{"agent":{"name":"builder-a"},"git_receipt":{"repository":"specgate","head_revision":"aaa"}}`,
		},
		{
			ID: "feedback-z", EventType: "coding_agent.completed", CreatedAt: "2026-07-12T00:00:00Z",
			PayloadJSON: `{"agent":{"name":"builder-z"},"git_receipt":{"repository":"specgate","head_revision":"zzz"}}`,
		},
	}
	path := filepath.Join(t.TempDir(), "peer.json")
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "peer-review", "CR-101", "--init="+path)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if strings.Contains(out.String(), `"schema_version"`) {
		t.Fatalf("plain peer-review init emitted a JSON envelope:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Wrote "+path) {
		t.Fatalf("plain peer-review init should name its scaffold:\n%s", out.String())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"completion_feedback_event_id": "feedback-z"`) {
		t.Fatalf("template did not bind completion: %s", body)
	}
	var scaffold struct {
		Criteria []struct {
			Evidence *struct {
				Kind string `json:"kind"`
				Path string `json:"path"`
			} `json:"evidence"`
		} `json:"criteria"`
	}
	if err := json.Unmarshal(body, &scaffold); err != nil {
		t.Fatal(err)
	}
	if len(scaffold.Criteria) != 1 || scaffold.Criteria[0].Evidence == nil ||
		scaffold.Criteria[0].Evidence.Kind != "" || scaffold.Criteria[0].Evidence.Path != "" {
		t.Fatalf("peer-review scaffold must include editable evidence {kind,path}: %s", body)
	}
}

func TestDeliveryPeerReviewPreservesBoundCompletionReceipt(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	dir := t.TempDir()
	deps.WorkingDir = dir
	deps.DeployRunner = deliveryGitRunner(dir, nil)
	body := map[string]any{
		"event_type": "coding_agent.peer_reviewed",
		"summary":    "Reviewed the implementation.",
		"agent":      map[string]any{"name": "reviewer"},
		"peer_review_of": map[string]any{
			"completion_feedback_event_id": "feedback-1",
			"git_receipt":                  map[string]any{"repository": "stale", "head_revision": "old"},
		},
		"criteria": []map[string]any{{
			"criterion_id": "ac-1", "claim": "satisfied", "evidence": map[string]any{"kind": "file", "path": "implementation.go"},
		}},
	}
	path := writeDeliveryJSON(t, body)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "peer-review", "CR-101", "--file", path)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	binding, _ := fc.lastFeedbackBody["peer_review_of"].(map[string]any)
	receipt, _ := binding["git_receipt"].(map[string]any)
	if receipt["repository"] != "stale" || receipt["head_revision"] != "old" {
		t.Fatalf("peer review receipt = %#v", receipt)
	}
}

// TestDeliveryReportBackfillsCheckNameFromCommand: a check authored with only
// a command must not arrive nameless — report has no --run-checks, so the
// backfill has to happen in submit-normalization, not check execution.
func TestDeliveryReportBackfillsCheckNameFromCommand(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"checks": []map[string]any{
			{"command": "go test ./...", "status": "pass"},
		},
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "report", "CR-101", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	checks, _ := fc.lastFeedbackBody["checks"].([]any)
	if len(checks) != 1 {
		t.Fatalf("checks = %#v", fc.lastFeedbackBody["checks"])
	}
	c0 := checks[0].(map[string]any)
	if c0["name"] != "go test ./..." {
		t.Fatalf("name not backfilled from command: %#v", c0)
	}
	if c0["command"] != "go test ./..." {
		t.Fatalf("command not retained for independent review: %#v", c0)
	}
}

func TestDeliveryReportFromFile(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	body := map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "All tests pass",
		"checks": []map[string]any{
			{"name": "tests", "command": "go test ./...", "status": "pass"},
		},
	}
	f := writeDeliveryJSON(t, body)
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
	checks, _ := fc.lastFeedbackBody["checks"].([]any)
	if len(checks) != 1 {
		t.Fatalf("checks = %#v", fc.lastFeedbackBody["checks"])
	}
	check := checks[0].(map[string]any)
	if check["command"] != "go test ./..." {
		t.Fatalf("command not retained for independent review: %#v", check)
	}
	if check["name"] != "tests" || check["status"] != "pass" {
		t.Fatalf("submitted check = %#v", check)
	}
}

func deliveryGitOutput(dir string, args ...string) string {
	return strings.Join(append([]string{"git", "-C", dir}, args...), " ")
}

func deliveryGitRunner(dir string, status []byte) *fakeDeployRunner {
	return &fakeDeployRunner{OutputByCommand: map[string][]byte{
		deliveryGitOutput(dir, "rev-parse", "--show-toplevel"):                            []byte(dir + "\n"),
		deliveryGitOutput(dir, "remote", "get-url", "origin"):                             []byte("https://github.com/acme/project.git\n"),
		deliveryGitOutput(dir, "branch", "--show-current"):                                []byte("main\n"),
		deliveryGitOutput(dir, "rev-parse", "HEAD"):                                       []byte("head-revision\n"),
		deliveryGitOutput(dir, "merge-base", "HEAD", "origin/main"):                       []byte("base-revision\n"),
		deliveryGitOutput(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all"): status,
	}}
}

func TestDeliveryReportInitIncludesGitReceipt(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	dir := t.TempDir()
	deps.WorkingDir = dir
	deps.DeployRunner = deliveryGitRunner(dir, nil)
	fc.acceptanceCriteria = []client.AcceptanceCriterion{{ID: "ac-1", Text: "AC"}}
	path := filepath.Join(t.TempDir(), "completion.json")

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "report", "CR-101", "--init", path)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var scaffold struct {
		GitReceipt map[string]any `json:"git_receipt"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &scaffold); err != nil {
		t.Fatalf("unmarshal scaffold: %v", err)
	}
	if scaffold.GitReceipt["repository"] != "https://github.com/acme/project.git" || scaffold.GitReceipt["availability"] != "available" {
		t.Fatalf("scaffold git_receipt = %#v", scaffold.GitReceipt)
	}
}

func TestDeliveryReportGitReceiptRefreshesStale(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	dir := t.TempDir()
	deps.WorkingDir = dir
	deps.DeployRunner = deliveryGitRunner(dir, nil)
	path := writeDeliveryJSON(t, map[string]any{
		"event_type":  "coding_agent.completed",
		"summary":     "done",
		"git_receipt": map[string]any{"repository": "stale", "head_revision": "old"},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "report", "CR-101", "--file", path)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	receipt, ok := fc.lastFeedbackBody["git_receipt"].(map[string]any)
	if !ok {
		t.Fatalf("git_receipt = %#v", fc.lastFeedbackBody["git_receipt"])
	}
	if receipt["repository"] != "https://github.com/acme/project.git" || receipt["head_revision"] != "head-revision" {
		t.Fatalf("stale receipt was not refreshed: %#v", receipt)
	}
}

func TestDeliverySubmitGitReceiptRefreshesStale(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	dir := t.TempDir()
	deps.WorkingDir = dir
	deps.DeployRunner = deliveryGitRunner(dir, nil)
	path := writeDeliveryJSON(t, map[string]any{
		"event_type":  "coding_agent.completed",
		"summary":     "done",
		"git_receipt": map[string]any{"repository": "stale", "head_revision": "old"},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "submit", "CR-101", "--file", path)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	receipt, ok := fc.lastFeedbackBody["git_receipt"].(map[string]any)
	if !ok {
		t.Fatalf("git_receipt = %#v", fc.lastFeedbackBody["git_receipt"])
	}
	if receipt["repository"] != "https://github.com/acme/project.git" || receipt["head_revision"] != "head-revision" {
		t.Fatalf("stale receipt was not refreshed: %#v", receipt)
	}
}

func TestDeliverySubmitGitReceiptMismatchWarnsHuman(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	dir := t.TempDir()
	deps.WorkingDir = dir
	deps.DeployRunner = deliveryGitRunner(dir, []byte(" M actual.go\x00"))
	for _, name := range []string{"actual.go", "reported.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package receipt\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := writeDeliveryJSON(t, map[string]any{
		"event_type":     "coding_agent.completed",
		"summary":        "done",
		"affected_files": []string{filepath.Join(dir, "reported.go")},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "submit", "CR-101", "--file", path)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "Git delivery scope differs") {
		t.Fatalf("output missing mismatch warning:\n%s", out.String())
	}
	receipt, ok := fc.lastFeedbackBody["git_receipt"].(map[string]any)
	if !ok || receipt["availability"] != "available" {
		t.Fatalf("submitted git_receipt = %#v", fc.lastFeedbackBody["git_receipt"])
	}
}

func TestDeliverySubmitGitReceiptAbsoluteAffectedPathUsesRepositoryRoot(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	root := t.TempDir()
	dir := filepath.Join(root, "sub")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "actual.go"), []byte("package receipt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps.WorkingDir = dir
	deps.DeployRunner = &fakeDeployRunner{OutputByCommand: map[string][]byte{
		deliveryGitOutput(dir, "rev-parse", "--show-toplevel"):                            []byte(root + "\n"),
		deliveryGitOutput(dir, "remote", "get-url", "origin"):                             []byte("https://github.com/acme/project.git\n"),
		deliveryGitOutput(dir, "branch", "--show-current"):                                []byte("main\n"),
		deliveryGitOutput(dir, "rev-parse", "HEAD"):                                       []byte("head-revision\n"),
		deliveryGitOutput(dir, "merge-base", "HEAD", "origin/main"):                       []byte("base-revision\n"),
		deliveryGitOutput(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all"): []byte(" M sub/actual.go\x00"),
	}}
	path := writeDeliveryJSON(t, map[string]any{
		"event_type":     "coding_agent.completed",
		"summary":        "done",
		"affected_files": []string{filepath.Join(root, "sub", "actual.go")},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "submit", "CR-101", "--file", path)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if strings.Contains(out.String(), "Git reported files do not match") {
		t.Fatalf("absolute repo-root path incorrectly mismatched:\n%s", out.String())
	}
}

func TestDeliverySubmitGitReceiptJSONIsSingleEnvelope(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	dir := t.TempDir()
	deps.WorkingDir = dir
	deps.DeployRunner = deliveryGitRunner(dir, nil)
	path := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", "CR-101", "--file", path)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var envelope struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("expected one JSON envelope, got %q: %v", out.String(), err)
	}
	if !envelope.OK {
		t.Fatalf("ok = false: %s", out.String())
	}
	if receipt, ok := fc.lastFeedbackBody["git_receipt"].(map[string]any); !ok || receipt["availability"] != "available" {
		t.Fatalf("submitted git_receipt = %#v", fc.lastFeedbackBody["git_receipt"])
	}
}

func TestDeliverySubmitReturnsGovernanceFailureForNonPassingVerdict(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		ChangeRequestID: "cr-1",
		Found:           true,
		Verdict:         "fail",
		Hint:            "tests failed",
	}
	path := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", "CR-101", "--file", path)
	if code != output.ExitGovernanceFailed {
		t.Fatalf("exit = %d, want governance failure; output = %s", code, out.String())
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Status client.DeliveryStatusResult `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("expected one JSON result envelope, got %q: %v", out.String(), err)
	}
	if !envelope.OK || envelope.Data.Status.Verdict != "fail" {
		t.Fatalf("result envelope lost the authoritative verdict: %s", out.String())
	}
}

func TestDeliverySubmitNonGitKeepsReceiptUnavailableAndContinues(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.WorkingDir = t.TempDir()
	deps.DeployRunner = &fakeDeployRunner{Err: errors.New("fatal: not a git repository")}
	path := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "submit", "CR-101", "--file", path)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	receipt, ok := fc.lastFeedbackBody["git_receipt"].(map[string]any)
	if !ok {
		t.Fatalf("git_receipt missing: %#v", fc.lastFeedbackBody)
	}
	if receipt["availability"] != "unavailable" || receipt["branch"] != "" || receipt["head_revision"] != "" || receipt["diff_digest"] != "" {
		t.Fatalf("non-git receipt fabricated identity: %#v", receipt)
	}
	if !strings.Contains(out.String(), "Git receipt unavailable") {
		t.Fatalf("output missing receipt-unavailable warning:\n%s", out.String())
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
		JudgeModel:      "google_genai/gemini-3.1-flash-lite",
		EvalSuite:       "delivery-review-v1",
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "status", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Ready for human review") {
		t.Errorf("output missing verdict:\n%s", got)
	}
	if !strings.Contains(got, "google_genai/gemini-3.1-flash-lite") {
		t.Errorf("output missing judge model:\n%s", got)
	}
}

func TestDeliveryStatusSeparatesEvidenceAssuranceDecisionAndReceipt(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		ChangeRequestID: "cr-1",
		Found:           true,
		Verdict:         "pass",
		JudgeModel:      "agent_attested",
		Executor:        "platform",
		GitReceipt: &client.GitReceipt{
			HeadRevision: "abc123def456",
		},
		PeerReview: client.PeerReviewState{State: "stale"},
		PerCriterion: []client.CriterionReview{{
			CriterionID: "ac-1",
			Verdict:     "met",
			TrustTier:   "grounded",
		}},
	}

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--plain", "delivery", "status", "CR-101", "--detail",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"Evidence: Ready for human review",
		"Assurance: Agent-reported; local citation captured",
		"Decision: Awaiting human acceptance",
		"Receipt: commit abc123def456",
		"Peer review: stale",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Run `specgate model set`") {
		t.Fatalf("output implies model review verifies code:\n%s", got)
	}
}

func TestDeliveryStatusIncludesRepositoryObservationInAssurance(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	var status client.DeliveryStatusResult
	if err := json.Unmarshal([]byte(`{
		"change_request_id":"cr-1",
		"found":true,
		"verdict":"pass",
		"executor":"platform",
		"assurance_sources":["repository_observed"]
	}`), &status); err != nil {
		t.Fatal(err)
	}
	fc.deliveryStatusResult = &status

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--plain", "delivery", "status", "CR-101",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if want := "Assurance: Agent-reported; Submitted commit observed on merged PR/MR"; !strings.Contains(out.String(), want) {
		t.Fatalf("output missing %q:\n%s", want, out.String())
	}
}

func TestDeliveryStatusKeepsPassingEvidenceWhenHumanRejects(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		ChangeRequestID: "cr-1",
		Found:           true,
		Verdict:         "fail",
		EvidenceVerdict: "pass",
		Executor:        "human",
	}

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--plain", "delivery", "status", "CR-101",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	for _, want := range []string{
		"Evidence: Ready for human review",
		"Decision: Rejected",
		"Stored verdict: Evidence gaps found",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestDeliveryStatusNamesUnavailablePolicy(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		ChangeRequestID: "cr-1",
		Found:           true,
		Verdict:         "needs_human_review",
		ReasonCode:      "policy_unavailable",
	}

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--plain", "delivery", "status", "CR-101",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "Evidence: Policy unavailable") {
		t.Fatalf("output does not name policy blocker:\n%s", out.String())
	}
}

// TestDeliveryStatusNotesChecksAreSelfSelected: delivery status output carries a
// runtime belt-and-suspenders note that the reported checks are agent-selected,
// so a human reviewer does not over-trust a narrow check set.
func TestDeliveryStatusNotesChecksAreSelfSelected(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		ChangeRequestID: "cr-1",
		Found:           true,
		Verdict:         "pass",
		JudgeModel:      "google_genai/gemini-3.1-flash-lite",
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "status", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "self-selected by the coding agent") {
		t.Fatalf("status output missing self-selected checks note:\n%s", out.String())
	}
}

func TestDeliveryStatusHumanDetailUsesCriterionCheckboxes(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	deps, fc, _, out := newFakeDeps(t)
	deps.StdoutIsTTY = func() bool { return true }
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
		"Independent confirmation required",
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

	out.Reset()
	deps.StdoutIsTTY = func() bool { return false }
	code = command.ExecuteForCode(command.NewRootCommand(deps), "delivery", "status", "CR-101", "--detail")
	if code != output.ExitOK {
		t.Fatalf("portable exit = %d, output = %s", code, out.String())
	}
	if strings.Contains(out.String(), "\x1b[") || strings.ContainsAny(out.String(), "█☑☐") {
		t.Fatalf("portable delivery status contains rich terminal output: %q", out.String())
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
	if len(tpl.Criteria) != 2 || tpl.Criteria[0].CriterionID != "ac-1" ||
		tpl.Criteria[0].Text != "Cannot over-redeem" || tpl.Criteria[0].Claim != "not_done" {
		t.Fatalf("criteria = %+v", tpl.Criteria)
	}
	// The declared check-binding is scaffolded so the agent keeps it in the report
	//; an unbound criterion omits it.
	if tpl.Criteria[0].VerificationBinding != "integration" {
		t.Fatalf("criteria[0].verification_binding = %q, want integration", tpl.Criteria[0].VerificationBinding)
	}
	if tpl.Criteria[1].VerificationBinding != "" {
		t.Fatalf("criteria[1].verification_binding = %q, want empty", tpl.Criteria[1].VerificationBinding)
	}
	if tpl.Criteria[0].Evidence == nil {
		t.Fatalf("criteria[0].evidence missing:\n%s", data)
	}
	if len(tpl.AffectedFiles) != 0 || len(tpl.Checks) != 1 {
		t.Fatalf("expected empty affected_files and one check entry:\n%s", data)
	}
	if tpl.Checks[0].Name != "integration" || tpl.Checks[0].Command != "" || tpl.Checks[0].Status != "skipped" {
		t.Fatalf("check example = %+v, want integration with blank command and skipped status", tpl.Checks[0])
	}
	if !strings.Contains(out.String(), path) {
		t.Fatalf("output should mention where the template was written:\n%s", out.String())
	}
	// No feedback event must be posted in --init mode.
	if fc.lastFeedbackBody != nil {
		t.Fatalf("ReportFeedback was called in --init mode")
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

func TestDeliverySubmitSkipEvidenceCheckFlag(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	missing := filepath.Join(t.TempDir(), "gone.go")
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
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", "CR-101", "--file", f, "--skip-evidence-check")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.calls != 4 {
		t.Fatalf("calls = %d, want 4", fc.calls)
	}
}

func TestDeliverySubmitWarnsOnMissingAffectedFiles(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.deliveryStatusResult = &client.DeliveryStatusResult{ChangeRequestID: "cr-1", Found: true, Verdict: "pass"}
	missing := filepath.Join(t.TempDir(), "deleted_file.go")
	f := writeDeliveryJSON(t, map[string]any{
		"event_type":     "coding_agent.completed",
		"summary":        "done",
		"affected_files": []string{missing},
		"criteria":       []map[string]any{},
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "submit", "CR-101", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d (affected_files may legitimately include deleted files), output = %s", code, out.String())
	}
	if fc.calls != 4 {
		t.Fatalf("calls = %d, want 4", fc.calls)
	}
	got := out.String()
	if !strings.Contains(got, "deleted_file.go") || !strings.Contains(got, "affected_files") {
		t.Fatalf("expected a warning naming the missing affected file:\n%s", got)
	}
}

func TestDeliveryReportRejectsMissingEvidencePaths(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	missing := filepath.Join(t.TempDir(), "phantom_test.go")
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"criteria": []map[string]any{
			{
				"criterion_id": "ac-0",
				"claim":        "satisfied",
				"evidence":     map[string]any{"kind": "test", "path": missing},
			},
		},
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "report", "CR-101", "--file", f)
	if code == output.ExitOK {
		t.Fatalf("expected non-zero exit for missing evidence path; output = %s", out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("calls = %d, want 0 (must fail before any network call)", fc.calls)
	}
	if !strings.Contains(out.String(), "phantom_test.go") {
		t.Fatalf("error should name the missing path:\n%s", out.String())
	}
}

// --- executed checks (--run-checks) ---

func TestDeliverySubmitRunChecksRequiresYesOutsideInteractiveTerminal(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	executed := false
	deps.RunCheckCommand = func(_ context.Context, _ string) (int, string) {
		executed = true
		return 0, ""
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"checks":     []map[string]any{{"name": "tests", "command": "go test ./...", "status": "pass"}},
		"criteria":   []map[string]any{},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "submit", "CR-101", "--file", f, "--run-checks")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if executed || fc.lastFeedbackBody != nil {
		t.Fatal("check or submission ran without explicit --yes")
	}
	if !strings.Contains(out.String(), "--yes") {
		t.Fatalf("error must explain automation confirmation: %s", out.String())
	}
}

func TestDeliverySubmitRunChecksShowsCommandsBeforeInteractiveConfirmation(t *testing.T) {
	deps, _, fp, out := newFakeDeps(t)
	deps.StdinIsTTY = func() bool { return true }
	fp.confirmValue = false
	var stderr bytes.Buffer
	deps.Stderr = &stderr
	executed := false
	deps.RunCheckCommand = func(_ context.Context, _ string) (int, string) {
		executed = true
		return 0, ""
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"checks":     []map[string]any{{"name": "tests", "command": "go test ./...", "status": "pass"}},
		"criteria":   []map[string]any{},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "delivery", "submit", "CR-101", "--file", f, "--run-checks")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if executed {
		t.Fatal("check ran after user declined confirmation")
	}
	if !strings.Contains(stderr.String(), "go test ./...") || !strings.Contains(fp.confirmTitle, "Run 1") {
		t.Fatalf("command preview/confirmation missing: stderr=%q confirm=%q", stderr.String(), fp.confirmTitle)
	}
}

func TestDeliverySubmitRunChecksReplacesClaimedStatuses(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	var stderr bytes.Buffer
	deps.Stderr = &stderr
	deps.RunCheckCommand = func(_ context.Context, cmd string) (int, string) {
		if strings.Contains(cmd, "lint") {
			return 0, "lint clean"
		}
		return 1, "--- FAIL: TestX (0.01s)"
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"checks": []map[string]any{
			{"name": "go test ./...", "command": "go test ./...", "status": "pass"},
			{"name": "make lint", "status": "fail"},
			{"name": "manual smoke", "status": "skipped", "detail": "no device available"},
		},
		"criteria": []map[string]any{},
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--yes", "delivery", "submit", "CR-101", "--file", f, "--run-checks")
	if code != output.ExitOK {
		t.Fatalf("exit = %d (honest statuses are submitted, review decides), output = %s", code, out.String())
	}
	checks, _ := fc.lastFeedbackBody["checks"].([]any)
	if len(checks) != 3 {
		t.Fatalf("checks = %#v", fc.lastFeedbackBody["checks"])
	}
	c0 := checks[0].(map[string]any)
	if c0["status"] != "fail" || !strings.Contains(c0["detail"].(string), "exit 1") {
		t.Fatalf("claimed pass must be corrected to observed fail: %#v", c0)
	}
	c1 := checks[1].(map[string]any)
	if c1["status"] != "fail" {
		t.Fatalf("check without explicit command must be untouched: %#v", c1)
	}
	c2 := checks[2].(map[string]any)
	if c2["status"] != "skipped" || c2["detail"] != "no device available" {
		t.Fatalf("skipped check must be untouched: %#v", c2)
	}
	if strings.Contains(out.String(), "Executed check") || strings.Contains(out.String(), "1/4") {
		t.Fatalf("stdout contains progress output:\n%s", out.String())
	}
	if !strings.Contains(stderr.String(), "go test ./...") || !strings.Contains(stderr.String(), "1/4") {
		t.Fatalf("stderr should report the corrected check:\n%s", stderr.String())
	}
}

func TestDeliverySubmitRunChecksUsesCommandFieldAsExecutable(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	var executed []string
	deps.RunCheckCommand = func(_ context.Context, cmd string) (int, string) {
		executed = append(executed, cmd)
		return 0, "OK"
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"checks": []map[string]any{
			{"name": "tests", "command": "python3 -m unittest", "status": "pass"},
		},
		"criteria": []map[string]any{},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--yes", "delivery", "submit", "CR-101", "--file", f, "--run-checks")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if len(executed) != 1 || executed[0] != "python3 -m unittest" {
		t.Fatalf("executed = %#v, want python3 -m unittest", executed)
	}
	checks, _ := fc.lastFeedbackBody["checks"].([]any)
	c0 := checks[0].(map[string]any)
	if c0["name"] != "tests" || c0["status"] != "pass" {
		t.Fatalf("submitted check = %#v", c0)
	}
	if c0["command"] != "python3 -m unittest" {
		t.Fatalf("command not retained for independent review: %#v", c0)
	}
	if !strings.Contains(c0["detail"].(string), "exit 0") {
		t.Fatalf("detail missing observed execution: %#v", c0)
	}
}

func TestDeliverySubmitChecksNotExecutedWithoutFlag(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	executed := false
	deps.RunCheckCommand = func(_ context.Context, _ string) (int, string) {
		executed = true
		return 0, ""
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"checks":     []map[string]any{{"name": "go test ./...", "command": "go test ./...", "status": "pass"}},
		"criteria":   []map[string]any{},
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "submit", "CR-101", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if executed {
		t.Fatal("checks were executed without --run-checks")
	}
	checks, _ := fc.lastFeedbackBody["checks"].([]any)
	if checks[0].(map[string]any)["status"] != "pass" {
		t.Fatalf("claimed status must be untouched without --run-checks: %#v", checks[0])
	}
}

// TestDeliverySubmitRunChecksAfterRefResolution: an invalid work ref must fail
// before any check command executes — checks can take minutes.
func TestDeliverySubmitRunChecksAfterRefResolution(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolveErr = errors.New("work item not found")
	executed := false
	deps.RunCheckCommand = func(_ context.Context, _ string) (int, string) {
		executed = true
		return 0, ""
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"checks":     []map[string]any{{"name": "go test ./...", "command": "go test ./...", "status": "pass"}},
		"criteria":   []map[string]any{},
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--yes", "delivery", "submit", "BAD-REF", "--file", f, "--run-checks")
	if code == output.ExitOK {
		t.Fatalf("expected non-zero exit for unresolved ref; output = %s", out.String())
	}
	if executed {
		t.Fatal("check commands ran before the work ref resolved")
	}
}

// TestDeliverySubmitRunChecksTruncatesOutputRuneSafe: detail truncation must
// not split a multibyte character.
func TestDeliverySubmitRunChecksTruncatesOutputRuneSafe(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.RunCheckCommand = func(_ context.Context, _ string) (int, string) {
		return 0, strings.Repeat("é", 400)
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "done",
		"checks":     []map[string]any{{"name": "echo unicode", "command": "echo unicode", "status": "pass"}},
		"criteria":   []map[string]any{},
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "delivery", "submit", "CR-101", "--file", f, "--run-checks")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	checks, _ := fc.lastFeedbackBody["checks"].([]any)
	detail := checks[0].(map[string]any)["detail"].(string)
	if !utf8.ValidString(detail) {
		t.Fatalf("detail is not valid UTF-8 after truncation: %q", detail)
	}
	if len([]rune(detail)) > 220 {
		t.Fatalf("detail not truncated: %d runes", len([]rune(detail)))
	}
}

// --- agent-reported evidence visibility ---

func TestDeliveryStatusWarnsWhenAgentAttested(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		ChangeRequestID: "cr-1",
		Found:           true,
		Verdict:         "pass",
		JudgeModel:      "agent_attested",
		EvalSuite:       "delivery-review-v1",
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "status", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Agent-reported evidence") {
		t.Fatalf("plain output missing agent-reported evidence warning:\n%s", got)
	}
	if strings.Contains(got, "specgate model set") {
		t.Fatalf("warning must not imply model-backed review verifies the code:\n%s", got)
	}
}

func TestDeliveryStatusDashboardWarnsWhenAgentAttested(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	deps, fc, _, out := newFakeDeps(t)
	deps.StdoutIsTTY = func() bool { return true }
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		ChangeRequestID: "cr-1",
		Found:           true,
		Verdict:         "pass",
		JudgeModel:      "agent_attested",
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "delivery", "status", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "Agent-reported evidence") {
		t.Fatalf("dashboard output missing agent-reported evidence warning:\n%s", out.String())
	}
}

func TestDeliveryStatusNoAttestedWarningForModelJudge(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		ChangeRequestID: "cr-1",
		Found:           true,
		Verdict:         "pass",
		JudgeModel:      "governance-gate-judge",
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "status", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if strings.Contains(out.String(), "Agent-reported evidence") {
		t.Fatalf("model-judged verdict must not carry the agent-reported warning:\n%s", out.String())
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
