package local

import (
	"context"
	"time"
)

type AuditEvent struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Detail    string `json:"detail"`
	CreatedAt string `json:"created_at"`
}

type Statistics struct {
	WorkItems       int `json:"work_items"`
	Delivered       int `json:"delivered"`
	DeliveryReviews int `json:"delivery_reviews"`
}

func (s *Store) Audit(ctx context.Context, workspaceID, ref string) ([]AuditEvent, error) {
	work, err := s.GetWork(ctx, workspaceID, ref)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, action, detail, created_at FROM (
		SELECT artifact_id AS id, 'artifact.approved' AS action, 'approved by ' || actor || CASE WHEN note = '' THEN '' ELSE ': ' || note END AS detail, created_at
		FROM artifact_approvals
		WHERE workspace_id = ? AND artifact_id = ?
		UNION ALL
		SELECT id, action, detail, created_at
		FROM audit_events
		WHERE workspace_id = ? AND work_id = ?
	) ORDER BY created_at, id`, workspaceID, work.ArtifactID, workspaceID, work.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []AuditEvent
	for rows.Next() {
		var event AuditEvent
		if err := rows.Scan(&event.ID, &event.Action, &event.Detail, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) Stats(ctx context.Context, workspaceID string) (Statistics, error) {
	var stats Statistics
	for _, query := range []struct {
		dest  *int
		query string
	}{
		{&stats.WorkItems, `SELECT COUNT(*) FROM work_items WHERE workspace_id = ?`},
		{&stats.Delivered, `SELECT COUNT(*) FROM work_items WHERE workspace_id = ? AND phase = 'delivered'`},
		{&stats.DeliveryReviews, `SELECT COUNT(*) FROM delivery_reviews WHERE workspace_id = ?`},
	} {
		if err := s.db.QueryRowContext(ctx, query.query, workspaceID).Scan(query.dest); err != nil {
			return Statistics{}, err
		}
	}
	return stats, nil
}

func (s *Store) recordAudit(ctx context.Context, workspaceID, workID, action, detail string) error {
	id, err := newID()
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO audit_events(id, workspace_id, work_id, action, detail, created_at) VALUES (?, ?, ?, ?, ?, ?)`, id, workspaceID, workID, action, detail, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
