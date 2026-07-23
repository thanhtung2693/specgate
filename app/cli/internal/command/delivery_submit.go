package command

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func newDeliverySubmitCmd(deps *Deps) *cobra.Command {
	return newDeliverySubmitCommand(deps, deliverySubmitCommandSpec{
		Use:       "submit [ref]",
		Short:     "Report completion, run gates, trigger review, and show the verdict",
		Long:      "Submit one completion file, run delivery gates, trigger review, and return\nthe combined verdict. Scaffold the file first with delivery report --init.",
		Example:   "  specgate delivery report CR-123 --init\n  specgate delivery submit CR-123 --file .specgate/completion-CR-123.json --json",
		Operation: "delivery.submit",
	})
}

type deliverySubmitCommandSpec struct {
	Use                string
	Short              string
	Long               string
	Example            string
	Operation          string
	DefaultFileFromRef bool
	CompactJSON        bool
}

func newDeliverySubmitCommand(deps *Deps, spec deliverySubmitCommandSpec) *cobra.Command {
	var (
		filePath          string
		skipEvidenceCheck bool
		runChecks         bool
	)
	argsPolicy := cobra.MaximumNArgs(1)
	if spec.DefaultFileFromRef {
		argsPolicy = cobra.ExactArgs(1)
	}

	cmd := &cobra.Command{
		Use:     spec.Use,
		Short:   spec.Short,
		Long:    spec.Long,
		Example: spec.Example,
		Args:    argsPolicy,
		RunE: func(cmd *cobra.Command, args []string) error {
			effectiveFilePath := filePath
			if effectiveFilePath == "" {
				if spec.DefaultFileFromRef {
					if !safeCompletionRef(args[0]) {
						payload := output.ErrorPayload{Code: "validation", Message: "--file is required when the ref is not file-safe"}
						code := deps.Printer.Error(spec.Operation, payload)
						return &output.ExitError{Code: code}
					}
					effectiveFilePath = filepath.Join(".specgate", "completion-"+args[0]+".json")
				} else {
					payload := output.ErrorPayload{Code: "validation", Message: "--file is required (scaffold one with `specgate delivery report --init`)"}
					code := deps.Printer.Error(spec.Operation, payload)
					return &output.ExitError{Code: code}
				}
			}
			body, err := readJSONBodyFile(deps, spec.Operation, effectiveFilePath)
			if err != nil {
				return err
			}
			eventType, _ := body["event_type"].(string)
			if strings.TrimSpace(eventType) != "coding_agent.completed" {
				return completionValidationError(deps, spec.Operation, "event_type must be coding_agent.completed")
			}
			if err := validateCompletionReport(deps, spec.Operation, body); err != nil {
				return err
			}
			if !skipEvidenceCheck {
				if err := verifyCompletionEvidence(deps, spec.Operation, body); err != nil {
					return err
				}
			}
			// Replace any scaffolded or hand-authored receipt with the checkout
			// observed for this submission in both modes.
			attachGitReceipt(cmd.Context(), deps, body)
			if deps.Topology == config.ModeLocal {
				if len(args) == 0 {
					return localExitError(deps, spec.Operation, ErrInputRequired)
				}
				if runChecks {
					proceed, err := confirmCompletionChecks(deps, spec.Operation, body)
					if err != nil || !proceed {
						return err
					}
					executeCompletionChecks(cmd.Context(), deps, body)
				}
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, spec.Operation, err)
				}
				defer store.Close()
				selection, err := localSelection(cmd.Context(), deps, store)
				if err != nil {
					return localExitError(deps, spec.Operation, err)
				}
				review, err := store.SubmitDelivery(cmd.Context(), selection.Workspace.ID, args[0], body)
				if err != nil {
					return localExitError(deps, spec.Operation, err)
				}
				var result any = map[string]any{"review": localDeliveryReviewView(review)}
				if spec.CompactJSON {
					work, workErr := store.GetWork(cmd.Context(), selection.Workspace.ID, args[0])
					if workErr != nil {
						return localExitError(deps, spec.Operation, workErr)
					}
					report, reportErr := store.DeliveryReportForReview(cmd.Context(), selection.Workspace.ID, review)
					if reportErr != nil {
						return localExitError(deps, spec.Operation, reportErr)
					}
					peer, peerErr := store.PeerReviewStatus(cmd.Context(), selection.Workspace.ID, work.Key)
					if peerErr != nil {
						return localExitError(deps, spec.Operation, peerErr)
					}
					status := deriveLocalChangeStatus(work, &review, &report, peer)
					result = applyCheckoutFreshness(cmd.Context(), deps, status, mapGitReceipt(report.Body))
				}
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success(spec.Operation, result)
				} else {
					fmt.Fprintf(deps.Stdout, "%s %s for %s\n", title(deps, "Delivery review"), styledStatus(deps, review.Verdict), styled(deps, output.StyleBold, args[0]))
				}
				if review.Verdict != "passed" {
					return &output.ExitError{Code: output.ExitGovernanceFailed}
				}
				return nil
			}
			ref, err := resolveRef(cmd, args, deps)
			if err != nil {
				return err
			}
			work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
			if err != nil {
				code := deps.Printer.Error(spec.Operation, mapWorkRefError(ref, err))
				return &output.ExitError{Code: code, Err: err}
			}

			// After the ref resolves — check commands can take minutes, and a
			// typo'd ref should not cost a test run.
			if runChecks {
				proceed, err := confirmCompletionChecks(deps, spec.Operation, body)
				if err != nil || !proceed {
					return err
				}
				executeCompletionChecks(cmd.Context(), deps, body)
			}
			normalizeCompletionChecksForSubmit(body)

			human := deps.Printer.Mode() != output.ModeJSON
			step := func(n int, msg string) {
				if human {
					fmt.Fprintf(deps.Stderr, "%d/4 %s\n", n, msg)
				}
			}
			fail := func(stage string, err error) error {
				payload := mapAPIError(err)
				if payload.Details == nil {
					payload.Details = map[string]any{}
				}
				payload.Details["stage"] = stage
				if human {
					fmt.Fprintf(deps.Stderr, "%s %q failed: %v\n", deps.Printer.StyleStderr("Stage", output.StyleDanger), stage, err)
				}
				code := deps.Printer.Error(spec.Operation, payload)
				return &output.ExitError{Code: code, Err: err}
			}

			normalizeFeedbackBody(body, work.ChangeRequestID)
			report, err := deps.Client.ReportFeedback(cmd.Context(), work.ChangeRequestID, body)
			if err != nil {
				return fail("report", err)
			}
			step(1, "Completion report recorded")

			gates, err := deps.Client.RunLLMGates(cmd.Context(), work.ChangeRequestID)
			if err != nil {
				return fail("gates", err)
			}
			step(2, "Quality gates triggered")

			review, err := deps.Client.TriggerDeliveryReview(cmd.Context(), work.ChangeRequestID)
			if err != nil {
				return fail("review", err)
			}
			step(3, "Delivery review triggered")

			ds, err := deps.Client.DeliveryStatus(cmd.Context(), work.ChangeRequestID, true)
			if err != nil {
				return fail("status", err)
			}
			step(4, "Delivery status fetched")

			if deps.Printer.Mode() == output.ModeJSON {
				if spec.CompactJSON {
					status := deriveFullChangeStatus(work, ds)
					status = applyCheckoutFreshness(cmd.Context(), deps, status, clientGitReceipt(ds.GitReceipt))
					deps.Printer.Success(spec.Operation, status)
				} else {
					deps.Printer.Success(spec.Operation, map[string]any{
						"report": report,
						"gates":  gates,
						"review": review,
						"status": ds,
					})
				}
			} else {
				fmt.Fprintln(deps.Stdout)
				printDeliveryStatus(deps, ds, true)
				printDeliveryDecisionCommands(deps, work.ChangeRequestKey, ds, false)
			}
			if !ds.Found || ds.Verdict != "pass" {
				return &output.ExitError{Code: output.ExitGovernanceFailed}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "JSON file containing the completion report body (scaffold with 'delivery report --init')")
	cmd.Flags().BoolVar(&skipEvidenceCheck, "skip-evidence-check", false, "Skip verifying that cited evidence paths exist in the working tree")
	cmd.Flags().BoolVar(&runChecks, "run-checks", false, "Re-execute each non-skipped checks[].command locally with sh -c and submit observed results")
	return cmd
}

func safeCompletionRef(ref string) bool {
	if ref == "" {
		return false
	}
	for _, r := range ref {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// specgate delivery review [work-ref]
func newDeliveryReviewCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "review [ref]",
		Short: "Trigger the delivery review for a work item",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if deps.Topology == config.ModeLocal {
				if len(args) == 0 {
					return localExitError(deps, "delivery.review", ErrInputRequired)
				}
				return printLocalDeliveryStatus(cmd, deps, args[0], "delivery.review")
			}
			ref, err := resolveRef(cmd, args, deps)
			if err != nil {
				return err
			}

			work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
			if err != nil {
				code := deps.Printer.Error("delivery.review", mapWorkRefError(ref, err))
				return &output.ExitError{Code: code, Err: err}
			}

			proceed, err := requireConfirm(deps,
				fmt.Sprintf("Trigger delivery review for %s?", work.ChangeRequestKey))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			result, err := deps.Client.TriggerDeliveryReview(cmd.Context(), work.ChangeRequestID)
			if err != nil {
				return apiExitError(deps, "delivery.review", err)
			}

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("delivery.review", result)
				return nil
			}

			if status, ok := result["status"].(string); ok {
				fmt.Fprintf(deps.Stdout, "Delivery review %s for %s\n", status, work.ChangeRequestKey)
			} else {
				fmt.Fprintf(deps.Stdout, "Delivery review triggered for %s\n", work.ChangeRequestKey)
			}
			fmt.Fprintf(deps.Stdout, "%s\n", styled(deps, output.StyleMuted, selfSelectedChecksNote))
			return nil
		},
	}
}

func newDeliveryApproveCmd(deps *Deps) *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:   "approve [ref]",
		Short: "Approve a delivery as a human reviewer",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeliveryDecision(cmd, deps, args, "delivery.approve", "approve", note, "Approve delivery for %s?")
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "Optional reviewer note recorded with the decision")
	return cmd
}

func newDeliveryRejectCmd(deps *Deps) *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:   "reject [ref]",
		Short: "Reject a delivery as a human reviewer",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeliveryDecision(cmd, deps, args, "delivery.reject", "reject", note, "Reject delivery for %s?")
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "Optional reviewer note recorded with the decision")
	return cmd
}

func runDeliveryDecision(cmd *cobra.Command, deps *Deps, args []string, op string, decision string, note string, prompt string) error {
	if deps.Topology == config.ModeLocal {
		if len(args) == 0 {
			return localExitError(deps, op, ErrInputRequired)
		}
		if !deps.Yes {
			payload := output.ErrorPayload{Code: "confirmation_required", Message: fmt.Sprintf(prompt+" Re-run with --yes to record this human decision.", args[0])}
			code := deps.Printer.Error(op, payload)
			return &output.ExitError{Code: code}
		}
		store, err := openLocalStore(deps)
		if err != nil {
			return localExitError(deps, op, err)
		}
		defer store.Close()
		selection, err := localSelection(cmd.Context(), deps, store)
		if err != nil {
			return localExitError(deps, op, err)
		}
		if err := store.DecideDelivery(cmd.Context(), selection.Workspace.ID, args[0], decision, selection.User.Username, note); err != nil {
			return localExitError(deps, op, err)
		}
		return printLocalDeliveryStatus(cmd, deps, args[0], op)
	}
	ref, err := resolveRef(cmd, args, deps)
	if err != nil {
		return err
	}
	work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
	if err != nil {
		code := deps.Printer.Error(op, mapWorkRefError(ref, err))
		return &output.ExitError{Code: code, Err: err}
	}
	proceed, err := requireConfirm(deps, fmt.Sprintf(prompt, work.ChangeRequestKey))
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}
	result, err := deps.Client.DecideDelivery(cmd.Context(), work.ChangeRequestID, client.DeliveryDecisionInput{
		Decision: decision,
		Actor:    currentActor(deps),
		Note:     note,
	})
	if err != nil {
		return apiExitError(deps, op, err)
	}
	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success(op, result)
		return nil
	}
	if strings.TrimSpace(result.Summary) != "" {
		fmt.Fprintln(deps.Stdout, result.Summary)
		return nil
	}
	verb := "approved"
	if decision == "reject" {
		verb = "rejected"
	}
	fmt.Fprintf(deps.Stdout, "Delivery %s for %s\n", verb, work.ChangeRequestKey)
	return nil
}

// specgate delivery status [work-ref] [--detail]
func newDeliveryStatusCmd(deps *Deps) *cobra.Command {
	var detail bool

	cmd := &cobra.Command{
		Use:   "status [ref]",
		Short: "Show the authoritative delivery review verdict for a work item",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if deps.Topology == config.ModeLocal {
				if len(args) == 0 {
					return localExitError(deps, "delivery.status", ErrInputRequired)
				}
				return printLocalDeliveryStatus(cmd, deps, args[0], "delivery.status")
			}
			ref, err := resolveRef(cmd, args, deps)
			if err != nil {
				return err
			}

			work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
			if err != nil {
				code := deps.Printer.Error("delivery.status", mapWorkRefError(ref, err))
				return &output.ExitError{Code: code, Err: err}
			}

			ds, err := deps.Client.DeliveryStatus(cmd.Context(), work.ChangeRequestID, detail)
			if err != nil {
				return apiExitError(deps, "delivery.status", err)
			}

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("delivery.status", ds)
				return nil
			}

			printDeliveryStatus(deps, ds, detail)
			if detail {
				printDeliveryDecisionCommands(deps, work.ChangeRequestKey, ds, false)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&detail, "detail", false, "Include per-criterion breakdown")
	return cmd
}
