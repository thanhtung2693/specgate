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
	"github.com/specgate/specgate/app/cli/internal/deploy"
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
	KeptData       bool     `json:"kept_data"`
}

// specgate uninstall
func newUninstallCmd(deps *Deps) *cobra.Command {
	var (
		deployDir     string
		purgeData     bool
		removePlugins = true
	)
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove this user's SpecGate setup",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.LoadFrom(deps.ConfigPath)
			if err != nil {
				code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}
			if config.ResolveMode(cfg) == config.ModeLocal {
				if cmd.Flags().Changed("dir") {
					err := fmt.Errorf("--dir is available only in Full mode; Local mode uses the configured SQLite state directory. Remove --dir and rerun `specgate uninstall`")
					code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "validation_failed", Message: err.Error()})
					return &output.ExitError{Code: code, Err: err}
				}
				return runLocalUninstall(cmd, deps, cfg.Local.Path, purgeData, removePlugins)
			}
			dir := deployDir
			if dir == "" {
				dir = resolveDeployDir(deps)
			}
			if canPrompt(deps) && !cmd.Flags().Changed("purge-data") {
				choices, err := deps.Prompter.MultiSelect("Remove SpecGate setup", []interactive.Option{
					{Label: "IDE plugin files", Value: "plugins"},
					{Label: "Local data (Docker volumes and deployment directory)", Value: "data"},
				}, []string{"plugins"})
				if err != nil {
					return err
				}
				removePlugins = slices.Contains(choices, "plugins")
				purgeData = slices.Contains(choices, "data")
			}
			if purgeData && !deps.Yes {
				if !canPrompt(deps) {
					err := fmt.Errorf("--purge-data permanently removes the deployment directory; pass --yes to confirm")
					code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "validation_failed", Message: err.Error()})
					return &output.ExitError{Code: code, Err: err}
				}
			}
			if purgeData && strings.TrimSpace(dir) != "" && deps.Printer.Mode() != output.ModeJSON {
				fmt.Fprintf(deps.Stderr, "Warning: Full purge permanently removes SpecGate-managed Docker volumes, containers, networks, and deployment directory %s. Repository files and container images are preserved.\n", dir)
			}

			result := uninstallResult{Dir: dir, PurgedData: purgeData, KeptData: !purgeData, RemovedPaths: []string{}}
			svc := makeDeployService(deps, dir)
			if purgeData {
				if err := validateFullPurgeDir(deps, dir); err != nil {
					code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "validation_failed", Message: err.Error()})
					return &output.ExitError{Code: code, Err: err}
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "compose.yml")); err == nil {
				if !purgeData {
					if err := deploy.ValidateManagedDirectory(dir); err != nil {
						code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "validation_failed", Message: err.Error()})
						return &output.ExitError{Code: code, Err: err}
					}
				}
				var downErr error
				if purgeData {
					downErr = svc.DownWithVolumes(cmd.Context())
				} else {
					downErr = svc.Down(cmd.Context())
				}
				if downErr != nil {
					code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "unavailable", Message: downErr.Error()})
					return &output.ExitError{Code: code, Err: downErr}
				}
				result.StoppedStack = true
			}
			if purgeData {
				if err := svc.RemoveLabeledResources(cmd.Context()); err != nil {
					code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
					return &output.ExitError{Code: code, Err: err}
				}
			}
			if purgeData {
				if removed, err := removePathIfExists(dir); err != nil {
					code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
					return &output.ExitError{Code: code, Err: err}
				} else if removed {
					result.RemovedPaths = append(result.RemovedPaths, dir)
				}
			}
			if removePlugins {
				removed, paths, preserved, err := removeSpecGatePluginFiles(deps)
				if err != nil {
					code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
					return &output.ExitError{Code: code, Err: err}
				}
				result.RemovedPlugins = removed
				result.RemovedPaths = append(result.RemovedPaths, paths...)
				result.PreservedPaths = append(result.PreservedPaths, preserved...)
			}
			configRemoved, err := removeConfigFile(deps)
			if err != nil {
				code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
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
			fmt.Fprintln(deps.Stdout, styled(deps, output.StyleSuccess, "SpecGate user setup removed."))
			if result.StoppedStack {
				fmt.Fprintln(deps.Stdout, styled(deps, output.StyleSuccess, "Full appliance stopped."))
			}
			if result.KeptData {
				fmt.Fprintf(deps.Stdout, "Local data kept in %s. Re-run with --purge-data --yes to remove it.\n", dir)
			} else {
				fmt.Fprintf(deps.Stdout, "Local data removed from %s.\n", dir)
			}
			for _, path := range result.PreservedPaths {
				fmt.Fprintf(deps.Stdout, "Unowned files kept under %s.\n", path)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&deployDir, "dir", "", "Deployment directory (default: saved deployment dir or ~/.specgate)")
	cmd.Flags().BoolVar(&purgeData, "purge-data", false, "Remove Local SQLite state or Full appliance data")
	return cmd
}

func runLocalUninstall(cmd *cobra.Command, deps *Deps, stateDir string, purgeData, removePlugins bool) error {
	if canPrompt(deps) && !cmd.Flags().Changed("purge-data") {
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
	if purgeData && !deps.Yes && !canPrompt(deps) {
		err := fmt.Errorf("--purge-data permanently removes Local SQLite state; pass --yes to confirm")
		code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "validation_failed", Message: err.Error()})
		return &output.ExitError{Code: code, Err: err}
	}
	if purgeData && strings.TrimSpace(stateDir) != "" && deps.Printer.Mode() != output.ModeJSON {
		fmt.Fprintf(deps.Stderr, "Warning: Local purge removes only SpecGate SQLite files in %s (state.db and its SQLite journal files). Other files in this directory are preserved.\n", stateDir)
	}
	if purgeData && strings.TrimSpace(stateDir) != "" {
		if err := validateRealDirectoryIfExists(stateDir); err != nil {
			code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
			return &output.ExitError{Code: code, Err: err}
		}
	}
	configPath, err := configPathForDeps(deps)
	if err != nil {
		code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
		return &output.ExitError{Code: code, Err: err}
	}
	if err := validateRegularFileIfExists(configPath); err != nil {
		code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
		return &output.ExitError{Code: code, Err: err}
	}

	result := uninstallResult{Dir: stateDir, PurgedData: purgeData, RemovedPaths: []string{}, KeptData: !purgeData}
	if purgeData && strings.TrimSpace(stateDir) != "" {
		removed, err := removeLocalSQLiteState(stateDir)
		if err != nil {
			code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
			return &output.ExitError{Code: code, Err: err}
		}
		result.RemovedPaths = append(result.RemovedPaths, removed...)
	}
	if removePlugins {
		removed, paths, preserved, err := removeSpecGatePluginFiles(deps)
		if err != nil {
			code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
			return &output.ExitError{Code: code, Err: err}
		}
		result.RemovedPlugins = removed
		result.RemovedPaths = append(result.RemovedPaths, paths...)
		result.PreservedPaths = append(result.PreservedPaths, preserved...)
	}
	configRemoved, err := removeConfigFile(deps)
	if err != nil {
		code := deps.Printer.Error("uninstall", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
		return &output.ExitError{Code: code, Err: err}
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
	return nil
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

func validateFullPurgeDir(deps *Deps, dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	abs = filepath.Clean(abs)
	if abs == filepath.Clean(string(filepath.Separator)) {
		return fmt.Errorf("refusing to purge filesystem root")
	}
	if home, homeErr := userHomeDir(deps); homeErr == nil && abs == filepath.Clean(home) {
		return fmt.Errorf("refusing to purge user home directory %s", abs)
	}
	if _, err := os.Stat(filepath.Join(abs, ".git")); err == nil {
		return fmt.Errorf("refusing to purge Git repository root %s", abs)
	}
	return deploy.ValidateManagedDirectory(abs)
}

func configPathForDeps(deps *Deps) (string, error) {
	if deps.ConfigPath != "" {
		return deps.ConfigPath, nil
	}
	return config.DefaultPath()
}
