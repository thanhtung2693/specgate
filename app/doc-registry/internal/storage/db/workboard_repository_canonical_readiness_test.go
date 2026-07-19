package db

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/workboard"
)

// Promoting a canonical artifact stamps the server-derived readiness state +
// override flag onto the feature.canonical_changed event payload.
func TestCanonicalEventCarriesReadinessState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		runs           []artifact.ReadinessRun // Gate + State; ID/ArtifactID/CreatedAt set in setup
		wantState      string
		wantOverridden bool
	}{
		{"no runs", nil, "not_run", true},
		{"all pass", []artifact.ReadinessRun{{Gate: "scope_clear", State: artifact.ReadinessStatePass}}, "pass", false},
		{"one warn", []artifact.ReadinessRun{
			{Gate: "scope_clear", State: artifact.ReadinessStatePass},
			{Gate: "required_roles_present", State: artifact.ReadinessStateWarn},
		}, "warn", true},
		{"ide agent warn", []artifact.ReadinessRun{
			{Gate: "scope_clear", State: artifact.ReadinessStateWarn, Executor: workboard.GateRunExecutorIDEAgent},
		}, "warn", true},
		{"human decision stays separate", []artifact.ReadinessRun{
			{Gate: "scope_clear", State: artifact.ReadinessStatePass},
			{Gate: "scope_clear", State: artifact.ReadinessStateFail, Executor: workboard.GateRunExecutorHuman},
		}, "pass", false},
	}
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := artifact.WithWorkspace(workboard.WithWorkspace(context.Background(), "ws-test"), "ws-test")
		repo := NewWorkBoardRepository(gdb)
		for _, tc := range cases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				now := time.Now().UTC()
				feat, err := repo.CreateFeature(ctx, workboard.Feature{Key: "FEAT-" + tc.name, Name: "F"})
				if err != nil {
					t.Fatal(err)
				}
				art := artifact.Artifact{
					ID: "art-" + tc.name, WorkspaceID: "ws-test", FeatureID: feat.ID, Version: "1",
					Status: artifact.StatusApproved, RequestType: artifact.RequestTypeBugfix,
					ImpactLevel: artifact.ImpactLevelMedium, CreatedBy: "test",
					CreatedAt: now, UpdatedAt: now,
				}
				if err := gdb.Create(&art).Error; err != nil {
					t.Fatalf("create artifact: %v", err)
				}
				for i, r := range tc.runs {
					if r.Executor != "" && r.Executor != workboard.GateRunExecutorPlatform {
						if err := gdb.Create(&workboard.GateRun{
							ID:          art.ID + "-run-" + r.Gate + "-" + strconv.Itoa(i),
							WorkspaceID: "ws-test",
							SubjectKind: workboard.GateRunSubjectArtifact,
							SubjectID:   art.ID,
							Gate:        r.Gate,
							State:       workboard.NextActionState(r.State),
							Executor:    r.Executor,
							CreatedAt:   now.Add(time.Duration(i) * time.Second),
						}).Error; err != nil {
							t.Fatalf("create %s run: %v", r.Executor, err)
						}
						continue
					}
					rows := []artifact.ReadinessRun{{
						ID: art.ID + "-run-" + r.Gate + "-" + strconv.Itoa(i), ArtifactID: art.ID, Gate: r.Gate,
						State: r.State, Executor: r.Executor, EvidenceJSON: "{}", CreatedAt: now.Add(time.Duration(i) * time.Second),
					}}
					if err := NewRepository(gdb).InsertReadinessRuns(ctx, rows); err != nil {
						t.Fatalf("create run: %v", err)
					}
				}
				if _, err := repo.SetFeatureCanonicalArtifact(ctx, feat.ID, art.ID, "tester"); err != nil {
					t.Fatalf("promote: %v", err)
				}
				var ev artifact.Event
				if err := gdb.Where("artifact_id = ? AND event_type = ?", art.ID, "feature.canonical_changed").
					Order("created_at DESC").First(&ev).Error; err != nil {
					t.Fatalf("load event: %v", err)
				}
				var payload map[string]any
				if err := json.Unmarshal([]byte(ev.Payload), &payload); err != nil {
					t.Fatal(err)
				}
				if payload["readiness_state"] != tc.wantState {
					t.Errorf("readiness_state = %v, want %v", payload["readiness_state"], tc.wantState)
				}
				if payload["readiness_overridden"] != tc.wantOverridden {
					t.Errorf("readiness_overridden = %v, want %v", payload["readiness_overridden"], tc.wantOverridden)
				}
			})
		}
	})
}

// Promotion's feature.canonical_changed event must join the artifact's
// tamper-evidence chain — an unchained row after chained rows reads as
// tampering (spec §8.2).
func TestCanonicalPromotionEventIsChained(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := artifact.WithWorkspace(workboard.WithWorkspace(context.Background(), "ws-test"), "ws-test")
		wb := NewWorkBoardRepository(gdb)
		repo := NewRepository(gdb)
		now := time.Now().UTC()

		feat, err := wb.CreateFeature(ctx, workboard.Feature{Key: "FEAT-CHAINPROM", Name: "F"})
		if err != nil {
			t.Fatal(err)
		}
		art := &artifact.Artifact{
			ID: "art-chainprom", WorkspaceID: "ws-test", FeatureID: feat.ID, Version: "1",
			Status: artifact.StatusApproved, RequestType: artifact.RequestTypeBugfix,
			ImpactLevel: artifact.ImpactLevelMedium, CreatedBy: "test",
			CreatedAt: now, UpdatedAt: now,
		}
		// Publish through the chained path so a prior chained event exists.
		if err := repo.InsertWithEvent(ctx, art, artifact.Event{
			ID: "chainprom-ev-1", ArtifactID: art.ID, EventType: artifact.EventPublished,
			Payload: `{}`, CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := wb.SetFeatureCanonicalArtifact(ctx, feat.ID, art.ID, "tester"); err != nil {
			t.Fatal(err)
		}
		events, err := repo.ListEvents(ctx, artifact.EventFilter{ArtifactID: art.ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(events) < 2 {
			t.Fatalf("events = %d, want >= 2 (publish + canonical_changed)", len(events))
		}
		if report := artifact.VerifyEventChain(events); report.State != artifact.ChainIntact {
			t.Fatalf("promotion event broke the chain: %+v", report)
		}
	})
}
