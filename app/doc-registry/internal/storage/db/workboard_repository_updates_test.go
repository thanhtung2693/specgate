package db

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/knowledge"
	"github.com/specgate/doc-registry/internal/workboard"
)

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
