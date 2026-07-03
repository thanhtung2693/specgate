package command

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func registerDeliveryCommands(root *cobra.Command, deps *Deps) {
	del := &cobra.Command{
		Use:   "delivery",
		Short: "Manage delivery reports and reviews",
	}
	del.AddCommand(newDeliveryReportCmd(deps))
	del.AddCommand(newDeliverySubmitCmd(deps))
	del.AddCommand(newDeliveryReviewCmd(deps))
	del.AddCommand(newDeliveryStatusCmd(deps))
	root.AddCommand(del)
}

// readJSONBodyFile reads and parses a JSON object file before any network
// call, emitting a usage error envelope on failure.
func readJSONBodyFile(deps *Deps, command, filePath string) (map[string]any, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		payload := output.ErrorPayload{Code: "usage", Message: fmt.Sprintf("read file %s: %v", filePath, err)}
		code := deps.Printer.Error(command, payload)
		return nil, &output.ExitError{Code: code, Err: err}
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		payload := output.ErrorPayload{Code: "usage", Message: fmt.Sprintf("parse JSON from %s: %v", filePath, err)}
		code := deps.Printer.Error(command, payload)
		return nil, &output.ExitError{Code: code, Err: err}
	}
	if body == nil {
		// "null" unmarshals into a nil map without error; later writes would panic.
		payload := output.ErrorPayload{Code: "usage", Message: fmt.Sprintf("%s must contain a JSON object", filePath)}
		code := deps.Printer.Error(command, payload)
		return nil, &output.ExitError{Code: code}
	}
	return body, nil
}

// normalizeFeedbackBody fills the envelope fields the server requires but the
// caller already expressed elsewhere: the work ref on the command line becomes
// change_request_id, and severity defaults to "info". Explicit values win.
func normalizeFeedbackBody(body map[string]any, changeRequestID string) {
	if v, ok := body["change_request_id"].(string); !ok || strings.TrimSpace(v) == "" {
		body["change_request_id"] = changeRequestID
	}
	if v, ok := body["severity"].(string); !ok || strings.TrimSpace(v) == "" {
		body["severity"] = "info"
	}
}

// specgate delivery report [work-ref] [--file <evidence.json>] [--init[=path] [--force]]
func newDeliveryReportCmd(deps *Deps) *cobra.Command {
	var (
		filePath string
		initPath string
		force    bool
	)

	cmd := &cobra.Command{
		Use:   "report [ref]",
		Short: "Record a coding-agent feedback event, or scaffold one with --init",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if initPath != "" {
				return runDeliveryReportInit(cmd, args, deps, initPath, force)
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
			} else if deps.NoInput {
				return &output.ExitError{Code: output.ExitUsage, Err: ErrInputRequired}
			} else {
				// Interactive: minimal completed event.
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
				}
			}

			ref, err := resolveRef(cmd, args, deps)
			if err != nil {
				return err
			}

			work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
			if err != nil {
				code := deps.Printer.Error("delivery.report", mapWorkRefError("delivery.report", ref, err))
				return &output.ExitError{Code: code, Err: err}
			}

			normalizeFeedbackBody(body, work.ChangeRequestID)
			result, err := deps.Client.ReportFeedback(cmd.Context(), work.ChangeRequestID, body)
			if err != nil {
				code := deps.Printer.Error("delivery.report", mapAPIError("delivery.report", err))
				return &output.ExitError{Code: code, Err: err}
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
	cmd.Flags().StringVar(&initPath, "init", "", "Write a completion.json template for the work item instead of reporting (default path completion.json)")
	cmd.Flags().Lookup("init").NoOptDefVal = "completion.json"
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing file with --init")
	cmd.MarkFlagsMutuallyExclusive("file", "init")
	return cmd
}

// completionTemplate is the completion.json scaffold written by
// `delivery report --init`. JSON has no comments, so example entries carry
// empty-string values that show the expected shape.
type completionTemplate struct {
	EventType     string                        `json:"event_type"`
	Summary       string                        `json:"summary"`
	AffectedFiles []string                      `json:"affected_files"`
	Checks        []completionCheckTemplate     `json:"checks"`
	Criteria      []completionCriterionTemplate `json:"criteria"`
}

type completionCheckTemplate struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type completionCriterionTemplate struct {
	CriterionID string                     `json:"criterion_id"`
	Text        string                     `json:"text"`
	Claim       string                     `json:"claim"` // satisfied | partial | not_done
	Evidence    completionEvidenceTemplate `json:"evidence"`
}

type completionEvidenceTemplate struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

// runDeliveryReportInit scaffolds a completion.json template with one criteria
// entry per acceptance criterion. Criterion IDs come from the work item's
// acceptance-criteria rows — the same IDs delivery review correlates against.
func runDeliveryReportInit(cmd *cobra.Command, args []string, deps *Deps, path string, force bool) error {
	ref, err := resolveRef(cmd, args, deps)
	if err != nil {
		return err
	}
	work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
	if err != nil {
		code := deps.Printer.Error("delivery.report", mapWorkRefError("delivery.report", ref, err))
		return &output.ExitError{Code: code, Err: err}
	}
	criteria, err := deps.Client.ListAcceptanceCriteria(cmd.Context(), work.ChangeRequestID)
	if err != nil {
		code := deps.Printer.Error("delivery.report", mapAPIError("delivery.report", err))
		return &output.ExitError{Code: code, Err: err}
	}

	if !force {
		if _, err := os.Stat(path); err == nil {
			payload := output.ErrorPayload{Code: "validation", Message: fmt.Sprintf("%s already exists; pass --force to overwrite", path)}
			code := deps.Printer.Error("delivery.report", payload)
			return &output.ExitError{Code: code}
		}
	}

	tpl := completionTemplate{
		EventType:     "coding_agent.completed",
		Summary:       "",
		AffectedFiles: []string{""},
		Checks:        []completionCheckTemplate{{}},
		Criteria:      make([]completionCriterionTemplate, 0, len(criteria)),
	}
	for _, c := range criteria {
		tpl.Criteria = append(tpl.Criteria, completionCriterionTemplate{
			CriterionID: c.ID,
			Text:        c.Text,
			Claim:       "satisfied",
		})
	}

	data, err := json.MarshalIndent(tpl, "", "  ")
	if err != nil {
		code := deps.Printer.Error("delivery.report", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
		return &output.ExitError{Code: code, Err: err}
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		payload := output.ErrorPayload{Code: "unavailable", Message: fmt.Sprintf("write %s: %v", path, err)}
		code := deps.Printer.Error("delivery.report", payload)
		return &output.ExitError{Code: code, Err: err}
	}

	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success("delivery.report", map[string]any{"path": path, "criteria": len(tpl.Criteria)})
		return nil
	}
	fmt.Fprintf(deps.Stdout, "Wrote %s for %s (%d acceptance criteria).\n", path, work.ChangeRequestKey, len(tpl.Criteria))
	if len(tpl.Criteria) == 0 {
		fmt.Fprintln(deps.Stdout, "No acceptance criteria found on the work item; add criteria entries manually if needed.")
	}
	fmt.Fprintln(deps.Stdout, "Fill in: summary, affected_files, checks, and per-criterion claim (satisfied|partial|not_done) with evidence {kind, path}.")
	fmt.Fprintf(deps.Stdout, "Then run: specgate delivery submit %s --file %s\n", work.ChangeRequestKey, path)
	return nil
}

// specgate delivery submit [work-ref] --file <completion.json>
//
// One-command delivery tail: report completion, run LLM gates, trigger the
// delivery review, then fetch and print the per-criterion delivery status.
func newDeliverySubmitCmd(deps *Deps) *cobra.Command {
	var filePath string

	cmd := &cobra.Command{
		Use:   "submit [ref]",
		Short: "Report completion, run gates, trigger review, and show the verdict",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if filePath == "" {
				payload := output.ErrorPayload{Code: "validation", Message: "--file is required (scaffold one with `specgate delivery report --init`)"}
				code := deps.Printer.Error("delivery.submit", payload)
				return &output.ExitError{Code: code}
			}
			body, err := readJSONBodyFile(deps, "delivery.submit", filePath)
			if err != nil {
				return err
			}

			ref, err := resolveRef(cmd, args, deps)
			if err != nil {
				return err
			}
			work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
			if err != nil {
				code := deps.Printer.Error("delivery.submit", mapWorkRefError("delivery.submit", ref, err))
				return &output.ExitError{Code: code, Err: err}
			}

			human := deps.Printer.Mode() != output.ModeJSON
			step := func(n int, msg string) {
				if human {
					fmt.Fprintf(deps.Stdout, "%d/4 %s\n", n, msg)
				}
			}
			fail := func(stage string, err error) error {
				payload := mapAPIError("delivery.submit", err)
				if payload.Details == nil {
					payload.Details = map[string]any{}
				}
				payload.Details["stage"] = stage
				if human {
					fmt.Fprintf(deps.Stdout, "Stage %q failed: %v\n", stage, err)
				}
				code := deps.Printer.Error("delivery.submit", payload)
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
				deps.Printer.Success("delivery.submit", map[string]any{
					"report": report,
					"gates":  gates,
					"review": review,
					"status": ds,
				})
				return nil
			}
			fmt.Fprintln(deps.Stdout)
			printDeliveryStatus(deps, ds, true)
			return nil
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "JSON file containing the completion report body (scaffold with 'delivery report --init')")
	return cmd
}

// specgate delivery review [work-ref]
func newDeliveryReviewCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "review [ref]",
		Short: "Trigger the delivery review for a work item",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := resolveRef(cmd, args, deps)
			if err != nil {
				return err
			}

			work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
			if err != nil {
				code := deps.Printer.Error("delivery.review", mapWorkRefError("delivery.review", ref, err))
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
				code := deps.Printer.Error("delivery.review", mapAPIError("delivery.review", err))
				return &output.ExitError{Code: code, Err: err}
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
			return nil
		},
	}
}

// specgate delivery status [work-ref] [--detail]
func newDeliveryStatusCmd(deps *Deps) *cobra.Command {
	var detail bool

	cmd := &cobra.Command{
		Use:   "status [ref]",
		Short: "Show the latest delivery review verdict for a work item",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := resolveRef(cmd, args, deps)
			if err != nil {
				return err
			}

			work, err := deps.Client.ResolveWorkRef(cmd.Context(), ref)
			if err != nil {
				code := deps.Printer.Error("delivery.status", mapWorkRefError("delivery.status", ref, err))
				return &output.ExitError{Code: code, Err: err}
			}

			ds, err := deps.Client.DeliveryStatus(cmd.Context(), work.ChangeRequestID, detail)
			if err != nil {
				code := deps.Printer.Error("delivery.status", mapAPIError("delivery.status", err))
				return &output.ExitError{Code: code, Err: err}
			}

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("delivery.status", ds)
				return nil
			}

			printDeliveryStatus(deps, ds, detail)
			return nil
		},
	}

	cmd.Flags().BoolVar(&detail, "detail", false, "Include per-criterion breakdown")
	return cmd
}

// printDeliveryStatus renders a delivery status result for human/plain modes.
// Shared by `delivery status` and `delivery submit`.
func printDeliveryStatus(deps *Deps, ds *client.DeliveryStatusResult, detail bool) {
	if !ds.Found {
		fmt.Fprintln(deps.Stdout, "No delivery review found for this work item.")
		return
	}

	if humanVisuals(deps) {
		printDeliveryStatusDashboard(deps, ds, detail)
		return
	}
	fmt.Fprintf(deps.Stdout, "%s  %s\n", styled(deps, output.StyleMuted, "Verdict:"), styledStatus(deps, ds.Verdict))
	if ds.Hint != "" {
		fmt.Fprintf(deps.Stdout, "%s     %s\n", styled(deps, output.StyleMuted, "Hint:"), ds.Hint)
	}
	if ds.ReviewedAt != "" {
		fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleMuted, "Reviewed:"), ds.ReviewedAt)
	}
	if ds.OutstandingMD != "" {
		fmt.Fprintf(deps.Stdout, "\n%s\n", ds.OutstandingMD)
	}
	if detail && len(ds.PerCriterion) > 0 {
		fmt.Fprintln(deps.Stdout, "\nPer criterion:")
		for _, c := range ds.PerCriterion {
			label := c.CriterionID
			if label == "" {
				label = c.Text
			}
			fmt.Fprintf(deps.Stdout, "  %-20s  %s\n", label, styledStatus(deps, c.Verdict))
			if c.Why != "" {
				fmt.Fprintf(deps.Stdout, "    %s\n", styled(deps, output.StyleMuted, c.Why))
			}
		}
	}
}

func printDeliveryStatusDashboard(deps *Deps, ds *client.DeliveryStatusResult, detail bool) {
	fmt.Fprintln(deps.Stdout, styled(deps, output.StyleInfo, "Delivery Review"))
	fmt.Fprintln(deps.Stdout, visualRule(deps))
	fmt.Fprintf(deps.Stdout, "%s %s %s\n",
		statusIcon(deps, ds.Verdict),
		styled(deps, output.StyleMuted, "Verdict:"),
		styledStatus(deps, ds.Verdict))
	if ds.Hint != "" {
		fmt.Fprintf(deps.Stdout, "  %s %s\n", styled(deps, output.StyleMuted, "Hint:"), ds.Hint)
	}
	if ds.ReviewedAt != "" {
		fmt.Fprintf(deps.Stdout, "  %s %s\n", styled(deps, output.StyleMuted, "Reviewed:"), ds.ReviewedAt)
	}
	if ds.OutstandingMD != "" {
		fmt.Fprintf(deps.Stdout, "\n%s\n", ds.OutstandingMD)
	}
	if detail && len(ds.PerCriterion) > 0 {
		printCriterionDashboard(deps, ds.PerCriterion)
	}
}

func printCriterionDashboard(deps *Deps, criteria []client.CriterionReview) {
	met := 0
	for _, c := range criteria {
		if passingStatus(c.Verdict) {
			met++
		}
	}
	fmt.Fprintf(deps.Stdout, "\n%s %s %s %d/%d met (%d%%)\n",
		coloredBullet(deps, output.StyleSuccess),
		styled(deps, output.StyleMuted, "Criteria:"),
		progressBar(deps, met, len(criteria), 18),
		met,
		len(criteria),
		percent(met, len(criteria)))
	fmt.Fprintln(deps.Stdout)
	for _, c := range criteria {
		label := c.CriterionID
		if label == "" {
			label = c.Text
		}
		fmt.Fprintf(deps.Stdout, "  %s %-20s %s\n",
			criterionBox(deps, c.Verdict),
			label,
			styledStatus(deps, c.Verdict))
		if c.Why != "" {
			fmt.Fprintf(deps.Stdout, "    %s\n", styled(deps, output.StyleMuted, c.Why))
		}
	}
}
