package db

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
)

// Delivery review resolution: which recorded run is the human-authoritative
// review for the current completion cycle, and how a human decision is stored.
// Gate-run refresh and persistence live in workboard_repository_gates.go.

func deliveryEvaluationCompletionID(evidence string) string {
	var detail struct {
		CompletionFeedbackEventID string `json:"completion_feedback_event_id"`
	}
	_ = json.Unmarshal([]byte(strings.TrimSpace(evidence)), &detail)
	return strings.TrimSpace(detail.CompletionFeedbackEventID)
}

// AuthoritativeDeliveryReviewRun returns the human-authoritative delivery run
// for the latest completion cycle without first truncating mixed gate history.
func (r *WorkBoardRepository) AuthoritativeDeliveryReviewRun(
	ctx context.Context,
	changeRequestID string,
) (*workboard.GateRun, error) {
	completionID, err := r.latestCompletionFeedbackEventID(ctx, changeRequestID)
	if err != nil {
		return nil, err
	}
	if completionID == "" {
		human, err := r.latestDeliveryReviewRunByExecutor(ctx, changeRequestID, true, "")
		if err != nil {
			return nil, err
		}
		platform, err := r.latestDeliveryReviewRunByExecutor(ctx, changeRequestID, false, "")
		if err != nil {
			return nil, err
		}
		if human == nil {
			return platform, nil
		}
		if platform == nil {
			return human, nil
		}
		return authoritativeDeliveryReviewCycle(human, platform), nil
	}
	human, err := r.latestDeliveryReviewRunByExecutor(ctx, changeRequestID, true, completionID)
	if err != nil {
		return nil, err
	}
	if human != nil {
		return human, nil
	}
	platform, err := r.latestDeliveryReviewRunByExecutor(ctx, changeRequestID, false, completionID)
	if err != nil {
		return nil, err
	}
	if platform != nil {
		return platform, nil
	}
	return nil, nil
}

// CurrentGateRuns returns one current row per non-delivery gate plus the
// completion-bound authoritative delivery review. Append-only delivery history
// cannot crowd an unresolved quality gate out of a Context Pack.
func (r *WorkBoardRepository) CurrentGateRuns(
	ctx context.Context,
	changeRequestID string,
) ([]workboard.GateRun, error) {
	ranked := r.db.WithContext(ctx).
		Table("gate_runs AS gr").
		Select(`gr.*,
			ROW_NUMBER() OVER (
				PARTITION BY gr.workspace_id, gr.subject_id, gr.gate
				ORDER BY gr.created_at DESC, gr.id DESC
			) AS gate_rank`).
		Where(
			"gr.subject_kind = ? AND gr.subject_id = ? AND gr.gate <> ?",
			workboard.GateRunSubjectChangeRequest,
			changeRequestID,
			governanceprofile.DeliveryReviewGateKey,
		)
	if workspaceID := workboard.WorkspaceID(ctx); workspaceID != "" {
		ranked = ranked.Where("gr.workspace_id = ?", workspaceID)
	}
	var rows []workboard.GateRun
	if err := r.db.WithContext(ctx).
		Table("(?) AS ranked_gates", ranked).
		Where("gate_rank = 1").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	delivery, err := r.AuthoritativeDeliveryReviewRun(ctx, changeRequestID)
	if err != nil {
		return nil, err
	}
	if delivery != nil {
		rows = append(rows, *delivery)
	}
	return rows, nil
}

func (r *WorkBoardRepository) latestCompletionFeedbackEventID(
	ctx context.Context,
	changeRequestID string,
) (string, error) {
	var row integrations.GovernanceFeedbackEvent
	q := r.db.WithContext(ctx).Where(
		"change_request_id = ? AND event_type = ?",
		changeRequestID,
		integrations.FeedbackEventCodingAgentCompleted,
	)
	if workspaceID := workboard.WorkspaceID(ctx); workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	err := q.Order("created_at DESC, id DESC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return row.ID, nil
}

func authoritativeDeliveryReviewFromRuns(runs []workboard.GateRun) *workboard.GateRun {
	var human, platform *workboard.GateRun
	for i := range runs {
		run := &runs[i]
		if run.Executor == workboard.GateRunExecutorHuman {
			if human == nil || deliveryGateRunNewer(*run, *human) {
				human = run
			}
		} else if platform == nil || deliveryGateRunNewer(*run, *platform) {
			platform = run
		}
	}
	if human == nil {
		return platform
	}
	if platform == nil {
		return human
	}
	return authoritativeDeliveryReviewCycle(human, platform)
}

func authoritativeDeliveryReviewCycle(
	human *workboard.GateRun,
	platform *workboard.GateRun,
) *workboard.GateRun {
	type completionBinding struct {
		CompletionFeedbackEventID string `json:"completion_feedback_event_id"`
	}
	var humanBinding, platformBinding completionBinding
	_ = json.Unmarshal([]byte(human.EvidenceJSON), &humanBinding)
	_ = json.Unmarshal([]byte(platform.EvidenceJSON), &platformBinding)
	if deliveryGateRunNewer(*platform, *human) &&
		strings.TrimSpace(humanBinding.CompletionFeedbackEventID) != "" &&
		strings.TrimSpace(platformBinding.CompletionFeedbackEventID) != "" &&
		humanBinding.CompletionFeedbackEventID != platformBinding.CompletionFeedbackEventID {
		return platform
	}
	return human
}

func deliveryGateRunNewer(candidate, current workboard.GateRun) bool {
	return candidate.CreatedAt.After(current.CreatedAt) ||
		(candidate.CreatedAt.Equal(current.CreatedAt) && candidate.ID > current.ID)
}

func (r *WorkBoardRepository) latestDeliveryReviewRunByExecutor(
	ctx context.Context,
	changeRequestID string,
	human bool,
	completionID string,
) (*workboard.GateRun, error) {
	var latest workboard.GateRun
	q := r.db.WithContext(ctx).Where(
		"subject_kind = ? AND subject_id = ? AND gate = ?",
		workboard.GateRunSubjectChangeRequest, changeRequestID, governanceprofile.DeliveryReviewGateKey,
	)
	if workspaceID := workboard.WorkspaceID(ctx); workspaceID != "" {
		q = q.Where("workspace_id = ?", workspaceID)
	}
	if human {
		q = q.Where("executor = ?", workboard.GateRunExecutorHuman)
	} else {
		q = q.Where("executor <> ?", workboard.GateRunExecutorHuman)
	}
	if completionID != "" {
		q = q.Where("completion_feedback_event_id = ?", completionID)
	}
	err := q.Order("created_at DESC, id DESC").First(&latest).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &latest, nil
}

func (r *WorkBoardRepository) RecordDeliveryDecision(
	ctx context.Context,
	in workboard.DeliveryDecisionInput,
) (*workboard.GateRun, error) {
	changeRequestID := strings.TrimSpace(in.ChangeRequestID)
	reviewedGateRunID := strings.TrimSpace(in.ReviewedGateRunID)
	completionFeedbackEventID := strings.TrimSpace(in.CompletionFeedbackEventID)
	actor := strings.TrimSpace(in.Actor)
	note := strings.TrimSpace(in.Note)
	if changeRequestID == "" || actor == "" {
		return nil, workboard.ErrValidation
	}
	decision := in.Decision
	state := workboard.NextActionStateFail
	hint := "delivery rejected"
	if decision == workboard.DeliveryDecisionApprove {
		state = workboard.NextActionStatePass
		hint = "delivery accepted"
	} else if decision != workboard.DeliveryDecisionReject {
		return nil, workboard.ErrValidation
	}
	if actor != "" {
		hint += " by " + actor
	}
	if note != "" {
		hint += ": " + note
	}
	now := time.Now().UTC()
	evidence := map[string]any{
		"evidence_contract_version": "gate-run-v1",
		"gate":                      governanceprofile.DeliveryReviewGateKey,
		"verdict":                   string(state),
		"confidence":                1.0,
		"decision":                  string(decision),
		"note":                      note,
		"change_request_id":         changeRequestID,
		"evaluator": map[string]any{
			"type":  "human_decision",
			"actor": actor,
			"trust": "human_decision",
		},
	}
	run := workboard.GateRun{
		ID:                        uuid.NewString(),
		SubjectKind:               workboard.GateRunSubjectChangeRequest,
		SubjectID:                 changeRequestID,
		Gate:                      governanceprofile.DeliveryReviewGateKey,
		State:                     state,
		Hint:                      hint,
		Executor:                  workboard.GateRunExecutorHuman,
		CompletionFeedbackEventID: completionFeedbackEventID,
		CreatedAt:                 now,
	}
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing workboard.ChangeRequest
		if err := scopeWorkBoardQuery(tx.Clauses(clause.Locking{Strength: "UPDATE"}), ctx).First(&existing, "id = ?", changeRequestID).Error; err != nil {
			return mapWorkBoardNotFound(err)
		}
		run.WorkspaceID = existing.WorkspaceID
		if reviewedGateRunID == "" || completionFeedbackEventID == "" {
			return workboard.ErrValidation
		}
		var latestCompletion integrations.GovernanceFeedbackEvent
		if err := tx.Where(
			"workspace_id = ? AND change_request_id = ? AND event_type = ?",
			existing.WorkspaceID,
			changeRequestID,
			integrations.FeedbackEventCodingAgentCompleted,
		).Order("created_at DESC, id DESC").First(&latestCompletion).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return workboard.ErrConflict
			}
			return err
		}
		if latestCompletion.ID != completionFeedbackEventID {
			return workboard.ErrConflict
		}
		var previous workboard.GateRun
		err := tx.
			Where(
				"id = ? AND workspace_id = ? AND subject_kind = ? AND subject_id = ? AND gate = ? AND executor <> ? AND completion_feedback_event_id = ?",
				reviewedGateRunID,
				existing.WorkspaceID,
				workboard.GateRunSubjectChangeRequest,
				changeRequestID,
				governanceprofile.DeliveryReviewGateKey,
				workboard.GateRunExecutorHuman,
				completionFeedbackEventID,
			).
			First(&previous).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return workboard.ErrConflict
		}
		if err != nil {
			return err
		}
		var latestPlatform workboard.GateRun
		if err := tx.Where(
			"workspace_id = ? AND subject_kind = ? AND subject_id = ? AND gate = ? AND executor <> ? AND completion_feedback_event_id = ?",
			existing.WorkspaceID,
			workboard.GateRunSubjectChangeRequest,
			changeRequestID,
			governanceprofile.DeliveryReviewGateKey,
			workboard.GateRunExecutorHuman,
			completionFeedbackEventID,
		).Order("created_at DESC, id DESC").First(&latestPlatform).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return workboard.ErrConflict
		} else if err != nil {
			return err
		}
		if latestPlatform.ID != reviewedGateRunID {
			return workboard.ErrConflict
		}
		var humanCount int64
		if err := tx.Model(&workboard.GateRun{}).Where(
			"workspace_id = ? AND subject_kind = ? AND subject_id = ? AND gate = ? AND executor = ? AND completion_feedback_event_id = ?",
			existing.WorkspaceID,
			workboard.GateRunSubjectChangeRequest,
			changeRequestID,
			governanceprofile.DeliveryReviewGateKey,
			workboard.GateRunExecutorHuman,
			completionFeedbackEventID,
		).Count(&humanCount).Error; err != nil {
			return err
		}
		if humanCount > 0 {
			return workboard.ErrConflict
		}
		var previousEvidence struct {
			Evidence                  string   `json:"evidence"`
			CompletionFeedbackEventID string   `json:"completion_feedback_event_id"`
			Confidence                *float64 `json:"confidence"`
			JudgeModel                string   `json:"judge_model"`
			EvalSuiteVersion          string   `json:"eval_suite_version"`
			Evaluator                 struct {
				JudgeModel       string `json:"judge_model"`
				EvalSuiteVersion string `json:"eval_suite_version"`
			} `json:"evaluator"`
		}
		if json.Unmarshal([]byte(previous.EvidenceJSON), &previousEvidence) == nil {
			if strings.TrimSpace(previousEvidence.Evidence) != "" {
				evidence["evidence"] = previousEvidence.Evidence
			}
			if strings.TrimSpace(previousEvidence.CompletionFeedbackEventID) != "" {
				evidence["completion_feedback_event_id"] = previousEvidence.CompletionFeedbackEventID
			}
			judgeModel := strings.TrimSpace(previousEvidence.Evaluator.JudgeModel)
			if judgeModel == "" {
				judgeModel = strings.TrimSpace(previousEvidence.JudgeModel)
			}
			if judgeModel != "" {
				evidence["evidence_judge_model"] = judgeModel
			}
			evalSuiteVersion := strings.TrimSpace(previousEvidence.Evaluator.EvalSuiteVersion)
			if evalSuiteVersion == "" {
				evalSuiteVersion = strings.TrimSpace(previousEvidence.EvalSuiteVersion)
			}
			if evalSuiteVersion != "" {
				evidence["evidence_eval_suite_version"] = evalSuiteVersion
			}
			if previousEvidence.Confidence != nil {
				evidence["evidence_confidence"] = *previousEvidence.Confidence
			}
		}
		evidence["evidence_verdict"] = string(previous.State)
		evidence["reviewed_gate_run_id"] = previous.ID
		evidencePayload, err := json.Marshal(evidence)
		if err != nil {
			return err
		}
		run.EvidenceJSON = string(evidencePayload)
		if err := tx.Create(&run).Error; err != nil {
			return err
		}
		if state == workboard.NextActionStatePass && r.autoArchiveOnDeliveryPass() {
			updates := map[string]any{
				"archived":       true,
				"archived_at":    now,
				"archived_by":    actor,
				"archive_reason": "delivery accepted by human reviewer",
				"updated_at":     now,
			}
			if err := scopeWorkBoardQuery(tx.Model(&workboard.ChangeRequest{}), ctx).Where("id = ?", changeRequestID).Updates(updates).Error; err != nil {
				return err
			}
			if !existing.Archived {
				if err := insertWorkBoardLifecycleEvent(tx, existing.WorkspaceID, "change_request", existing.ID, "change_request.archived", actor, map[string]any{
					"change_request_id":  existing.ID,
					"change_request_key": existing.Key,
					"feature_id":         existing.FeatureID,
					"archive_reason":     "delivery accepted by human reviewer",
					"delivery_decision":  string(decision),
					"changed_at":         now,
				}, now); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return &run, nil
}

func deliveryRunAuditFields(run workboard.GateRun) (actor string, note string, summary string) {
	var wrapper struct {
		Decision  string `json:"decision,omitempty"`
		Note      string `json:"note,omitempty"`
		Evaluator struct {
			Actor string `json:"actor,omitempty"`
			Trust string `json:"trust,omitempty"`
			Type  string `json:"type,omitempty"`
		} `json:"evaluator,omitempty"`
	}
	_ = json.Unmarshal([]byte(strings.TrimSpace(run.EvidenceJSON)), &wrapper)
	actor = strings.TrimSpace(wrapper.Evaluator.Actor)
	note = strings.TrimSpace(wrapper.Note)
	summary = workboard.DeliveryDecisionSummary(run, actor, note)
	return actor, note, summary
}
