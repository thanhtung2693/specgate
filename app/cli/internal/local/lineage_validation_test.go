package local

import "testing"

func TestLineageRejectsInvalidDeclarations(t *testing.T) {
	base := []SourceCriterion{{ID: "a"}, {ID: "b"}}
	target := []SourceCriterion{{ID: "x"}, {ID: "y"}}
	valid := func() SourceLineage {
		return SourceLineage{Version: 1, BaseArtifactID: "base", BaseDigest: "digest", Rows: []LineageRow{{BaseID: "a", TargetIDs: []string{"x"}}, {BaseID: "b", TargetIDs: []string{"y"}}}}
	}
	for _, tc := range []struct {
		name   string
		change func(*SourceLineage)
	}{
		{"stale digest", func(l *SourceLineage) { l.BaseDigest = "stale" }},
		{"foreign base", func(l *SourceLineage) { l.BaseArtifactID = "foreign" }},
		{"dangling base", func(l *SourceLineage) { l.Rows[0].BaseID = "missing" }},
		{"dangling target", func(l *SourceLineage) { l.Rows[0].TargetIDs = []string{"missing"} }},
		{"duplicate row", func(l *SourceLineage) { l.Rows[1].BaseID = "a" }},
		{"duplicate target", func(l *SourceLineage) { l.Rows[0].TargetIDs = []string{"x", "x"}; l.Rows[0].Reason = "split" }},
		{"missing removal reason", func(l *SourceLineage) { l.Rows[0].TargetIDs = nil }},
		{"missing merge reason", func(l *SourceLineage) { l.Rows[1].TargetIDs = []string{"x"}; l.Added = []string{"y"} }},
		{"conflicting addition", func(l *SourceLineage) { l.Added = []string{"x"} }},
		{"undeclared addition", func(l *SourceLineage) { l.Rows[1].TargetIDs = nil; l.Rows[1].Reason = "removed" }},
		{"duplicate addition", func(l *SourceLineage) {
			l.Rows[1].TargetIDs = nil
			l.Rows[1].Reason = "removed"
			l.Added = []string{"y", "y"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := valid()
			tc.change(&l)
			if _, err := validateSourceLineage(l, "base", "digest", base, target); err == nil {
				t.Fatal("invalid lineage accepted")
			}
		})
	}
}
