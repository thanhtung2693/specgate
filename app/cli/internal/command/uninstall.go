package command

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/output"
)

type uninstallResult struct {
	Dir            string   `json:"dir"`
	StoppedStack   bool     `json:"stopped_stack"`
	PurgedData     bool     `json:"purged_data"`
	RemovedConfig  bool     `json:"removed_config"`
	RemovedPlugins int      `json:"removed_plugins"`
	RemovedPaths   []string `json:"removed_paths"`
	PreservedPaths []string `json:"preserved_paths,omitempty"`
	// ModifiedPaths are files SpecGate edited, not deleted, so an auditor can
	// tell "we dropped our entry from your config" from "we removed this file".
	ModifiedPaths []string `json:"modified_paths,omitempty"`
	KeptData      bool     `json:"kept_data"`
}

// specgate uninstall
func newUninstallCmd(deps *Deps) *cobra.Command {
	var (
		purgeData     bool
		removePlugins = true
	)
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove this user's SpecGate setup",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.LoadFrom(deps.ConfigPath)
			if err != nil {
				return uninstallError(deps, "unavailable", err)
			}
			return runLocalUninstall(cmd, deps, cfg, purgeData, removePlugins)
		},
	}
	cmd.Flags().BoolVar(&purgeData, "purge-data", false, "Remove Local SQLite state")
	return cmd
}

func runLocalUninstall(cmd *cobra.Command, deps *Deps, cfg config.Config, purgeData, removePlugins bool) error {
	if !deps.Yes && sessionInteractive(deps) && !cmd.Flags().Changed("purge-data") {
		choices, err := deps.Prompter.MultiSelect("Remove Local SpecGate setup", []interactive.Option{
			{Label: "IDE plugin files", Value: "plugins"},
			{Label: "Local SQLite state", Value: "data"},
		}, []string{"plugins"})
		if err != nil {
			return err
		}
		removePlugins = slices.Contains(choices, "plugins")
		purgeData = slices.Contains(choices, "data")
	}
	stateDir := cfg.Local.Path
	if purgeData {
		var err error
		stateDir, err = resolveUninstallPurgeDir(deps, cfg)
		if err != nil {
			return err
		}
		if err := validateRealDirectoryIfExists(stateDir); err != nil {
			return uninstallError(deps, "unavailable", err)
		}
	}
	configPath, err := configPathForDeps(deps)
	if err != nil {
		return uninstallError(deps, "unavailable", err)
	}
	if err := validateRegularFileIfExists(configPath); err != nil {
		return uninstallError(deps, "unavailable", err)
	}
	if purgeData {
		if !deps.Yes {
			if !sessionInteractive(deps) {
				err := fmt.Errorf("--purge-data permanently removes Local SQLite state in %s; pass --yes to confirm", stateDir)
				return uninstallError(deps, "validation_failed", err)
			}
			confirmed, err := deps.Prompter.Confirm(fmt.Sprintf("Permanently remove Local SQLite state in %s?", stateDir), false)
			if err != nil {
				return uninstallError(deps, "validation_failed", err)
			}
			if !confirmed {
				fmt.Fprintln(deps.Stdout, "Cancelled.")
				return nil
			}
		}
		if deps.Printer.Mode() != output.ModeJSON {
			fmt.Fprintf(deps.Stderr, "Warning: Local purge removes only SpecGate SQLite files in %s (state.db and its SQLite journal files). Other files in this directory are preserved.\n", stateDir)
		}
	}

	result := uninstallResult{Dir: stateDir, PurgedData: purgeData, RemovedPaths: []string{}, KeptData: !purgeData}
	if purgeData {
		removed, err := removeLocalSQLiteState(stateDir)
		if err != nil {
			return uninstallError(deps, "unavailable", err)
		}
		result.RemovedPaths = append(result.RemovedPaths, removed...)
	}
	if removePlugins {
		removed, paths, preserved, modified, err := removeSpecGatePluginFiles(deps)
		if err != nil {
			return uninstallError(deps, "unavailable", err)
		}
		result.RemovedPlugins = removed
		result.RemovedPaths = append(result.RemovedPaths, paths...)
		result.PreservedPaths = append(result.PreservedPaths, preserved...)
		result.ModifiedPaths = append(result.ModifiedPaths, modified...)
	}
	configRemoved, err := removeConfigFile(deps)
	if err != nil {
		return uninstallError(deps, "unavailable", err)
	}
	result.RemovedConfig = configRemoved
	if configRemoved {
		if path, err := configPathForDeps(deps); err == nil {
			result.RemovedPaths = append(result.RemovedPaths, path)
		}
	}
	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success("uninstall", result)
		return nil
	}
	fmt.Fprintln(deps.Stdout, styled(deps, output.StyleSuccess, "Local SpecGate setup removed."))
	if result.KeptData && strings.TrimSpace(stateDir) != "" {
		fmt.Fprintf(deps.Stdout, "Local SQLite data kept in %s. Re-run with --purge-data --yes to remove it.\n", stateDir)
	} else if result.PurgedData && strings.TrimSpace(stateDir) != "" {
		fmt.Fprintf(deps.Stdout, "Local SQLite data removed from %s.\n", stateDir)
	}
	for _, path := range result.PreservedPaths {
		fmt.Fprintf(deps.Stdout, "Unowned files kept under %s.\n", path)
	}
	for _, path := range result.ModifiedPaths {
		fmt.Fprintf(deps.Stdout, "Removed only SpecGate's entries from %s; the file was kept.\n", path)
	}
	return nil
}

func resolveUninstallPurgeDir(deps *Deps, cfg config.Config) (string, error) {
	project := config.ProjectConfig{}
	if root, ok := config.FindProjectRoot(deps.WorkingDir); ok {
		project = cfg.Projects[root]
	}
	dir := config.ResolveLocalDir("", "", project, cfg, "")
	if dir == "" {
		return "", uninstallError(deps, "validation_failed", fmt.Errorf("purging Local SQLite state requires a saved project or global Local store selection in the SpecGate config"))
	}
	target, err := filepath.Abs(dir)
	if err != nil {
		return "", uninstallError(deps, "unavailable", err)
	}
	canonicalTarget, err := canonicalComparisonPath(target)
	if err != nil {
		return "", uninstallError(deps, "unavailable", err)
	}
	envDir := os.Getenv("SPECGATE_LOCAL_DIR")
	for _, path := range []string{envDir, project.Local.Path, cfg.Local.Path} {
		path = strings.TrimSpace(path)
		if path == "" || path == dir {
			continue
		}
		canonical, err := canonicalComparisonPath(path)
		if err != nil {
			return "", uninstallError(deps, "unavailable", err)
		}
		if canonical != canonicalTarget && !sameExistingFile(path, target) {
			err := fmt.Errorf("conflicting Local store selections for purge (SPECGATE_LOCAL_DIR=%q, current project=%q, global=%q). Resolve the store selection before rerunning uninstall",
				strings.TrimSpace(envDir), strings.TrimSpace(project.Local.Path), strings.TrimSpace(cfg.Local.Path))
			return "", uninstallError(deps, "conflict", err)
		}
	}
	return target, nil
}

func uninstallError(deps *Deps, errorCode string, err error) error {
	code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: errorCode, Message: err.Error()})
	return &output.ExitError{Code: code, Err: err}
}

var localSQLiteStateFiles = []string{"state.db", "state.db-wal", "state.db-shm", "state.db-journal"}

func removeLocalSQLiteState(stateDir string) ([]string, error) {
	var removed []string
	if err := validateRealDirectoryIfExists(stateDir); err != nil {
		return removed, err
	}
	var candidates []string
	for _, name := range localSQLiteStateFiles {
		path := filepath.Join(stateDir, name)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return removed, err
		}
		if !info.Mode().IsRegular() {
			return removed, fmt.Errorf("refusing to remove Local SQLite path %s because it is not a regular file", path)
		}
		candidates = append(candidates, path)
	}
	for _, path := range candidates {
		if err := os.Remove(path); err != nil {
			return removed, err
		}
		removed = append(removed, path)
	}
	if len(removed) == 0 {
		return nil, nil
	}
	if empty, err := removeDirIfEmpty(stateDir); err != nil {
		return removed, err
	} else if empty {
		removed = append(removed, stateDir)
		parent := filepath.Dir(stateDir)
		if filepath.Base(parent) == ".specgate" {
			if parentEmpty, err := removeDirIfEmpty(parent); err != nil {
				return removed, err
			} else if parentEmpty {
				removed = append(removed, parent)
			}
		}
	}
	return removed, nil
}

func validateRealDirectoryIfExists(path string) error {
	if err := rejectSymlinkedRemovalAncestors(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s must be a real directory, not a symlink or file", path)
	}
	return nil
}

func validateRegularFileIfExists(path string) error {
	if err := rejectSymlinkedRemovalAncestors(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to remove SpecGate config path %s because it is not a regular file", path)
	}
	return nil
}

func rejectSymlinkedRemovalAncestors(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		for _, alias := range []string{"/tmp", "/var"} {
			if absolute == alias || strings.HasPrefix(absolute, alias+"/") {
				absolute = "/private" + absolute
				break
			}
		}
	}
	for current := filepath.Dir(filepath.Clean(absolute)); ; {
		info, err := os.Lstat(current)
		switch {
		case err == nil && info.Mode()&os.ModeSymlink != 0:
			return fmt.Errorf("refusing removal because path %s contains symlinked ancestor %s", path, current)
		case err != nil && !os.IsNotExist(err):
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

func removeConfigFile(deps *Deps) (bool, error) {
	path, err := configPathForDeps(deps)
	if err != nil {
		return false, err
	}
	if err := validateRegularFileIfExists(path); err != nil {
		return false, err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("refusing to remove SpecGate config path %s because it is not a regular file", path)
	}
	return true, os.Remove(path)
}

func configPathForDeps(deps *Deps) (string, error) {
	if deps.ConfigPath != "" {
		return deps.ConfigPath, nil
	}
	return config.DefaultPath()
}
