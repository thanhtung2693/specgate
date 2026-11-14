package command

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
	"github.com/specgate/specgate/app/cli/internal/provenance"
)

const deliveryInitAuto = "auto"

// specgate delivery report [work-ref] [--file <evidence.json>] [--init[=path] [--force]]

func newDeliveryReportCmd(deps *Deps) *cobra.Command {
	var (
		filePath          string
		initPath          string
		force             bool
		skipEvidenceCheck bool
	)

	cmd := &cobra.Command{
		Use:   "report [ref]",
		Short: "Scaffold a coding-agent completion with --init",
		Args: func(cmd *cobra.Command, args []string) error {
			if initPath == deliveryInitAuto && len(args) == 2 {
				return nil
			}
			return cobra.MaximumNArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if initPath != "" {
				if initPath == deliveryInitAuto && len(args) == 2 {
					initPath = args[1]
					args = args[:1]
				}
				return runLocalDeliveryReportInit(cmd, args, deps, initPath, force)
			}
			return incompatibleCommand(deps, "delivery.report", "use `specgate delivery report <ref> --init` to scaffold or `specgate change submit <ref> --file <completion.json>` to submit evidence")
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "JSON file containing the feedback body")
	cmd.Flags().BoolVar(&skipEvidenceCheck, "skip-evidence-check", false, "Skip verifying that cited evidence paths exist in the working tree")
	cmd.Flags().StringVar(&initPath, "init", "", "Write a completion template for the work item instead of reporting (default path .specgate/completion-<ref>.json; pass --init=<path> for a specific file)")
	cmd.Flags().Lookup("init").NoOptDefVal = deliveryInitAuto
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing file with --init")
	cmd.MarkFlagsMutuallyExclusive("file", "init")
	return cmd
}

// newDeliveryPeerReviewCmd records a second agent's review of the latest
// completion. The Local store verifies the bound completion, receipt and criteria.
func newDeliveryPeerReviewCmd(deps *Deps) *cobra.Command {
	var filePath, initPath string
	var force bool
	cmd := &cobra.Command{
		Use:   "peer-review [ref]",
		Short: "Record an independent agent review of the latest completion",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if initPath != "" {
				return runLocalDeliveryPeerReviewInit(cmd, args, deps, initPath, force)
			}
			if filePath == "" {
				return completionValidationError(deps, "delivery.peer-review", "--file is required (scaffold one with `specgate delivery peer-review --init`)")
			}
			body, err := readJSONBodyFile(deps, "delivery.peer-review", filePath)
			if err != nil {
				return err
			}
			if strings.TrimSpace(fmt.Sprint(body["event_type"])) != "coding_agent.peer_reviewed" {
				return completionValidationError(deps, "delivery.peer-review", "event_type must be coding_agent.peer_reviewed")
			}
			if completionAgentName(body) == "" {
				return completionValidationError(deps, "delivery.peer-review", "agent.name is required")
			}
			if err := validateCompletionReport(deps, "delivery.peer-review", body, false); err != nil {
				return err
			}
			if err := verifyCompletionEvidence(deps, "delivery.peer-review", body); err != nil {
				return err
			}
			if len(args) == 0 {
				return localExitError(deps, "delivery.peer-review", ErrWorkRefRequired)
			}
			delete(body, "git_receipt")
			store, err := openLocalStore(deps)
			if err != nil {
				return localExitError(deps, "delivery.peer-review", err)
			}
			defer store.Close()
			selection, err := localSelection(cmd.Context(), deps, store)
			if err != nil {
				return localExitError(deps, "delivery.peer-review", err)
			}
			review, err := store.PeerReviewDelivery(cmd.Context(), selection.Workspace.ID, args[0], body)
			if err != nil {
				return localExitError(deps, "delivery.peer-review", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("delivery.peer-review", review)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "Peer review recorded for %s by %s\n", args[0], review.AgentName)
			return nil
		},
	}
	cmd.Flags().StringVar(&filePath, "file", "", "JSON file containing the peer review")
	cmd.Flags().StringVar(&initPath, "init", "", "Write a peer-review template (default .specgate/peer-review-<ref>.json)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing file with --init")
	cmd.Flags().Lookup("init").NoOptDefVal = deliveryInitAuto
	cmd.MarkFlagsMutuallyExclusive("file", "init")
	return cmd
}

// Peer reviews must preserve exact completion binding. Refuse unsafe legacy
// bindings instead of rewriting them into a receipt the completion never had.
func validatePeerReceiptOrigin(deps *Deps, command string, receipt map[string]any) error {
	if raw, exists := receipt["repository"]; exists {
		origin, ok := raw.(string)
		if !ok || provenance.Repository(origin) != origin {
			return completionValidationError(deps, command, "legacy git_receipt is not credential-free; submit a fresh completion before peer review")
		}
	}
	return nil
}

func runLocalDeliveryPeerReviewInit(cmd *cobra.Command, args []string, deps *Deps, path string, force bool) error {
	if len(args) == 0 {
		return localExitError(deps, "delivery.peer-review", ErrWorkRefRequired)
	}
	store, err := openLocalStore(deps)
	if err != nil {
		return localExitError(deps, "delivery.peer-review", err)
	}
	defer store.Close()
	selection, err := localSelection(cmd.Context(), deps, store)
	if err != nil {
		return localExitError(deps, "delivery.peer-review", err)
	}
	work, err := store.GetWork(cmd.Context(), selection.Workspace.ID, args[0])
	if err != nil {
		return localExitError(deps, "delivery.peer-review", err)
	}
	completion, err := store.LatestDeliveryReport(cmd.Context(), selection.Workspace.ID, work.Key)
	if err != nil {
		return localExitError(deps, "delivery.peer-review", err)
	}
	if completionAgentName(completion.Body) == "" {
		return localExitError(deps, "delivery.peer-review", fmt.Errorf("latest completion agent.name is required"))
	}
	receipt, _ := completion.Body["git_receipt"].(map[string]any)
	if receipt == nil {
		return localExitError(deps, "delivery.peer-review", fmt.Errorf("latest completion has no git_receipt"))
	}
	if err := validatePeerReceiptOrigin(deps, "delivery.peer-review", receipt); err != nil {
		return err
	}
	if path == deliveryInitAuto {
		if err := ensureSpecgateWorkingDir(); err != nil {
			return localExitError(deps, "delivery.peer-review", err)
		}
		_ = config.EnsureSpecgateDirGitignore(".specgate")
		path = filepath.Join(".specgate", "peer-review-"+work.Key+".json")
	}
	criteria := make([]map[string]any, 0, len(work.AcceptanceCriteria))
	for index, criterion := range work.AcceptanceCriteria {
		criteria = append(criteria, map[string]any{
			"criterion_id": fmt.Sprintf("local-%d", index+1),
			"text":         criterion,
			"claim":        "not_done",
			"evidence":     completionEvidenceTemplate{},
		})
	}
	body := map[string]any{
		"event_type": "coding_agent.peer_reviewed",
		"summary":    "",
		"agent":      map[string]any{"name": ""},
		"peer_review_of": map[string]any{
			"completion_feedback_event_id": completion.ID,
			"git_receipt":                  receipt,
		},
		"criteria": criteria,
	}
	data, _ := json.MarshalIndent(body, "", "  ")
	if err := writeDeliveryScaffold(path, append(data, '\n'), force); err != nil {
		return deliveryScaffoldWriteError(deps, "delivery.peer-review", path, err)
	}
	writePeerReviewScaffoldSuccess(deps, work.Key, path, len(criteria))
	return nil
}

func writePeerReviewScaffoldSuccess(deps *Deps, workKey, path string, criteria int) {
	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success("delivery.peer-review", map[string]any{"path": path, "criteria": criteria})
		return
	}
	fmt.Fprintf(
		deps.Stdout,
		"%s %s for %s (%d acceptance criteria).\n",
		styled(deps, output.StyleSuccess, "Wrote"),
		styled(deps, output.StyleAction, path),
		styled(deps, output.StyleBold, workKey),
		criteria,
	)
	fmt.Fprintln(
		deps.Stdout,
		label(deps, "Fill in:")+" reviewer name, summary, and per-criterion claim (satisfied|partial|not_done) with evidence {kind, path}.",
	)
	fmt.Fprintln(deps.Stdout, nextStep(deps, "submit the independent review with", fmt.Sprintf("specgate delivery peer-review %s --file %s", workKey, path)))
}

// completionTemplate is the completion.json scaffold written by
// `delivery report --init`. JSON has no comments, so example entries carry
// empty-string values that show the expected shape.
type completionTemplate struct {
	VerificationContractDigest string                        `json:"verification_contract_digest,omitempty"`
	EventType                  string                        `json:"event_type"`
	ChangeRequestID            string                        `json:"change_request_id"`
	Summary                    string                        `json:"summary"`
	Agent                      map[string]string             `json:"agent"`
	ContextDigest              string                        `json:"context_digest,omitempty"`
	AffectedFiles              []string                      `json:"affected_files"`
	GitReceipt                 gitReceipt                    `json:"git_receipt"`
	Checks                     []completionCheckTemplate     `json:"checks"`
	Criteria                   []completionCriterionTemplate `json:"criteria"`
}

func runLocalDeliveryReportInit(cmd *cobra.Command, args []string, deps *Deps, path string, force bool) error {
	if len(args) == 0 {
		return localExitError(deps, "delivery.report", ErrWorkRefRequired)
	}
	store, err := openLocalStore(deps)
	if err != nil {
		return localExitError(deps, "delivery.report", err)
	}
	defer store.Close()
	selection, err := localSelection(cmd.Context(), deps, store)
	if err != nil {
		return localExitError(deps, "delivery.report", err)
	}
	work, err := store.GetWork(cmd.Context(), selection.Workspace.ID, args[0])
	if err != nil {
		return localExitError(deps, "delivery.report", err)
	}
	if path == deliveryInitAuto {
		if err := ensureSpecgateWorkingDir(); err != nil {
			return localExitError(deps, "delivery.report", err)
		}
		_ = config.EnsureSpecgateDirGitignore(".specgate")
		path = filepath.Join(".specgate", "completion-"+work.Key+".json")
	}
	generation, err := store.CheckoutGeneration(cmd.Context())
	if err != nil {
		return localExitError(deps, "delivery.report", err)
	}
	receipt := collectGitReceipt(cmd.Context(), deps.CommandRunner, deliveryWorkingDir(deps), nil, generation)
	if deps.Printer != nil && deps.Printer.Mode() != output.ModeJSON {
		for _, warning := range receipt.Warnings {
			fmt.Fprintf(deps.Stderr, "Warning: %s\n", warning)
		}
	}
	tpl := completionTemplate{
		EventType:       "coding_agent.completed",
		ChangeRequestID: work.ID,
		Agent:           map[string]string{"name": ""},
		ContextDigest:   work.ContextDigest,
		AffectedFiles:   []string{},
		GitReceipt:      receipt,
		Criteria:        make([]completionCriterionTemplate, 0, len(work.AcceptanceCriteria)),
	}
	bindings := make([]string, 0, len(work.AcceptanceCriteria))
	for index, criterion := range work.AcceptanceCriteria {
		text, binding := parseAcceptanceCriterionBinding(criterion)
		bindings = append(bindings, binding)
		tpl.Criteria = append(tpl.Criteria, completionCriterionTemplate{
			CriterionID: fmt.Sprintf("local-%d", index+1), Text: text,
			Claim: "not_done", VerificationBinding: binding,
		})
	}
	tpl.Checks = completionTemplateChecks(bindings)
	contract, err := store.GetVerificationContract(cmd.Context(), selection.Workspace.ID, work.Key)
	if err != nil {
		return localExitError(deps, "delivery.report", err)
	}
	if contract.Status == "pinned" {
		tpl.VerificationContractDigest = contract.Digest
		tpl.Checks = nil
		for _, check := range contract.Checks {
			tpl.Checks = append(tpl.Checks, completionCheckTemplate{Name: check.Name, Command: check.Command, Cwd: check.Cwd, Status: "pending"})
		}
	}
	data, err := json.MarshalIndent(tpl, "", "  ")
	if err != nil {
		return localExitError(deps, "delivery.report", err)
	}
	if err := writeDeliveryScaffold(path, append(data, '\n'), force); err != nil {
		return deliveryScaffoldWriteError(deps, "delivery.report", path, err)
	}
	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success("delivery.report", map[string]any{"path": path, "criteria": len(tpl.Criteria), "context_digest": work.ContextDigest})
		return nil
	}
	fmt.Fprintf(deps.Stdout, "Wrote %s for %s. Fill evidence, then run: specgate change submit %s --file %s\n", path, work.Key, work.Key, path)
	return nil
}

type completionCheckTemplate struct {
	Cwd     string `json:"cwd,omitempty"`
	Name    string `json:"name"`
	Command string `json:"command"`
	Status  string `json:"status"`
	Detail  string `json:"detail"`
}

type completionCriterionTemplate struct {
	CriterionID string `json:"criterion_id"`
	Text        string `json:"text"`
	Claim       string `json:"claim"` // satisfied | partial | not_done
	// VerificationBinding echoes any check-binding declared on the acceptance
	// criterion. When set, delivery review takes this
	// criterion's verdict from the named check instead of the LLM/claim path.
	VerificationBinding string                     `json:"verification_binding,omitempty"`
	Evidence            completionEvidenceTemplate `json:"evidence"`
}

type completionEvidenceTemplate struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

func completionTemplateChecks(bindings []string) []completionCheckTemplate {
	seen := make(map[string]bool, len(bindings))
	checks := make([]completionCheckTemplate, 0, len(bindings))
	for _, raw := range bindings {
		binding := strings.TrimSpace(raw)
		if binding == "" || seen[binding] {
			continue
		}
		seen[binding] = true
		checks = append(checks, pendingCompletionCheck(binding))
	}
	if len(checks) == 0 {
		checks = append(checks, pendingCompletionCheck("tests"))
	}
	return checks
}

func pendingCompletionCheck(name string) completionCheckTemplate {
	return completionCheckTemplate{
		Name: name, Command: "", Status: "pending",
		Detail: "TODO: set command and observed status to pass, fail, or skipped",
	}
}

// specgate delivery submit [work-ref] --file <completion.json>
//
// One-command delivery tail: report completion, run quality gates, trigger the
// delivery review, then fetch and print the per-criterion delivery status.
