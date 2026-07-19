package command

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/deploy"
	"github.com/specgate/specgate/app/cli/internal/fsutil"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func registerDeliveryCommands(root *cobra.Command, deps *Deps) {
	del := &cobra.Command{
		Use:   "delivery",
		Short: "Manage delivery reports and reviews",
	}
	del.AddCommand(newDeliveryReportCmd(deps))
	del.AddCommand(newDeliveryPeerReviewCmd(deps))
	del.AddCommand(newDeliverySubmitCmd(deps))
	del.AddCommand(newDeliveryReviewCmd(deps))
	del.AddCommand(newDeliveryApproveCmd(deps))
	del.AddCommand(newDeliveryRejectCmd(deps))
	del.AddCommand(newDeliveryStatusCmd(deps))
	root.AddCommand(del)
}

func ensureSpecgateWorkingDir() error {
	info, err := os.Lstat(".specgate")
	if os.IsNotExist(err) {
		if err := os.Mkdir(".specgate", 0o700); err != nil {
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf(".specgate must be a real directory")
	}
	return os.Chmod(".specgate", 0o700)
}

type deliveryScaffoldUsageError struct {
	message string
}

func (e *deliveryScaffoldUsageError) Error() string {
	return e.message
}

func writeDeliveryScaffold(path string, data []byte, force bool) error {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return &deliveryScaffoldUsageError{message: "target exists but is not a regular file; refusing to overwrite it"}
		}
		if !force {
			return &deliveryScaffoldUsageError{message: "target already exists; pass --force to overwrite"}
		}
	case !os.IsNotExist(err):
		return err
	}

	if !force {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if os.IsExist(err) {
			return &deliveryScaffoldUsageError{message: "target already exists; pass --force to overwrite"}
		}
		if err != nil {
			return err
		}
		if _, err := file.Write(data); err != nil {
			_ = file.Close()
			_ = os.Remove(path)
			return err
		}
		return file.Close()
	}
	return fsutil.AtomicWriteFile(path, data, 0o600)
}

func deliveryScaffoldWriteError(deps *Deps, command, path string, err error) error {
	codeName := "unavailable"
	var usageErr *deliveryScaffoldUsageError
	if errors.As(err, &usageErr) {
		codeName = "usage"
	}
	payload := output.ErrorPayload{Code: codeName, Message: fmt.Sprintf("write %s: %v", path, err)}
	code := deps.Printer.Error(command, payload)
	return &output.ExitError{Code: code, Err: err}
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

// collectMissingCompletionPaths returns evidence paths and affected_files
// entries from a completion body that do not exist on disk, resolved against
// the current working directory. Empty paths (scaffold placeholders) are
// skipped.
func collectMissingCompletionPaths(body map[string]any) (evidence []string, affected []string) {
	criteria, _ := body["criteria"].([]any)
	for _, raw := range criteria {
		entry, _ := raw.(map[string]any)
		ev, _ := entry["evidence"].(map[string]any)
		path, _ := ev["path"].(string)
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			evidence = append(evidence, path)
		}
	}
	files, _ := body["affected_files"].([]any)
	for _, raw := range files {
		path, _ := raw.(string)
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			affected = append(affected, path)
		}
	}
	return evidence, affected
}

// verifyCompletionEvidence enforces that cited evidence paths exist before the
// report leaves the machine: the delivery review judge cannot inspect the
// repository, so a fabricated or mistyped citation would otherwise pass
// unchecked. Missing affected_files entries only warn — deletions are
// legitimate. Returns an *output.ExitError when verification fails.
func verifyCompletionEvidence(deps *Deps, command string, body map[string]any) error {
	missingEvidence, missingAffected := collectMissingCompletionPaths(body)
	if len(missingEvidence) > 0 {
		payload := output.ErrorPayload{
			Code: "validation",
			Message: fmt.Sprintf(
				"completion evidence cites paths that do not exist in the working tree: %s — fix the paths (relative to the directory specgate runs from) or pass --skip-evidence-check",
				strings.Join(missingEvidence, ", ")),
			Details: map[string]any{"missing_paths": missingEvidence},
		}
		code := deps.Printer.Error(command, payload)
		return &output.ExitError{Code: code}
	}
	if len(missingAffected) > 0 && deps.Printer.Mode() != output.ModeJSON {
		fmt.Fprintf(deps.Stderr, "Warning: affected_files entries not found in the working tree (deleted files are expected): %s\n",
			strings.Join(missingAffected, ", "))
	}
	groundCompletionEvidence(body)
	return nil
}

func validateCompletionReport(deps *Deps, command string, body map[string]any) error {
	if strings.TrimSpace(fmt.Sprint(body["event_type"])) == "coding_agent.completed" {
		if completionAgentName(body) == "" {
			return completionValidationError(deps, command, "completion agent.name is required")
		}
	}

	criteria, _ := body["criteria"].([]any)
	for _, raw := range criteria {
		criterion, _ := raw.(map[string]any)
		claim := strings.TrimSpace(fmt.Sprint(criterion["claim"]))
		switch claim {
		case "satisfied", "partial", "not_done":
		default:
			return completionValidationError(deps, command, fmt.Sprintf("criterion %s claim must be satisfied, partial, or not_done", criterion["criterion_id"]))
		}
		if claim != "satisfied" {
			continue
		}
		evidence, _ := criterion["evidence"].(map[string]any)
		hasEvidence := false
		for _, value := range evidence {
			if strings.TrimSpace(fmt.Sprint(value)) != "" {
				hasEvidence = true
				break
			}
		}
		if !hasEvidence {
			return completionValidationError(deps, command, fmt.Sprintf("criterion %s claims satisfied but has no evidence", criterion["criterion_id"]))
		}
	}

	checks, _ := body["checks"].([]any)
	for _, raw := range checks {
		check, _ := raw.(map[string]any)
		status := strings.TrimSpace(fmt.Sprint(check["status"]))
		switch status {
		case "pass", "fail", "skipped":
		default:
			return completionValidationError(deps, command, fmt.Sprintf("check %s status must be pass, fail, or skipped", check["name"]))
		}
		commandValue, _ := check["command"].(string)
		if status == "pass" && strings.TrimSpace(commandValue) == "" {
			return completionValidationError(deps, command, fmt.Sprintf("passing check %s requires a runnable command", check["name"]))
		}
	}
	return nil
}

func completionAgentName(body map[string]any) string {
	agent, _ := body["agent"].(map[string]any)
	name, _ := agent["name"].(string)
	return strings.TrimSpace(name)
}

func completionValidationError(deps *Deps, command, message string) error {
	payload := output.ErrorPayload{Code: "validation", Message: message}
	code := deps.Printer.Error(command, payload)
	return &output.ExitError{Code: code}
}

func groundCompletionEvidence(body map[string]any) {
	criteria, _ := body["criteria"].([]any)
	for _, raw := range criteria {
		entry, _ := raw.(map[string]any)
		ev, _ := entry["evidence"].(map[string]any)
		if ev == nil {
			continue
		}
		path, _ := ev["path"].(string)
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		excerpt := evidenceExcerpt(data, evidenceLine(ev["line"]))
		if excerpt == "" {
			continue
		}
		sum := sha256.Sum256(data)
		ev["grounding"] = map[string]any{
			"status":  "grounded",
			"excerpt": excerpt,
			"digest":  fmt.Sprintf("sha256:%x", sum[:]),
		}
	}
}

func evidenceLine(raw any) int {
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}

func evidenceExcerpt(data []byte, line int) string {
	text := string(data)
	if line > 0 {
		lines := strings.Split(text, "\n")
		if line <= len(lines) {
			return strings.TrimSpace(lines[line-1])
		}
	}
	text = strings.TrimSpace(text)
	if len(text) > 2000 {
		text = text[:2000]
	}
	return text
}

// executeCompletionChecks re-runs each checks[].command locally and replaces the
// claimed status with the observed result, converting narrated checks into
// executed checks. Skipped checks and checks without an explicit command are
// untouched. The corrected body is submitted either way — an observed failure
// is honest data for the delivery review, which already fails the verdict on any
// failed check.
func executeCompletionChecks(ctx context.Context, deps *Deps, body map[string]any) {
	runner := deps.RunCheckCommand
	if runner == nil {
		runner = defaultRunCheckCommand
	}
	checks, _ := body["checks"].([]any)
	for _, raw := range checks {
		entry, _ := raw.(map[string]any)
		if entry == nil {
			continue
		}
		command, _ := entry["command"].(string)
		claimed, _ := entry["status"].(string)
		command = strings.TrimSpace(command)
		if command == "" || claimed == "skipped" {
			continue
		}
		exitCode, combined := runner(ctx, command)
		observed := "pass"
		if exitCode != 0 {
			observed = "fail"
		}
		detail := fmt.Sprintf("executed by specgate: exit %d", exitCode)
		if tail := lastOutputLine(combined); tail != "" {
			detail += " — " + tail
		}
		entry["status"] = observed
		entry["detail"] = detail
		if deps.Printer.Mode() != output.ModeJSON {
			note := ""
			if observed != claimed {
				note = fmt.Sprintf(" (reported %q)", claimed)
			}
			fmt.Fprintf(deps.Stderr, "Executed check %q → %s%s\n", command, observed, note)
		}
	}
}

func confirmCompletionChecks(deps *Deps, operation string, body map[string]any) (bool, error) {
	var commands []string
	checks, _ := body["checks"].([]any)
	for _, raw := range checks {
		entry, _ := raw.(map[string]any)
		if entry == nil {
			continue
		}
		status, _ := entry["status"].(string)
		if strings.TrimSpace(status) == "skipped" {
			continue
		}
		command, _ := entry["command"].(string)
		if command = strings.TrimSpace(command); command != "" {
			commands = append(commands, command)
		}
	}
	if len(commands) == 0 || deps.Yes {
		return true, nil
	}
	if !sessionInteractive(deps) {
		return false, completionValidationError(
			deps,
			operation,
			"--run-checks executes commands from the completion file; review them and pass --yes to confirm in non-interactive use",
		)
	}
	fmt.Fprintln(deps.Stderr, "Commands requested by the completion report:")
	for _, command := range commands {
		fmt.Fprintf(deps.Stderr, "  %s\n", command)
	}
	confirmed, err := deps.Prompter.Confirm(fmt.Sprintf("Run %d completion check command(s)?", len(commands)), false)
	if err != nil {
		return false, &output.ExitError{Code: output.ExitUsage, Err: err}
	}
	if !confirmed {
		fmt.Fprintln(deps.Stdout, "Cancelled.")
	}
	return confirmed, nil
}

// normalizeCompletionChecksForSubmit ensures every command-only check also has
// a display name. The command itself remains in the append-only receipt so an
// independent reviewer can reproduce the reported check.
func normalizeCompletionChecksForSubmit(body map[string]any) {
	checks, _ := body["checks"].([]any)
	for _, raw := range checks {
		entry, _ := raw.(map[string]any)
		if entry == nil {
			continue
		}
		name, _ := entry["name"].(string)
		command, _ := entry["command"].(string)
		if strings.TrimSpace(name) == "" && strings.TrimSpace(command) != "" {
			entry["name"] = strings.TrimSpace(command)
		}
		if strings.TrimSpace(command) != "" {
			entry["command"] = strings.TrimSpace(command)
		}
	}
}

// defaultRunCheckCommand runs a check command through the shell and returns
// its exit code with combined output.
func defaultRunCheckCommand(ctx context.Context, command string) (int, string) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	combined, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(combined)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), string(combined)
	}
	return -1, string(combined) + err.Error()
}

// lastOutputLine returns the last non-empty line of command output, capped so
// check details stay readable in the report.
func lastOutputLine(combined string) string {
	lines := strings.Split(strings.TrimSpace(combined), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		const maxRunes = 160
		if runes := []rune(line); len(runes) > maxRunes {
			line = string(runes[:maxRunes])
		}
		return line
	}
	return ""
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

// deliveryWorkingDir returns the checkout used for local receipt collection.
// Tests and callers embedding the CLI can pin this through Deps.WorkingDir;
// production defaults to the process working directory.
func deliveryWorkingDir(deps *Deps) string {
	if deps != nil && strings.TrimSpace(deps.WorkingDir) != "" {
		return deps.WorkingDir
	}
	if dir, err := os.Getwd(); err == nil {
		return dir
	}
	return "."
}

func completionAffectedFiles(body map[string]any) []string {
	var files []string
	switch raw := body["affected_files"].(type) {
	case []any:
		files = make([]string, 0, len(raw))
		for _, value := range raw {
			if path, ok := value.(string); ok && strings.TrimSpace(path) != "" {
				files = append(files, path)
			}
		}
	case []string:
		for _, path := range raw {
			if strings.TrimSpace(path) != "" {
				files = append(files, path)
			}
		}
	}
	return files
}

// deliveryReceiptRepoRoot resolves paths the same way Git does: status paths
// are relative to the repository root even when the CLI runs from a nested
// checkout directory. A non-Git checkout falls back to an absolute working
// directory so path handling remains deterministic without inventing a root.
func deliveryReceiptRepoRoot(ctx context.Context, deps *Deps, dir string) string {
	runner := deps.DeployRunner
	if runner == nil {
		runner = deploy.ExecRunner{}
	}
	root, err := gitOutput(ctx, runner, dir, "rev-parse", "--show-toplevel")
	if err == nil && strings.TrimSpace(string(root)) != "" {
		if absolute, absErr := filepath.Abs(strings.TrimSpace(string(root))); absErr == nil {
			return absolute
		}
		return filepath.Clean(strings.TrimSpace(string(root)))
	}
	if absolute, absErr := filepath.Abs(dir); absErr == nil {
		return absolute
	}
	return filepath.Clean(dir)
}

// attachGitReceipt replaces authored receipt identity with a fresh local
// observation. When a prior base still precedes HEAD after push, it preserves
// that safe base for the delivered commit range. Absolute affected-file paths
// are made repository-relative for comparison with Git's porcelain status paths;
// the submitted affected_files field itself is left untouched for evidence
// grounding and review context.
func attachGitReceipt(ctx context.Context, deps *Deps, body map[string]any) gitReceipt {
	dir := deliveryWorkingDir(deps)
	repoRoot := deliveryReceiptRepoRoot(ctx, deps, dir)
	reported := completionAffectedFiles(body)
	for i, path := range reported {
		reported[i] = filepath.Clean(path)
		if filepath.IsAbs(path) {
			if rel, err := filepath.Rel(repoRoot, path); err == nil {
				reported[i] = filepath.Clean(rel)
			}
		}
	}
	receipt := collectGitReceiptWithPriorBase(ctx, deps.DeployRunner, dir, reported, priorGitReceiptBase(body))
	body["git_receipt"] = gitReceiptPayload(receipt)
	if deps.Printer != nil && deps.Printer.Mode() != output.ModeJSON {
		for _, warning := range receipt.Warnings {
			fmt.Fprintf(deps.Stderr, "Warning: %s\n", warning)
		}
	}
	return receipt
}

func priorGitReceiptBase(body map[string]any) string {
	raw, _ := body["git_receipt"].(map[string]any)
	base, _ := raw["base_revision"].(string)
	return strings.TrimSpace(base)
}

func gitReceiptPayload(receipt gitReceipt) map[string]any {
	data, err := json.Marshal(receipt)
	if err != nil {
		return map[string]any{}
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return map[string]any{}
	}
	return payload
}

// deliveryInitAuto is the sentinel for `--init` given without an explicit path.
// The scaffold is then derived under the project-local `.specgate/` working
// directory as `completion-<ref>.json` once the ref resolves, so it lands
// in the gitignored working dir instead of the repo root and per-work-item
// scaffolds never clobber each other. The readable value also keeps Cobra's
// generated help free of control characters.
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
	EventType     string                        `json:"event_type"`
	Summary       string                        `json:"summary"`
	Agent         map[string]string             `json:"agent"`
	ContextDigest string                        `json:"context_digest,omitempty"`
	AffectedFiles []string                      `json:"affected_files"`
	GitReceipt    gitReceipt                    `json:"git_receipt"`
	Checks        []completionCheckTemplate     `json:"checks"`
	Criteria      []completionCriterionTemplate `json:"criteria"`
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
	tpl := completionTemplate{
		EventType:     "coding_agent.completed",
		Agent:         map[string]string{"name": ""},
		ContextDigest: work.ContextDigest,
		AffectedFiles: []string{},
		Checks:        []completionCheckTemplate{{Name: "tests", Command: "", Status: "skipped", Detail: ""}},
		Criteria:      make([]completionCriterionTemplate, 0, len(work.AcceptanceCriteria)),
	}
	for index, criterion := range work.AcceptanceCriteria {
		text, binding := parseAcceptanceCriterionBinding(criterion)
		tpl.Criteria = append(tpl.Criteria, completionCriterionTemplate{
			CriterionID: fmt.Sprintf("local-%d", index+1), Text: text,
			Claim: "not_done", VerificationBinding: binding,
		})
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
	fmt.Fprintf(deps.Stdout, "Wrote %s for %s. Fill evidence, then run: specgate delivery submit %s --file %s\n", path, work.Key, work.Key, path)
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
		EventType:     "coding_agent.completed",
		Agent:         map[string]string{"name": ""},
		Summary:       "",
		AffectedFiles: []string{},
		GitReceipt:    receipt,
		Checks:        []completionCheckTemplate{{Name: completionTemplateCheckName(criteria), Command: "", Status: "skipped", Detail: ""}},
		Criteria:      make([]completionCriterionTemplate, 0, len(criteria)),
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

func completionTemplateCheckName(criteria []client.AcceptanceCriterion) string {
	for _, c := range criteria {
		if binding := strings.TrimSpace(c.VerificationBinding); binding != "" {
			return binding
		}
	}
	return "tests"
}

// specgate delivery submit [work-ref] --file <completion.json>
//
// One-command delivery tail: report completion, run quality gates, trigger the
// delivery review, then fetch and print the per-criterion delivery status.
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
					result = deriveLocalChangeStatus(work, &review, &report, peer)
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
					deps.Printer.Success(spec.Operation, deriveFullChangeStatus(work, ds))
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
	cmd.Flags().BoolVar(&runChecks, "run-checks", false, "Re-execute each explicit checks[].command locally and submit observed results")
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

func localDeliveryReviewView(review local.DeliveryReview) map[string]any {
	return map[string]any{"found": true, "id": review.ID, "work_id": review.WorkID, "report_id": review.ReportID, "verdict": review.Verdict, "summary": review.Summary, "human_decision": review.HumanDecision, "note": review.Note, "created_at": review.CreatedAt}
}

func printLocalDeliveryStatus(cmd *cobra.Command, deps *Deps, ref, commandName string) error {
	store, err := openLocalStore(deps)
	if err != nil {
		return localExitError(deps, commandName, err)
	}
	defer store.Close()
	selection, err := localSelection(cmd.Context(), deps, store)
	if err != nil {
		return localExitError(deps, commandName, err)
	}
	work, err := store.GetWork(cmd.Context(), selection.Workspace.ID, ref)
	if err != nil {
		return localExitError(deps, commandName, err)
	}
	review, err := store.DeliveryStatus(cmd.Context(), selection.Workspace.ID, ref)
	if errors.Is(err, sql.ErrNoRows) {
		next := "specgate delivery report " + work.Key + " --init"
		if deps.Printer.Mode() == output.ModeJSON {
			deps.Printer.Success(commandName, map[string]any{"found": false, "work_id": work.ID, "work_key": work.Key, "next": next})
			return nil
		}
		fmt.Fprintf(deps.Stdout, "No delivery review found for %s.\nNext: %s\n", work.Key, next)
		return nil
	}
	if err != nil {
		return localExitError(deps, commandName, err)
	}
	peer, err := store.PeerReviewStatus(cmd.Context(), selection.Workspace.ID, ref)
	if err != nil {
		return localExitError(deps, commandName, err)
	}
	report, err := store.DeliveryReportForReview(cmd.Context(), selection.Workspace.ID, review)
	if err != nil {
		return localExitError(deps, commandName, err)
	}
	receiptLabel := localDeliveryReceiptLabel(report.Body)
	if deps.Printer.Mode() == output.ModeJSON {
		data := localDeliveryReviewView(review)
		data["peer_review"] = peer
		data["evidence_assessment"] = deliveryEvidenceLabel(review.Verdict, "")
		data["assurance_source"] = localDeliveryAssuranceLabel(peer)
		data["decision_state"] = localDeliveryDecisionLabel(review.HumanDecision)
		data["receipt"] = receiptLabel
		deps.Printer.Success(commandName, data)
		return nil
	}
	fmt.Fprintf(deps.Stdout, "Evidence: %s\n", deliveryEvidenceLabel(review.Verdict, ""))
	fmt.Fprintf(deps.Stdout, "Assurance: %s\n", localDeliveryAssuranceLabel(peer))
	fmt.Fprintf(deps.Stdout, "Decision: %s\n", localDeliveryDecisionLabel(review.HumanDecision))
	fmt.Fprintf(deps.Stdout, "Receipt: %s\n", receiptLabel)
	fmt.Fprintf(deps.Stdout, "Stored verdict: %s\n", review.Verdict)
	fmt.Fprintf(deps.Stdout, "Peer review: %s\n", peer.State)
	fmt.Fprintln(deps.Stdout, review.Summary)
	if review.HumanDecision == "" {
		printDeliveryDecisionCommands(deps, work.Key, &client.DeliveryStatusResult{
			Found: true, Verdict: review.Verdict, Executor: "platform",
		}, true)
	}
	return nil
}

func printDeliveryDecisionCommands(deps *Deps, ref string, ds *client.DeliveryStatusResult, localMode bool) {
	if !ds.Found || strings.TrimSpace(ds.Executor) == "human" ||
		strings.TrimSpace(ds.ReasonCode) == "policy_unavailable" ||
		strings.TrimSpace(ds.ReasonCode) == "delivery_review_outdated" {
		return
	}
	prefix := "specgate "
	if localMode {
		prefix = "specgate --yes "
	}
	fmt.Fprintln(deps.Stdout, "\nDecision commands:")
	fmt.Fprintf(deps.Stdout, "  %schange accept %s\n", prefix, ref)
	fmt.Fprintf(deps.Stdout, "  %schange request-changes %s --note \"<reason>\"\n", prefix, ref)
}

func localDeliveryDecisionLabel(decision string) string {
	switch strings.TrimSpace(decision) {
	case "approve":
		return "Accepted"
	case "reject":
		return "Rejected"
	default:
		return "Awaiting human acceptance"
	}
}

func localDeliveryAssuranceLabel(peer local.PeerReviewStatus) string {
	switch strings.TrimSpace(peer.State) {
	case "passed":
		return "Agent-reported; second agent affirmed"
	case "failed":
		return "Agent-reported; peer review found gaps"
	default:
		return "Agent-reported"
	}
}

func localDeliveryReceiptLabel(body map[string]any) string {
	receipt, _ := body["git_receipt"].(map[string]any)
	warnings := stringSlice(receipt["warnings"])
	suffix := receiptWarningSuffix(warnings)
	availability, _ := receipt["availability"].(string)
	if availability = strings.TrimSpace(availability); availability != "" && availability != "available" {
		return "Git receipt unavailable" + suffix
	}
	head, _ := receipt["head_revision"].(string)
	head = strings.TrimSpace(head)
	if head == "" {
		return "No Git receipt recorded" + suffix
	}
	return "commit " + shortGitRevision(head) + suffix
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
	printDeliveryTrustSummary(deps, ds, "")
	fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleMuted, "Stored verdict:"), styledStatus(deps, deliveryStoredVerdictLabel(ds)))
	if warn := attestedWarning(ds); warn != "" {
		fmt.Fprintf(deps.Stdout, "%s\n", warn)
	}
	fmt.Fprintf(deps.Stdout, "%s\n", styled(deps, output.StyleMuted, selfSelectedChecksNote))
	if ds.Hint != "" {
		fmt.Fprintf(deps.Stdout, "%s     %s\n", styled(deps, output.StyleMuted, "Hint:"), ds.Hint)
	}
	if ds.Summary != "" {
		fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleMuted, "Review summary:"), ds.Summary)
	}
	if ds.ReviewedAt != "" {
		fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleMuted, "Reviewed:"), ds.ReviewedAt)
	}
	if judge := deliveryJudgeLabel(ds); judge != "" {
		fmt.Fprintf(deps.Stdout, "%s    %s\n", styled(deps, output.StyleMuted, "Judge:"), judge)
	}
	if detail {
		fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleMuted, "Peer review:"), ds.PeerReview.State)
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
	fmt.Fprintln(deps.Stdout, title(deps, "Delivery Review"))
	fmt.Fprintln(deps.Stdout, visualRule(deps))
	printDeliveryTrustSummary(deps, ds, "  ")
	fmt.Fprintf(deps.Stdout, "%s %s %s\n",
		statusIcon(deps, ds.Verdict),
		styled(deps, output.StyleMuted, "Stored verdict:"),
		styledStatus(deps, deliveryStoredVerdictLabel(ds)))
	if warn := attestedWarning(ds); warn != "" {
		fmt.Fprintf(deps.Stdout, "  %s\n", styled(deps, output.StyleWarning, warn))
	}
	fmt.Fprintf(deps.Stdout, "  %s\n", styled(deps, output.StyleMuted, selfSelectedChecksNote))
	if ds.Hint != "" {
		fmt.Fprintf(deps.Stdout, "  %s %s\n", styled(deps, output.StyleMuted, "Hint:"), ds.Hint)
	}
	if ds.Summary != "" {
		fmt.Fprintf(deps.Stdout, "  %s %s\n", styled(deps, output.StyleMuted, "Review summary:"), ds.Summary)
	}
	if ds.ReviewedAt != "" {
		fmt.Fprintf(deps.Stdout, "  %s %s\n", styled(deps, output.StyleMuted, "Reviewed:"), ds.ReviewedAt)
	}
	if judge := deliveryJudgeLabel(ds); judge != "" {
		fmt.Fprintf(deps.Stdout, "  %s %s\n", styled(deps, output.StyleMuted, "Judge:"), judge)
	}
	if detail {
		fmt.Fprintf(deps.Stdout, "  %s %s\n", styled(deps, output.StyleMuted, "Peer review:"), ds.PeerReview.State)
	}
	if ds.OutstandingMD != "" {
		fmt.Fprintf(deps.Stdout, "\n%s\n", ds.OutstandingMD)
	}
	if detail && len(ds.PerCriterion) > 0 {
		printCriterionDashboard(deps, ds.PerCriterion)
	}
}

// selfSelectedChecksNote is a runtime belt-and-suspenders reminder: delivery
// review judges only the checks the coding agent chose to report, so a
// regression outside that self-selected scope can still pass. Surfacing this in
// the status/review output keeps a human reviewer from over-trusting a narrow
// check set.
const selfSelectedChecksNote = "Checks are self-selected by the coding agent; delivery review judges only the reported checks."

func deliveryStoredVerdictLabel(ds *client.DeliveryStatusResult) string {
	switch strings.TrimSpace(ds.Verdict) {
	case "pass", "passed":
		if strings.TrimSpace(ds.Executor) == "human" {
			return "Accepted"
		}
		return "Ready for human review"
	case "fail", "failed":
		return "Evidence gaps found"
	case "needs_human_review":
		switch strings.TrimSpace(ds.ReasonCode) {
		case "policy_unavailable":
			return "Policy unavailable"
		case "delivery_review_outdated":
			return "Evidence stale"
		default:
			return "Independent confirmation required"
		}
	default:
		return ds.Verdict
	}
}

func printDeliveryTrustSummary(deps *Deps, ds *client.DeliveryStatusResult, indent string) {
	fmt.Fprintf(deps.Stdout, "%s%s %s\n", indent, styled(deps, output.StyleMuted, "Evidence:"), deliveryEvidenceLabel(deliveryEvidenceVerdict(ds), ds.ReasonCode))
	fmt.Fprintf(deps.Stdout, "%s%s %s\n", indent, styled(deps, output.StyleMuted, "Assurance:"), deliveryAssuranceLabel(ds))
	fmt.Fprintf(deps.Stdout, "%s%s %s\n", indent, styled(deps, output.StyleMuted, "Decision:"), deliveryDecisionLabel(ds))
	fmt.Fprintf(deps.Stdout, "%s%s %s\n", indent, styled(deps, output.StyleMuted, "Receipt:"), deliveryReceiptLabel(ds.GitReceipt))
}

func deliveryEvidenceVerdict(ds *client.DeliveryStatusResult) string {
	if verdict := strings.TrimSpace(ds.EvidenceVerdict); verdict != "" {
		return verdict
	}
	if strings.TrimSpace(ds.Executor) == "human" {
		return ""
	}
	return ds.Verdict
}

func deliveryEvidenceLabel(verdict, reasonCode string) string {
	switch strings.TrimSpace(reasonCode) {
	case "policy_unavailable":
		return "Policy unavailable"
	case "delivery_review_outdated":
		return "Review pending for latest completion"
	}
	switch strings.TrimSpace(verdict) {
	case "pass", "passed":
		return "Ready for human review"
	case "fail", "failed", "needs_changes":
		return "Evidence gaps found"
	case "needs_human_review":
		return "Human review required"
	default:
		return "Not reviewed"
	}
}

func deliveryAssuranceLabel(ds *client.DeliveryStatusResult) string {
	labels := []string{"Agent-reported"}
	seen := map[string]bool{"Agent-reported": true}
	appendSource := func(source string) {
		label := deliveryAssuranceSourceLabel(source)
		if label != "" && !seen[label] {
			labels = append(labels, label)
			seen[label] = true
		}
	}
	for _, source := range ds.AssuranceSources {
		appendSource(source)
	}
	for _, criterion := range ds.PerCriterion {
		appendSource(criterion.TrustTier)
	}
	return strings.Join(labels, "; ")
}

func deliveryAssuranceSourceLabel(source string) string {
	switch strings.TrimSpace(source) {
	case "grounded":
		return "local citation captured"
	case "deterministic":
		return "locally reproduced"
	case "peer_reviewed":
		return "second agent affirmed"
	case "repository_observed":
		return "Submitted commit observed on merged PR/MR"
	default:
		return ""
	}
}

func deliveryDecisionLabel(ds *client.DeliveryStatusResult) string {
	if strings.TrimSpace(ds.Executor) != "human" {
		return "Awaiting human acceptance"
	}
	if passingStatus(ds.Verdict) {
		return "Accepted"
	}
	return "Rejected"
}

func deliveryReceiptLabel(receipt *client.GitReceipt) string {
	if receipt == nil {
		return "No Git receipt recorded"
	}
	suffix := receiptWarningSuffix(receipt.Warnings)
	if availability := strings.TrimSpace(receipt.Availability); availability != "" && availability != "available" {
		return "Git receipt unavailable" + suffix
	}
	if strings.TrimSpace(receipt.HeadRevision) == "" {
		return "No Git receipt recorded" + suffix
	}
	return "commit " + shortGitRevision(receipt.HeadRevision) + suffix
}

func receiptWarningSuffix(warnings []string) string {
	clean := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		if warning = strings.TrimSpace(warning); warning != "" {
			clean = append(clean, warning)
		}
	}
	switch len(clean) {
	case 0:
		return ""
	case 1:
		return "; warning: " + clean[0]
	default:
		return "; warnings: " + strings.Join(clean, " | ")
	}
}

func stringSlice(value any) []string {
	switch items := value.(type) {
	case []string:
		return items
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func shortGitRevision(revision string) string {
	revision = strings.TrimSpace(revision)
	if len(revision) <= 12 {
		return revision
	}
	return revision[:12]
}

// attestedWarning flags verdicts the platform derived from the coding agent's
// own claims (no governance model configured) — the delivery was not
// independently reviewed, and a human should read the verdict accordingly.
func attestedWarning(ds *client.DeliveryStatusResult) string {
	if strings.TrimSpace(ds.JudgeModel) != "agent_attested" {
		return ""
	}
	return "Agent-reported evidence: the implementing agent supplied these claims. " +
		"A platform model can add another review of the submitted evidence; it does not verify the code or replace CI and human review."
}

func deliveryJudgeLabel(ds *client.DeliveryStatusResult) string {
	if strings.TrimSpace(ds.JudgeModel) == "" {
		return ""
	}
	label := strings.TrimSpace(ds.JudgeModel)
	switch label {
	case "agent_attested":
		label = "Agent-reported"
	case "deterministic_checks":
		label = "Locally reproduced checks"
	case "deterministic_policy_guard":
		label = "Policy guard"
	}
	if strings.TrimSpace(ds.EvalSuite) != "" {
		label += " / " + strings.TrimSpace(ds.EvalSuite)
	}
	return label
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
		status := styledStatus(deps, c.Verdict)
		if c.VerificationBinding != "" {
			// Deterministic verdict from a bound check.
			status += styled(deps, output.StyleMuted, fmt.Sprintf(" (via check: %s)", c.VerificationBinding))
		}
		fmt.Fprintf(deps.Stdout, "  %s %-20s %s\n",
			criterionBox(deps, c.Verdict),
			label,
			status)
		if c.Why != "" {
			fmt.Fprintf(deps.Stdout, "    %s\n", styled(deps, output.StyleMuted, c.Why))
		}
	}
}
