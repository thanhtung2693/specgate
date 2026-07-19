package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/governancethreads"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/knowledge"
	"github.com/specgate/doc-registry/internal/workboard"
)

func insertDeliveryCompletion(
	t *testing.T,
	gdb *gorm.DB,
	workspaceID string,
	changeRequestID string,
	completionID string,
	createdAt time.Time,
) {
	t.Helper()
	if err := gdb.Create(&integrations.GovernanceFeedbackEvent{
		ID: completionID, WorkspaceID: workspaceID, ChangeRequestID: changeRequestID,
		EventType:   integrations.FeedbackEventCodingAgentCompleted,
		PayloadJSON: `{"summary":"done","agent":{"name":"builder"}}`,
		Status:      integrations.FeedbackStatusReceived, CreatedAt: createdAt, UpdatedAt: createdAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestWorkBoardRepository_CreateChangeRequestAllowsFeaturelessQuickWork(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		now := time.Now().UTC().Truncate(time.Second)

		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:                 "cr-featureless-quick",
			Key:                "CR-FEATURELESS-QUICK",
			WorkType:           workboard.WorkTypeBugFix,
			Title:              "Fix quick path",
			IntentMD:           "Small CR without a durable product Feature.",
			AcceptanceCriteria: `["Quick path remains self-contained."]`,
			CreatedAt:          now,
			UpdatedAt:          now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if cr.FeatureID != "" {
			t.Fatalf("FeatureID = %q, want empty", cr.FeatureID)
		}

		reloaded, err := repo.GetChangeRequest(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if reloaded.FeatureID != "" {
			t.Fatalf("reloaded FeatureID = %q, want empty", reloaded.FeatureID)
		}
		if reloaded.Phase != workboard.BoardPhaseReady {
			t.Fatalf("phase = %q, want %q", reloaded.Phase, workboard.BoardPhaseReady)
		}
	})
}

func TestWorkBoardRepository_CreateChangeRequestRejectsCrossWorkspaceFeatureAtomically(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := context.Background()
		if _, err := repo.CreateFeature(ctx, workboard.Feature{ID: "feat-link-a", WorkspaceID: "ws-a", Key: "link-a", Name: "A"}); err != nil {
			t.Fatal(err)
		}
		_, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID: "cr-link-b", Key: "CR-LINK-B", WorkspaceID: "ws-b", FeatureID: "feat-link-a",
			WorkType: workboard.WorkTypeFeatureChange, Title: "must reject", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		})
		if !errors.Is(err, workboard.ErrNotFound) {
			t.Fatalf("error = %v, want not found", err)
		}
		var count int64
		if err := gdb.Model(&workboard.ChangeRequest{}).Where("id = ?", "cr-link-b").Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("cross-workspace request was persisted: %d rows", count)
		}
	})
}

func TestWorkBoardRepository_CreateChangeRequestValidatesLeadArtifactBinding(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		artifactRepo := NewRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-a")
		now := time.Now().UTC()

		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID: "feat-a", Key: "FEAT-A", Name: "Feature A",
		})
		if err != nil {
			t.Fatal(err)
		}
		otherFeature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID: "feat-other", Key: "FEAT-OTHER", Name: "Other feature",
		})
		if err != nil {
			t.Fatal(err)
		}

		artifacts := []*artifact.Artifact{
			{ID: "art-approved", WorkspaceID: "ws-a", FeatureID: feature.ID, Version: "v0.1", Status: artifact.StatusApproved},
			{ID: "art-superseded", WorkspaceID: "ws-a", FeatureID: feature.ID, Version: "v0.2", Status: artifact.StatusSuperseded},
			{ID: "art-draft", WorkspaceID: "ws-a", FeatureID: feature.ID, Version: "v0.3", Status: artifact.StatusDraft},
			{ID: "art-needs-changes", WorkspaceID: "ws-a", FeatureID: feature.ID, Version: "v0.4", Status: artifact.StatusNeedsChanges},
			{ID: "art-other-feature", WorkspaceID: "ws-a", FeatureID: otherFeature.ID, Version: "v0.1", Status: artifact.StatusApproved},
			{ID: "art-other-workspace", WorkspaceID: "ws-b", FeatureID: feature.ID, Version: "v0.1", Status: artifact.StatusApproved},
		}
		for _, row := range artifacts {
			row.RequestType = artifact.RequestTypeChangeRequest
			row.ImpactLevel = artifact.ImpactLevelLow
			row.CreatedBy = "tester"
			row.CreatedAt = now
			row.UpdatedAt = now
			if err := artifactRepo.Insert(context.Background(), row); err != nil {
				t.Fatalf("insert %s: %v", row.ID, err)
			}
		}

		tests := []struct {
			name       string
			artifactID string
			wantErr    bool
		}{
			{name: "approved", artifactID: "art-approved"},
			{name: "superseded", artifactID: "art-superseded"},
			{name: "draft", artifactID: "art-draft", wantErr: true},
			{name: "needs changes", artifactID: "art-needs-changes", wantErr: true},
			{name: "different feature", artifactID: "art-other-feature", wantErr: true},
			{name: "different workspace", artifactID: "art-other-workspace", wantErr: true},
		}
		for i, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
					ID:             fmt.Sprintf("cr-binding-%d", i),
					Key:            fmt.Sprintf("CR-BINDING-%d", i),
					FeatureID:      feature.ID,
					WorkType:       workboard.WorkTypeFeatureChange,
					Title:          test.name,
					LeadArtifactID: test.artifactID,
				})
				if test.wantErr {
					if !errors.Is(err, workboard.ErrNotFound) {
						t.Fatalf("error = %v, want ErrNotFound", err)
					}
					if cr != nil {
						t.Fatalf("created invalid change request: %+v", cr)
					}
					return
				}
				if err != nil {
					t.Fatalf("CreateChangeRequest: %v", err)
				}
			})
		}
	})
}

func TestWorkBoardRepository_UpdateChangeRequestRejectsCrossWorkspaceGovernanceThread(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-a")
		now := time.Now().UTC()
		if err := gdb.Create(&governancethreads.Thread{
			ThreadID:    "thread-ws-b",
			WorkspaceID: "ws-b",
			Title:       "Other workspace thread",
			CreatedAt:   now,
			UpdatedAt:   now,
		}).Error; err != nil {
			t.Fatal(err)
		}
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID: "cr-thread-ws-a", Key: "CR-THREAD-WS-A", WorkType: workboard.WorkTypeBugFix, Title: "Scoped CR",
		})
		if err != nil {
			t.Fatal(err)
		}

		_, err = repo.UpdateChangeRequest(ctx, workboard.ChangeRequest{
			ID: cr.ID, GovernanceThreadID: "thread-ws-b",
		})
		if !errors.Is(err, workboard.ErrNotFound) {
			t.Fatalf("UpdateChangeRequest error = %v, want ErrNotFound", err)
		}
		stored, err := repo.GetChangeRequest(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.GovernanceThreadID != "" {
			t.Fatalf("GovernanceThreadID = %q, cross-workspace link persisted", stored.GovernanceThreadID)
		}
	})
}

func TestWorkBoardRepository_WorkspaceScopedFeatures(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := context.Background()

		a, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:          "feat-shared-a",
			WorkspaceID: "ws-a",
			Key:         "shared-key",
			Name:        "Shared A",
			Status:      workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}
		b, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:          "feat-shared-b",
			WorkspaceID: "ws-b",
			Key:         "shared-key",
			Name:        "Shared B",
			Status:      workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}

		list, err := repo.ListFeaturesInWorkspace(ctx, "ws-a")
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].ID != a.ID {
			t.Fatalf("workspace list=%+v, want only %s", list, a.ID)
		}
		got, err := repo.GetFeatureInWorkspace(ctx, "ws-a", a.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != a.ID || got.WorkspaceID != "ws-a" {
			t.Fatalf("got %+v, want ws-a feature", got)
		}
		if _, err := repo.GetFeatureInWorkspace(ctx, "ws-a", b.ID); err != workboard.ErrNotFound {
			t.Fatalf("cross-workspace get err=%v, want ErrNotFound", err)
		}
		byKey, err := repo.GetFeatureByKey(
			workboard.WithWorkspace(ctx, "ws-b"),
			"shared-key",
		)
		if err != nil {
			t.Fatal(err)
		}
		if byKey.ID != b.ID {
			t.Fatalf("GetFeatureByKey ws-b=%s, want %s", byKey.ID, b.ID)
		}
	})
}

func TestWorkBoardRepository_ListChangeRequestsInWorkspace(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := context.Background()
		for _, item := range []workboard.ChangeRequest{
			{ID: "cr-list-a", WorkspaceID: "ws-a", Key: "CR-LIST-A", WorkType: workboard.WorkTypeBugFix, Title: "A"},
			{ID: "cr-list-b", WorkspaceID: "ws-b", Key: "CR-LIST-B", WorkType: workboard.WorkTypeBugFix, Title: "B"},
		} {
			if _, err := repo.CreateChangeRequest(ctx, item); err != nil {
				t.Fatal(err)
			}
		}

		items, err := repo.ListChangeRequestsInWorkspace(ctx, "ws-a", false)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].ID != "cr-list-a" {
			t.Fatalf("workspace list = %+v, want only cr-list-a", items)
		}
	})
}

func TestWorkBoardRepository_ListAcceptanceCriteriaEmptyReturnsPromptly(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		now := time.Now().UTC().Truncate(time.Second)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:                 "cr-empty-ac",
			Key:                "CR-EMPTY-AC",
			WorkType:           workboard.WorkTypeBugFix,
			Title:              "No acceptance criteria",
			IntentMD:           "CR with no acceptance criteria.",
			AcceptanceCriteria: "",
			CreatedAt:          now,
			UpdatedAt:          now,
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		rows, err := repo.ListAcceptanceCriteria(ctx, cr.ID)
		if err != nil {
			t.Fatalf("ListAcceptanceCriteria hung/failed on empty AC: %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("rows = %d, want 0", len(rows))
		}
	})
}

func TestWorkBoardRepository_ListAcceptanceCriteriaDoesNotRecreateMissingRows(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:                 "cr-missing-ac-rows",
			Key:                "CR-MISSING-AC-ROWS",
			WorkType:           workboard.WorkTypeBugFix,
			Title:              "Missing canonical criteria",
			AcceptanceCriteria: `["The canonical row keeps its identity."]`,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := gdb.Where("change_request_id = ?", cr.ID).
			Delete(&workboard.AcceptanceCriterion{}).Error; err != nil {
			t.Fatal(err)
		}

		rows, err := repo.ListAcceptanceCriteria(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 0 {
			t.Fatalf("rows = %+v, want no synthesized criteria", rows)
		}

		var count int64
		if err := gdb.Model(&workboard.AcceptanceCriterion{}).
			Where("change_request_id = ?", cr.ID).
			Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("canonical row count = %d, want 0 after read", count)
		}
	})
}

func TestWorkBoardRepository_RecordDeliveryDecisionInsertsHumanGateRunAndArchivesWithHumanReason(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		repo.SetAutoArchiveOnDeliveryPass(func() bool { return true })
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		now := time.Now().UTC().Truncate(time.Second)
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:        "cr-human-approve",
			Key:       "CR-HUMAN-APPROVE",
			WorkType:  workboard.WorkTypeBugFix,
			Title:     "Approve delivery",
			CreatedAt: now,
			UpdatedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		insertDeliveryCompletion(t, gdb, "ws-test", cr.ID, "completion-1", now.Add(-time.Minute))
		if err := gdb.Create(&workboard.GateRun{
			ID: "platform-review-1", WorkspaceID: "ws-test",
			SubjectKind: workboard.GateRunSubjectChangeRequest, SubjectID: cr.ID,
			Gate: "delivery_review", State: workboard.NextActionStatePass,
			Executor:                  workboard.GateRunExecutorPlatform,
			EvidenceJSON:              `{"completion_feedback_event_id":"completion-1"}`,
			CompletionFeedbackEventID: "completion-1", CreatedAt: now,
		}).Error; err != nil {
			t.Fatal(err)
		}

		run, err := repo.RecordDeliveryDecision(ctx, workboard.DeliveryDecisionInput{
			ChangeRequestID: cr.ID, ReviewedGateRunID: "platform-review-1",
			CompletionFeedbackEventID: "completion-1",
			Decision:                  workboard.DeliveryDecisionApprove,
			Actor:                     "lead@example.com",
			Note:                      "Reviewed the evidence and release notes.",
		})
		if err != nil {
			t.Fatalf("RecordDeliveryDecision: %v", err)
		}
		if run.Gate != "delivery_review" || run.State != workboard.NextActionStatePass {
			t.Fatalf("run gate/state = %s/%s, want delivery_review/pass", run.Gate, run.State)
		}
		if run.Executor != workboard.GateRunExecutorHuman {
			t.Fatalf("executor = %q, want human", run.Executor)
		}
		if !strings.Contains(run.EvidenceJSON, `"actor":"lead@example.com"`) ||
			!strings.Contains(run.EvidenceJSON, `"trust":"human_decision"`) ||
			!strings.Contains(run.EvidenceJSON, `"note":"Reviewed the evidence and release notes."`) {
			t.Fatalf("evidence_json missing human audit fields: %s", run.EvidenceJSON)
		}

		reloaded, err := repo.GetChangeRequest(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reloaded.Archived {
			t.Fatal("human-approved delivery should auto-archive when auto-archive is enabled")
		}
		if reloaded.ArchiveReason != "delivery accepted by human reviewer" {
			t.Fatalf("archive reason = %q, want human acceptance reason", reloaded.ArchiveReason)
		}
	})
}

func TestWorkBoardRepository_RecordDeliveryDecisionPreservesLatestPlatformEvidence(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		now := time.Now().UTC().Truncate(time.Second)
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:        "cr-human-evidence",
			Key:       "CR-HUMAN-EVIDENCE",
			WorkType:  workboard.WorkTypeBugFix,
			Title:     "Preserve delivery evidence",
			CreatedAt: now,
			UpdatedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		insertDeliveryCompletion(t, gdb, "ws-test", cr.ID, "completion-1", now.Add(-2*time.Minute))

		detail := `{"criteria":[{"criterion_id":"ac-1","text":"Tests pass","verdict":"met","trust_tier":"grounded"}],"checks":[{"name":"tests","status":"pass","detail":"ok"}]}`
		platformEvidence, err := json.Marshal(map[string]any{
			"evidence_contract_version":    "delivery-review-v2",
			"verdict":                      "needs_human_review",
			"completion_feedback_event_id": "completion-1",
			"evidence":                     detail,
			"judge_model":                  "agent_attested",
			"eval_suite_version":           "delivery-review-v1",
			"confidence":                   0,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := gdb.Create(&workboard.GateRun{
			ID:                        "platform-review-with-evidence",
			WorkspaceID:               "ws-test",
			SubjectKind:               workboard.GateRunSubjectChangeRequest,
			SubjectID:                 cr.ID,
			Executor:                  workboard.GateRunExecutorPlatform,
			Gate:                      "delivery_review",
			State:                     workboard.NextActionStateNeedsHumanReview,
			Hint:                      "Human review required.",
			EvidenceJSON:              string(platformEvidence),
			CompletionFeedbackEventID: "completion-1",
			CreatedAt:                 now.Add(-time.Minute),
		}).Error; err != nil {
			t.Fatal(err)
		}

		run, err := repo.RecordDeliveryDecision(ctx, workboard.DeliveryDecisionInput{
			ChangeRequestID: cr.ID, ReviewedGateRunID: "platform-review-with-evidence",
			CompletionFeedbackEventID: "completion-1",
			Decision:                  workboard.DeliveryDecisionApprove,
			Actor:                     "lead@example.com",
			Note:                      "Reviewed the grounded evidence.",
		})
		if err != nil {
			t.Fatalf("RecordDeliveryDecision: %v", err)
		}
		var recorded struct {
			Evidence                  string  `json:"evidence"`
			EvidenceVerdict           string  `json:"evidence_verdict"`
			CompletionFeedbackEventID string  `json:"completion_feedback_event_id"`
			ReviewedGateRunID         string  `json:"reviewed_gate_run_id"`
			EvidenceJudgeModel        string  `json:"evidence_judge_model"`
			EvidenceEvalSuiteVersion  string  `json:"evidence_eval_suite_version"`
			EvidenceConfidence        float64 `json:"evidence_confidence"`
		}
		if err := json.Unmarshal([]byte(run.EvidenceJSON), &recorded); err != nil {
			t.Fatalf("decode human evidence: %v", err)
		}
		if recorded.Evidence != detail {
			t.Fatalf("human decision evidence = %q, want latest platform evidence %q", recorded.Evidence, detail)
		}
		if recorded.EvidenceVerdict != "needs_human_review" ||
			recorded.CompletionFeedbackEventID != "completion-1" ||
			recorded.ReviewedGateRunID != "platform-review-with-evidence" ||
			recorded.EvidenceJudgeModel != "agent_attested" ||
			recorded.EvidenceEvalSuiteVersion != "delivery-review-v1" ||
			recorded.EvidenceConfidence != 0 ||
			!strings.Contains(run.EvidenceJSON, `"evidence_confidence":0`) {
			t.Fatalf("human decision binding = %+v, want prior evidence verdict and completion", recorded)
		}
	})
}

func TestWorkBoardRepository_RecordDeliveryDecisionRejectsSecondHumanDecisionForSameCompletion(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		now := time.Now().UTC().Truncate(time.Second)
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID: "cr-one-human-decision", Key: "CR-ONE-HUMAN-DECISION",
			WorkType: workboard.WorkTypeBugFix, Title: "Keep one decision per completion",
		})
		if err != nil {
			t.Fatal(err)
		}
		insertDeliveryCompletion(t, gdb, "ws-test", cr.ID, "completion-1", now.Add(-time.Minute))
		if err := gdb.Create(&workboard.GateRun{
			ID: "platform-completion-1", WorkspaceID: "ws-test",
			SubjectKind: workboard.GateRunSubjectChangeRequest, SubjectID: cr.ID,
			Gate: "delivery_review", State: workboard.NextActionStatePass,
			Executor:                  workboard.GateRunExecutorPlatform,
			EvidenceJSON:              `{"completion_feedback_event_id":"completion-1"}`,
			CompletionFeedbackEventID: "completion-1", CreatedAt: now,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := repo.RecordDeliveryDecision(ctx, workboard.DeliveryDecisionInput{
			ChangeRequestID: cr.ID, ReviewedGateRunID: "platform-completion-1",
			CompletionFeedbackEventID: "completion-1",
			Decision:                  workboard.DeliveryDecisionApprove, Actor: "lead",
		}); err != nil {
			t.Fatal(err)
		}

		_, err = repo.RecordDeliveryDecision(ctx, workboard.DeliveryDecisionInput{
			ChangeRequestID: cr.ID, ReviewedGateRunID: "platform-completion-1",
			CompletionFeedbackEventID: "completion-1",
			Decision:                  workboard.DeliveryDecisionReject, Actor: "second-reviewer",
		})
		if !errors.Is(err, workboard.ErrValidation) {
			t.Fatalf("second decision error = %v, want validation", err)
		}
		var humanRuns int64
		if err := gdb.Model(&workboard.GateRun{}).
			Where("subject_id = ? AND gate = ? AND executor = ?", cr.ID, "delivery_review", workboard.GateRunExecutorHuman).
			Count(&humanRuns).Error; err != nil {
			t.Fatal(err)
		}
		if humanRuns != 1 {
			t.Fatalf("human decision rows = %d, want 1", humanRuns)
		}
	})
}

func TestWorkBoardRepository_NewerCompletionStartsNewDeliveryDecisionCycle(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		now := time.Now().UTC().Truncate(time.Second)
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID: "cr-new-delivery-cycle", Key: "CR-NEW-DELIVERY-CYCLE",
			WorkType: workboard.WorkTypeBugFix, Title: "Rework delivery",
		})
		if err != nil {
			t.Fatal(err)
		}
		insertPlatform := func(id, completionID string, state workboard.NextActionState, at time.Time) {
			t.Helper()
			detail := `{"completion_feedback_event_id":"` + completionID + `","criteria":[],"checks":[]}`
			outer, marshalErr := json.Marshal(map[string]any{
				"verdict": state, "completion_feedback_event_id": completionID, "evidence": detail,
			})
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			if createErr := gdb.Create(&workboard.GateRun{
				ID: id, WorkspaceID: "ws-test",
				SubjectKind: workboard.GateRunSubjectChangeRequest, SubjectID: cr.ID,
				Gate: "delivery_review", State: state, Executor: workboard.GateRunExecutorPlatform,
				EvidenceJSON: string(outer), CompletionFeedbackEventID: completionID,
				CreatedAt: at,
			}).Error; createErr != nil {
				t.Fatal(createErr)
			}
		}
		insertDeliveryCompletion(t, gdb, "ws-test", cr.ID, "completion-1", now.Add(-time.Minute))
		insertPlatform("platform-completion-1", "completion-1", workboard.NextActionStateFail, now)
		if _, err := repo.RecordDeliveryDecision(ctx, workboard.DeliveryDecisionInput{
			ChangeRequestID: cr.ID, ReviewedGateRunID: "platform-completion-1",
			CompletionFeedbackEventID: "completion-1",
			Decision:                  workboard.DeliveryDecisionReject, Actor: "lead",
		}); err != nil {
			t.Fatal(err)
		}
		insertPlatform("platform-completion-1-rerun", "completion-1", workboard.NextActionStatePass, now.Add(time.Minute))

		authoritative, err := repo.AuthoritativeDeliveryReviewRun(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if authoritative == nil || authoritative.Executor != workboard.GateRunExecutorHuman {
			t.Fatalf("same-completion rerun = %+v, want existing human authority", authoritative)
		}

		insertDeliveryCompletion(t, gdb, "ws-test", cr.ID, "completion-2", now.Add(2*time.Minute))
		insertPlatform("platform-completion-2", "completion-2", workboard.NextActionStatePass, now.Add(3*time.Minute))
		authoritative, err = repo.AuthoritativeDeliveryReviewRun(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if authoritative == nil || authoritative.ID != "platform-completion-2" {
			t.Fatalf("new completion authority = %+v, want new platform review awaiting human decision", authoritative)
		}
	})
}

func TestAuthoritativeDeliveryReviewFromRunsBreaksTimestampTiesByID(t *testing.T) {
	t.Parallel()
	createdAt := time.Unix(100, 0).UTC()
	got := authoritativeDeliveryReviewFromRuns([]workboard.GateRun{
		{
			ID: "run-a", State: workboard.NextActionStateFail,
			Executor: workboard.GateRunExecutorPlatform, CreatedAt: createdAt,
		},
		{
			ID: "run-z", State: workboard.NextActionStatePass,
			Executor: workboard.GateRunExecutorPlatform, CreatedAt: createdAt,
		},
	})
	if got == nil || got.ID != "run-z" {
		t.Fatalf("authoritative delivery run = %#v, want run-z", got)
	}
}

func TestAuthoritativeDeliveryReviewCycleBreaksTimestampTiesByID(t *testing.T) {
	t.Parallel()
	createdAt := time.Unix(100, 0).UTC()
	human := &workboard.GateRun{
		ID: "run-a", Executor: workboard.GateRunExecutorHuman,
		EvidenceJSON: `{"completion_feedback_event_id":"completion-a"}`, CreatedAt: createdAt,
	}
	platform := &workboard.GateRun{
		ID: "run-z", Executor: workboard.GateRunExecutorPlatform,
		EvidenceJSON: `{"completion_feedback_event_id":"completion-z"}`, CreatedAt: createdAt,
	}
	if got := authoritativeDeliveryReviewCycle(human, platform); got.ID != "run-z" {
		t.Fatalf("authoritative delivery cycle = %#v, want run-z", got)
	}
}

func TestWorkBoardRepository_DelayedOldReviewCannotStealLatestCompletionAuthority(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		now := time.Now().UTC().Truncate(time.Second)
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID: "cr-delayed-review", Key: "CR-DELAYED-REVIEW",
			WorkType: workboard.WorkTypeBugFix, Title: "Ignore delayed old review",
		})
		if err != nil {
			t.Fatal(err)
		}
		insertDeliveryCompletion(t, gdb, "ws-test", cr.ID, "completion-1", now)
		insertDeliveryCompletion(t, gdb, "ws-test", cr.ID, "completion-2", now.Add(time.Minute))
		for _, run := range []workboard.GateRun{
			{
				ID: "platform-completion-2", WorkspaceID: "ws-test",
				SubjectKind: workboard.GateRunSubjectChangeRequest, SubjectID: cr.ID,
				Gate: "delivery_review", State: workboard.NextActionStatePass,
				Executor:                  workboard.GateRunExecutorPlatform,
				EvidenceJSON:              `{"completion_feedback_event_id":"completion-2"}`,
				CompletionFeedbackEventID: "completion-2", CreatedAt: now.Add(2 * time.Minute),
			},
			{
				ID: "delayed-platform-completion-1", WorkspaceID: "ws-test",
				SubjectKind: workboard.GateRunSubjectChangeRequest, SubjectID: cr.ID,
				Gate: "delivery_review", State: workboard.NextActionStateFail,
				Executor:                  workboard.GateRunExecutorPlatform,
				EvidenceJSON:              `{"completion_feedback_event_id":"completion-1"}`,
				CompletionFeedbackEventID: "completion-1", CreatedAt: now.Add(3 * time.Minute),
			},
		} {
			if err := gdb.Create(&run).Error; err != nil {
				t.Fatal(err)
			}
		}

		authoritative, err := repo.AuthoritativeDeliveryReviewRun(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if authoritative == nil || authoritative.ID != "platform-completion-2" {
			t.Fatalf("authoritative = %+v, want latest completion review", authoritative)
		}
	})
}

func TestWorkBoardRepository_UnboundLegacyHumanDoesNotMaskCurrentPlatformReview(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		now := time.Now().UTC().Truncate(time.Second)
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID: "cr-legacy-human", Key: "CR-LEGACY-HUMAN",
			WorkType: workboard.WorkTypeBugFix, Title: "Do not wedge current review",
		})
		if err != nil {
			t.Fatal(err)
		}
		insertDeliveryCompletion(t, gdb, "ws-test", cr.ID, "completion-2", now)
		for _, run := range []workboard.GateRun{
			{
				ID: "legacy-human", WorkspaceID: "ws-test",
				SubjectKind: workboard.GateRunSubjectChangeRequest, SubjectID: cr.ID,
				Gate: "delivery_review", State: workboard.NextActionStatePass,
				Executor: workboard.GateRunExecutorHuman, EvidenceJSON: `{}`,
				CreatedAt: now.Add(2 * time.Minute),
			},
			{
				ID: "platform-completion-2", WorkspaceID: "ws-test",
				SubjectKind: workboard.GateRunSubjectChangeRequest, SubjectID: cr.ID,
				Gate: "delivery_review", State: workboard.NextActionStateNeedsHumanReview,
				Executor:                  workboard.GateRunExecutorPlatform,
				EvidenceJSON:              `{"completion_feedback_event_id":"completion-2"}`,
				CompletionFeedbackEventID: "completion-2", CreatedAt: now.Add(time.Minute),
			},
		} {
			if err := gdb.Create(&run).Error; err != nil {
				t.Fatal(err)
			}
		}

		authoritative, err := repo.AuthoritativeDeliveryReviewRun(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if authoritative == nil || authoritative.ID != "platform-completion-2" {
			t.Fatalf("authoritative = %+v, want current bound platform review", authoritative)
		}
	})
}

func TestWorkBoardRepository_CurrentGateRunsKeepsQualityGateBeyondDeliveryHistory(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		now := time.Now().UTC().Truncate(time.Second)
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID: "cr-current-gates", Key: "CR-CURRENT-GATES",
			WorkType: workboard.WorkTypeBugFix, Title: "Keep unresolved gate",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := gdb.Create(&workboard.GateRun{
			ID: "quality-fail", WorkspaceID: "ws-test",
			SubjectKind: workboard.GateRunSubjectChangeRequest, SubjectID: cr.ID,
			Gate: "test_coverage", State: workboard.NextActionStateFail,
			Executor: workboard.GateRunExecutorPlatform, CreatedAt: now,
		}).Error; err != nil {
			t.Fatal(err)
		}
		for i := range 60 {
			if err := gdb.Create(&workboard.GateRun{
				ID: fmt.Sprintf("delivery-%02d", i), WorkspaceID: "ws-test",
				SubjectKind: workboard.GateRunSubjectChangeRequest, SubjectID: cr.ID,
				Gate: "delivery_review", State: workboard.NextActionStatePass,
				Executor:  workboard.GateRunExecutorPlatform,
				CreatedAt: now.Add(time.Duration(i+1) * time.Minute),
			}).Error; err != nil {
				t.Fatal(err)
			}
		}

		runs, err := repo.CurrentGateRuns(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		foundQuality := false
		for _, run := range runs {
			foundQuality = foundQuality || run.ID == "quality-fail"
		}
		if !foundQuality {
			t.Fatalf("current runs = %+v, want unresolved quality gate", runs)
		}
	})
}

func TestWorkBoardRepository_HumanDeliveryDecisionOutranksLaterPlatformRun(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		now := time.Now().UTC().Truncate(time.Second)
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:        "cr-human-precedence",
			Key:       "CR-HUMAN-PRECEDENCE",
			WorkType:  workboard.WorkTypeBugFix,
			Title:     "Human precedence",
			CreatedAt: now,
			UpdatedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		insertDeliveryCompletion(t, gdb, "ws-test", cr.ID, "completion-1", now.Add(-time.Minute))
		if err := gdb.Create(&workboard.GateRun{
			ID: "run-platform-pass", WorkspaceID: "ws-test",
			SubjectKind: workboard.GateRunSubjectChangeRequest, SubjectID: cr.ID,
			Gate: "delivery_review", State: workboard.NextActionStatePass,
			Executor:                  workboard.GateRunExecutorPlatform,
			EvidenceJSON:              `{"completion_feedback_event_id":"completion-1"}`,
			CompletionFeedbackEventID: "completion-1", CreatedAt: now,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := repo.RecordDeliveryDecision(ctx, workboard.DeliveryDecisionInput{
			ChangeRequestID: cr.ID, ReviewedGateRunID: "run-platform-pass",
			CompletionFeedbackEventID: "completion-1",
			Decision:                  workboard.DeliveryDecisionApprove,
			Actor:                     "lead@example.com",
			Note:                      "Human cleared the false failure.",
		}); err != nil {
			t.Fatalf("RecordDeliveryDecision: %v", err)
		}
		if err := gdb.Create(&workboard.GateRun{
			ID:                        "run-later-platform-fail",
			WorkspaceID:               "ws-test",
			SubjectKind:               workboard.GateRunSubjectChangeRequest,
			SubjectID:                 cr.ID,
			Gate:                      "delivery_review",
			State:                     workboard.NextActionStateFail,
			Hint:                      "platform rerun still disagrees",
			Executor:                  workboard.GateRunExecutorPlatform,
			EvidenceJSON:              `{"verdict":"fail"}`,
			CompletionFeedbackEventID: "completion-1",
			CreatedAt:                 now.Add(time.Hour),
		}).Error; err != nil {
			t.Fatal(err)
		}

		reloaded, err := repo.GetChangeRequest(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if reloaded.Phase != workboard.BoardPhaseDelivered {
			t.Fatalf("phase = %q, want Delivered from human decision despite later platform run", reloaded.Phase)
		}
		if reloaded.DeliveryReview == nil {
			t.Fatal("missing delivery review snapshot")
		}
		if reloaded.DeliveryReview.Verdict != string(workboard.NextActionStatePass) ||
			reloaded.DeliveryReview.Executor != workboard.GateRunExecutorHuman ||
			reloaded.DeliveryReview.Actor != "lead@example.com" ||
			!strings.Contains(reloaded.DeliveryReview.Summary, "delivery accepted by lead") {
			t.Fatalf("delivery review snapshot = %+v, want human pass with override summary", reloaded.DeliveryReview)
		}
	})
}

func TestWorkBoardRepository_CanonicalPromotionIncrementsOnlyOnReplacement(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		artifactRepo := NewRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		now := time.Now().UTC().Truncate(time.Second)

		oldArtifact := newArtifact("art-old", "FEAT-CANON", "v0.1", artifact.StatusApproved, now)
		oldArtifact.ApprovedBy = "pm"
		oldArtifact.ApprovedAt = &now
		newArtifactRow := newArtifact("art-new", "FEAT-CANON", "v0.2", artifact.StatusApproved, now)
		newArtifactRow.ApprovedBy = "pm"
		newArtifactRow.ApprovedAt = &now
		if err := artifactRepo.Insert(ctx, oldArtifact); err != nil {
			t.Fatal(err)
		}
		if err := artifactRepo.Insert(ctx, newArtifactRow); err != nil {
			t.Fatal(err)
		}
		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:                  "feature-canon",
			Key:                 "FEAT-CANON",
			Name:                "Canonical",
			Status:              workboard.FeatureStatusPlanned,
			Version:             1,
			CanonicalArtifactID: "art-old",
			CreatedAt:           now,
			UpdatedAt:           now,
		})
		if err != nil {
			t.Fatal(err)
		}
		updated, err := repo.SetFeatureCanonicalArtifact(ctx, feature.ID, "art-new", "pm")
		if err != nil {
			t.Fatal(err)
		}
		if updated.CanonicalArtifactID != "art-new" || updated.Version != 2 {
			t.Fatalf("feature after promotion = %+v", updated)
		}

		unchanged, err := repo.SetFeatureCanonicalArtifact(ctx, updated.ID, "art-new", "pm")
		if err != nil {
			t.Fatal(err)
		}
		if unchanged.Version != 2 {
			t.Fatalf("same canonical should not increment version, got %d", unchanged.Version)
		}
		// Promotion regression guard (per FINDINGS-r6 / Bug #6): once a
		// feature gets an approved canonical artifact, planned/candidate
		// features must transition to active.
		if updated.Status != workboard.FeatureStatusActive {
			t.Fatalf("feature should be active after canonical promotion, got %q", updated.Status)
		}
	})
}

func TestWorkBoardRepository_CanonicalPromotionPromotesPlannedFeatureToActive(t *testing.T) {
	// Direct SetFeatureCanonicalArtifact path (bypassing CR promotion):
	// planned/candidate features must transition to active when first
	// approved canonical artifact is set; rejected/deprecated stay put.
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		artifactRepo := NewRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		now := time.Now().UTC().Truncate(time.Second)

		cases := []struct {
			name     string
			start    workboard.FeatureStatus
			expected workboard.FeatureStatus
		}{
			{"candidate_promotes", workboard.FeatureStatusCandidate, workboard.FeatureStatusActive},
			{"planned_promotes", workboard.FeatureStatusPlanned, workboard.FeatureStatusActive},
			{"active_stays", workboard.FeatureStatusActive, workboard.FeatureStatusActive},
			{"deprecated_stays", workboard.FeatureStatusDeprecated, workboard.FeatureStatusDeprecated},
			{"rejected_stays", workboard.FeatureStatusRejected, workboard.FeatureStatusRejected},
		}
		for i, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				artID := fmt.Sprintf("art-promote-%d", i)
				featureID := fmt.Sprintf("feat-promote-%d", i)
				featureKey := fmt.Sprintf("FEAT-PROMOTE-%d", i)
				art := newArtifact(artID, featureKey, "v0.1", artifact.StatusApproved, now)
				art.ApprovedBy = "pm"
				art.ApprovedAt = &now
				if err := artifactRepo.Insert(ctx, art); err != nil {
					t.Fatal(err)
				}
				feature, err := repo.CreateFeature(ctx, workboard.Feature{
					ID:        featureID,
					Key:       featureKey,
					Name:      "Promote " + tc.name,
					Status:    tc.start,
					Version:   1,
					CreatedAt: now,
					UpdatedAt: now,
				})
				if err != nil {
					t.Fatal(err)
				}
				updated, err := repo.SetFeatureCanonicalArtifact(ctx, feature.ID, artID, "pm")
				if err != nil {
					t.Fatal(err)
				}
				if updated.Status != tc.expected {
					t.Fatalf("status after canonical: got %q, want %q", updated.Status, tc.expected)
				}
			})
		}
	})
}

func TestWorkBoardRepository_SetFeatureCanonicalArtifactRejectsUnapproved(t *testing.T) {
	// Promotion of a non-approved artifact must fail as a validation error
	// (mapped to HTTP 400), not a generic 500.
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		artifactRepo := NewRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		now := time.Now().UTC().Truncate(time.Second)

		draft := newArtifact("art-draft", "FEAT-UNAPPROVED", "v0.1", artifact.StatusDraft, now)
		if err := artifactRepo.Insert(ctx, draft); err != nil {
			t.Fatal(err)
		}
		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:     "feat-unapproved",
			Key:    "FEAT-UNAPPROVED",
			Name:   "Unapproved",
			Status: workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = repo.SetFeatureCanonicalArtifact(ctx, feature.ID, "art-draft", "pm")
		if !errors.Is(err, workboard.ErrValidation) {
			t.Fatalf("expected ErrValidation for non-approved artifact, got %v", err)
		}
	})
}

func TestWorkBoardRepository_SetFeatureCanonicalArtifactResolvesFeatureKey(t *testing.T) {
	// The promotion endpoint passes the approved artifact's feature_id, which
	// published feature-backed artifacts set to the feature KEY (not the UUID).
	// SetFeatureCanonicalArtifact must resolve either.
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		artifactRepo := NewRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		now := time.Now().UTC().Truncate(time.Second)

		art := newArtifact("art-approved", "FEAT-BY-KEY", "v1.0", artifact.StatusApproved, now)
		art.ApprovedBy = "pm"
		art.ApprovedAt = &now
		if err := artifactRepo.Insert(ctx, art); err != nil {
			t.Fatal(err)
		}
		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:     "feat-by-key-uuid",
			Key:    "FEAT-BY-KEY",
			Name:   "By Key",
			Status: workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}
		// Pass the KEY (the artifact's feature_id), not the UUID.
		updated, err := repo.SetFeatureCanonicalArtifact(ctx, feature.Key, "art-approved", "pm")
		if err != nil {
			t.Fatalf("promote by key: %v", err)
		}
		if updated.ID != feature.ID || updated.CanonicalArtifactID != "art-approved" {
			t.Fatalf("promote by key resolved wrong feature or canonical: %+v", updated)
		}
		if updated.Status != workboard.FeatureStatusActive {
			t.Fatalf("planned feature should be active after promotion, got %q", updated.Status)
		}
	})
}

func TestWorkBoardRepository_FeatureArchivedStatus(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")

		created, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:     "feature-to-archive",
			Key:    "ARCHIVE-1",
			Name:   "Retired capability",
			Status: workboard.FeatureStatusActive,
		})
		if err != nil {
			t.Fatal(err)
		}

		updated, err := repo.UpdateFeature(ctx, workboard.Feature{
			ID:     created.ID,
			Status: workboard.FeatureStatusArchived,
		})
		if err != nil {
			t.Fatalf("UpdateFeature to archived: %v", err)
		}
		if updated.Status != workboard.FeatureStatusArchived {
			t.Fatalf("status = %q, want archived", updated.Status)
		}

		reloaded, err := repo.GetFeature(ctx, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if reloaded.Status != workboard.FeatureStatusArchived {
			t.Fatalf("reloaded status = %q, want archived", reloaded.Status)
		}
	})
}

func TestWorkBoardRepository_PurgeArchivedChangeRequests(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")

		archived, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:       "cr-archived",
			Key:      "CR-ARCHIVED-1",
			WorkType: workboard.WorkTypeBugFix,
			Title:    "Old finished work",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.RefreshGateRuns(ctx, workboard.RefreshGateRunsInput{
			ChangeRequestID: archived.ID,
			Evaluations: []workboard.GateEvaluation{{
				Gate:  "scope_clear",
				State: workboard.NextActionStatePass,
				Hint:  "done",
			}},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.UpdateChangeRequest(ctx, workboard.ChangeRequest{
			ID:       archived.ID,
			Archived: true,
		}); err != nil {
			t.Fatal(err)
		}

		active, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:       "cr-active",
			Key:      "CR-ACTIVE-1",
			WorkType: workboard.WorkTypeBugFix,
			Title:    "Live work",
		})
		if err != nil {
			t.Fatal(err)
		}

		purged, err := repo.PurgeArchivedChangeRequests(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if purged != 1 {
			t.Fatalf("purged = %d, want 1", purged)
		}
		if _, err := repo.GetChangeRequest(ctx, archived.ID); err == nil {
			t.Fatal("archived change request should be deleted")
		}
		if _, err := repo.GetChangeRequest(ctx, active.ID); err != nil {
			t.Fatalf("active change request must survive: %v", err)
		}
		runs, err := repo.ListGateRuns(ctx, archived.ID, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) != 0 {
			t.Fatalf("gate runs of purged CR should be gone, got %+v", runs)
		}

		again, err := repo.PurgeArchivedChangeRequests(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if again != 0 {
			t.Fatalf("second purge should delete nothing, got %d", again)
		}
	})
}

func TestWorkBoardRepository_PurgeArchivedChangeRequestsScopesWorkspace(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := context.Background()
		for _, cr := range []workboard.ChangeRequest{
			{ID: "cr-archived-a", Key: "CR-ARCHIVED-A", WorkspaceID: "ws-a", WorkType: workboard.WorkTypeCleanup, Title: "A", Archived: true},
			{ID: "cr-archived-b", Key: "CR-ARCHIVED-B", WorkspaceID: "ws-b", WorkType: workboard.WorkTypeCleanup, Title: "B", Archived: true},
		} {
			if _, err := repo.CreateChangeRequest(ctx, cr); err != nil {
				t.Fatal(err)
			}
		}
		now := time.Now().UTC()
		if err := gdb.Create(&integrations.Integration{ID: "integration-b", WorkspaceID: "ws-b", Provider: integrations.ProviderGitLab, Name: "B", Status: integrations.StatusConnected, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
		if err := gdb.Create(&integrations.Resource{ID: "resource-b", IntegrationID: "integration-b", ResourceType: integrations.ResourceTypeProject, ExternalKey: "b/project", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
		// The CR id is intentionally referenced by a foreign integration row to
		// ensure a workspace-scoped purge cannot delete another workspace's data.
		for _, link := range []any{
			&integrations.TrackerLink{ID: "tracker-b", IntegrationID: "integration-b", ResourceID: "resource-b", ChangeRequestID: "cr-archived-a", ExternalKey: "B-1", State: integrations.TrackerStateOpened, CreatedAt: now, UpdatedAt: now},
			&integrations.DeliveryLink{ID: "delivery-b", IntegrationID: "integration-b", ResourceID: "resource-b", ChangeRequestID: "cr-archived-a", ExternalType: integrations.ExternalTypeMergeRequest, ExternalIID: "1", State: integrations.DeliveryStateOpened, CreatedAt: now, UpdatedAt: now},
		} {
			if err := gdb.Create(link).Error; err != nil {
				t.Fatal(err)
			}
		}

		purged, err := repo.PurgeArchivedChangeRequests(workboard.WithWorkspace(ctx, "ws-a"))
		if err != nil {
			t.Fatal(err)
		}
		if purged != 1 {
			t.Fatalf("purged = %d, want 1", purged)
		}
		if _, err := repo.GetChangeRequest(ctx, "cr-archived-a"); err == nil {
			t.Fatal("workspace A archived request should be deleted")
		}
		if _, err := repo.GetChangeRequest(ctx, "cr-archived-b"); err != nil {
			t.Fatalf("workspace B archived request must survive: %v", err)
		}
		for table, id := range map[string]string{"tracker_links": "tracker-b", "integration_delivery_links": "delivery-b"} {
			var count int64
			if err := gdb.Table(table).Where("id = ?", id).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("foreign %s row was deleted by ws-a purge", table)
			}
		}
	})
}

func TestWorkBoardRepository_PurgeArchivedChangeRequestsRequiresWorkspace(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		if _, err := repo.PurgeArchivedChangeRequests(context.Background()); !errors.Is(err, workboard.ErrWorkspaceRequired) {
			t.Fatalf("PurgeArchivedChangeRequests error = %v, want ErrWorkspaceRequired", err)
		}
	})
}

func TestWorkBoardRepository_ListReferencedArtifactIDs(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")

		if _, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:     "feature-ref",
			Key:    "REF-1",
			Name:   "Referenced feature",
			Status: workboard.FeatureStatusActive,
		}); err != nil {
			t.Fatal(err)
		}
		if err := gdb.Exec("UPDATE features SET canonical_artifact_id = 'art-canonical' WHERE id = 'feature-ref'").Error; err != nil {
			t.Fatal(err)
		}
		if _, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:       "cr-ref",
			Key:      "CR-REF-1",
			WorkType: workboard.WorkTypeBugFix,
			Title:    "Referenced CR",
		}); err != nil {
			t.Fatal(err)
		}
		if err := gdb.Exec("UPDATE change_requests SET lead_artifact_id = 'art-lead' WHERE id = 'cr-ref'").Error; err != nil {
			t.Fatal(err)
		}

		referenced, err := repo.ListReferencedArtifactIDs(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"art-canonical", "art-lead"} {
			if !referenced[want] {
				t.Fatalf("referenced missing %q: %+v", want, referenced)
			}
		}
		if referenced[""] {
			t.Fatalf("referenced must not contain empty id: %+v", referenced)
		}
	})
}

func TestWorkBoardRepository_StaleWarnings(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		now := time.Now().UTC().Truncate(time.Second)

		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:        "feature-warn",
			Key:       "FEAT-WARN",
			Name:      "Warn",
			Status:    workboard.FeatureStatusPlanned,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		warnings, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{FeatureID: feature.ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(warnings) != 1 || warnings[0].Code != workboard.WarningCanonicalArtifactMissing {
			t.Fatalf("warnings = %+v", warnings)
		}

		allWarnings, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(allWarnings) != 1 || allWarnings[0].FeatureID != feature.ID {
			t.Fatalf("system-wide warnings = %+v", allWarnings)
		}

		// Quick-route change requests have no feature; the system-wide listing
		// must evaluate their CR-scoped warnings instead of failing on the
		// missing feature row.
		if _, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:        "cr-featureless",
			Key:       "CR-FEATURELESS",
			Title:     "Quick item without feature",
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		afterQuick, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{})
		if err != nil {
			t.Fatalf("system-wide warnings with feature-less CR: %v", err)
		}
		if len(afterQuick) != 1 {
			t.Fatalf("feature-less CR changed warnings = %+v", afterQuick)
		}
	})
}

func TestWorkBoardRepository_NextActionsExposeChangeRequestGates(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		artifactRepo := NewRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		now := time.Now().UTC().Truncate(time.Second)

		approvedAt := now.Add(-1 * time.Hour)
		lead := newArtifact("art-next-lead", "feature-next", "v0.2", artifact.StatusApproved, approvedAt)
		lead.ApprovedBy = "pm"
		lead.ApprovedAt = &approvedAt
		if err := artifactRepo.Insert(ctx, lead); err != nil {
			t.Fatal(err)
		}
		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:                  "feature-next",
			Key:                 "FEAT-NEXT",
			Name:                "Next actions",
			Status:              workboard.FeatureStatusPlanned,
			Version:             1,
			CanonicalArtifactID: "art-older-canon",
			CreatedAt:           now,
			UpdatedAt:           now,
		})
		if err != nil {
			t.Fatal(err)
		}
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:             "cr-next",
			Key:            "CR-NEXT",
			FeatureID:      feature.ID,
			WorkType:       workboard.WorkTypeFeatureChange,
			Title:          "Refresh next actions",
			LeadArtifactID: lead.ID,
			CreatedAt:      now,
			UpdatedAt:      now,
		})
		if err != nil {
			t.Fatal(err)
		}

		actions, err := repo.NextActions(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		byGate := map[string]workboard.NextAction{}
		for _, action := range actions {
			byGate[action.Gate] = action
		}
		if byGate["spec_drafted"].State != workboard.NextActionStatePass {
			t.Fatalf("spec_drafted action = %+v", byGate["spec_drafted"])
		}
		if byGate["canonical_spec"].State != workboard.NextActionStateWarn {
			t.Fatalf("canonical_spec action = %+v", byGate["canonical_spec"])
		}
	})
}

func TestWorkBoardRepository_RefreshAndListGateRuns(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		now := time.Now().UTC()
		if err := gdb.Create(&artifact.Artifact{
			ID:          "art-canonical",
			WorkspaceID: "ws-test",
			FeatureID:   "feat-gate",
			Version:     "v1.0",
			Status:      artifact.StatusApproved,
			RequestType: artifact.RequestTypeNewFeature,
			ImpactLevel: artifact.ImpactLevelMedium,
			CreatedBy:   "tester",
			CreatedAt:   now,
			UpdatedAt:   now,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := gdb.Create(&artifact.Artifact{
			ID:          "art-lead",
			WorkspaceID: "ws-test",
			FeatureID:   "feat-gate",
			Version:     "v1.1",
			Status:      artifact.StatusApproved,
			RequestType: artifact.RequestTypeChangeRequest,
			ImpactLevel: artifact.ImpactLevelMedium,
			CreatedBy:   "tester",
			CreatedAt:   now,
			UpdatedAt:   now,
		}).Error; err != nil {
			t.Fatal(err)
		}
		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:                  "feat-gate",
			Key:                 "FEAT-GATE",
			Name:                "Gate feature",
			Status:              workboard.FeatureStatusPlanned,
			CanonicalArtifactID: "art-canonical",
		})
		if err != nil {
			t.Fatal(err)
		}
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:             "cr-gate",
			Key:            "CR-GATE",
			FeatureID:      feature.ID,
			WorkType:       workboard.WorkTypeFeatureChange,
			Title:          "Refresh gates",
			LeadArtifactID: "art-lead",
		})
		if err != nil {
			t.Fatal(err)
		}
		rows, err := repo.RefreshGateRuns(ctx, workboard.RefreshGateRunsInput{ChangeRequestID: cr.ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			t.Fatal("expected gate run rows")
		}
		foundCanonicalSpec := false
		for _, row := range rows {
			if row.Gate == "canonical_spec" {
				foundCanonicalSpec = true
				if !strings.Contains(row.EvidenceJSON, `"source_artifact_id":"art-lead"`) {
					t.Fatalf("expected evidence source artifact in %s", row.EvidenceJSON)
				}
				if !strings.Contains(row.EvidenceJSON, `"evidence_contract_version":"gate-run-v1"`) {
					t.Fatalf("expected evidence contract version in %s", row.EvidenceJSON)
				}
				if !strings.Contains(row.EvidenceJSON, `"config_version":"workboard-next-actions-v1"`) {
					t.Fatalf("expected evaluator config version in %s", row.EvidenceJSON)
				}
				if !strings.Contains(row.EvidenceJSON, `"judge_model":"deterministic-v1"`) {
					t.Fatalf("expected judge model in %s", row.EvidenceJSON)
				}
				if !strings.Contains(row.EvidenceJSON, `"verdict":"warn"`) {
					t.Fatalf("expected verdict in %s", row.EvidenceJSON)
				}
				if !strings.Contains(row.EvidenceJSON, `"confidence":0.75`) {
					t.Fatalf("expected confidence in %s", row.EvidenceJSON)
				}
				break
			}
		}
		if !foundCanonicalSpec {
			t.Fatalf("expected canonical_spec warn row, got %+v", rows)
		}
		evaluatedRows, err := repo.RefreshGateRuns(ctx, workboard.RefreshGateRunsInput{
			ChangeRequestID: cr.ID,
			Evaluations: []workboard.GateEvaluation{
				{
					Gate:             "canonical_spec",
					State:            workboard.NextActionStateNeedsHumanReview,
					Hint:             "Judge confidence below threshold",
					Confidence:       0.42,
					JudgeModel:       "gpt-5-mini",
					EvalSuiteVersion: "gate-calibration-2026-05-31",
					Evidence:         "spec §3 lacks a rollback trigger",
				},
				{
					// An eval-only gate has no deterministic next-action — it must
					// still persist a row straight from the evaluation.
					Gate:       "rollback_plan_present",
					State:      workboard.NextActionStateFail,
					Hint:       "No rollback described",
					Confidence: 0.88,
					JudgeModel: "gpt-5-mini",
					Evidence:   "no backout procedure in the spec",
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		var llmRow *workboard.GateRun
		for i := range evaluatedRows {
			if evaluatedRows[i].Gate == "rollback_plan_present" {
				llmRow = &evaluatedRows[i]
				break
			}
		}
		if llmRow == nil {
			t.Fatalf("expected an eval-only rollback_plan_present row in %+v", evaluatedRows)
		}
		if llmRow.State != workboard.NextActionStateFail {
			t.Fatalf("eval-only gate state = %s, want fail", llmRow.State)
		}
		if !strings.Contains(llmRow.EvidenceJSON, `"evidence":"no backout procedure in the spec"`) {
			t.Fatalf("expected eval-only evidence in %s", llmRow.EvidenceJSON)
		}
		foundEvaluated := false
		for _, row := range evaluatedRows {
			if row.Gate != "canonical_spec" {
				continue
			}
			if row.State != workboard.NextActionStateNeedsHumanReview {
				t.Fatalf("expected evaluated state, got %s", row.State)
			}
			if !strings.Contains(row.EvidenceJSON, `"judge_model":"gpt-5-mini"`) {
				t.Fatalf("expected judge model in %s", row.EvidenceJSON)
			}
			if !strings.Contains(row.EvidenceJSON, `"evidence":"spec §3 lacks a rollback trigger"`) {
				t.Fatalf("expected judge evidence quote in %s", row.EvidenceJSON)
			}
			foundEvaluated = true
			break
		}
		if !foundEvaluated {
			t.Fatalf("expected evaluated canonical_spec row in %+v", evaluatedRows)
		}
		evaluationOnlyRows, err := repo.RefreshGateRuns(ctx, workboard.RefreshGateRunsInput{
			ChangeRequestID: cr.ID,
			EvaluationsOnly: true,
			Evaluations: []workboard.GateEvaluation{{
				Gate:             "delivery_review",
				State:            workboard.NextActionStatePass,
				Hint:             "Ready for human review",
				Confidence:       1,
				JudgeModel:       "deterministic_checks",
				EvalSuiteVersion: "delivery-review-v1",
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(evaluationOnlyRows) != 1 || evaluationOnlyRows[0].Gate != "delivery_review" {
			t.Fatalf("evaluation-only refresh persisted unrelated gates: %+v", evaluationOnlyRows)
		}
		if _, err := repo.RefreshGateRuns(ctx, workboard.RefreshGateRunsInput{
			ChangeRequestID: cr.ID,
			Evaluations: []workboard.GateEvaluation{
				{Gate: "canonical_spec", Confidence: 1.2},
			},
		}); !errors.Is(err, workboard.ErrValidation) {
			t.Fatalf("expected validation error for invalid confidence, got %v", err)
		}
		listed, err := repo.ListGateRuns(ctx, cr.ID, 10)
		if err != nil {
			t.Fatal(err)
		}
		expectedListed := len(rows) + len(evaluatedRows)
		if expectedListed > 10 {
			expectedListed = 10
		}
		if len(listed) != expectedListed {
			t.Fatalf("listed=%d want=%d", len(listed), expectedListed)
		}
	})
}

func TestWorkBoardRepository_PersistsPolicyGuardWithDanglingFeature(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:       "cr-policy-dangling-feature",
			Key:      "CR-POLICY-DANGLING",
			WorkType: workboard.WorkTypeNewFeature,
			Title:    "Persist policy guard",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := gdb.Model(&workboard.ChangeRequest{}).
			Where("id = ?", cr.ID).
			Update("feature_id", "feature-missing").Error; err != nil {
			t.Fatal(err)
		}

		rows, err := repo.RefreshGateRuns(ctx, workboard.RefreshGateRunsInput{
			ChangeRequestID: cr.ID,
			Evaluations: []workboard.GateEvaluation{{
				Gate:             "delivery_review",
				State:            workboard.NextActionStateNeedsHumanReview,
				Hint:             "Governing delivery policy is unavailable.",
				Confidence:       1,
				JudgeModel:       "deterministic_policy_guard",
				EvalSuiteVersion: "delivery-review-v1",
				Evidence:         `{"reason_code":"policy_unavailable","criteria":[],"checks":[]}`,
			}},
		})
		if err != nil {
			t.Fatalf("RefreshGateRuns: %v", err)
		}
		if len(rows) != 1 || rows[0].Gate != "delivery_review" ||
			rows[0].State != workboard.NextActionStateNeedsHumanReview {
			t.Fatalf("rows = %+v, want persisted policy guard", rows)
		}
		persisted, err := repo.ListGateRuns(ctx, cr.ID, 10)
		if err != nil {
			t.Fatal(err)
		}
		var wrapper struct {
			Evidence string `json:"evidence"`
		}
		if len(persisted) == 1 {
			_ = json.Unmarshal([]byte(persisted[0].EvidenceJSON), &wrapper)
		}
		if len(persisted) != 1 ||
			!strings.Contains(wrapper.Evidence, `"reason_code":"policy_unavailable"`) {
			t.Fatalf("persisted = %+v, want policy_unavailable evidence", persisted)
		}
	})
}

func TestWorkBoardRepository_StaleWarningsLinkedKnowledgeScopedByWorkspace(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		artifactRepo := NewRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		approvedAt := now.Add(-2 * time.Hour)
		canonical := newArtifact("art-knowledge-scoped-canon", "FEAT-KNOW-SCOPED", "v1.0", artifact.StatusApproved, approvedAt)
		canonical.WorkspaceID = "ws-a"
		canonical.ApprovedBy = "pm"
		canonical.ApprovedAt = &approvedAt
		if err := artifactRepo.Insert(ctx, canonical); err != nil {
			t.Fatal(err)
		}
		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:                  "feature-knowledge-scoped",
			WorkspaceID:         "ws-a",
			Key:                 "FEAT-KNOW-SCOPED",
			Name:                "Knowledge-scoped feature",
			Status:              workboard.FeatureStatusPlanned,
			Version:             1,
			CanonicalArtifactID: canonical.ID,
			CreatedAt:           now,
			UpdatedAt:           now,
		})
		if err != nil {
			t.Fatal(err)
		}
		// Newer indexed knowledge, but in another workspace — must be ignored.
		if err := gdb.WithContext(ctx).Create(&knowledge.Document{
			DocumentID:      "doc-linked-knowledge-other-workspace",
			Version:         "v2",
			WorkspaceID:     "ws-b",
			IsLatest:        true,
			Title:           "Other workspace update",
			DocumentType:    knowledge.DocumentTypeProductBrief,
			AuthorityLevel:  knowledge.AuthorityHigh,
			SourceKind:      knowledge.SourceKindText,
			Status:          knowledge.StatusIndexed,
			LinkedFeatureID: feature.Key,
			CreatedAt:       now.Add(-30 * time.Minute),
			UpdatedAt:       now.Add(-30 * time.Minute),
		}).Error; err != nil {
			t.Fatal(err)
		}

		warnings, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{FeatureID: feature.ID, WorkspaceID: "ws-a"})
		if err != nil {
			t.Fatal(err)
		}
		if len(warnings) != 0 {
			t.Fatalf("warnings = %+v, want none for ws-a", warnings)
		}

		// Same-workspace newer indexed knowledge — must raise the warning.
		if err := gdb.WithContext(ctx).Create(&knowledge.Document{
			DocumentID:      "doc-linked-knowledge-current-workspace",
			Version:         "v2",
			WorkspaceID:     "ws-a",
			IsLatest:        true,
			Title:           "Current workspace update",
			DocumentType:    knowledge.DocumentTypeProductBrief,
			AuthorityLevel:  knowledge.AuthorityHigh,
			SourceKind:      knowledge.SourceKindText,
			Status:          knowledge.StatusIndexed,
			LinkedFeatureID: feature.Key,
			CreatedAt:       now.Add(-20 * time.Minute),
			UpdatedAt:       now.Add(-20 * time.Minute),
		}).Error; err != nil {
			t.Fatal(err)
		}

		warnings, err = repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{FeatureID: feature.ID, WorkspaceID: "ws-a"})
		if err != nil {
			t.Fatal(err)
		}
		if len(warnings) != 1 || warnings[0].Code != workboard.WarningLinkedKnowledgeNewer {
			t.Fatalf("warnings = %+v, want linked_knowledge_newer", warnings)
		}
	})
}

func TestWorkBoardRepository_StaleWarningsLinkedKnowledgeClearsAfterCanonicalUpdate(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		artifactRepo := NewRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		approvedAt := now.Add(-10 * time.Minute)
		canonical := newArtifact("art-knowledge-clear-canon", "FEAT-KNOW-CLEAR", "v1.1", artifact.StatusApproved, approvedAt)
		canonical.WorkspaceID = "ws-a"
		canonical.ApprovedBy = "pm"
		canonical.ApprovedAt = &approvedAt
		if err := artifactRepo.Insert(ctx, canonical); err != nil {
			t.Fatal(err)
		}
		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:                  "feature-knowledge-clear",
			WorkspaceID:         "ws-a",
			Key:                 "FEAT-KNOW-CLEAR",
			Name:                "Knowledge clear feature",
			Status:              workboard.FeatureStatusPlanned,
			Version:             1,
			CanonicalArtifactID: canonical.ID,
			CreatedAt:           now,
			UpdatedAt:           now,
		})
		if err != nil {
			t.Fatal(err)
		}
		// Knowledge update predates the canonical approval — already incorporated,
		// so no warning (the warning cleared after re-approval caught up).
		if err := gdb.WithContext(ctx).Create(&knowledge.Document{
			DocumentID:      "doc-linked-knowledge-before-canonical",
			Version:         "v2",
			WorkspaceID:     "ws-a",
			IsLatest:        true,
			Title:           "Already incorporated update",
			DocumentType:    knowledge.DocumentTypeProductBrief,
			AuthorityLevel:  knowledge.AuthorityHigh,
			SourceKind:      knowledge.SourceKindText,
			Status:          knowledge.StatusIndexed,
			LinkedFeatureID: feature.Key,
			CreatedAt:       now.Add(-30 * time.Minute),
			UpdatedAt:       now.Add(-30 * time.Minute),
		}).Error; err != nil {
			t.Fatal(err)
		}

		warnings, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{FeatureID: feature.ID, WorkspaceID: "ws-a"})
		if err != nil {
			t.Fatal(err)
		}
		if len(warnings) != 0 {
			t.Fatalf("warnings = %+v, want cleared after canonical approval", warnings)
		}
	})
}

func TestWorkBoardRepository_StaleWarningsIncludeNewerLinkedKnowledge(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		artifactRepo := NewRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		approvedAt := now.Add(-2 * time.Hour)
		canonical := newArtifact("art-knowledge-canon", "FEAT-KNOW", "v1.0", artifact.StatusApproved, approvedAt)
		canonical.WorkspaceID = "ws-a"
		canonical.ApprovedBy = "pm"
		canonical.ApprovedAt = &approvedAt
		if err := artifactRepo.Insert(ctx, canonical); err != nil {
			t.Fatal(err)
		}
		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:                  "feature-knowledge",
			WorkspaceID:         "ws-a",
			Key:                 "FEAT-KNOW",
			Name:                "Knowledge-linked feature",
			Status:              workboard.FeatureStatusPlanned,
			Version:             1,
			CanonicalArtifactID: canonical.ID,
			CreatedAt:           now,
			UpdatedAt:           now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := gdb.WithContext(ctx).Create(&knowledge.Document{
			DocumentID:      "doc-linked-knowledge",
			Version:         "v2",
			WorkspaceID:     "ws-a",
			IsLatest:        true,
			Title:           "Updated checkout research",
			DocumentType:    knowledge.DocumentTypeProductBrief,
			AuthorityLevel:  knowledge.AuthorityHigh,
			SourceKind:      knowledge.SourceKindText,
			Status:          knowledge.StatusIndexed,
			LinkedFeatureID: feature.Key,
			CreatedAt:       now.Add(-30 * time.Minute),
			UpdatedAt:       now.Add(-30 * time.Minute),
		}).Error; err != nil {
			t.Fatal(err)
		}

		warnings, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{FeatureID: feature.ID, WorkspaceID: "ws-a"})
		if err != nil {
			t.Fatal(err)
		}
		if len(warnings) != 1 || warnings[0].Code != workboard.WarningLinkedKnowledgeNewer {
			t.Fatalf("warnings = %+v", warnings)
		}
		if warnings[0].ArtifactID != canonical.ID {
			t.Fatalf("warning artifact id = %q, want %q", warnings[0].ArtifactID, canonical.ID)
		}
	})
}

func TestWorkBoardRepository_StaleWarningsIgnoreLinkedKnowledgeThatIsNotNewerOrIndexed(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		artifactRepo := NewRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		tieApprovedAt := now.Add(-1 * time.Hour)
		tieCanonical := newArtifact("art-knowledge-tie", "FEAT-KNOW-TIE", "v1.0", artifact.StatusApproved, tieApprovedAt)
		tieCanonical.WorkspaceID = "ws-a"
		tieCanonical.ApprovedBy = "pm"
		tieCanonical.ApprovedAt = &tieApprovedAt
		if err := artifactRepo.Insert(ctx, tieCanonical); err != nil {
			t.Fatal(err)
		}
		tieFeature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:                  "feature-knowledge-tie",
			WorkspaceID:         "ws-a",
			Key:                 "FEAT-KNOW-TIE",
			Name:                "Knowledge tie feature",
			Status:              workboard.FeatureStatusPlanned,
			Version:             1,
			CanonicalArtifactID: tieCanonical.ID,
			CreatedAt:           now,
			UpdatedAt:           now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := gdb.WithContext(ctx).Create(&knowledge.Document{
			DocumentID:      "doc-linked-knowledge-tie",
			Version:         "v1",
			WorkspaceID:     "ws-a",
			IsLatest:        true,
			Title:           "Tie update",
			DocumentType:    knowledge.DocumentTypeProductBrief,
			AuthorityLevel:  knowledge.AuthorityHigh,
			SourceKind:      knowledge.SourceKindText,
			Status:          knowledge.StatusIndexed,
			LinkedFeatureID: tieFeature.Key,
			CreatedAt:       tieApprovedAt,
			UpdatedAt:       tieApprovedAt,
		}).Error; err != nil {
			t.Fatal(err)
		}

		draftApprovedAt := now.Add(-2 * time.Hour)
		draftCanonical := newArtifact("art-knowledge-draft", "FEAT-KNOW-DRAFT", "v1.0", artifact.StatusApproved, draftApprovedAt)
		draftCanonical.WorkspaceID = "ws-a"
		draftCanonical.ApprovedBy = "pm"
		draftCanonical.ApprovedAt = &draftApprovedAt
		if err := artifactRepo.Insert(ctx, draftCanonical); err != nil {
			t.Fatal(err)
		}
		draftFeature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:                  "feature-knowledge-draft",
			WorkspaceID:         "ws-a",
			Key:                 "FEAT-KNOW-DRAFT",
			Name:                "Knowledge draft feature",
			Status:              workboard.FeatureStatusPlanned,
			Version:             1,
			CanonicalArtifactID: draftCanonical.ID,
			CreatedAt:           now,
			UpdatedAt:           now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := gdb.WithContext(ctx).Create(&knowledge.Document{
			DocumentID:      "doc-linked-knowledge-draft",
			Version:         "v2",
			WorkspaceID:     "ws-a",
			IsLatest:        true,
			Title:           "Draft update",
			DocumentType:    knowledge.DocumentTypeProductBrief,
			AuthorityLevel:  knowledge.AuthorityHigh,
			SourceKind:      knowledge.SourceKindText,
			Status:          knowledge.StatusUploaded,
			LinkedFeatureID: draftFeature.ID,
			CreatedAt:       now.Add(-30 * time.Minute),
			UpdatedAt:       now.Add(-30 * time.Minute),
		}).Error; err != nil {
			t.Fatal(err)
		}

		tieWarnings, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{FeatureID: tieFeature.ID, WorkspaceID: "ws-a"})
		if err != nil {
			t.Fatal(err)
		}
		if len(tieWarnings) != 0 {
			t.Fatalf("tie warnings = %+v, want none", tieWarnings)
		}

		draftWarnings, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{FeatureID: draftFeature.ID, WorkspaceID: "ws-a"})
		if err != nil {
			t.Fatal(err)
		}
		if len(draftWarnings) != 0 {
			t.Fatalf("non-indexed warnings = %+v, want none", draftWarnings)
		}
	})
}

func TestWorkBoardRepository_UpdateChangeRequestPatchesAcceptanceCriteriaWithoutBlankingFields(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		now := time.Now().UTC().Truncate(time.Second)

		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID: "feature-patch", Key: "FEAT-PATCH", Name: "Patch",
			Status: workboard.FeatureStatusPlanned, Version: 1,
			CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		original, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:                 "cr-patch",
			Key:                "CR-PATCH",
			FeatureID:          feature.ID,
			WorkType:           workboard.WorkTypeNewFeature,
			Title:              "Original title",
			IntentMD:           "Original intent",
			AcceptanceCriteria: `["AC1"]`,
			CreatedAt:          now,
			UpdatedAt:          now,
		})
		if err != nil {
			t.Fatal(err)
		}

		// Sparse PATCH: only acceptance_criteria_json. Title/IntentMD/WorkType
		// must NOT be blanked.
		patched, err := repo.UpdateChangeRequest(ctx, workboard.ChangeRequest{
			ID:                 original.ID,
			AcceptanceCriteria: `[{"id":"a","text":"AC1 refined","done":true}]`,
		})
		if err != nil {
			t.Fatal(err)
		}
		if patched.Title != "Original title" {
			t.Errorf("title was blanked: %q", patched.Title)
		}
		if patched.IntentMD != "Original intent" {
			t.Errorf("intent was blanked: %q", patched.IntentMD)
		}
		if patched.WorkType != workboard.WorkTypeNewFeature {
			t.Errorf("work_type was blanked: %q", patched.WorkType)
		}
		if patched.AcceptanceCriteria != `[{"id":"a","text":"AC1 refined","done":true}]` {
			t.Errorf("acceptance_criteria not persisted: %q", patched.AcceptanceCriteria)
		}

		rows, err := repo.ListAcceptanceCriteria(ctx, original.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("acceptance criteria rows = %+v", rows)
		}
		if rows[0].ID != "a" || rows[0].Text != "AC1 refined" || !rows[0].Done || rows[0].Source != workboard.AcceptanceCriterionSourceHuman {
			t.Fatalf("acceptance criteria row = %+v", rows[0])
		}
	})
}

func TestWorkBoardRepository_AcceptanceCriterionVerificationBindingRoundTrips(t *testing.T) {
	// verification_binding threads through storage —
	// the migration adds the column, the model + list path carry it, and a CR whose
	// acceptance_criteria_json names a binding surfaces it on the listed rows.
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		now := time.Now().UTC().Truncate(time.Second)

		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:       "cr-bind",
			Key:      "CR-BIND",
			WorkType: workboard.WorkTypeBugFix,
			Title:    "Binding round-trip",
			AcceptanceCriteria: `[
				{"id":"ac-0","text":"Migration adds column","verification_binding":"migrate"},
				{"id":"ac-1","text":"CTA shown"}
			]`,
			CreatedAt: now,
			UpdatedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}

		rows, err := repo.ListAcceptanceCriteria(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Fatalf("acceptance criteria rows = %+v", rows)
		}
		if rows[0].VerificationBinding != "migrate" {
			t.Errorf("bound criterion binding = %q, want %q", rows[0].VerificationBinding, "migrate")
		}
		if rows[1].VerificationBinding != "" {
			t.Errorf("unbound criterion binding = %q, want empty", rows[1].VerificationBinding)
		}
	})
}

func TestWorkBoardRepository_UpdateFeaturePatchesStatusWithoutBlankingNameOrSummary(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		now := time.Now().UTC().Truncate(time.Second)

		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:        "feature-status-patch",
			Key:       "FEAT-STATUS-PATCH",
			Name:      "Status patch",
			Summary:   "Original summary",
			Status:    workboard.FeatureStatusPlanned,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}

		// Sparse PATCH: only status. Name/summary must NOT be blanked.
		patched, err := repo.UpdateFeature(ctx, workboard.Feature{
			ID:     feature.ID,
			Status: workboard.FeatureStatusActive,
		})
		if err != nil {
			t.Fatal(err)
		}
		if patched.Status != workboard.FeatureStatusActive {
			t.Errorf("status not persisted: %q", patched.Status)
		}
		if patched.Name != "Status patch" {
			t.Errorf("name was blanked: %q", patched.Name)
		}
		if patched.Summary != "Original summary" {
			t.Errorf("summary was blanked: %q", patched.Summary)
		}
	})
}

func TestWorkBoardRepository_UpdateFeatureWritesLifecycleAuditOnStatusChange(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:     "feat-status-audit",
			Key:    "FEAT-STATUS-AUDIT",
			Name:   "Status audit",
			Status: workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.UpdateFeature(ctx, workboard.Feature{
			ID:     feature.ID,
			Status: workboard.FeatureStatusRejected,
		}); err != nil {
			t.Fatal(err)
		}
		var events []workboard.LifecycleEvent
		if err := gdb.Where("entity_kind = ? AND entity_id = ? AND event_type = ?", "feature", feature.ID, "feature.status_changed").
			Find(&events).Error; err != nil {
			t.Fatal(err)
		}
		if len(events) != 1 {
			t.Fatalf("expected 1 lifecycle event, got %d", len(events))
		}
		if !strings.Contains(events[0].PayloadJSON, `"previous":"planned"`) || !strings.Contains(events[0].PayloadJSON, `"next":"rejected"`) {
			t.Fatalf("unexpected payload=%s", events[0].PayloadJSON)
		}
	})
}

func TestWorkBoardRepository_ListChangeRequestsHidesArchivedByDefault(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:     "feat-archive",
			Key:    "FEAT-ARCHIVE",
			Name:   "Archive filter",
			Status: workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}
		active, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:        "cr-active",
			Key:       "CR-ACTIVE",
			FeatureID: feature.ID,
			Title:     "Active CR",
			WorkType:  workboard.WorkTypeNewFeature,
		})
		if err != nil {
			t.Fatal(err)
		}
		archived, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:        "cr-archived",
			Key:       "CR-ARCHIVED",
			FeatureID: feature.ID,
			Title:     "Archived CR",
			WorkType:  workboard.WorkTypeNewFeature,
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = repo.UpdateChangeRequest(ctx, workboard.ChangeRequest{
			ID:            archived.ID,
			Archived:      true,
			ArchivedBy:    "pm@example.com",
			ArchiveReason: "closed",
		})
		if err != nil {
			t.Fatal(err)
		}
		items, err := repo.ListChangeRequests(ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].ID != active.ID {
			t.Fatalf("default list should exclude archived, got %+v", items)
		}
		items, err = repo.ListChangeRequests(ctx, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 2 {
			t.Fatalf("include archived expected 2 items, got %d", len(items))
		}
	})
}

func TestWorkBoardRepository_UnarchiveChangeRequestRestoresAndAudits(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID: "feat-unarch", Key: "FEAT-UNARCH", Name: "Unarchive", Status: workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID: "cr-unarch", Key: "CR-UNARCH", FeatureID: feature.ID, Title: "Unarchive me", WorkType: workboard.WorkTypeNewFeature,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.UpdateChangeRequest(ctx, workboard.ChangeRequest{
			ID: cr.ID, Archived: true, ArchivedBy: "pm@example.com", ArchiveReason: "closed",
		}); err != nil {
			t.Fatal(err)
		}

		restored, err := repo.UnarchiveChangeRequest(ctx, cr.ID, "lead@example.com")
		if err != nil {
			t.Fatalf("UnarchiveChangeRequest: %v", err)
		}
		if restored.Archived {
			t.Fatalf("expected unarchived, got archived=%v", restored.Archived)
		}
		// Back in the default (non-archived) list.
		items, err := repo.ListChangeRequests(ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].ID != cr.ID {
			t.Fatalf("unarchived CR should be in the default list, got %+v", items)
		}
		// Auditable.
		var events []workboard.LifecycleEvent
		if err := gdb.Where("entity_kind = ? AND entity_id = ? AND event_type = ?", "change_request", cr.ID, "change_request.unarchived").
			Find(&events).Error; err != nil {
			t.Fatal(err)
		}
		if len(events) != 1 || events[0].Actor != "lead@example.com" {
			t.Fatalf("expected 1 unarchive lifecycle event by lead@example.com, got %+v", events)
		}
	})
}

func TestWorkBoardRepository_UpdateChangeRequestArchiveWritesLifecycleAudit(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:     "feat-cr-audit",
			Key:    "FEAT-CR-AUDIT",
			Name:   "CR audit",
			Status: workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:        "cr-audit",
			Key:       "CR-AUDIT",
			FeatureID: feature.ID,
			Title:     "Archive this CR",
			WorkType:  workboard.WorkTypeNewFeature,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.UpdateChangeRequest(ctx, workboard.ChangeRequest{
			ID:            cr.ID,
			Archived:      true,
			ArchivedBy:    "pm@example.com",
			ArchiveReason: "superseded",
		}); err != nil {
			t.Fatal(err)
		}
		var events []workboard.LifecycleEvent
		if err := gdb.Where("entity_kind = ? AND entity_id = ? AND event_type = ?", "change_request", cr.ID, "change_request.archived").
			Find(&events).Error; err != nil {
			t.Fatal(err)
		}
		if len(events) != 1 {
			t.Fatalf("expected 1 lifecycle event, got %d", len(events))
		}
		if events[0].Actor != "pm@example.com" {
			t.Fatalf("actor=%q", events[0].Actor)
		}
		if !strings.Contains(events[0].PayloadJSON, `"archive_reason":"superseded"`) {
			t.Fatalf("unexpected payload=%s", events[0].PayloadJSON)
		}
	})
}

func TestWorkBoardRepository_PlatformDeliveryPassNeverAutoArchives(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:     "feat-auto-archive",
			Key:    "FEAT-AUTO-ARCHIVE",
			Name:   "Auto archive",
			Status: workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:        "cr-auto-archive",
			Key:       "CR-AUTO-ARCHIVE",
			FeatureID: feature.ID,
			Title:     "Archive after pass",
			WorkType:  workboard.WorkTypeNewFeature,
		})
		if err != nil {
			t.Fatal(err)
		}

		if _, err := repo.RefreshGateRuns(ctx, workboard.RefreshGateRunsInput{
			ChangeRequestID: cr.ID,
			Evaluations: []workboard.GateEvaluation{{
				Gate:  "delivery_review",
				State: workboard.NextActionStatePass,
				Hint:  "Delivery review passed.",
			}},
		}); err != nil {
			t.Fatal(err)
		}
		stillActive, err := repo.GetChangeRequest(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stillActive.Archived {
			t.Fatal("delivery pass should not archive when auto-archive is disabled")
		}

		repo.SetAutoArchiveOnDeliveryPass(func() bool { return true })
		if _, err := repo.RefreshGateRuns(ctx, workboard.RefreshGateRunsInput{
			ChangeRequestID: cr.ID,
			Evaluations: []workboard.GateEvaluation{{
				Gate:  "delivery_review",
				State: workboard.NextActionStatePass,
				Hint:  "Delivery review passed again.",
			}},
		}); err != nil {
			t.Fatal(err)
		}
		stillAwaitingHuman, err := repo.GetChangeRequest(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stillAwaitingHuman.Archived {
			t.Fatal("platform delivery pass must wait for human acceptance before auto-archive")
		}
		var events []workboard.LifecycleEvent
		if err := gdb.Where("entity_kind = ? AND entity_id = ? AND event_type = ?", "change_request", cr.ID, "change_request.archived").
			Find(&events).Error; err != nil {
			t.Fatal(err)
		}
		if len(events) != 0 {
			t.Fatalf("platform delivery pass must not write an archive event, got %+v", events)
		}
	})
}

func TestWorkBoardRepository_PlatformPassDoesNotAutoArchiveOverHumanDecision(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		repo.SetAutoArchiveOnDeliveryPass(func() bool { return true })
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:     "feat-human-auto-archive",
			Key:    "FEAT-HUMAN-AUTO-ARCHIVE",
			Name:   "Human auto archive",
			Status: workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:        "cr-human-auto-archive",
			Key:       "CR-HUMAN-AUTO-ARCHIVE",
			FeatureID: feature.ID,
			Title:     "Archive after human pass",
			WorkType:  workboard.WorkTypeNewFeature,
		})
		if err != nil {
			t.Fatal(err)
		}

		if err := gdb.Create(&workboard.GateRun{
			ID:          "human-pass-before-platform-pass",
			WorkspaceID: "ws-test",
			SubjectKind: workboard.GateRunSubjectChangeRequest,
			SubjectID:   cr.ID,
			Executor:    workboard.GateRunExecutorHuman,
			Gate:        "delivery_review",
			State:       workboard.NextActionStatePass,
			Hint:        "human reviewer cleared delivery",
			CreatedAt:   time.Now().UTC().Add(-time.Hour),
		}).Error; err != nil {
			t.Fatal(err)
		}

		if _, err := repo.RefreshGateRuns(ctx, workboard.RefreshGateRunsInput{
			ChangeRequestID: cr.ID,
			Evaluations: []workboard.GateEvaluation{{
				Gate:  "delivery_review",
				State: workboard.NextActionStatePass,
				Hint:  "Later platform pass.",
			}},
		}); err != nil {
			t.Fatal(err)
		}
		reloaded, err := repo.GetChangeRequest(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if reloaded.Archived {
			t.Fatalf("platform pass should not auto-archive when human decision is authoritative: %+v", reloaded)
		}
		if reloaded.DeliveryReview == nil ||
			reloaded.DeliveryReview.Executor != workboard.GateRunExecutorHuman ||
			reloaded.DeliveryReview.Verdict != string(workboard.NextActionStatePass) {
			t.Fatalf("delivery review = %+v, want authoritative human pass", reloaded.DeliveryReview)
		}
	})
}

func TestWorkBoardRepository_ListStaleWarnings_DeliveryInProgress(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		integRepo := NewIntegrationRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")

		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:     "feat-delivery-warn",
			Key:    "FEAT-DELIVERY-WARN",
			Name:   "Delivery warn",
			Status: workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:        "cr-delivery-warn",
			Key:       "CR-DELIVERY-WARN",
			FeatureID: feature.ID,
			WorkType:  workboard.WorkTypeNewFeature,
			Title:     "Delivery work",
		})
		if err != nil {
			t.Fatal(err)
		}

		// Prerequisites for delivery link FK constraints.
		integration, err := integRepo.CreateIntegration(ctx, integrations.Integration{
			ID:       "integ-delivery-warn",
			Provider: integrations.ProviderGitLab,
			Name:     "Delivery test GitLab",
			Status:   integrations.StatusConnected,
		})
		if err != nil {
			t.Fatal(err)
		}
		resource, err := integRepo.CreateResource(ctx, integrations.Resource{
			IntegrationID: integration.ID,
			ResourceType:  integrations.ResourceTypeProject,
			ExternalID:    "42",
			ExternalKey:   "group/project",
		})
		if err != nil {
			t.Fatal(err)
		}

		// No delivery link yet — no delivery_in_progress warning.
		warnings, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{FeatureID: feature.ID})
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range warnings {
			if w.Code == workboard.WarningDeliveryInProgress {
				t.Fatal("expected no delivery_in_progress warning before any open MR")
			}
		}

		// Insert an open MR delivery link.
		openLink, err := integRepo.UpsertDeliveryLink(ctx, integrations.DeliveryLink{
			IntegrationID:   integration.ID,
			ResourceID:      resource.ID,
			FeatureID:       feature.ID,
			ChangeRequestID: cr.ID,
			ExternalType:    integrations.ExternalTypeMergeRequest,
			ExternalID:      "101",
			ExternalIID:     "42",
			ExternalKey:     "!42",
			URL:             "https://gitlab.example.com/group/project/-/merge_requests/42",
			Title:           "feat: checkout overhaul",
			State:           integrations.DeliveryStateOpened,
		})
		if err != nil {
			t.Fatal(err)
		}

		// Now the warning should appear.
		warnings, err = repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{FeatureID: feature.ID})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, w := range warnings {
			if w.Code == workboard.WarningDeliveryInProgress {
				found = true
				if w.FeatureID != feature.ID {
					t.Fatalf("expected feature_id=%s got %s", feature.ID, w.FeatureID)
				}
				if w.Message == "" {
					t.Fatal("expected non-empty message")
				}
			}
		}
		if !found {
			t.Fatal("expected delivery_in_progress warning for open MR")
		}

		// After the MR merges the warning must disappear.
		_, err = integRepo.UpsertDeliveryLink(ctx, integrations.DeliveryLink{
			IntegrationID:   integration.ID,
			ResourceID:      resource.ID,
			FeatureID:       feature.ID,
			ChangeRequestID: cr.ID,
			ExternalType:    integrations.ExternalTypeMergeRequest,
			ExternalID:      "101",
			ExternalIID:     openLink.ExternalIID,
			ExternalKey:     "!42",
			URL:             "https://gitlab.example.com/group/project/-/merge_requests/42",
			Title:           "feat: checkout overhaul",
			State:           integrations.DeliveryStateMerged,
			MergeCommitSHA:  "abc123",
		})
		if err != nil {
			t.Fatal(err)
		}
		warnings, err = repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{FeatureID: feature.ID})
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range warnings {
			if w.Code == workboard.WarningDeliveryInProgress {
				t.Fatal("delivery_in_progress warning should clear after MR merges")
			}
		}
	})
}

// trackerFeedbackPayload builds the JSON payload createTrackerFeedback emits for
// a delivery.tracker_status_changed event: it carries the raw tracker state and
// the correlation id (SPECGATE-{key|id}) that ties the signal to a work item.
func trackerFeedbackPayload(t *testing.T, trackerState, correlationID string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"provider":       integrations.ProviderLinear,
		"identifier":     "LOY-128",
		"tracker_state":  trackerState,
		"correlation_id": correlationID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestWorkBoardRepository_ChangeRequestPhaseDerivedOnRead(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		artifactRepo := NewRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		now := time.Now().UTC().Truncate(time.Second)

		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			Key:    "FEAT-PHASE",
			Name:   "Phase feature",
			Status: workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}

		// Intake: no artifact pointers.
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			Key:       "CR-PHASE",
			FeatureID: feature.ID,
			WorkType:  workboard.WorkTypeNewFeature,
			Title:     "Phase work",
		})
		if err != nil {
			t.Fatal(err)
		}
		got, err := repo.GetChangeRequest(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Phase != workboard.BoardPhaseIntake {
			t.Fatalf("no pointers: phase = %q, want Intake", got.Phase)
		}

		// Draft: thread exists, but no working spec is attached yet.
		if _, err := repo.UpdateChangeRequest(ctx, workboard.ChangeRequest{
			ID:                 cr.ID,
			GovernanceThreadID: "thread-phase",
		}); err != nil {
			t.Fatal(err)
		}
		got, err = repo.GetChangeRequest(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Phase != workboard.BoardPhaseDraft {
			t.Fatalf("thread but no lead: phase = %q, want Draft", got.Phase)
		}

		// Review: a non-approved lead artifact is waiting for human approval.
		lead := newArtifact("art-phase-lead", feature.ID, "v1.0", artifact.StatusDraft, now)
		if err := artifactRepo.Insert(ctx, lead); err != nil {
			t.Fatal(err)
		}
		if err := gdb.Model(&workboard.ChangeRequest{}).Where("id = ?", cr.ID).
			Updates(map[string]any{"lead_artifact_id": lead.ID, "updated_at": now}).Error; err != nil {
			t.Fatal(err)
		}
		got, err = repo.GetChangeRequest(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Phase != workboard.BoardPhaseReview {
			t.Fatalf("draft lead artifact: phase = %q, want Review", got.Phase)
		}

		// Ready: an approved lead artifact is ready for handoff.
		approvedLead := newArtifact("art-phase-lead-approved", feature.ID, "v1.2", artifact.StatusApproved, now.Add(time.Minute))
		if err := artifactRepo.Insert(ctx, approvedLead); err != nil {
			t.Fatal(err)
		}
		if err := gdb.Model(&workboard.ChangeRequest{}).Where("id = ?", cr.ID).
			Updates(map[string]any{"lead_artifact_id": approvedLead.ID, "updated_at": now.Add(time.Minute)}).Error; err != nil {
			t.Fatal(err)
		}
		got, err = repo.GetChangeRequest(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Phase != workboard.BoardPhaseReady {
			t.Fatalf("approved lead artifact: phase = %q, want Ready", got.Phase)
		}

		// The list path must populate the derived phase too.
		items, err := repo.ListChangeRequests(ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range items {
			if item.ID == cr.ID {
				found = true
				if item.Phase != workboard.BoardPhaseReady {
					t.Fatalf("list: phase = %q, want Ready", item.Phase)
				}
			}
		}
		if !found {
			t.Fatal("change request not returned by ListChangeRequests")
		}
	})
}

func TestWorkBoardRepository_DerivedContextPackHasNoStaleWarning(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := integrations.WithWorkspace(context.Background(), "ws-test")

		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			Key:    "FEAT-PACK-STALE",
			Name:   "Pack stale feature",
			Status: workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := gdb.Create(&artifact.Artifact{
			ID: "pack-1", WorkspaceID: "ws-test", FeatureID: feature.ID, Version: "v1.0",
			Status: artifact.StatusDraft, RequestType: artifact.RequestTypeChangeRequest,
			ImpactLevel: artifact.ImpactLevelLow, CreatedBy: "tester", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}).Error; err != nil {
			t.Fatal(err)
		}
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			Key:       "CR-PACK-STALE",
			FeatureID: feature.ID,
			WorkType:  workboard.WorkTypeFeatureChange,
			Title:     "Pack stale work",
		})
		if err != nil {
			t.Fatal(err)
		}
		warnings, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{ChangeRequestID: cr.ID})
		if err != nil {
			t.Fatal(err)
		}
		for _, warning := range warnings {
			if strings.Contains(strings.ToLower(warning.Message), "context pack") {
				t.Fatalf("derived Context Pack must not create a stale warning: %+v", warnings)
			}
		}

	})
}

func TestWorkBoardRepository_CommentScopeDriftDoesNotCreateStalePackWarning(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		integRepo := NewIntegrationRepository(gdb)
		ctx := integrations.WithWorkspace(context.Background(), "ws-test")

		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			Key:    "FEAT-COMMENT-DRIFT",
			Name:   "Comment drift feature",
			Status: workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			Key:       "CR-COMMENT-DRIFT",
			FeatureID: feature.ID,
			WorkType:  workboard.WorkTypeFeatureChange,
			Title:     "Comment drift work",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := integRepo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
			FeatureID:       feature.ID,
			ChangeRequestID: cr.ID,
			EventType:       integrations.FeedbackEventCommentScopeDrift,
			PayloadJSON:     `{"provider":"github","correlation_id":"CR-COMMENT-DRIFT"}`,
			Status:          integrations.FeedbackStatusReceived,
			Reason:          "Reviewer requested new acceptance criteria after handoff.",
		}); err != nil {
			t.Fatal(err)
		}

		warnings, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{ChangeRequestID: cr.ID})
		if err != nil {
			t.Fatal(err)
		}
		for _, warning := range warnings {
			if strings.Contains(strings.ToLower(warning.Message), "context pack") {
				t.Fatalf("comment drift must not create a stale Context Pack warning: %+v", warnings)
			}
		}
	})
}

func TestWorkBoardRepository_ChangeRequestTrackerStatusReflectsLatestEvent(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		integRepo := NewIntegrationRepository(gdb)
		ctx := integrations.WithWorkspace(context.Background(), "ws-test")

		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			Key:    "FEAT-TRACKER",
			Name:   "Tracker feature",
			Status: workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			Key:       "CR-TRACKER",
			FeatureID: feature.ID,
			WorkType:  workboard.WorkTypeNewFeature,
			Title:     "Tracker work",
		})
		if err != nil {
			t.Fatal(err)
		}

		// No tracker event yet — tracker status is empty.
		got, err := repo.GetChangeRequest(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.TrackerStatus != "" {
			t.Fatalf("no tracker event: tracker_status = %q, want empty", got.TrackerStatus)
		}

		integration, err := integRepo.CreateIntegration(ctx, integrations.Integration{
			Provider: integrations.ProviderLinear,
			Name:     "Linear test",
			Status:   integrations.StatusConnected,
		})
		if err != nil {
			t.Fatal(err)
		}

		// An older "started" tracker event correlated by CR key.
		older := time.Now().UTC().Add(-1 * time.Hour)
		if _, err := integRepo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
			IntegrationID: integration.ID,
			EventType:     integrations.FeedbackEventTrackerStatusChanged,
			PayloadJSON:   trackerFeedbackPayload(t, "started", cr.Key),
			Status:        integrations.FeedbackStatusReceived,
			CreatedAt:     older,
		}); err != nil {
			t.Fatal(err)
		}
		// A newer "completed" tracker event correlated by CR id.
		if _, err := integRepo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
			IntegrationID: integration.ID,
			EventType:     integrations.FeedbackEventTrackerStatusChanged,
			PayloadJSON:   trackerFeedbackPayload(t, "completed", cr.ID),
			Status:        integrations.FeedbackStatusReceived,
			CreatedAt:     time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}

		got, err = repo.GetChangeRequest(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.TrackerStatus != "completed" {
			t.Fatalf("tracker_status = %q, want completed (latest)", got.TrackerStatus)
		}
	})
}

func TestWorkBoardRepository_TrackerStatusConflictWarning(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := integrations.WithWorkspace(context.Background(), "ws-test")

		// hasConflict reports whether ListStaleWarnings for the CR emits a
		// tracker_status_conflict warning with severity "warning".
		hasConflict := func(t *testing.T, repo *WorkBoardRepository, crID string) bool {
			t.Helper()
			warnings, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{ChangeRequestID: crID})
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range warnings {
				if w.Code == workboard.WarningTrackerStatusConflict {
					if w.Severity != "warning" {
						t.Fatalf("tracker_status_conflict severity = %q, want warning", w.Severity)
					}
					if w.Message == "" {
						t.Fatal("tracker_status_conflict message is empty")
					}
					return true
				}
			}
			return false
		}

		// setup builds a feature + CR + Linear integration + resource and returns
		// the repos and ids. Each subtest gets isolated rows.
		setup := func(t *testing.T, suffix string) (*WorkBoardRepository, *IntegrationRepository, workboard.ChangeRequest, integrations.Integration, integrations.Resource) {
			t.Helper()
			repo := NewWorkBoardRepository(gdb)
			integRepo := NewIntegrationRepository(gdb)
			feature, err := repo.CreateFeature(ctx, workboard.Feature{
				Key:    "FEAT-CONFLICT-" + suffix,
				Name:   "Conflict feature " + suffix,
				Status: workboard.FeatureStatusPlanned,
			})
			if err != nil {
				t.Fatal(err)
			}
			cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
				Key:       "CR-CONFLICT-" + suffix,
				FeatureID: feature.ID,
				WorkType:  workboard.WorkTypeNewFeature,
				Title:     "Conflict work " + suffix,
			})
			if err != nil {
				t.Fatal(err)
			}
			integration, err := integRepo.CreateIntegration(ctx, integrations.Integration{
				Provider: integrations.ProviderGitLab,
				Name:     "Conflict GitLab " + suffix,
				Status:   integrations.StatusConnected,
			})
			if err != nil {
				t.Fatal(err)
			}
			resource, err := integRepo.CreateResource(ctx, integrations.Resource{
				IntegrationID: integration.ID,
				ResourceType:  integrations.ResourceTypeProject,
				ExternalID:    "p-" + suffix,
				ExternalKey:   "group/project-" + suffix,
			})
			if err != nil {
				t.Fatal(err)
			}
			return repo, integRepo, *cr, *integration, *resource
		}

		emitTracker := func(t *testing.T, integRepo *IntegrationRepository, integrationID, state, correlation string) {
			t.Helper()
			if _, err := integRepo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
				IntegrationID: integrationID,
				EventType:     integrations.FeedbackEventTrackerStatusChanged,
				PayloadJSON:   trackerFeedbackPayload(t, state, correlation),
				Status:        integrations.FeedbackStatusReceived,
			}); err != nil {
				t.Fatal(err)
			}
		}

		mergeDelivery := func(t *testing.T, integRepo *IntegrationRepository, integration integrations.Integration, resource integrations.Resource, cr workboard.ChangeRequest) {
			t.Helper()
			if _, err := integRepo.UpsertDeliveryLink(ctx, integrations.DeliveryLink{
				IntegrationID:   integration.ID,
				ResourceID:      resource.ID,
				FeatureID:       cr.FeatureID,
				ChangeRequestID: cr.ID,
				ExternalType:    integrations.ExternalTypeMergeRequest,
				ExternalID:      "mr-" + cr.Key,
				ExternalIID:     "1",
				ExternalKey:     "!1",
				URL:             "https://gitlab.example.com/mr/1",
				Title:           "feat: work",
				State:           integrations.DeliveryStateMerged,
				MergeCommitSHA:  "deadbeef",
			}); err != nil {
				t.Fatal(err)
			}
		}

		t.Run("completed but no merge -> conflict", func(t *testing.T) {
			repo, integRepo, cr, integration, _ := setup(t, "A")
			emitTracker(t, integRepo, integration.ID, "completed", cr.Key)
			if !hasConflict(t, repo, cr.ID) {
				t.Fatal("expected tracker_status_conflict for completed tracker with no merged PR")
			}
			allWarnings, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, warning := range allWarnings {
				if warning.Code == workboard.WarningTrackerStatusConflict && warning.ChangeRequestID == cr.ID {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected aggregate stale warnings to include tracker_status_conflict, got %+v", allWarnings)
			}
		})

		t.Run("merged but tracker not completed -> conflict", func(t *testing.T) {
			repo, integRepo, cr, integration, resource := setup(t, "B")
			emitTracker(t, integRepo, integration.ID, "started", cr.Key)
			mergeDelivery(t, integRepo, integration, resource, cr)
			if !hasConflict(t, repo, cr.ID) {
				t.Fatal("expected tracker_status_conflict for merged PR with tracker not completed")
			}
		})

		t.Run("completed and merged -> no conflict", func(t *testing.T) {
			repo, integRepo, cr, integration, resource := setup(t, "C")
			emitTracker(t, integRepo, integration.ID, "completed", cr.Key)
			mergeDelivery(t, integRepo, integration, resource, cr)
			if hasConflict(t, repo, cr.ID) {
				t.Fatal("did not expect conflict when tracker completed and PR merged")
			}
		})

		t.Run("canceled and merged -> no conflict", func(t *testing.T) {
			repo, integRepo, cr, integration, resource := setup(t, "D")
			emitTracker(t, integRepo, integration.ID, "canceled", cr.Key)
			mergeDelivery(t, integRepo, integration, resource, cr)
			if hasConflict(t, repo, cr.ID) {
				t.Fatal("did not expect conflict when tracker canceled and PR merged")
			}
		})

		t.Run("no tracker event -> no conflict", func(t *testing.T) {
			repo, integRepo, cr, integration, resource := setup(t, "E")
			mergeDelivery(t, integRepo, integration, resource, cr)
			if hasConflict(t, repo, cr.ID) {
				t.Fatal("did not expect conflict when there is no tracker event")
			}
		})

		t.Run("started and no merge -> no conflict", func(t *testing.T) {
			repo, integRepo, cr, integration, _ := setup(t, "F")
			emitTracker(t, integRepo, integration.ID, "started", cr.Key)
			if hasConflict(t, repo, cr.ID) {
				t.Fatal("did not expect conflict when tracker started and no merge")
			}
		})
	})
}

func TestWorkBoardRepository_TrackerPriorityUrgentWarning(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := integrations.WithWorkspace(context.Background(), "ws-test")

		// hasPriorityWarning reports whether ListStaleWarnings for the CR emits a
		// tracker_priority_urgent warning with severity "warn".
		hasPriorityWarning := func(t *testing.T, repo *WorkBoardRepository, crID string) bool {
			t.Helper()
			warnings, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{ChangeRequestID: crID})
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range warnings {
				if w.Code == workboard.WarningTrackerPriorityUrgent {
					if w.Severity != "warn" {
						t.Fatalf("tracker_priority_urgent severity = %q, want warn", w.Severity)
					}
					if w.Message == "" {
						t.Fatal("tracker_priority_urgent message is empty")
					}
					return true
				}
			}
			return false
		}

		// setup creates a feature + CR and a Linear integration. Suffix keeps rows
		// isolated between subtests.
		setup := func(t *testing.T, suffix string) (*WorkBoardRepository, *IntegrationRepository, workboard.ChangeRequest, integrations.Integration) {
			t.Helper()
			repo := NewWorkBoardRepository(gdb)
			integRepo := NewIntegrationRepository(gdb)
			feature, err := repo.CreateFeature(ctx, workboard.Feature{
				Key:    "FEAT-PRI-" + suffix,
				Name:   "Priority feature " + suffix,
				Status: workboard.FeatureStatusPlanned,
			})
			if err != nil {
				t.Fatal(err)
			}
			cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
				Key:       "CR-PRI-" + suffix,
				FeatureID: feature.ID,
				WorkType:  workboard.WorkTypeNewFeature,
				Title:     "Priority work " + suffix,
			})
			if err != nil {
				t.Fatal(err)
			}
			integration, err := integRepo.CreateIntegration(ctx, integrations.Integration{
				Provider: integrations.ProviderLinear,
				Name:     "Priority Linear " + suffix,
				Status:   integrations.StatusConnected,
			})
			if err != nil {
				t.Fatal(err)
			}
			return repo, integRepo, *cr, *integration
		}

		// emitTrackerWithPriority seeds a delivery.tracker_status_changed event that
		// carries a `priority` field in its payload_json, mirroring what
		// createTrackerFeedback emits after the fix.
		emitTrackerWithPriority := func(t *testing.T, integRepo *IntegrationRepository, integrationID, state, correlation string, priority int) {
			t.Helper()
			body, err := json.Marshal(map[string]any{
				"provider":       integrations.ProviderLinear,
				"identifier":     "LOY-999",
				"tracker_state":  state,
				"correlation_id": correlation,
				"priority":       priority,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := integRepo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
				IntegrationID: integrationID,
				EventType:     integrations.FeedbackEventTrackerStatusChanged,
				PayloadJSON:   string(body),
				Status:        integrations.FeedbackStatusReceived,
			}); err != nil {
				t.Fatal(err)
			}
		}

		t.Run("priority 1 (urgent) -> warning fires", func(t *testing.T) {
			repo, integRepo, cr, integration := setup(t, "A")
			emitTrackerWithPriority(t, integRepo, integration.ID, "started", cr.Key, 1)
			if !hasPriorityWarning(t, repo, cr.ID) {
				t.Fatal("expected tracker_priority_urgent for urgent priority")
			}
		})

		t.Run("priority 2 (high) -> warning fires", func(t *testing.T) {
			repo, integRepo, cr, integration := setup(t, "B")
			emitTrackerWithPriority(t, integRepo, integration.ID, "started", cr.Key, 2)
			if !hasPriorityWarning(t, repo, cr.ID) {
				t.Fatal("expected tracker_priority_urgent for high priority")
			}
		})

		t.Run("priority 3 (normal) -> warning absent", func(t *testing.T) {
			repo, integRepo, cr, integration := setup(t, "C")
			emitTrackerWithPriority(t, integRepo, integration.ID, "started", cr.Key, 3)
			if hasPriorityWarning(t, repo, cr.ID) {
				t.Fatal("did not expect tracker_priority_urgent for normal priority")
			}
		})

		t.Run("priority 0 (no priority) -> warning absent", func(t *testing.T) {
			repo, integRepo, cr, integration := setup(t, "D")
			emitTrackerWithPriority(t, integRepo, integration.ID, "started", cr.Key, 0)
			if hasPriorityWarning(t, repo, cr.ID) {
				t.Fatal("did not expect tracker_priority_urgent for unprioritized issue")
			}
		})

		t.Run("no tracker event -> warning absent", func(t *testing.T) {
			repo, _, cr, _ := setup(t, "F")
			if hasPriorityWarning(t, repo, cr.ID) {
				t.Fatal("did not expect tracker_priority_urgent when no tracker event exists")
			}
		})
	})
}

func TestWorkBoardRepository_UpsertFeatureByKey(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")

		// First call: creates a new feature.
		first, err := repo.UpsertFeatureByKey(ctx, "checkout-loyalty", "Checkout loyalty")
		if err != nil {
			t.Fatalf("first upsert: %v", err)
		}
		if first.ID == "" {
			t.Fatal("expected non-empty feature ID")
		}
		if first.Key != "checkout-loyalty" {
			t.Fatalf("key = %q, want %q", first.Key, "checkout-loyalty")
		}
		if first.Name != "Checkout loyalty" {
			t.Fatalf("name = %q, want %q", first.Name, "Checkout loyalty")
		}
		if first.Status != workboard.FeatureStatusCandidate {
			t.Fatalf("status = %q, want %q", first.Status, workboard.FeatureStatusCandidate)
		}

		// Second call: returns the existing feature (same ID, no duplicate).
		second, err := repo.UpsertFeatureByKey(ctx, "checkout-loyalty", "Checkout loyalty")
		if err != nil {
			t.Fatalf("second upsert: %v", err)
		}
		if second.ID != first.ID {
			t.Fatalf("second upsert returned different ID: got %q, want %q", second.ID, first.ID)
		}

		// Verify exactly one row with this key exists.
		var count int64
		if err := gdb.Model(&workboard.Feature{}).Where("key = ?", "checkout-loyalty").Count(&count).Error; err != nil {
			t.Fatalf("count query: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected 1 feature row, got %d", count)
		}
	})
}

// trackerFeedbackPayloadForProvider builds a tracker_status_changed payload
// with a specific provider, for testing provider-aware warning messages.
func trackerFeedbackPayloadForProvider(t *testing.T, provider, trackerState, correlationID string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"provider":       provider,
		"identifier":     "ISSUE-1",
		"tracker_state":  trackerState,
		"correlation_id": correlationID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// TestWorkBoardRepository_DeliveryStaleWarning covers the delivery_stale
// stale warning: fires when a CR in handoff has no delivery_review gate run
// within the SLA threshold.
func TestWorkBoardRepository_DeliveryStaleWarning(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		repo := NewWorkBoardRepository(gdb)
		// 0-day threshold: any failing delivery review is immediately stale.
		repo.SetDeliverySLADays(0)
		now := time.Now().UTC()

		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:     "feat-sla-test",
			Key:    "FEAT-SLA-TEST",
			Name:   "SLA test feature",
			Status: workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:        "cr-sla-test",
			Key:       "CR-SLA-TEST",
			FeatureID: feature.ID,
			WorkType:  workboard.WorkTypeNewFeature,
			Title:     "SLA test CR",
		})
		if err != nil {
			t.Fatal(err)
		}

		hasDeliveryStale := func(t *testing.T) bool {
			t.Helper()
			warnings, wErr := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{ChangeRequestID: cr.ID})
			if wErr != nil {
				t.Fatal(wErr)
			}
			for _, w := range warnings {
				if w.Code == workboard.WarningDeliveryStale {
					return true
				}
			}
			return false
		}
		deleteGateRuns := func(t *testing.T) {
			t.Helper()
			if err := gdb.Where("subject_kind = ? AND subject_id = ? AND gate = ?", workboard.GateRunSubjectChangeRequest, cr.ID, "delivery_review").
				Delete(&workboard.GateRun{}).Error; err != nil {
				t.Fatal(err)
			}
		}

		// No delivery review history at all — no warning regardless of threshold.
		if hasDeliveryStale(t) {
			t.Fatal("expected no delivery_stale warning when no delivery review exists")
		}

		// Latest delivery review passed — no warning.
		if err := gdb.Create(&workboard.GateRun{
			ID:          "%s",
			WorkspaceID: "ws-test",
			SubjectKind: workboard.GateRunSubjectChangeRequest,
			SubjectID:   cr.ID,
			Executor:    workboard.GateRunExecutorPlatform,
			Gate:        "delivery_review",
			State:       workboard.NextActionStatePass,
			Hint:        "all criteria satisfied",
			CreatedAt:   now,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if hasDeliveryStale(t) {
			t.Fatal("expected no delivery_stale warning when authoritative delivery review passed")
		}
		deleteGateRuns(t)

		// Authoritative delivery review failed but within threshold (threshold = 7, age = 0s).
		repo.SetDeliverySLADays(7)
		if err := gdb.Create(&workboard.GateRun{
			ID:          "%s",
			WorkspaceID: "ws-test",
			SubjectKind: workboard.GateRunSubjectChangeRequest,
			SubjectID:   cr.ID,
			Executor:    workboard.GateRunExecutorPlatform,
			Gate:        "delivery_review",
			State:       workboard.NextActionStateFail,
			Hint:        "criteria not met",
			CreatedAt:   now,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if hasDeliveryStale(t) {
			t.Fatal("expected no delivery_stale warning when failed delivery review is within SLA threshold")
		}
		deleteGateRuns(t)

		// Authoritative delivery review failed and older than threshold (threshold = 0).
		repo.SetDeliverySLADays(0)
		if err := gdb.Create(&workboard.GateRun{
			ID:          "%s",
			WorkspaceID: "ws-test",
			SubjectKind: workboard.GateRunSubjectChangeRequest,
			SubjectID:   cr.ID,
			Executor:    workboard.GateRunExecutorPlatform,
			Gate:        "delivery_review",
			State:       workboard.NextActionStateFail,
			Hint:        "criteria not met",
			CreatedAt:   now.Add(-48 * time.Hour),
		}).Error; err != nil {
			t.Fatal(err)
		}
		if !hasDeliveryStale(t) {
			t.Fatal("expected delivery_stale warning when authoritative delivery review is a stale fail")
		}

		// Message must include the number of days stale.
		warnings, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{ChangeRequestID: cr.ID})
		if err != nil {
			t.Fatal(err)
		}
		var msg string
		for _, w := range warnings {
			if w.Code == workboard.WarningDeliveryStale {
				msg = w.Message
			}
		}
		if !strings.Contains(msg, "day") {
			t.Errorf("expected delivery_stale message to include day count, got %q", msg)
		}
		deleteGateRuns(t)

		// Human approval is authoritative even if a later platform delivery
		// review failed; stale warnings must follow the same trust precedence as
		// the board phase.
		if err := gdb.Create(&workboard.GateRun{
			ID:          "human-delivery-approval",
			WorkspaceID: "ws-test",
			SubjectKind: workboard.GateRunSubjectChangeRequest,
			SubjectID:   cr.ID,
			Executor:    workboard.GateRunExecutorHuman,
			Gate:        "delivery_review",
			State:       workboard.NextActionStatePass,
			Hint:        "human reviewer cleared delivery",
			CreatedAt:   now.Add(-72 * time.Hour),
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := gdb.Create(&workboard.GateRun{
			ID:          "later-platform-delivery-fail",
			WorkspaceID: "ws-test",
			SubjectKind: workboard.GateRunSubjectChangeRequest,
			SubjectID:   cr.ID,
			Executor:    workboard.GateRunExecutorPlatform,
			Gate:        "delivery_review",
			State:       workboard.NextActionStateFail,
			Hint:        "platform reviewer would fail",
			CreatedAt:   now.Add(-48 * time.Hour),
		}).Error; err != nil {
			t.Fatal(err)
		}
		if hasDeliveryStale(t) {
			t.Fatal("expected no delivery_stale warning when authoritative human approval outranks later platform fail")
		}
	})
}

// TestWorkBoardRepository_TrackerConflictWarning_UsesProviderName verifies
// that the tracker_status_conflict warning message names the specific provider
// (e.g. "GitHub") rather than the hardcoded "Linear" text.
func TestWorkBoardRepository_TrackerConflictWarning_UsesProviderName(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := integrations.WithWorkspace(context.Background(), "ws-test")
		repo := NewWorkBoardRepository(gdb)
		integRepo := NewIntegrationRepository(gdb)

		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:     "feat-provider-conflict",
			Key:    "FEAT-PROVIDER-CONFLICT",
			Name:   "Provider conflict test",
			Status: workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:        "cr-provider-conflict",
			Key:       "CR-PROVIDER-CONFLICT",
			FeatureID: feature.ID,
			WorkType:  workboard.WorkTypeNewFeature,
			Title:     "Provider conflict CR",
		})
		if err != nil {
			t.Fatal(err)
		}
		integration, err := integRepo.CreateIntegration(ctx, integrations.Integration{
			ID:       "integ-provider-conflict",
			Provider: integrations.ProviderGitHub,
			Name:     "GitHub test",
			Status:   integrations.StatusConnected,
		})
		if err != nil {
			t.Fatal(err)
		}

		// GitHub issue marked completed, but no merge detected → conflict.
		if _, err := integRepo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
			IntegrationID: integration.ID,
			EventType:     integrations.FeedbackEventTrackerStatusChanged,
			PayloadJSON:   trackerFeedbackPayloadForProvider(t, integrations.ProviderGitHub, "completed", cr.ID),
			Status:        integrations.FeedbackStatusReceived,
		}); err != nil {
			t.Fatal(err)
		}

		warnings, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{ChangeRequestID: cr.ID})
		if err != nil {
			t.Fatal(err)
		}
		var conflictMsg string
		for _, w := range warnings {
			if w.Code == workboard.WarningTrackerStatusConflict {
				conflictMsg = w.Message
				break
			}
		}
		if conflictMsg == "" {
			t.Fatal("expected a tracker_status_conflict warning; got none")
		}
		if !strings.Contains(conflictMsg, "GitHub") {
			t.Errorf("conflict message = %q; want it to contain provider name 'GitHub'", conflictMsg)
		}
		if strings.Contains(conflictMsg, "Linear") {
			t.Errorf("conflict message = %q; must not contain hardcoded 'Linear'", conflictMsg)
		}
	})
}

// Delivered phase is human authority, not a platform evidence verdict (per
// spec §15). A platform pass remains Ready for acceptance.
func TestWorkBoardRepository_DeliveredPhaseRequiresHumanApproval(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		now := time.Now().UTC().Truncate(time.Second)

		mkCR := func(t *testing.T, id string) *workboard.ChangeRequest {
			t.Helper()
			cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
				ID:        id,
				Key:       strings.ToUpper(id),
				WorkType:  workboard.WorkTypeBugFix,
				Title:     "Delivered phase " + id,
				CreatedAt: now,
				UpdatedAt: now,
			})
			if err != nil {
				t.Fatal(err)
			}
			return cr
		}
		addReview := func(
			t *testing.T,
			crID string,
			state workboard.NextActionState,
			executor string,
			at time.Time,
		) {
			t.Helper()
			if err := gdb.Create(&workboard.GateRun{
				ID:           fmt.Sprintf("run-%s-%d", crID, at.UnixNano()),
				WorkspaceID:  "ws-test",
				SubjectKind:  workboard.GateRunSubjectChangeRequest,
				SubjectID:    crID,
				Executor:     executor,
				Gate:         "delivery_review",
				State:        state,
				Hint:         "review " + string(state),
				EvidenceJSON: "{}",
				CreatedAt:    at,
			}).Error; err != nil {
				t.Fatal(err)
			}
		}

		passCR := mkCR(t, "cr-delivered-pass")
		acceptedCR := mkCR(t, "cr-delivered-accepted")
		failCR := mkCR(t, "cr-delivered-fail")
		noneCR := mkCR(t, "cr-delivered-none")
		flipToFailCR := mkCR(t, "cr-delivered-flip-fail")
		flipToPassCR := mkCR(t, "cr-delivered-flip-pass")

		addReview(t, passCR.ID, workboard.NextActionStatePass, workboard.GateRunExecutorPlatform, now)
		addReview(t, acceptedCR.ID, workboard.NextActionStatePass, workboard.GateRunExecutorHuman, now)
		addReview(t, failCR.ID, workboard.NextActionStateFail, workboard.GateRunExecutorPlatform, now)
		addReview(t, flipToFailCR.ID, workboard.NextActionStatePass, workboard.GateRunExecutorHuman, now.Add(-time.Hour))
		addReview(t, flipToFailCR.ID, workboard.NextActionStateFail, workboard.GateRunExecutorPlatform, now)
		addReview(t, flipToPassCR.ID, workboard.NextActionStateFail, workboard.GateRunExecutorHuman, now.Add(-time.Hour))
		addReview(t, flipToPassCR.ID, workboard.NextActionStatePass, workboard.GateRunExecutorPlatform, now)

		want := map[string]workboard.BoardPhase{
			passCR.ID:       workboard.BoardPhaseReady,
			acceptedCR.ID:   workboard.BoardPhaseDelivered,
			failCR.ID:       workboard.BoardPhaseReady,
			noneCR.ID:       workboard.BoardPhaseReady,
			flipToFailCR.ID: workboard.BoardPhaseDelivered,
			flipToPassCR.ID: workboard.BoardPhaseReady,
		}

		// Single-read path.
		for id, phase := range want {
			got, err := repo.GetChangeRequest(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if got.Phase != phase {
				t.Fatalf("GetChangeRequest(%s): phase = %q, want %q", id, got.Phase, phase)
			}
		}

		// List path — the delivered override is batch-loaded across many CRs.
		items, err := repo.ListChangeRequests(ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		seen := 0
		for _, item := range items {
			phase, ok := want[item.ID]
			if !ok {
				continue
			}
			seen++
			if item.Phase != phase {
				t.Fatalf("ListChangeRequests(%s): phase = %q, want %q", item.ID, item.Phase, phase)
			}
			if id := item.ID; id == failCR.ID {
				if item.DeliveryReview == nil || item.DeliveryReview.Verdict != string(workboard.NextActionStateFail) {
					t.Fatalf("ListChangeRequests(%s): delivery_review = %#v, want fail snapshot", id, item.DeliveryReview)
				}
			}
		}
		if seen != len(want) {
			t.Fatalf("ListChangeRequests returned %d of %d expected CRs", seen, len(want))
		}
	})
}

// Quick-route CRs (no lead artifact and no feature) never grow a working spec,
// so the full-artifact-flow gates persist as not_applicable for audit instead
// of pending forever (per spec §15).
func TestWorkBoardRepository_NextActionsQuickRouteMarksArtifactGatesNotApplicable(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")

		quickGates := []string{"spec_drafted", "spec_approved", "no_conflicts", "knowledge_fresh", "canonical_spec"}

		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:       "cr-quick-gates",
			Key:      "CR-QUICK-GATES",
			WorkType: workboard.WorkTypeBugFix,
			Title:    "Quick-route gates",
		})
		if err != nil {
			t.Fatal(err)
		}

		actions, err := repo.NextActions(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		byGate := map[string]workboard.NextAction{}
		for _, action := range actions {
			byGate[action.Gate] = action
		}
		for _, gate := range quickGates {
			if byGate[gate].State != workboard.NextActionStateNotApplicable {
				t.Fatalf("%s state = %q, want not_applicable", gate, byGate[gate].State)
			}
			if byGate[gate].Hint != "Not required for quick-route work" {
				t.Fatalf("%s hint = %q", gate, byGate[gate].Hint)
			}
		}
		if _, exists := byGate["delivery_pack"]; exists {
			t.Fatal("delivery_pack must not be emitted: Context Packs are derived on read")
		}

		// RefreshGateRuns persists the not_applicable rows for audit.
		rows, err := repo.RefreshGateRuns(ctx, workboard.RefreshGateRunsInput{ChangeRequestID: cr.ID})
		if err != nil {
			t.Fatal(err)
		}
		rowByGate := map[string]workboard.GateRun{}
		for _, row := range rows {
			rowByGate[row.Gate] = row
		}
		for _, gate := range quickGates {
			if rowByGate[gate].State != workboard.NextActionStateNotApplicable {
				t.Fatalf("persisted %s state = %q, want not_applicable", gate, rowByGate[gate].State)
			}
		}
		persisted, err := repo.ListGateRuns(ctx, cr.ID, 50)
		if err != nil {
			t.Fatal(err)
		}
		persistedNA := 0
		for _, row := range persisted {
			if row.State == workboard.NextActionStateNotApplicable {
				persistedNA++
			}
		}
		if persistedNA != len(quickGates) {
			t.Fatalf("persisted not_applicable rows = %d, want %d", persistedNA, len(quickGates))
		}

		// A featureless CR with a lead artifact is NOT quick-route: gates stay
		// deterministic. (Feature-backed CRs are covered by the existing
		// NextActions tests.)
		if err := gdb.Create(&artifact.Artifact{
			ID: "art-missing-lead", WorkspaceID: "ws-test", Version: "v1.0",
			Status: artifact.StatusApproved, RequestType: artifact.RequestTypeChangeRequest,
			ImpactLevel: artifact.ImpactLevelLow, CreatedBy: "tester", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}).Error; err != nil {
			t.Fatal(err)
		}
		fullCR, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:             "cr-quick-gates-full",
			Key:            "CR-QUICK-GATES-FULL",
			WorkType:       workboard.WorkTypeBugFix,
			Title:          "Not quick: lead attached",
			LeadArtifactID: "art-missing-lead",
		})
		if err != nil {
			t.Fatal(err)
		}
		fullActions, err := repo.NextActions(ctx, fullCR.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, action := range fullActions {
			if action.Gate == "spec_drafted" && action.State != workboard.NextActionStatePass {
				t.Fatalf("full-route spec_drafted state = %q, want pass", action.State)
			}
			if action.Gate == "spec_approved" && action.State == workboard.NextActionStateNotApplicable {
				t.Fatalf("full-route spec_approved must not be not_applicable")
			}
		}
	})
}

func TestWorkBoardRepository_NextActionsFeatureLinkedQuickRouteMarksArtifactGatesNotApplicable(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		feature, err := repo.CreateFeature(ctx, workboard.Feature{ID: "feat-quick", Key: "quick", Name: "Quick feature"})
		if err != nil {
			t.Fatal(err)
		}
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID: "cr-feature-linked-quick", Key: "CR-FEATURE-LINKED-QUICK", FeatureID: feature.ID,
			WorkType: workboard.WorkTypeBugFix, Title: "Quick route with feature context",
		})
		if err != nil {
			t.Fatal(err)
		}

		actions, err := repo.NextActions(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, action := range actions {
			if action.Gate == "spec_drafted" && action.State != workboard.NextActionStateNotApplicable {
				t.Fatalf("feature-linked quick spec_drafted = %q, want not_applicable", action.State)
			}
		}
	})
}

func TestWorkBoardRepository_ListLifecycleEvents(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-a")
		base := time.Now().UTC().Truncate(time.Second)

		// Two events on the CR (out of chronological insert order) and one on
		// another entity that must be filtered out.
		rows := []workboard.LifecycleEvent{
			{ID: "le-2", WorkspaceID: "ws-a", EntityKind: "change_request", EntityID: "cr-audit", EventType: "change_request.unarchived", Actor: "alice", PayloadJSON: `{}`, CreatedAt: base.Add(2 * time.Hour)},
			{ID: "le-1", WorkspaceID: "ws-a", EntityKind: "change_request", EntityID: "cr-audit", EventType: "change_request.archived", Actor: "bob", PayloadJSON: `{}`, CreatedAt: base.Add(1 * time.Hour)},
			{ID: "le-foreign", WorkspaceID: "ws-b", EntityKind: "change_request", EntityID: "cr-audit", EventType: "change_request.archived", Actor: "eve", PayloadJSON: `{}`, CreatedAt: base},
			{ID: "le-other", WorkspaceID: "ws-a", EntityKind: "feature", EntityID: "feat-audit", EventType: "feature.status_changed", PayloadJSON: `{}`, CreatedAt: base},
		}
		for i := range rows {
			if err := gdb.WithContext(ctx).Create(&rows[i]).Error; err != nil {
				t.Fatal(err)
			}
		}

		got, err := repo.ListLifecycleEvents(ctx, "change_request", "cr-audit", 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d events, want 2 (filtered by entity)", len(got))
		}
		if got[0].ID != "le-1" || got[1].ID != "le-2" {
			t.Fatalf("events not ordered ascending by created_at: %s, %s", got[0].ID, got[1].ID)
		}

		feat, err := repo.ListLifecycleEvents(ctx, "feature", "feat-audit", 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(feat) != 1 || feat[0].ID != "le-other" {
			t.Fatalf("feature scope = %+v, want single le-other", feat)
		}
	})
}

func TestWorkBoardRepository_RejectsCrossWorkspaceMutations(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctxA := workboard.WithWorkspace(context.Background(), "ws-a")
		now := time.Now().UTC()
		if _, err := repo.CreateFeature(ctxA, workboard.Feature{
			ID: "feat-created-b", WorkspaceID: "ws-b", Key: "created-b", Name: "Created B",
		}); !errors.Is(err, workboard.ErrNotFound) {
			t.Fatalf("CreateFeature workspace mismatch error = %v, want ErrNotFound", err)
		}
		if err := gdb.Create(&workboard.Feature{
			ID: "feat-b", WorkspaceID: "ws-b", Key: "feature-b", Name: "Feature B", Status: workboard.FeatureStatusPlanned, CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := gdb.Create(&workboard.ChangeRequest{
			ID: "cr-b", WorkspaceID: "ws-b", Key: "CR-B", Title: "CR B", IntentMD: "intent", WorkType: workboard.WorkTypeBugFix, Archived: true, CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			t.Fatal(err)
		}

		for name, mutate := range map[string]func() error{
			"update feature": func() error {
				_, err := repo.UpdateFeature(ctxA, workboard.Feature{ID: "feat-b", Status: workboard.FeatureStatusActive})
				return err
			},
			"update change request": func() error {
				_, err := repo.UpdateChangeRequest(ctxA, workboard.ChangeRequest{ID: "cr-b", Title: "changed"})
				return err
			},
			"unarchive change request": func() error {
				_, err := repo.UnarchiveChangeRequest(ctxA, "cr-b", "reviewer")
				return err
			},
			"record delivery decision": func() error {
				_, err := repo.RecordDeliveryDecision(ctxA, workboard.DeliveryDecisionInput{ChangeRequestID: "cr-b", Actor: "reviewer", Decision: workboard.DeliveryDecisionApprove})
				return err
			},
		} {
			t.Run(name, func(t *testing.T) {
				if err := mutate(); !errors.Is(err, workboard.ErrNotFound) {
					t.Fatalf("error = %v, want ErrNotFound", err)
				}
			})
		}
		for name, read := range map[string]func() error{
			"next actions": func() error {
				_, err := repo.NextActions(ctxA, "cr-b")
				return err
			},
			"acceptance criteria": func() error {
				_, err := repo.ListAcceptanceCriteria(ctxA, "cr-b")
				return err
			},
			"refresh gate runs": func() error {
				_, err := repo.RefreshGateRuns(ctxA, workboard.RefreshGateRunsInput{ChangeRequestID: "cr-b"})
				return err
			},
			"stale warnings": func() error {
				_, err := repo.ListStaleWarnings(ctxA, workboard.StaleWarningFilter{ChangeRequestID: "cr-b"})
				return err
			},
		} {
			t.Run(name, func(t *testing.T) {
				if err := read(); !errors.Is(err, workboard.ErrNotFound) {
					t.Fatalf("error = %v, want ErrNotFound", err)
				}
			})
		}

		featureA, err := repo.CreateFeature(ctxA, workboard.Feature{ID: "feat-a", Key: "feature-a", Name: "Feature A"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = repo.CreateChangeRequest(ctxA, workboard.ChangeRequest{
			ID: "cr-a", FeatureID: featureA.ID, Key: "CR-A", Title: "CR A", IntentMD: "intent", WorkType: workboard.WorkTypeBugFix,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := gdb.Create(&artifact.Artifact{
			ID: "artifact-b", WorkspaceID: "ws-b", FeatureID: "feat-b", Version: "v1.0",
			Status: artifact.StatusApproved, RequestType: artifact.RequestTypeChangeRequest,
			ImpactLevel: artifact.ImpactLevelLow, CreatedBy: "tester", CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := repo.SetFeatureCanonicalArtifact(ctxA, featureA.ID, "artifact-b", "reviewer"); !errors.Is(err, workboard.ErrNotFound) {
			t.Fatalf("SetFeatureCanonicalArtifact cross-workspace error = %v, want ErrNotFound", err)
		}
	})
}

func TestWorkBoardRepository_GetFeatureByKey(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(context.Background(), "ws-test")
		if _, err := repo.CreateFeature(ctx, workboard.Feature{Key: "by-key-feature", Name: "F"}); err != nil {
			t.Fatal(err)
		}
		got, err := repo.GetFeatureByKey(ctx, "BY-KEY-FEATURE") // case-insensitive
		if err != nil {
			t.Fatal(err)
		}
		if got.Key != "by-key-feature" {
			t.Fatalf("key = %q", got.Key)
		}
		if _, err := repo.GetFeatureByKey(ctx, "missing"); err != workboard.ErrNotFound {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}
