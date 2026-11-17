package command

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

const customCompletionFileField = "custom_completion_file"

func newDeliverySubmitCmd(deps *Deps) *cobra.Command {
	return newDeliverySubmitCommand(deps, deliverySubmitCommandSpec{
		Use:       "submit [ref]",
		Short:     "Report completion, run gates, trigger review, and show the verdict",
		Long:      "Submit one completion file, run delivery gates, trigger review, and return\nthe combined verdict. Scaffold the file first with delivery report --init.",
		Example:   "  specgate delivery report LOCAL-123 --init\n  specgate delivery submit LOCAL-123 --file .specgate/completion-LOCAL-123.json --json",
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
			clearCheckObservations(body)
			clearCompletionEvidenceGrounding(body)
			if err := validateCompletionReport(deps, spec.Operation, body, runChecks); err != nil {
				return err
			}
			if !skipEvidenceCheck {
				if err := verifyCompletionEvidence(deps, spec.Operation, body); err != nil {
					return err
				}
			}
			if len(args) == 0 {
				return localExitError(deps, spec.Operation, ErrWorkRefRequired)
			}
			store, err := openLocalStore(deps)
			if err != nil {
				return localExitError(deps, spec.Operation, err)
			}
			defer store.Close()
			generation, err := store.CheckoutGeneration(cmd.Context())
			if err != nil {
				return localExitError(deps, spec.Operation, err)
			}
			// Replace any scaffolded or hand-authored receipt with the checkout
			// observed for this submission.
			attachGitReceipt(cmd.Context(), deps, body, generation)
			selection, err := localSelection(cmd.Context(), deps, store)
			if err != nil {
				return localExitError(deps, spec.Operation, err)
			}
			work, err := store.GetWork(cmd.Context(), selection.Workspace.ID, args[0])
			if err != nil {
				return localExitError(deps, spec.Operation, err)
			}
			delete(body, customCompletionFileField)
			draft, draftErr := filepath.Abs(effectiveFilePath)
			canonicalDraft, canonicalErr := filepath.Abs(filepath.Join(".specgate", "completion-"+work.Key+".json"))
			// Abs cleans ".." before resolving symlinks; the file was opened using
			// the original spelling, so a parent step cannot prove canonical origin.
			parentStep := strings.Contains("/"+filepath.ToSlash(effectiveFilePath)+"/", "/../")
			if draftErr != nil || canonicalErr != nil || draft != canonicalDraft || parentStep {
				// Record only the origin class. The file may be outside this checkout
				// or available only on the machine that submitted it.
				body[customCompletionFileField] = true
			}
			if work.Phase == "delivered" {
				return localExitError(deps, spec.Operation, local.ErrDeliveryApproved)
			}
			if digest, _ := body["context_digest"].(string); digest != work.ContextDigest {
				return completionValidationError(deps, spec.Operation, "completion context_digest does not match work")
			}
			if _, err := store.ContextPack(cmd.Context(), selection.Workspace.ID, args[0]); err != nil {
				return localExitError(deps, spec.Operation, err)
			}
			root, _ := config.FindProjectRoot(deps.WorkingDir)
			if err := store.ValidateVerificationReport(cmd.Context(), selection.Workspace.ID, args[0], root, body); err != nil {
				return localExitError(deps, spec.Operation, err)
			}
			contract, err := store.GetVerificationContract(cmd.Context(), selection.Workspace.ID, args[0])
			if err != nil {
				return localExitError(deps, spec.Operation, err)
			}
			if verificationNeedsObservedJUnit(contract) && !runChecks {
				return completionValidationError(deps, spec.Operation, "report-enabled verification requires --run-checks so SpecGate can observe the selected JUnit cases")
			}
			if runChecks {
				proceed, err := confirmCompletionChecks(deps, spec.Operation, body)
				if err != nil || !proceed {
					return err
				}
				if contract.Status == "pinned" {
					executeCompletionChecks(cmd.Context(), deps, body, &contract, generation, root)
				} else {
					executeCompletionChecks(cmd.Context(), deps, body, nil, generation)
				}
				if err := validateCompletionReport(deps, spec.Operation, body, false); err != nil {
					return err
				}
			}
			review, err := store.SubmitDelivery(cmd.Context(), selection.Workspace.ID, args[0], body, root)
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
				status.VerificationContract = contract.Status
				status.VerificationVersion = contract.Version
				status.VerificationDigest = contract.Digest
				status.CriterionEvidence, _ = local.ProjectAcceptanceEvidence(work.AcceptanceCriteria, report.Body, contract)
				status, err = enrichLocalStatusScope(cmd.Context(), store, selection.Workspace.ID, work, status)
				if err != nil {
					return localExitError(deps, spec.Operation, err)
				}
				status, err = augmentAcceptanceStatus(cmd, deps, store, selection.Workspace.ID, work.Key, status)
				if err != nil {
					return localExitError(deps, spec.Operation, err)
				}
				status.Risks = acceptanceRisks(status)
				// Acceptance readback can see a newer concurrent submission.
				// The invocation's file is known only for the report it wrote.
				if status.CompletionID == report.ID {
					retry := deliveryReportResubmit(status.Ref)
					switch status.NextCommand {
					case retry + " --file":
						status.NextCommand += " " + shellQuote(effectiveFilePath)
					case retry:
						if filePath != "" {
							status.NextCommand += " --file " + shellQuote(effectiveFilePath)
						}
					}
				}
				result = status
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success(spec.Operation, result)
			} else {
				fmt.Fprintf(deps.Stdout, "%s %s for %s\n", title(deps, "Delivery review"), styledStatus(deps, review.Verdict), styled(deps, output.StyleBold, args[0]))
				if status, ok := result.(changeStatusResult); ok && status.CompletionID == review.ReportID && status.NextActor == "implementing_agent" && filePath != "" {
					fmt.Fprintln(deps.Stdout, nextStep(deps, "Retry with the revised completion file", status.NextCommand))
				}
			}
			if review.Verdict != "passed" {
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

func verificationNeedsObservedJUnit(contract local.VerificationContract) bool {
	for _, check := range contract.Checks {
		if check.TestReport != nil {
			return true
		}
	}
	return false
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
		Short: "Show the recorded delivery review for a work item",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return localExitError(deps, "delivery.review", ErrWorkRefRequired)
			}
			return printLocalDeliveryStatus(cmd, deps, args[0], "delivery.review")
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
	cmd.Flags().String("review-id", "", "Exact reviewed delivery ID from status (required in Local mode)")
	cmd.Flags().String("basis-digest", "", "Exact enhanced Local acceptance basis")
	acceptanceSelectionFlags(cmd)
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
	cmd.Flags().String("review-id", "", "Exact reviewed delivery ID from status (required in Local mode)")
	cmd.Flags().String("basis-digest", "", "Exact enhanced Local acceptance basis")
	acceptanceSelectionFlags(cmd)
	return cmd
}

func runDeliveryDecision(cmd *cobra.Command, deps *Deps, args []string, op string, decision string, note string, prompt string) error {
	reviewID, _ := cmd.Flags().GetString("review-id")
	basisDigest, _ := cmd.Flags().GetString("basis-digest")
	if len(args) == 0 {
		return localExitError(deps, op, ErrWorkRefRequired)
	}
	if !deps.Yes {
		payload := output.ErrorPayload{Code: "confirmation_required", Message: fmt.Sprintf(prompt+" Re-run with --yes to record this human decision.", args[0])}
		code := deps.Printer.Error(op, payload)
		return &output.ExitError{Code: code}
	}
	if strings.TrimSpace(reviewID) == "" {
		return completionValidationError(deps, op, "--review-id is required in Local mode; read `specgate change status "+args[0]+" --json` and review its evidence before deciding")
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
	selected := strings.TrimSpace(basisDigest) != "" || cmd.Flags().Changed("checkpoint") || cmd.Flags().Changed("impact-base") || cmd.Flags().Changed("impact-target")
	var upgrade *local.StoreUpgrade
	if selected {
		upgrade, err = store.PendingStoreUpgrade(cmd.Context())
		if err != nil {
			return localExitError(deps, op, err)
		}
	}
	proceed, err := confirmStoreWrite(deps, op, fmt.Sprintf(prompt, args[0]), upgrade)
	if err != nil || !proceed {
		return err
	}
	basisOptions, _, err := acceptanceOptions(cmd, deps, store, selection.Workspace.ID, args[0])
	if err != nil {
		return localExitError(deps, op, err)
	}
	if err := store.DecideDeliveryWithBasis(cmd.Context(), selection.Workspace.ID, args[0], decision, selection.User.Username, note, reviewID, basisDigest, basisOptions); err != nil {
		return localExitError(deps, op, err)
	}
	return printLocalDeliveryStatusWithUpgrade(cmd, deps, args[0], op, upgrade)
}

// specgate delivery status [work-ref] [--detail]
func newDeliveryStatusCmd(deps *Deps) *cobra.Command {
	var detail bool

	cmd := &cobra.Command{
		Use:   "status [ref]",
		Short: "Show the authoritative delivery review verdict for a work item",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return localExitError(deps, "delivery.status", ErrWorkRefRequired)
			}
			return printLocalDeliveryStatus(cmd, deps, args[0], "delivery.status")
		},
	}

	acceptanceSelectionFlags(cmd)
	cmd.Flags().BoolVar(&detail, "detail", false, "Include per-criterion breakdown")
	return cmd
}
