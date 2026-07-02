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
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/knowledge"
	"github.com/specgate/doc-registry/internal/workboard"
)

func TestWorkBoardRepository_CreateChangeRequestAllowsFeaturelessQuickWork(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := context.Background()
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
		if reloaded.Phase != workboard.BoardPhaseIntake {
			t.Fatalf("phase = %q, want %q", reloaded.Phase, workboard.BoardPhaseIntake)
		}
	})
}

func TestWorkBoardRepository_CanonicalPromotionIncrementsOnlyOnReplacement(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		artifactRepo := NewRepository(gdb)
		ctx := context.Background()
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
		ctx := context.Background()
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

func TestWorkBoardRepository_StaleWarnings(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := context.Background()
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
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		approvedAt := now.Add(-1 * time.Hour)
		lead := newArtifact("art-next-lead", "FEAT-NEXT", "v0.2", artifact.StatusApproved, approvedAt)
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
		ctx := context.Background()
		now := time.Now().UTC()
		if err := gdb.Create(&artifact.Artifact{
			ID:          "art-canonical",
			FeatureID:   "FEAT-GATE",
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
			FeatureID:   "FEAT-GATE",
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
		foundProposal := false
		for _, row := range rows {
			if row.Gate == "canonical_spec" && row.ProposalRef != "" {
				foundProposal = true
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
		if !foundProposal {
			t.Fatalf("expected proposal_ref on canonical_spec warn row, got %+v", rows)
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
					// An LLM-only gate has no deterministic next-action — it must
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

func TestWorkBoardRepository_StaleWarningsIncludeNewerLinkedKnowledge(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		artifactRepo := NewRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		approvedAt := now.Add(-2 * time.Hour)
		canonical := newArtifact("art-knowledge-canon", "FEAT-KNOW", "v1.0", artifact.StatusApproved, approvedAt)
		canonical.ApprovedBy = "pm"
		canonical.ApprovedAt = &approvedAt
		if err := artifactRepo.Insert(ctx, canonical); err != nil {
			t.Fatal(err)
		}
		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:                  "feature-knowledge",
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

		warnings, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{FeatureID: feature.ID})
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
		tieCanonical.ApprovedBy = "pm"
		tieCanonical.ApprovedAt = &tieApprovedAt
		if err := artifactRepo.Insert(ctx, tieCanonical); err != nil {
			t.Fatal(err)
		}
		tieFeature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:                  "feature-knowledge-tie",
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
		draftCanonical.ApprovedBy = "pm"
		draftCanonical.ApprovedAt = &draftApprovedAt
		if err := artifactRepo.Insert(ctx, draftCanonical); err != nil {
			t.Fatal(err)
		}
		draftFeature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:                  "feature-knowledge-draft",
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

		tieWarnings, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{FeatureID: tieFeature.ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(tieWarnings) != 0 {
			t.Fatalf("tie warnings = %+v, want none", tieWarnings)
		}

		draftWarnings, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{FeatureID: draftFeature.ID})
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
		ctx := context.Background()
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

func TestWorkBoardRepository_UpdateFeaturePatchesStatusWithoutBlankingNameOrSummary(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := context.Background()
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

func TestWorkBoardRepository_DeleteFeatureDisabled(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := context.Background()

		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:     "feat-delete",
			Key:    "FEAT-DELETE",
			Name:   "To delete",
			Status: workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}
		err = repo.DeleteFeature(ctx, feature.ID)
		if !errors.Is(err, workboard.ErrValidation) {
			t.Fatalf("delete feature: want ErrValidation, got %v", err)
		}
	})
}

func TestWorkBoardRepository_DeleteChangeRequestDisabled(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := context.Background()

		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:     "feat-cr-only",
			Key:    "FEAT-CR-ONLY",
			Name:   "Feature stays",
			Status: workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			ID:                 "cr-only",
			Key:                "CR-ONLY",
			FeatureID:          feature.ID,
			Title:              "Standalone CR",
			WorkType:           workboard.WorkTypeNewFeature,
			AcceptanceCriteria: `[{"text":"Done","done":false}]`,
		})
		if err != nil {
			t.Fatal(err)
		}

		err = repo.DeleteChangeRequest(ctx, cr.ID)
		if !errors.Is(err, workboard.ErrValidation) {
			t.Fatalf("delete CR: want ErrValidation, got %v", err)
		}
	})
}

func TestWorkBoardRepository_UpdateFeatureWritesLifecycleAuditOnStatusChange(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := context.Background()
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
		ctx := context.Background()
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
		ctx := context.Background()
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
		ctx := context.Background()
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

func TestWorkBoardRepository_AutoArchivesOnPassedDeliveryReviewWhenEnabled(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := context.Background()
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
		archived, err := repo.GetChangeRequest(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !archived.Archived {
			t.Fatal("delivery pass should archive when auto-archive is enabled")
		}
		if archived.ArchivedBy != "specgate-auto-archive" {
			t.Fatalf("ArchivedBy=%q", archived.ArchivedBy)
		}
		if archived.ArchiveReason != "delivery review passed" {
			t.Fatalf("ArchiveReason=%q", archived.ArchiveReason)
		}
		var events []workboard.LifecycleEvent
		if err := gdb.Where("entity_kind = ? AND entity_id = ? AND event_type = ?", "change_request", cr.ID, "change_request.archived").
			Find(&events).Error; err != nil {
			t.Fatal(err)
		}
		if len(events) != 1 || events[0].Actor != "specgate-auto-archive" {
			t.Fatalf("expected one auto archive lifecycle event, got %+v", events)
		}
	})
}

func TestWorkBoardRepository_ListStaleWarnings_DeliveryInProgress(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		integRepo := NewIntegrationRepository(gdb)
		ctx := context.Background()

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
		ctx := context.Background()
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
		lead := newArtifact("art-phase-lead", feature.Key, "v1.0", artifact.StatusDraft, now)
		if err := artifactRepo.Insert(ctx, lead); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.PatchLeadArtifact(ctx, cr.ID, lead.ID); err != nil {
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
		approvedLead := newArtifact("art-phase-lead-approved", feature.Key, "v1.2", artifact.StatusApproved, now.Add(time.Minute))
		if err := artifactRepo.Insert(ctx, approvedLead); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.PatchLeadArtifact(ctx, cr.ID, approvedLead.ID); err != nil {
			t.Fatal(err)
		}
		got, err = repo.GetChangeRequest(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Phase != workboard.BoardPhaseReady {
			t.Fatalf("approved lead artifact: phase = %q, want Ready", got.Phase)
		}

		// Handoff: a context pack pointer wins over the lead artifact.
		pack := newArtifact("art-phase-pack", feature.Key, "v1.1", artifact.StatusApproved, now)
		if err := artifactRepo.Insert(ctx, pack); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.PatchContextPackArtifact(ctx, cr.ID, pack.ID); err != nil {
			t.Fatal(err)
		}
		got, err = repo.GetChangeRequest(ctx, cr.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Phase != workboard.BoardPhaseHandoff {
			t.Fatalf("context pack: phase = %q, want Handoff", got.Phase)
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
				if item.Phase != workboard.BoardPhaseHandoff {
					t.Fatalf("list: phase = %q, want Handoff", item.Phase)
				}
			}
		}
		if !found {
			t.Fatal("change request not returned by ListChangeRequests")
		}
	})
}

func TestWorkBoardRepository_ContextPackStaleWarning(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		integRepo := NewIntegrationRepository(gdb)
		ctx := context.Background()

		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			Key:    "FEAT-PACK-STALE",
			Name:   "Pack stale feature",
			Status: workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}
		cr, err := repo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			Key:                   "CR-PACK-STALE",
			FeatureID:             feature.ID,
			WorkType:              workboard.WorkTypeFeatureChange,
			Title:                 "Pack stale work",
			ContextPackArtifactID: "pack-1",
		})
		if err != nil {
			t.Fatal(err)
		}
		integration, err := integRepo.CreateIntegration(ctx, integrations.Integration{
			Provider: integrations.ProviderGitLab,
			Name:     "Pack stale GitLab",
			Status:   integrations.StatusConnected,
		})
		if err != nil {
			t.Fatal(err)
		}

		warnings, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{ChangeRequestID: cr.ID})
		if err != nil {
			t.Fatal(err)
		}
		for _, warning := range warnings {
			if warning.Code == workboard.WarningContextPackStale {
				t.Fatal("did not expect context_pack_stale before feedback arrives")
			}
		}

		if _, err := integRepo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
			IntegrationID:   integration.ID,
			FeatureID:       feature.ID,
			ChangeRequestID: cr.ID,
			EventType:       integrations.FeedbackEventContextPackStale,
			PayloadJSON:     `{"provider":"gitlab","correlation_id":"CR-PACK-STALE"}`,
			Status:          integrations.FeedbackStatusPending,
			Reason:          "delivery changed after context pack handoff",
		}); err != nil {
			t.Fatal(err)
		}

		warnings, err = repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{ChangeRequestID: cr.ID})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, warning := range warnings {
			if warning.Code != workboard.WarningContextPackStale {
				continue
			}
			found = true
			if warning.Severity != "warning" {
				t.Fatalf("context_pack_stale severity = %q, want warning", warning.Severity)
			}
			if warning.Message == "" {
				t.Fatal("context_pack_stale message is empty")
			}
		}
		if !found {
			t.Fatalf("expected context_pack_stale warning, got %+v", warnings)
		}

		allWarnings, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{})
		if err != nil {
			t.Fatal(err)
		}
		found = false
		for _, warning := range allWarnings {
			if warning.Code == workboard.WarningContextPackStale && warning.ChangeRequestID == cr.ID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected aggregate stale warnings to include context_pack_stale, got %+v", allWarnings)
		}
	})
}

func TestWorkBoardRepository_ChangeRequestTrackerStatusReflectsLatestEvent(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		integRepo := NewIntegrationRepository(gdb)
		ctx := context.Background()

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
			Status:        integrations.FeedbackStatusPending,
			CreatedAt:     older,
		}); err != nil {
			t.Fatal(err)
		}
		// A newer "completed" tracker event correlated by CR id.
		if _, err := integRepo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
			IntegrationID: integration.ID,
			EventType:     integrations.FeedbackEventTrackerStatusChanged,
			PayloadJSON:   trackerFeedbackPayload(t, "completed", cr.ID),
			Status:        integrations.FeedbackStatusPending,
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
		ctx := context.Background()

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
				Status:        integrations.FeedbackStatusPending,
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
		ctx := context.Background()

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

		// setup creates a feature + CR with no context_pack_artifact_id (not handed off)
		// and a Linear integration. Suffix keeps rows isolated between subtests.
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
				// ContextPackArtifactID intentionally empty — not yet handed off.
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
				Status:        integrations.FeedbackStatusPending,
			}); err != nil {
				t.Fatal(err)
			}
		}

		t.Run("priority 1 (urgent) no handoff -> warning fires", func(t *testing.T) {
			repo, integRepo, cr, integration := setup(t, "A")
			emitTrackerWithPriority(t, integRepo, integration.ID, "started", cr.Key, 1)
			if !hasPriorityWarning(t, repo, cr.ID) {
				t.Fatal("expected tracker_priority_urgent for urgent priority with no handoff")
			}
		})

		t.Run("priority 2 (high) no handoff -> warning fires", func(t *testing.T) {
			repo, integRepo, cr, integration := setup(t, "B")
			emitTrackerWithPriority(t, integRepo, integration.ID, "started", cr.Key, 2)
			if !hasPriorityWarning(t, repo, cr.ID) {
				t.Fatal("expected tracker_priority_urgent for high priority with no handoff")
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

		t.Run("priority 1 but CR handed off -> warning absent", func(t *testing.T) {
			repo, integRepo, cr, integration := setup(t, "E")
			emitTrackerWithPriority(t, integRepo, integration.ID, "started", cr.Key, 1)
			// Simulate handoff by setting context_pack_artifact_id.
			if err := gdb.Model(&workboard.ChangeRequest{}).
				Where("id = ?", cr.ID).
				Updates(map[string]any{"context_pack_artifact_id": "artifact-handoff-E"}).Error; err != nil {
				t.Fatal(err)
			}
			if hasPriorityWarning(t, repo, cr.ID) {
				t.Fatal("did not expect tracker_priority_urgent when CR is already handed off")
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

func TestWorkBoardRepository_SetFeatureSummary(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:        "feature-summary",
			Key:       "FEAT-SUMMARY",
			Name:      "Summary",
			Status:    workboard.FeatureStatusActive,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}

		updated, err := repo.SetFeatureSummary(ctx, feature.ID, "# Overview\nbody", "v0.2")
		if err != nil {
			t.Fatal(err)
		}
		if updated.SummaryMD != "# Overview\nbody" || updated.SummarySourceVersion != "v0.2" {
			t.Fatalf("summary not persisted on returned row: %+v", updated)
		}

		reloaded, err := repo.GetFeature(ctx, feature.ID)
		if err != nil {
			t.Fatal(err)
		}
		if reloaded.SummaryMD != "# Overview\nbody" || reloaded.SummarySourceVersion != "v0.2" {
			t.Fatalf("summary not persisted on reload: %+v", reloaded)
		}

		if _, err := repo.SetFeatureSummary(ctx, "missing-feature", "x", "v1"); !errors.Is(err, workboard.ErrNotFound) {
			t.Fatalf("expected ErrNotFound for missing feature, got %v", err)
		}
	})
}

func TestWorkBoardRepository_StaleWarningsFeatureSummaryOutdated(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		artifactRepo := NewRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		art := newArtifact("art-summary", "FEAT-SUM-WARN", "v0.3", artifact.StatusApproved, now)
		art.ApprovedBy = "pm"
		art.ApprovedAt = &now
		if err := artifactRepo.Insert(ctx, art); err != nil {
			t.Fatal(err)
		}
		feature, err := repo.CreateFeature(ctx, workboard.Feature{
			ID:                  "feature-sum-warn",
			Key:                 "FEAT-SUM-WARN",
			Name:                "Summary Warn",
			Status:              workboard.FeatureStatusActive,
			Version:             1,
			CanonicalArtifactID: "art-summary",
			CreatedAt:           now,
			UpdatedAt:           now,
		})
		if err != nil {
			t.Fatal(err)
		}

		hasOutdated := func(t *testing.T) bool {
			t.Helper()
			warnings, err := repo.ListStaleWarnings(ctx, workboard.StaleWarningFilter{FeatureID: feature.ID})
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range warnings {
				if w.Code == workboard.WarningFeatureSummaryOutdated {
					return true
				}
			}
			return false
		}

		// No summary yet -> no warning.
		if hasOutdated(t) {
			t.Fatal("did not expect outdated warning before a summary exists")
		}

		// Summary generated from the current canonical version -> no warning.
		if _, err := repo.SetFeatureSummary(ctx, feature.ID, "# Overview", "v0.3"); err != nil {
			t.Fatal(err)
		}
		if hasOutdated(t) {
			t.Fatal("did not expect outdated warning when source version matches canonical")
		}

		// Summary generated from an older canonical version -> warning.
		if _, err := repo.SetFeatureSummary(ctx, feature.ID, "# Overview", "v0.1"); err != nil {
			t.Fatal(err)
		}
		if !hasOutdated(t) {
			t.Fatal("expected outdated warning when source version differs from canonical")
		}
	})
}

func TestWorkBoardRepository_UpsertFeatureByKey(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		repo := NewWorkBoardRepository(gdb)
		ctx := context.Background()

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
		ctx := context.Background()
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
			t.Fatal("expected no delivery_stale warning when latest delivery review passed")
		}
		deleteGateRuns(t)

		// Latest delivery review failed but within threshold (threshold = 7, age = 0s).
		repo.SetDeliverySLADays(7)
		if err := gdb.Create(&workboard.GateRun{
			ID:          "%s",
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

		// Latest delivery review failed and older than threshold (threshold = 0).
		repo.SetDeliverySLADays(0)
		if err := gdb.Create(&workboard.GateRun{
			ID:          "%s",
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
			t.Fatal("expected delivery_stale warning when latest delivery review is a stale fail")
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
	})
}

// TestWorkBoardRepository_TrackerConflictWarning_UsesProviderName verifies
// that the tracker_status_conflict warning message names the specific provider
// (e.g. "GitHub") rather than the hardcoded "Linear" text.
func TestWorkBoardRepository_TrackerConflictWarning_UsesProviderName(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
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
			Status:        integrations.FeedbackStatusPending,
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
