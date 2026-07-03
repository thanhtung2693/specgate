package command

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/buildinfo"
	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/deploy"
	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/output"
)

const defaultTimeout = 180 * time.Second

const (
	commandGroupCore     = "core"
	commandGroupSetup    = "setup"
	commandGroupAdvanced = "advanced"
	commandGroupLocal    = "local"
)

// APIClient is satisfied by *client.Client and test fakes.
// Methods are added here as commands are implemented.
type APIClient interface {
	Meta(ctx context.Context) (*client.Meta, error)
	Status(ctx context.Context, workspaceID string) (*client.GovernanceStatus, error)
	Stats(ctx context.Context, workspaceID string, days int) (*client.StatsResult, error)
	Healthz(ctx context.Context) error
	ResolveWorkRef(ctx context.Context, ref string) (*client.ResolvedWork, error)
	ContextPack(ctx context.Context, id, lane string) (*client.ContextPackResult, error)
	CreateQuickWorkItem(ctx context.Context, in map[string]any) (map[string]any, error)
	ArchiveWorkItem(ctx context.Context, id string, reason string, actor string) (map[string]any, error)
	// Artifact commands
	ListArtifacts(ctx context.Context, filter client.ArtifactFilter) (*client.ArtifactList, error)
	GetArtifact(ctx context.Context, id string) (*client.Artifact, error)
	ListArtifactFiles(ctx context.Context, id string) ([]client.ArtifactFile, error)
	GetArtifactFile(ctx context.Context, id, filePath string) (*client.ArtifactFileContent, error)
	DraftProposal(ctx context.Context, artifactID string, body map[string]any) (*client.ProposalResult, error)
	UpdateArtifactStatus(ctx context.Context, id string, in client.UpdateArtifactStatusInput) (*client.Artifact, error)
	ListArtifactProposals(ctx context.Context) ([]client.ProposalSession, error)
	SaveArtifactProposal(ctx context.Context, sessionID, requestedBy string) (*client.SavedRevision, error)
	RejectArtifactProposal(ctx context.Context, sessionID string) error
	// Skill commands
	ListSkills(ctx context.Context, nameFilter string) ([]client.Skill, error)
	GetSkill(ctx context.Context, id string) (*client.Skill, error)
	// Feature commands
	ListFeatures(ctx context.Context, search string) ([]client.Feature, error)
	GetFeature(ctx context.Context, ref string) (*client.Feature, error)
	// Artifact publish / readiness
	PublishArtifact(ctx context.Context, body map[string]any) (map[string]any, error)
	RunArtifactReadiness(ctx context.Context, artifactID string) (map[string]any, error)
	// Gates commands
	RunLLMGates(ctx context.Context, id string) (map[string]any, error)
	GatesStatus(ctx context.Context, id string) (*client.GatesStatusResult, error)
	GateHistory(ctx context.Context, id, gate string, limit int) (*client.GateHistoryResult, error)
	// Delivery commands
	ReportFeedback(ctx context.Context, id string, body map[string]any) (map[string]any, error)
	TriggerDeliveryReview(ctx context.Context, id string) (map[string]any, error)
	DeliveryStatus(ctx context.Context, id string, detail bool) (*client.DeliveryStatusResult, error)
	ListAcceptanceCriteria(ctx context.Context, id string) ([]client.AcceptanceCriterion, error)
	// Policy commands
	ListGovernanceLevels(ctx context.Context) ([]client.GovernanceLevel, error)
	WorkPolicy(ctx context.Context, ref string) (*client.PolicyExplanation, error)
	ResolvePolicy(ctx context.Context, in client.ResolvePolicyInput) (*client.PolicyExplanation, error)
	// Gate task commands
	ListGateTasks(ctx context.Context, artifactID string) ([]client.GateTask, error)
	GetGateTask(ctx context.Context, taskID string) (*client.GateTask, error)
	SubmitGateResult(ctx context.Context, taskID string, body any) (*client.GateResultResponse, error)
	GatePreview(ctx context.Context, artifactID string) (map[string]any, error)
	DispatchGateTasks(ctx context.Context, artifactID string) (*client.DispatchGateTasksResult, error)
	// Settings (model configuration)
	GetSettings(ctx context.Context) (map[string]string, error)
	UpdateSettings(ctx context.Context, settings map[string]string) (map[string]string, error)
	// Identity commands
	BootstrapIdentity(ctx context.Context, in client.IdentityBootstrapInput) (*client.IdentitySelection, error)
	ListUsers(ctx context.Context) ([]client.IdentityUser, error)
	GetUser(ctx context.Context, id string) (*client.IdentityUser, error)
	ListWorkspaces(ctx context.Context) ([]client.IdentityWorkspace, error)
	GetWorkspace(ctx context.Context, id string) (*client.IdentityWorkspace, error)
}

// Deps carries injectable dependencies shared by all commands.
type Deps struct {
	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader

	// Inject in tests to bypass os.UserConfigDir; empty = use default path.
	ConfigPath string

	// Opener launches a URL in the default browser. Injected for tests.
	Opener func(url string) error

	// Prompter handles interactive terminal prompts. Injected for tests.
	Prompter interactive.Prompter

	// StdinIsTTY reports whether stdin is an interactive terminal. Nil uses
	// real TTY detection on os.Stdin; tests inject a fixed answer.
	StdinIsTTY func() bool

	// OpenRouterModelOptions returns filterable OpenRouter model picker options.
	// Nil uses the public OpenRouter catalog.
	OpenRouterModelOptions func(ctx context.Context) ([]interactive.Option, error)

	// DeployRunner executes docker / docker compose commands. Injected for tests;
	// nil uses the production deploy.ExecRunner.
	DeployRunner deploy.CommandRunner

	// CheckLatestRelease fetches the latest public GitHub release tag. Nil skips
	// the public freshness check; DefaultDeps wires the production checker.
	CheckLatestRelease   func(ctx context.Context, timeout time.Duration, cachePath string) (string, error)
	UpdateCheckCachePath string
	CLIInstallURL        string
	PluginRegistryURL    string
	PublicRegistryURL    string
	BundleBaseURL        string

	// Resolved by PersistentPreRunE; safe to read in any subcommand RunE.
	Printer      *output.Printer
	Client       APIClient
	ServerURL    string
	Mode         output.Mode
	JSONProgress bool
	NoInput      bool
	Yes          bool
	Timeout      time.Duration

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
		jsonMode  bool
		plainMode bool
		serverURL string
		timeout   time.Duration
	)

	root := &cobra.Command{
		Use:           "specgate",
		Short:         "SpecGate CLI — manage features and change requests",
		Version:       buildinfo.Version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetVersionTemplate("specgate {{.Version}}\n")
	root.AddGroup(
		&cobra.Group{ID: commandGroupCore, Title: "Core workflow commands"},
		&cobra.Group{ID: commandGroupSetup, Title: "Setup and identity commands"},
		&cobra.Group{ID: commandGroupAdvanced, Title: "Advanced governance commands"},
		&cobra.Group{ID: commandGroupLocal, Title: "Local stack commands"},
	)

	f := root.PersistentFlags()
	f.BoolVar(&jsonMode, "json", false, "Output JSON (implies --plain and --no-input)")
	f.BoolVar(&deps.JSONProgress, "json-progress", false, "Emit compact JSON progress events before the final JSON envelope")
	f.BoolVar(&plainMode, "plain", false, "Plain text; no colour or interactive prompts")
	f.BoolVar(&deps.NoInput, "no-input", false, "Disable interactive prompts; error if a prompt would be required")
	f.BoolVar(&deps.Yes, "yes", false, "Auto-confirm all prompts")
	f.StringVar(&serverURL, "server", "", "SpecGate server URL (overrides SPECGATE_SERVER)")
	f.DurationVar(&timeout, "timeout", defaultTimeout, "Request timeout (default 3 min)")

	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		// --json implies --plain and --no-input.
		if jsonMode {
			plainMode = true
			deps.NoInput = true
		} else {
			deps.JSONProgress = false
		}

		deps.Mode = output.ModeHuman
		if plainMode {
			deps.Mode = output.ModePlain
		}
		if jsonMode {
			deps.Mode = output.ModeJSON
		}

		deps.Printer = output.New(deps.Stdout, deps.Stderr, deps.Mode)

		cfg, _ := config.LoadFrom(deps.ConfigPath)
		deps.ServerURL = config.ResolveServer(serverURL, cfg)

		if timeout > 0 {
			deps.Timeout = timeout
		} else {
			deps.Timeout = defaultTimeout
		}

		// Create the HTTP client if one has not been injected (e.g. in tests).
		if deps.Client == nil {
			deps.Client = client.New(deps.ServerURL, deps.Timeout)
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
	registerWorkCommands(root, deps)
	registerArtifactCommands(root, deps)
	registerSkillCommands(root, deps)
	registerFeatureCommands(root, deps)
	registerGatesCommands(root, deps)
	registerDeliveryCommands(root, deps)
	registerPolicyCommands(root, deps)
	registerModelCommands(root, deps)
	registerIdentityCommands(root, deps)
	registerPluginCommands(root, deps)
	assignRootCommandGroups(root)

	return root
}

func assignRootCommandGroups(root *cobra.Command) {
	groups := map[string]string{
		"artifact":     commandGroupCore,
		"delivery":     commandGroupCore,
		"feature":      commandGroupCore,
		"gates":        commandGroupCore,
		"stats":        commandGroupCore,
		"status":       commandGroupCore,
		"work":         commandGroupCore,
		"config":       commandGroupSetup,
		"doctor":       commandGroupSetup,
		"model":        commandGroupSetup,
		"open":         commandGroupSetup,
		"plugins":      commandGroupSetup,
		"uninstall":    commandGroupSetup,
		"skill":        commandGroupSetup,
		"update":       commandGroupSetup,
		"user":         commandGroupSetup,
		"workspace":    commandGroupSetup,
		"policy":       commandGroupAdvanced,
		"down":         commandGroupLocal,
		"init":         commandGroupLocal,
		"local-status": commandGroupLocal,
		"up":           commandGroupLocal,
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
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		return output.ExitCodeFromError(err)
	}
	return output.ExitOK
}
