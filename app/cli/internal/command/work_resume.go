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
	return &cobra.Command{Use: "resume <ref>", Short: "Read a Local work's scope, pinned document index and next action", Example: "  specgate work resume LOCAL-123 --json", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
		contract, err := store.GetVerificationContract(cmd.Context(), sel.Workspace.ID, args[0])
		if err != nil {
			return localExitError(deps, op, err)
		}
		status, report, err := deriveLocalChangeStatusFromStore(cmd.Context(), store, sel.Workspace.ID, view.Work)
		if err != nil {
			return localExitError(deps, op, err)
		}
		if report != nil {
			status = applyCheckoutFreshness(cmd.Context(), deps, status, mapGitReceipt(report.Body))
		}
		if deps.Printer.Mode() == output.ModeJSON {
			deps.Printer.Success(op, struct {
				localContextView
				Verification local.VerificationContract `json:"verification_contract"`
				Status       changeStatusResult         `json:"status"`
			}{view, contract, status})
			return nil
		}
		printLocalContextView(deps, view)
		printChangeStatus(deps, status)
		return nil
	}}
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
