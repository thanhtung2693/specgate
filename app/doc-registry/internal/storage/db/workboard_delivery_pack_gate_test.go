package db

import (
	"testing"

	"github.com/specgate/doc-registry/internal/workboard"
)

func TestStateForDeliveryPackGate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                string
		contextPackArtifact string
		leadArtifact        string
		want                workboard.NextActionState
	}{
		{
			name:                "stored pack present → pass",
			contextPackArtifact: "some-artifact-id",
			leadArtifact:        "",
			want:                workboard.NextActionStatePass,
		},
		{
			name:                "stored pack present even with lead → pass",
			contextPackArtifact: "pack-id",
			leadArtifact:        "lead-id",
			want:                workboard.NextActionStatePass,
		},
		{
			name:                "full-route (lead but no stored pack) → not_applicable",
			contextPackArtifact: "",
			leadArtifact:        "lead-id",
			want:                workboard.NextActionStateNotApplicable,
		},
		{
			name:                "quick-route (no pack, no lead) → pending",
			contextPackArtifact: "",
			leadArtifact:        "",
			want:                workboard.NextActionStatePending,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := stateForDeliveryPackGate(tc.contextPackArtifact, tc.leadArtifact)
			if got != tc.want {
				t.Errorf("stateForDeliveryPackGate(%q, %q) = %q, want %q",
					tc.contextPackArtifact, tc.leadArtifact, got, tc.want)
			}
		})
	}
}

func TestHintForDeliveryPackGate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                string
		contextPackArtifact string
		leadArtifact        string
		wantContains        string
	}{
		{
			name:                "stored pack → returns artifact ID",
			contextPackArtifact: "pack-123",
			leadArtifact:        "",
			wantContains:        "pack-123",
		},
		{
			name:                "full-route (lead, no pack) → on-demand message",
			contextPackArtifact: "",
			leadArtifact:        "lead-id",
			wantContains:        "on-demand",
		},
		{
			name:                "quick-route (no pack, no lead) → not attached message",
			contextPackArtifact: "",
			leadArtifact:        "",
			wantContains:        "No delivery",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := hintForDeliveryPackGate(tc.contextPackArtifact, tc.leadArtifact)
			if got == "" {
				t.Fatal("hint must not be empty")
			}
			if tc.wantContains != "" {
				found := false
				for i := 0; i+len(tc.wantContains) <= len(got); i++ {
					if got[i:i+len(tc.wantContains)] == tc.wantContains {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("hintForDeliveryPackGate(%q, %q) = %q; want it to contain %q",
						tc.contextPackArtifact, tc.leadArtifact, got, tc.wantContains)
				}
			}
		})
	}
}
