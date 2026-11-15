package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/buildinfo"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/executil"
	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/output"
)

const defaultTimeout = 180 * time.Second

const (
	requestedJSONAnnotation = "specgate.io/requested-json"
	groupHelpAnnotation     = "specgate.io/group-help"
)

const (
	commandGroupStart    = "start"
	commandGroupBrowser  = "browser"
	commandGroupAdvanced = "advanced"
)

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

	// PluginAgentDefaults returns preselected IDE agents for interactive plugin
	// prompts. Nil detects installed tools from the local machine.
	PluginAgentDefaults func() []string

	// CommandRunner captures Git command output. Nil uses executil.ExecRunner.
	CommandRunner executil.CommandRunner

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
	PublicRegistryURL    string

	// RuntimeGOOS and ExecutablePath isolate the platform-specific updater in
	// tests. Empty/nil values use runtime.GOOS and os.Executable.
	RuntimeGOOS    string
	ExecutablePath func() (string, error)

	// SelfUpdateCLI replaces the running Windows executable. Nil uses the
	// checksum-verified GitHub release updater.
	SelfUpdateCLI func(ctx context.Context, version, executablePath string) error

	// Local UI maintenance hooks isolate release checks and installation in tests.
	// Nil uses the official release API and the existing Local update command.
	LocalUICheckUpdate func(ctx context.Context) (string, error)
	LocalUIUpdate      func(ctx context.Context, version string) error

	// Resolved by PersistentPreRunE; safe to read in any subcommand RunE.
	Printer      *output.Printer
	Mode         output.Mode
	JSONProgress bool
	NoInput      bool
	Yes          bool
	Timeout      time.Duration
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
		workspaceOverride string
		timeout           time.Duration
	)

	root := &cobra.Command{
		Use:   "specgate",
		Short: "SpecGate CLI — govern the handoff from specs to delivery",
		Long: `SpecGate governs the handoff from approved specs to delivery evidence.

specgate init sets up Local mode by default. Work with your coding agent without Docker, a model API key, or a server.
Approve the scope, give the work reference to your agent, and have it submit evidence.
Review the evidence and follow the next command from change status to record your decision.

Use specgate ui when you want to inspect a delivery in your browser.`,
		Example: `  specgate init
  specgate doctor

  # Give the approved work reference to your agent.
  specgate work resume LOCAL-123 --json
  # When the agent hands back evidence:
  specgate change status LOCAL-123`,
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
		&cobra.Group{ID: commandGroupBrowser, Title: "Optional browser review"},
		&cobra.Group{ID: commandGroupAdvanced, Title: "Advanced and maintenance commands"},
	)

	f := root.PersistentFlags()
	f.BoolVar(&jsonMode, "json", false, "Output JSON (implies --plain and --no-input)")
	f.BoolVar(&deps.JSONProgress, "json-progress", false, "Emit compact JSON progress events before the final JSON envelope")
	f.BoolVar(&plainMode, "plain", false, "Plain text; no colour or interactive prompts")
	f.BoolVar(&deps.NoInput, "no-input", false, "Disable interactive prompts; error if a prompt would be required")
	f.BoolVar(&deps.Yes, "yes", false, "Auto-confirm all prompts")
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
		deps.Timeout = defaultTimeout
		if timeout > 0 {
			deps.Timeout = timeout
		}
		if deps.Prompter == nil {
			deps.Prompter = interactive.NewHuhPrompter(deps.Stdin, deps.Stderr)
		}
		if cmd.Annotations[groupHelpAnnotation] == "true" ||
			(cmd.Parent() != nil && cmd.Parent().Parent() == root && cmd.Parent().Name() == "completion") {
			return nil
		}
		switch commandOutputName(cmd) {
		case "help", "version", "delivery.handoff.show", "transfer.inspect", "transfer.receive", "transfer.activate":
			return nil
		}

		cfg, err := config.LoadFrom(deps.ConfigPath)
		if err != nil {
			payload := output.ErrorPayload{
				Code:    "unavailable",
				Message: fmt.Sprintf("cannot load SpecGate config: %v; repair or move the config file aside, then run `specgate init`", err),
			}
			code := deps.Printer.Error(commandOutputName(cmd), payload)
			return &output.ExitError{Code: code, Err: err}
		}
		mode, _ := cmd.Flags().GetString("mode")
		if cfg.LegacyFull() || mode == "full" ||
			(cfg.Mode == "" && (strings.TrimSpace(os.Getenv("SPECGATE_SERVER")) != "" ||
				strings.TrimSpace(config.LoadRepoConfig(deps.WorkingDir).Server) != "")) {
			return incompatibleCommand(deps, commandOutputName(cmd), "Full mode is retired. Keep the existing configuration and appliance data for recovery with a previous compatible release. For a separate Local setup, select a new file with SPECGATE_CONFIG_PATH, unset SPECGATE_SERVER, and run specgate init outside the legacy repository. You can then return to the checkout and bind it to a Local workspace.")
		}
		deps.WorkspaceOverride = ""
		if trimmed := strings.TrimSpace(workspaceOverride); trimmed != "" {
			deps.WorkspaceOverride = trimmed
		} else if trimmed := strings.TrimSpace(os.Getenv("SPECGATE_WORKSPACE")); trimmed != "" {
			deps.WorkspaceOverride = trimmed
		}
		if err := enforceProjectWorkspaceBoundary(cmd, deps, cfg); err != nil {
			return err
		}
		return nil
	}

	registerSystemCommands(root, deps)
	registerAuditCommands(root, deps)
	registerWorkCommands(root, deps)
	registerArtifactCommands(root, deps)
	registerFeatureCommands(root, deps)
	registerGatesCommands(root, deps)
	registerDeliveryCommands(root, deps)
	root.AddCommand(newTransferCmd(deps))
	root.AddCommand(newChangeCmd(deps))
	registerIdentityCommands(root, deps)
	registerPluginCommands(root, deps)
	root.AddCommand(newCapabilitiesCmd(deps))
	root.AddCommand(newCoverageCmd(deps))
	root.AddCommand(newVerifyCmd(deps))
	root.InitDefaultCompletionCmd()
	defaultArgumentPolicies(root, deps)
	wrapArgumentErrors(root, renderUsageError)
	guaranteeErrorEnvelope(root, deps)
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

func defaultArgumentPolicies(cmd *cobra.Command, deps *Deps) {
	children := cmd.Commands()
	if cmd.Args == nil {
		cmd.Args = cobra.NoArgs
	}
	if len(children) > 0 && cmd.Run == nil && cmd.RunE == nil {
		if cmd.Annotations == nil {
			cmd.Annotations = map[string]string{}
		}
		cmd.Annotations[groupHelpAnnotation] = "true"
		cmd.RunE = func(cmd *cobra.Command, _ []string) error {
			if deps.Mode != output.ModeJSON {
				return cmd.Help()
			}
			var help bytes.Buffer
			previous := cmd.OutOrStdout()
			cmd.SetOut(&help)
			err := cmd.Help()
			cmd.SetOut(previous)
			if err != nil {
				return err
			}
			deps.Printer.Success(commandOutputName(cmd), map[string]string{"help": help.String()})
			return nil
		}
	}
	for _, child := range children {
		defaultArgumentPolicies(child, deps)
	}
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
	case "status", "audit", "coverage", "verify", "gates", "delivery", "change", "feature", "open", "ui":
		return true
	case "work":
		return len(path) < 3 || (path[2] != "create" && path[2] != "create-quick")
	case "transfer":
		return len(path) == 3 && path[2] == "prepare"
	case "artifact":
		return len(path) < 3 || path[2] != "publish"
	default:
		return false
	}
}

func assignRootCommandGroups(root *cobra.Command) {
	groups := map[string]string{
		"change":       commandGroupStart,
		"doctor":       commandGroupStart,
		"init":         commandGroupStart,
		"plugins":      commandGroupStart,
		"status":       commandGroupStart,
		"work":         commandGroupStart,
		"open":         commandGroupBrowser,
		"ui":           commandGroupBrowser,
		"audit":        commandGroupAdvanced,
		"artifact":     commandGroupAdvanced,
		"capabilities": commandGroupAdvanced,
		"cleanup":      commandGroupAdvanced,
		"coverage":     commandGroupAdvanced,
		"delivery":     commandGroupAdvanced,
		"feature":      commandGroupAdvanced,
		"gates":        commandGroupAdvanced,
		"transfer":     commandGroupAdvanced,
		"uninstall":    commandGroupAdvanced,
		"update":       commandGroupAdvanced,
		"user":         commandGroupAdvanced,
		"verify":       commandGroupAdvanced,
		"workspace":    commandGroupAdvanced,
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

// guaranteeErrorEnvelope enforces the machine-output contract: every invocation
// emits exactly one envelope. Several commands return an ExitError directly —
// a missing work ref, a prompt that cannot run under --no-input, an invalid
// provider — without printing one, which left a `--json` caller with a bare
// exit code and empty stdout. An IDE agent cannot act on that: it sees neither
// what failed nor what to do. Wrapping every RunE is the one place that covers
// present and future sites alike.
func guaranteeErrorEnvelope(root *cobra.Command, deps *Deps) {
	var wrap func(cmd *cobra.Command)
	wrap = func(cmd *cobra.Command) {
		if run := cmd.RunE; run != nil {
			cmd.RunE = func(c *cobra.Command, args []string) error {
				err := run(c, args)
				if err == nil || deps.Printer == nil || deps.Printer.Emitted() {
					return err
				}
				var exit *output.ExitError
				message := err.Error()
				code := "usage"
				if errors.As(err, &exit) {
					if exit.Err == nil {
						// A code-only ExitError means the command already
						// reported through another printer; leave it alone.
						return err
					}
					message = exit.Err.Error()
					code = output.ErrorCodeForExit(exit.Code)
				}
				deps.Printer.Error(commandOutputName(c), output.ErrorPayload{Code: code, Message: message})
				return err
			}
		}
		for _, child := range cmd.Commands() {
			wrap(child)
		}
	}
	wrap(root)
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
