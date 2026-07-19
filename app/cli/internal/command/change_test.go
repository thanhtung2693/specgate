package command_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestChangeApproveApprovesThenPromotesWithOneDecision(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.artifactResult = &client.Artifact{ID: "artifact-1", Version: "v1", Status: "draft", SnapshotDigest: "sha256:approved"}
	fc.updateStatusResult = &client.Artifact{ID: "artifact-1", Version: "v1", Status: "approved"}
	fc.promoteResult = &client.Feature{ID: "feature-1", Key: "LOGIN", Version: 1, CanonicalArtifactID: "artifact-1"}

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "--yes", "change", "approve", "artifact-1", "--note", "scope approved",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if strings.Join(fc.callOrder, ",") != "artifact_status,artifact_promote" {
		t.Fatalf("call order = %v", fc.callOrder)
	}
	if fc.lastStatusInput.Status != "approved" || fc.lastStatusInput.Note != "scope approved" {
		t.Fatalf("status input = %#v", fc.lastStatusInput)
	}
	if fc.lastPromoteID != "artifact-1" {
		t.Fatalf("promoted artifact = %q", fc.lastPromoteID)
	}
	var envelope struct {
		Command string `json:"command"`
		Data    struct {
			ArtifactID      string `json:"artifact_id"`
			ArtifactVersion string `json:"artifact_version"`
			SnapshotDigest  string `json:"snapshot_digest"`
			FeatureKey      string `json:"feature_key"`
			Next            string `json:"next_command"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Command != "change.approve" || envelope.Data.ArtifactID != "artifact-1" ||
		envelope.Data.ArtifactVersion != "v1" || envelope.Data.SnapshotDigest != "sha256:approved" ||
		envelope.Data.FeatureKey != "LOGIN" || envelope.Data.Next == "" {
		t.Fatalf("result = %#v, output = %s", envelope, out.String())
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

func TestChangeSubmitValidatesCompletionBeforeFullTail(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.docs_updated",
		"summary":    "Updated docs",
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "submit", "CR-101", "--file", f)
	if code != output.ExitUsage || fc.calls != 0 {
		t.Fatalf("exit = %d calls = %d output = %s", code, fc.calls, out.String())
	}
	var envelope struct {
		Command string `json:"command"`
		OK      bool   `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Command != "change.submit" || envelope.OK {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestChangeSubmitRunChecksRequiresYesWithFacadeEnvelope(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "All done",
		"checks": []map[string]any{{
			"name": "tests", "command": "go test ./...", "status": "pass",
		}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "submit", "CR-101", "--file", f, "--run-checks")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	var envelope struct {
		Command string `json:"command"`
		OK      bool   `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Command != "change.submit" || envelope.OK {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestChangeSubmitUsesSafeRefDefaultFile(t *testing.T) {
	// No t.Parallel: t.Chdir changes process-wide state.
	deps, fc, _, out := newFakeDeps(t)
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.Mkdir(".specgate", 0o700); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"event_type": "coding_agent.completed",
		"summary":    "All done",
		"agent":      map[string]any{"name": "builder"},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(".specgate", "completion-CR-101.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "submit", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastFeedbackBody == nil {
		t.Fatal("default completion file was not submitted")
	}
}

func TestChangeSubmitDefaultFileDoesNotLeakAcrossCommandExecutions(t *testing.T) {
	// No t.Parallel: t.Chdir changes process-wide state.
	deps, fc, _, out := newFakeDeps(t)
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.Mkdir(".specgate", 0o700); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		ref     string
		summary string
	}{
		{ref: "CR-101", summary: "first completion"},
		{ref: "CR-202", summary: "second completion"},
	} {
		body, err := json.Marshal(map[string]any{
			"event_type": "coding_agent.completed",
			"summary":    fixture.summary,
			"agent":      map[string]any{"name": "builder"},
		})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(".specgate", "completion-"+fixture.ref+".json")
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	root := command.NewRootCommand(deps)
	for _, ref := range []string{"CR-101", "CR-202"} {
		out.Reset()
		code := command.ExecuteForCode(root, "--json", "change", "submit", ref)
		if code != output.ExitOK {
			t.Fatalf("%s: exit = %d, output = %s", ref, code, out.String())
		}
	}
	if got := fc.lastFeedbackBody["summary"]; got != "second completion" {
		t.Fatalf("second submission used summary %q, want second completion", got)
	}
}

func TestChangeSubmitRequiresFileForUnsafeRef(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "submit", "../CR-101")
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	if fc.calls != 0 || !strings.Contains(out.String(), "--file") {
		t.Fatalf("calls = %d output = %s", fc.calls, out.String())
	}
}

func TestChangeDecisionsLocalRequireYes(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "receipt-1")
	closeLocalChangeStore(t, deps, stateDir, store)

	for _, verb := range []string{"approve", "accept", "request-changes"} {
		out.Reset()
		code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", verb, work.Key)
		if code != output.ExitUsage || !strings.Contains(out.String(), "--yes") {
			t.Fatalf("%s: exit = %d output = %s", verb, code, out.String())
		}
		if fc.calls != 0 {
			t.Fatalf("%s: Local confirmation path made %d client calls", verb, fc.calls)
		}
	}
}

func TestChangeAcceptLocalWithYesStoresDecisionWithoutHTTP(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "builder", "receipt-1")
	closeLocalChangeStore(t, deps, stateDir, store)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "change", "accept", work.Key, "--note", "accepted locally")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("Local acceptance made %d client calls", fc.calls)
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
	verified, err := local.Open(stateDir + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer verified.Close()
	review, err := verified.DeliveryStatus(t.Context(), selection.Workspace.ID, work.Key)
	if err != nil {
		t.Fatal(err)
	}
	if review.HumanDecision != "approve" {
		t.Fatalf("stored decision = %q, want approve", review.HumanDecision)
	}
}

func TestChangeAcceptLocalRejectsCompletionReporterWithoutHTTP(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	stateDir, store, selection, work := newLocalChangeWork(t, deps)
	submitLocalChangeDelivery(t, store, selection.Workspace.ID, work, "local", "receipt-1")
	closeLocalChangeStore(t, deps, stateDir, store)

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json",
		"--yes",
		"change",
		"accept",
		work.Key,
	)
	if code == output.ExitOK ||
		!strings.Contains(out.String(), "cannot approve its own delivery") {
		t.Fatalf("exit = %d output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("Local self-approval check made %d HTTP calls", fc.calls)
	}
}

func TestChangeSubmitLocalUsesStoreWithoutHTTP(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)
	f := writeDeliveryJSON(t, map[string]any{
		"event_type":     "coding_agent.completed",
		"summary":        "All done",
		"context_digest": work.ContextDigest,
		"criteria": []map[string]any{{
			"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"summary": "targeted test passed"},
		}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "change", "submit", work.Key, "--file", f)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.calls != 0 {
		t.Fatalf("Local submit made %d client calls", fc.calls)
	}
	var envelope struct {
		Command string `json:"command"`
		OK      bool   `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Command != "change.submit" || !envelope.OK {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func newLocalChangeWork(t *testing.T, deps *command.Deps) (string, *local.Store, local.Selection, local.WorkItem) {
	t.Helper()
	stateDir := t.TempDir()
	store, err := local.Open(stateDir + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := store.Initialize(t.Context(), local.InitInput{WorkspaceName: "Local workspace", DisplayName: "Local developer", Username: "local"})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.PublishArtifact(t.Context(), selection.Workspace.ID, local.ArtifactInput{
		FeatureKey: "CHANGE", RequestType: "new_feature",
		Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("# Change\n\n## Acceptance criteria\n\n1. Status is actionable.")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RunReadiness(t.Context(), selection.Workspace.ID, artifact.ID); err != nil {
		t.Fatal(err)
	}
	tasks, err := store.ListGateTasks(t.Context(), selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		result := local.GateResultInput{Gate: task.GateKey, GateDigest: task.GateDigest, InputDigest: task.ArtifactDigest, State: "pass", Summary: "reviewed"}
		result.Evaluator.Executor = task.Executor
		if _, err := store.SubmitGateResult(t.Context(), selection.Workspace.ID, task.TaskID, result); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ApproveArtifact(t.Context(), selection.Workspace.ID, artifact.ID, "human", "approved"); err != nil {
		t.Fatal(err)
	}
	feature, err := store.PromoteArtifact(t.Context(), selection.Workspace.ID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(t.Context(), selection.Workspace.ID, local.WorkInput{FeatureRef: feature.Key, Title: "Add change status", AcceptanceCriteria: []string{"Status is actionable."}})
	if err != nil {
		t.Fatal(err)
	}
	return stateDir, store, selection, work
}

func submitLocalChangeDelivery(t *testing.T, store *local.Store, workspaceID string, work local.WorkItem, agent, receipt string) {
	t.Helper()
	_, err := store.SubmitDelivery(t.Context(), workspaceID, work.Key, map[string]any{
		"context_digest": work.ContextDigest,
		"agent":          map[string]any{"name": agent},
		"git_receipt":    map[string]any{"head_revision": receipt},
		"criteria": []any{map[string]any{
			"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"summary": "targeted test passed"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func latestLocalReportID(t *testing.T, store *local.Store, workspaceID, ref string) string {
	t.Helper()
	report, err := store.LatestDeliveryReport(t.Context(), workspaceID, ref)
	if err != nil {
		t.Fatal(err)
	}
	return report.ID
}

func closeLocalChangeStore(t *testing.T, deps *command.Deps, stateDir string, store *local.Store) {
	t.Helper()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
}
