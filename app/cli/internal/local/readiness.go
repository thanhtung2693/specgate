package local

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

type ReadinessRun struct {
	ID         string                  `json:"id"`
	ArtifactID string                  `json:"artifact_id"`
	Aggregate  string                  `json:"aggregate"`
	Checks     map[string]any          `json:"checks"`
	CreatedAt  string                  `json:"created_at"`
	Dispatch   DispatchGateTasksResult `json:"dispatched_to_ide_agent,omitempty"`
}

func (s *Store) RunReadiness(ctx context.Context, workspaceID, artifactID string) (ReadinessRun, error) {
	artifact, err := s.GetArtifact(ctx, workspaceID, artifactID)
	if err != nil {
		return ReadinessRun{}, err
	}
	dispatch, err := s.dispatchReadinessGateTasks(ctx, workspaceID, artifactID)
	if err != nil {
		return ReadinessRun{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ReadinessRun{}, err
	}
	defer tx.Rollback()
	run, err := recordReadinessRunTx(ctx, tx, workspaceID, artifact)
	if err != nil {
		return ReadinessRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return ReadinessRun{}, err
	}
	run.Dispatch = dispatch
	return run, nil
}

// unresolvedReadinessRemedy names the gates that are actually blocking approval
// and each one's own hint. A single "resolve gate tasks" line sent the author to
// `gates tasks list` even when every task was already submitted and a
// deterministic gate such as required_roles_present was the blocker, which
// reports nothing to do and leaves no next step.
func unresolvedReadinessRemedy(checks map[string]any, artifactID string) string {
	var blocked []string
	awaitingTask := false
	for _, raw := range checks {
		check, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		state, _ := check["state"].(string)
		if state == "pass" || state == "warn" || state == "not_applicable" {
			continue
		}
		gate, _ := check["gate"].(string)
		hint, _ := check["hint"].(string)
		if state == "not_run" {
			awaitingTask = true
		}
		if hint != "" {
			blocked = append(blocked, fmt.Sprintf("%s (%s): %s", gate, state, hint))
		} else {
			blocked = append(blocked, fmt.Sprintf("%s (%s)", gate, state))
		}
	}
	if len(blocked) == 0 {
		return "re-run `specgate gates check " + artifactID + "`"
	}
	sort.Strings(blocked)
	remedy := "blocked by " + strings.Join(blocked, "; ")
	if awaitingTask {
		remedy += "\nSubmit the pending IDE results with `specgate gates tasks list " + artifactID + "`."
	}
	return remedy
}

func (s *Store) ApproveArtifact(ctx context.Context, workspaceID, artifactID, actor, note string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	artifact, err := getArtifactTx(ctx, tx, workspaceID, artifactID)
	if err != nil {
		return err
	}
	var readinessRunID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM artifact_readiness_runs WHERE workspace_id = ? AND artifact_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`, workspaceID, artifactID).Scan(&readinessRunID)
	if err == sql.ErrNoRows {
		return fmt.Errorf("run `specgate gates check %s` before approving this artifact", artifactID)
	}
	if err != nil {
		return err
	}
	checks, err := readinessChecks(ctx, tx, workspaceID, artifact)
	if err != nil {
		return err
	}
	aggregate := aggregateChecks(checks)
	if aggregate != "pass" && aggregate != "warn" {
		return fmt.Errorf("artifact readiness is %s; %s", aggregate, unresolvedReadinessRemedy(checks, artifactID))
	}
	result, err := tx.ExecContext(ctx, `UPDATE artifacts SET status = 'approved' WHERE id = ? AND workspace_id = ?`, artifactID, workspaceID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO artifact_approvals(artifact_id, workspace_id, actor, note, created_at) VALUES (?, ?, ?, ?, ?)`, artifactID, workspaceID, actor, note, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}
