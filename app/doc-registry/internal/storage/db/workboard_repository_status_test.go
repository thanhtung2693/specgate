package db

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
)

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
