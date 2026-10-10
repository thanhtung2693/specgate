package command

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
	"github.com/spf13/cobra"
)

type contextDocument struct {
	Path   string `json:"path"`
	Role   string `json:"role"`
	Digest string `json:"digest"`
}
type localContextView struct {
	Work            local.WorkItem    `json:"work"`
	ContextDigest   string            `json:"context_digest"`
	ArtifactVersion int               `json:"artifact_version,omitempty"`
	ArtifactDigest  string            `json:"artifact_digest,omitempty"`
	Documents       []contextDocument `json:"documents"`
	Guidance        string            `json:"guidance"`
}

type newerArtifactReference struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	Digest  string `json:"digest"`
	Status  string `json:"status"`
}

type checkpointDelta struct {
	State         string   `json:"state"`
	BaselineKind  string   `json:"baseline_kind"`
	BaselineID    string   `json:"baseline_id,omitempty"`
	Added         []string `json:"added,omitempty"`
	Removed       []string `json:"removed,omitempty"`
	Modified      []string `json:"modified,omitempty"`
	AddedCount    int      `json:"added_count,omitempty"`
	RemovedCount  int      `json:"removed_count,omitempty"`
	ModifiedCount int      `json:"modified_count,omitempty"`
	Truncated     bool     `json:"truncated,omitempty"`
	Limit         string   `json:"limit,omitempty"`
}

const resumeDeltaPathLimit = 100

// resumeDeltaOutput keeps a pickup packet bounded by default. The persisted
// checkpoint remains complete; --detail is an explicit, read-only request for
// every compared path.
func resumeDeltaOutput(delta checkpointDelta, detail bool) checkpointDelta {
	delta.AddedCount, delta.RemovedCount, delta.ModifiedCount = len(delta.Added), len(delta.Removed), len(delta.Modified)
	if detail || delta.AddedCount+delta.RemovedCount+delta.ModifiedCount <= resumeDeltaPathLimit {
		return delta
	}
	delta.Added, delta.Removed, delta.Modified = nil, nil, nil
	delta.Truncated = true
	const detailLimit = "path lists summarized; re-run with --detail to read every path"
	if delta.Limit == "" {
		delta.Limit = detailLimit
	} else {
		delta.Limit += "; " + detailLimit
	}
	return delta
}

// The projection adds work scope without changing legacy Context Pack bytes.
// It is an index: document non-goals must still be read, not inferred away.
func readLocalContextView(ctx context.Context, store *local.Store, workspaceID, ref string) (localContextView, error) {
	work, err := store.GetWork(ctx, workspaceID, ref)
	if err != nil {
		return localContextView{}, err
	}
	pack, err := store.ContextPack(ctx, workspaceID, ref)
	if err != nil {
		return localContextView{}, err
	}
	view := localContextView{Work: work, ContextDigest: pack.Digest, Documents: []contextDocument{}, Guidance: "Read the indexed pinned documents before implementation, including scope and non-goals. Use work context <ref> --document <path> or the full Context Pack."}
	if work.ArtifactID != "" {
		artifact, err := store.GetArtifact(ctx, workspaceID, work.ArtifactID)
		if err != nil {
			return localContextView{}, err
		}
		view.ArtifactVersion = artifact.Version
		view.ArtifactDigest = artifact.SnapshotDigest
		for _, doc := range artifact.Documents {
			view.Documents = append(view.Documents, contextDocument{Path: doc.Path, Role: doc.Role, Digest: doc.Digest})
		}
	} else {
		view.Guidance = "Quick work: the persisted description and acceptance criteria above define scope; no artifact documents are attached."
	}
	return view, nil
}

func printLocalContextView(deps *Deps, view localContextView) {
	fmt.Fprintf(deps.Stdout, "Work: %s — %s\nContext: %s\n%s\n", view.Work.Key, view.Work.Title, view.ContextDigest, view.Work.Description)
	for i, ac := range view.Work.AcceptanceCriteria {
		fmt.Fprintf(deps.Stdout, "local-%d: %s\n", i+1, ac)
	}
	for _, doc := range view.Documents {
		fmt.Fprintf(deps.Stdout, "Document: %s (%s, %s)\n", doc.Path, doc.Role, doc.Digest)
	}
	fmt.Fprintln(deps.Stdout, view.Guidance)
}

func newWorkResumeCmd(deps *Deps) *cobra.Command {
	var since string
	var detail bool
	cmd := &cobra.Command{Use: "resume <ref>", Short: "Read a Local work's scope, pinned document index and next action", Example: "  specgate work resume LOCAL-123 --json", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		const op = "work.resume"
		if deps.Topology != config.ModeLocal {
			return incompatibleCommand(deps, op, "work resume is Local-only; use work show, work context and change status in Full mode")
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
		view, err := readLocalContextView(cmd.Context(), store, sel.Workspace.ID, args[0])
		if err != nil {
			return localExitError(deps, op, err)
		}
		var newer *newerArtifactReference
		if view.Work.ArtifactID != "" {
			pinned, err := store.GetArtifact(cmd.Context(), sel.Workspace.ID, view.Work.ArtifactID)
			if err != nil {
				return localExitError(deps, op, err)
			}
			artifacts, err := store.ListArtifacts(cmd.Context(), sel.Workspace.ID)
			if err != nil {
				return localExitError(deps, op, err)
			}
			for _, artifact := range artifacts {
				if artifact.FeatureKey == pinned.FeatureKey && artifact.Version > pinned.Version && (newer == nil || artifact.Version > newer.Version) {
					newer = &newerArtifactReference{ID: artifact.ID, Version: artifact.Version, Digest: artifact.SnapshotDigest, Status: artifact.Status}
				}
			}
		}
		contract, err := store.GetVerificationContract(cmd.Context(), sel.Workspace.ID, args[0])
		if err != nil {
			return localExitError(deps, op, err)
		}
		status, report, err := deriveLocalChangeStatusWithContract(cmd.Context(), store, sel.Workspace.ID, view.Work, contract)
		if err != nil {
			return localExitError(deps, op, err)
		}
		if report != nil {
			status, err = augmentAcceptanceStatus(cmd, deps, store, sel.Workspace.ID, args[0], status)
			if err != nil {
				return localExitError(deps, op, err)
			}
			if inspection := status.Inspection; inspection != nil {
				view.Work, contract = inspection.Work, inspection.Contract
				report = &inspection.Report
			}
		}
		status.Risks = acceptanceRisks(status)
		currentSnapshot := captureCheckout(cmd.Context(), deps)
		currentReceipt := collectGitReceipt(cmd.Context(), deps.DeployRunner, deliveryWorkingDir(deps), nil)
		checkpoint, err := selectedCheckpoint(store, cmd, sel.Workspace.ID, args[0], since, currentSnapshot.CheckoutID)
		if err != nil {
			return localExitError(deps, op, err)
		}
		delta := checkpointDelta{State: "no_baseline", BaselineKind: "none"}
		if checkpoint != nil {
			delta = checkpointDelta{State: "unavailable", BaselineKind: "checkpoint", BaselineID: checkpoint.ID, Limit: "legacy checkpoint has no path manifest"}
			if checkpoint.Snapshot != nil {
				delta = compareCheckout(cmd.Context(), deps, *checkpoint.Snapshot, currentSnapshot)
				delta.BaselineKind, delta.BaselineID = "checkpoint", checkpoint.ID
			}
		} else if since == "" && report != nil {
			if baseline, ok := completionReceiptBaseline(mapGitReceipt(report.Body), currentReceipt, currentSnapshot); ok {
				delta = compareCommittedTrees(cmd.Context(), deps, baseline, currentSnapshot)
				delta.BaselineKind, delta.BaselineID = "last_completion_receipt", report.ID
				const receiptLimit = "last completion receipt has no dirty-file manifest; only committed Git-tree paths are listed"
				if delta.Limit == "" {
					delta.Limit = receiptLimit
				} else {
					delta.Limit += "; " + receiptLimit
				}
			}
		}
		delta = resumeDeltaOutput(delta, detail)
		if deps.Printer.Mode() == output.ModeJSON {
			var checkpointView *checkpointOutputView
			if checkpoint != nil {
				view := checkpointOutput(*checkpoint)
				checkpointView = &view
			}
			deps.Printer.Success(op, struct {
				localContextView
				Verification  local.VerificationContract `json:"verification_contract"`
				Status        changeStatusResult         `json:"status"`
				Checkpoint    *checkpointOutputView      `json:"checkpoint,omitempty"`
				NewerArtifact *newerArtifactReference    `json:"newer_artifact,omitempty"`
				Delta         checkpointDelta            `json:"delta"`
			}{view, contract, status, checkpointView, newer, delta})
			return nil
		}
		printLocalContextView(deps, view)
		printChangeStatus(deps, status)
		if checkpoint != nil {
			fmt.Fprintf(deps.Stdout, "Checkpoint baseline: %s (%s)\n", checkpoint.ID, checkpoint.CreatedAt)
		}
		if newer != nil {
			fmt.Fprintf(deps.Stdout, "Newer artifact available: %s v%d (%s); informational only, pinned scope unchanged\n", newer.ID, newer.Version, newer.Status)
		}
		fmt.Fprintf(deps.Stdout, "Delta: %s (%s)\n", delta.State, delta.BaselineKind)
		fmt.Fprintf(deps.Stdout, "Paths: added: %d, removed: %d, modified: %d\n", delta.AddedCount, delta.RemovedCount, delta.ModifiedCount)
		for _, group := range []struct {
			name  string
			paths []string
		}{{"Added", delta.Added}, {"Removed", delta.Removed}, {"Modified", delta.Modified}} {
			for _, path := range group.paths {
				fmt.Fprintf(deps.Stdout, "%s: %s\n", group.name, path)
			}
		}
		if delta.Limit != "" {
			fmt.Fprintf(deps.Stdout, "Limit: %s\n", delta.Limit)
		}
		return nil
	}}
	cmd.Flags().StringVar(&since, "since", "", "Compare resume context to an exact Local checkpoint")
	cmd.Flags().BoolVar(&detail, "detail", false, "Include every compared path when a resume delta is large")
	return cmd
}

func runLocalContextProjection(cmd *cobra.Command, deps *Deps, ref, document, role string) error {
	const op = "work.context"
	store, err := openLocalStore(deps)
	if err != nil {
		return localExitError(deps, op, err)
	}
	defer store.Close()
	sel, err := localSelection(cmd.Context(), deps, store)
	if err != nil {
		return localExitError(deps, op, err)
	}
	view, err := readLocalContextView(cmd.Context(), store, sel.Workspace.ID, ref)
	if err != nil {
		return localExitError(deps, op, err)
	}
	if document != "" {
		if view.Work.ArtifactID == "" {
			return localExitError(deps, op, sql.ErrNoRows)
		}
		artifact, err := store.GetArtifact(cmd.Context(), sel.Workspace.ID, view.Work.ArtifactID)
		if err != nil {
			return localExitError(deps, op, err)
		}
		var matches []local.ArtifactDocument
		for _, doc := range artifact.Documents {
			if doc.Path == document && (role == "" || doc.Role == role) {
				matches = append(matches, doc)
			}
		}
		if len(matches) == 0 {
			return localExitError(deps, op, sql.ErrNoRows)
		}
		if len(matches) > 1 {
			roles := make([]string, 0, len(matches))
			for _, doc := range matches {
				roles = append(roles, doc.Role)
			}
			return completionValidationError(deps, op, "document path is mapped to multiple roles ("+strings.Join(roles, ", ")+"); re-run with --role <role>")
		}
		doc := matches[0]
		if deps.Printer.Mode() == output.ModeJSON {
			deps.Printer.Success(op, map[string]any{"work_id": view.Work.ID, "context_digest": view.ContextDigest, "artifact_id": artifact.ID, "path": doc.Path, "role": doc.Role, "digest": doc.Digest, "content": string(doc.Content)})
		} else {
			fmt.Fprint(deps.Stdout, string(doc.Content))
		}
		return nil
	}
	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success(op, view)
	} else {
		printLocalContextView(deps, view)
	}
	return nil
}
