package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/fsutil"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

type transferBinding struct {
	LocalDir   string                  `json:"local_dir"`
	StoreID    string                  `json:"store_id"`
	Generation string                  `json:"checkout_generation"`
	Workspace  config.CurrentWorkspace `json:"workspace"`
}

type transferActivation struct {
	Repo       string          `json:"repo"`
	ConfigPath string          `json:"config_path"`
	Old        transferBinding `json:"old_binding"`
	New        transferBinding `json:"new_binding"`
}

func newTransferActivateCmd(deps *Deps) *cobra.Command {
	var from, repo string
	cmd := &cobra.Command{
		Use:   "activate --from <received-directory> --repo <local-git-root>",
		Short: "Explicitly bind one Git checkout to a received Local store",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(from) == "" || strings.TrimSpace(repo) == "" {
				return transferError(deps, cmd, "usage", "--from and --repo are required", nil)
			}
			if !deps.Yes && !sessionInteractive(deps) {
				return transferError(deps, cmd, "confirmation_required", "Use --yes to confirm changing this repository's Local binding.", nil)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), deps.Timeout)
			defer cancel()
			return activateTransfer(ctx, cmd, deps, from, repo)
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "Directory published by transfer receive")
	cmd.Flags().StringVar(&repo, "repo", "", "Root of the receiving Local Git checkout")
	return cmd
}

func writeTransferReceipt(path string, receipt localTransferReceipt) error {
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	if len(data) > transferManifestMax+1024 {
		return fmt.Errorf("%w: received receipt exceeds the manifest limit", errTransferTooLarge)
	}
	return fsutil.AtomicWriteFile(path, data, 0o600)
}

func readTransferReceipt(ctx context.Context, dir string) (localTransferReceipt, error) {
	var receipt localTransferReceipt
	input, err := openTransferInput(filepath.Join(dir, transferReceiptName))
	if err != nil {
		return receipt, err
	}
	defer input.Close()
	var body bytes.Buffer
	if _, _, err := copyTransferBytes(ctx, &body, input, transferManifestMax+1024, "received receipt"); err != nil {
		return receipt, err
	}
	if err := decodeTransferJSON(body.Bytes(), &receipt); err != nil {
		return receipt, fmt.Errorf("invalid received transfer receipt: %w", err)
	}
	if err := receipt.Manifest.validate(); err != nil {
		return receipt, err
	}
	if !validLowerHex(receipt.ReceivingGeneration, 16) || !validLowerHex(receipt.SnapshotSHA, 32) ||
		receipt.ReceivingGeneration == receipt.Manifest.Inventory.Generation {
		return receipt, fmt.Errorf("received receipt has invalid snapshot SHA-256 or receiving checkout generation")
	}
	return receipt, nil
}

func verifyReceivedTransfer(ctx context.Context, dir string, receipt localTransferReceipt) (transferBinding, error) {
	book := receipt.Manifest.Inventory.Bookmark
	actual, err := local.ReadReceivedTransferBookmark(ctx, filepath.Join(dir, "state.db"), book)
	if err != nil {
		return transferBinding{}, fmt.Errorf("verify received Local store: %w", err)
	}
	if actual.StoreID != receipt.Manifest.Inventory.StoreID || actual.Generation != receipt.ReceivingGeneration || actual.Bookmark != book {
		return transferBinding{}, fmt.Errorf("%w: received store identity, generation or pickup does not match its receipt", errTransferConflict)
	}
	return transferBinding{dir, actual.StoreID, actual.Generation, localWorkspaceConfig(actual.Bookmark.Workspace)}, nil
}

func activateTransfer(ctx context.Context, cmd *cobra.Command, deps *Deps, from, repo string) error {
	from, err := absoluteTransferStorePath(from)
	if err != nil {
		return transferFailure(deps, cmd, "usage", err)
	}
	if err := rejectSymlinkedRemovalAncestors(filepath.Join(from, transferReceiptName)); err != nil {
		return transferFailure(deps, cmd, "usage", err)
	}
	repo, err = filepath.Abs(repo)
	if err != nil {
		return transferFailure(deps, cmd, "usage", err)
	}
	repo, err = filepath.EvalSymlinks(repo)
	if err != nil {
		return transferFailure(deps, cmd, "usage", err)
	}
	cfgPath, err := configPathForDeps(deps)
	if err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	cfgPath, err = filepath.Abs(cfgPath)
	if err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	if !transferConfigPathRepresentable(cfgPath) {
		return transferError(deps, cmd, "usage", "Selected config path cannot end in whitespace that SPECGATE_CONFIG_PATH strips.", nil)
	}
	workspaceOverride, _ := cmd.Flags().GetString("workspace")
	preview, cfgBefore, receipt, err := previewTransferActivation(ctx, deps, repo, from, cfgPath, workspaceOverride)
	if err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	if sessionInteractive(deps) && !deps.Yes {
		printTransferActivation(deps.Stderr, preview)
		ok, err := requireConfirm(deps, "Bind only this repository to the received Local store?")
		if err != nil || !ok {
			return err
		}
	}
	fresh, cfgNow, receiptNow, err := previewTransferActivation(ctx, deps, repo, from, cfgPath, workspaceOverride)
	if err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	if preview != fresh || !bytes.Equal(cfgBefore, cfgNow) || receipt.SnapshotSHA != receiptNow.SnapshotSHA {
		return transferError(deps, cmd, "conflict", "Configuration or the Local binding changed after preview. Review it again before activating.", nil)
	}
	if err := ctx.Err(); err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	if err := saveTransferBinding(cfgPath, cfgBefore, preview); err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	if err := verifyTransferActivation(ctx, deps, preview, receipt, workspaceOverride); err != nil {
		recovery := preview
		recovery.New = transferBinding{}
		return transferError(deps, cmd, "unavailable",
			"Activation committed, but verification failed. Inspect the config and both stores before retrying. "+err.Error(),
			map[string]any{
				"activation_committed": true, "verified": false, "repo": repo, "config_path": cfgPath,
				"old_local_dir": preview.Old.LocalDir, "new_local_dir": preview.New.LocalDir,
				"recovery_command": transferReadCommand(runtime.GOOS, recovery, "doctor", "--json"), "command_shell": transferCommandShell(runtime.GOOS),
			})
	}
	pickup := []string{receipt.Manifest.Inventory.Bookmark.WorkKey}
	if id := receipt.Manifest.Inventory.Bookmark.CheckpointID; id != "" {
		pickup = append(pickup, "--since", id)
	}
	resume := transferReadCommand(runtime.GOOS, preview, "work resume", append(pickup, "--json")...)
	shell := transferCommandShell(runtime.GOOS)
	if deps.Mode == output.ModeJSON {
		deps.Printer.Success(commandOutputName(cmd), struct {
			transferActivation
			ActivationCommitted bool   `json:"activation_committed"`
			Verified            bool   `json:"verified"`
			SnapshotSHA         string `json:"snapshot_sha256"`
			Resume              string `json:"resume_command"`
			Shell               string `json:"command_shell"`
			PickupNote          string `json:"pickup_note,omitempty"`
		}{preview, true, true, receipt.SnapshotSHA, resume, shell, receipt.Manifest.Note})
	} else {
		printTransferActivation(deps.Stdout, preview)
		fmt.Fprintln(deps.Stdout, "The project binding was saved and verified.")
		if note := receipt.Manifest.Note; note != "" {
			fmt.Fprintf(deps.Stdout, "Pickup note %q\n", note)
		}
		fmt.Fprintln(deps.Stdout, transferSequential)
		fmt.Fprintf(deps.Stdout, "Resume using %s\n%s\n", shell, resume)
	}
	return nil
}

func previewTransferActivation(ctx context.Context, deps *Deps, repo, from, cfgPath, workspaceOverride string) (transferActivation, []byte, localTransferReceipt, error) {
	preview := transferActivation{Repo: repo, ConfigPath: cfgPath}
	var receipt localTransferReceipt
	cfg, configBytes, err := loadTransferConfig(cfgPath, repo)
	if err != nil {
		return preview, nil, receipt, err
	}
	for _, protected := range []string{filepath.Join(repo, ".specgate", "config"), filepath.Join(from, transferReceiptName),
		filepath.Join(from, "state.db"), filepath.Join(from, "state.db-wal"), filepath.Join(from, "state.db-shm"),
		filepath.Join(from, "state.db-journal"), filepath.Join(from, "state.db.pre-enhanced.bak")} {
		if transferPathsAlias(cfgPath, protected) {
			return preview, nil, receipt, fmt.Errorf("%w: selected CLI config aliases repository or Local store state", errTransferConflict)
		}
	}
	receipt, err = readTransferReceipt(ctx, from)
	if err != nil {
		return preview, nil, receipt, err
	}
	if err := verifyTransferRepository(ctx, deps, repo, receipt.Manifest.Git); err != nil {
		return preview, nil, receipt, fmt.Errorf("%w: %v", errTransferConflict, err)
	}
	preview.New, err = verifyReceivedTransfer(ctx, from, receipt)
	if err != nil {
		return preview, nil, receipt, err
	}
	if err := checkTransferOverrides(preview.New, workspaceOverride); err != nil {
		return preview, nil, receipt, err
	}
	project := cfg.Projects[repo]
	oldDir := config.ResolveLocalDir("", os.Getenv("SPECGATE_LOCAL_DIR"), project, cfg, "")
	if oldDir != "" {
		preview.Old.LocalDir, err = filepath.Abs(oldDir)
		if err != nil {
			return preview, nil, receipt, err
		}
		resolved := config.ResolveWorkspaceSource(cfg, config.LoadRepoConfig(repo), repo, "")
		ref := refForTransferBinding(resolved)
		if override := strings.TrimSpace(os.Getenv("SPECGATE_WORKSPACE")); override != "" {
			ref = override
		}
		if override := strings.TrimSpace(workspaceOverride); override != "" {
			ref = override
		}
		actual, err := local.ReadTransferBinding(ctx, filepath.Join(preview.Old.LocalDir, "state.db"), ref)
		if err != nil {
			return preview, nil, receipt, fmt.Errorf("verify prior effective Local binding: %w", err)
		}
		expected := cfg.Local.ID
		if project.Local.Path != "" {
			expected = project.Local.ID
		}
		if strings.TrimSpace(os.Getenv("SPECGATE_LOCAL_DIR")) == "" && expected != "" && expected != actual.StoreID {
			return preview, nil, receipt, fmt.Errorf("%w: prior Local store identity differs from the configured binding", errTransferConflict)
		}
		preview.Old.StoreID, preview.Old.Generation = actual.StoreID, actual.Generation
		preview.Old.Workspace = localWorkspaceConfig(actual.Bookmark.Workspace)
	}
	return preview, configBytes, receipt, nil
}

func transferConfigPathRepresentable(path string) bool {
	return strings.TrimSpace(path) == path
}

func refForTransferBinding(resolved config.ResolvedWorkspace) string {
	if resolved.Source == config.WorkspaceSourceGlobal {
		return ""
	}
	if resolved.Workspace.ID != "" {
		return resolved.Workspace.ID
	}
	return resolved.Workspace.Slug
}

func checkTransferOverrides(binding transferBinding, workspaceOverride string) error {
	if env := strings.TrimSpace(os.Getenv("SPECGATE_LOCAL_DIR")); env != "" {
		if !filepath.IsAbs(env) {
			return fmt.Errorf("%w: use the receiving directory's absolute path in SPECGATE_LOCAL_DIR or unset it", errTransferConflict)
		}
		if err := rejectSymlinkedRemovalAncestors(filepath.Join(env, "state.db")); err != nil {
			return fmt.Errorf("%w: SPECGATE_LOCAL_DIR must use a real Local directory without symlink components", errTransferConflict)
		}
		if !sameTransferFile(env, binding.LocalDir) {
			return fmt.Errorf("%w: SPECGATE_LOCAL_DIR selects another store, so unset it or select the receiving directory", errTransferConflict)
		}
	}
	for _, selection := range []struct{ key, value string }{
		{"--workspace", workspaceOverride}, {"SPECGATE_WORKSPACE", os.Getenv("SPECGATE_WORKSPACE")},
	} {
		ref := strings.TrimSpace(selection.value)
		if ref != "" && ref != binding.Workspace.ID && ref != binding.Workspace.Slug {
			return fmt.Errorf("%w: %s selects another workspace, so clear the override or select the pickup workspace", errTransferConflict, selection.key)
		}
	}
	return nil
}

func transferPathsAlias(a, b string) bool {
	if transferDestinationAliases(a, b) {
		return true
	}
	return sameTransferFile(a, b)
}

func sameTransferFile(a, b string) bool {
	x, xErr := os.Stat(a)
	y, yErr := os.Stat(b)
	return xErr == nil && yErr == nil && os.SameFile(x, y)
}

func loadTransferConfig(path, repo string) (config.Config, []byte, error) {
	var cfg config.Config
	if err := validateRegularFileIfExists(path); err != nil {
		return cfg, nil, err
	}
	body, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return cfg, nil, err
	}
	cfg, err = config.Parse(body)
	if err != nil {
		// Parse errors can contain arbitrary values from a secret-bearing file.
		return cfg, nil, fmt.Errorf("selected SpecGate config is malformed; repair it before activation")
	}
	repoPath := filepath.Join(repo, ".specgate", "config")
	rawRepo, err := os.ReadFile(repoPath)
	if err != nil && !os.IsNotExist(err) {
		return cfg, nil, fmt.Errorf("could not read the target repository's .specgate/config")
	}
	var repoConfig config.RepoConfig
	if rawRepo != nil && (json.Unmarshal(rawRepo, &repoConfig) != nil || !bytes.HasPrefix(bytes.TrimSpace(rawRepo), []byte{'{'})) {
		return cfg, nil, fmt.Errorf("target repository's .specgate/config is malformed")
	}
	if cfg.LegacyFull() || (cfg.Mode == "" &&
		(strings.TrimSpace(os.Getenv("SPECGATE_SERVER")) != "" || strings.TrimSpace(repoConfig.Server) != "")) {
		return cfg, nil, fmt.Errorf("%w: selected Full mode is retired, so use a separate Local configuration", local.ErrStoreIncompatible)
	}
	return cfg, body, nil
}

// Patch only the project binding. Preserve unknown and legacy config fields
// as raw JSON so activation cannot erase another tool's settings.
func saveTransferBinding(path string, before []byte, activation transferActivation) error {
	root := map[string]json.RawMessage{}
	if before != nil {
		if err := json.Unmarshal(before, &root); err != nil {
			return err
		}
	}
	modeKey, err := transferConfigField(root, "mode")
	if err != nil {
		return err
	}
	if len(root[modeKey]) == 0 || bytes.Equal(root[modeKey], []byte(`""`)) || bytes.Equal(root[modeKey], []byte("null")) {
		root[modeKey] = json.RawMessage(`"local"`)
	}
	projectsKey, err := transferConfigField(root, "projects")
	if err != nil {
		return err
	}
	projects, err := transferConfigObject(root[projectsKey])
	if err != nil {
		return err
	}
	project, err := transferConfigObject(projects[activation.Repo])
	if err != nil {
		return err
	}
	localKey, err := transferConfigField(project, "local")
	if err != nil {
		return err
	}
	workspaceKey, err := transferConfigField(project, "workspace")
	if err != nil {
		return err
	}
	project[localKey], err = json.Marshal(config.LocalStore{Path: activation.New.LocalDir, ID: activation.New.StoreID})
	if err != nil {
		return err
	}
	project[workspaceKey], err = json.Marshal(activation.New.Workspace)
	if err != nil {
		return err
	}
	projects[activation.Repo], err = json.Marshal(project)
	if err != nil {
		return err
	}
	root[projectsKey], err = json.Marshal(projects)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := validateRegularFileIfExists(path); err != nil {
		return err
	}
	current, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !bytes.Equal(before, current) || (before == nil) != (current == nil) {
		return fmt.Errorf("%w: selected config changed during activation", errTransferConflict)
	}
	return fsutil.AtomicWriteFile(path, append(data, '\n'), 0o600)
}

func transferConfigField(object map[string]json.RawMessage, field string) (string, error) {
	var actual string
	for name := range object {
		if strings.EqualFold(name, field) {
			if actual != "" {
				return "", fmt.Errorf("selected SpecGate config has ambiguous casing for %s", field)
			}
			actual = name
		}
	}
	if actual == "" {
		actual = field
	}
	return actual, nil
}

func transferConfigObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil, err
		}
	}
	if object == nil {
		object = make(map[string]json.RawMessage)
	}
	return object, nil
}

func verifyTransferActivation(ctx context.Context, deps *Deps, preview transferActivation, receipt localTransferReceipt, workspaceOverride string) error {
	cfg, _, err := loadTransferConfig(preview.ConfigPath, preview.Repo)
	if err != nil {
		return err
	}
	if err := verifyTransferRepository(ctx, deps, preview.Repo, receipt.Manifest.Git); err != nil {
		return err
	}
	if err := checkTransferOverrides(preview.New, workspaceOverride); err != nil {
		return err
	}
	binding := cfg.Projects[preview.Repo]
	if binding.Local != (config.LocalStore{Path: preview.New.LocalDir, ID: preview.New.StoreID}) || binding.Workspace != preview.New.Workspace {
		return fmt.Errorf("the saved project binding no longer matches the reviewed binding")
	}
	resolved := config.ResolveWorkspaceSource(cfg, config.LoadRepoConfig(preview.Repo), preview.Repo, "")
	effectiveDir := config.ResolveLocalDir("", os.Getenv("SPECGATE_LOCAL_DIR"), binding, cfg, "")
	if !sameTransferFile(effectiveDir, preview.New.LocalDir) || resolved.Workspace != preview.New.Workspace {
		return fmt.Errorf("saved config does not resolve to the receiving Local store and workspace")
	}
	actual, err := verifyReceivedTransfer(ctx, preview.New.LocalDir, receipt)
	if err != nil {
		return err
	}
	if actual != preview.New {
		return fmt.Errorf("the effective received Local store or workspace changed")
	}
	return ctx.Err()
}

func printTransferActivation(w io.Writer, preview transferActivation) {
	fmt.Fprintf(w, "Repository %q, config %q\n", preview.Repo, preview.ConfigPath)
	for _, item := range []struct {
		label   string
		binding transferBinding
	}{{"Current effective binding", preview.Old}, {"Receiving binding", preview.New}} {
		if item.binding.LocalDir == "" {
			fmt.Fprintf(w, "%s is unconfigured.\n", item.label)
			continue
		}
		generation := item.binding.Generation
		if generation == "" {
			generation = "(original store)"
		}
		fmt.Fprintf(w, "%s uses %q, generation %s, workspace %q (%q).\n",
			item.label, item.binding.LocalDir, generation, item.binding.Workspace.Slug, item.binding.Workspace.ID)
	}
}

func transferCommandShell(goos string) string {
	if goos == "windows" {
		return "PowerShell"
	}
	return "POSIX shell"
}

func transferQuote(goos, value string) string {
	if goos == "windows" {
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	}
	return shellQuote(value)
}

func transferReadCommand(goos string, activation transferActivation, action string, args ...string) string {
	for i, arg := range args {
		if arg != "--since" && arg != "--json" {
			args[i] = transferQuote(goos, arg)
		}
	}
	quote := func(value string) string { return transferQuote(goos, value) }
	command := "specgate"
	if activation.New.Workspace.ID != "" {
		command += " --workspace " + quote(activation.New.Workspace.ID)
	}
	command += " " + action + " " + strings.Join(args, " ")
	if goos == "windows" {
		return "& { $sgTransferConfig = $env:SPECGATE_CONFIG_PATH; $sgTransferStore = $env:SPECGATE_LOCAL_DIR; $sgTransferWorkspace = $env:SPECGATE_WORKSPACE; " +
			"Push-Location -LiteralPath " + quote(activation.Repo) + " -ErrorAction Stop; try { " +
			"$env:SPECGATE_CONFIG_PATH = " + quote(activation.ConfigPath) + "; " +
			"$env:SPECGATE_LOCAL_DIR = " + quote(activation.New.LocalDir) + "; $env:SPECGATE_WORKSPACE = ''; & " + command +
			" } finally { $env:SPECGATE_CONFIG_PATH = $sgTransferConfig; $env:SPECGATE_LOCAL_DIR = $sgTransferStore; $env:SPECGATE_WORKSPACE = $sgTransferWorkspace; Pop-Location } }"
	}
	return "cd " + quote(activation.Repo) + " && SPECGATE_CONFIG_PATH=" + quote(activation.ConfigPath) +
		" SPECGATE_LOCAL_DIR=" + quote(activation.New.LocalDir) + " SPECGATE_WORKSPACE='' " + command
}
