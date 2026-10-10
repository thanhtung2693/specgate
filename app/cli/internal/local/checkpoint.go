package local

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Checkpoint is an append-only Local pickup baseline. CheckoutFingerprint is
// opaque to the store so the CLI can evolve its Git observation without making
// a checkpoint a Git operation or a source of scope changes.
type Checkpoint struct {
	Snapshot            *CheckoutSnapshot `json:"snapshot,omitempty"`
	ID                  string            `json:"id"`
	WorkspaceID         string            `json:"workspace_id"`
	WorkID              string            `json:"work_id"`
	ContextDigest       string            `json:"context_digest"`
	CheckoutFingerprint string            `json:"checkout_fingerprint,omitempty"`
	ChangedFiles        []string          `json:"changed_files,omitempty"`
	VerificationDigest  string            `json:"verification_digest,omitempty"`
	CompletionID        string            `json:"completion_id,omitempty"`
	ReviewID            string            `json:"review_id,omitempty"`
	Note                string            `json:"note,omitempty"`
	CreatedAt           string            `json:"created_at"`
}

type CheckoutSnapshot struct {
	Version     int               `json:"version"`
	State       string            `json:"state"`
	Reason      string            `json:"reason,omitempty"`
	CheckoutID  string            `json:"checkout_id"`
	Head        string            `json:"head"`
	Tree        string            `json:"tree"`
	Fingerprint string            `json:"fingerprint"`
	Files       map[string]string `json:"dirty_manifest,omitempty"`
}

// CreateCheckpointWithFiles records a caller-observed, bounded checkout path
// list. The store never scans the checkout itself, so creating a checkpoint
// cannot broaden its filesystem access or mutate Git state.
func (s *Store) CreateCheckpointWithFiles(ctx context.Context, workspaceID, ref, fingerprint string, changedFiles []string, note string, snapshots ...CheckoutSnapshot) (Checkpoint, error) {
	work, err := s.GetWork(ctx, workspaceID, ref)
	if err != nil {
		return Checkpoint{}, err
	}
	id, err := newID()
	if err != nil {
		return Checkpoint{}, err
	}
	checkpoint := Checkpoint{ID: id, WorkspaceID: workspaceID, WorkID: work.ID, ContextDigest: work.ContextDigest, CheckoutFingerprint: strings.TrimSpace(fingerprint), ChangedFiles: normalizedCheckpointPaths(changedFiles), Note: strings.TrimSpace(note)}
	if len(snapshots) > 0 {
		checkpoint.Snapshot = &snapshots[0]
	}
	err = s.WithEnhancedWrite(ctx, func(tx *sql.Tx) error {
		current, err := getWork(ctx, tx, workspaceID, ref)
		if err != nil {
			return err
		}
		contract, err := getVerificationContract(ctx, tx, current)
		if err != nil {
			return err
		}
		checkpoint.ContextDigest, checkpoint.VerificationDigest = current.ContextDigest, contract.Digest
		var reviewID, completionID string
		err = tx.QueryRowContext(ctx, `SELECT id, report_id FROM delivery_reviews WHERE workspace_id=? AND work_id=? ORDER BY created_at DESC,id DESC LIMIT 1`, workspaceID, current.ID).Scan(&reviewID, &completionID)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		checkpoint.ReviewID, checkpoint.CompletionID = reviewID, completionID
		checkpoint.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		body, err := json.Marshal(checkpoint)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO work_checkpoints(id, workspace_id, work_id, body, created_at) VALUES (?, ?, ?, ?, ?)`, checkpoint.ID, workspaceID, work.ID, body, checkpoint.CreatedAt)
		return err
	})
	return checkpoint, err
}

func normalizedCheckpointPaths(paths []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path != "" && !seen[path] {
			seen[path] = true
			result = append(result, path)
		}
	}
	return result
}

func (s *Store) GetCheckpoint(ctx context.Context, workspaceID, ref, checkpointID string) (Checkpoint, error) {
	work, err := s.GetWork(ctx, workspaceID, ref)
	if err != nil {
		return Checkpoint{}, err
	}
	var body string
	err = s.db.QueryRowContext(ctx, `SELECT body FROM work_checkpoints WHERE id = ? AND workspace_id = ? AND work_id = ?`, checkpointID, workspaceID, work.ID).Scan(&body)
	if err != nil {
		return Checkpoint{}, err
	}
	var checkpoint Checkpoint
	if err := json.Unmarshal([]byte(body), &checkpoint); err != nil {
		return Checkpoint{}, fmt.Errorf("corrupt checkpoint: %w", err)
	}
	return checkpoint, validateCheckpointVersion(checkpoint)
}

// LatestCheckpointForCheckout selects only a baseline captured in this exact
// checkout. A checkpoint from another worktree is not a conservative baseline.
func (s *Store) LatestCheckpointForCheckout(ctx context.Context, workspaceID, ref, checkoutID string) (Checkpoint, error) {
	if strings.TrimSpace(checkoutID) == "" {
		return Checkpoint{}, sql.ErrNoRows
	}
	work, err := s.GetWork(ctx, workspaceID, ref)
	if err != nil {
		return Checkpoint{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT body FROM work_checkpoints WHERE workspace_id=? AND work_id=? ORDER BY created_at DESC,id DESC`, workspaceID, work.ID)
	if err != nil {
		return Checkpoint{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return Checkpoint{}, err
		}
		var checkpoint Checkpoint
		if err := json.Unmarshal([]byte(body), &checkpoint); err != nil {
			return Checkpoint{}, fmt.Errorf("corrupt checkpoint: %w", err)
		}
		if err := validateCheckpointVersion(checkpoint); err != nil {
			return Checkpoint{}, err
		}
		if checkpoint.Snapshot != nil && checkpoint.Snapshot.State == "available" && checkpoint.Snapshot.CheckoutID == checkoutID {
			return checkpoint, nil
		}
	}
	if err := rows.Err(); err != nil {
		return Checkpoint{}, err
	}
	return Checkpoint{}, sql.ErrNoRows
}

func validateCheckpointVersion(checkpoint Checkpoint) error {
	if checkpoint.Snapshot != nil && checkpoint.Snapshot.Version != 1 {
		return fmt.Errorf("%w: checkout snapshot version %d", ErrStoreIncompatible, checkpoint.Snapshot.Version)
	}
	return nil
}
