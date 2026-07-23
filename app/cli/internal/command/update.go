package command

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	selfupdate "github.com/contriboss/go-update"
	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/buildinfo"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/deploy"
	"github.com/specgate/specgate/app/cli/internal/output"
)

const (
	maxInstallerScriptBytes      = 1 << 20
	maxReleaseChecksumBytes      = 1 << 20
	maxWindowsUpdateArchiveBytes = 64 << 20
	maxWindowsUpdateBinaryBytes  = 64 << 20

	defaultPublicRegistryURL = "https://raw.githubusercontent.com/thanhtung2693/specgate/main"
	defaultCLIInstallURL     = defaultPublicRegistryURL + "/scripts/install-cli.sh"
	defaultCLIReleaseBaseURL = "https://github.com/thanhtung2693/specgate/releases/download"
)

type updateStepResult struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type progressEvent struct {
	Event   string `json:"event"`
	Command string `json:"command"`
	Step    string `json:"step,omitempty"`
	Message string `json:"message,omitempty"`
}

// specgate update
func newUpdateCmd(deps *Deps) *cobra.Command {
	var (
		deployDir string
		version   string
	)
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update the SpecGate CLI, IDE setup, and Full appliance",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := config.LoadFrom(deps.ConfigPath)
			if err != nil {
				code := deps.Printer.Error("update", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}
			deps.Topology = config.ResolveMode(cfg)
			resolvedVersion, versionErr := resolveUpdateVersion(ctx, deps, version)
			if versionErr != nil {
				err := fmt.Errorf("could not resolve update version: %v; retry or pass --version <release>", versionErr)
				code := deps.Printer.Error("update", output.ErrorPayload{Code: "unavailable", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}
			executablePath, executableErr := updateExecutablePath(deps)
			installArgs := updateInstallerArgsForExecutable(executablePath, executableErr)
			steps := []updateStepResult{
				{ID: "detect_target", Label: "Detect install target", Status: "pending"},
				{ID: "install_cli", Label: "Install CLI", Status: "pending"},
				{ID: "update_plugins", Label: "Update IDE setup", Status: "pending"},
				{ID: "update_stack", Label: "Update Full appliance", Status: "pending"},
			}

			steps[0].Status = "pass"
			if len(installArgs) == 2 {
				steps[0].Message = installArgs[1]
			} else {
				steps[0].Message = "auto"
			}
			emitProgress(deps, "step_succeeded", steps[0].ID, steps[0].Message)

			if deps.Printer.Mode() != output.ModeJSON {
				fmt.Fprintf(deps.Stdout, "%s\n", title(deps, "Step 1/4 Detect install target"))
				fmt.Fprintf(deps.Stdout, "  %s %s\n\n", label(deps, "Using"), steps[0].Message)
				fmt.Fprintf(deps.Stdout, "%s\n", title(deps, "Step 2/4 Install CLI"))
			}
			emitProgress(deps, "step_started", steps[1].ID, steps[1].Label)
			if err := updateCLI(ctx, deps, resolvedVersion, executablePath, executableErr, installArgs); err != nil {
				steps[1].Status = "fail"
				steps[1].Message = err.Error()
				steps[2].Status = "skipped"
				steps[2].Message = "previous step failed"
				steps[3].Status = "skipped"
				steps[3].Message = "previous step failed"
				emitProgress(deps, "step_failed", steps[1].ID, err.Error())
				payload := output.ErrorPayload{Code: "unavailable", Message: err.Error()}
				if deps.Printer.Mode() == output.ModeJSON {
					payload.Details = map[string]any{"steps": steps}
				}
				code := deps.Printer.Error("update", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			steps[1].Status = "pass"
			emitProgress(deps, "step_succeeded", steps[1].ID, "")

			if deps.Printer.Mode() != output.ModeJSON {
				fmt.Fprintf(deps.Stdout, "\n%s\n", title(deps, "Step 3/4 Update IDE setup"))
			}
			emitProgress(deps, "step_started", steps[2].ID, steps[2].Label)
			home, homeErr := userHomeDir(deps)
			installedAgents := []string{}
			if homeErr == nil {
				installedAgents = detectInstalledPluginAgents(home)
			}
			if len(installedAgents) == 0 {
				steps[2].Status = "skipped"
				steps[2].Message = "no global SpecGate IDE setup is installed"
				emitProgress(deps, "step_skipped", steps[2].ID, steps[2].Message)
				if deps.Printer.Mode() != output.ModeJSON {
					fmt.Fprintf(deps.Stdout, "  %s %s\n", styled(deps, output.StyleWarning, "Skipped:"), steps[2].Message)
				}
			} else {
				pluginDeps := *deps
				pluginDeps.PluginRegistryURL = pluginRegistryURLForUpdate(deps, resolvedVersion)
				if _, err := runPluginInstall(ctx, &pluginDeps, pluginInstallOptions{
					Agent:       strings.Join(installedAgents, ","),
					Registry:    pluginDeps.PluginRegistryURL,
					useRegistry: true,
				}); err != nil {
					steps[2].Status = "fail"
					steps[2].Message = err.Error()
					steps[3].Status = "skipped"
					steps[3].Message = "previous step failed"
					emitProgress(deps, "step_failed", steps[2].ID, err.Error())
					payload := pluginInstallErrorPayload(err)
					if deps.Printer.Mode() == output.ModeJSON {
						payload.Details = map[string]any{"steps": steps}
					}
					code := deps.Printer.Error("update", payload)
					return &output.ExitError{Code: code, Err: err}
				}
				if deps.Printer.Mode() != output.ModeJSON {
					fmt.Fprintf(deps.Stdout, "  %s %s (%s)\n", styled(deps, output.StyleSuccess, "Updated SpecGate IDE setup from"), pluginDeps.PluginRegistryURL, strings.Join(installedAgents, ", "))
				}
				steps[2].Status = "pass"
				steps[2].Message = strings.Join(installedAgents, ",")
				emitProgress(deps, "step_succeeded", steps[2].ID, steps[2].Message)
			}

			if deps.Printer.Mode() != output.ModeJSON {
				fmt.Fprintf(deps.Stdout, "\n%s\n", title(deps, "Step 4/4 Update Full appliance"))
			}
			emitProgress(deps, "step_started", steps[3].ID, steps[3].Label)
			if deps.Topology == config.ModeLocal {
				steps[3].Status = "skipped"
				steps[3].Message = "Local mode has no appliance to update"
				emitProgress(deps, "step_skipped", steps[3].ID, steps[3].Message)
				if deps.Printer.Mode() != output.ModeJSON {
					fmt.Fprintf(deps.Stdout, "  %s %s\n", styled(deps, output.StyleWarning, "Skipped:"), steps[3].Message)
				}
			} else {
				dir := deployDir
				if dir == "" {
					dir = resolveDeployDir(deps)
				}
				if _, err := os.Stat(filepath.Join(dir, "compose.yml")); err != nil {
					steps[3].Status = "skipped"
					steps[3].Message = "no CLI-managed deployment found in " + dir
					emitProgress(deps, "step_skipped", steps[3].ID, steps[3].Message)
					if deps.Printer.Mode() != output.ModeJSON {
						fmt.Fprintf(deps.Stdout, "  %s %s\n", styled(deps, output.StyleWarning, "Skipped:"), steps[3].Message)
					}
				} else if err := makeDeployService(deps, dir).Update(ctx, deploy.UpdateOptions{BundleVersion: resolvedVersion, BundleBaseURL: deps.BundleBaseURL}); err != nil {
					steps[3].Status = "fail"
					steps[3].Message = err.Error()
					emitProgress(deps, "step_failed", steps[3].ID, err.Error())
					payload := output.ErrorPayload{Code: "unavailable", Message: err.Error()}
					if deps.Printer.Mode() == output.ModeJSON {
						payload.Details = map[string]any{"steps": steps}
					}
					code := deps.Printer.Error("update", payload)
					return &output.ExitError{Code: code, Err: err}
				} else {
					steps[3].Status = "pass"
					steps[3].Message = resolvedVersion
					emitProgress(deps, "step_succeeded", steps[3].ID, resolvedVersion)
					if deps.Printer.Mode() != output.ModeJSON {
						fmt.Fprintf(deps.Stdout, "  %s %s to %s\n", styled(deps, output.StyleSuccess, "Updated Full appliance in"), dir, resolvedVersion)
					}
				}
			}

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("update", map[string]any{"steps": steps})
				return nil
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "Release version to install (default: latest GitHub release)")
	cmd.Flags().StringVar(&deployDir, "dir", "", "Deployment directory to update (default: saved deployment dir or ~/.specgate)")
	return cmd
}

func publicRegistryURL(deps *Deps) string {
	if deps != nil && strings.TrimSpace(deps.PublicRegistryURL) != "" {
		return strings.TrimRight(strings.TrimSpace(deps.PublicRegistryURL), "/")
	}
	return defaultPublicRegistryURL
}

func publicRegistryURLForVersion(deps *Deps, version string) string {
	if deps != nil && strings.TrimSpace(deps.PublicRegistryURL) != "" {
		return publicRegistryURL(deps)
	}
	if strings.TrimSpace(version) == "" {
		return defaultPublicRegistryURL
	}
	return "https://raw.githubusercontent.com/thanhtung2693/specgate/" + strings.TrimSpace(version)
}

func pluginRegistryURLForUpdate(deps *Deps, version string) string {
	return publicRegistryURLForVersion(deps, version)
}

func resolveUpdateVersion(ctx context.Context, deps *Deps, explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit), nil
	}
	if deps != nil && deps.CheckLatestRelease != nil {
		return deps.CheckLatestRelease(ctx, deps.Timeout, deps.UpdateCheckCachePath)
	}
	if !isDevVersion(buildinfo.Version) {
		return buildinfo.Version, nil
	}
	return "", fmt.Errorf("no published version available from this CLI build")
}

// fetchAndRunScript fetches a shell script from url and pipes it to sh.
// extraArgs, if non-empty, are appended after "sh -s --" so the script sees them as $@.
func fetchAndRunScript(ctx context.Context, deps *Deps, url string, extraArgs ...string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	httpClient := &http.Client{Timeout: deps.Timeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d for %s", resp.StatusCode, url)
	}
	script, err := io.ReadAll(io.LimitReader(resp.Body, maxInstallerScriptBytes+1))
	if err != nil {
		return err
	}
	if len(script) > maxInstallerScriptBytes {
		return fmt.Errorf("installer script exceeds %d bytes", maxInstallerScriptBytes)
	}

	args := []string{"-s", "--"}
	args = append(args, extraArgs...)
	sh := exec.CommandContext(ctx, "sh", args...)
	sh.Stdin = bytes.NewReader(script)
	sh.Stdout = deps.Stdout
	sh.Stderr = deps.Stderr
	return sh.Run()
}

func updateInstallerArgs() []string {
	exe, err := updateExecutablePath(nil)
	return updateInstallerArgsForExecutable(exe, err)
}

func updateExecutablePath(deps *Deps) (string, error) {
	executable := os.Executable
	if deps != nil && deps.ExecutablePath != nil {
		executable = deps.ExecutablePath
	}
	exe, err := executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		if err == nil {
			err = fmt.Errorf("executable path is empty")
		}
		return "", err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(exe); resolveErr == nil && resolved != "" {
		exe = resolved
	}
	return exe, nil
}

func updateInstallerArgsForExecutable(exe string, err error) []string {
	if err != nil || strings.TrimSpace(exe) == "" {
		return nil
	}
	dir := strings.TrimSpace(filepath.Dir(exe))
	if dir == "" || dir == "." || dir == string(filepath.Separator) {
		return nil
	}
	return []string{"--install-dir", dir}
}

func updateCLI(
	ctx context.Context,
	deps *Deps,
	version string,
	executablePath string,
	executableErr error,
	installArgs []string,
) error {
	goos := runtime.GOOS
	if deps != nil && strings.TrimSpace(deps.RuntimeGOOS) != "" {
		goos = strings.TrimSpace(deps.RuntimeGOOS)
	}
	if goos == "windows" {
		if executableErr != nil {
			return fmt.Errorf("locate current SpecGate executable: %w", executableErr)
		}
		if deps != nil && deps.SelfUpdateCLI != nil {
			return deps.SelfUpdateCLI(ctx, version, executablePath)
		}
		return selfUpdateWindowsCLI(ctx, deps, version, executablePath, runtime.GOARCH)
	}

	registryURL := publicRegistryURLForVersion(deps, version)
	cliInstallURL := strings.TrimSpace(deps.CLIInstallURL)
	if cliInstallURL == "" {
		cliInstallURL = registryURL + "/scripts/install-cli.sh"
	}
	cliInstallArgs := append([]string(nil), installArgs...)
	if version != "" {
		cliInstallArgs = append(cliInstallArgs, "--version", version)
	}
	return fetchAndRunScript(ctx, deps, cliInstallURL, cliInstallArgs...)
}

func selfUpdateWindowsCLI(ctx context.Context, deps *Deps, version, executablePath, arch string) error {
	if err := validateReleaseVersion(version); err != nil {
		return err
	}
	if arch != "amd64" {
		return fmt.Errorf("windows %s updates are not published; install a supported release manually", arch)
	}

	releaseBaseURL := defaultCLIReleaseBaseURL
	if deps != nil && strings.TrimSpace(deps.CLIReleaseBaseURL) != "" {
		releaseBaseURL = strings.TrimRight(strings.TrimSpace(deps.CLIReleaseBaseURL), "/")
	}
	releaseVersion := strings.TrimPrefix(version, "v")
	archiveName := fmt.Sprintf("specgate_%s_windows_%s.zip", releaseVersion, arch)
	checksumName := fmt.Sprintf("specgate_%s_checksums.txt", releaseVersion)
	releaseURL := releaseBaseURL + "/" + version + "/"

	archive, err := downloadUpdateFile(ctx, deps, releaseURL+archiveName, maxWindowsUpdateArchiveBytes)
	if err != nil {
		return fmt.Errorf("download windows CLI archive: %w", err)
	}
	checksums, err := downloadUpdateFile(ctx, deps, releaseURL+checksumName, maxReleaseChecksumBytes)
	if err != nil {
		return fmt.Errorf("download windows CLI checksums: %w", err)
	}
	expectedChecksum, err := checksumForReleaseAsset(checksums, archiveName)
	if err != nil {
		return err
	}
	actualChecksum := sha256.Sum256(archive)
	if subtle.ConstantTimeCompare(actualChecksum[:], expectedChecksum) != 1 {
		return fmt.Errorf("checksum mismatch for %s", archiveName)
	}

	binary, err := windowsExecutableFromArchive(archive)
	if err != nil {
		return err
	}
	if err := selfupdate.Apply(bytes.NewReader(binary), selfupdate.Options{
		TargetPath: executablePath,
		TargetMode: 0o755,
		Lock:       true,
	}); err != nil {
		if rollbackErr := selfupdate.RollbackError(err); rollbackErr != nil {
			return fmt.Errorf("replace windows CLI: %w (rollback failed: %v)", err, rollbackErr)
		}
		return fmt.Errorf("replace windows CLI: %w", err)
	}
	return nil
}

func validateReleaseVersion(version string) error {
	if version == "" || version == "." || version == ".." {
		return fmt.Errorf("invalid release version %q", version)
	}
	for _, char := range version {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			strings.ContainsRune("._+-", char) {
			continue
		}
		return fmt.Errorf("invalid release version %q", version)
	}
	return nil
}

func downloadUpdateFile(ctx context.Context, deps *Deps, url string, maxBytes int64) ([]byte, error) {
	timeout := defaultTimeout
	if deps != nil && deps.Timeout > 0 {
		timeout = deps.Timeout
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d for %s", resp.StatusCode, url)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("download exceeds %d bytes", maxBytes)
	}
	return data, nil
}

func checksumForReleaseAsset(checksums []byte, assetName string) ([]byte, error) {
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != assetName {
			continue
		}
		checksum, err := hex.DecodeString(fields[0])
		if err != nil || len(checksum) != sha256.Size {
			return nil, fmt.Errorf("invalid checksum for %s", assetName)
		}
		return checksum, nil
	}
	return nil, fmt.Errorf("checksum for %s was not found", assetName)
}

func windowsExecutableFromArchive(archive []byte) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("open windows CLI archive: %w", err)
	}
	var (
		binary []byte
		found  bool
	)
	for _, file := range reader.File {
		if file.Name != "specgate.exe" {
			continue
		}
		if found {
			return nil, fmt.Errorf("windows CLI archive contains duplicate specgate.exe")
		}
		found = true
		if file.FileInfo().IsDir() || !file.Mode().IsRegular() {
			return nil, fmt.Errorf("windows CLI archive contains invalid specgate.exe")
		}
		if file.UncompressedSize64 > maxWindowsUpdateBinaryBytes {
			return nil, fmt.Errorf("windows CLI executable exceeds %d bytes", maxWindowsUpdateBinaryBytes)
		}
		contents, openErr := file.Open()
		if openErr != nil {
			return nil, fmt.Errorf("open specgate.exe: %w", openErr)
		}
		binary, err = io.ReadAll(io.LimitReader(contents, maxWindowsUpdateBinaryBytes+1))
		closeErr := contents.Close()
		if err != nil {
			return nil, fmt.Errorf("read specgate.exe: %w", err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close specgate.exe: %w", closeErr)
		}
		if len(binary) > maxWindowsUpdateBinaryBytes {
			return nil, fmt.Errorf("windows CLI executable exceeds %d bytes", maxWindowsUpdateBinaryBytes)
		}
	}
	if !found || len(binary) == 0 {
		return nil, fmt.Errorf("windows CLI archive does not contain specgate.exe")
	}
	return binary, nil
}

func emitProgress(deps *Deps, event, step, message string) {
	if deps.Printer == nil || deps.Printer.Mode() != output.ModeJSON || !deps.JSONProgress {
		return
	}
	data, err := json.Marshal(progressEvent{
		Event:   event,
		Command: "update",
		Step:    step,
		Message: message,
	})
	if err != nil {
		return
	}
	fmt.Fprintf(deps.Stdout, "%s\n", data)
}
