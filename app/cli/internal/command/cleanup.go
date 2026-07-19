package command

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/deploy"
	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/output"
)

type workCleanupResult struct {
	WorkDir   string   `json:"work_dir"`
	Available []string `json:"available"`
	Selected  []string `json:"selected"`
	Removed   []string `json:"removed"`
	DryRun    bool     `json:"dry_run"`
}

type backupCleanupResult struct {
	BackupDir string   `json:"backup_dir"`
	Available []string `json:"available"`
	Selected  []string `json:"selected"`
	Removed   []string `json:"removed"`
	DryRun    bool     `json:"dry_run"`
}

// specgate cleanup
//
// Housekeeping cleanup: runs the retention sweep immediately, removes demo
// seed data, and hard-deletes archived work items. Approved and draft
// artifacts, active features, in-flight work, and audit events are never
// touched.
func newCleanupCmd(deps *Deps) *cobra.Command {
	var (
		work      bool
		backups   bool
		deployDir string
		items     []string
		dryRun    bool
	)
	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Clean workspace retention data or transient project work files",
		Long: `Clean workspace retention data, or remove selected transient project work files.

Use --work for transient project files, or --backups for CLI-managed recovery
archives. Neither mode removes project configuration or active SpecGate data.
In an interactive terminal, choose entries; automation uses --item, --dry-run,
and --yes.`,
		Example: `  specgate cleanup --work --dry-run
  specgate cleanup --work --item completion-CR-123.json --yes
  specgate cleanup --backups --dry-run
  specgate cleanup --backups --item specgate-before-0.2.0-20260717T010203Z.tar.gz --yes
  specgate cleanup --yes`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if fileCleanupRequested(cmd) {
				if work && backups {
					return cleanupWorkError(deps, "--work and --backups cannot be used together")
				}
				if !work && !backups {
					return cleanupWorkError(deps, "--item, --dry-run, and --dir require --work or --backups")
				}
				if work && cmd.Flags().Changed("dir") {
					return cleanupWorkError(deps, "--dir applies only to --backups")
				}
				if backups {
					if deployDir == "" {
						deployDir = resolveDeployDir(deps)
					}
					if err := deploy.ValidateManagedDirectory(deployDir); err != nil {
						return cleanupWorkError(deps, err.Error())
					}
					return runBackupCleanup(deps, filepath.Join(deployDir, "backups"), items, dryRun)
				}
				return runWorkCleanup(cmd, deps, items, dryRun)
			}
			proceed, err := requireConfirm(deps, "Clean up the workspace? This deletes expired superseded/needs-changes artifacts, demo seed data, and archived work items. Approved artifacts and active work are kept.")
			if err != nil || !proceed {
				return err
			}
			counts, err := deps.Client.MaintenanceCleanup(cmd.Context())
			if err != nil {
				return apiExitError(deps, "cleanup", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("cleanup", counts)
				return nil
			}
			fmt.Fprintln(deps.Stdout, styled(deps, output.StyleSuccess, "Workspace cleanup complete."))
			fmt.Fprintf(deps.Stdout, "  %s %s (skipped, still referenced: %s)\n", label(deps, "expired artifacts deleted:"), countText(counts, "expired_artifacts_deleted"), countText(counts, "referenced_skipped"))
			fmt.Fprintf(deps.Stdout, "  %s %s\n", label(deps, "archived work items purged:"), countText(counts, "archived_change_requests_deleted"))
			fmt.Fprintf(deps.Stdout, "  %s %s features, %s work items, %s artifacts\n", label(deps, "demo seed data removed:"),
				countText(counts, "demo_features_deleted"), countText(counts, "demo_change_requests_deleted"), countText(counts, "demo_artifacts_deleted"))
			return nil
		},
	}
	cmd.Flags().BoolVar(&work, "work", false, "Clean transient project files under .specgate/work")
	cmd.Flags().BoolVar(&backups, "backups", false, "Clean CLI-managed recovery archives")
	cmd.Flags().StringVar(&deployDir, "dir", "", "Deployment directory containing backups (requires --backups)")
	cmd.Flags().StringArrayVar(&items, "item", nil, "Entry to remove (repeatable; requires --work or --backups)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show selected project work files or recovery archives without removing them")
	return cmd
}

func runWorkCleanup(cmd *cobra.Command, deps *Deps, requested []string, dryRun bool) error {
	specgateDir := filepath.Join(deliveryWorkingDir(deps), ".specgate")
	workDir := filepath.Join(specgateDir, "work")
	available, paths, err := workCleanupEntries(specgateDir, workDir)
	if err != nil {
		return cleanupWorkError(deps, fmt.Sprintf("list transient project files: %v", err))
	}
	if !dryRun && deps.Yes && !sessionInteractive(deps) && len(requested) == 0 && len(available) > 0 {
		return cleanupWorkError(deps, fmt.Sprintf(
			"noninteractive work cleanup requires explicit --item values; review with `specgate cleanup --work --dry-run`, then run `specgate cleanup --work --item %s --yes`",
			available[0],
		))
	}
	selected, err := selectWorkCleanupEntries(deps, available, requested, dryRun)
	if err != nil {
		return cleanupWorkError(deps, err.Error())
	}
	result := workCleanupResult{WorkDir: workDir, Available: available, Selected: selected, Removed: []string{}, DryRun: dryRun}
	if len(selected) == 0 {
		return printWorkCleanupResult(deps, result)
	}
	if dryRun {
		return printWorkCleanupResult(deps, result)
	}
	if !deps.Yes {
		if !sessionInteractive(deps) {
			return cleanupWorkError(deps, "--work permanently removes selected project files; review with --dry-run, then pass --yes")
		}
		confirmed, err := deps.Prompter.Confirm(fmt.Sprintf("Remove %d selected transient project work entries?", len(selected)), false)
		if err != nil {
			return cleanupWorkError(deps, err.Error())
		}
		if !confirmed {
			fmt.Fprintln(deps.Stdout, "Cancelled.")
			return nil
		}
	}
	for _, name := range selected {
		path := paths[name]
		var err error
		if filepath.Dir(path) == specgateDir {
			err = os.Remove(path)
		} else {
			err = os.RemoveAll(path)
		}
		if err != nil {
			return cleanupWorkError(deps, fmt.Sprintf("remove %s: %v", path, err))
		}
		result.Removed = append(result.Removed, name)
	}
	if len(result.Removed) > 0 {
		_ = os.Remove(workDir)
		_ = os.Remove(filepath.Dir(workDir))
	}
	return printWorkCleanupResult(deps, result)
}

func workCleanupEntries(specgateDir, workDir string) ([]string, map[string]string, error) {
	paths := map[string]string{}
	if err := rejectSymlinkedCleanupDirectory(specgateDir); err != nil {
		return nil, nil, err
	}
	if err := rejectSymlinkedCleanupDirectory(workDir); err != nil {
		return nil, nil, err
	}
	entries, err := os.ReadDir(workDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		paths[entry.Name()] = filepath.Join(workDir, entry.Name())
	}
	rootEntries, err := os.ReadDir(specgateDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	for _, entry := range rootEntries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			continue
		}
		name := entry.Name()
		if !(strings.HasPrefix(name, "completion-") || strings.HasPrefix(name, "peer-review-")) || filepath.Ext(name) != ".json" {
			continue
		}
		if _, exists := paths[name]; exists {
			paths["work/"+name] = paths[name]
		}
		paths[name] = filepath.Join(specgateDir, name)
	}
	items := make([]string, 0, len(paths))
	for name := range paths {
		items = append(items, name)
	}
	slices.Sort(items)
	return items, paths, nil
}

func rejectSymlinkedCleanupDirectory(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing cleanup through symlinked directory %s", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("cleanup path %s is not a directory", path)
	}
	return nil
}

func runBackupCleanup(deps *Deps, backupDir string, requested []string, dryRun bool) error {
	available, err := backupCleanupEntries(backupDir)
	if err != nil {
		return cleanupWorkError(deps, fmt.Sprintf("list recovery archives: %v", err))
	}
	selected, err := selectCleanupEntries(deps, available, requested, dryRun, "recovery archive", "specgate cleanup --backups --dry-run")
	if err != nil {
		return cleanupWorkError(deps, err.Error())
	}
	result := backupCleanupResult{
		BackupDir: backupDir,
		Available: available,
		Selected:  selected,
		Removed:   []string{},
		DryRun:    dryRun,
	}
	if len(selected) == 0 || dryRun {
		return printBackupCleanupResult(deps, result)
	}
	if !deps.Yes {
		if !sessionInteractive(deps) {
			return cleanupWorkError(deps, "--backups permanently removes selected recovery archives; review with --dry-run, then pass --yes")
		}
		confirmed, err := deps.Prompter.Confirm(fmt.Sprintf("Remove %d selected SpecGate recovery archives?", len(selected)), false)
		if err != nil {
			return cleanupWorkError(deps, err.Error())
		}
		if !confirmed {
			fmt.Fprintln(deps.Stdout, "Cancelled.")
			return nil
		}
	}
	for _, name := range selected {
		if err := os.Remove(filepath.Join(backupDir, name)); err != nil {
			return cleanupWorkError(deps, fmt.Sprintf("remove recovery archive %s: %v", name, err))
		}
		result.Removed = append(result.Removed, name)
	}
	return printBackupCleanupResult(deps, result)
}

func backupCleanupEntries(backupDir string) ([]string, error) {
	if err := rejectSymlinkedCleanupDirectory(backupDir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(backupDir)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			continue
		}
		if matched, _ := filepath.Match("specgate-before-*.tar.gz", entry.Name()); matched {
			items = append(items, entry.Name())
		}
	}
	slices.Sort(items)
	return items, nil
}

func selectWorkCleanupEntries(deps *Deps, available, requested []string, dryRun bool) ([]string, error) {
	return selectCleanupEntries(deps, available, requested, dryRun, "transient work entry", "specgate cleanup --work --dry-run")
}

func selectCleanupEntries(deps *Deps, available, requested []string, dryRun bool, kind, listCommand string) ([]string, error) {
	if len(requested) > 0 {
		selected := make([]string, 0, len(requested))
		for _, item := range requested {
			if !slices.Contains(available, item) {
				return nil, fmt.Errorf("%q is not an eligible %s; run `%s` to list eligible entries", item, kind, listCommand)
			}
			if !slices.Contains(selected, item) {
				selected = append(selected, item)
			}
		}
		return selected, nil
	}
	if dryRun || !sessionInteractive(deps) {
		return available, nil
	}
	options := make([]interactive.Option, 0, len(available))
	for _, item := range available {
		options = append(options, interactive.Option{Label: item, Value: item})
	}
	title := "Select transient SpecGate work files to remove"
	if kind == "recovery archive" {
		title = "Select SpecGate recovery archives to remove"
	}
	return deps.Prompter.MultiSelect(title, options, available)
}

func printWorkCleanupResult(deps *Deps, result workCleanupResult) error {
	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success("cleanup", result)
		return nil
	}
	if len(result.Selected) == 0 && len(result.Available) == 0 {
		message := fmt.Sprintf("No transient project work files found in %s.", result.WorkDir)
		if humanVisuals(deps) {
			message = notice(deps, output.StyleInfo, "Notice", message)
		}
		fmt.Fprintln(deps.Stdout, message)
		return nil
	}
	if len(result.Selected) == 0 {
		message := "No project work files selected."
		if humanVisuals(deps) {
			message = notice(deps, output.StyleInfo, "Notice", message)
		}
		fmt.Fprintln(deps.Stdout, message)
		return nil
	}
	if result.DryRun {
		fmt.Fprintf(deps.Stdout, "%s %d transient project work entries from %s:\n", styled(deps, output.StyleWarning, "Would remove"), len(result.Selected), result.WorkDir)
		for _, item := range result.Selected {
			fmt.Fprintf(deps.Stdout, "  %s\n", item)
		}
		if humanVisuals(deps) {
			fmt.Fprintln(deps.Stdout, nextStep(deps, "remove them:", "specgate cleanup --work --yes"))
		} else {
			fmt.Fprintln(deps.Stdout, "Run again with --yes to remove them.")
		}
		return nil
	}
	fmt.Fprintf(deps.Stdout, "%s %d transient project work entries from %s.\n", styled(deps, output.StyleSuccess, "Removed"), len(result.Removed), result.WorkDir)
	return nil
}

func printBackupCleanupResult(deps *Deps, result backupCleanupResult) error {
	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success("cleanup", result)
		return nil
	}
	if len(result.Selected) == 0 && len(result.Available) == 0 {
		fmt.Fprintf(deps.Stdout, "No CLI-managed recovery archives found in %s.\n", result.BackupDir)
		return nil
	}
	if len(result.Selected) == 0 {
		fmt.Fprintln(deps.Stdout, "No recovery archives selected.")
		return nil
	}
	if result.DryRun {
		fmt.Fprintf(deps.Stdout, "%s %d recovery archives from %s:\n", styled(deps, output.StyleWarning, "Would remove"), len(result.Selected), result.BackupDir)
		for _, item := range result.Selected {
			fmt.Fprintf(deps.Stdout, "  %s\n", item)
		}
		fmt.Fprintln(deps.Stdout, "Run again with --yes to remove them.")
		return nil
	}
	fmt.Fprintf(deps.Stdout, "%s %d recovery archives from %s.\n", styled(deps, output.StyleSuccess, "Removed"), len(result.Removed), result.BackupDir)
	return nil
}

func cleanupWorkError(deps *Deps, message string) error {
	payload := output.ErrorPayload{Code: "validation_failed", Message: message}
	code := deps.Printer.Error("cleanup", payload)
	return &output.ExitError{Code: code, Err: fmt.Errorf("%s", message)}
}

func fileCleanupRequested(cmd *cobra.Command) bool {
	work, _ := cmd.Flags().GetBool("work")
	backups, _ := cmd.Flags().GetBool("backups")
	return work || backups || cmd.Flags().Changed("item") || cmd.Flags().Changed("dry-run") || cmd.Flags().Changed("dir")
}

// countText renders a numeric JSON count for plain output ("0" when missing).
func countText(counts map[string]any, key string) string {
	if v, ok := counts[key].(float64); ok {
		return fmt.Sprintf("%.0f", v)
	}
	return "0"
}
