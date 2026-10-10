package local

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AcceptanceBasis is a compact, deterministic record of the material a human
// reviewed. It is not an automated verdict: failed or incomplete evidence is
// deliberately part of the basis and a human may still record the existing
// accept/reject decision.
type AcceptanceBasis struct {
	Version            int                           `json:"version"`
	Digest             string                        `json:"digest"`
	WorkID             string                        `json:"work_id"`
	ContextDigest      string                        `json:"context_digest"`
	ReviewID           string                        `json:"review_id"`
	CompletionID       string                        `json:"completion_id"`
	CompletionDigest   string                        `json:"completion_digest"`
	VerificationDigest string                        `json:"verification_digest,omitempty"`
	CheckpointID       string                        `json:"checkpoint_id,omitempty"`
	CheckpointDigest   string                        `json:"checkpoint_digest,omitempty"`
	Selections         AcceptanceOptions             `json:"selections"`
	Watched            []WatchedPathDrift            `json:"watched_inputs,omitempty"`
	PeerDigest         string                        `json:"peer_digest"`
	PeerReview         PeerReviewStatus              `json:"peer_review"`
	Freshness          string                        `json:"freshness"`
	ScopeDigest        string                        `json:"scope_digest"`
	ImpactDigest       string                        `json:"impact_digest,omitempty"`
	SelectedImpact     *ArtifactImpact               `json:"selected_impact,omitempty"`
	CheckpointState    string                        `json:"checkpoint_state"`
	CheckpointCompare  *AcceptanceCheckpointCompare  `json:"checkpoint_comparison,omitempty"`
	CriterionEvidence  []AcceptanceCriterionEvidence `json:"criterion_evidence,omitempty"`
	Gaps               []string                      `json:"gaps,omitempty"`
	ObservedAt         string                        `json:"observed_at,omitempty"`
	Actor              string                        `json:"actor,omitempty"`
	Note               string                        `json:"note,omitempty"`
	Decision           string                        `json:"decision,omitempty"`
}

type AcceptanceCriterionEvidence struct {
	ID              string                 `json:"id"`
	Text            string                 `json:"text"`
	Claim           string                 `json:"claim"`
	CheckName       string                 `json:"check_name,omitempty"`
	CheckStatus     string                 `json:"check_status,omitempty"`
	CheckSource     string                 `json:"check_source,omitempty"`
	CheckProvenance string                 `json:"check_provenance,omitempty"`
	ReportDigest    string                 `json:"report_digest,omitempty"`
	RunFreshness    string                 `json:"run_freshness,omitempty"`
	GroundingStatus string                 `json:"grounding_status,omitempty"`
	Selected        []JUnitCaseObservation `json:"selected_cases,omitempty"`
	EvidenceKind    string                 `json:"evidence_kind,omitempty"`
	EvidencePath    string                 `json:"evidence_path,omitempty"`
	Citation        string                 `json:"citation,omitempty"`
	Gaps            []string               `json:"gaps,omitempty"`
}

type AcceptanceOptions struct {
	RepoRoot            string                                                              `json:"-"`
	CheckoutFingerprint string                                                              `json:"checkout_fingerprint"`
	CheckoutID          string                                                              `json:"checkout_id,omitempty"`
	CheckoutRepository  string                                                              `json:"checkout_repository,omitempty"`
	CheckpointID        string                                                              `json:"checkpoint_id,omitempty"`
	ImpactBase          string                                                              `json:"impact_base,omitempty"`
	ImpactTarget        string                                                              `json:"impact_target,omitempty"`
	CheckpointCompare   *AcceptanceCheckpointCompare                                        `json:"-"`
	ObserveCheckout     func(priorBase string) (checkoutID, repository, fingerprint string) `json:"-"`
}

type AcceptanceCheckpointCompare struct {
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

func (s *Store) AcceptanceBasis(ctx context.Context, workspaceID, ref string, options ...AcceptanceOptions) (AcceptanceBasis, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AcceptanceBasis{}, err
	}
	defer tx.Rollback()
	work, err := getWork(ctx, tx, workspaceID, ref)
	if err != nil {
		return AcceptanceBasis{}, err
	}
	review, err := deliveryStatus(ctx, tx, workspaceID, work)
	if err != nil {
		return AcceptanceBasis{}, err
	}
	return acceptanceBasis(ctx, tx, workspaceID, work, review, options...)
}

func acceptanceBasis(ctx context.Context, q artifactQueryer, workspaceID string, work WorkItem, review DeliveryReview, options ...AcceptanceOptions) (AcceptanceBasis, error) {
	var opts AcceptanceOptions
	if len(options) > 0 {
		opts = options[0]
	}
	if (opts.ImpactBase == "") != (opts.ImpactTarget == "") {
		return AcceptanceBasis{}, fmt.Errorf("%w: impact base and target must be supplied together", ErrVerificationInvalid)
	}
	contract, err := getVerificationContract(ctx, q, work)
	if err != nil {
		return AcceptanceBasis{}, err
	}
	var body string
	if err := q.QueryRowContext(ctx, `SELECT body FROM delivery_reports WHERE id = ? AND workspace_id = ? AND work_id = ?`, review.ReportID, workspaceID, work.ID).Scan(&body); err != nil {
		return AcceptanceBasis{}, err
	}
	basis := AcceptanceBasis{Version: 2, WorkID: work.ID, ContextDigest: work.ContextDigest, ReviewID: review.ID, CompletionID: review.ReportID, CompletionDigest: digestText(body), VerificationDigest: contract.Digest, Selections: opts, CheckpointState: "not_requested"}
	var completion map[string]any
	if err := json.Unmarshal([]byte(body), &completion); err != nil {
		basis.Gaps = append(basis.Gaps, "completion report unreadable")
	} else {
		basis.CriterionEvidence, basis.Gaps = ProjectAcceptanceEvidence(work.AcceptanceCriteria, completion, contract)
	}
	if err := validateObservedRunVersions(completion); err != nil {
		return AcceptanceBasis{}, err
	}
	if opts.ObserveCheckout != nil {
		receipt, _ := completion["git_receipt"].(map[string]any)
		priorBase, _ := receipt["base_revision"].(string)
		opts.CheckoutID, opts.CheckoutRepository, opts.CheckoutFingerprint = opts.ObserveCheckout(priorBase)
		basis.Selections = opts
	}
	basis.Freshness = acceptanceCheckoutFreshness(completion, opts)
	if basis.Freshness != "matching_endpoints" {
		basis.Gaps = append(basis.Gaps, "current checkout freshness: "+basis.Freshness)
	}
	basis.Watched = WatchedPathDriftFor(opts.RepoRoot, contract.WatchedPaths)
	for _, watched := range basis.Watched {
		if watched.State != "unchanged" {
			basis.Gaps = append(basis.Gaps, "watched input "+watched.Path+": "+watched.State)
		}
	}
	var peer string
	err = q.QueryRowContext(ctx, `SELECT body FROM delivery_peer_reviews WHERE workspace_id=? AND work_id=? ORDER BY created_at DESC,id DESC LIMIT 1`, workspaceID, work.ID).Scan(&peer)
	if err != nil && err != sql.ErrNoRows {
		return AcceptanceBasis{}, err
	}
	basis.PeerDigest = digestText(peer)
	basis.PeerReview, err = peerReviewStatus(ctx, q, workspaceID, work)
	if err != nil {
		return AcceptanceBasis{}, err
	}
	if basis.PeerReview.State != "passed" {
		basis.Gaps = append(basis.Gaps, "peer review: "+basis.PeerReview.State)
	}
	if work.ArtifactID != "" {
		var source string
		if err := q.QueryRowContext(ctx, `SELECT snapshot_digest || source_criteria_json || source_lineage_json FROM artifacts WHERE workspace_id=? AND id=?`, workspaceID, work.ArtifactID).Scan(&source); err != nil {
			return AcceptanceBasis{}, err
		}
		basis.ScopeDigest = digestText(source)
	}
	if opts.ImpactBase != "" {
		impact, impactErr := artifactImpactForWorkAcceptance(ctx, q, workspaceID, work, opts.ImpactTarget, opts.ImpactBase)
		err = impactErr
		if err != nil {
			return AcceptanceBasis{}, err
		}
		basis.SelectedImpact = &impact
		encoded, marshalErr := json.Marshal(basis.SelectedImpact)
		if marshalErr != nil {
			return AcceptanceBasis{}, marshalErr
		}
		basis.ImpactDigest = digestText(string(encoded))
	}
	var checkpointBody string
	if opts.CheckpointID != "" {
		err = q.QueryRowContext(ctx, `SELECT body FROM work_checkpoints WHERE workspace_id = ? AND work_id = ? AND id=?`, workspaceID, work.ID, opts.CheckpointID).Scan(&checkpointBody)
		if err == sql.ErrNoRows {
			return AcceptanceBasis{}, fmt.Errorf("%w: checkpoint does not belong to this work", ErrReviewChanged)
		}
		if err != nil {
			return AcceptanceBasis{}, err
		}
		basis.CheckpointState = "selected"
	}
	if checkpointBody != "" {
		var checkpoint Checkpoint
		if err := json.Unmarshal([]byte(checkpointBody), &checkpoint); err != nil {
			return AcceptanceBasis{}, fmt.Errorf("corrupt checkpoint: %w", err)
		}
		if err := validateCheckpointVersion(checkpoint); err != nil {
			return AcceptanceBasis{}, err
		}
		if opts.CheckpointCompare == nil {
			return AcceptanceBasis{}, fmt.Errorf("%w: selected checkpoint comparison is required", ErrVerificationInvalid)
		}
		comparison := *opts.CheckpointCompare
		validState := comparison.State == "changed" || comparison.State == "unchanged"
		if checkpoint.Snapshot == nil || checkpoint.Snapshot.State != "available" || opts.CheckoutID == "" || checkpoint.Snapshot.CheckoutID != opts.CheckoutID {
			return AcceptanceBasis{}, fmt.Errorf("%w: selected checkpoint requires the same comparable checkout", ErrVerificationInvalid)
		}
		if !validState || comparison.BaselineKind != "checkpoint" || comparison.BaselineID != checkpoint.ID {
			return AcceptanceBasis{}, fmt.Errorf("%w: selected checkpoint comparison must name this checkpoint and its observed state", ErrVerificationInvalid)
		}
		basis.CheckpointCompare = &comparison
		basis.CheckpointID = checkpoint.ID
		basis.CheckpointDigest = digestText(checkpointBody)
	}
	material, err := json.Marshal(basis)
	if err != nil {
		return AcceptanceBasis{}, err
	}
	basis.Digest = digestText(string(material))
	return basis, nil
}

// This compares observed endpoints, not continuous checkout immutability.
// Read the stored receipt from the same transaction as the rest of the basis.
func acceptanceCheckoutFreshness(completion map[string]any, opts AcceptanceOptions) string {
	receipt, _ := completion["git_receipt"].(map[string]any)
	availability, _ := receipt["availability"].(string)
	head, _ := receipt["head_revision"].(string)
	digest, _ := receipt["diff_digest"].(string)
	current := strings.SplitN(opts.CheckoutFingerprint, ":", 3)
	if availability != "available" || head == "" || digest == "" || len(current) != 3 || current[0] != "available" || current[1] == "" || current[2] == "" {
		return "unavailable"
	}
	scope, _ := receipt["freshness_scope"].(string)
	if scope == "local_checkout" {
		checkoutID, _ := receipt["checkout_id"].(string)
		if checkoutID == "" || opts.CheckoutID == "" || checkoutID != opts.CheckoutID {
			return "noncomparable"
		}
	} else {
		repository, _ := receipt["repository"].(string)
		if repository == "" || opts.CheckoutRepository == "" {
			return "unavailable"
		}
		if repository != opts.CheckoutRepository {
			return "noncomparable"
		}
	}
	if current[1] != head || current[2] != digest {
		return "stale"
	}
	return "matching_endpoints"
}

func acceptanceBasisRequired(ctx context.Context, q verificationQuerier, workspaceID string, work WorkItem) (bool, error) {
	contract, err := getVerificationContract(ctx, q, work)
	if err != nil {
		return false, err
	}
	if contract.Version >= 2 {
		return true, nil
	}
	var count int
	err = q.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM work_checkpoints WHERE workspace_id = ? AND work_id = ?) +
		(SELECT COUNT(*) FROM artifacts WHERE workspace_id=? AND id=? AND source_lineage_json!='') +
		(SELECT COUNT(*) FROM acceptance_bases WHERE workspace_id=? AND work_id=?)`,
		workspaceID, work.ID, workspaceID, work.ArtifactID, workspaceID, work.ID).Scan(&count)
	return count > 0, err
}

func persistAcceptanceBasis(ctx context.Context, tx *sql.Tx, workspaceID string, work WorkItem, basis AcceptanceBasis) error {
	body, err := json.Marshal(basis)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO acceptance_bases(review_id, workspace_id, work_id, digest, body, created_at) VALUES (?, ?, ?, ?, ?, ?)`, basis.ReviewID, workspaceID, work.ID, basis.Digest, body, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func matchingBasis(got, want string) bool { return strings.TrimSpace(got) != "" && got == want }

func validateAcceptanceBasisVersion(basis AcceptanceBasis) error {
	if basis.Version != 1 && basis.Version != 2 {
		return fmt.Errorf("%w: acceptance basis version %d", ErrStoreIncompatible, basis.Version)
	}
	return nil
}
