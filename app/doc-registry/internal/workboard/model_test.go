package workboard

import "testing"

// The route follows the presence of a governed snapshot and nothing else. It used
// to also require work_type == bug_fix, which made a quick item of any other type
// fall through to the artifact-backed path: gates it could never satisfy applied,
// and delivery review blocked it for lacking a policy snapshot it could not have.
func TestIsQuickRouteFollowsTheArtifactNotTheWorkType(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		artifact string
		workType WorkType
		want     bool
	}{
		{"no artifact, bug fix", "", WorkTypeBugFix, true},
		{"no artifact, new feature", "", WorkTypeNewFeature, true},
		{"no artifact, documentation", "", WorkTypeDocumentation, true},
		{"no artifact, no type recorded", "", "", true},
		{"artifact-backed new feature", "artifact-1", WorkTypeNewFeature, false},
		{"artifact-backed bug fix", "artifact-1", WorkTypeBugFix, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cr := ChangeRequest{LeadArtifactID: tc.artifact, WorkType: tc.workType}
			if got := cr.IsQuickRoute(); got != tc.want {
				t.Fatalf("IsQuickRoute() = %v, want %v for artifact=%q type=%q", got, tc.want, tc.artifact, tc.workType)
			}
		})
	}
}
