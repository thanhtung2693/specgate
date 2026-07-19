package command

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/buildinfo"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/deploy"
	"github.com/specgate/specgate/app/cli/internal/output"
)

const maxInstallerScriptBytes = 1 << 20

const (
	defaultPublicRegistryURL = "https://raw.githubusercontent.com/thanhtung2693/specgate/main"
	defaultCLIInstallURL     = defaultPublicRegistryURL + "/scripts/install-cli.sh"
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
			installArgs := updateInstallerArgs()
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
			registryURL := publicRegistryURLForVersion(deps, resolvedVersion)
			cliInstallURL := strings.TrimSpace(deps.CLIInstallURL)
			if cliInstallURL == "" {
				cliInstallURL = registryURL + "/scripts/install-cli.sh"
			}
			cliInstallArgs := append([]string(nil), installArgs...)
			if resolvedVersion != "" {
				cliInstallArgs = append(cliInstallArgs, "--version", resolvedVersion)
			}
			emitProgress(deps, "step_started", steps[1].ID, steps[1].Label)
			if err := fetchAndRunScript(ctx, deps, cliInstallURL, cliInstallArgs...); err != nil {
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
	exe, err := os.Executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		return nil
	}
	if resolved, resolveErr := filepath.EvalSymlinks(exe); resolveErr == nil && resolved != "" {
		exe = resolved
	}
	dir := strings.TrimSpace(filepath.Dir(exe))
	if dir == "" || dir == "." || dir == string(filepath.Separator) {
		return nil
	}
	return []string{"--install-dir", dir}
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
