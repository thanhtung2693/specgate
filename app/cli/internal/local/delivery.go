package local

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

var ErrDeliveryApproved = errors.New("delivery is already approved")

type DeliveryReview struct {
	ID            string `json:"id"`
	WorkID        string `json:"work_id"`
	ReportID      string `json:"report_id"`
	Verdict       string `json:"verdict"`
	Summary       string `json:"summary"`
	HumanDecision string `json:"human_decision,omitempty"`
	Note          string `json:"note,omitempty"`
	CreatedAt     string `json:"created_at"`
}

type PeerReview struct {
	ID        string `json:"id"`
	WorkID    string `json:"work_id"`
	AgentName string `json:"agent_name"`
	CreatedAt string `json:"created_at"`
}

// PeerReviewStatus is deliberately informational. Human delivery approval
// remains the authorization boundary regardless of this state.
type PeerReviewStatus struct {
	State      string `json:"state"`
	AgentName  string `json:"agent_name,omitempty"`
	ReviewedAt string `json:"reviewed_at,omitempty"`
}

type DeliveryReport struct {
	ID   string         `json:"id"`
	Body map[string]any `json:"body"`
}

func (s *Store) SubmitDelivery(ctx context.Context, workspaceID, ref string, body map[string]any) (DeliveryReview, error) {
	work, err := s.GetWork(ctx, workspaceID, ref)
	if err != nil {
		return DeliveryReview{}, err
	}
	if work.Phase == "delivered" {
		return DeliveryReview{}, fmt.Errorf("%w for %s; create a new work item for further changes", ErrDeliveryApproved, work.Key)
	}
	if feedbackAgentName(body) == "" {
		return DeliveryReview{}, fmt.Errorf("completion agent.name is required")
	}
	if digest, _ := body["context_digest"].(string); digest != work.ContextDigest {
		return DeliveryReview{}, fmt.Errorf("completion context_digest does not match %s; rerun `specgate work context %s --json`", work.Key, work.Key)
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return DeliveryReview{}, err
	}
	reportID, err := newID()
	if err != nil {
		return DeliveryReview{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	verdict, summary := localDeliveryVerdict(body, work.AcceptanceCriteria)
	reviewID, err := newID()
	if err != nil {
		return DeliveryReview{}, err
	}
	auditID, err := newID()
	if err != nil {
		return DeliveryReview{}, err
	}
	review := DeliveryReview{
		ID: reviewID, WorkID: work.ID, ReportID: reportID,
		Verdict: verdict, Summary: summary, CreatedAt: now,
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return DeliveryReview{}, err
	}
	defer tx.Rollback()
	var currentPhase, currentDigest string
	if err := tx.QueryRowContext(
		ctx,
		`SELECT phase, context_digest FROM work_items WHERE id = ? AND workspace_id = ?`,
		work.ID,
		workspaceID,
	).Scan(&currentPhase, &currentDigest); err != nil {
		return DeliveryReview{}, err
	}
	if currentPhase == "delivered" {
		return DeliveryReview{}, fmt.Errorf("%w for %s; create a new work item for further changes", ErrDeliveryApproved, work.Key)
	}
	if currentDigest != work.ContextDigest {
		return DeliveryReview{}, fmt.Errorf("work context changed; rerun `specgate work context %s --json`", work.Key)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO delivery_reports(id, workspace_id, work_id, context_digest, body, created_at) VALUES (?, ?, ?, ?, ?, ?)`, reportID, workspaceID, work.ID, work.ContextDigest, encoded, now); err != nil {
		return DeliveryReview{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO delivery_reviews(id, workspace_id, work_id, report_id, verdict, summary, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, review.ID, workspaceID, review.WorkID, review.ReportID, review.Verdict, review.Summary, review.CreatedAt); err != nil {
		return DeliveryReview{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(id, workspace_id, work_id, action, detail, created_at) VALUES (?, ?, ?, ?, ?, ?)`, auditID, workspaceID, work.ID, "delivery.reviewed", review.Verdict+": "+review.Summary, now); err != nil {
		return DeliveryReview{}, err
	}
	if err := tx.Commit(); err != nil {
		return DeliveryReview{}, err
	}
	return review, nil
}

func (s *Store) DecideDelivery(ctx context.Context, workspaceID, ref, decision, actor, note string) error {
	if decision != "approve" && decision != "reject" {
		return fmt.Errorf("delivery decision must be approve or reject")
	}
	work, err := s.GetWork(ctx, workspaceID, ref)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var review DeliveryReview
	err = tx.QueryRowContext(ctx, `SELECT id, work_id, report_id, verdict, summary, human_decision, note, created_at FROM delivery_reviews WHERE workspace_id = ? AND work_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`, workspaceID, work.ID).Scan(&review.ID, &review.WorkID, &review.ReportID, &review.Verdict, &review.Summary, &review.HumanDecision, &review.Note, &review.CreatedAt)
	if err == sql.ErrNoRows {
		return fmt.Errorf("run `specgate delivery submit %s --file <completion.json>` before a human decision", work.Key)
	}
	if err != nil {
		return err
	}
	if review.HumanDecision != "" {
		return fmt.Errorf("delivery decision is already recorded for %s; submit corrected evidence before another human decision", work.Key)
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "human"
	}
	if decision == "approve" {
		report, reportErr := deliveryReportByID(ctx, tx, workspaceID, work.ID, review.ReportID)
		if reportErr != nil {
			return reportErr
		}
		if reporter := feedbackAgentName(report.Body); reporter != "" &&
			strings.EqualFold(reporter, actor) {
			return fmt.Errorf("completion reporter %q cannot approve its own delivery", actor)
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE delivery_reviews SET human_decision = ?, note = ? WHERE id = ? AND human_decision = ''`, decision, note, review.ID)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return fmt.Errorf("delivery decision is already recorded for %s; submit corrected evidence before another human decision", work.Key)
	}
	if decision == "approve" {
		_, err = tx.ExecContext(ctx, `UPDATE work_items SET phase = 'delivered' WHERE id = ?`, work.ID)
	}
	if err == nil {
		verb := "approved"
		if decision == "reject" {
			verb = "rejected"
		}
		detail := verb + " by " + actor
		if trimmedNote := strings.TrimSpace(note); trimmedNote != "" {
			detail += ": " + trimmedNote
		}
		auditID, idErr := newID()
		if idErr != nil {
			return idErr
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(id, workspace_id, work_id, action, detail, created_at) VALUES (?, ?, ?, ?, ?, ?)`, auditID, workspaceID, work.ID, "delivery."+decision, detail, time.Now().UTC().Format(time.RFC3339Nano))
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeliveryStatus(ctx context.Context, workspaceID, ref string) (DeliveryReview, error) {
	work, err := s.GetWork(ctx, workspaceID, ref)
	if err != nil {
		return DeliveryReview{}, err
	}
	var review DeliveryReview
	query := `SELECT id, work_id, report_id, verdict, summary, human_decision, note, created_at FROM delivery_reviews WHERE workspace_id = ? AND work_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`
	if work.Phase == "delivered" {
		query = `SELECT id, work_id, report_id, verdict, summary, human_decision, note, created_at FROM delivery_reviews WHERE workspace_id = ? AND work_id = ? AND human_decision = 'approve' ORDER BY created_at DESC, id DESC LIMIT 1`
	}
	err = s.db.QueryRowContext(ctx, query, workspaceID, work.ID).Scan(&review.ID, &review.WorkID, &review.ReportID, &review.Verdict, &review.Summary, &review.HumanDecision, &review.Note, &review.CreatedAt)
	return review, err
}

func (s *Store) DeliveryReportForReview(
	ctx context.Context,
	workspaceID string,
	review DeliveryReview,
) (DeliveryReport, error) {
	return deliveryReportByID(ctx, s.db, workspaceID, review.WorkID, review.ReportID)
}

type deliveryReportQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func deliveryReportByID(
	ctx context.Context,
	db deliveryReportQueryer,
	workspaceID string,
	workID string,
	reportID string,
) (DeliveryReport, error) {
	if strings.TrimSpace(reportID) == "" {
		return DeliveryReport{}, fmt.Errorf("delivery review is not bound to a completion report")
	}
	var report DeliveryReport
	var encoded string
	err := db.QueryRowContext(
		ctx,
		`SELECT id, body FROM delivery_reports WHERE id = ? AND workspace_id = ? AND work_id = ?`,
		reportID,
		workspaceID,
		workID,
	).Scan(&report.ID, &encoded)
	if err != nil {
		return DeliveryReport{}, err
	}
	if err := json.Unmarshal([]byte(encoded), &report.Body); err != nil {
		return DeliveryReport{}, fmt.Errorf("bound completion report is unreadable")
	}
	return report, nil
}

func (s *Store) LatestDeliveryReport(ctx context.Context, workspaceID, ref string) (DeliveryReport, error) {
	work, err := s.GetWork(ctx, workspaceID, ref)
	if err != nil {
		return DeliveryReport{}, err
	}
	var report DeliveryReport
	var encoded string
	err = s.db.QueryRowContext(ctx, `SELECT id, body FROM delivery_reports WHERE workspace_id = ? AND work_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`, workspaceID, work.ID).Scan(&report.ID, &encoded)
	if err == sql.ErrNoRows {
		return DeliveryReport{}, fmt.Errorf("submit a completion report before peer review")
	}
	if err != nil {
		return DeliveryReport{}, err
	}
	if err := json.Unmarshal([]byte(encoded), &report.Body); err != nil {
		return DeliveryReport{}, fmt.Errorf("latest completion report is unreadable")
	}
	return report, nil
}

func (s *Store) PeerReviewStatus(ctx context.Context, workspaceID, ref string) (PeerReviewStatus, error) {
	work, err := s.GetWork(ctx, workspaceID, ref)
	if err != nil {
		return PeerReviewStatus{}, err
	}
	var peer PeerReview
	var encoded string
	err = s.db.QueryRowContext(ctx, `SELECT id, work_id, agent_name, body, created_at FROM delivery_peer_reviews WHERE workspace_id = ? AND work_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`, workspaceID, work.ID).Scan(&peer.ID, &peer.WorkID, &peer.AgentName, &encoded, &peer.CreatedAt)
	if err == sql.ErrNoRows {
		return PeerReviewStatus{State: "not_run"}, nil
	}
	if err != nil {
		return PeerReviewStatus{}, err
	}
	status := PeerReviewStatus{State: "passed", AgentName: peer.AgentName, ReviewedAt: peer.CreatedAt}
	var body map[string]any
	if err := json.Unmarshal([]byte(encoded), &body); err != nil {
		return PeerReviewStatus{}, fmt.Errorf("latest peer review is unreadable")
	}
	completion, err := s.LatestDeliveryReport(ctx, workspaceID, ref)
	if err != nil {
		return PeerReviewStatus{}, err
	}
	binding, _ := body["peer_review_of"].(map[string]any)
	if completionID, _ := binding["completion_feedback_event_id"].(string); completionID != completion.ID {
		status.State = "stale"
		return status, nil
	}
	criteria, _ := body["criteria"].([]any)
	for _, raw := range criteria {
		criterion, _ := raw.(map[string]any)
		if strings.TrimSpace(fmt.Sprint(criterion["claim"])) != "satisfied" {
			status.State = "failed"
			break
		}
	}
	return status, nil
}

// PeerReviewDelivery records an independent review of the latest completion.
// The peer must review every canonical Local criterion and bind the checkout
// receipt observed by the original completion.
func (s *Store) PeerReviewDelivery(ctx context.Context, workspaceID, ref string, body map[string]any) (PeerReview, error) {
	work, err := s.GetWork(ctx, workspaceID, ref)
	if err != nil {
		return PeerReview{}, err
	}
	completion, err := s.LatestDeliveryReport(ctx, workspaceID, ref)
	if err != nil {
		return PeerReview{}, err
	}
	completer := feedbackAgentName(completion.Body)
	peer := feedbackAgentName(body)
	if completer == "" {
		return PeerReview{}, fmt.Errorf("latest completion agent.name is required")
	}
	if peer == "" {
		return PeerReview{}, fmt.Errorf("peer review agent.name is required")
	}
	if strings.EqualFold(completer, peer) {
		return PeerReview{}, fmt.Errorf("agent %q cannot peer-review its own completion", peer)
	}
	peerReviewOf, _ := body["peer_review_of"].(map[string]any)
	if completionID, _ := peerReviewOf["completion_feedback_event_id"].(string); completionID != completion.ID {
		return PeerReview{}, fmt.Errorf("peer review must bind the latest completion report")
	}
	completionReceipt, _ := completion.Body["git_receipt"].(map[string]any)
	peerReceipt, _ := peerReviewOf["git_receipt"].(map[string]any)
	if completionReceipt == nil || peerReceipt == nil || !reflect.DeepEqual(completionReceipt, peerReceipt) {
		return PeerReview{}, fmt.Errorf("peer review git_receipt must match the latest completion receipt")
	}
	if err := validateLocalPeerCriteria(body, len(work.AcceptanceCriteria)); err != nil {
		return PeerReview{}, err
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return PeerReview{}, err
	}
	id, err := newID()
	if err != nil {
		return PeerReview{}, err
	}
	peerReview := PeerReview{ID: id, WorkID: work.ID, AgentName: peer, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO delivery_peer_reviews(id, workspace_id, work_id, agent_name, body, created_at) VALUES (?, ?, ?, ?, ?, ?)`, peerReview.ID, workspaceID, peerReview.WorkID, peerReview.AgentName, encoded, peerReview.CreatedAt); err != nil {
		return PeerReview{}, err
	}
	if err := s.recordAudit(ctx, workspaceID, work.ID, "delivery.peer_reviewed", "peer review recorded by "+peer); err != nil {
		return PeerReview{}, err
	}
	return peerReview, nil
}

func feedbackAgentName(body map[string]any) string {
	agent, _ := body["agent"].(map[string]any)
	name, _ := agent["name"].(string)
	return strings.TrimSpace(name)
}

func validateLocalPeerCriteria(body map[string]any, count int) error {
	seen := make(map[string]struct{}, count)
	criteria, _ := body["criteria"].([]any)
	for _, raw := range criteria {
		criterion, _ := raw.(map[string]any)
		id := strings.TrimSpace(fmt.Sprint(criterion["criterion_id"]))
		if !isLocalCriterionID(id, count) {
			return fmt.Errorf("peer review criterion_id %q is not canonical", id)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("peer review criterion_id %q is duplicated", id)
		}
		claim := strings.TrimSpace(fmt.Sprint(criterion["claim"]))
		if claim != "satisfied" && claim != "partial" && claim != "not_done" {
			return fmt.Errorf("peer review claim %q is invalid", criterion["claim"])
		}
		if claim == "satisfied" && !hasDeliveryEvidence(criterion) {
			return fmt.Errorf("peer review criterion %s claims satisfied but has no evidence", id)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != count {
		return fmt.Errorf("peer review must cover every acceptance criterion exactly once")
	}
	return nil
}

func localDeliveryVerdict(body map[string]any, requiredCriteria []string) (string, string) {
	satisfied := make(map[string]bool, len(requiredCriteria))
	criteria, _ := body["criteria"].([]any)
	for _, raw := range criteria {
		criterion, _ := raw.(map[string]any)
		if strings.TrimSpace(fmt.Sprint(criterion["claim"])) != "satisfied" {
			continue
		}
		id := strings.TrimSpace(fmt.Sprint(criterion["criterion_id"]))
		if !isLocalCriterionID(id, len(requiredCriteria)) || satisfied[id] || !hasDeliveryEvidence(criterion) {
			return "failed", "completion needs one evidence-backed satisfied claim for each acceptance criterion"
		}
		satisfied[id] = true
	}
	checks, _ := body["checks"].([]any)
	for _, raw := range checks {
		check, _ := raw.(map[string]any)
		if status := strings.TrimSpace(fmt.Sprint(check["status"])); status != "pass" && status != "skipped" {
			return "failed", "one or more reported checks did not pass"
		}
	}
	if len(satisfied) != len(requiredCriteria) {
		return "failed", "completion needs one evidence-backed satisfied claim for each acceptance criterion"
	}
	return "passed", "delivery evidence and reported checks satisfy the Local review"
}

func isLocalCriterionID(id string, count int) bool {
	for index := range count {
		if id == fmt.Sprintf("local-%d", index+1) {
			return true
		}
	}
	return false
}

func hasDeliveryEvidence(criterion map[string]any) bool {
	evidence, _ := criterion["evidence"].(map[string]any)
	for _, value := range evidence {
		if strings.TrimSpace(fmt.Sprint(value)) != "" {
			return true
		}
	}
	return false
}
