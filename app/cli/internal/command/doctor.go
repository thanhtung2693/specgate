package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/deploy"
	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/output"
)

const apiVersion = "specgate.api/v1"

type doctorResult struct {
	Mode                config.Mode `json:"mode"`
	Server              doctorCheck `json:"server"`
	Components          doctorCheck `json:"components,omitempty"`
	Identity            doctorCheck `json:"identity"`
	Workspace           doctorCheck `json:"workspace"`
	WorkspaceMember     doctorCheck `json:"workspace_member"`
	DatabaseSchema      doctorCheck `json:"database_schema"`
	Model               doctorCheck `json:"model"`
	KnowledgeEmbeddings doctorCheck `json:"knowledge_embeddings"`
	Plugins             doctorCheck `json:"plugins"`
	Next                string      `json:"next,omitempty"`
}

type doctorCheck struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Command string `json:"command,omitempty"`
}

// specgate doctor
func newDoctorCmd(deps *Deps) *cobra.Command {
	var fix bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check Local or Full setup health",
		Example: strings.TrimSpace(`specgate doctor
specgate doctor --fix
specgate plugins doctor
specgate model test`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if deps.Topology == config.ModeLocal {
				if fix {
					return incompatibleCommand(
						deps,
						"doctor",
						"`doctor --fix` repairs only a CLI-managed Full appliance; run `specgate doctor` without --fix in Local mode",
					)
				}
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "doctor", err)
				}
				defer store.Close()
				selection, err := localSelection(cmd.Context(), deps, store)
				if err != nil {
					return localExitError(deps, "doctor", err)
				}
				cfg, _ := config.LoadFrom(deps.ConfigPath)
				result := map[string]any{
					"mode":      "local",
					"store":     map[string]any{"path": cfg.Local.Path, "id": selection.StoreID, "status": "ok"},
					"identity":  map[string]any{"status": "ok", "username": selection.User.Username},
					"workspace": map[string]any{"status": "ok", "slug": selection.Workspace.Slug},
					"network":   map[string]any{"status": "not_required", "message": "Local mode uses no server or TCP service"},
				}
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("doctor", result)
					return nil
				}
				fmt.Fprintln(deps.Stdout, title(deps, "SpecGate Doctor"))
				fmt.Fprintln(deps.Stdout, notice(deps, output.StyleSuccess, "Local mode", "ready"))
				fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Store:"), cfg.Local.Path)
				fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "User:"), selection.User.Username)
				fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Workspace:"), selection.Workspace.Slug)
				fmt.Fprintf(deps.Stdout, "%s not required\n", label(deps, "Network:"))
				return nil
			}
			ctx := cmd.Context()

			meta, components, payload, err := runDoctorChecks(ctx, deps)
			if err != nil {
				if fix && payload.Code == "unavailable" {
					if repairErr := runDoctorFix(ctx, deps); repairErr != nil {
						code := deps.Printer.Error("doctor", output.ErrorPayload{Code: "unavailable", Message: repairErr.Error()})
						return &output.ExitError{Code: code, Err: repairErr}
					}
					meta, components, payload, err = runDoctorChecks(ctx, deps)
					if err == nil && deps.Printer.Mode() != output.ModeJSON {
						fmt.Fprintln(deps.Stdout, styled(deps, output.StyleSuccess, "Environment repaired successfully."))
					}
				}
			}
			if err != nil {
				code := deps.Printer.Error("doctor", payload)
				return &output.ExitError{Code: code, Err: err}
			}

			result := buildDoctorSummary(ctx, deps, meta, components)
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("doctor", result)
				return nil
			}
			printDoctorSummary(deps, result)
			printDoctorFullAppliance(ctx, deps)
			return nil
		},
	}
	cmd.Flags().BoolVar(&fix, "fix", false, "Repair a CLI-managed Full appliance when unavailable")
	return cmd
}

func buildDoctorSummary(ctx context.Context, deps *Deps, meta *client.Meta, components *client.ComponentHealth) doctorResult {
	cfg, _ := config.LoadFrom(deps.ConfigPath)
	workspace := resolveWorkspaceSelection(deps, cfg)
	result := doctorResult{
		Mode: config.ModeFull,
		Server: doctorCheck{
			Status:  "ok",
			Message: fmt.Sprintf("%s version=%s api=%s", deps.ServerURL, doctorMetaVersion(meta), meta.APIVersion),
		},
		Plugins: doctorCheck{
			Status:  "check",
			Message: "IDE plugin files are optional unless you use Codex, Cursor, or Claude Code.",
			Command: "specgate plugins doctor",
		},
		Next: "Run `specgate status` to inspect governed work.",
	}
	result.Components = doctorComponentCheck(components)
	result.DatabaseSchema = doctorDatabaseSchemaCheck(ctx, deps)

	if cfg.CurrentUser.Username == "" && cfg.CurrentUser.DisplayName == "" {
		result.Identity = doctorCheck{Status: "missing", Message: "No local user is selected.", Command: "specgate user login"}
		result.Next = "Run `specgate user login` to create or select a local user."
	} else {
		label := cfg.CurrentUser.Username
		if label == "" {
			label = cfg.CurrentUser.DisplayName
		}
		result.Identity = doctorCheck{Status: "ok", Message: label}
	}

	if workspace.Source == config.WorkspaceSourceNone {
		result.Workspace = doctorCheck{Status: "missing", Message: "No workspace is selected.", Command: "specgate workspace select"}
		if result.Identity.Status == "ok" {
			result.Next = "Run `specgate workspace select` to choose a workspace."
		}
	} else {
		message := formatWorkspaceLabel(workspace.Workspace)
		if workspace.Source != "" {
			message = fmt.Sprintf("%s via %s", message, formatWorkspaceSource(workspace))
		}
		workspaceID, err := workspaceIDForSelection(ctx, deps, workspace)
		if err != nil {
			if apiErr, ok := err.(*client.APIError); ok && apiErr.Kind == client.ErrorNotFound {
				result.Workspace = doctorCheck{Status: "missing", Message: "Selected workspace no longer exists.", Command: "specgate workspace select"}
				if result.Identity.Status == "ok" {
					result.Next = "Run `specgate workspace select` to choose an existing workspace."
				}
			} else {
				result.Workspace = doctorCheck{Status: "unknown", Message: "Could not resolve selected workspace.", Command: "specgate workspace current"}
			}
		} else if _, err := deps.Client.GetWorkspace(ctx, workspaceID); err != nil {
			if apiErr, ok := err.(*client.APIError); ok && apiErr.Kind == client.ErrorNotFound {
				result.Workspace = doctorCheck{Status: "missing", Message: "Selected workspace no longer exists.", Command: "specgate workspace select"}
				if result.Identity.Status == "ok" {
					result.Next = "Run `specgate workspace select` to choose an existing workspace."
				}
			} else {
				result.Workspace = doctorCheck{Status: "unknown", Message: "Could not verify selected workspace.", Command: "specgate workspace current"}
			}
		} else {
			result.Workspace = doctorCheck{Status: "ok", Message: message}
			result.WorkspaceMember = doctorWorkspaceMemberCheck(ctx, deps, cfg, workspace)
		}
	}

	settings, err := deps.Client.GetSettings(ctx)
	if err != nil {
		result.Model = doctorCheck{Status: "unknown", Message: "Could not read model settings.", Command: "specgate model show"}
		return result
	}
	modelStatus := modelSettingsStatus(modelView(settings))
	if modelStatus.NeedsSetup {
		result.Model = doctorCheck{Status: "missing", Message: modelStatus.Message, Command: "specgate model set"}
		if result.Identity.Status == "ok" && result.Workspace.Status == "ok" {
			result.Next = "Run `specgate model set` to configure governance LLM review."
		}
	} else {
		msg := fmt.Sprintf("%s %s", modelStatus.Provider, modelStatus.Model)
		// Surface the thinking-level lever: governance judgment quality/latency is
		// tuned by governance.default_thinking_level (low|medium|high). The agent
		// runtime defaults to low; medium is the recommended first upgrade for
		// model-backed delivery review. See docs/using-specgate/guides/configure-models.md.
		if tl := strings.TrimSpace(settings["governance.default_thinking_level"]); tl != "" {
			msg += fmt.Sprintf(" (thinking: %s)", tl)
		}
		result.Model = doctorCheck{Status: "ok", Message: msg}
	}
	result.KnowledgeEmbeddings = embeddingSettingsStatus(settings)
	return result
}

func doctorDatabaseSchemaCheck(ctx context.Context, deps *Deps) doctorCheck {
	schemaClient, ok := deps.Client.(interface {
		SchemaStatus(context.Context) (*client.SchemaStatus, error)
	})
	if !ok {
		return doctorCheck{Status: "unknown", Message: "Database schema compatibility check is unavailable."}
	}
	workspaceID, err := currentWorkspaceID(ctx, deps)
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.Kind == client.ErrorNotFound {
			return doctorCheck{Status: "unknown", Message: "Select an existing workspace to verify database schema compatibility.", Command: "specgate workspace select"}
		}
		return doctorCheck{Status: "unknown", Message: "Could not resolve the selected workspace for database schema compatibility.", Command: "specgate workspace current"}
	}
	if workspaceID == "" {
		return doctorCheck{Status: "unknown", Message: "Select a workspace to verify database schema compatibility.", Command: "specgate workspace select"}
	}
	status, err := schemaClient.SchemaStatus(client.WithWorkspace(ctx, workspaceID))
	if err != nil {
		return doctorCheck{Status: "unknown", Message: "Could not verify database schema compatibility.", Command: "specgate doctor"}
	}
	if status == nil {
		return doctorCheck{Status: "unknown", Message: "Server does not expose database schema compatibility diagnostics."}
	}
	check := doctorCheck{Status: status.Status, Message: status.Message}
	if !strings.EqualFold(status.Status, "ok") {
		check.Command = "specgate doctor"
	}
	return check
}

// embeddingSettingsStatus reports Knowledge embedding readiness separately from
// the chat/gate model. It uses the same key-presence normalization as the model
// check (providerAPIKeyIsSet): raw settings carry the redacted key value, so a
// non-empty raw value means the key is set — comparing against the literal "set"
// (which only exists in modelView's normalized output) would misreport every
// configured key as missing.
func embeddingSettingsStatus(settings map[string]string) doctorCheck {
	provider := strings.TrimSpace(settings["embedding.model_provider"])
	modelID := strings.TrimSpace(settings["embedding.model"])
	if provider == "" || modelID == "" {
		return doctorCheck{
			Status:  "missing",
			Message: "Knowledge embeddings are not configured; Knowledge upload/search is unavailable.",
			Command: "specgate model set",
		}
	}
	if !providerAPIKeyIsSet(settings, provider) {
		return doctorCheck{
			Status:  "missing",
			Message: fmt.Sprintf("Knowledge embeddings are not configured: API key is not set for %s; Knowledge upload/search is unavailable.", provider),
			Command: "specgate model set",
		}
	}
	return doctorCheck{Status: "ok", Message: fmt.Sprintf("%s %s", provider, modelID)}
}

func doctorWorkspaceMemberCheck(ctx context.Context, deps *Deps, cfg config.Config, workspace config.ResolvedWorkspace) doctorCheck {
	if cfg.CurrentUser.ID == "" && cfg.CurrentUser.Username == "" {
		return doctorCheck{Status: "unknown", Message: "No local user is selected.", Command: "specgate user login"}
	}
	workspaceID, err := workspaceIDForSelection(ctx, deps, workspace)
	if err != nil || workspaceID == "" {
		return doctorCheck{Status: "unknown", Message: "Could not resolve workspace membership.", Command: "specgate workspace members"}
	}
	members, err := deps.Client.ListWorkspaceMembers(ctx, workspaceID, cfg.CurrentUser.ID, cfg.CurrentUser.Username)
	if err != nil {
		return doctorCheck{Status: "unknown", Message: "Could not read workspace members.", Command: "specgate workspace members"}
	}
	label := cfg.CurrentUser.Username
	if label == "" {
		label = cfg.CurrentUser.DisplayName
	}
	for _, member := range members.Members {
		if member.Current {
			return doctorCheck{Status: "ok", Message: fmt.Sprintf("%s is listed in workspace members.", label)}
		}
	}
	return doctorCheck{Status: "not_member", Message: fmt.Sprintf("%s is not listed in workspace members.", label), Command: "specgate workspace members"}
}

func doctorMetaVersion(meta *client.Meta) string {
	if meta == nil {
		return ""
	}
	if strings.TrimSpace(meta.ServerVersion) != "" {
		return meta.ServerVersion
	}
	return "unknown"
}

func printDoctorSummary(deps *Deps, result doctorResult) {
	printDoctorLine(deps, "Server", result.Server)
	if result.Components.Status != "" {
		printDoctorLine(deps, "Components", result.Components)
	}
	printDoctorLine(deps, "Identity", result.Identity)
	printDoctorLine(deps, "Workspace", result.Workspace)
	if result.WorkspaceMember.Status != "" {
		printDoctorLine(deps, "Member", result.WorkspaceMember)
	}
	printDoctorLine(deps, "DB schema", result.DatabaseSchema)
	printDoctorLine(deps, "Model", result.Model)
	printDoctorLine(deps, "Embeddings", result.KnowledgeEmbeddings)
	printDoctorLine(deps, "IDE plugins", result.Plugins)
	if strings.TrimSpace(result.Next) != "" {
		fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Next:"), styled(deps, output.StyleAction, result.Next))
	}
}

func printDoctorLine(deps *Deps, name string, check doctorCheck) {
	fmt.Fprintf(deps.Stdout, "%-12s %-14s %s\n", label(deps, name+":"), styledStatusPadded(deps, strings.ToUpper(check.Status), 12), check.Message)
	if check.Command != "" {
		fmt.Fprintf(deps.Stdout, "%-12s          %s\n", "", styled(deps, output.StyleAction, check.Command))
	}
}

func runDoctorChecks(ctx context.Context, deps *Deps) (*client.Meta, *client.ComponentHealth, output.ErrorPayload, error) {
	health, err := componentHealth(ctx, deps)
	if err != nil {
		return nil, nil, output.ErrorPayload{Code: "unavailable", Message: fmt.Sprintf("appliance diagnostics unavailable: %v", err)}, err
	}
	if health != nil && !strings.EqualFold(health.Status, "ok") {
		message := failedComponentMessage(health)
		err := errors.New(message)
		return nil, health, output.ErrorPayload{Code: "unavailable", Message: message}, err
	}

	// A missing health endpoint remains compatible with origin-only servers;
	// an advertised but failing appliance health endpoint is not healthy.
	if err := deps.Client.Healthz(ctx); err != nil {
		return nil, health, output.ErrorPayload{Code: "unavailable", Message: fmt.Sprintf("server health check failing: %v", err)}, err
	}

	meta, err := deps.Client.Meta(ctx)
	if err != nil {
		return nil, health, mapAPIError(err), err
	}

	if meta.APIVersion != apiVersion {
		return nil, health, output.ErrorPayload{
			Code:    "incompatible",
			Message: fmt.Sprintf("server api_version %q is not supported (want %q)", meta.APIVersion, apiVersion),
		}, fmt.Errorf("server api_version %q is not supported", meta.APIVersion)
	}

	agents, set := meta.CapabilityDetails["agents"]
	if !set || (agents.State != "available" && agents.State != "unavailable") {
		return nil, health, output.ErrorPayload{
			Code:    "incompatible",
			Message: "server did not report a valid agents capability state",
		}, fmt.Errorf("server did not report a valid agents capability state")
	}
	if agents.State == "unavailable" {
		return nil, health, output.ErrorPayload{
			Code:    "unavailable",
			Message: "agents service is not available on this server",
		}, fmt.Errorf("agents service is not available on this server")
	}

	// Probe the selected workspace's board when one is available. The board is
	// workspace-scoped, while doctor must still be able to report that no
	// workspace is configured.
	cfg, _ := config.LoadFrom(deps.ConfigPath)
	workspaceID, err := workspaceIDForSelection(ctx, deps, resolveWorkspaceSelection(deps, cfg))
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.Kind == client.ErrorNotFound {
			return meta, health, output.ErrorPayload{}, nil
		}
		return nil, health, mapAPIError(err), err
	}
	if workspaceID != "" {
		if _, err := deps.Client.Status(ctx, workspaceID); err != nil {
			return nil, health, output.ErrorPayload{
				Code:    "unavailable",
				Message: fmt.Sprintf("board endpoint failing (status/work list will not work): %v", err),
			}, err
		}
	}

	return meta, health, output.ErrorPayload{}, nil
}

func componentHealth(ctx context.Context, deps *Deps) (*client.ComponentHealth, error) {
	componentClient, ok := deps.Client.(interface {
		ComponentHealth(context.Context) (*client.ComponentHealth, error)
	})
	if !ok {
		return nil, nil
	}
	health, err := componentClient.ComponentHealth(ctx)
	if err == nil {
		return health, nil
	}
	dir := resolveDeployDir(deps)
	composePath := filepath.Join(dir, "compose.yml")
	if _, statErr := os.Stat(composePath); statErr != nil {
		return nil, err
	}
	runner := deps.DeployRunner
	if runner == nil {
		runner = deploy.ExecRunner{}
	}
	out, localErr := runner.Output(
		ctx,
		"docker", "compose", "-f", composePath,
		"exec", "-T", "specgate",
		"curl", "--fail", "--silent", "--show-error", "--max-time", "5",
		"http://127.0.0.1:9090/healthz/components",
	)
	if localErr != nil {
		return nil, err
	}
	var local client.ComponentHealth
	if jsonErr := json.Unmarshal(out, &local); jsonErr != nil {
		return nil, err
	}
	return &local, nil
}

func doctorComponentCheck(health *client.ComponentHealth) doctorCheck {
	if health == nil {
		return doctorCheck{}
	}
	names := make([]string, 0, len(health.Components))
	for name := range health.Components {
		names = append(names, name)
	}
	slices.Sort(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+health.Components[name].Status)
	}
	return doctorCheck{Status: health.Status, Message: strings.Join(parts, ", ")}
}

func failedComponentMessage(health *client.ComponentHealth) string {
	failed := make([]string, 0, len(health.Components))
	for name, component := range health.Components {
		if !strings.EqualFold(component.Status, "ok") {
			failed = append(failed, name+"="+component.Status)
		}
	}
	slices.Sort(failed)
	if len(failed) == 0 {
		return "appliance is not ready"
	}
	return "appliance component failure: " + strings.Join(failed, ", ")
}

func runDoctorFix(ctx context.Context, deps *Deps) error {
	dir := resolveDeployDir(deps)
	if _, err := os.Stat(filepath.Join(dir, "compose.yml")); err != nil {
		return fmt.Errorf("no CLI-managed deployment found in %s; run `specgate init` first", dir)
	}

	const applianceAction = "full-appliance"
	actions := []interactive.Option{
		{Label: "Start or repair the Full appliance", Value: applianceAction},
	}
	defaults := []string{applianceAction}
	selected := defaults
	if !deps.Yes {
		if !canPrompt(deps) {
			return errors.New("doctor --fix needs an interactive terminal or --yes")
		}
		values, err := deps.Prompter.MultiSelect("Repair SpecGate environment", actions, defaults)
		if err != nil {
			return err
		}
		selected = values
	}
	if !slices.Contains(selected, applianceAction) {
		return errors.New("no repair actions selected")
	}
	if deps.Printer.Mode() != output.ModeJSON {
		fmt.Fprintf(deps.Stderr, "Repairing Full appliance (%s)...\n", dir)
	}
	return makeDeployService(deps, dir).Up(ctx)
}

// printDoctorFullAppliance renders a "Full appliance" section when a CLI-managed
// deployment exists, using the same data as `local-status` (which stays as-is
// for scripts). Skipped silently when no deployment directory is initialized.
func printDoctorFullAppliance(ctx context.Context, deps *Deps) {
	dir := resolveDeployDir(deps)
	if _, err := os.Stat(filepath.Join(dir, "compose.yml")); err != nil {
		return // no CLI-managed deployment
	}
	fmt.Fprintf(deps.Stdout, "\nFull appliance (%s):\n", dir)
	statuses, err := makeDeployService(deps, dir).LocalStatus(ctx)
	if err != nil {
		fmt.Fprintf(deps.Stdout, "  unavailable: %v\n", err)
		return
	}
	if len(statuses) == 0 {
		fmt.Fprintln(deps.Stdout, "  no services running")
		return
	}
	for _, s := range statuses {
		fmt.Fprintf(deps.Stdout, "  %-30s  %s\n", s.Name, s.Status)
	}
}
