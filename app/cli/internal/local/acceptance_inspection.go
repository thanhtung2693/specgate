package local

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// AcceptanceInspection supplies the displayed status and decision token from
// one SQLite snapshot. Filesystem observations remain endpoint observations.
type AcceptanceInspection struct {
	Work      WorkItem
	Review    DeliveryReview
	Report    DeliveryReport
	Peer      PeerReviewStatus
	Contract  VerificationContract
	Artifact  *Artifact
	WorkItems []WorkItem
	Basis     *AcceptanceBasis
	Recorded  *AcceptanceBasis
}

func (s *Store) InspectAcceptance(ctx context.Context, workspaceID, ref string, opts AcceptanceOptions) (AcceptanceInspection, error) {
	var view AcceptanceInspection
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return view, err
	}
	defer tx.Rollback()
	view.Work, err = getWork(ctx, tx, workspaceID, ref)
	if err != nil {
		return view, err
	}
	view.Review, err = deliveryStatus(ctx, tx, workspaceID, view.Work)
	if err != nil {
		return view, err
	}
	view.Report, err = deliveryReportByID(ctx, tx, workspaceID, view.Work.ID, view.Review.ReportID)
	if err != nil {
		return view, err
	}
	view.Contract, err = getVerificationContract(ctx, tx, view.Work)
	if err != nil {
		return view, err
	}
	view.Peer, err = peerReviewStatus(ctx, tx, workspaceID, view.Work)
	if err != nil {
		return view, err
	}
	if view.Work.ArtifactID != "" {
		artifact, err := getArtifact(ctx, tx, workspaceID, view.Work.ArtifactID)
		if err != nil {
			return view, err
		}
		view.Artifact = &artifact
		view.WorkItems, err = listWork(ctx, tx, workspaceID)
		if err != nil {
			return view, err
		}
	}
	var recorded string
	err = tx.QueryRowContext(ctx, `SELECT body FROM acceptance_bases WHERE workspace_id=? AND work_id=? AND review_id=?`, workspaceID, view.Work.ID, view.Review.ID).Scan(&recorded)
	if err != nil && err != sql.ErrNoRows {
		return view, err
	}
	if err == nil {
		if err := json.Unmarshal([]byte(recorded), &view.Recorded); err != nil {
			return view, err
		}
		if view.Recorded == nil {
			return view, fmt.Errorf("recorded acceptance basis is null")
		}
		if err := validateAcceptanceBasisVersion(*view.Recorded); err != nil {
			return view, err
		}
	}
	required, err := acceptanceBasisRequired(ctx, tx, workspaceID, view.Work)
	if err != nil {
		return view, err
	}
	if required || opts.CheckpointID != "" || opts.ImpactBase != "" || opts.ImpactTarget != "" {
		basis, err := acceptanceBasis(ctx, tx, workspaceID, view.Work, view.Review, opts)
		if err != nil {
			return view, err
		}
		view.Basis = &basis
	}
	return view, nil
}
