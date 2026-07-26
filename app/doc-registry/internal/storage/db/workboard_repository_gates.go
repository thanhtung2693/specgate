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
	"github.com/specgate/doc-registry/internal/knowledge"
	"github.com/specgate/doc-registry/internal/workboard"
)

func (r *WorkBoardRepository) RefreshGateRuns(
	ctx context.Context,
	in workboard.RefreshGateRunsInput,
) ([]workboard.GateRun, error) {
	changeRequestID := strings.TrimSpace(in.ChangeRequestID)
	if changeRequestID == "" {
		return nil, workboard.ErrValidation
	}
	var cr workboard.ChangeRequest
	if err := scopeWorkBoardQuery(r.db.WithContext(ctx), ctx).First(&cr, "id = ?", changeRequestID).Error; err != nil {
		return nil, mapWorkBoardNotFound(err)
	}
	var actions []workboard.NextAction
	if !in.EvaluationsOnly {
		nextActions, err := r.NextActions(ctx, changeRequestID)
		if err != nil {
			if errors.Is(err, workboard.ErrNotFound) {
				rows, fallbackErr := r.persistPolicyUnavailableEvaluations(ctx, cr, in.Evaluations)
				if fallbackErr != nil {
					return nil, fallbackErr
				}
				if len(rows) > 0 {
					return rows, nil
				}
			}
			return nil, err
		}
		actions = nextActions
	}
	// Quick-route change requests may have no feature (see NextActions).
	var feature workboard.Feature
	if cr.FeatureID != "" {
		featureQuery := r.db.WithContext(ctx).Where("id = ?", cr.FeatureID)
		if cr.WorkspaceID != "" {
			featureQuery = featureQuery.Where("workspace_id = ?", cr.WorkspaceID)
		}
		if err := featureQuery.First(&feature).Error; err != nil {
			return nil, mapWorkBoardNotFound(err)
		}
	}
	warnings, err := r.ListStaleWarnings(ctx, workboard.StaleWarningFilter{ChangeRequestID: changeRequestID})
	if err != nil {
		return nil, err
	}
	warningRows := make([]map[string]any, 0, len(warnings))
	for _, warning := range warnings {
		warningRows = append(warningRows, map[string]any{
			"code":        warning.Code,
			"message":     warning.Message,
			"artifact_id": warning.ArtifactID,
		})
	}
	linkedKnowledgeRows, err := r.listLinkedKnowledgeEvidence(ctx, cr.WorkspaceID, feature)
	if err != nil {
		return nil, err
	}
	baseArtifactID := strings.TrimSpace(cr.LeadArtifactID)
	if baseArtifactID == "" {
		baseArtifactID = strings.TrimSpace(feature.CanonicalArtifactID)
	}
	now := time.Now().UTC()
	rows := make([]workboard.GateRun, 0, len(actions))
	evalsByGate := map[string]workboard.GateEvaluation{}
	for _, eval := range in.Evaluations {
		gate := strings.TrimSpace(eval.Gate)
		if gate == "" {
			continue
		}
		if eval.Confidence < 0 || eval.Confidence > 1 {
			return nil, workboard.ErrValidation
		}
		evalsByGate[gate] = eval
	}
	for _, action := range actions {
		eval, hasEval := evalsByGate[action.Gate]
		state := action.State
		hint := action.Hint
		confidence := gateConfidenceFromState(action.State)
		judgeModel := "deterministic-v1"
		evalSuiteVersion := "none"
		evidence := ""
		if hasEval {
			if eval.State != "" {
				state = eval.State
			}
			if strings.TrimSpace(eval.Hint) != "" {
				hint = strings.TrimSpace(eval.Hint)
			}
			if eval.Confidence >= 0 {
				confidence = eval.Confidence
			}
			if strings.TrimSpace(eval.JudgeModel) != "" {
				judgeModel = strings.TrimSpace(eval.JudgeModel)
			}
			if strings.TrimSpace(eval.EvalSuiteVersion) != "" {
				evalSuiteVersion = strings.TrimSpace(eval.EvalSuiteVersion)
			}
			evidence = strings.TrimSpace(eval.Evidence)
		}
		evidenceJSON := `{}`
		verdict := gateVerdictFromState(state)
		completionFeedbackEventID := deliveryEvaluationCompletionID(evidence)
		evidencePayload, _ := json.Marshal(map[string]any{
			"evidence_contract_version": "gate-run-v1",
			"gate":                      action.Gate,
			"evaluator": map[string]any{
				"type":               evaluatorType(hasEval),
				"judge_model":        judgeModel,
				"config_version":     "workboard-next-actions-v1",
				"eval_suite_version": evalSuiteVersion,
			},
			"verdict":                      verdict,
			"confidence":                   confidence,
			"evidence":                     evidence,
			"completion_feedback_event_id": completionFeedbackEventID,
			"change_request_id":            changeRequestID,
			"feature_id":                   feature.ID,
			"source_artifact_id":           baseArtifactID,
			"lead_artifact_id":             cr.LeadArtifactID,
			"canonical_artifact_id":        feature.CanonicalArtifactID,
			"linked_knowledge":             linkedKnowledgeRows,
			"warnings":                     warningRows,
		})
		if len(evidencePayload) > 0 {
			evidenceJSON = string(evidencePayload)
		}
		run := workboard.GateRun{
			ID:                        uuid.NewString(),
			WorkspaceID:               cr.WorkspaceID,
			SubjectKind:               workboard.GateRunSubjectChangeRequest,
			SubjectID:                 changeRequestID,
			Gate:                      action.Gate,
			State:                     state,
			Hint:                      hint,
			Executor:                  workboard.GateRunExecutorPlatform,
			EvidenceJSON:              evidenceJSON,
			CompletionFeedbackEventID: completionFeedbackEventID,
			CreatedAt:                 now,
		}
		rows = append(rows, run)
	}
	// Eval-only gates — the model-judged ones — have no deterministic next-action,
	// so the loop above never emits a row for them. Persist them straight from
	// the evaluations so the review UI can show their verdicts.
	actionGates := make(map[string]bool, len(actions))
	for _, action := range actions {
		actionGates[action.Gate] = true
	}
	for _, eval := range in.Evaluations {
		gate := strings.TrimSpace(eval.Gate)
		if gate == "" || actionGates[gate] {
			continue
		}
		completionFeedbackEventID := deliveryEvaluationCompletionID(eval.Evidence)
		evidencePayload, _ := json.Marshal(map[string]any{
			"evidence_contract_version": "gate-run-v1",
			"gate":                      gate,
			"evaluator": map[string]any{
				"type":               "agent_judge",
				"judge_model":        strings.TrimSpace(eval.JudgeModel),
				"config_version":     "workboard-next-actions-v1",
				"eval_suite_version": strings.TrimSpace(eval.EvalSuiteVersion),
			},
			"verdict":                      string(eval.State),
			"confidence":                   eval.Confidence,
			"evidence":                     strings.TrimSpace(eval.Evidence),
			"completion_feedback_event_id": completionFeedbackEventID,
			"change_request_id":            changeRequestID,
			"feature_id":                   feature.ID,
			"source_artifact_id":           baseArtifactID,
		})
		rows = append(rows, workboard.GateRun{
			ID:                        uuid.NewString(),
			WorkspaceID:               cr.WorkspaceID,
			SubjectKind:               workboard.GateRunSubjectChangeRequest,
			SubjectID:                 changeRequestID,
			Gate:                      gate,
			State:                     eval.State,
			Hint:                      strings.TrimSpace(eval.Hint),
			Executor:                  workboard.GateRunExecutorPlatform,
			EvidenceJSON:              string(evidencePayload),
			CompletionFeedbackEventID: completionFeedbackEventID,
			CreatedAt:                 now,
		})
	}
	if len(rows) == 0 {
		return rows, nil
	}
	if err := r.createGateRunsWithChangeRequestLock(ctx, changeRequestID, rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// persistPolicyUnavailableEvaluations keeps the deterministic fail-closed
// delivery guard durable even when a dangling policy dependency prevents the
// normal deterministic gate refresh from being assembled.
func (r *WorkBoardRepository) persistPolicyUnavailableEvaluations(
	ctx context.Context,
	cr workboard.ChangeRequest,
	evaluations []workboard.GateEvaluation,
) ([]workboard.GateRun, error) {
	now := time.Now().UTC()
	rows := make([]workboard.GateRun, 0, 1)
	for _, eval := range evaluations {
		if eval.Confidence < 0 || eval.Confidence > 1 {
			return nil, workboard.ErrValidation
		}
		if strings.TrimSpace(eval.Gate) != governanceprofile.DeliveryReviewGateKey ||
			eval.State != workboard.NextActionStateNeedsHumanReview {
			continue
		}
		var detail struct {
			ReasonCode string `json:"reason_code"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(eval.Evidence)), &detail) != nil ||
			detail.ReasonCode != "policy_unavailable" {
			continue
		}
		completionFeedbackEventID := deliveryEvaluationCompletionID(eval.Evidence)
		evidencePayload, _ := json.Marshal(map[string]any{
			"evidence_contract_version": "gate-run-v1",
			"gate":                      governanceprofile.DeliveryReviewGateKey,
			"evaluator": map[string]any{
				"type":               "deterministic_policy_guard",
				"judge_model":        strings.TrimSpace(eval.JudgeModel),
				"config_version":     "workboard-next-actions-v1",
				"eval_suite_version": strings.TrimSpace(eval.EvalSuiteVersion),
			},
			"verdict":                      string(eval.State),
			"confidence":                   eval.Confidence,
			"evidence":                     strings.TrimSpace(eval.Evidence),
			"completion_feedback_event_id": completionFeedbackEventID,
			"change_request_id":            cr.ID,
			"feature_id":                   cr.FeatureID,
			"source_artifact_id":           cr.LeadArtifactID,
		})
		rows = append(rows, workboard.GateRun{
			ID:                        uuid.NewString(),
			WorkspaceID:               cr.WorkspaceID,
			SubjectKind:               workboard.GateRunSubjectChangeRequest,
			SubjectID:                 cr.ID,
			Gate:                      governanceprofile.DeliveryReviewGateKey,
			State:                     eval.State,
			Hint:                      strings.TrimSpace(eval.Hint),
			Executor:                  workboard.GateRunExecutorPlatform,
			EvidenceJSON:              string(evidencePayload),
			CompletionFeedbackEventID: completionFeedbackEventID,
			CreatedAt:                 now,
		})
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if err := r.createGateRunsWithChangeRequestLock(ctx, cr.ID, rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *WorkBoardRepository) createGateRunsWithChangeRequestLock(
	ctx context.Context,
	changeRequestID string,
	rows []workboard.GateRun,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var cr workboard.ChangeRequest
		if err := scopeWorkBoardQuery(
			tx.Clauses(clause.Locking{Strength: "UPDATE"}),
			ctx,
		).Select("id").First(&cr, "id = ?", changeRequestID).Error; err != nil {
			return mapWorkBoardNotFound(err)
		}
		return tx.Create(&rows).Error
	})
}

func evaluatorType(hasEval bool) string {
	if hasEval {
		return "agent_judge"
	}
	return "deterministic"
}

func gateVerdictFromState(state workboard.NextActionState) string {
	switch state {
	case workboard.NextActionStatePass:
		return "pass"
	case workboard.NextActionStateWarn:
		return "warn"
	case workboard.NextActionStateNotApplicable:
		return "not_applicable"
	default:
		return "pending"
	}
}

func gateConfidenceFromState(state workboard.NextActionState) float64 {
	switch state {
	case workboard.NextActionStatePass:
		return 0.95
	case workboard.NextActionStateWarn:
		return 0.75
	default:
		return 0.60
	}
}

func (r *WorkBoardRepository) listLinkedKnowledgeEvidence(
	ctx context.Context,
	workspaceID string,
	feature workboard.Feature,
) ([]map[string]any, error) {
	// Gate evidence is workspace-scoped like the freshness warning; without a
	// workspace there is nothing safely in scope.
	if strings.TrimSpace(workspaceID) == "" {
		return []map[string]any{}, nil
	}
	featureRefs := []string{feature.ID}
	if strings.TrimSpace(feature.Key) != "" && feature.Key != feature.ID {
		featureRefs = append(featureRefs, feature.Key)
	}
	var docs []knowledge.Document
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND is_latest = ? AND status = ? AND linked_feature_id IN ?", workspaceID, true, knowledge.StatusIndexed, featureRefs).
		Order("updated_at DESC").
		Limit(5).
		Find(&docs).Error; err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(docs))
	for _, doc := range docs {
		out = append(out, map[string]any{
			"document_id": doc.DocumentID,
			"version":     doc.Version,
			"title":       doc.Title,
			"updated_at":  doc.UpdatedAt,
		})
	}
	return out, nil
}
