package command

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/deploy"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

const deliveryHandoffSchemaVersion = "specgate.delivery-handoff/v1"

// deliveryHandoffBundle is a self-contained review request. It carries the work
// item, its recorded review, and the completion it is bound to, so a reviewer
// can read the evidence beside the diff without a workspace, a store, or a
// server. It is written into the repository and travels with the branch.
//
// ponytail: export and render only. A reviewer's decision still goes through
// the author's `change accept` / `request-changes`; ingesting a decision file
// needs merge and authorship rules this does not attempt.
type deliveryHandoffBundle struct {
	SchemaVersion string                 `json:"schema_version"`
	ExportedAt    string                 `json:"exported_at"`
	SourceMode    config.Mode            `json:"source_mode"`
	Work          local.WorkItem         `json:"work"`
	Review        local.DeliveryReview   `json:"review"`
	Report        local.DeliveryReport   `json:"report"`
	PeerReview    local.PeerReviewStatus `json:"peer_review"`
	Checksum      string                 `json:"checksum"`
}

func newDeliveryHandoffCmd(deps *Deps) *cobra.Command {
	handoff := &cobra.Command{
		Use:   "handoff",
		Short: "Export a delivery review request into the repository, or render one",
	}
	handoff.AddCommand(newDeliveryHandoffExportCmd(deps))
	handoff.AddCommand(newDeliveryHandoffShowCmd(deps))
	return handoff
}

// specgate delivery handoff export <work-ref> [--file path]
func newDeliveryHandoffExportCmd(deps *Deps) *cobra.Command {
	var filePath string
	cmd := &cobra.Command{
		Use:   "export <ref>",
		Short: "Write a committable review request for a reviewed work item",
		Long: "Write the work item, its delivery review, and the bound completion to a\n" +
			"single JSON file so a reviewer can render it beside the diff.\n\n" +
			"Examples:\n" +
			"  specgate delivery handoff export LOCAL-1a2b\n" +
			"  specgate delivery handoff export LOCAL-1a2b --file review.json --json",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if deps.Topology != config.ModeLocal {
				return incompatibleCommand(deps, "delivery.handoff.export", "delivery handoff export is available only in Local mode")
			}
			if strings.TrimSpace(filePath) != "" {
				if err := rejectPortableStateDestination(deps, filePath); err != nil {
					return completionValidationError(deps, "delivery.handoff.export", err.Error())
				}
			}
			store, err := openLocalStore(deps)
			if err != nil {
				return localExitError(deps, "delivery.handoff.export", err)
			}
			defer store.Close()
			selection, err := localSelection(cmd.Context(), deps, store)
			if err != nil {
				return localExitError(deps, "delivery.handoff.export", err)
			}
			work, err := store.GetWork(cmd.Context(), selection.Workspace.ID, args[0])
			if err != nil {
				return localExitError(deps, "delivery.handoff.export", err)
			}
			review, err := store.DeliveryStatus(cmd.Context(), selection.Workspace.ID, work.Key)
			if err == sql.ErrNoRows {
				payload := output.ErrorPayload{
					Code:    "not_found",
					Message: fmt.Sprintf("%s has no delivery review to hand off; scaffold one with `specgate delivery report %s --init`", work.Key, work.Key),
				}
				code := deps.Printer.Error("delivery.handoff.export", payload)
				return &output.ExitError{Code: code}
			}
			if err != nil {
				return localExitError(deps, "delivery.handoff.export", err)
			}
			report, err := store.DeliveryReportForReview(cmd.Context(), selection.Workspace.ID, review)
			if err != nil {
				return localExitError(deps, "delivery.handoff.export", err)
			}
			peer, err := store.PeerReviewStatus(cmd.Context(), selection.Workspace.ID, work.Key)
			if err != nil {
				return localExitError(deps, "delivery.handoff.export", err)
			}

			if strings.TrimSpace(filePath) == "" {
				if err := ensureSpecgateWorkingDir(); err != nil {
					return localExitError(deps, "delivery.handoff.export", err)
				}
				_ = config.EnsureSpecgateDirGitignore(".specgate")
				dir := filepath.Join(".specgate", "handoffs")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					return localExitError(deps, "delivery.handoff.export", err)
				}
				filePath = filepath.Join(dir, work.Key+".json")
			}

			exported, err := redactHandoffEvidence(report)
			if err != nil {
				return localExitError(deps, "delivery.handoff.export", err)
			}
			bundle := deliveryHandoffBundle{
				SchemaVersion: deliveryHandoffSchemaVersion,
				ExportedAt:    time.Now().UTC().Format(time.RFC3339),
				SourceMode:    config.ModeLocal,
				Work:          work,
				Review:        review,
				Report:        exported,
				PeerReview:    peer,
			}
			bundle.Checksum = deliveryHandoffChecksum(bundle)
			data, err := json.MarshalIndent(bundle, "", "  ")
			if err != nil {
				return localExitError(deps, "delivery.handoff.export", err)
			}
			if err := os.WriteFile(filePath, append(data, '\n'), 0o600); err != nil {
				return localExitError(deps, "delivery.handoff.export", err)
			}

			ignored := gitIgnoresPath(cmd.Context(), deps, filePath)
			if ignored && deps.Printer.Mode() != output.ModeJSON {
				fmt.Fprintf(deps.Stderr,
					"Warning: %s is ignored by Git, so a reviewer will not receive it.\nAdd `!handoffs/` and `!handoffs/**` to .specgate/.gitignore, or pass --file with a tracked path.\n",
					filePath)
			}
			result := map[string]any{
				"path": filePath, "ref": work.Key, "schema_version": bundle.SchemaVersion,
				"checksum": bundle.Checksum, "verdict": review.Verdict, "git_ignored": ignored,
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("delivery.handoff.export", result)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "Wrote %s for %s (%s)\n", filePath, work.Key, review.Verdict)
			fmt.Fprintf(deps.Stdout, "Commit it, then the reviewer runs: specgate delivery handoff show --file %s\n", filePath)
			return nil
		},
	}
	cmd.Flags().StringVar(&filePath, "file", "", "Destination file (default .specgate/handoffs/<ref>.json)")
	return cmd
}

// specgate delivery handoff show --file <path>
//
// Read-only, and deliberately store-free: a reviewer needs no workspace, no
// SQLite state, and no server to read a handoff a teammate committed.
func newDeliveryHandoffShowCmd(deps *Deps) *cobra.Command {
	var filePath string
	cmd := &cobra.Command{
		Use:   "show --file <path>",
		Short: "Render a committed review request without a workspace or server",
		Long: "Render the criteria, evidence, checks, and recorded decision from a\n" +
			"handoff file. Read-only; it records nothing.\n\n" +
			"Examples:\n" +
			"  specgate delivery handoff show --file .specgate/handoffs/LOCAL-1a2b.json\n" +
			"  specgate delivery handoff show --file review.json --json",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(filePath) == "" {
				return completionValidationError(deps, "delivery.handoff.show", "--file is required")
			}
			raw, err := os.ReadFile(filePath)
			if err != nil {
				payload := output.ErrorPayload{Code: "not_found", Message: fmt.Sprintf("read handoff: %v", err)}
				code := deps.Printer.Error("delivery.handoff.show", payload)
				return &output.ExitError{Code: code}
			}
			var bundle deliveryHandoffBundle
			if err := json.Unmarshal(raw, &bundle); err != nil {
				return completionValidationError(deps, "delivery.handoff.show", fmt.Sprintf("handoff is not readable JSON: %v", err))
			}
			if bundle.SchemaVersion != deliveryHandoffSchemaVersion {
				return completionValidationError(deps, "delivery.handoff.show",
					fmt.Sprintf("handoff schema_version %q is not supported by this CLI (expected %s); upgrade SpecGate", bundle.SchemaVersion, deliveryHandoffSchemaVersion))
			}
			recorded := bundle.Checksum
			bundle.Checksum = ""
			if computed := deliveryHandoffChecksum(bundle); computed != recorded {
				return completionValidationError(deps, "delivery.handoff.show",
					"handoff checksum does not match its contents; the file was edited after export — ask for a fresh `specgate delivery handoff export`")
			}
			bundle.Checksum = recorded

			// Re-derive rather than trust: a bundle exported by an older CLI can
			// carry a verdict its own evidence no longer supports.
			verdict, summary := local.DeliveryVerdict(bundle.Report.Body, bundle.Work.AcceptanceCriteria)
			disagrees := verdict != bundle.Review.Verdict
			criteria, checks := localReportEvidence(bundle.Work.AcceptanceCriteria, bundle.Report)
			status := deriveLocalChangeStatus(bundle.Work, &bundle.Review, &bundle.Report, bundle.PeerReview)
			status = applyCheckoutFreshness(cmd.Context(), deps, status, mapGitReceipt(bundle.Report.Body))

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("delivery.handoff.show", map[string]any{
					"ref": bundle.Work.Key, "title": bundle.Work.Title,
					"exported_at": bundle.ExportedAt, "checksum": bundle.Checksum,
					"recorded_verdict": bundle.Review.Verdict, "recomputed_verdict": verdict,
					"verdict_disagreement": disagrees, "recomputed_summary": summary,
					"criteria": criteria, "checks": checks, "status": status,
				})
			} else {
				printDeliveryHandoff(deps, bundle, criteria, checks, status, verdict, summary, disagrees)
			}
			if verdict != "passed" || disagrees {
				return &output.ExitError{Code: output.ExitGovernanceFailed}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&filePath, "file", "", "Handoff file to render (required)")
	return cmd
}

func printDeliveryHandoff(
	deps *Deps,
	bundle deliveryHandoffBundle,
	criteria []client.CriterionReview,
	checks []client.CheckResult,
	status changeStatusResult,
	verdict string,
	summary string,
	disagrees bool,
) {
	fmt.Fprintf(deps.Stdout, "%s %s — %s\n", label(deps, "Handoff:"), bundle.Work.Key, bundle.Work.Title)
	fmt.Fprintf(deps.Stdout, "%s %s by %s\n", label(deps, "Exported:"), bundle.ExportedAt, deliveryHandoffAgent(bundle))
	// Same shape as `verify`, and it keeps the criterion text in view — a
	// reviewer reading beside a diff needs the wording, not just the ID.
	for _, criterion := range criteria {
		fmt.Fprintf(deps.Stdout, "  [%s] %s — %s\n", criterion.Verdict, criterion.Text, criterion.Why)
	}
	for _, check := range checks {
		fmt.Fprintf(deps.Stdout, "  [%s] %s — %s\n", check.Status, check.Name, check.Detail)
	}
	fmt.Fprintf(deps.Stdout, "\n%s %s\n", label(deps, "Evidence:"), status.Evidence)
	fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Assurance:"), status.Assurance)
	fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Decision:"), status.Decision)
	fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Receipt:"), status.Receipt)
	fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Freshness:"), status.Freshness)
	fmt.Fprintf(deps.Stdout, "%s %s — %s\n", label(deps, "Verdict:"), verdict, summary)
	if disagrees {
		fmt.Fprintf(deps.Stderr,
			"Warning: the recorded verdict %q and this evidence disagree (recomputed %q). Ask for a fresh export from a current SpecGate CLI.\n",
			bundle.Review.Verdict, verdict)
	}
	fmt.Fprintf(deps.Stdout, "\nThis view is read-only. The author records the decision with:\n")
	fmt.Fprintf(deps.Stdout, "  specgate --yes change accept %s --review-id %s\n", bundle.Work.Key, bundle.Review.ID)
	fmt.Fprintf(deps.Stdout, "  specgate --yes change request-changes %s --review-id %s --note \"<reason>\"\n", bundle.Work.Key, bundle.Review.ID)
}

func deliveryHandoffAgent(bundle deliveryHandoffBundle) string {
	if name := completionAgentName(bundle.Report.Body); name != "" {
		return name
	}
	return "unknown agent"
}

// redactHandoffEvidence copies a completion report for export and drops each
// grounding excerpt. Grounding cites any path the agent read, including files
// outside the repository, and a handoff is committed — the digest and status
// still prove the citation was grounded without republishing file contents.
func redactHandoffEvidence(report local.DeliveryReport) (local.DeliveryReport, error) {
	encoded, err := json.Marshal(report.Body)
	if err != nil {
		return local.DeliveryReport{}, err
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		return local.DeliveryReport{}, err
	}
	criteria, _ := body["criteria"].([]any)
	for _, raw := range criteria {
		criterion, _ := raw.(map[string]any)
		evidence, _ := criterion["evidence"].(map[string]any)
		grounding, _ := evidence["grounding"].(map[string]any)
		if grounding != nil {
			delete(grounding, "excerpt")
		}
	}
	return local.DeliveryReport{ID: report.ID, Body: body}, nil
}

// deliveryHandoffChecksum digests the bundle with its own checksum field
// cleared, so recording the result never changes it.
func deliveryHandoffChecksum(bundle deliveryHandoffBundle) string {
	bundle.Checksum = ""
	return jsonChecksum(bundle)
}

// gitIgnoresPath reports whether Git would ignore path. A directory that is not
// a repository is not a warning: the author may be exporting outside a checkout.
func gitIgnoresPath(ctx context.Context, deps *Deps, path string) bool {
	runner := deps.DeployRunner
	if runner == nil {
		runner = deploy.ExecRunner{}
	}
	dir := filepath.Dir(path)
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	if _, err := gitOutput(ctx, runner, dir, "rev-parse", "--show-toplevel"); err != nil {
		return false
	}
	_, err := gitOutput(ctx, runner, dir, "check-ignore", "-q", filepath.Base(path))
	return err == nil
}
