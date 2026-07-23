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

func TestLocalDeliveryReportFromFileReturnsActionableError(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)
	deps.Client = nil
	f := writeDeliveryJSON(t, map[string]any{
		"event_type":     "coding_agent.completed",
		"summary":        "done",
		"context_digest": work.ContextDigest,
		"agent":          map[string]any{"name": "Codex"},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "report", work.Key, "--file", f)
	if code != output.ExitIncompatible {
		t.Fatalf("exit = %d, want incompatible; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "specgate change submit") || !strings.Contains(out.String(), "--file") {
		t.Fatalf("error does not route Local completion to change submit: %s", out.String())
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
