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
