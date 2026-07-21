package command

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

type changeStatusResult struct {
	Mode        config.Mode `json:"mode"`
	Ref         string      `json:"ref"`
	Title       string      `json:"title"`
	State       string      `json:"state"`
	Evidence    string      `json:"evidence"`
	Assurance   string      `json:"assurance"`
	Decision    string      `json:"decision"`
	Receipt     string      `json:"receipt"`
	Freshness   string      `json:"freshness"`
	NextActor   string      `json:"next_actor"`
	Missing     []string    `json:"missing"`
	Guidance    string      `json:"guidance,omitempty"`
	Stale       bool        `json:"stale"`
	StaleReason string      `json:"stale_reason,omitempty"`
	NextCommand string      `json:"next_command"`
}

func newChangeCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "change",
		Short: "Use the normal governed change workflow",
		Long: "Use change for the normal approval and post-handoff delivery path. Status makes the next\n" +
			"actor, trust signals, missing evidence, and exact next command explicit; submit\n" +
			"runs the delivery tail; approve, accept, and request-changes record human decisions.\n\n" +
			"This facade reads and acts on the existing work and delivery records. For detailed\n" +
			"diagnosis or preparation, use the delivery, work, gates, artifact, audit, and\n" +
			"verify command families.",
		Example: "  specgate change status CR-123\n" +
			"  specgate change submit CR-123\n" +
			"  specgate change submit CR-123 --file .specgate/completion-CR-123.json\n" +
			"  specgate --yes change accept CR-123 --note \"Approved after review\"\n" +
			"  specgate --yes change request-changes CR-123 --note \"Please address the failing check\"",
	}
	cmd.AddCommand(newDeliverySubmitCommand(deps, deliverySubmitCommandSpec{
		Use:   "submit <ref>",
		Short: "Submit completion evidence and run the delivery tail",
		Long: "Submit one completion file, run delivery gates, trigger review, and return the combined verdict.\n\n" +
			"Default completion file: .specgate/completion-<ref>.json for a file-safe ref made of\n" +
			"letters, digits, hyphens (-), and underscores (_).\n" +
			"--file is required when the ref is not file-safe.",
		Example: "  specgate change submit CR-123\n" +
			"  specgate change submit CR-123 --file .specgate/completion-CR-123.json --json",
		Operation:          "change.submit",
		DefaultFileFromRef: true,
		CompactJSON:        true,
	}))
	cmd.AddCommand(newChangeApproveCmd(deps))
	cmd.AddCommand(newChangeDecisionCmd(deps, "accept", "Accept a change as a human reviewer", "change.accept", "approve", "Accept change for %s?"))
	cmd.AddCommand(newChangeDecisionCmd(deps, "request-changes", "Request changes from the implementing agent", "change.request-changes", "reject", "Request changes for %s?"))
	cmd.AddCommand(&cobra.Command{
		Use:   "status <work-ref>",
		Short: "Show actionable change status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				result changeStatusResult
				err    error
			)
			if deps.Topology == config.ModeLocal {
				result, err = changeStatusLocal(cmd, deps, args[0])
				if err != nil {
					return localExitError(deps, "change.status", err)
				}
			} else {
				result, err = changeStatusFull(cmd, deps, args[0])
				if err != nil {
					var resolveErr *changeStatusResolveError
					if errors.As(err, &resolveErr) {
						code := deps.Printer.Error("change.status", mapWorkRefError(resolveErr.ref, resolveErr.err))
						return &output.ExitError{Code: code, Err: resolveErr.err}
					}
					return apiExitError(deps, "change.status", err)
				}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("change.status", result)
				return nil
			}
			printChangeStatus(deps, result)
			return nil
		},
	})
	return cmd
}

func newChangeApproveCmd(deps *Deps) *cobra.Command {
	var (
		note        string
		title       string
		description string
		criteria    []string
	)
	cmd := &cobra.Command{
		Use:   "approve <artifact-id>",
		Short: "Approve an exact snapshot and create its implementation handoff",
		Long: "Record one human approval for the exact artifact snapshot, then promote that\n" +
			"approved version, create its artifact-bound work item, and verify the derived\n" +
			"Context Pack. Supply the human-reviewed title and acceptance criteria explicitly;\n" +
			"SpecGate never infers them from document prose. Exact retries reuse existing work.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if deps.Topology == config.ModeLocal && !deps.Yes {
				payload := output.ErrorPayload{
					Code:    "confirmation_required",
					Message: "Local artifact approval requires an explicit human assertion; re-run with --yes.",
				}
				code := deps.Printer.Error("change.approve", payload)
				return &output.ExitError{Code: code}
			}
			title = strings.TrimSpace(title)
			criteria = normalizedChangeApprovalCriteria(criteria)
			if title == "" || len(criteria) == 0 {
				payload := output.ErrorPayload{
					Code:    "usage",
					Message: "change approve requires --title and at least one --ac so the approved snapshot has an explicit implementation handoff",
				}
				code := deps.Printer.Error("change.approve", payload)
				return &output.ExitError{Code: code}
			}
			if deps.Topology == config.ModeLocal {
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "change.approve", err)
				}
				defer store.Close()
				selection, err := localSelection(cmd.Context(), deps, store)
				if err != nil {
					return localExitError(deps, "change.approve", err)
				}
				artifact, err := store.GetArtifact(cmd.Context(), selection.Workspace.ID, id)
				if err != nil {
					return localExitError(deps, "change.approve", err)
				}
				if artifact.Status != "approved" {
					if err := store.ApproveArtifact(cmd.Context(), selection.Workspace.ID, id, selection.User.Username, note); err != nil {
						return localExitError(deps, "change.approve", err)
					}
				}
				feature, err := store.PromoteArtifact(cmd.Context(), selection.Workspace.ID, id)
				if err != nil {
					return localExitError(deps, "change.approve", err)
				}
				handoff, err := ensureLocalApprovedWork(
					cmd.Context(), store, selection.Workspace.ID, artifact.ID, feature.Key, title, description, criteria,
				)
				if err != nil {
					return localExitError(deps, "change.approve", err)
				}
				return printChangeApproval(deps, id, "v"+strconv.Itoa(artifact.Version), artifact.SnapshotDigest, feature.Key, feature.Version, handoff)
			}

			artifact, err := deps.Client.GetArtifact(cmd.Context(), id)
			if err != nil {
				return apiExitError(deps, "change.approve", err)
			}
			prompt := fmt.Sprintf(
				"Approve artifact %s (%s, digest %s) and hand off %q with %d acceptance criteria?",
				id, artifact.Version, artifact.SnapshotDigest, title, len(criteria),
			)
			proceed, err := requireConfirm(deps, prompt)
			if err != nil || !proceed {
				return err
			}
			if artifact.Status != "approved" {
				if _, err := deps.Client.UpdateArtifactStatus(cmd.Context(), id, client.UpdateArtifactStatusInput{
					Status: "approved", ApprovedBy: currentActor(deps), ActorKind: "human", Note: note,
				}); err != nil {
					return apiExitError(deps, "change.approve", err)
				}
			}
			feature, err := deps.Client.PromoteArtifactCanonical(cmd.Context(), id, currentActor(deps))
			if err != nil {
				return apiExitError(deps, "change.approve", err)
			}
			handoff, err := ensureFullApprovedWork(cmd.Context(), deps, id, feature.Key, title, description, criteria)
			if err != nil {
				return apiExitError(deps, "change.approve", err)
			}
			return printChangeApproval(deps, id, artifact.Version, artifact.SnapshotDigest, feature.Key, feature.Version, handoff)
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "Optional reviewer note recorded with the approval")
	cmd.Flags().StringVar(&title, "title", "", "Implementation work title (required)")
	cmd.Flags().StringVar(&description, "description", "", "Implementation scope or description")
	cmd.Flags().StringArrayVar(&criteria, "ac", nil, "Acceptance criterion (required and repeatable; append @check:<name> for a confirmed check binding)")
	return cmd
}

type changeApprovalHandoff struct {
	WorkID        string
	WorkRef       string
	ContextState  string
	ContextDigest string
	WorkPhase     string
	Created       bool
}

func normalizedChangeApprovalCriteria(input []string) []string {
	seen := make(map[string]bool, len(input))
	result := make([]string, 0, len(input))
	for _, raw := range input {
		text, binding := parseAcceptanceCriterionBinding(raw)
		criterion := text
		if binding != "" {
			criterion += " @check:" + binding
		}
		if criterion == "" || seen[criterion] {
			continue
		}
		seen[criterion] = true
		result = append(result, criterion)
	}
	return result
}

func ensureLocalApprovedWork(
	ctx context.Context,
	store *local.Store,
	workspaceID, artifactID, featureKey, title, description string,
	criteria []string,
) (changeApprovalHandoff, error) {
	items, err := store.ListWork(ctx, workspaceID)
	if err != nil {
		return changeApprovalHandoff{}, err
	}
	for _, item := range items {
		if item.ArtifactID != artifactID {
			continue
		}
		if item.Title != title || strings.TrimSpace(item.Description) != strings.TrimSpace(description) ||
			!sameStrings(item.AcceptanceCriteria, criteria) {
			return changeApprovalHandoff{}, fmt.Errorf(
				"artifact %s already has work %s with a different work contract; use that work or create a separate expert work item",
				artifactID, item.Key,
			)
		}
		pack, err := store.ContextPack(ctx, workspaceID, item.Key)
		if err != nil {
			return changeApprovalHandoff{}, err
		}
		return changeApprovalHandoff{
			WorkID: item.ID, WorkRef: item.Key, ContextState: "assembled", ContextDigest: pack.Digest, WorkPhase: item.Phase,
		}, nil
	}
	work, err := store.CreateWork(ctx, workspaceID, local.WorkInput{
		FeatureRef: featureKey, Title: title, Description: description, AcceptanceCriteria: criteria,
	})
	if err != nil {
		return changeApprovalHandoff{}, err
	}
	if work.ArtifactID != artifactID {
		return changeApprovalHandoff{}, fmt.Errorf("created work %s is not bound to approved artifact %s", work.Key, artifactID)
	}
	pack, err := store.ContextPack(ctx, workspaceID, work.Key)
	if err != nil {
		return changeApprovalHandoff{}, err
	}
	return changeApprovalHandoff{
		WorkID: work.ID, WorkRef: work.Key, ContextState: "assembled", ContextDigest: pack.Digest, WorkPhase: work.Phase, Created: true,
	}, nil
}

func ensureFullApprovedWork(
	ctx context.Context,
	deps *Deps,
	artifactID, featureKey, title, description string,
	criteria []string,
) (changeApprovalHandoff, error) {
	body := map[string]any{
		"feature": featureKey, "title": title, "acceptance_criteria": criteria,
	}
	if strings.TrimSpace(description) != "" {
		body["description"] = strings.TrimSpace(description)
	}
	if err := annotateBodyWithCurrentSelection(ctx, deps, body); err != nil {
		return changeApprovalHandoff{}, err
	}
	workspaceID, _ := body["workspace_id"].(string)
	requestCtx := requestContextForBody(ctx, body)
	items, err := deps.Client.ListWorkItemsIncludingArchived(requestCtx, workspaceID)
	if err != nil {
		return changeApprovalHandoff{}, err
	}
	for _, item := range items {
		if item.LeadArtifactID != artifactID {
			continue
		}
		storedCriteria, err := deps.Client.ListAcceptanceCriteria(requestCtx, item.ID)
		if err != nil {
			return changeApprovalHandoff{}, err
		}
		if item.Title != title || strings.TrimSpace(item.IntentMD) != strings.TrimSpace(description) ||
			!sameStrings(fullCriterionArguments(storedCriteria), criteria) {
			return changeApprovalHandoff{}, fmt.Errorf(
				"artifact %s already has work %s with a different work contract; use that work or create a separate expert work item",
				artifactID, item.Key,
			)
		}
		pack, err := deps.Client.ContextPack(requestCtx, item.ID)
		if err != nil {
			return changeApprovalHandoff{}, err
		}
		if pack.State != "assembled" {
			return changeApprovalHandoff{}, fmt.Errorf("context pack for %s is %s, want assembled", item.Key, pack.State)
		}
		return changeApprovalHandoff{WorkID: item.ID, WorkRef: item.Key, ContextState: pack.State, WorkPhase: item.Phase}, nil
	}
	created, err := deps.Client.CreateWorkItem(requestCtx, body)
	if err != nil {
		return changeApprovalHandoff{}, err
	}
	workID, _ := created["change_request_id"].(string)
	workRef, _ := created["change_request_key"].(string)
	leadArtifactID, _ := created["lead_artifact_id"].(string)
	if strings.TrimSpace(workID) == "" || strings.TrimSpace(workRef) == "" || leadArtifactID != artifactID {
		return changeApprovalHandoff{}, fmt.Errorf("created work did not return the expected approved artifact binding")
	}
	pack, err := deps.Client.ContextPack(requestCtx, workID)
	if err != nil {
		return changeApprovalHandoff{}, err
	}
	if pack.State != "assembled" {
		return changeApprovalHandoff{}, fmt.Errorf("context pack for %s is %s, want assembled", workRef, pack.State)
	}
	return changeApprovalHandoff{WorkID: workID, WorkRef: workRef, ContextState: pack.State, WorkPhase: "ready", Created: true}, nil
}

func fullCriterionArguments(criteria []client.AcceptanceCriterion) []string {
	result := make([]string, 0, len(criteria))
	for _, criterion := range criteria {
		value := strings.TrimSpace(criterion.Text)
		if binding := strings.TrimSpace(criterion.VerificationBinding); binding != "" {
			value += " @check:" + binding
		}
		result = append(result, value)
	}
	return result
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func printChangeApproval(
	deps *Deps,
	artifactID, artifactVersion, snapshotDigest, featureKey string,
	featureVersion int,
	handoff changeApprovalHandoff,
) error {
	next := "specgate work context " + handoff.WorkRef + " --json"
	state := "ready_for_implementation"
	if strings.EqualFold(strings.TrimSpace(handoff.WorkPhase), "delivered") {
		state = "already_delivered"
		next = "specgate change status " + handoff.WorkRef
	}
	result := map[string]any{
		"artifact_id": artifactID, "artifact_version": artifactVersion, "snapshot_digest": snapshotDigest,
		"feature_key": featureKey, "version": featureVersion,
		"work_id": handoff.WorkID, "work_ref": handoff.WorkRef, "work_created": handoff.Created, "work_phase": handoff.WorkPhase,
		"context_state": handoff.ContextState, "state": state, "next_command": next,
	}
	if handoff.ContextDigest != "" {
		result["context_digest"] = handoff.ContextDigest
	}
	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success("change.approve", result)
		return nil
	}
	fmt.Fprintf(deps.Stdout, "%s %s (artifact %s, digest %s); canonical for feature %s (v%d)\n",
		styled(deps, output.StyleSuccess, "Approved"), styled(deps, output.StyleBold, artifactID),
		artifactVersion, snapshotDigest, featureKey, featureVersion)
	fmt.Fprintf(deps.Stdout, "%s %s; Context Pack %s.\n", label(deps, "Work:"), styled(deps, output.StyleBold, handoff.WorkRef), handoff.ContextState)
	fmt.Fprintln(deps.Stdout, nextStep(deps, "Start implementation by reading the exact handoff with", next))
	return nil
}

func newChangeDecisionCmd(deps *Deps, use, short, operation, decision, prompt string) *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:   use + " <ref>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeliveryDecision(cmd, deps, args, operation, decision, note, prompt)
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "Optional reviewer note recorded with the decision")
	return cmd
}

func changeStatusFull(cmd *cobra.Command, deps *Deps, ref string) (changeStatusResult, error) {
	work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
	if err != nil {
		return changeStatusResult{}, &changeStatusResolveError{ref: ref, err: err}
	}
	delivery, err := deps.Client.DeliveryStatus(cmd.Context(), work.ChangeRequestID, true)
	if err != nil {
		return changeStatusResult{}, err
	}
	return deriveFullChangeStatus(work, delivery), nil
}

type changeStatusResolveError struct {
	ref string
	err error
}

func (e *changeStatusResolveError) Error() string { return e.err.Error() }

func (e *changeStatusResolveError) Unwrap() error { return e.err }

func changeStatusLocal(cmd *cobra.Command, deps *Deps, ref string) (changeStatusResult, error) {
	store, err := openLocalStore(deps)
	if err != nil {
		return changeStatusResult{}, err
	}
	defer store.Close()
	selection, err := localSelection(cmd.Context(), deps, store)
	if err != nil {
		return changeStatusResult{}, err
	}
	work, err := store.GetWork(cmd.Context(), selection.Workspace.ID, ref)
	if err != nil {
		return changeStatusResult{}, err
	}
	review, err := store.DeliveryStatus(cmd.Context(), selection.Workspace.ID, work.Key)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return changeStatusResult{}, err
	}
	if err != nil {
		return deriveLocalChangeStatus(work, nil, nil, local.PeerReviewStatus{State: "not_run"}), nil
	}
	report, err := store.DeliveryReportForReview(cmd.Context(), selection.Workspace.ID, review)
	if err != nil {
		return changeStatusResult{}, err
	}
	peer, err := store.PeerReviewStatus(cmd.Context(), selection.Workspace.ID, work.Key)
	if err != nil {
		return changeStatusResult{}, err
	}
	return deriveLocalChangeStatus(work, &review, &report, peer), nil
}

func deriveFullChangeStatus(work *client.ResolvedWork, delivery *client.DeliveryStatusResult) changeStatusResult {
	result := changeStatusResult{
		Mode: config.ModeFull, Ref: work.ChangeRequestKey, Title: work.Title,
		Missing: []string{},
	}
	if delivery == nil || !delivery.Found {
		return implementationChangeStatus(result)
	}
	result.Evidence = deliveryEvidenceLabel(deliveryEvidenceVerdict(delivery), delivery.ReasonCode)
	result.Assurance = deliveryAssuranceLabel(delivery)
	result.Decision = deliveryDecisionLabel(delivery)
	result.Receipt = deliveryReceiptLabel(delivery.GitReceipt)
	hasReceipt := deliveryGitReceiptAvailable(delivery.GitReceipt)
	result.Freshness, result.Stale, result.StaleReason = changeFreshness(hasReceipt, delivery.PeerReview.State)
	if strings.TrimSpace(delivery.Executor) == "human" {
		if passingStatus(delivery.Verdict) {
			return acceptedChangeStatus(result)
		}
		result.Guidance = strings.TrimSpace(delivery.Note)
		if result.Guidance == "" {
			result.Guidance = strings.TrimSpace(delivery.Summary)
		}
		return reworkRequestedChangeStatus(result)
	}
	if strings.TrimSpace(delivery.ReasonCode) == "delivery_review_outdated" {
		result.State = "review_pending"
		result.NextActor = "implementing_agent"
		result.Missing = []string{"Delivery review for the latest completion"}
		result.NextCommand = "specgate delivery review " + result.Ref
		return result
	}
	if strings.TrimSpace(delivery.ReasonCode) == "policy_unavailable" {
		result.State = "blocked"
		result.NextActor = "maintainer"
		result.Missing = []string{"Delivery policy unavailable"}
		result.NextCommand = "specgate delivery status " + result.Ref + " --detail"
		return result
	}
	if strings.TrimSpace(delivery.Verdict) == "needs_human_review" && strings.TrimSpace(delivery.Executor) != "human" {
		return humanReviewRequiredChangeStatus(result, delivery.Hint)
	}
	if passingStatus(delivery.Verdict) {
		return awaitingAcceptanceChangeStatus(result)
	}
	return implementationRequiredChangeStatus(result)
}

func deriveLocalChangeStatus(work local.WorkItem, review *local.DeliveryReview, report *local.DeliveryReport, peer local.PeerReviewStatus) changeStatusResult {
	result := changeStatusResult{
		Mode: config.ModeLocal, Ref: work.Key, Title: work.Title, Missing: []string{},
	}
	if review == nil || report == nil {
		return implementationChangeStatus(result)
	}
	result.Evidence = deliveryEvidenceLabel(review.Verdict, "")
	result.Assurance = localDeliveryAssuranceLabel(report.Body, peer)
	result.Decision = localDeliveryDecisionLabel(review.HumanDecision)
	result.Receipt = localDeliveryReceiptLabel(report.Body)
	result.Freshness, result.Stale, result.StaleReason = changeFreshness(localDeliveryReceiptAvailable(report.Body), peer.State)
	if review.HumanDecision == "approve" {
		return acceptedChangeStatus(result)
	}
	if review.HumanDecision == "reject" {
		result.Guidance = strings.TrimSpace(review.Note)
		return reworkRequestedChangeStatus(result)
	}
	if peer.State == "failed" {
		return humanReviewRequiredChangeStatus(result, "Peer review found evidence gaps")
	}
	if review.Verdict == "passed" {
		return awaitingAcceptanceChangeStatus(result)
	}
	return implementationRequiredChangeStatus(result)
}

func implementationChangeStatus(result changeStatusResult) changeStatusResult {
	result.State = "implementation"
	result.Evidence = "Not reviewed"
	result.Assurance = "Agent-reported"
	result.Decision = "Awaiting human acceptance"
	result.Receipt = "No Git receipt recorded"
	result.Freshness = "No stored receipt was checked against the current checkout."
	result.NextActor = "implementing_agent"
	result.Missing = []string{"Delivery evidence"}
	result.NextCommand = deliveryReportScaffold(result.Ref)
	return result
}

func awaitingAcceptanceChangeStatus(result changeStatusResult) changeStatusResult {
	result.State = "awaiting_acceptance"
	result.NextActor = "human_reviewer"
	result.Missing = []string{"Human acceptance"}
	if result.Mode == config.ModeLocal {
		result.NextCommand = "specgate --yes change accept " + result.Ref
	} else {
		result.NextCommand = "specgate change accept " + result.Ref
	}
	return result
}

func humanReviewRequiredChangeStatus(result changeStatusResult, hint string) changeStatusResult {
	result.State = "awaiting_review"
	result.NextActor = "human_reviewer"
	hint = strings.TrimSpace(hint)
	if hint == "" {
		hint = "Independent confirmation required"
	}
	result.Missing = []string{hint}
	result.NextCommand = "specgate delivery status " + result.Ref + " --detail"
	return result
}

func acceptedChangeStatus(result changeStatusResult) changeStatusResult {
	result.State = "accepted"
	result.NextActor = "none"
	result.Missing = []string{}
	result.NextCommand = "specgate audit " + result.Ref
	return result
}

func reworkRequestedChangeStatus(result changeStatusResult) changeStatusResult {
	result.State = "rework_requested"
	result.NextActor = "implementing_agent"
	result.Missing = []string{"Revised completion addressing requested changes"}
	result.NextCommand = deliveryReportScaffold(result.Ref)
	return result
}

func implementationRequiredChangeStatus(result changeStatusResult) changeStatusResult {
	result.State = "implementation"
	result.NextActor = "implementing_agent"
	result.Missing = []string{"Passing delivery evidence"}
	result.NextCommand = deliveryReportScaffold(result.Ref)
	return result
}

func deliveryReportScaffold(ref string) string {
	return "specgate delivery report " + ref + " --init"
}

func deliveryGitReceiptAvailable(receipt *client.GitReceipt) bool {
	if receipt == nil || strings.TrimSpace(receipt.HeadRevision) == "" {
		return false
	}
	availability := strings.TrimSpace(receipt.Availability)
	return availability == "" || availability == "available"
}

func localDeliveryReceiptAvailable(body map[string]any) bool {
	receipt, _ := body["git_receipt"].(map[string]any)
	if receipt == nil || strings.TrimSpace(fmt.Sprint(receipt["head_revision"])) == "" {
		return false
	}
	availability := strings.TrimSpace(fmt.Sprint(receipt["availability"]))
	return availability == "" || availability == "available"
}

func changeFreshness(hasReceipt bool, peerState string) (string, bool, string) {
	if strings.TrimSpace(peerState) == "stale" {
		if !hasReceipt {
			return "Peer review is stale; no stored receipt was checked against the current checkout.", true, "Peer review is stale"
		}
		return "Peer review is stale; the stored receipt was not checked against the current checkout.", true, "Peer review is stale"
	}
	if !hasReceipt {
		return "No stored receipt was checked against the current checkout.", false, ""
	}
	return "The stored receipt was not checked against the current checkout.", false, ""
}

func printChangeStatus(deps *Deps, result changeStatusResult) {
	fmt.Fprintf(deps.Stdout, "Change: %s — %s\n", result.Ref, result.Title)
	fmt.Fprintf(deps.Stdout, "State: %s\n", result.State)
	fmt.Fprintf(deps.Stdout, "Evidence: %s\n", result.Evidence)
	fmt.Fprintf(deps.Stdout, "Assurance: %s\n", result.Assurance)
	fmt.Fprintf(deps.Stdout, "Decision: %s\n", result.Decision)
	fmt.Fprintf(deps.Stdout, "Receipt: %s\n", result.Receipt)
	fmt.Fprintf(deps.Stdout, "Freshness: %s\n", result.Freshness)
	fmt.Fprintf(deps.Stdout, "Next actor: %s\n", result.NextActor)
	missing := strings.Join(result.Missing, ", ")
	if missing == "" {
		missing = "none"
	}
	fmt.Fprintf(deps.Stdout, "Missing: %s\n", missing)
	if result.Guidance != "" {
		fmt.Fprintf(deps.Stdout, "Requested changes: %s\n", result.Guidance)
	}
	fmt.Fprintf(deps.Stdout, "Stale: %t\n", result.Stale)
	if result.StaleReason != "" {
		fmt.Fprintf(deps.Stdout, "Stale reason: %s\n", result.StaleReason)
	}
	fmt.Fprintf(deps.Stdout, "Next: %s\n", result.NextCommand)
}
