package command

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/buildinfo"
	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/deploy"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func registerSystemCommands(root *cobra.Command, deps *Deps) {
	root.AddCommand(newVersionCmd(deps))
	root.AddCommand(newStatusCmd(deps))
	root.AddCommand(newDoctorCmd(deps))
	root.AddCommand(newOpenCmd(deps))
	root.AddCommand(newConfigCmd(deps))
	root.AddCommand(newUpdateCmd(deps))
	root.AddCommand(newUninstallCmd(deps))
	root.AddCommand(newInitCmd(deps))
	root.AddCommand(newUpCmd(deps))
	root.AddCommand(newDownCmd(deps))
	root.AddCommand(newLocalStatusCmd(deps))
	root.AddCommand(newDemoCmd(deps))
	root.AddCommand(newCleanupCmd(deps))
}

func warnIfCLIUpdateRecommended(ctx context.Context, deps *Deps, cmd *cobra.Command) {
	if deps == nil || deps.Client == nil || deps.Printer == nil {
		return
	}
	if deps.versionWarningShown || deps.Printer.Mode() == output.ModeJSON {
		return
	}
	if !shouldCheckCLIUpdate(cmd) || isDevVersion(buildinfo.Version) {
		return
	}
	meta, err := deps.Client.Meta(ctx)
	if err != nil {
		return
	}
	recommended := strings.TrimSpace(meta.RecommendedCLIVersion)
	if recommended == "" {
		recommended = strings.TrimSpace(meta.ServerVersion)
	}
	if isDevVersion(recommended) || versionsMatch(buildinfo.Version, recommended) {
		warnIfGitHubReleaseIsNewer(ctx, deps)
		return
	}
	deps.versionWarningShown = true
	fmt.Fprintln(deps.Stderr, stderrNotice(deps, output.StyleWarning, "Warning", fmt.Sprintf("specgate CLI %s is not the server-recommended version %s. Run `specgate update` to update this install.", buildinfo.Version, recommended)))
}

func warnIfGitHubReleaseIsNewer(ctx context.Context, deps *Deps) {
	if deps.CheckLatestRelease == nil || publicUpdateCheckDisabled() {
		return
	}
	latest, err := deps.CheckLatestRelease(ctx, deps.Timeout, deps.UpdateCheckCachePath)
	if err != nil || !isVersionNewer(latest, buildinfo.Version) {
		return
	}
	deps.versionWarningShown = true
	fmt.Fprintln(deps.Stderr, stderrNotice(deps, output.StyleWarning, "Warning", fmt.Sprintf("specgate CLI %s is older than latest GitHub release %s. Run `curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh` to update this install.", buildinfo.Version, latest)))
}

func publicUpdateCheckDisabled() bool {
	return truthyEnv("SPECGATE_NO_UPDATE_CHECK") || truthyEnv("CI")
}

func truthyEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func shouldCheckCLIUpdate(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	root := cmd.Root()
	if root == nil {
		return false
	}
	args := strings.Fields(strings.TrimPrefix(cmd.CommandPath(), root.Name()))
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "cleanup":
		return !fileCleanupRequested(cmd)
	case "config", "completion", "down", "help", "init", "local-status", "uninstall", "update", "up", "version":
		return false
	default:
		return true
	}
}

func isDevVersion(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "" || v == "dev" || v == "unknown"
}

func versionsMatch(a, b string) bool {
	return normalizeVersion(a) == normalizeVersion(b)
}

func normalizeVersion(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.TrimPrefix(v, "version ")
	v = strings.TrimPrefix(v, "specgate ")
	v = strings.TrimPrefix(v, "v")
	return v
}

// defaultDeployDir returns the default deployment directory (~/.specgate).
func defaultDeployDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".specgate"
	}
	return filepath.Join(home, ".specgate")
}

func userHomeDir(deps *Deps) (string, error) {
	if deps != nil && deps.UserHomeDir != nil {
		return deps.UserHomeDir()
	}
	return os.UserHomeDir()
}

// resolveDeployDir returns the deployment dir from config or the default.
func resolveDeployDir(deps *Deps) string {
	cfg, _ := config.LoadFrom(deps.ConfigPath)
	if cfg.DeploymentDir != "" {
		return cfg.DeploymentDir
	}
	return defaultDeployDir()
}

// makeDeployService creates a deploy.Service, using the injected DeployRunner
// for tests and the production ExecRunner otherwise.
func makeDeployService(deps *Deps, dir string) *deploy.Service {
	runner := deps.DeployRunner
	if runner == nil {
		runner = deploy.ExecRunner{}
	}
	return deploy.New(dir, runner)
}

func validateUsernamePrompt(value string) error {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) < 3 || len(value) > 40 {
		return fmt.Errorf("username must be 3-40 characters")
	}
	for i, r := range value {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if !valid {
			return fmt.Errorf("username can only use lowercase letters, numbers, underscores, and hyphens")
		}
		if i == 0 && !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return fmt.Errorf("username must start with a letter or number")
		}
	}
	return nil
}

// mapAPIError converts a *client.APIError to an output.ErrorPayload.
func mapAPIError(err error) output.ErrorPayload {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		details := apiErr.Details
		switch apiErr.Kind {
		case client.ErrorNotFound:
			return output.ErrorPayload{Code: "not_found", Message: apiErr.Error(), Details: details}
		case client.ErrorConflict:
			return output.ErrorPayload{Code: "conflict", Message: apiErr.Error(), Details: details}
		case client.ErrorUnavailable:
			return output.ErrorPayload{Code: "unavailable", Message: apiErr.Error(), Transient: true, Details: details}
		case client.ErrorIncompatible:
			return output.ErrorPayload{Code: "incompatible", Message: apiErr.Error(), Details: details}
		case client.ErrorUsage:
			return output.ErrorPayload{Code: "validation_failed", Message: apiErr.Error(), Details: details}
		case client.ErrorForbidden:
			return output.ErrorPayload{Code: "governance_failed", Message: apiErr.Error(), Details: details}
		}
	}
	// An *url.Error means the HTTP round trip itself failed — the server never
	// answered. A timeout is distinct from unreachable: the stack is up but the
	// request hung (a slow model-backed gate, or a stalled container engine), so
	// "is the stack running? Try specgate up" would misdirect.
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return output.ErrorPayload{
				Code:      "unavailable",
				Message:   fmt.Sprintf("SpecGate request to %s timed out — the server may be busy (e.g. a slow model-backed gate) or the container engine stalled. Retry, raise --timeout, or run `specgate doctor`.", serverBaseURL(urlErr.URL)),
				Transient: true,
			}
		}
		return output.ErrorPayload{
			Code:      "unavailable",
			Message:   fmt.Sprintf("SpecGate server unreachable at %s — run `specgate doctor`. If this IDE agent runs in a restricted sandbox, rerun it with local-network access; do not start Docker unless the user asks.", serverBaseURL(urlErr.URL)),
			Transient: true,
		}
	}
	return output.ErrorPayload{Code: "unavailable", Message: err.Error()}
}

func apiExitError(deps *Deps, command string, err error) error {
	code := deps.Printer.Error(command, mapAPIError(err))
	return &output.ExitError{Code: code, Err: err}
}

// serverBaseURL reduces a full request URL to scheme://host for error messages.
func serverBaseURL(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Scheme + "://" + u.Host
	}
	return raw
}

// mapWorkRefError converts a work-item resolution failure into a human-facing
// message that names the ref and the next step instead of the internal
// operation name. Used by every command that resolves a work ref.
func mapWorkRefError(ref string, err error) output.ErrorPayload {
	payload := mapAPIError(err)
	var apiErr *client.APIError
	if errors.As(err, &apiErr) && (apiErr.Kind == client.ErrorNotFound || apiErr.Kind == client.ErrorUsage) {
		payload.Message = fmt.Sprintf("work item %q not found — try `specgate work list`", ref)
	}
	return payload
}
