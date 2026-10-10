package local

import (
	"testing"
)

func TestArtifactImpactKeepsLegacyEmptyBaseInventoryUnknown(t *testing.T) {
	s, err := Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	sel, err := s.Initialize(t.Context(), InitInput{WorkspaceName: "Legacy impact", Username: "human", DisplayName: "Human"})
	if err != nil {
		t.Fatal(err)
	}
	base := Artifact{ID: "legacy-base", FeatureKey: "LEGACY", SnapshotDigest: "base-digest"}
	target := Artifact{ID: "legacy-target", FeatureKey: "LEGACY", SnapshotDigest: "target-digest", SourceLineage: &SourceLineage{Version: 1, BaseArtifactID: base.ID, BaseDigest: base.SnapshotDigest, Added: []string{"new"}}}
	impact, err := artifactImpactPairQuery(t.Context(), s.db, sel.Workspace.ID, target, base)
	if err != nil || impact.RequirementImpact != "unavailable" || impact.RequirementReason != "base requirement inventory is unknown" {
		t.Fatalf("legacy empty inventory impact = %#v %v", impact, err)
	}
}

func TestArtifactImpactListsExactVersionWorkAndSourceLinks(t *testing.T) {
	s, err := Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	sel, err := s.Initialize(t.Context(), InitInput{WorkspaceName: "Impact work", Username: "human", DisplayName: "Human"})
	if err != nil {
		t.Fatal(err)
	}
	base, err := s.PublishArtifact(t.Context(), sel.Workspace.ID, ArtifactInput{FeatureKey: "IMPACT-WORK", RequestType: "new_feature", Documents: []ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("base")}}, SourceCriteria: []SourceCriterion{{ID: "req-a", Text: "A", SourcePath: "spec.md"}}})
	if err != nil {
		t.Fatal(err)
	}
	target, err := s.PublishArtifact(t.Context(), sel.Workspace.ID, ArtifactInput{FeatureKey: "IMPACT-WORK", RequestType: "change_request", BaseVersion: "v1", Documents: []ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("target")}}, SourceCriteria: []SourceCriterion{{ID: "req-b", Text: "B", SourcePath: "spec.md"}, {ID: "req-c", Text: "C", SourcePath: "spec.md"}}, SourceLineage: &SourceLineage{Version: 1, BaseArtifactID: base.ID, BaseDigest: base.SnapshotDigest, Rows: []LineageRow{{BaseID: "req-a", TargetIDs: []string{"req-b"}}}, Added: []string{"req-c"}}})
	if err != nil {
		t.Fatal(err)
	}
	insertWork := func(id, key, artifactID, criteria string) {
		t.Helper()
		if _, err := s.db.ExecContext(t.Context(), `INSERT INTO work_items(id, workspace_id, key, artifact_id, title, description, phase, context_digest, acceptance_criteria, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, sel.Workspace.ID, key, artifactID, key, "", "ready", "context", criteria, "2026-09-20T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	insertWork("base-linked", "LOCAL-BASE", base.ID, `["Preserve A @source:req-a"]`)
	insertWork("base-unlinked", "LOCAL-MANUAL", base.ID, `["Manual inspection"]`)
	insertWork("target-linked", "LOCAL-TARGET", target.ID, `["Deliver B @source:req-b"]`)
	for _, report := range []struct{ id, workID, repo string }{{"report-base", "base-linked", "https://example.test/one.git"}, {"report-target", "target-linked", "https://example.test/one.git"}} {
		body := `{"affected_files":["src/feature.go"],"git_receipt":{"availability":"available","freshness_scope":"shared_repository","repository":"` + report.repo + `"}}`
		if _, err := s.db.ExecContext(t.Context(), `INSERT INTO delivery_reports(id,workspace_id,work_id,context_digest,body,created_at) VALUES(?,?,?,?,?,?)`, report.id, sel.Workspace.ID, report.workID, "context", body, "2026-09-20T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}

	impact, err := s.ArtifactImpact(t.Context(), sel.Workspace.ID, target.ID, base.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(impact.BaseWorkItems) != 1 || len(impact.TargetWorkItems) != 1 || len(impact.UnlinkedWorkItems) != 1 {
		t.Fatalf("work projection = %#v", impact)
	}
	if got := impact.BaseWorkItems[0]; got.Key != "LOCAL-BASE" || len(got.SourceCriteria) != 1 || got.SourceCriteria[0] != "req-a" || got.Evidence != "not_reviewed" {
		t.Fatalf("base linked work = %#v", got)
	}
	if got := impact.UnlinkedWorkItems[0]; got.Key != "LOCAL-MANUAL" || got.ArtifactID != base.ID {
		t.Fatalf("unlinked work = %#v", got)
	}
	if len(impact.SourceOverlaps) != 1 || impact.SourceOverlaps[0].SourceCriterion != "req-b" || len(impact.SourceOverlaps[0].WorkKeys) != 2 || impact.SourceOverlaps[0].WorkKeys[0] != "LOCAL-BASE" || impact.SourceOverlaps[0].WorkKeys[1] != "LOCAL-TARGET" {
		t.Fatalf("source overlaps = %#v", impact.SourceOverlaps)
	}
	if len(impact.UnassignedRequirements) != 1 || impact.UnassignedRequirements[0].ID != "req-c" || impact.BaseWorkItems[0].Inspection != "inspect_changed_source" {
		t.Fatalf("source/work inspection gaps = %#v", impact)
	}
	if impact.PathOverlapState != "recorded" || len(impact.PathOverlaps) != 1 || impact.PathOverlaps[0].Path != "src/feature.go" || len(impact.PathOverlaps[0].ReportIDs) != 2 {
		t.Fatalf("path overlaps = %#v", impact)
	}
	if _, err := s.db.ExecContext(t.Context(), `UPDATE delivery_reports SET body = ? WHERE id = 'report-target'`, `{"affected_files":["src/feature.go"],"git_receipt":{"availability":"available","freshness_scope":"shared_repository","repository":"https://example.test/two.git"}}`); err != nil {
		t.Fatal(err)
	}
	impact, err = s.ArtifactImpact(t.Context(), sel.Workspace.ID, target.ID, base.ID)
	if err != nil || len(impact.PathOverlaps) != 0 || impact.PathOverlapState != "noncomparable" {
		t.Fatalf("foreign repository path matched: %#v %v", impact, err)
	}
	for _, id := range []string{"report-base", "report-target"} {
		if _, err := s.db.ExecContext(t.Context(), `UPDATE delivery_reports SET body = ? WHERE id = ?`, `{"affected_files":["src/feature.go"],"git_receipt":{"availability":"available","freshness_scope":"local_checkout","checkout_id":"same-checkout"}}`, id); err != nil {
			t.Fatal(err)
		}
	}
	impact, err = s.ArtifactImpact(t.Context(), sel.Workspace.ID, target.ID, base.ID)
	if err != nil || len(impact.PathOverlaps) != 1 || impact.PathOverlapState != "recorded" {
		t.Fatalf("same-checkout path not matched: %#v %v", impact, err)
	}
	if _, err := s.db.ExecContext(t.Context(), `UPDATE delivery_reports SET body = ? WHERE id = 'report-target'`, `{"git_receipt":{"availability":"available","freshness_scope":"local_checkout","checkout_id":"same-checkout"}}`); err != nil {
		t.Fatal(err)
	}
	impact, err = s.ArtifactImpact(t.Context(), sel.Workspace.ID, target.ID, base.ID)
	if err != nil || impact.PathOverlapState != "unknown" {
		t.Fatalf("missing affected-file list not unknown: %#v %v", impact, err)
	}
}
