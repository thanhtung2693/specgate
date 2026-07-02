package db

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
)

// Governance stats projection queries (per doc-registry spec §6.8). Both
// queries filter by window and optional workspace in SQL so callers never
// load unbounded rows. Archived change requests are included on purpose: a
// passing delivery review can auto-archive its CR, and those items still
// count as reviewed work. Gate runs come from the unified gate_runs table,
// restricted to change_request subjects (artifact readiness rows are not
// workboard stats input).

// ListGateRunsForStats returns all gate runs created at or after since, joined
// with their change request for the workspace filter, the display key, and the
// CR creation time (cycle-time input). Implements governanceops.StatsReader.
func (r *WorkBoardRepository) ListGateRunsForStats(
	ctx context.Context,
	workspaceID string,
	since time.Time,
) ([]workboard.StatsGateRun, error) {
	q := r.db.WithContext(ctx).
		Table("gate_runs AS gr").
		Select("gr.id AS run_id, gr.subject_id AS change_request_id, cr.key AS change_request_key, gr.gate AS gate, gr.state AS state, gr.hint AS hint, gr.created_at AS run_created_at, cr.created_at AS cr_created_at").
		Joins("JOIN change_requests cr ON cr.id = gr.subject_id").
		Where("gr.subject_kind = ?", workboard.GateRunSubjectChangeRequest).
		Where("gr.created_at >= ?", since)
	q = statsWorkspaceFilter(q, workspaceID)

	var rows []workboard.StatsGateRun
	if err := q.Order("gr.created_at ASC").Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListDeliveryRunsForStats returns every delivery_review run for the
// workspace regardless of age, joined with its change request. First-pass
// yield is defined against a work item's first review ever, so this query is
// deliberately unwindowed. Implements governanceops.StatsReader.
func (r *WorkBoardRepository) ListDeliveryRunsForStats(
	ctx context.Context,
	workspaceID string,
) ([]workboard.StatsGateRun, error) {
	q := r.db.WithContext(ctx).
		Table("gate_runs AS gr").
		Select("gr.id AS run_id, gr.subject_id AS change_request_id, cr.key AS change_request_key, gr.gate AS gate, gr.state AS state, gr.hint AS hint, gr.created_at AS run_created_at, cr.created_at AS cr_created_at").
		Joins("JOIN change_requests cr ON cr.id = gr.subject_id").
		Where("gr.subject_kind = ?", workboard.GateRunSubjectChangeRequest).
		Where("gr.gate = ?", "delivery_review")
	q = statsWorkspaceFilter(q, workspaceID)

	var rows []workboard.StatsGateRun
	if err := q.Order("gr.created_at ASC").Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListAmbiguityFeedbackForStats returns coding_agent.blocked_ambiguity
// feedback events created at or after since, joined with their change request.
// Implements governanceops.StatsReader.
func (r *WorkBoardRepository) ListAmbiguityFeedbackForStats(
	ctx context.Context,
	workspaceID string,
	since time.Time,
) ([]workboard.StatsFeedbackEvent, error) {
	q := r.db.WithContext(ctx).
		Table("governance_feedback_events AS fe").
		Select("fe.id AS event_id, fe.change_request_id AS change_request_id, cr.key AS change_request_key, fe.reason AS detail, fe.created_at AS created_at").
		Joins("JOIN change_requests cr ON cr.id = fe.change_request_id").
		Where("fe.event_type = ?", integrations.FeedbackEventCodingAgentBlockedAmbiguity).
		Where("fe.created_at >= ?", since)
	q = statsWorkspaceFilter(q, workspaceID)

	var rows []workboard.StatsFeedbackEvent
	if err := q.Order("fe.created_at ASC").Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func statsWorkspaceFilter(q *gorm.DB, workspaceID string) *gorm.DB {
	if workspaceID == "" {
		return q
	}
	return q.Where("cr.workspace_id = ?", workspaceID)
}
