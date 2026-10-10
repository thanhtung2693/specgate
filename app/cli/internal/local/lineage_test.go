package local_test

import (
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/local"
)

func TestLineageRequiresEveryBaseRequirementAndExplicitAdditions(t *testing.T) {
	s, err := local.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	sel, err := s.Initialize(t.Context(), local.InitInput{WorkspaceName: "Lineage", Username: "human", DisplayName: "Human"})
	if err != nil {
		t.Fatal(err)
	}
	base, err := s.PublishArtifact(t.Context(), sel.Workspace.ID, local.ArtifactInput{FeatureKey: "LINEAGE", RequestType: "new_feature", Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("base")}}, SourceCriteria: []local.SourceCriterion{{ID: "req-a", Text: "A", SourcePath: "spec.md"}, {ID: "req-b", Text: "B", SourcePath: "spec.md"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.PublishArtifact(t.Context(), sel.Workspace.ID, local.ArtifactInput{FeatureKey: "LINEAGE", RequestType: "change_request", BaseVersion: "v1", Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("target")}}, SourceCriteria: []local.SourceCriterion{{ID: "req-a", Text: "A2", SourcePath: "spec.md"}, {ID: "req-c", Text: "C", SourcePath: "spec.md"}}, SourceLineage: &local.SourceLineage{Version: 1, BaseArtifactID: base.ID, BaseDigest: base.SnapshotDigest, Rows: []local.LineageRow{{BaseID: "req-a", TargetIDs: []string{"req-a"}}}}})
	if err == nil || !strings.Contains(err.Error(), "every base") {
		t.Fatalf("incomplete lineage error = %v", err)
	}
}

func TestImpactDoesNotTraverseNonAdjacentOrReversePairs(t *testing.T) {
	s, err := local.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	sel, err := s.Initialize(t.Context(), local.InitInput{WorkspaceName: "Impact", Username: "human", DisplayName: "Human"})
	if err != nil {
		t.Fatal(err)
	}
	base, err := s.PublishArtifact(t.Context(), sel.Workspace.ID, local.ArtifactInput{FeatureKey: "IMPACT", RequestType: "new_feature", Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("base")}}, SourceCriteria: []local.SourceCriterion{{ID: "req-a", Text: "A", SourcePath: "spec.md"}}})
	if err != nil {
		t.Fatal(err)
	}
	target, err := s.PublishArtifact(t.Context(), sel.Workspace.ID, local.ArtifactInput{FeatureKey: "IMPACT", RequestType: "change_request", BaseVersion: "v1", Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("target")}}, SourceCriteria: []local.SourceCriterion{{ID: "req-b", Text: "B", SourcePath: "spec.md"}}, SourceLineage: &local.SourceLineage{Version: 1, BaseArtifactID: base.ID, BaseDigest: base.SnapshotDigest, Rows: []local.LineageRow{{BaseID: "req-a", TargetIDs: []string{"req-b"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	impact, err := s.ArtifactImpact(t.Context(), sel.Workspace.ID, target.ID, base.ID)
	if err != nil || impact.RequirementImpact != "declared" || len(impact.Transitions) != 1 {
		t.Fatalf("impact = %#v %v", impact, err)
	}
	if impact.BaseSnapshotDigest != base.SnapshotDigest || impact.TargetSnapshotDigest != target.SnapshotDigest || impact.BaseStatus != base.Status || impact.TargetStatus != target.Status || len(impact.DocumentDelta.Modified) != 1 {
		t.Fatalf("impact projection incomplete: %#v", impact)
	}
	transition := impact.Transitions[0]
	if transition.Before.Text != "A" || len(transition.After) != 1 || transition.After[0].Text != "B" || len(transition.ChangedFields) != 2 || transition.ChangedFields[0] != "id" || transition.ChangedFields[1] != "text" {
		t.Fatalf("literal source delta missing: %#v", transition)
	}
	reverse, err := s.ArtifactImpact(t.Context(), sel.Workspace.ID, base.ID, target.ID)
	if err != nil || reverse.RequirementImpact != "unavailable" {
		t.Fatalf("reverse = %#v %v", reverse, err)
	}
	third, err := s.PublishArtifact(t.Context(), sel.Workspace.ID, local.ArtifactInput{FeatureKey: "IMPACT", RequestType: "change_request", BaseVersion: "v2",
		Documents:      []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("third")}},
		SourceCriteria: []local.SourceCriterion{{ID: "req-c", Text: "C", SourcePath: "spec.md"}},
		SourceLineage:  &local.SourceLineage{Version: 1, BaseArtifactID: target.ID, BaseDigest: target.SnapshotDigest, Rows: []local.LineageRow{{BaseID: "req-b", TargetIDs: []string{"req-c"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	nonadjacent, err := s.ArtifactImpact(t.Context(), sel.Workspace.ID, third.ID, base.ID)
	if err != nil || nonadjacent.RequirementImpact != "unavailable" || len(nonadjacent.Transitions) != 0 || len(nonadjacent.DocumentDelta.Modified) != 1 {
		t.Fatalf("nonadjacent inferred lineage or lost file delta: %#v %v", nonadjacent, err)
	}
}

func TestImpactReportsOnlyDeclaredSplitMergeRemovalAndAddition(t *testing.T) {
	s, err := local.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	sel, err := s.Initialize(t.Context(), local.InitInput{WorkspaceName: "Lineage transitions", Username: "human", DisplayName: "Human"})
	if err != nil {
		t.Fatal(err)
	}
	base, err := s.PublishArtifact(t.Context(), sel.Workspace.ID, local.ArtifactInput{
		FeatureKey: "TRANSITIONS", RequestType: "new_feature",
		Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("base")}},
		SourceCriteria: []local.SourceCriterion{
			{ID: "old-a", Text: "Split this obligation", SourcePath: "spec.md"},
			{ID: "old-b", Text: "Merge obligation B", SourcePath: "spec.md"},
			{ID: "old-c", Text: "Merge obligation C", SourcePath: "spec.md"},
			{ID: "old-remove", Text: "Remove this obligation", SourcePath: "spec.md"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	target, err := s.PublishArtifact(t.Context(), sel.Workspace.ID, local.ArtifactInput{
		FeatureKey: "TRANSITIONS", RequestType: "change_request", BaseVersion: "v1",
		Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("target")}},
		SourceCriteria: []local.SourceCriterion{
			{ID: "new-a", Text: "Split this obligation", SourcePath: "spec.md"},
			{ID: "new-b", Text: "Split detail", SourcePath: "spec.md"},
			{ID: "new-c", Text: "Merged obligations", SourcePath: "spec.md"},
			{ID: "new-add", Text: "New obligation", SourcePath: "spec.md"},
		},
		SourceLineage: &local.SourceLineage{Version: 1, BaseArtifactID: base.ID, BaseDigest: base.SnapshotDigest,
			Rows: []local.LineageRow{
				{BaseID: "old-a", TargetIDs: []string{"new-a", "new-b"}, Reason: "split"},
				{BaseID: "old-b", TargetIDs: []string{"new-c"}, Reason: "merge"},
				{BaseID: "old-c", TargetIDs: []string{"new-c"}, Reason: "merge"},
				{BaseID: "old-remove", Reason: "removed"},
			}, Added: []string{"new-add"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	impact, err := s.ArtifactImpact(t.Context(), sel.Workspace.ID, target.ID, base.ID)
	if err != nil {
		t.Fatal(err)
	}
	if impact.RequirementImpact != "declared" || len(impact.Transitions) != 4 || len(impact.AddedRequirements) != 1 || impact.AddedRequirements[0].ID != "new-add" || len(impact.RemovedRequirements) != 1 || impact.RemovedRequirements[0].ID != "old-remove" {
		t.Fatalf("declared transitions = %#v", impact)
	}
	if len(impact.Transitions[0].After) != 2 || impact.Transitions[0].Before.ID != "old-a" || impact.Transitions[0].TargetIDs[0] != "new-a" || impact.Transitions[0].TargetIDs[1] != "new-b" {
		t.Fatalf("split was not literal: %#v", impact.Transitions[0])
	}
	if impact.Transitions[1].TargetIDs[0] != "new-c" || impact.Transitions[2].TargetIDs[0] != "new-c" || impact.Transitions[3].ChangedFields[0] != "removed" {
		t.Fatalf("merge/removal were not literal: %#v", impact.Transitions)
	}
}

func TestLineageSplitRequiresRationale(t *testing.T) {
	s, w, _ := verificationFixture(t)
	base, err := s.PublishArtifact(t.Context(), w.WorkspaceID, local.ArtifactInput{FeatureKey: "SPLIT", RequestType: "new_feature", Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("base")}}, SourceCriteria: []local.SourceCriterion{{ID: "a", Text: "A", SourcePath: "spec.md"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.PublishArtifact(t.Context(), w.WorkspaceID, local.ArtifactInput{FeatureKey: "SPLIT", RequestType: "change_request", BaseVersion: "v1", Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("next")}}, SourceCriteria: []local.SourceCriterion{{ID: "b", Text: "B", SourcePath: "spec.md"}, {ID: "c", Text: "C", SourcePath: "spec.md"}}, SourceLineage: &local.SourceLineage{Version: 1, BaseArtifactID: base.ID, BaseDigest: base.SnapshotDigest, Rows: []local.LineageRow{{BaseID: "a", TargetIDs: []string{"b", "c"}}}}})
	if err == nil {
		t.Fatal("split without rationale accepted")
	}
}

func TestLineageRequiresExplicitEmptyTargetInventory(t *testing.T) {
	s, w, _ := verificationFixture(t)
	base, err := s.PublishArtifact(t.Context(), w.WorkspaceID, local.ArtifactInput{FeatureKey: "LAST-REQ", RequestType: "new_feature", Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("base")}}, SourceCriteria: []local.SourceCriterion{{ID: "only", Text: "old", SourcePath: "spec.md"}}})
	if err != nil {
		t.Fatal(err)
	}
	target := local.ArtifactInput{FeatureKey: "LAST-REQ", RequestType: "change_request", BaseVersion: "v1", Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("target")}}, SourceLineage: &local.SourceLineage{Version: 1, BaseArtifactID: base.ID, BaseDigest: base.SnapshotDigest, Rows: []local.LineageRow{{BaseID: "only", TargetIDs: []string{}, Reason: "removed"}}}}
	if _, err := s.PublishArtifact(t.Context(), w.WorkspaceID, target); err == nil {
		t.Fatal("omitted target inventory accepted as explicit empty")
	}
	target.SourceCriteria = []local.SourceCriterion{}
	empty, err := s.PublishArtifact(t.Context(), w.WorkspaceID, target)
	if err != nil {
		t.Fatalf("explicitly empty target inventory rejected: %v", err)
	}
	_, err = s.PublishArtifact(t.Context(), w.WorkspaceID, local.ArtifactInput{FeatureKey: "LAST-REQ", RequestType: "change_request", BaseVersion: "v2", Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("next")}}, SourceCriteria: []local.SourceCriterion{{ID: "new", Text: "new", SourcePath: "spec.md"}}, SourceLineage: &local.SourceLineage{Version: 1, BaseArtifactID: empty.ID, BaseDigest: empty.SnapshotDigest, Added: []string{"new"}}})
	if err != nil {
		t.Fatalf("explicit empty inventory was not retained as known: %v", err)
	}
}

func TestLineageRejectsUnknownLegacyBaseInventory(t *testing.T) {
	s, w, _ := verificationFixture(t)
	base, err := s.PublishArtifact(t.Context(), w.WorkspaceID, local.ArtifactInput{FeatureKey: "LEGACY-EMPTY", RequestType: "new_feature", Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("legacy")}}})
	if err != nil {
		t.Fatal(err)
	}
	target := local.ArtifactInput{FeatureKey: "LEGACY-EMPTY", RequestType: "change_request", BaseVersion: "v1", Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("target")}}, SourceCriteria: []local.SourceCriterion{{ID: "new", Text: "new", SourcePath: "spec.md"}}, SourceLineage: &local.SourceLineage{Version: 1, BaseArtifactID: base.ID, BaseDigest: base.SnapshotDigest, Added: []string{"new"}}}
	if _, err := s.PublishArtifact(t.Context(), w.WorkspaceID, target); err == nil || !strings.Contains(err.Error(), "inventory is unknown") {
		t.Fatalf("unknown empty base inventory error = %v", err)
	}
	if _, err := s.PreviewArtifactImpact(t.Context(), w.WorkspaceID, base.ID, target); err == nil || !strings.Contains(err.Error(), "inventory is unknown") {
		t.Fatalf("unknown empty base preview error = %v", err)
	}
}

func TestPreviewImpactValidatesLineageAndMatchesStoredPair(t *testing.T) {
	s, w, _ := verificationFixture(t)
	base, err := s.PublishArtifact(t.Context(), w.WorkspaceID, local.ArtifactInput{FeatureKey: "PREVIEW", RequestType: "new_feature", Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("base")}}, SourceCriteria: []local.SourceCriterion{{ID: "old", Text: "old text", SourcePath: "spec.md"}}})
	if err != nil {
		t.Fatal(err)
	}
	target := local.ArtifactInput{FeatureKey: "PREVIEW", RequestType: "change_request", BaseVersion: "v1", Documents: []local.ArtifactDocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("target")}}, SourceCriteria: []local.SourceCriterion{{ID: "new", Text: "new text", SourcePath: "spec.md"}}, SourceLineage: &local.SourceLineage{Version: 1, BaseArtifactID: base.ID, BaseDigest: base.SnapshotDigest, Rows: []local.LineageRow{{BaseID: "old", TargetIDs: []string{"new"}}}}}
	bad := target
	bad.SourceLineage = &local.SourceLineage{Version: 1, BaseArtifactID: base.ID, BaseDigest: base.SnapshotDigest, Rows: []local.LineageRow{{BaseID: "missing", TargetIDs: []string{"new"}}}}
	if _, err := s.PreviewArtifactImpact(t.Context(), w.WorkspaceID, base.ID, bad); err == nil {
		t.Fatal("invalid lineage received a normal impact preview")
	}
	preview, err := s.PreviewArtifactImpact(t.Context(), w.WorkspaceID, base.ID, target)
	if err != nil || preview.RequirementImpact != "declared" || len(preview.Transitions) != 1 || preview.Transitions[0].Before.Text != "old text" {
		t.Fatalf("preview impact = %#v %v", preview, err)
	}
	items, err := s.ListArtifacts(t.Context(), w.WorkspaceID)
	if err != nil || len(items) == 0 {
		t.Fatalf("preview readback: %v", err)
	}
	for _, item := range items {
		if item.FeatureKey == "PREVIEW" && item.ID != base.ID {
			t.Fatalf("preview published artifact: %#v", item)
		}
	}
	published, err := s.PublishArtifact(t.Context(), w.WorkspaceID, target)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := s.ArtifactImpact(t.Context(), w.WorkspaceID, published.ID, base.ID)
	if err != nil || preview.TargetSnapshotDigest != published.SnapshotDigest || preview.Transitions[0].Summary != stored.Transitions[0].Summary {
		t.Fatalf("preview/stored mismatch: %#v %#v %v", preview, stored, err)
	}
	if _, err := s.PreviewArtifactImpact(t.Context(), w.WorkspaceID, base.ID, target); err == nil {
		t.Fatal("stale base preview succeeded")
	}
}
