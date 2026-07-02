package db

import (
	"context"
	"encoding/json"
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
	}
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
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
					ID: "art-" + tc.name, FeatureID: feat.ID, Version: "1",
					Status: artifact.StatusApproved, RequestType: artifact.RequestTypeBugfix,
					ImpactLevel: artifact.ImpactLevelMedium, CreatedBy: "test",
					CreatedAt: now, UpdatedAt: now,
				}
				if err := gdb.Create(&art).Error; err != nil {
					t.Fatalf("create artifact: %v", err)
				}
				for i, r := range tc.runs {
					rows := []artifact.ReadinessRun{{
						ID: art.ID + "-run-" + r.Gate, ArtifactID: art.ID, Gate: r.Gate,
						State: r.State, EvidenceJSON: "{}", CreatedAt: now.Add(time.Duration(i) * time.Second),
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
