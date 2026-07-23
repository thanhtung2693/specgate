package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestChangeApproveApprovesThenPromotesWithOneDecision(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.artifactResult = &client.Artifact{ID: "artifact-1", Version: "v1", Status: "draft", SnapshotDigest: "sha256:approved"}
	fc.updateStatusResult = &client.Artifact{ID: "artifact-1", Version: "v1", Status: "approved"}
	fc.promoteResult = &client.Feature{ID: "feature-1", Key: "LOGIN", Version: 1, CanonicalArtifactID: "artifact-1"}
	fc.createWorkItemResult = map[string]any{
		"change_request_id": "cr-1", "change_request_key": "CR-1", "feature_key": "LOGIN",
		"lead_artifact_id": "artifact-1", "acceptance_criteria": []any{"Login succeeds"},
	}

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "--yes", "change", "approve", "artifact-1", "--note", "scope approved",
		"--title", "Implement login", "--ac", "Login succeeds",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if strings.Join(fc.callOrder, ",") != "artifact_status,artifact_promote,work_create,context_pack" {
		t.Fatalf("call order = %v", fc.callOrder)
	}
	if fc.lastStatusInput.Status != "approved" || fc.lastStatusInput.Note != "scope approved" {
		t.Fatalf("status input = %#v", fc.lastStatusInput)
	}
	if fc.lastPromoteID != "artifact-1" {
		t.Fatalf("promoted artifact = %q", fc.lastPromoteID)
	}
	if fc.lastCreateWorkItem["feature"] != "LOGIN" || fc.lastCreateWorkItem["title"] != "Implement login" {
		t.Fatalf("work create input = %#v", fc.lastCreateWorkItem)
	}
	var envelope struct {
		Command string `json:"command"`
		Data    struct {
			ArtifactID      string `json:"artifact_id"`
			ArtifactVersion string `json:"artifact_version"`
			SnapshotDigest  string `json:"snapshot_digest"`
			FeatureKey      string `json:"feature_key"`
			WorkRef         string `json:"work_ref"`
			ContextState    string `json:"context_state"`
			Next            string `json:"next_command"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Command != "change.approve" || envelope.Data.ArtifactID != "artifact-1" ||
		envelope.Data.ArtifactVersion != "v1" || envelope.Data.SnapshotDigest != "sha256:approved" ||
		envelope.Data.FeatureKey != "LOGIN" || envelope.Data.WorkRef != "CR-1" ||
		envelope.Data.ContextState != "assembled" || envelope.Data.Next != "specgate work context CR-1 --json" {
		t.Fatalf("result = %#v, output = %s", envelope, out.String())
	}
}

func TestChangeApproveRequiresExplicitWorkContractBeforeMutation(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "approve", "artifact-1")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if len(fc.callOrder) != 0 || fc.lastCreateWorkItem != nil {
		t.Fatalf("approval mutated state before a complete work contract: calls=%v create=%#v", fc.callOrder, fc.lastCreateWorkItem)
	}
	for _, want := range []string{"--title", "--ac"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing actionable flag %q: %s", want, out.String())
		}
	}
}

func TestChangeApproveRetryReusesExactArtifactWork(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.artifactResult = &client.Artifact{ID: "artifact-1", Version: "v1", Status: "approved", SnapshotDigest: "sha256:approved"}
	fc.promoteResult = &client.Feature{ID: "feature-1", Key: "LOGIN", Version: 1, CanonicalArtifactID: "artifact-1"}
	fc.workItems = []client.WorkItemSummary{{
		ID: "cr-1", Key: "CR-1", Title: "Implement login", IntentMD: "Bound scope", Phase: "ready", LeadArtifactID: "artifact-1",
	}}
	fc.acceptanceCriteria = []client.AcceptanceCriterion{{ID: "ac-1", Text: "Login succeeds", VerificationBinding: "integration"}}

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "--yes", "change", "approve", "artifact-1",
		"--title", "Implement login", "--description", "Bound scope", "--ac", " Login succeeds   @check:integration ",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastCreateWorkItem != nil {
		t.Fatalf("retry created duplicate work: %#v", fc.lastCreateWorkItem)
	}
	if fc.lastContextID != "cr-1" || !strings.Contains(out.String(), `"work_ref":"CR-1"`) {
		t.Fatalf("retry did not reuse and verify existing work: context=%q output=%s", fc.lastContextID, out.String())
	}
}

func TestChangeApproveRetryDoesNotPresentDeliveredWorkAsNewImplementation(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.artifactResult = &client.Artifact{ID: "artifact-1", Version: "v1", Status: "approved", SnapshotDigest: "sha256:approved"}
	fc.promoteResult = &client.Feature{ID: "feature-1", Key: "LOGIN", Version: 1, CanonicalArtifactID: "artifact-1"}
	fc.workItems = []client.WorkItemSummary{{
		ID: "cr-1", Key: "CR-1", Title: "Implement login", Phase: "delivered", LeadArtifactID: "artifact-1",
	}}
	fc.acceptanceCriteria = []client.AcceptanceCriterion{{ID: "ac-1", Text: "Login succeeds"}}

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "--yes", "change", "approve", "artifact-1",
		"--title", "Implement login", "--ac", "Login succeeds",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), `"state":"already_delivered"`) ||
		!strings.Contains(out.String(), `"next_command":"specgate change status CR-1"`) {
		t.Fatalf("delivered retry is not terminal-aware: %s", out.String())
	}
}

func TestChangeSubmitRunChecksMarksLocalAssuranceAsReproduced(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)
	deps.RunCheckCommand = func(_ context.Context, command string) (int, string) {
		if command != "go test ./..." {
			t.Fatalf("executed command = %q", command)
		}
		return 0, "ok"
	}
	f := writeDeliveryJSON(t, map[string]any{
		"event_type":     "coding_agent.completed",
		"summary":        "done",
		"context_digest": work.ContextDigest,
		"checks": []map[string]any{{
			"name": "tests", "command": "go test ./...", "status": "pass",
		}},
		"criteria": []map[string]any{{
			"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"summary": "verified"},
		}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "submit", work.Key, "--file", f, "--run-checks")
	if code != output.ExitOK {
		t.Fatalf("submit exit = %d, output = %s", code, out.String())
	}
	if fc.calls != 0 || !strings.Contains(out.String(), `"assurance":"Agent-reported; locally reproduced"`) {
		t.Fatalf("submit assurance did not expose local reproduction: calls=%d output=%s", fc.calls, out.String())
	}
	out.Reset()
	got := runChangeStatusJSON(t, deps, out, work.Key)
	if got.Assurance != "Agent-reported; locally reproduced" {
		t.Fatalf("stored assurance = %q", got.Assurance)
	}
}

type changeStatusData struct {
	Mode        string   `json:"mode"`
	Ref         string   `json:"ref"`
	Title       string   `json:"title"`
	State       string   `json:"state"`
	Evidence    string   `json:"evidence"`
	Assurance   string   `json:"assurance"`
	Decision    string   `json:"decision"`
	Receipt     string   `json:"receipt"`
	Freshness   string   `json:"freshness"`
	NextActor   string   `json:"next_actor"`
	Missing     []string `json:"missing"`
	Guidance    string   `json:"guidance"`
	Stale       bool     `json:"stale"`
	StaleReason string   `json:"stale_reason"`
	NextCommand string   `json:"next_command"`
}

type changeStatusEnvelope struct {
	SchemaVersion string           `json:"schema_version"`
	Command       string           `json:"command"`
	OK            bool             `json:"ok"`
	Data          changeStatusData `json:"data"`
}

func runChangeStatusJSON(t *testing.T, deps *command.Deps, out *bytes.Buffer, ref string) changeStatusData {
	t.Helper()
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "status", ref)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var envelope changeStatusEnvelope
	if err := json.NewDecoder(out).Decode(&envelope); err != nil {
		t.Fatalf("decode status output: %v", err)
	}
	if envelope.SchemaVersion != "specgate.cli/v1" || envelope.Command != "change.status" || !envelope.OK {
		t.Fatalf("unexpected success envelope: %#v", envelope)
	}
	return envelope.Data
}

func TestChangeStatusFullPassingAgentReviewAwaitsAcceptance(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolvedWork = &client.ResolvedWork{ChangeRequestID: "cr-1", ChangeRequestKey: "CR-101", Title: "Add change status"}
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		Found: true, Verdict: "pass", JudgeModel: "agent_attested", Executor: "platform",
	}

	got := runChangeStatusJSON(t, deps, out, "CR-101")
	if got.State != "awaiting_acceptance" || got.NextActor != "human_reviewer" {
		t.Fatalf("status = %#v", got)
	}
	if len(got.Missing) != 1 || got.Missing[0] != "Human acceptance" {
		t.Fatalf("missing = %#v", got.Missing)
	}
	if got.NextCommand != "specgate change accept CR-101" {
		t.Fatalf("next command = %q", got.NextCommand)
	}
	if !fc.lastDetailFlag || fc.lastGatesID != "cr-1" {
		t.Fatalf("delivery query detail=%t id=%q, want true and cr-1", fc.lastDetailFlag, fc.lastGatesID)
	}
}

func TestChangeStatusFullPolicyUnavailableIsBlocked(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolvedWork = &client.ResolvedWork{ChangeRequestID: "cr-1", ChangeRequestKey: "CR-101", Title: "Add change status"}
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		Found: true, Verdict: "needs_human_review", ReasonCode: "policy_unavailable",
	}

	got := runChangeStatusJSON(t, deps, out, "CR-101")
	if got.State != "blocked" || got.NextActor != "maintainer" {
		t.Fatalf("status = %#v", got)
	}
	if !strings.Contains(strings.ToLower(strings.Join(append(got.Missing, got.StaleReason), " ")), "policy unavailable") {
		t.Fatalf("policy blocker not named: %#v", got)
	}
}

func TestChangeStatusFullLatestCompletionAwaitingReview(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolvedWork = &client.ResolvedWork{
		ChangeRequestID: "cr-1", ChangeRequestKey: "CR-101", Title: "Corrected completion",
	}
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		Found: true, Verdict: "needs_human_review", ReasonCode: "delivery_review_outdated",
	}

	got := runChangeStatusJSON(t, deps, out, "CR-101")
	if got.State != "review_pending" ||
		got.NextActor != "implementing_agent" ||
		got.NextCommand != "specgate delivery review CR-101" {
		t.Fatalf("status = %#v, want actionable review_pending state", got)
	}
}

func TestChangeStatusFullHumanOverrideOfUnavailablePolicyIsAccepted(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolvedWork = &client.ResolvedWork{ChangeRequestID: "cr-1", ChangeRequestKey: "CR-101", Title: "Human policy override"}
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		Found: true, Verdict: "pass", ReasonCode: "policy_unavailable", Executor: "human",
	}

	got := runChangeStatusJSON(t, deps, out, "CR-101")
	if got.State != "accepted" || got.Decision != "Accepted" ||
		got.Evidence != "Policy unavailable" || got.NextActor != "none" {
		t.Fatalf("status = %#v, want accepted human authority with unavailable evidence disclosed", got)
	}
}

func TestChangeStatusFullHumanReviewRoutesToReviewer(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolvedWork = &client.ResolvedWork{ChangeRequestID: "cr-1", ChangeRequestKey: "CR-101", Title: "Review the delivered UI"}
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		Found: true, Verdict: "needs_human_review", Hint: "One criterion needs visual confirmation", Executor: "platform",
	}

	got := runChangeStatusJSON(t, deps, out, "CR-101")
	if got.State != "awaiting_review" || got.NextActor != "human_reviewer" {
		t.Fatalf("status = %#v", got)
	}
	if len(got.Missing) != 1 || got.Missing[0] != "One criterion needs visual confirmation" {
		t.Fatalf("missing = %#v", got.Missing)
	}
	if got.NextCommand != "specgate delivery status CR-101 --detail" {
		t.Fatalf("next command = %q", got.NextCommand)
	}
}

func TestChangeStatusFullHumanReviewDetailOffersDecisionCommands(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolvedWork = &client.ResolvedWork{ChangeRequestID: "cr-1", ChangeRequestKey: "CR-101", Title: "Review the delivered UI"}
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		ChangeRequestID: "cr-1",
		Found:           true,
		Verdict:         "needs_human_review",
		Hint:            "One criterion needs visual confirmation",
		Executor:        "platform",
	}

	status := runChangeStatusJSON(t, deps, out, "CR-101")
	if status.NextCommand != "specgate delivery status CR-101 --detail" {
		t.Fatalf("next command = %q", status.NextCommand)
	}
	out.Reset()
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "delivery", "status", "CR-101", "--detail")
	if code != output.ExitOK {
		t.Fatalf("delivery status exit = %d: %s", code, out.String())
	}
	for _, want := range []string{
		"specgate change accept CR-101",
		`specgate change request-changes CR-101 --note "<reason>"`,
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("delivery detail missing resolving command %q:\n%s", want, out.String())
		}
	}
}

func TestChangeStatusFullStalePeerReviewDoesNotClaimReceiptCurrent(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolvedWork = &client.ResolvedWork{ChangeRequestID: "cr-1", ChangeRequestKey: "CR-101", Title: "Add change status"}
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		Found: true, Verdict: "pass", GitReceipt: &client.GitReceipt{HeadRevision: "abc123def456"},
		PeerReview: client.PeerReviewState{State: "stale"},
	}

	got := runChangeStatusJSON(t, deps, out, "CR-101")
	if !got.Stale {
		t.Fatalf("stale = false: %#v", got)
	}
	if strings.Contains(strings.ToLower(got.Freshness), "is current") {
		t.Fatalf("freshness incorrectly claims current receipt: %q", got.Freshness)
	}
}

func TestChangeStatusFullPlainIncludesActionableTrustFields(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolvedWork = &client.ResolvedWork{ChangeRequestID: "cr-1", ChangeRequestKey: "CR-101", Title: "Add change status"}
	fc.deliveryStatusResult = &client.DeliveryStatusResult{Found: true, Verdict: "pass", GitReceipt: &client.GitReceipt{HeadRevision: "abc123def456"}}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "change", "status", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	for _, field := range []string{"State:", "Evidence:", "Assurance:", "Decision:", "Receipt:", "Freshness:", "Next actor:", "Missing:", "Stale:", "Next:"} {
		if !strings.Contains(out.String(), field) {
			t.Fatalf("output missing %q:\n%s", field, out.String())
		}
	}
}

func TestChangeStatusFullStoredReceiptWasNotCheckedAgainstCheckout(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolvedWork = &client.ResolvedWork{ChangeRequestID: "cr-1", ChangeRequestKey: "CR-101", Title: "Add change status"}
	fc.deliveryStatusResult = &client.DeliveryStatusResult{Found: true, Verdict: "pass", GitReceipt: &client.GitReceipt{HeadRevision: "abc123def456"}}

	got := runChangeStatusJSON(t, deps, out, "CR-101")
	if strings.Contains(strings.ToLower(got.Freshness), "is current") || !strings.Contains(strings.ToLower(got.Freshness), "not checked against the current checkout") {
		t.Fatalf("freshness = %q", got.Freshness)
	}
}

func TestChangeStatusFullBlankReceiptIsNotRecorded(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolvedWork = &client.ResolvedWork{ChangeRequestID: "cr-1", ChangeRequestKey: "CR-101", Title: "Add change status"}
	fc.deliveryStatusResult = &client.DeliveryStatusResult{Found: true, Verdict: "pass", GitReceipt: &client.GitReceipt{}}

	got := runChangeStatusJSON(t, deps, out, "CR-101")
	if got.Receipt != "No Git receipt recorded" || !strings.Contains(got.Freshness, "No stored receipt") {
		t.Fatalf("blank receipt = %#v", got)
	}
}

func TestChangeStatusFullReceiptSurfacesAvailabilityWarnings(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolvedWork = &client.ResolvedWork{
		ChangeRequestID: "cr-1", ChangeRequestKey: "CR-101", Title: "Warn on partial receipt",
	}
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		Found:   true,
		Verdict: "pass",
		GitReceipt: &client.GitReceipt{
			Availability: "unavailable",
			Warnings:     []string{"Git metadata could not be read"},
		},
	}

	got := runChangeStatusJSON(t, deps, out, "CR-101")
	if got.Receipt != "Git receipt unavailable; warning: Git metadata could not be read" ||
		!strings.Contains(got.Freshness, "No stored receipt") {
		t.Fatalf("status = %#v, want unavailable receipt kept out of freshness claim", got)
	}
}

func TestChangeStatusFullAcceptedHasExplicitTerminalNextStep(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolvedWork = &client.ResolvedWork{ChangeRequestID: "cr-1", ChangeRequestKey: "CR-101", Title: "Add change status"}
	fc.deliveryStatusResult = &client.DeliveryStatusResult{Found: true, Verdict: "pass", Executor: "human"}

	got := runChangeStatusJSON(t, deps, out, "CR-101")
	if got.State != "accepted" || got.NextActor != "none" || len(got.Missing) != 0 || got.NextCommand != "specgate audit CR-101" {
		t.Fatalf("accepted status = %#v", got)
	}
}

func TestChangeStatusFullHumanRejectionRequestsRework(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolvedWork = &client.ResolvedWork{ChangeRequestID: "cr-1", ChangeRequestKey: "CR-101", Title: "Add change status"}
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		Found: true, Verdict: "fail", Executor: "human",
		Note: "Restore the missing rollback test.",
	}

	got := runChangeStatusJSON(t, deps, out, "CR-101")
	if got.State != "rework_requested" || got.NextActor != "implementing_agent" ||
		got.NextCommand != "specgate delivery report CR-101 --init" ||
		len(got.Missing) != 1 || got.Missing[0] != "Revised completion addressing requested changes" ||
		got.Guidance != "Restore the missing rollback test." {
		t.Fatalf("rework status = %#v", got)
	}
}

func TestChangeStatusFullNeedsChangesReportsEvidenceGap(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolvedWork = &client.ResolvedWork{
		ChangeRequestID: "cr-1", ChangeRequestKey: "CR-101", Title: "Address evidence gaps",
	}
	fc.deliveryStatusResult = &client.DeliveryStatusResult{
		Found: true, Verdict: "needs_changes", Executor: "platform",
	}

	got := runChangeStatusJSON(t, deps, out, "CR-101")
	if got.State != "implementation" || got.Evidence != "Evidence gaps found" {
		t.Fatalf("status = %#v, want actionable evidence gap", got)
	}
}

func TestChangeStatusFullResolveFailureUsesWorkRefEnvelope(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolveErr = &client.APIError{Kind: client.ErrorNotFound, Status: 404, Message: "no match"}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "status", "CR-404")
	if code != output.ExitNotFound {
		t.Fatalf("exit = %d, want %d; output = %s", code, output.ExitNotFound, out.String())
	}
	var envelope struct {
		Command string `json:"command"`
		OK      bool   `json:"ok"`
		Error   struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Command != "change.status" || envelope.OK || envelope.Error.Code != "not_found" || !strings.Contains(envelope.Error.Message, "work item \"CR-404\" not found") {
		t.Fatalf("resolve error envelope = %#v", envelope)
	}
}

func TestChangeStatusLocalWithoutReportShowsImplementationScaffold(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)

	got := runChangeStatusJSON(t, deps, out, work.Key)
	if got.State != "implementation" || got.NextActor != "implementing_agent" {
		t.Fatalf("status = %#v", got)
	}
	want := "specgate delivery report " + work.Key + " --init"
	if got.NextCommand != want {
		t.Fatalf("next command = %q, want %q", got.NextCommand, want)
	}
	if fc.calls != 0 {
		t.Fatalf("Local status made %d client calls, want 0", fc.calls)
	}
}

func TestChangeStatusLocalPassingEvidenceAwaitsAcceptance(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "receipt-1")
	closeLocalChangeStore(t, deps, stateDir, store)

	got := runChangeStatusJSON(t, deps, out, work.Key)
	if got.State != "awaiting_acceptance" || got.NextActor != "human_reviewer" || got.Decision != "Awaiting human acceptance" {
		t.Fatalf("status = %#v", got)
	}
	if want := "specgate --yes change accept " + work.Key; got.NextCommand != want {
		t.Fatalf("next command = %q, want %q", got.NextCommand, want)
	}
}

func TestChangeStatusPlainLocalAwaitingAcceptanceShowsRunnableDecision(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "receipt-1")
	closeLocalChangeStore(t, deps, stateDir, store)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "change", "status", work.Key)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	want := "Next: specgate --yes change accept " + work.Key
	if !strings.Contains(out.String(), want) {
		t.Fatalf("output missing %q:\n%s", want, out.String())
	}
}

func TestChangeStatusLocalApprovedDeliveryIsAccepted(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "receipt-1")
	if err := store.DecideDelivery(t.Context(), selection.Workspace.ID, work.Key, "approve", "human", "accepted"); err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, stateDir, store)

	got := runChangeStatusJSON(t, deps, out, work.Key)
	if got.State != "accepted" || got.NextActor != "none" || len(got.Missing) != 0 || got.NextCommand != "specgate audit "+work.Key || got.Decision != "Accepted" {
		t.Fatalf("status = %#v", got)
	}
}

func TestChangeStatusLocalHumanCanAcceptEvidenceGapWithoutHidingIt(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	review, err := store.SubmitDelivery(t.Context(), selection.Workspace.ID, work.Key, map[string]any{
		"context_digest": work.ContextDigest,
		"agent":          map[string]any{"name": "builder"},
		"criteria": []any{map[string]any{
			"criterion_id": "local-1", "claim": "unsatisfied",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if review.Verdict != "failed" {
		t.Fatalf("review = %#v, want failed evidence", review)
	}
	if err := store.DecideDelivery(
		t.Context(),
		selection.Workspace.ID,
		work.Key,
		"approve",
		"human",
		"reviewed the false negative",
	); err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, stateDir, store)

	got := runChangeStatusJSON(t, deps, out, work.Key)
	if got.State != "accepted" ||
		got.Decision != "Accepted" ||
		got.Evidence != "Evidence gaps found" {
		t.Fatalf("status = %#v, want accepted decision with evidence gap disclosed", got)
	}
}

func TestChangeStatusLocalReceiptSurfacesAvailabilityWarnings(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	_, err := store.SubmitDelivery(t.Context(), selection.Workspace.ID, work.Key, map[string]any{
		"context_digest": work.ContextDigest,
		"agent":          map[string]any{"name": "builder"},
		"git_receipt": map[string]any{
			"availability": "unavailable",
			"warnings":     []any{"Git metadata could not be read"},
		},
		"criteria": []any{map[string]any{
			"criterion_id": "local-1",
			"claim":        "satisfied",
			"evidence":     map[string]any{"summary": "targeted test passed"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, stateDir, store)

	got := runChangeStatusJSON(t, deps, out, work.Key)
	if got.Receipt != "Git receipt unavailable; warning: Git metadata could not be read" ||
		!strings.Contains(got.Freshness, "No stored receipt") {
		t.Fatalf("status = %#v, want unavailable receipt kept out of freshness claim", got)
	}
}

func TestChangeStatusLocalHumanRejectionRequestsRework(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "receipt-1")
	if err := store.DecideDelivery(t.Context(), selection.Workspace.ID, work.Key, "reject", "human", "rework"); err != nil {
		t.Fatal(err)
	}
	closeLocalChangeStore(t, deps, stateDir, store)

	got := runChangeStatusJSON(t, deps, out, work.Key)
	if got.State != "rework_requested" || got.NextActor != "implementing_agent" ||
		got.NextCommand != "specgate delivery report "+work.Key+" --init" ||
		len(got.Missing) != 1 || got.Missing[0] != "Revised completion addressing requested changes" ||
		got.Guidance != "rework" {
		t.Fatalf("rework status = %#v", got)
	}
}

func TestChangeStatusPlainAcceptedShowsTerminalFields(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolvedWork = &client.ResolvedWork{ChangeRequestID: "cr-1", ChangeRequestKey: "CR-101", Title: "Add change status"}
	fc.deliveryStatusResult = &client.DeliveryStatusResult{Found: true, Verdict: "pass", Executor: "human"}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "change", "status", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	for _, want := range []string{"Next actor: none", "Missing: none", "Next: specgate audit CR-101"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestChangeStatusLocalStalePeerEvidenceIsSeparateFromAuthority(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "receipt-1")
	if _, err := store.PeerReviewDelivery(t.Context(), selection.Workspace.ID, work.Key, map[string]any{
		"agent": map[string]any{"name": "reviewer"},
		"peer_review_of": map[string]any{
			"completion_feedback_event_id": latestLocalReportID(t, store, selection.Workspace.ID, work.Key),
			"git_receipt":                  map[string]any{"head_revision": "receipt-1"},
		},
		"criteria": []any{map[string]any{
			"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"summary": "reviewed"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "receipt-2")
	closeLocalChangeStore(t, deps, stateDir, store)

	got := runChangeStatusJSON(t, deps, out, work.Key)
	if !got.Stale || got.Decision != "Awaiting human acceptance" || got.State != "awaiting_acceptance" {
		t.Fatalf("stale peer changed delivery authority: %#v", got)
	}
}

func TestChangeAcceptPostsHumanDecisionWithFacadeEnvelope(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	if err := (config.Config{CurrentUser: config.CurrentUser{Username: "lead"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "accept", "CR-101", "--note", "reviewed and accepted")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastDeliveryDecisionID != "cr-1" || fc.lastDeliveryDecision.Decision != "approve" || fc.lastDeliveryDecision.Actor != "lead" || fc.lastDeliveryDecision.Note != "reviewed and accepted" {
		t.Fatalf("decision = id %q body %+v", fc.lastDeliveryDecisionID, fc.lastDeliveryDecision)
	}
	var envelope struct {
		Command string `json:"command"`
		OK      bool   `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Command != "change.accept" || !envelope.OK {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestChangeRequestChangesPostsRejectWithFacadeEnvelope(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	if err := (config.Config{CurrentUser: config.CurrentUser{Username: "reviewer"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "request-changes", "CR-101", "--note", "criterion two is missing")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastDeliveryDecision.Decision != "reject" || fc.lastDeliveryDecision.Actor != "reviewer" || fc.lastDeliveryDecision.Note != "criterion two is missing" {
		t.Fatalf("decision = %+v", fc.lastDeliveryDecision)
	}
	var envelope struct {
		Command string `json:"command"`
		OK      bool   `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Command != "change.request-changes" || !envelope.OK {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestChangeSubmitWithFileRunsFullTailWithFacadeEnvelope(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "All done",
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "submit", "CR-101", "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastFeedbackBody == nil || fc.calls != 4 || !fc.lastDetailFlag {
		t.Fatalf("feedback = %#v calls = %d detail = %t", fc.lastFeedbackBody, fc.calls, fc.lastDetailFlag)
	}
	var envelope struct {
		Command string         `json:"command"`
		OK      bool           `json:"ok"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Command != "change.submit" || !envelope.OK {
		t.Fatalf("envelope = %#v", envelope)
	}
	if envelope.Data["state"] != "awaiting_acceptance" || envelope.Data["next_actor"] != "human_reviewer" || envelope.Data["next_command"] == "" {
		t.Fatalf("facade result is not actionable: %#v", envelope.Data)
	}
	for _, internal := range []string{"gates", "report", "review", "status"} {
		if _, leaked := envelope.Data[internal]; leaked {
			t.Fatalf("facade leaked expert %q payload: %#v", internal, envelope.Data)
		}
	}
}
