package command

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/buildinfo"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/fsutil"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

const transferScope = "ENTIRE selected Local store"
const transferContents = "The snapshot contains private Local history. Keep this file private and receive only a package you trust. Checksums check consistency; they do not authenticate the author."
const transferExternal = "Carry Git/code and uncommitted or untracked files separately. External evidence (including ignored .specgate/work files) stays referenced by path. CLI configuration, credentials, toolchains, .env, UI sessions, caches, backups and physical SQLite sidecars stay local. Pinned verification commands stay unchanged; check destination paths and tools before running them."
const transferSequential = "Stop agents and UI writers on the source before switching. A snapshot cannot detect later offline writes; receiving never merges or deletes earlier stores."

var errTransferConflict = errors.New("conflicting transfer state")

// Check the raw path before the platform can normalize away trailing spaces.
func absoluteTransferStorePath(path string) (string, error) {
	if !transferConfigPathRepresentable(path) {
		return "", errors.New("receiving path cannot have surrounding whitespace")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if !transferConfigPathRepresentable(absolute) {
		return "", errors.New("receiving path cannot have surrounding whitespace")
	}
	return absolute, nil
}

func newTransferCmd(deps *Deps) *cobra.Command {
	group := &cobra.Command{Use: "transfer", Short: "Move a private Local snapshot between checkouts"}
	var work, archive, checkpoint, note string
	prepare := &cobra.Command{
		Use:   "prepare --work <ref> --file <archive>",
		Short: "Prepare a private snapshot of the entire selected Local store",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(work) == "" || strings.TrimSpace(archive) == "" {
				return transferError(deps, cmd, "usage", "--work and --file are required", nil)
			}
			if !deps.Yes && !sessionInteractive(deps) {
				return transferError(deps, cmd, "confirmation_required", "Use --yes to confirm this snapshot of the ENTIRE selected Local store.", nil)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), deps.Timeout)
			defer cancel()
			return prepareTransfer(ctx, cmd, deps, archive, work, checkpoint, note)
		},
	}
	prepare.Flags().StringVar(&work, "work", "", "Required pickup work reference in the selected workspace")
	prepare.Flags().StringVar(&archive, "file", "", "New destination for the private ZIP archive")
	prepare.Flags().StringVar(&checkpoint, "checkpoint", "", "Existing checkpoint in the pickup work")
	prepare.Flags().StringVar(&note, "note", "", "Optional private pickup note")

	var inspectFile string
	inspect := &cobra.Command{
		Use:   "inspect --file <archive>",
		Short: "Verify a private Local snapshot and show its scope and SHA-256",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(inspectFile) == "" {
				return transferError(deps, cmd, "usage", "--file is required", nil)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), deps.Timeout)
			defer cancel()
			scratch, err := os.MkdirTemp("", ".specgate-transfer-")
			if err != nil {
				return transferFailure(deps, cmd, "unavailable", err)
			}
			defer os.RemoveAll(scratch)
			result, err := readTransferPackage(ctx, inspectFile, scratch, "")
			if err != nil {
				return transferFailure(deps, cmd, "validation", err)
			}
			if deps.Mode == output.ModeJSON {
				deps.Printer.Success(commandOutputName(cmd), result)
			} else {
				printTransferSummary(deps.Stdout, result)
			}
			return nil
		},
	}
	inspect.Flags().StringVar(&inspectFile, "file", "", "Private ZIP archive to verify")

	var receiveFile, into, expected string
	receive := &cobra.Command{
		Use:   "receive --file <archive> --into <new-directory>",
		Short: "Restore a verified private snapshot into a new Local store",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(receiveFile) == "" || strings.TrimSpace(into) == "" {
				return transferError(deps, cmd, "usage", "--file and --into are required", nil)
			}
			sha := strings.ToLower(strings.TrimSpace(expected))
			if (!sessionInteractive(deps) || sha != "") && !validLowerHex(sha, 32) {
				return transferError(deps, cmd, "usage", "Provide a verified archive SHA-256 with --expect-snapshot.", nil)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), deps.Timeout)
			defer cancel()
			return receiveTransfer(ctx, cmd, deps, receiveFile, into, sha)
		},
	}
	receive.Flags().StringVar(&receiveFile, "file", "", "Private ZIP archive to receive")
	receive.Flags().StringVar(&into, "into", "", "New receiving directory")
	receive.Flags().StringVar(&expected, "expect-snapshot", "", "Verified archive SHA-256 (required without an interactive terminal)")
	group.AddCommand(prepare, inspect, receive, newTransferActivateCmd(deps))
	return group
}

func transferError(deps *Deps, cmd *cobra.Command, code, message string, details map[string]any) error {
	exit := deps.Printer.Error(commandOutputName(cmd), output.ErrorPayload{Code: code, Message: message, Details: details})
	return &output.ExitError{Code: exit}
}

func transferFailure(deps *Deps, cmd *cobra.Command, fallback string, err error) error {
	code := fallback
	switch {
	case errors.Is(err, local.ErrStoreIncompatible):
		code = "incompatible"
	case errors.Is(err, errTransferConflict), errors.Is(err, os.ErrExist):
		code = "conflict"
	case errors.Is(err, os.ErrNotExist), errors.Is(err, sql.ErrNoRows):
		code = "not_found"
	case errors.Is(err, errTransferTooLarge):
		code = "package_too_large"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		code = "unavailable"
	}
	return transferError(deps, cmd, code, err.Error(), nil)
}

func prepareTransfer(ctx context.Context, cmd *cobra.Command, deps *Deps, archive, work, checkpoint, note string) error {
	archive, err := filepath.Abs(archive)
	if err != nil {
		return transferFailure(deps, cmd, "usage", err)
	}
	if err := requireNewTransferPath(archive); err != nil {
		return transferFailure(deps, cmd, "usage", err)
	}
	source, err := localStatePath(deps)
	if err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	cfgPath, err := configPathForDeps(deps)
	if err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	if transferDestinationAliases(cfgPath, archive) {
		return transferError(deps, cmd, "conflict", "Archive destination aliases the CLI configuration.", nil)
	}
	cfg, err := config.LoadFrom(cfgPath)
	if err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	resolved := resolveWorkspaceSelection(deps, cfg)
	var workspaceRef string
	if resolved.Source != config.WorkspaceSourceGlobal && resolved.Source != config.WorkspaceSourceNone {
		workspaceRef = resolved.Workspace.ID
		if workspaceRef == "" {
			workspaceRef = resolved.Workspace.Slug
		}
	}
	git, err := captureTransferGit(ctx, deps)
	if err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	scratch, err := os.MkdirTemp(filepath.Dir(archive), ".specgate-transfer-")
	if err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	defer os.RemoveAll(scratch)
	captured := time.Now().UTC()
	dbPath := filepath.Join(scratch, "state.db")
	if err := local.CreateTransferSnapshot(ctx, source, dbPath); err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	if info, err := os.Stat(dbPath); err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	} else if info.Size() > transferPayloadMax {
		return transferError(deps, cmd, "package_too_large", "state.db is limited to 512 MiB.", nil)
	}
	inventory, err := local.ValidateTransferSnapshot(ctx, dbPath, workspaceRef, work, checkpoint)
	if err != nil {
		return transferFailure(deps, cmd, "validation", err)
	}
	zipPath := filepath.Join(scratch, "archive.zip")
	result, err := writeTransferPackage(ctx, dbPath, zipPath, localTransferManifest{
		Format: transferFormat, CLIVersion: buildinfo.Version, CapturedAt: captured,
		Inventory: inventory, Git: git, Note: strings.TrimSpace(note),
	})
	if err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	if sessionInteractive(deps) && !deps.Yes {
		printTransferSummary(deps.Stderr, result)
		ok, err := requireConfirm(deps, "Save this private snapshot of the entire Local store?")
		if err != nil || !ok {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	if err := requireNewTransferPath(archive); err != nil {
		return transferFailure(deps, cmd, "usage", err)
	}
	if err := os.Link(zipPath, archive); err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	if deps.Mode == output.ModeJSON {
		deps.Printer.Success(commandOutputName(cmd), struct {
			localTransferPackage
			File string `json:"file"`
		}{result, archive})
	} else {
		printTransferSummary(deps.Stdout, result)
		fmt.Fprintf(deps.Stdout, "Saved private snapshot to %q\n", archive)
	}
	return nil
}

func receiveTransfer(ctx context.Context, cmd *cobra.Command, deps *Deps, input, into, sha string) error {
	into, err := absoluteTransferStorePath(into)
	if err != nil {
		return transferFailure(deps, cmd, "usage", err)
	}
	if err := requireNewTransferPath(into); err != nil {
		return transferFailure(deps, cmd, "usage", err)
	}
	scratch, err := os.MkdirTemp(filepath.Dir(into), ".specgate-receive-")
	if err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	defer os.RemoveAll(scratch)
	result, err := readTransferPackage(ctx, input, scratch, sha)
	if err != nil {
		return transferFailure(deps, cmd, "validation", err)
	}
	if err := os.Remove(filepath.Join(scratch, "archive.zip")); err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	if sessionInteractive(deps) && (!deps.Yes || sha == "") {
		printTransferSummary(deps.Stderr, result)
		promptDeps := *deps
		promptDeps.Yes = false
		ok, err := requireConfirm(&promptDeps, fmt.Sprintf("Receive this snapshot into the new store %q?", into))
		if err != nil || !ok {
			return err
		}
	}
	dbPath := filepath.Join(scratch, "state.db")
	generation, err := local.ReceiveTransferGeneration(ctx, dbPath)
	if err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	book := result.Manifest.Inventory.Bookmark
	actual, err := local.ValidateTransferSnapshot(ctx, dbPath, book.Workspace.ID, book.WorkID, book.CheckpointID)
	if err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	if actual.StoreID != result.Manifest.Inventory.StoreID || actual.Generation != generation || actual.Bookmark != book {
		return transferError(deps, cmd, "unavailable", "Reopened receiving store did not match the intended pickup.", nil)
	}
	receipt := localTransferReceipt{localTransferPackage: result, ReceivingGeneration: generation}
	if err := writeTransferReceipt(filepath.Join(scratch, transferReceiptName), receipt); err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	if err := ctx.Err(); err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	if err := requireNewTransferPath(into); err != nil {
		return transferFailure(deps, cmd, "usage", err)
	}
	if err := fsutil.RenameNewDirectory(scratch, into); err != nil {
		return transferFailure(deps, cmd, "unavailable", err)
	}
	activate := "specgate transfer activate --from " + transferQuote(runtime.GOOS, into) + " --repo " + transferQuote(runtime.GOOS, "<local-git-root>")
	if deps.Mode == output.ModeJSON {
		deps.Printer.Success(commandOutputName(cmd), struct {
			localTransferReceipt
			Directory       string `json:"directory"`
			Activated       bool   `json:"activated"`
			ActivateCommand string `json:"activate_command"`
		}{receipt, into, false, activate})
	} else {
		printTransferSummary(deps.Stdout, result)
		fmt.Fprintf(deps.Stdout, "Received an unbound Local store at %q (generation %s).\nNext: %s\n", into, generation, activate)
	}
	return nil
}

func printTransferSummary(w io.Writer, result localTransferPackage) {
	m := result.Manifest
	inv := m.Inventory
	fmt.Fprintf(w, "%s (%d workspaces, %d users, %d work items)\n", transferScope, inv.WorkspaceCount, inv.UserCount, inv.WorkCount)
	for _, workspace := range inv.Workspaces {
		fmt.Fprintf(w, "Workspace %q (%q, %q)\n", workspace.Name, workspace.Slug, workspace.ID)
	}
	for _, user := range inv.Users {
		fmt.Fprintf(w, "User %q (%q)\n", user.Username, user.ID)
	}
	fmt.Fprintf(w, "Store %s, format %q, generation %q; snapshot SHA-256 %s\n", inv.StoreID, inv.StoreVersion, inv.Generation, result.SnapshotSHA)
	fmt.Fprintf(w, "Pickup %q in workspace %q", inv.Bookmark.WorkKey, inv.Bookmark.Workspace.Slug)
	if inv.Bookmark.CheckpointID != "" {
		fmt.Fprintf(w, ", checkpoint %q", inv.Bookmark.CheckpointID)
	}
	fmt.Fprintln(w)
	if m.Note != "" {
		fmt.Fprintf(w, "Pickup note %q\n", m.Note)
	}
	if m.Git.Repository != "" {
		fmt.Fprintf(w, "Git repository %q\n", m.Git.Repository)
	}
	if m.Git.Head != "" {
		fmt.Fprintf(w, "Git branch %q, HEAD %s, tree %s\n", m.Git.Branch, m.Git.Head, m.Git.Tree)
	}
	switch {
	case m.Git.Dirty == nil:
		fmt.Fprintln(w, "Git working tree status was unavailable.")
	case *m.Git.Dirty:
		fmt.Fprintln(w, "The checkout has uncommitted or untracked files.")
	}
	fmt.Fprintln(w, transferContents)
	fmt.Fprintln(w, transferExternal)
	fmt.Fprintln(w, transferSequential)
}
