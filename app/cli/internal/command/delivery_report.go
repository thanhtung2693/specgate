package command

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
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
		Short: "Record a coding-agent feedback event, or scaffold one with --init",
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
				if deps.Topology == config.ModeLocal {
					return runLocalDeliveryReportInit(cmd, args, deps, initPath, force)
				}
				return runDeliveryReportInit(cmd, args, deps, initPath, force)
			}
			if deps.Topology == config.ModeLocal {
				return incompatibleCommand(deps, "delivery.report", "direct delivery report submission is available only in Full mode; in Local mode, run `specgate change submit <ref> --file <completion.json>`")
			}
			// Read and validate the file before any network call so input errors
			// are caught early, without consuming a ResolveWorkRef round-trip.
			var body map[string]any
			if filePath != "" {
				var err error
				body, err = readJSONBodyFile(deps, "delivery.report", filePath)
				if err != nil {
					return err
				}
				if err := validateCompletionReport(deps, "delivery.report", body); err != nil {
					return err
				}
				if !skipEvidenceCheck {
					if err := verifyCompletionEvidence(deps, "delivery.report", body); err != nil {
						return err
					}
				}
			} else if !canPrompt(deps) {
				return &output.ExitError{Code: output.ExitUsage, Err: ErrInputRequired}
			} else {
				// Interactive: minimal completed event.
				agentName, err := deps.Prompter.Input("Coding agent name", "", func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("coding agent name is required")
					}
					return nil
				})
				if err != nil {
					return &output.ExitError{Code: output.ExitUsage, Err: err}
				}
				summary, err := deps.Prompter.Input("Feedback summary", "", func(s string) error {
					if s == "" {
						return fmt.Errorf("summary is required")
					}
					return nil
				})
				if err != nil {
					return &output.ExitError{Code: output.ExitUsage, Err: err}
				}
				body = map[string]any{
					"event_type": "coding_agent.completed",
					"summary":    summary,
					"agent":      map[string]any{"name": strings.TrimSpace(agentName)},
				}
				if err := validateCompletionReport(deps, "delivery.report", body); err != nil {
					return err
				}
			}
			// Capture checkout identity immediately after local input validation,
			// before resolving the work item or posting feedback over the network.
			attachGitReceipt(cmd.Context(), deps, body)

			ref, err := resolveRef(cmd, args, deps)
			if err != nil {
				return err
			}

			work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
			if err != nil {
				code := deps.Printer.Error("delivery.report", mapWorkRefError(ref, err))
				return &output.ExitError{Code: code, Err: err}
			}

			normalizeCompletionChecksForSubmit(body)
			normalizeFeedbackBody(body, work.ChangeRequestID)
			result, err := deps.Client.ReportFeedback(cmd.Context(), work.ChangeRequestID, body)
			if err != nil {
				return apiExitError(deps, "delivery.report", err)
			}

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("delivery.report", result)
				return nil
			}

			if id, ok := result["feedback_event_id"].(string); ok {
				fmt.Fprintf(deps.Stdout, "Feedback recorded: %s\n", id)
			} else {
				fmt.Fprintln(deps.Stdout, "Feedback recorded.")
			}
			return nil
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
// completion. The server verifies the completion event, receipt, and AC coverage.
func newDeliveryPeerReviewCmd(deps *Deps) *cobra.Command {
	var filePath, initPath string
	var force bool
	cmd := &cobra.Command{
		Use:   "peer-review [ref]",
		Short: "Record an independent agent review of the latest completion",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if initPath != "" {
				if deps.Topology == config.ModeLocal {
					return runLocalDeliveryPeerReviewInit(cmd, args, deps, initPath, force)
				}
				return runDeliveryPeerReviewInit(cmd, args, deps, initPath, force)
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
			if err := validateCompletionReport(deps, "delivery.peer-review", body); err != nil {
				return err
			}
			if deps.Topology == config.ModeLocal {
				if len(args) == 0 {
					return localExitError(deps, "delivery.peer-review", ErrInputRequired)
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
			}
			delete(body, "git_receipt")
			ref, err := resolveRef(cmd, args, deps)
			if err != nil {
				return err
			}
			work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
			if err != nil {
				return &output.ExitError{Code: deps.Printer.Error("delivery.peer-review", mapWorkRefError(ref, err)), Err: err}
			}
			normalizeFeedbackBody(body, work.ChangeRequestID)
			result, err := deps.Client.ReportFeedback(cmd.Context(), work.ChangeRequestID, body)
			if err != nil {
				return apiExitError(deps, "delivery.peer-review", err)
			}
			review, err := deps.Client.TriggerDeliveryReview(cmd.Context(), work.ChangeRequestID)
			if err != nil {
				return apiExitError(deps, "delivery.peer-review", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("delivery.peer-review", map[string]any{"report": result, "review": review})
				return nil
			}
			fmt.Fprintln(deps.Stdout, "Peer review recorded; delivery review rerun.")
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

func runLocalDeliveryPeerReviewInit(cmd *cobra.Command, args []string, deps *Deps, path string, force bool) error {
	if len(args) == 0 {
		return localExitError(deps, "delivery.peer-review", ErrInputRequired)
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

func runDeliveryPeerReviewInit(cmd *cobra.Command, args []string, deps *Deps, path string, force bool) error {
	ref, err := resolveRef(cmd, args, deps)
	if err != nil {
		return err
	}
	work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
	if err != nil {
		return &output.ExitError{Code: deps.Printer.Error("delivery.peer-review", mapWorkRefError(ref, err)), Err: err}
	}
	events, err := deps.Client.ListGovernanceFeedbackEvents(cmd.Context(), work.ChangeRequestID)
	if err != nil {
		return apiExitError(deps, "delivery.peer-review", err)
	}
	completion, payload, ok := latestCompletionFeedback(events)
	if !ok {
		return completionValidationError(deps, "delivery.peer-review", "no completion report found")
	}
	if completionAgentName(payload) == "" {
		return completionValidationError(deps, "delivery.peer-review", "latest completion agent.name is required")
	}
	receipt, _ := payload["git_receipt"].(map[string]any)
	if receipt == nil {
		return completionValidationError(deps, "delivery.peer-review", "latest completion has no git_receipt")
	}
	criteria, err := deps.Client.ListAcceptanceCriteria(cmd.Context(), work.ChangeRequestID)
	if err != nil {
		return apiExitError(deps, "delivery.peer-review", err)
	}
	if path == deliveryInitAuto {
		if err := ensureSpecgateWorkingDir(); err != nil {
			return completionValidationError(deps, "delivery.peer-review", err.Error())
		}
		_ = config.EnsureSpecgateDirGitignore(".specgate")
		path = filepath.Join(".specgate", "peer-review-"+work.ChangeRequestKey+".json")
	}
	body := map[string]any{
		"event_type":     "coding_agent.peer_reviewed",
		"summary":        "",
		"agent":          map[string]any{"name": ""},
		"peer_review_of": map[string]any{"completion_feedback_event_id": completion.ID, "git_receipt": receipt},
		"criteria": func() []map[string]any {
			out := make([]map[string]any, 0, len(criteria))
			for _, criterion := range criteria {
				out = append(out, map[string]any{
					"criterion_id": criterion.ID,
					"text":         criterion.Text,
					"claim":        "not_done",
					"evidence":     completionEvidenceTemplate{},
				})
			}
			return out
		}(),
	}
	data, _ := json.MarshalIndent(body, "", "  ")
	if err := writeDeliveryScaffold(path, append(data, '\n'), force); err != nil {
		return deliveryScaffoldWriteError(deps, "delivery.peer-review", path, err)
	}
	writePeerReviewScaffoldSuccess(deps, work.ChangeRequestKey, path, len(criteria))
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

func latestCompletionFeedback(events []client.GovernanceFeedbackEvent) (client.GovernanceFeedbackEvent, map[string]any, bool) {
	completed := make([]client.GovernanceFeedbackEvent, 0, len(events))
	for _, event := range events {
		if event.EventType == "coding_agent.completed" {
			completed = append(completed, event)
		}
	}
	if len(completed) == 0 {
		return client.GovernanceFeedbackEvent{}, nil, false
	}
	sort.Slice(completed, func(i, j int) bool {
		if completed[i].CreatedAt == completed[j].CreatedAt {
			return completed[i].ID > completed[j].ID
		}
		return completed[i].CreatedAt > completed[j].CreatedAt
	})
	payload := map[string]any{}
	if err := json.Unmarshal([]byte(completed[0].PayloadJSON), &payload); err != nil {
		return client.GovernanceFeedbackEvent{}, nil, false
	}
	return completed[0], payload, true
}

// completionTemplate is the completion.json scaffold written by
// `delivery report --init`. JSON has no comments, so example entries carry
// empty-string values that show the expected shape.
type completionTemplate struct {
	EventType       string                        `json:"event_type"`
	ChangeRequestID string                        `json:"change_request_id"`
	Summary         string                        `json:"summary"`
	Agent           map[string]string             `json:"agent"`
	ContextDigest   string                        `json:"context_digest,omitempty"`
	AffectedFiles   []string                      `json:"affected_files"`
	GitReceipt      gitReceipt                    `json:"git_receipt"`
	Checks          []completionCheckTemplate     `json:"checks"`
	Criteria        []completionCriterionTemplate `json:"criteria"`
}

func runLocalDeliveryReportInit(cmd *cobra.Command, args []string, deps *Deps, path string, force bool) error {
	if len(args) == 0 {
		return localExitError(deps, "delivery.report", ErrInputRequired)
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
	receipt := collectGitReceipt(cmd.Context(), deps.DeployRunner, deliveryWorkingDir(deps), nil)
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

// runDeliveryReportInit scaffolds a completion.json template with one criteria
// entry per acceptance criterion. Criterion IDs come from the work item's
// acceptance-criteria rows — the same IDs delivery review correlates against.
func runDeliveryReportInit(cmd *cobra.Command, args []string, deps *Deps, path string, force bool) error {
	receipt := collectGitReceipt(cmd.Context(), deps.DeployRunner, deliveryWorkingDir(deps), nil)
	if deps.Printer != nil && deps.Printer.Mode() != output.ModeJSON {
		for _, warning := range receipt.Warnings {
			fmt.Fprintf(deps.Stderr, "Warning: %s\n", warning)
		}
	}
	ref, err := resolveRef(cmd, args, deps)
	if err != nil {
		return err
	}
	work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
	if err != nil {
		code := deps.Printer.Error("delivery.report", mapWorkRefError(ref, err))
		return &output.ExitError{Code: code, Err: err}
	}
	// Bare `--init`: derive the per-work-item scaffold under the project-local
	// `.specgate/` working directory (gitignored) instead of the repo root,
	// so concurrent runs write to distinct files and the repo root stays clean.
	if path == deliveryInitAuto {
		if err := ensureSpecgateWorkingDir(); err != nil {
			payload := output.ErrorPayload{Code: "unavailable", Message: fmt.Sprintf("create .specgate: %v", err)}
			code := deps.Printer.Error("delivery.report", payload)
			return &output.ExitError{Code: code, Err: err}
		}
		// Drop a nested .gitignore in .specgate/ so the per-work-item scaffold we
		// just wrote is ignored and never leaks into the user's commits, while the
		// committed config stays tracked. Best-effort; never fail the report over it.
		_ = config.EnsureSpecgateDirGitignore(".specgate")
		path = filepath.Join(".specgate", "completion-"+work.ChangeRequestKey+".json")
	}
	criteria, err := deps.Client.ListAcceptanceCriteria(cmd.Context(), work.ChangeRequestID)
	if err != nil {
		return apiExitError(deps, "delivery.report", err)
	}

	tpl := completionTemplate{
		EventType:       "coding_agent.completed",
		ChangeRequestID: work.ChangeRequestID,
		Agent:           map[string]string{"name": ""},
		Summary:         "",
		AffectedFiles:   []string{},
		GitReceipt:      receipt,
		Checks:          completionTemplateChecksFromCriteria(criteria),
		Criteria:        make([]completionCriterionTemplate, 0, len(criteria)),
	}
	for _, c := range criteria {
		tpl.Criteria = append(tpl.Criteria, completionCriterionTemplate{
			CriterionID:         c.ID,
			Text:                c.Text,
			Claim:               "not_done",
			VerificationBinding: c.VerificationBinding,
		})
	}

	data, err := json.MarshalIndent(tpl, "", "  ")
	if err != nil {
		code := deps.Printer.Error("delivery.report", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
		return &output.ExitError{Code: code, Err: err}
	}
	if err := writeDeliveryScaffold(path, append(data, '\n'), force); err != nil {
		return deliveryScaffoldWriteError(deps, "delivery.report", path, err)
	}

	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success("delivery.report", map[string]any{"path": path, "criteria": len(tpl.Criteria)})
		return nil
	}
	fmt.Fprintf(deps.Stdout, "%s %s for %s (%d acceptance criteria).\n", styled(deps, output.StyleSuccess, "Wrote"), styled(deps, output.StyleAction, path), styled(deps, output.StyleBold, work.ChangeRequestKey), len(tpl.Criteria))
	if len(tpl.Criteria) == 0 {
		fmt.Fprintln(deps.Stdout, notice(deps, output.StyleWarning, "Notice", "No acceptance criteria found on the work item; add criteria entries manually if needed."))
	}
	fmt.Fprintln(deps.Stdout, label(deps, "Fill in:")+" summary, affected_files, checks, and per-criterion claim (satisfied|partial|not_done) with evidence {kind, path}.")
	fmt.Fprintln(deps.Stdout, nextStep(deps, "submit the receipt with", fmt.Sprintf("specgate delivery submit %s --file %s", work.ChangeRequestKey, path)))
	return nil
}

func completionTemplateChecksFromCriteria(criteria []client.AcceptanceCriterion) []completionCheckTemplate {
	bindings := make([]string, 0, len(criteria))
	for _, c := range criteria {
		bindings = append(bindings, c.VerificationBinding)
	}
	return completionTemplateChecks(bindings)
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
		checks = append(checks, completionCheckTemplate{Name: binding, Command: "", Status: "skipped", Detail: ""})
	}
	if len(checks) == 0 {
		checks = append(checks, completionCheckTemplate{Name: "tests", Command: "", Status: "skipped", Detail: ""})
	}
	return checks
}

// specgate delivery submit [work-ref] --file <completion.json>
//
// One-command delivery tail: report completion, run quality gates, trigger the
// delivery review, then fetch and print the per-criterion delivery status.
