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
	"github.com/specgate/doc-registry/internal/workboard"
)

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
