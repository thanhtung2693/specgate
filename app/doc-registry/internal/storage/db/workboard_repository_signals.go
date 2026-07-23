package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/knowledge"
	"github.com/specgate/doc-registry/internal/workboard"
)

func (r *WorkBoardRepository) ListGateRuns(
	ctx context.Context,
	changeRequestID string,
	limit int,
) ([]workboard.GateRun, error) {
	if limit == 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	var rows []workboard.GateRun
	q := scopeWorkBoardQuery(r.db.WithContext(ctx), ctx).
		Where("subject_kind = ? AND subject_id = ?", workboard.GateRunSubjectChangeRequest, changeRequestID).
		Order("created_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// ListLifecycleEvents returns the workboard_lifecycle_events rows for one entity
// (entity_kind, entity_id) ordered by created_at ascending — chronological for
// the governance audit trail.
func (r *WorkBoardRepository) ListLifecycleEvents(
	ctx context.Context,
	entityKind string,
	entityID string,
	limit int,
) ([]workboard.LifecycleEvent, error) {
	if limit == 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	var rows []workboard.LifecycleEvent
	q := r.db.WithContext(ctx).Where("entity_kind = ? AND entity_id = ?", entityKind, entityID)
	if workspaceID := workboard.WorkspaceID(ctx); workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	q = q.Order("created_at ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *WorkBoardRepository) ListAcceptanceCriteria(
	ctx context.Context,
	changeRequestID string,
) ([]workboard.AcceptanceCriterion, error) {
	var rows []workboard.AcceptanceCriterion
	q := r.db.WithContext(ctx).Where("change_request_id = ?", changeRequestID)
	if workspaceID := workboard.WorkspaceID(ctx); workspaceID != "" {
		q = q.Where("change_request_id IN (SELECT id FROM change_requests WHERE workspace_id = ?)", workspaceID)
	}
	err := q.
		Order("sort_order ASC, created_at ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		return rows, nil
	}
	var cr struct{ ID string }
	if err := scopeWorkBoardQuery(r.db.WithContext(ctx), ctx).
		Model(&workboard.ChangeRequest{}).
		Select("id").
		First(&cr, "id = ?", changeRequestID).Error; err != nil {
		return nil, mapWorkBoardNotFound(err)
	}
	return rows, nil
}

func (r *WorkBoardRepository) linkedKnowledgeNewerWarning(
	ctx context.Context,
	workspaceID string,
	feature workboard.Feature,
	canonical artifact.Artifact,
	changeRequestID string,
) (*workboard.StaleWarning, error) {
	// Knowledge freshness is workspace-scoped; with no known workspace there is
	// no safe scope, so do not raise (avoids cross-workspace false positives).
	if strings.TrimSpace(workspaceID) == "" {
		return nil, nil
	}
	canonicalAt := canonical.UpdatedAt
	if canonical.ApprovedAt != nil {
		canonicalAt = *canonical.ApprovedAt
	}
	featureRefs := []string{feature.ID}
	if strings.TrimSpace(feature.Key) != "" && feature.Key != feature.ID {
		featureRefs = append(featureRefs, feature.Key)
	}

	var doc knowledge.Document
	err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND is_latest = ? AND status = ? AND linked_feature_id IN ?", workspaceID, true, knowledge.StatusIndexed, featureRefs).
		Order("updated_at DESC").
		First(&doc).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !doc.UpdatedAt.After(canonicalAt) {
		return nil, nil
	}
	message := fmt.Sprintf("Linked knowledge %q (%s) is newer than the feature source of truth.", doc.Title, doc.Version)
	warning := staleWarning(workboard.WarningLinkedKnowledgeNewer, feature.ID, changeRequestID, canonical.ID, message)
	return &warning, nil
}

// trackerStatusConflictWarning emits tracker_status_conflict when the inbound
// tracker status contradicts the git merge evidence for the change request.
// Conservative by design: a missing tracker event (or any non-contradicting
// combination) yields no warning.
func (r *WorkBoardRepository) trackerStatusConflictWarning(
	ctx context.Context,
	feature workboard.Feature,
	cr workboard.ChangeRequest,
) (*workboard.StaleWarning, error) {
	provider, tracker, err := r.latestTrackerFeedback(ctx, cr)
	if err != nil {
		return nil, err
	}
	if tracker == "" {
		return nil, nil
	}
	merged, err := r.hasMergedDelivery(ctx, cr.ID)
	if err != nil {
		return nil, err
	}
	trackerLabel := trackerProviderLabel(provider)
	switch {
	case tracker == trackerStateCompleted && !merged:
		msg := trackerLabel + " marked this done, but no merge was detected."
		w := staleWarning(workboard.WarningTrackerStatusConflict, feature.ID, cr.ID, "", msg)
		return &w, nil
	case merged && tracker != trackerStateCompleted && tracker != trackerStateCanceled:
		msg := "A merge was detected, but " + trackerLabel + " hasn't marked this done."
		w := staleWarning(workboard.WarningTrackerStatusConflict, feature.ID, cr.ID, "", msg)
		return &w, nil
	}
	return nil, nil
}

// trackerProviderLabel returns a human-readable label for the provider to use
// in warning messages, e.g. "GitHub" or "Linear". Falls back to "Tracker" when
// the provider string is empty or unrecognised.
func trackerProviderLabel(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "github":
		return "GitHub"
	case "gitlab":
		return "GitLab"
	case "linear":
		return "Linear"
	default:
		return "Tracker"
	}
}

// trackerPriorityUrgentWarning emits tracker_priority_urgent when the latest
// tracker feedback event for the CR carries a Linear priority of 1 (urgent) or
// 2 (high). Priority 0 (no priority set) never fires this warning; priorities 3–4
// (normal, low) never fire it. Tracker feedback rows carry no change_request_id;
// the link is the payload `correlation_id` matched against the CR id or key
// (same convention as latestTrackerStatus). per spec §14.
func (r *WorkBoardRepository) trackerPriorityUrgentWarning(
	ctx context.Context,
	feature workboard.Feature,
	cr workboard.ChangeRequest,
) (*workboard.StaleWarning, error) {
	var rows []integrations.GovernanceFeedbackEvent
	query := r.db.WithContext(ctx).Where("event_type = ?", integrations.FeedbackEventTrackerStatusChanged)
	if cr.WorkspaceID != "" {
		query = query.Where("workspace_id = ?", cr.WorkspaceID)
	}
	if err := query.
		Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		var payload struct {
			CorrelationID string `json:"correlation_id"`
			Priority      int    `json:"priority"`
		}
		if err := json.Unmarshal([]byte(row.PayloadJSON), &payload); err != nil {
			continue
		}
		corr := strings.TrimSpace(payload.CorrelationID)
		if corr == "" {
			continue
		}
		if !strings.EqualFold(corr, cr.ID) && !strings.EqualFold(corr, cr.Key) {
			continue
		}
		// Priority 1 = urgent, 2 = high. 0 means no priority was set — skip.
		if payload.Priority == 1 || payload.Priority == 2 {
			w := workboard.StaleWarning{
				Code:            workboard.WarningTrackerPriorityUrgent,
				Severity:        "warn",
				Message:         "Linked tracker issue is marked urgent/high priority but this work item has not been handed off yet.",
				FeatureID:       feature.ID,
				ChangeRequestID: cr.ID,
			}
			return &w, nil
		}
		return nil, nil
	}
	return nil, nil
}

// deliveryStaleWarning emits delivery_stale when the authoritative
// delivery_review gate run for the change request has state=fail and was created
// more than r.deliverySLADays days ago. Human decisions outrank later platform
// runs. Returns nil when no delivery review history exists, when the
// authoritative review passed, or when the failure is within the threshold.
// per spec §delivery-sla.
func (r *WorkBoardRepository) deliveryStaleWarning(
	ctx context.Context,
	feature workboard.Feature,
	cr workboard.ChangeRequest,
) (*workboard.StaleWarning, error) {
	latest, err := r.AuthoritativeDeliveryReviewRun(ctx, cr.ID)
	if err != nil {
		return nil, err
	}
	if latest == nil {
		return nil, nil // no delivery review yet — no warning
	}
	if latest.State != workboard.NextActionStateFail {
		return nil, nil // authoritative review passed or is pending — no warning
	}
	days := int(time.Since(latest.CreatedAt).Hours() / 24)
	if days < r.deliverySLADays {
		return nil, nil
	}
	msg := fmt.Sprintf("Delivery review has been failing for %d day(s). Last failed on %s.", days, latest.CreatedAt.UTC().Format("2006-01-02"))
	w := staleWarning(workboard.WarningDeliveryStale, feature.ID, cr.ID, "", msg)
	return &w, nil
}

// Linear workflow state.type values that matter to the conflict check. These
// are structural tracker enum keys, not user-supplied content.
const (
	trackerStateCompleted = "completed"
	trackerStateCanceled  = "canceled"
)

func validateCanonicalArtifact(tx *gorm.DB, artifactID string, workspaceID string) error {
	var a artifact.Artifact
	q := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ?", artifactID)
	if workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	if err := q.First(&a).Error; err != nil {
		return mapWorkBoardNotFound(err)
	}
	if a.Status != artifact.StatusApproved {
		return fmt.Errorf("%w: canonical artifact must be approved", workboard.ErrValidation)
	}
	return nil
}

func staleWarning(code workboard.WarningCode, featureID, crID, artifactID, message string) workboard.StaleWarning {
	return workboard.StaleWarning{
		Code:            code,
		Severity:        "warning",
		Message:         message,
		FeatureID:       featureID,
		ChangeRequestID: crID,
		ArtifactID:      artifactID,
	}
}

func replaceAcceptanceCriteria(
	tx *gorm.DB,
	changeRequestID string,
	raw string,
	source workboard.AcceptanceCriterionSource,
	now time.Time,
) error {
	rows := parseAcceptanceCriteriaRows(changeRequestID, raw, source, now)
	if err := tx.Where("change_request_id = ?", changeRequestID).Delete(&workboard.AcceptanceCriterion{}).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Create(&rows).Error
}

func parseAcceptanceCriteriaRows(
	changeRequestID string,
	raw string,
	defaultSource workboard.AcceptanceCriterionSource,
	now time.Time,
) []workboard.AcceptanceCriterion {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var items []any
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}
	rows := make([]workboard.AcceptanceCriterion, 0, len(items))
	for i, item := range items {
		row := workboard.AcceptanceCriterion{
			ID:              uuid.NewString(),
			ChangeRequestID: changeRequestID,
			Source:          defaultSource,
			SortOrder:       i,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		switch v := item.(type) {
		case string:
			row.Text = strings.TrimSpace(v)
		case map[string]any:
			if id, ok := v["id"].(string); ok && strings.TrimSpace(id) != "" {
				row.ID = strings.TrimSpace(id)
			}
			if text, ok := v["text"].(string); ok {
				row.Text = strings.TrimSpace(text)
			}
			if done, ok := v["done"].(bool); ok {
				row.Done = done
			}
			if binding, ok := v["verification_binding"].(string); ok {
				row.VerificationBinding = strings.TrimSpace(binding)
			}
			if src, ok := v["source"].(string); ok {
				switch workboard.AcceptanceCriterionSource(src) {
				case workboard.AcceptanceCriterionSourceHuman, workboard.AcceptanceCriterionSourceLLM:
					row.Source = workboard.AcceptanceCriterionSource(src)
				}
			}
		}
		if row.Text != "" {
			rows = append(rows, row)
		}
	}
	return rows
}

func insertWorkBoardEvent(tx *gorm.DB, eventType, artifactID string, payload map[string]any, now time.Time) error {
	if artifactID == "" {
		artifactID = valueOrDefault(fmt.Sprint(payload["feature_id"]), "workboard")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	// Route through the tamper-evidence chain (spec §8.2) — every
	// artifact_events row must link to its artifact's chain, or verification
	// flags the unchained row as tampering.
	e := artifact.Event{
		ID:         uuid.NewString(),
		ArtifactID: artifactID,
		EventType:  eventType,
		Payload:    string(body),
		CreatedAt:  now,
	}
	return createChainedEvent(tx, &e)
}

func insertWorkBoardLifecycleEvent(
	tx *gorm.DB,
	workspaceID string,
	entityKind string,
	entityID string,
	eventType string,
	actor string,
	payload map[string]any,
	now time.Time,
) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return tx.Create(&workboard.LifecycleEvent{
		ID:          uuid.NewString(),
		WorkspaceID: strings.TrimSpace(workspaceID),
		EntityKind:  entityKind,
		EntityID:    entityID,
		EventType:   eventType,
		Actor:       actor,
		PayloadJSON: string(body),
		CreatedAt:   now,
	}).Error
}

func stateIf(ok bool, yes, no workboard.NextActionState) workboard.NextActionState {
	if ok {
		return yes
	}
	return no
}

func hintForArtifactStatus(lead *artifact.Artifact, fallback string) string {
	if lead == nil {
		return fallback
	}
	return string(lead.Status)
}

func hintForConflictGate(lead *artifact.Artifact) string {
	if lead != nil {
		return "No blocking conflicts on impacted services"
	}
	return "No working spec attached"
}

func hintForKnowledgeGate(hasWarning bool) string {
	if hasWarning {
		return "Linked knowledge is newer than the canonical spec"
	}
	return "No newer linked knowledge"
}

func hintForCanonicalGate(isCanonical, drifted bool, canonicalArtifactID string) string {
	if isCanonical {
		return "This spec is the feature canonical document"
	}
	if drifted {
		return "Feature canonical is a different artifact (" + canonicalArtifactID + "). Promote to take over."
	}
	return "Working spec is not yet promoted"
}

func normalizeFeature(feature *workboard.Feature, now time.Time) {
	feature.ID = idOrNew(feature.ID)
	if feature.Status == "" {
		feature.Status = workboard.FeatureStatusCandidate
	}
	if feature.Version == 0 {
		feature.Version = 1
	}
	if feature.SourceDocumentIDs == "" {
		feature.SourceDocumentIDs = "[]"
	}
	if feature.SourceArtifactIDs == "" {
		feature.SourceArtifactIDs = "[]"
	}
	if feature.CreatedAt.IsZero() {
		feature.CreatedAt = now
	}
	if feature.UpdatedAt.IsZero() {
		feature.UpdatedAt = now
	}
}

func normalizeChangeRequest(cr *workboard.ChangeRequest, now time.Time) {
	cr.ID = idOrNew(cr.ID)
	if cr.Key == "" {
		cr.Key = "CR-" + shortID()
	}
	if cr.WorkType == "" {
		cr.WorkType = workboard.WorkTypeNewFeature
	}
	if cr.AcceptanceCriteria == "" {
		cr.AcceptanceCriteria = "[]"
	}
	if cr.NonGoals == "" {
		cr.NonGoals = "[]"
	}
	if cr.OpenQuestions == "" {
		cr.OpenQuestions = "[]"
	}
	if cr.SourceRefs == "" {
		cr.SourceRefs = "[]"
	}
	if cr.CreatedAt.IsZero() {
		cr.CreatedAt = now
	}
	if cr.UpdatedAt.IsZero() {
		cr.UpdatedAt = now
	}
}

func idOrNew(id string) string {
	if id != "" {
		return id
	}
	return uuid.NewString()
}

func shortID() string {
	return strings.ToUpper(strings.ReplaceAll(uuid.NewString()[:8], "-", ""))
}

func valueOrDefault(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func mapWorkBoardNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return workboard.ErrNotFound
	}
	return err
}
