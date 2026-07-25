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

// Statistics separates all-time workspace totals from windowed governance
// signals. The signal field names match the Full-mode stats contract so the
// same readout means the same thing in both modes.
type Statistics struct {
	WorkItems       int `json:"work_items"`
	Delivered       int `json:"delivered"`
	DeliveryReviews int `json:"delivery_reviews"`

	WindowDays             int     `json:"window_days"`
	ReviewedItems          int     `json:"reviewed_items"`
	FirstPass              int     `json:"first_pass"`
	GateCatchesPreBuild    int     `json:"gate_catches_pre_build"`
	ReviewCatchesPostBuild int     `json:"review_catches_post_build"`
	ReviewCatchesFixed     int     `json:"review_catches_fixed"`
	Rework                 int     `json:"rework"`
	ItemsWithRework        int     `json:"items_with_rework"`
	CycleTimeAvgHours      float64 `json:"cycle_time_avg_hours"`
	CycleTimeItems         int     `json:"cycle_time_items"`
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

func (s *Store) Stats(ctx context.Context, workspaceID string, windowDays int) (Statistics, error) {
	if windowDays < 1 {
		windowDays = 1
	}
	if windowDays > 365 {
		windowDays = 365
	}
	stats := Statistics{WindowDays: windowDays}
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

	since := time.Now().UTC().AddDate(0, 0, -windowDays).Format(time.RFC3339Nano)
	if err := s.db.QueryRowContext(
		ctx,
		// Distinct (artifact, gate) pairs, matching the server: gate results are
		// point-in-time snapshots, so re-running a gate against the same defect
		// must not inflate the readout. `warn` is not a catch.
		`SELECT COUNT(*) FROM (
		   SELECT DISTINCT artifact_id, gate_key FROM local_gate_tasks
		    WHERE workspace_id = ? AND submitted_at >= ?
		      AND result_state IN ('fail', 'needs_human_review')
		 )`,
		workspaceID, since,
	).Scan(&stats.GateCatchesPreBuild); err != nil {
		return Statistics{}, err
	}

	// Read each item's complete review history, not just the in-window slice.
	// The window decides which items count as active; the item's own first
	// review decides first-pass yield. Windowing the reviews themselves would
	// hide an older failure and report a reworked item as a clean first pass.
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT r.work_id, r.verdict, r.created_at, w.created_at
		   FROM delivery_reviews r
		   JOIN work_items w ON w.id = r.work_id
		  WHERE r.workspace_id = ?
		    AND r.work_id IN (
		        SELECT work_id FROM delivery_reviews
		         WHERE workspace_id = ? AND created_at >= ?
		    )
		  ORDER BY r.created_at, r.id`,
		workspaceID, workspaceID, since,
	)
	if err != nil {
		return Statistics{}, err
	}
	defer rows.Close()

	type review struct {
		passed   bool
		at       string
		inWindow bool
	}
	history := map[string][]review{}
	created := map[string]string{}
	order := make([]string, 0)
	for rows.Next() {
		var workID, verdict, reviewedAt, workCreatedAt string
		if err := rows.Scan(&workID, &verdict, &reviewedAt, &workCreatedAt); err != nil {
			return Statistics{}, err
		}
		if _, seen := history[workID]; !seen {
			created[workID] = workCreatedAt
			order = append(order, workID)
		}
		history[workID] = append(history[workID], review{
			passed: verdict == "passed", at: reviewedAt, inWindow: reviewedAt >= since,
		})
	}
	if err := rows.Err(); err != nil {
		return Statistics{}, err
	}

	var cycleHours float64
	for _, workID := range order {
		seq := history[workID]
		stats.ReviewedItems++
		if seq[0].passed {
			stats.FirstPass++
		}

		inWindow := 0
		for _, entry := range seq {
			if entry.inWindow {
				inWindow++
			}
		}
		resubmits := inWindow
		if seq[0].inWindow {
			resubmits--
		}
		if resubmits > 0 {
			stats.Rework += resubmits
			stats.ItemsWithRework++
		}

		for index, entry := range seq {
			if entry.passed || !entry.inWindow {
				continue
			}
			stats.ReviewCatchesPostBuild++
			for _, later := range seq[index+1:] {
				if later.passed {
					stats.ReviewCatchesFixed++
					break
				}
			}
		}

		// Cycle time closes only while the item stands passed; a later failure
		// reopens it.
		if !seq[len(seq)-1].passed {
			continue
		}
		for _, entry := range seq {
			if !entry.passed {
				continue
			}
			if hours, ok := hoursBetween(created[workID], entry.at); ok {
				cycleHours += hours
				stats.CycleTimeItems++
			}
			break
		}
	}
	if stats.CycleTimeItems > 0 {
		stats.CycleTimeAvgHours = cycleHours / float64(stats.CycleTimeItems)
	}
	return stats, nil
}

// hoursBetween reports the elapsed hours between two stored timestamps. An
// unparseable or absent bound yields no measurement rather than a zero, so a
// missing timestamp never reads as an instant delivery.
func hoursBetween(from, to string) (float64, bool) {
	if from == "" || to == "" {
		return 0, false
	}
	start, err := time.Parse(time.RFC3339Nano, from)
	if err != nil {
		return 0, false
	}
	end, err := time.Parse(time.RFC3339Nano, to)
	if err != nil {
		return 0, false
	}
	elapsed := end.Sub(start).Hours()
	if elapsed < 0 {
		return 0, false
	}
	return elapsed, true
}

func (s *Store) recordAudit(ctx context.Context, workspaceID, workID, action, detail string) error {
	id, err := newID()
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO audit_events(id, workspace_id, work_id, action, detail, created_at) VALUES (?, ?, ?, ?, ?, ?)`, id, workspaceID, workID, action, detail, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
