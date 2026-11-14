package command

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

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

func newCleanupCmd(deps *Deps) *cobra.Command {
	var (
		work   bool
		items  []string
		dryRun bool
	)
	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Clean selected transient project work files",
		Example: `  specgate cleanup --work --dry-run
  specgate cleanup --work --item completion-LOCAL-123.json --yes`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !work {
				return cleanupWorkError(deps, "cleanup arguments require --work to select transient project files")
			}
			return runWorkCleanup(cmd, deps, items, dryRun)
		},
	}
	cmd.Flags().BoolVar(&work, "work", false, "Clean transient project files under .specgate/work")
	cmd.Flags().StringArrayVar(&items, "item", nil, "Entry to remove (repeatable; requires --work)")
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

func cleanupWorkError(deps *Deps, message string) error {
	payload := output.ErrorPayload{Code: "validation_failed", Message: message}
	code := deps.Printer.Error("cleanup", payload)
	return &output.ExitError{Code: code, Err: fmt.Errorf("%s", message)}
}
