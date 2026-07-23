package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
)

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
