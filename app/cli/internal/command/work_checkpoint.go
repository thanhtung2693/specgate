package command

import (
	"database/sql"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

type checkpointOutputView struct {
	local.Checkpoint
	DirtyFileCount   int `json:"dirty_file_count,omitempty"`
	ChangedFileCount int `json:"changed_file_count,omitempty"`
}

func checkpointOutput(checkpoint local.Checkpoint) checkpointOutputView {
	view := checkpointOutputView{Checkpoint: checkpoint, ChangedFileCount: len(checkpoint.ChangedFiles)}
	view.Checkpoint.ChangedFiles = nil
	if checkpoint.Snapshot != nil {
		snapshot := *checkpoint.Snapshot
		view.DirtyFileCount = len(snapshot.Files)
		snapshot.Files = nil
		view.Checkpoint.Snapshot = &snapshot
	}
	return view
}

func newWorkCheckpointCmd(deps *Deps) *cobra.Command {
	var note string
	cmd := &cobra.Command{Use: "checkpoint <ref>", Short: "Record an explicit Local pickup baseline", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		const op = "work.checkpoint"
		if deps.Topology != config.ModeLocal {
			return incompatibleCommand(deps, op, "checkpoints are Local-only")
		}
		store, err := openLocalStore(deps)
		if err != nil {
			return localExitError(deps, op, err)
		}
		defer store.Close()
		sel, err := localSelection(cmd.Context(), deps, store)
		if err != nil {
			return localExitError(deps, op, err)
		}
		receipt := collectGitReceipt(cmd.Context(), deps.DeployRunner, deliveryWorkingDir(deps), nil)
		upgrade, err := store.PendingStoreUpgrade(cmd.Context())
		if err != nil {
			return localExitError(deps, op, err)
		}
		proceed, err := confirmStoreWrite(deps, op, "Record this Local checkpoint? It is append-only and does not change scope.", upgrade)
		if err != nil || !proceed {
			return err
		}
		snapshot := captureCheckout(cmd.Context(), deps)
		if snapshot.State != "available" {
			return localExitError(deps, op, fmt.Errorf("cannot record checkpoint: %s", snapshot.Reason))
		}
		checkpoint, err := store.CreateCheckpointWithFiles(cmd.Context(), sel.Workspace.ID, args[0], snapshot.Fingerprint, receipt.ChangedFiles, note, snapshot)
		if err != nil {
			return localExitError(deps, op, err)
		}
		if deps.Printer.Mode() == output.ModeJSON {
			deps.Printer.Success(op, struct {
				checkpointOutputView
				Upgrade *local.StoreUpgrade `json:"store_upgrade,omitempty"`
			}{checkpointOutput(checkpoint), upgrade})
			return nil
		}
		fmt.Fprintf(deps.Stdout, "Checkpoint %s recorded for %s\n", checkpoint.ID, args[0])
		return nil
	}}
	cmd.Flags().StringVar(&note, "note", "", "Optional author-supplied pickup note")
	return cmd
}

func selectedCheckpoint(store *local.Store, cmd *cobra.Command, workspaceID, ref, id, checkoutID string) (*local.Checkpoint, error) {
	var checkpoint local.Checkpoint
	var err error
	if id == "" {
		checkpoint, err = store.LatestCheckpointForCheckout(cmd.Context(), workspaceID, ref, checkoutID)
	} else {
		checkpoint, err = store.GetCheckpoint(cmd.Context(), workspaceID, ref, id)
	}
	if err == sql.ErrNoRows {
		if id != "" {
			return nil, fmt.Errorf("%w: checkpoint does not belong to this work and workspace", local.ErrReviewChanged)
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &checkpoint, nil
}
