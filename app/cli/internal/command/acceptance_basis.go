package command

import (
	"fmt"
	"strings"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/spf13/cobra"
)

func acceptanceSelectionFlags(cmd *cobra.Command) {
	cmd.Flags().String("checkpoint", "", "Exact Local checkpoint to include in acceptance")
	cmd.Flags().String("impact-base", "", "Exact Local impact base artifact (requires --impact-target)")
	cmd.Flags().String("impact-target", "", "Exact Local impact target artifact (requires --impact-base)")
}

func rejectFullAcceptanceSelections(cmd *cobra.Command, deps *Deps, op string) error {
	if deps.Topology == config.ModeLocal {
		return nil
	}
	for _, name := range []string{"checkpoint", "impact-base", "impact-target"} {
		if cmd.Flags().Changed(name) {
			return incompatibleCommand(deps, op, "--"+name+" is Local-only")
		}
	}
	return nil
}

func acceptanceOptions(cmd *cobra.Command, deps *Deps, store *local.Store, workspaceID, ref string) (local.AcceptanceOptions, *checkpointDelta, error) {
	root, _ := config.FindProjectRoot(deps.WorkingDir)
	receipt := collectGitReceipt(cmd.Context(), deps.DeployRunner, deliveryWorkingDir(deps), nil)
	opts := local.AcceptanceOptions{RepoRoot: root, CheckoutID: receipt.CheckoutID, CheckoutRepository: receipt.Repository, CheckoutFingerprint: receipt.Availability + ":" + receipt.HeadRevision + ":" + receipt.DiffDigest}
	// Use the completion selected by the basis transaction, not a separate
	// latest-report read, when preserving its delivered commit range.
	opts.ObserveCheckout = func(priorBase string) (string, string, string) {
		current := collectGitReceiptWithPriorBase(cmd.Context(), deps.DeployRunner, deliveryWorkingDir(deps), nil, priorBase)
		return current.CheckoutID, current.Repository, current.Availability + ":" + current.HeadRevision + ":" + current.DiffDigest
	}
	opts.CheckpointID, _ = cmd.Flags().GetString("checkpoint")
	opts.ImpactBase, _ = cmd.Flags().GetString("impact-base")
	opts.ImpactTarget, _ = cmd.Flags().GetString("impact-target")
	if opts.CheckpointID == "" {
		return opts, nil, nil
	}
	checkpoint, err := store.GetCheckpoint(cmd.Context(), workspaceID, ref, opts.CheckpointID)
	if err != nil {
		return opts, nil, err
	}
	current := captureCheckout(cmd.Context(), deps)
	if current.State == "available" {
		opts.CheckoutID = current.CheckoutID
		opts.CheckoutFingerprint = "available:" + current.Head + ":" + current.Fingerprint
	}
	delta := compareCheckout(cmd.Context(), deps, checkpointSnapshot(checkpoint), current)
	delta.BaselineKind, delta.BaselineID = "checkpoint", checkpoint.ID
	compact := resumeDeltaOutput(delta, false)
	opts.CheckpointCompare = &local.AcceptanceCheckpointCompare{
		State: compact.State, BaselineKind: compact.BaselineKind, BaselineID: compact.BaselineID,
		Added: compact.Added, Removed: compact.Removed, Modified: compact.Modified,
		AddedCount: compact.AddedCount, RemovedCount: compact.RemovedCount, ModifiedCount: compact.ModifiedCount,
		Truncated: compact.Truncated, Limit: compact.Limit,
	}
	detail, _ := cmd.Flags().GetBool("detail")
	selected := resumeDeltaOutput(delta, detail)
	return opts, &selected, nil
}

func augmentAcceptanceStatus(cmd *cobra.Command, deps *Deps, store *local.Store, workspaceID, ref string, result changeStatusResult) (changeStatusResult, error) {
	if result.ReviewID == "" {
		return result, nil
	}
	opts, selectedDelta, err := acceptanceOptions(cmd, deps, store, workspaceID, ref)
	if err != nil {
		return result, err
	}
	inspection, err := store.InspectAcceptance(cmd.Context(), workspaceID, ref, opts)
	if err != nil {
		return result, err
	}
	result = deriveLocalChangeStatus(inspection.Work, &inspection.Review, &inspection.Report, inspection.Peer)
	result.Inspection = &inspection
	result.VerificationContract = inspection.Contract.Status
	result.VerificationVersion = inspection.Contract.Version
	result.VerificationDigest = inspection.Contract.Digest
	result.CriterionEvidence, _ = local.ProjectAcceptanceEvidence(inspection.Work.AcceptanceCriteria, inspection.Report.Body, inspection.Contract)
	result.ContextDigest = inspection.Work.ContextDigest
	result.ArtifactID = inspection.Work.ArtifactID
	if inspection.Artifact != nil {
		result.ArtifactVersion, result.ArtifactDigest = inspection.Artifact.Version, inspection.Artifact.SnapshotDigest
		coverage := localArtifactCoverageView(*inspection.Artifact, inspection.WorkItems)
		result.SourceCoverage, _ = coverage["source_coverage"].(string)
		result.SourceRequirements, _ = coverage["source_requirements"].([]sourceRequirementCoverage)
	}
	result = applyCheckoutFreshness(cmd.Context(), deps, result, mapGitReceipt(inspection.Report.Body))
	result.RecordedBasis = inspection.Recorded
	if inspection.Basis == nil {
		return result, nil
	}
	basis := inspection.Basis
	if opts.CheckpointID != "" {
		checkpoint, err := store.GetCheckpoint(cmd.Context(), workspaceID, ref, opts.CheckpointID)
		if err != nil {
			return result, err
		}
		view := checkpointOutput(checkpoint)
		result.SelectedCheckpoint = &view
		result.SelectedCheckpointDelta = selectedDelta
	}
	result.SelectedImpact = basis.SelectedImpact
	result.BasisDigest = basis.Digest
	result.AcceptanceBasis = basis
	for _, watched := range basis.Watched {
		if watched.State != "unchanged" {
			result.Missing = append(result.Missing, fmt.Sprintf("Review watched input %s: %s", watched.Path, watched.State))
		}
	}
	if strings.Contains(result.NextCommand, "--review-id ") {
		result.NextCommand += " --basis-digest " + basis.Digest
		if opts.CheckpointID != "" {
			result.NextCommand += " --checkpoint " + shellQuote(opts.CheckpointID)
		}
		if opts.ImpactBase != "" {
			result.NextCommand += " --impact-base " + shellQuote(opts.ImpactBase) + " --impact-target " + shellQuote(opts.ImpactTarget)
		}
	}
	return result, nil
}
