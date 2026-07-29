package command

import (
	"strings"
	"testing"
)

// Quick work never runs the artifact route's verifiability gate, so the only
// honest signal available at creation time is how many criteria have a check
// standing behind them. These cases pin the three states a human needs to tell
// apart, and pin that the notice stays a count rather than becoming a judgement
// about criterion wording.
func TestCriteriaEnforcementNoticeReportsBindingCoverage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		criteria []string
		want     []string
		absent   []string
	}{
		{
			name:     "no criteria stays silent",
			criteria: nil,
			want:     []string{""},
		},
		{
			name:     "every criterion bound",
			criteria: []string{"Tags persist @check:unit", "Filter works @check:unit"},
			want:     []string{"all 2 criteria are bound"},
			absent:   []string{"agent's claim"},
		},
		{
			name:     "none bound names the consequence and the fix",
			criteria: []string{"Tags persist", "Error message is friendly"},
			want:     []string{"none of the 2", "agent's claims", "@check:"},
		},
		{
			name:     "partial coverage reports both sides",
			criteria: []string{"Tags persist @check:unit", "Error message is friendly"},
			want:     []string{"1 of 2", "agent's claim"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := criteriaEnforcementNotice(tc.criteria)
			for _, want := range tc.want {
				if want == "" {
					if got != "" {
						t.Fatalf("notice = %q, want empty", got)
					}
					continue
				}
				if !strings.Contains(got, want) {
					t.Fatalf("notice = %q, want it to contain %q", got, want)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(got, absent) {
					t.Fatalf("notice = %q, want it to omit %q", got, absent)
				}
			}
		})
	}
}

// A criterion whose binding cannot be resolved must not be counted as enforced.
// Counting it would tell a human a check stands behind a criterion when review
// will fall back to the agent's claim.
func TestCriteriaEnforcementNoticeIgnoresAmbiguousBindings(t *testing.T) {
	t.Parallel()
	got := criteriaEnforcementNotice([]string{"Tags persist @check:unit @check:integration"})
	if !strings.Contains(got, "none of the 1") {
		t.Fatalf("notice = %q, want an ambiguous binding to count as unbound", got)
	}
}
