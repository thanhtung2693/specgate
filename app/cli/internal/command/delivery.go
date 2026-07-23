package command

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/deploy"
	"github.com/specgate/specgate/app/cli/internal/fsutil"
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
	payload := output.ErrorPayload{
		Code: codeName, Message: fmt.Sprintf("write %s: %v", path, err),
		Details: map[string]any{"path": path},
	}
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
		entry["source"] = "specgate_cli"
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
