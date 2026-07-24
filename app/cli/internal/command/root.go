package command

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/buildinfo"
	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/deploy"
	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/output"
)

const defaultTimeout = 180 * time.Second

const requestedJSONAnnotation = "specgate.io/requested-json"

const (
	commandGroupStart = "start"
	commandGroupCore  = "core"
	commandGroupSetup = "setup"
	commandGroupFull  = "full"
)

// APIClient is satisfied by *client.Client and test fakes.
// Methods are added here as commands are implemented.
type APIClient interface {
	Meta(ctx context.Context) (*client.Meta, error)
	Status(ctx context.Context, workspaceID string) (*client.GovernanceStatus, error)
	ListWorkItems(ctx context.Context, workspaceID string) ([]client.WorkItemSummary, error)
	ListWorkItemsIncludingArchived(ctx context.Context, workspaceID string) ([]client.WorkItemSummary, error)
	Stats(ctx context.Context, workspaceID string, days int) (*client.StatsResult, error)
	Healthz(ctx context.Context) error
	ResolveWorkRef(ctx context.Context, ref string) (*client.ResolvedWork, error)
	ContextPack(ctx context.Context, id string) (*client.ContextPackResult, error)
	AuditTrail(ctx context.Context, ref string, verify bool) (*client.AuditTrail, error)
	CreateQuickWorkItem(ctx context.Context, in map[string]any) (map[string]any, error)
	CreateWorkItem(ctx context.Context, in map[string]any) (map[string]any, error)
	ArchiveWorkItem(ctx context.Context, id string, reason string, actor string) (map[string]any, error)
	// Artifact commands
	ListArtifacts(ctx context.Context, filter client.ArtifactFilter) (*client.ArtifactList, error)
	GetArtifact(ctx context.Context, id string) (*client.Artifact, error)
	ListArtifactFiles(ctx context.Context, id string) ([]client.ArtifactFile, error)
	GetArtifactFile(ctx context.Context, id, filePath string) (*client.ArtifactFileContent, error)
	UpdateArtifactStatus(ctx context.Context, id string, in client.UpdateArtifactStatusInput) (*client.Artifact, error)
	PromoteArtifactCanonical(ctx context.Context, artifactID, approvedBy string) (*client.Feature, error)
	// Skill commands
	ListSkills(ctx context.Context, nameFilter string) ([]client.Skill, error)
	GetSkill(ctx context.Context, id string) (*client.Skill, error)
	// Feature commands
	ListFeatures(ctx context.Context, search string) ([]client.Feature, error)
	GetFeature(ctx context.Context, ref string) (*client.Feature, error)
	UpdateFeatureStatus(ctx context.Context, id, status string) (*client.Feature, error)
	// Knowledge commands
	ListKnowledgeDocuments(ctx context.Context, filter client.KnowledgeListFilter) (*client.KnowledgeDocumentList, error)
	GetKnowledgeDocument(ctx context.Context, id, version string) (*client.KnowledgeDocumentDetail, error)
	CreateTextKnowledgeDocument(ctx context.Context, in client.KnowledgeCreateTextInput) (*client.KnowledgeDocument, error)
	CurateKnowledgeLinks(ctx context.Context, id string, in client.KnowledgeCurateLinksInput) (*client.KnowledgeDocument, error)
	SearchKnowledge(ctx context.Context, in client.KnowledgeSearchInput) ([]client.KnowledgeSearchResult, error)
	// Artifact publish / readiness
	PublishArtifact(ctx context.Context, body map[string]any) (map[string]any, error)
	RunArtifactReadiness(ctx context.Context, artifactID string) (map[string]any, error)
	ListArtifactReadinessRuns(ctx context.Context, artifactID string) (map[string]any, error)
	// Gates commands
	RunLLMGates(ctx context.Context, id string) (map[string]any, error)
	GatesStatus(ctx context.Context, id string) (*client.GatesStatusResult, error)
	GateHistory(ctx context.Context, id, gate string, limit int) (*client.GateHistoryResult, error)
	// Delivery commands
	ReportFeedback(ctx context.Context, id string, body map[string]any) (map[string]any, error)
	ListGovernanceFeedbackEvents(ctx context.Context, changeRequestID string) ([]client.GovernanceFeedbackEvent, error)
	TriggerDeliveryReview(ctx context.Context, id string) (map[string]any, error)
	DecideDelivery(ctx context.Context, id string, in client.DeliveryDecisionInput) (*client.DeliveryDecisionResult, error)
	DeliveryStatus(ctx context.Context, id string, detail bool) (*client.DeliveryStatusResult, error)
	ListAcceptanceCriteria(ctx context.Context, id string) ([]client.AcceptanceCriterion, error)
	// Policy commands
	ListGovernanceLevels(ctx context.Context) ([]client.GovernanceLevel, error)
	WorkPolicy(ctx context.Context, ref string) (*client.PolicyExplanation, error)
	// Gate task commands
	ListGateTasks(ctx context.Context, artifactID string) ([]client.GateTask, error)
	GetGateTask(ctx context.Context, taskID string) (*client.GateTask, error)
	SubmitGateResult(ctx context.Context, taskID string, body any) (*client.GateResultResponse, error)
	DispatchGateTasks(ctx context.Context, artifactID string) (*client.DispatchGateTasksResult, error)
	// Settings (model configuration)
	GetSettings(ctx context.Context) (map[string]string, error)
	UpdateSettings(ctx context.Context, settings map[string]string) (map[string]string, error)
	// Maintenance
	MaintenanceCleanup(ctx context.Context) (map[string]any, error)
	RemoveDemo(ctx context.Context) (map[string]any, error)
	// Identity commands
	BootstrapIdentity(ctx context.Context, in client.IdentityBootstrapInput) (*client.IdentitySelection, error)
	ListUsers(ctx context.Context) ([]client.IdentityUser, error)
	GetUser(ctx context.Context, id string) (*client.IdentityUser, error)
	ListWorkspaces(ctx context.Context) ([]client.IdentityWorkspace, error)
	GetWorkspace(ctx context.Context, id string) (*client.IdentityWorkspace, error)
	ListWorkspaceMembers(ctx context.Context, id, currentUserID, currentUsername string) (*client.WorkspaceMembersResult, error)
}

// Deps carries injectable dependencies shared by all commands.
type Deps struct {
	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader

	// Inject in tests to bypass os.UserConfigDir; empty = use default path.
	ConfigPath string

	// UserHomeDir returns the user home directory for commands that manage
	// IDE/user files. Nil uses os.UserHomeDir.
	UserHomeDir func() (string, error)

	// WorkingDir overrides os.Getwd for project-scoped CLI config resolution in
	// tests. Empty means the current process directory.
	WorkingDir string

	// WorkspaceOverride carries the effective one-command workspace override
	// resolved from the root flag or SPECGATE_WORKSPACE.
	WorkspaceOverride string

	// Opener launches a URL in the default browser. Injected for tests.
	Opener func(url string) error

	// Prompter handles interactive terminal prompts. Injected for tests.
	Prompter interactive.Prompter

	// StdinIsTTY reports whether stdin is an interactive terminal. Nil uses
	// real TTY detection on os.Stdin; tests inject a fixed answer.
	StdinIsTTY func() bool

	// StdoutIsTTY reports whether stdout supports human ANSI output. Nil uses
	// real TTY detection on os.Stdout; tests inject a fixed answer.
	StdoutIsTTY func() bool

	// StderrIsTTY reports whether stderr supports human ANSI output. Nil uses
	// real TTY detection on os.Stderr; tests inject a fixed answer.
	StderrIsTTY func() bool

	// OpenRouterModelOptions returns filterable OpenRouter model picker options.
	// Nil uses the public OpenRouter catalog.
	OpenRouterModelOptions func(ctx context.Context) ([]interactive.Option, error)

	// PluginAgentDefaults returns preselected IDE agents for interactive plugin
	// prompts. Nil detects installed tools from the local machine.
	PluginAgentDefaults func() []string

	// DeployRunner executes docker / docker compose commands. Injected for tests;
	// nil uses the production deploy.ExecRunner.
	DeployRunner deploy.CommandRunner

	// RunCheckCommand executes one completion-report check command locally for
	// `delivery submit --run-checks`, returning the exit code and combined
	// output. Nil uses the production `sh -c` runner; tests inject results.
	RunCheckCommand func(ctx context.Context, command string) (int, string)

	// CheckLatestRelease fetches the latest public GitHub release tag. Nil skips
	// the public freshness check; DefaultDeps wires the production checker.
	CheckLatestRelease   func(ctx context.Context, timeout time.Duration, cachePath string) (string, error)
	UpdateCheckCachePath string
	CLIInstallURL        string
	CLIReleaseBaseURL    string
	PluginRegistryURL    string
	PublicRegistryURL    string
	BundleBaseURL        string

	// RuntimeGOOS and ExecutablePath isolate the platform-specific updater in
	// tests. Empty/nil values use runtime.GOOS and os.Executable.
	RuntimeGOOS    string
	ExecutablePath func() (string, error)

	// SelfUpdateCLI replaces the running Windows executable. Nil uses the
	// checksum-verified GitHub release updater.
	SelfUpdateCLI func(ctx context.Context, version, executablePath string) error

	// Resolved by PersistentPreRunE; safe to read in any subcommand RunE.
	Printer      *output.Printer
	Client       APIClient
	Topology     config.Mode
	ServerURL    string
	Mode         output.Mode
	JSONProgress bool
	NoInput      bool
	Yes          bool
	Timeout      time.Duration

	useEmbeddedPlugins  bool // no topology or configured server before init
	versionWarningShown bool
}

// DefaultDeps returns production-ready Deps backed by OS standard streams.
func DefaultDeps() *Deps {
	return &Deps{
		Stdout:             os.Stdout,
		Stderr:             os.Stderr,
		Stdin:              os.Stdin,
		Opener:             defaultOpener,
		CheckLatestRelease: latestGitHubReleaseWithCache,
	}
}

// NewRootCommand builds the root cobra command with persistent flags and
// registers all subcommands defined in this package.
func NewRootCommand(deps *Deps) *cobra.Command {
	var (
		jsonMode          bool
		plainMode         bool
		serverURL         string
		workspaceOverride string
		timeout           time.Duration
	)

	root := &cobra.Command{
		Use:           "specgate",
		Short:         "SpecGate CLI — govern the handoff from specs to delivery",
		Version:       buildinfo.Version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetOut(deps.Stdout)
	root.SetErr(deps.Stderr)
	root.SetVersionTemplate("specgate {{.Version}}\n")
	renderUsageError := func(cmd *cobra.Command, err error) error {
		mode := output.ModeHuman
		if plainMode {
			mode = output.ModePlain
		}
		if jsonMode || cmd.Root().Annotations[requestedJSONAnnotation] == "true" {
			mode = output.ModeJSON
		}
		printer := output.NewWithCapabilities(deps.Stdout, deps.Stderr, mode, colorEnabled(deps, mode), stderrColorEnabled(deps, mode))
		code := printer.Error(commandOutputName(cmd), output.ErrorPayload{
			Code:    "usage",
			Message: fmt.Sprintf("%s; run `%s --help` for usage", err, cmd.CommandPath()),
		})
		return &output.ExitError{Code: code, Err: err}
	}
	root.SetFlagErrorFunc(renderUsageError)
	root.AddGroup(
		&cobra.Group{ID: commandGroupStart, Title: "Start here"},
		&cobra.Group{ID: commandGroupCore, Title: "Core workflow commands"},
		&cobra.Group{ID: commandGroupSetup, Title: "Setup and identity commands"},
		&cobra.Group{ID: commandGroupFull, Title: "Full appliance commands"},
	)

	f := root.PersistentFlags()
	f.BoolVar(&jsonMode, "json", false, "Output JSON (implies --plain and --no-input)")
	f.BoolVar(&deps.JSONProgress, "json-progress", false, "Emit compact JSON progress events before the final JSON envelope")
	f.BoolVar(&plainMode, "plain", false, "Plain text; no colour or interactive prompts")
	f.BoolVar(&deps.NoInput, "no-input", false, "Disable interactive prompts; error if a prompt would be required")
	f.BoolVar(&deps.Yes, "yes", false, "Auto-confirm all prompts")
	f.StringVar(&serverURL, "server", "", "SpecGate server URL (overrides SPECGATE_SERVER)")
	f.StringVar(&workspaceOverride, "workspace", "", "Workspace slug or ID for this command (overrides project/global selection and SPECGATE_WORKSPACE)")
	f.DurationVar(&timeout, "timeout", defaultTimeout, "Request timeout (default 3 min)")

	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		// --json implies --plain and --no-input.
		if jsonMode {
			plainMode = true
			deps.NoInput = true
		}

		deps.Mode = output.ModeHuman
		if plainMode {
			deps.Mode = output.ModePlain
		}
		if jsonMode {
			deps.Mode = output.ModeJSON
		}

		deps.Printer = output.NewWithCapabilities(deps.Stdout, deps.Stderr, deps.Mode, colorEnabled(deps, deps.Mode), stderrColorEnabled(deps, deps.Mode))
		if deps.JSONProgress && !jsonMode {
			payload := output.ErrorPayload{Code: "usage", Message: "--json-progress requires --json"}
			code := deps.Printer.Error(commandOutputName(cmd), payload)
			return &output.ExitError{Code: code}
		}

		cfg, err := config.LoadFrom(deps.ConfigPath)
		if err != nil && cmd.Name() != "version" && cmd.Name() != "completion" {
			payload := output.ErrorPayload{
				Code:    "unavailable",
				Message: fmt.Sprintf("cannot load SpecGate config: %v; repair or move the config file aside, then run `specgate init`", err),
			}
			code := deps.Printer.Error(commandOutputName(cmd), payload)
			return &output.ExitError{Code: code, Err: err}
		}
		deps.useEmbeddedPlugins = cfg.Mode == "" &&
			strings.TrimSpace(serverURL) == "" &&
			strings.TrimSpace(os.Getenv("SPECGATE_SERVER")) == "" &&
			strings.TrimSpace(cfg.Server) == ""
		deps.WorkspaceOverride = ""
		if trimmed := strings.TrimSpace(workspaceOverride); trimmed != "" {
			deps.WorkspaceOverride = trimmed
		} else if trimmed := strings.TrimSpace(os.Getenv("SPECGATE_WORKSPACE")); trimmed != "" {
			deps.WorkspaceOverride = trimmed
		}
		initMode := ""
		if cmd.Name() == "init" {
			writeInitWelcome(deps)
			initMode, _ = cmd.Flags().GetString("mode")
			if initMode == "" && canPrompt(deps) {
				if deps.Prompter == nil {
					deps.Prompter = interactive.NewHuhPrompter()
				}
				selected, err := deps.Prompter.Select("Choose setup mode", []interactive.Option{
					{Label: "Local — personal workflow, no Docker or server", Value: "local"},
					{Label: "Full — team workspace, browser UI, chat, and integrations", Value: "full"},
				})
				if err != nil {
					return err
				}
				if err := cmd.Flags().Set("mode", selected); err != nil {
					return err
				}
				initMode = selected
			}
		}
		if cmd.Name() == "init" && (initMode == "local" || initMode == "") {
			deps.Topology = config.ModeLocal
			if strings.TrimSpace(serverURL) != "" {
				payload := output.ErrorPayload{Code: "incompatible", Message: "--server cannot be used with Local mode; run `specgate init --mode full` for a server-backed setup"}
				code := deps.Printer.Error(commandOutputName(cmd), payload)
				return &output.ExitError{Code: code}
			}
			if deps.Prompter == nil {
				deps.Prompter = interactive.NewHuhPrompter()
			}
			return nil
		}
		if cmd.Name() == "init" && initMode == "full" {
			deps.Topology = config.ModeFull
			deps.useEmbeddedPlugins = false
		} else {
			deps.Topology = config.ResolveMode(cfg)
		}
		if err := enforceProjectWorkspaceBoundary(cmd, deps, cfg); err != nil {
			return err
		}
		if deps.Topology == config.ModeLocal {
			if strings.TrimSpace(serverURL) != "" {
				payload := output.ErrorPayload{Code: "incompatible", Message: "--server cannot be used with Local mode; run `specgate init --mode full` for a server-backed setup"}
				code := deps.Printer.Error(commandOutputName(cmd), payload)
				return &output.ExitError{Code: code}
			}
			if !localCommandSupported(cmd) {
				payload := output.ErrorPayload{Code: "incompatible", Message: fmt.Sprintf("`%s` requires Full mode; run `specgate init --mode full` to use the server-backed workflow", cmd.CommandPath())}
				code := deps.Printer.Error(commandOutputName(cmd), payload)
				return &output.ExitError{Code: code}
			}
			if deps.Prompter == nil {
				deps.Prompter = interactive.NewHuhPrompter()
			}
			return nil
		}
		repoCfg := config.LoadRepoConfig(deps.WorkingDir)
		if commandRequiresTrustedServer(cmd) {
			repoCfg.Server = ""
		}
		deps.ServerURL = config.ResolveServer(serverURL, repoCfg, cfg)
		if strings.TrimSpace(deps.PluginRegistryURL) == "" {
			deps.PluginRegistryURL = config.ResolvePluginRegistry(serverURL, cfg)
		}
		if timeout > 0 {
			deps.Timeout = timeout
		} else {
			deps.Timeout = defaultTimeout
		}

		// Create the HTTP client if one has not been injected (e.g. in tests).
		if deps.Client == nil {
			deps.Client = client.New(deps.ServerURL, deps.Timeout)
		}
		if commandUsesSelectedWorkspace(cmd) {
			workspaceID, err := workspaceIDForSelection(cmd.Context(), deps, resolveWorkspaceSelection(deps, cfg))
			if err != nil {
				return apiExitError(deps, commandOutputName(cmd), err)
			}
			if workspaceID != "" {
				cmd.SetContext(client.WithWorkspace(cmd.Context(), workspaceID))
			}
		}
		// Create the prompter if one has not been injected (e.g. in tests).
		if deps.Prompter == nil {
			deps.Prompter = interactive.NewHuhPrompter()
		}

		warnIfCLIUpdateRecommended(cmd.Context(), deps, cmd)

		return nil
	}

	registerSystemCommands(root, deps)
	registerStatsCommands(root, deps)
	registerAuditCommands(root, deps)
	registerWorkCommands(root, deps)
	registerArtifactCommands(root, deps)
	registerSkillCommands(root, deps)
	registerFeatureCommands(root, deps)
	registerKnowledgeCommands(root, deps)
	registerGatesCommands(root, deps)
	registerDeliveryCommands(root, deps)
	root.AddCommand(newChangeCmd(deps))
	registerPolicyCommands(root, deps)
	registerModelCommands(root, deps)
	registerIdentityCommands(root, deps)
	registerPluginCommands(root, deps)
	root.AddCommand(newCapabilitiesCmd(deps))
	root.AddCommand(newCoverageCmd(deps))
	root.AddCommand(newPortableCmd(deps))
	root.AddCommand(newVerifyCmd(deps))
	defaultArgumentPolicies(root)
	wrapArgumentErrors(root, renderUsageError)
	assignRootCommandGroups(root)

	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if jsonMode || plainMode || !colorEnabled(deps, output.ModeHuman) {
			defaultHelp(cmd, args)
			return
		}
		var rendered bytes.Buffer
		previous := cmd.OutOrStdout()
		cmd.SetOut(&rendered)
		defaultHelp(cmd, args)
		cmd.SetOut(previous)
		helpDeps := *deps
		helpDeps.Printer = output.NewWithColor(deps.Stdout, deps.Stderr, output.ModeHuman, true)
		fmt.Fprint(previous, styleHelpHeadings(&helpDeps, rendered.String()))
	})

	return root
}

func defaultArgumentPolicies(cmd *cobra.Command) {
	children := cmd.Commands()
	if cmd.Args == nil {
		cmd.Args = cobra.NoArgs
	}
	if len(children) > 0 && cmd.Run == nil && cmd.RunE == nil {
		cmd.RunE = func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		}
	}
	for _, child := range children {
		defaultArgumentPolicies(child)
	}
}

func commandRequiresTrustedServer(cmd *cobra.Command) bool {
	return cmd.Name() == "set" && cmd.Parent() != nil && cmd.Parent().Name() == "model"
}

func wrapArgumentErrors(cmd *cobra.Command, render func(*cobra.Command, error) error) {
	if validate := cmd.Args; validate != nil {
		cmd.Args = func(cmd *cobra.Command, args []string) error {
			if err := validate(cmd, args); err != nil {
				return render(cmd, err)
			}
			return nil
		}
	}
	for _, child := range cmd.Commands() {
		wrapArgumentErrors(child, render)
	}
}

func colorEnabled(deps *Deps, mode output.Mode) bool {
	return terminalColorEnabled(mode, stdoutIsTTY(deps))
}

func stderrColorEnabled(deps *Deps, mode output.Mode) bool {
	return terminalColorEnabled(mode, stderrIsTTY(deps))
}

func terminalColorEnabled(mode output.Mode, isTTY bool) bool {
	return mode == output.ModeHuman && isTTY && !truthyEnv("CI") && os.Getenv("NO_COLOR") == "" && !strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb")
}

func stdoutIsTTY(deps *Deps) bool {
	if deps == nil {
		return false
	}
	if deps.StdoutIsTTY != nil {
		return deps.StdoutIsTTY()
	}
	f, ok := deps.Stdout.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

func stderrIsTTY(deps *Deps) bool {
	if deps == nil {
		return false
	}
	if deps.StderrIsTTY != nil {
		return deps.StderrIsTTY()
	}
	f, ok := deps.Stderr.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

func localCommandSupported(cmd *cobra.Command) bool {
	path := strings.Fields(cmd.CommandPath())
	if len(path) < 2 {
		return true
	}
	if path[1] == "cleanup" {
		return fileCleanupRequested(cmd)
	}
	commandPath := strings.Join(path[1:], " ")
	if _, ok := localCommandCapabilities[commandPath]; ok {
		return true
	}
	for supported := range localCommandCapabilities {
		if strings.HasPrefix(supported, commandPath+" ") {
			return true
		}
	}
	return false
}

func commandOutputName(cmd *cobra.Command) string {
	path := strings.Fields(cmd.CommandPath())
	if len(path) <= 1 {
		return "specgate"
	}
	return strings.Join(path[1:], ".")
}

func commandUsesSelectedWorkspace(cmd *cobra.Command) bool {
	if cmd.Flags().Lookup("all-workspaces") != nil {
		all, _ := cmd.Flags().GetBool("all-workspaces")
		if all {
			return false
		}
	}
	path := strings.Fields(cmd.CommandPath())
	if len(path) < 2 {
		return false
	}
	switch path[1] {
	case "cleanup":
		return !fileCleanupRequested(cmd)
	case "status", "stats", "audit", "coverage", "verify", "portable", "gates", "delivery", "change", "feature", "knowledge", "skill", "open", "demo":
		return true
	case "work":
		return len(path) < 3 || (path[2] != "create" && path[2] != "create-quick")
	case "artifact":
		return len(path) < 3 || path[2] != "publish"
	default:
		return false
	}
}

func assignRootCommandGroups(root *cobra.Command) {
	groups := map[string]string{
		"change":       commandGroupStart,
		"audit":        commandGroupCore,
		"artifact":     commandGroupCore,
		"capabilities": commandGroupCore,
		"coverage":     commandGroupCore,
		"delivery":     commandGroupCore,
		"feature":      commandGroupCore,
		"gates":        commandGroupCore,
		"stats":        commandGroupCore,
		"status":       commandGroupCore,
		"work":         commandGroupCore,
		"verify":       commandGroupCore,
		"config":       commandGroupSetup,
		"cleanup":      commandGroupSetup,
		"doctor":       commandGroupSetup,
		"init":         commandGroupSetup,
		"plugins":      commandGroupSetup,
		"portable":     commandGroupSetup,
		"uninstall":    commandGroupSetup,
		"update":       commandGroupSetup,
		"user":         commandGroupSetup,
		"workspace":    commandGroupSetup,
		"demo":         commandGroupFull,
		"down":         commandGroupFull,
		"knowledge":    commandGroupFull,
		"local-status": commandGroupFull,
		"model":        commandGroupFull,
		"open":         commandGroupFull,
		"policy":       commandGroupFull,
		"skill":        commandGroupFull,
		"up":           commandGroupFull,
	}
	for _, cmd := range root.Commands() {
		if groupID, ok := groups[cmd.Name()]; ok {
			cmd.GroupID = groupID
		}
	}
}

// ExecuteForCode runs root with the given args and returns the process exit code.
// It never calls os.Exit itself; the caller is responsible for that.
func ExecuteForCode(root *cobra.Command, args ...string) int {
	if root.Annotations == nil {
		root.Annotations = map[string]string{}
	}
	// Flag parsing happens before PersistentPreRunE and stops at the first bad
	// flag. Preserve JSON output even when --json follows that bad flag.
	root.Annotations[requestedJSONAnnotation] = strconv.FormatBool(jsonOutputRequested(args))
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		return output.ExitCodeFromError(err)
	}
	return output.ExitOK
}

func jsonOutputRequested(args []string) bool {
	if args == nil {
		args = os.Args[1:]
	}
	requested := false
	for _, arg := range args {
		switch {
		case arg == "--json":
			requested = true
		case strings.HasPrefix(arg, "--json="):
			value, err := strconv.ParseBool(strings.TrimPrefix(arg, "--json="))
			if err == nil {
				requested = value
			}
		}
	}
	return requested
}
