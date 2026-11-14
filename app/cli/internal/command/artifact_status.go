package command

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/output"
)

func newArtifactApproveCmd(deps *Deps) *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:   "approve <artifact-id>",
		Short: "Approve an artifact version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runArtifactStatusChange(cmd, deps, "artifact.approve", args[0], note,
				fmt.Sprintf("Approve artifact %s?", args[0]))
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "Optional reviewer note recorded with the decision")
	return cmd
}

// specgate artifact promote <artifact-id>
//
// Promotes an approved artifact to its feature's canonical spec — the
// deliberate approve->promote->handoff step. Without it, an approved
// feature-backed artifact never becomes the feature canonical, so the Context
// Pack handoff renders no spec content. Promotion is never automatic on approval.
func newArtifactPromoteCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "promote <artifact-id>",
		Short: "Promote an approved artifact to its feature's canonical spec",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if !deps.Yes {
				payload := output.ErrorPayload{Code: "confirmation_required", Message: fmt.Sprintf("Promote artifact %s to its feature's canonical? Re-run with --yes to record this human decision.", id)}
				code := deps.Printer.Error("artifact.promote", payload)
				return &output.ExitError{Code: code}
			}
			store, err := openLocalStore(deps)
			if err != nil {
				return localExitError(deps, "artifact.promote", err)
			}
			defer store.Close()
			selection, err := localSelection(cmd.Context(), deps, store)
			if err != nil {
				return localExitError(deps, "artifact.promote", err)
			}
			feature, err := store.PromoteArtifact(cmd.Context(), selection.Workspace.ID, id)
			if err != nil {
				return localExitError(deps, "artifact.promote", err)
			}
			result := map[string]any{"id": feature.ID, "key": feature.Key, "canonical_artifact_id": feature.CanonicalArtifactID, "version": feature.Version}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("artifact.promote", result)
				return nil
			}
			// ponytail: machine callers read the envelope above, so this line is for a
			// human — it says what promotion means instead of naming the internal state.
			fmt.Fprintf(deps.Stdout, "%s %s — now the current version of %s\n", styled(deps, output.StyleSuccess, "Promoted"), styled(deps, output.StyleBold, id), feature.Key)
			return nil
		},
	}
}

func runArtifactStatusChange(cmd *cobra.Command, deps *Deps, op, id, note, confirmPrompt string) error {
	if !deps.Yes {
		payload := output.ErrorPayload{Code: "confirmation_required", Message: fmt.Sprintf("%s Re-run with --yes to record this human decision.", confirmPrompt)}
		code := deps.Printer.Error(op, payload)
		return &output.ExitError{Code: code}
	}
	store, err := openLocalStore(deps)
	if err != nil {
		return localExitError(deps, op, err)
	}
	defer store.Close()
	selection, err := localSelection(cmd.Context(), deps, store)
	if err != nil {
		return localExitError(deps, op, err)
	}
	if err := store.ApproveArtifact(cmd.Context(), selection.Workspace.ID, id, selection.User.Username, note); err != nil {
		return localExitError(deps, op, err)
	}
	artifact, err := store.GetArtifact(cmd.Context(), selection.Workspace.ID, id)
	if err != nil {
		return localExitError(deps, op, err)
	}
	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success(op, localArtifactView(artifact))
		return nil
	}
	fmt.Fprintf(deps.Stdout, "%s %s (v%d)\n", styled(deps, output.StyleSuccess, "Approved"), styled(deps, output.StyleBold, artifact.ID), artifact.Version)
	return nil
}
