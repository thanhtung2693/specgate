package governanceops

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/knowledge"
	"github.com/specgate/doc-registry/internal/workboard"
	"github.com/specgate/doc-registry/internal/workspace"
)

// --- fakes for context-pack tests ---

func TestContextPackForArtifact(t *testing.T) {
	t.Parallel()
	art := &artifact.Artifact{
		ID: "art-x",
		Files: []artifact.File{
			{ArtifactID: "art-x", Path: "docs/any-framework/spec.md", Role: artifact.RoleSpec},
			{ArtifactID: "art-x", Path: "docs/any-framework/plan.md", Role: artifact.RolePlan},
		},
	}
	svc := &Service{
		Artifacts: &fakeContextPackArtifactReader{
			art: art,
			files: map[string]string{
				"docs/any-framework/spec.md": "# The Spec",
				"docs/any-framework/plan.md": "# The Plan",
			},
		},
	}
	got, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "artifact", ID: "art-x"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "assembled" {
		t.Fatalf("state = %q, want assembled", got.State)
	}
	for _, want := range []string{"## Spec", "# The Spec", "## Implementation Plan", "# The Plan"} {
		if !strings.Contains(got.Markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, got.Markdown)
		}
	}
}

func TestContextPackForArtifactFailsWhenReaderIsUnavailable(t *testing.T) {
	t.Parallel()
	svc := &Service{}

	_, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "artifact", ID: "art-x"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestContextPackCapsOversizedArtifactContent(t *testing.T) {
	t.Parallel()
	huge := strings.Repeat("specification detail ", 20_000)
	art := &artifact.Artifact{
		ID: "art-large",
		Files: []artifact.File{
			{ArtifactID: "art-large", Path: "spec.md", Role: artifact.RoleSpec},
			{ArtifactID: "art-large", Path: "plan.md", Role: artifact.RolePlan},
		},
	}
	svc := &Service{Artifacts: &fakeContextPackArtifactReader{
		art: art,
		files: map[string]string{
			"spec.md": huge,
			"plan.md": huge,
		},
	}}

	got, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "artifact", ID: art.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Markdown) > maxContextPackChars {
		t.Fatalf("context pack size = %d, want <= %d", len(got.Markdown), maxContextPackChars)
	}
	if !strings.Contains(got.Markdown, contextTruncationMarker) {
		t.Fatalf("oversized context pack did not disclose truncation")
	}
}

func TestContextPackForArtifactRejectsTrustedWorkspaceMismatch(t *testing.T) {
	t.Parallel()
	svc := &Service{Artifacts: &fakeContextPackArtifactReader{
		art:   &artifact.Artifact{ID: "art-b", WorkspaceID: "ws-b"},
		files: map[string]string{},
	}}

	_, err := svc.ContextPack(workspace.WithID(context.Background(), "ws-a"), ContextPackInput{
		Kind: "artifact",
		ID:   "art-b",
	})
	if !errors.Is(err, workboard.ErrNotFound) {
		t.Fatalf("error = %v, want workspace-scoped not found", err)
	}
}

func TestContextPackForCRRejectsCrossWorkspaceSourceArtifact(t *testing.T) {
	t.Parallel()
	readiness := &fakeContextPackReadinessReader{}
	svc := &Service{
		WorkBoard: &fakeContextPackWorkBoard{cr: &workboard.ChangeRequest{
			ID:             "cr-a",
			WorkspaceID:    "ws-a",
			LeadArtifactID: "art-b",
		}},
		Artifacts: &fakeContextPackArtifactReader{
			art:   &artifact.Artifact{ID: "art-b", WorkspaceID: "ws-b"},
			files: map[string]string{"spec.md": "cross-workspace content"},
		},
		ReadinessRuns: readiness,
	}

	_, err := svc.ContextPack(workspace.WithID(context.Background(), "ws-a"), ContextPackInput{
		Kind: "change_request",
		ID:   "cr-a",
	})
	if !errors.Is(err, workboard.ErrNotFound) {
		t.Fatalf("error = %v, want workspace-scoped not found", err)
	}
	if readiness.calls != 0 {
		t.Fatalf("cross-workspace source made %d readiness read(s)", readiness.calls)
	}
}

// --- buildKnowledgeProvenance unit tests ---

func TestBuildKnowledgeProvenance_Empty(t *testing.T) {
	t.Parallel()
	rows, warns := buildKnowledgeProvenance(context.Background(), &fakeContextPackKnowledgeReader{}, "ws-1", []string{"feat-1"}, "cr-1")
	if len(rows) != 0 {
		t.Errorf("rows = %d, want 0", len(rows))
	}
	if len(warns) != 0 {
		t.Errorf("warns = %v, want none", warns)
	}
}

func TestBuildKnowledgeProvenance_NilReader(t *testing.T) {
	t.Parallel()
	rows, warns := buildKnowledgeProvenance(context.Background(), nil, "ws-1", []string{"feat-1"}, "cr-1")
	if rows == nil {
		t.Error("rows should not be nil (spec §2.2 never null)")
	}
	if len(rows) != 0 {
		t.Errorf("rows = %d, want 0", len(rows))
	}
	if len(warns) != 0 {
		t.Errorf("warns = %v, want none", warns)
	}
}

func TestBuildKnowledgeProvenance_UsesWorkspace(t *testing.T) {
	t.Parallel()
	kr := &fakeContextPackKnowledgeReader{docs: []knowledge.Document{}}
	buildKnowledgeProvenance(context.Background(), kr, "ws-a", []string{"feat-1"}, "cr-1")
	if kr.workspaceID != "ws-a" {
		t.Fatalf("workspaceID = %q, want ws-a", kr.workspaceID)
	}
}

func TestBuildKnowledgeProvenance_CurrentDoc(t *testing.T) {
	t.Parallel()
	doc := knowledge.Document{
		DocumentID:     "doc-1",
		Title:          "Payments SRS",
		Version:        "v2",
		DocumentType:   knowledge.DocumentTypeSRS,
		AuthorityLevel: knowledge.AuthoritySourceOfTruth,
		IsLatest:       true,
		CreatedAt:      time.Now(),
	}
	rows, warns := buildKnowledgeProvenance(context.Background(), &fakeContextPackKnowledgeReader{docs: []knowledge.Document{doc}}, "ws-1", []string{"feat-1"}, "cr-1")
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Freshness != "current" {
		t.Errorf("freshness = %q, want current", rows[0].Freshness)
	}
	if rows[0].DocumentID != "doc-1" {
		t.Errorf("document_id = %q, want doc-1", rows[0].DocumentID)
	}
	if rows[0].KnowledgeStoreURI != "specgate://knowledge/doc-1" {
		t.Errorf("knowledge_store_uri = %q", rows[0].KnowledgeStoreURI)
	}
}

func TestBuildKnowledgeProvenance_StaleDoc(t *testing.T) {
	t.Parallel()
	doc := knowledge.Document{
		DocumentID:     "doc-old",
		Title:          "Old Design Doc",
		Version:        "v1",
		DocumentType:   knowledge.DocumentTypeDesignReference,
		AuthorityLevel: knowledge.AuthorityReference,
		IsLatest:       false,
		CreatedAt:      time.Now(),
	}
	rows, warns := buildKnowledgeProvenance(context.Background(), &fakeContextPackKnowledgeReader{docs: []knowledge.Document{doc}}, "ws-1", []string{"feat-1"}, "cr-1")
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Freshness != "stale" {
		t.Errorf("freshness = %q, want stale", rows[0].Freshness)
	}
}

func TestBuildKnowledgeProvenance_MixedAuthority(t *testing.T) {
	t.Parallel()
	docs := []knowledge.Document{
		{DocumentID: "d-ref", Title: "Ref Doc", Version: "v1", DocumentType: knowledge.DocumentTypeSupportingDoc, AuthorityLevel: knowledge.AuthorityReference, IsLatest: true, CreatedAt: time.Now()},
		{DocumentID: "d-sot", Title: "SoT Doc", Version: "v1", DocumentType: knowledge.DocumentTypeSRS, AuthorityLevel: knowledge.AuthoritySourceOfTruth, IsLatest: true, CreatedAt: time.Now()},
		{DocumentID: "d-high", Title: "A High Doc", Version: "v1", DocumentType: knowledge.DocumentTypePolicyDoc, AuthorityLevel: knowledge.AuthorityHigh, IsLatest: true, CreatedAt: time.Now()},
	}
	rows, _ := buildKnowledgeProvenance(context.Background(), &fakeContextPackKnowledgeReader{docs: docs}, "ws-1", []string{"feat-1"}, "cr-1")
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if rows[0].AuthorityLevel != string(knowledge.AuthoritySourceOfTruth) {
		t.Errorf("rows[0].authority = %q, want source_of_truth", rows[0].AuthorityLevel)
	}
	if rows[1].AuthorityLevel != string(knowledge.AuthorityHigh) {
		t.Errorf("rows[1].authority = %q, want high", rows[1].AuthorityLevel)
	}
	if rows[2].AuthorityLevel != string(knowledge.AuthorityReference) {
		t.Errorf("rows[2].authority = %q, want reference", rows[2].AuthorityLevel)
	}
}

func TestBuildKnowledgeProvenance_RepoError(t *testing.T) {
	t.Parallel()
	kr := &fakeContextPackKnowledgeReader{err: errors.New("db timeout")}
	rows, warns := buildKnowledgeProvenance(context.Background(), kr, "ws-1", []string{"feat-1"}, "cr-1")
	if len(rows) != 0 {
		t.Errorf("rows = %d, want 0 on error", len(rows))
	}
	if len(warns) != 1 {
		t.Fatalf("warns = %d, want 1", len(warns))
	}
	if warns[0].Code != "knowledge_provenance_unavailable" {
		t.Errorf("warning code = %q, want knowledge_provenance_unavailable", warns[0].Code)
	}
}

func TestBuildKnowledgeProvenance_DedupsPreferIsLatest(t *testing.T) {
	t.Parallel()
	now := time.Now()
	docs := []knowledge.Document{
		{DocumentID: "doc-dup", Title: "Doc", Version: "v1", DocumentType: knowledge.DocumentTypeSRS, AuthorityLevel: knowledge.AuthorityHigh, IsLatest: false, CreatedAt: now.Add(-time.Hour)},
		{DocumentID: "doc-dup", Title: "Doc", Version: "v2", DocumentType: knowledge.DocumentTypeSRS, AuthorityLevel: knowledge.AuthorityHigh, IsLatest: true, CreatedAt: now},
	}
	rows, _ := buildKnowledgeProvenance(context.Background(), &fakeContextPackKnowledgeReader{docs: docs}, "ws-1", []string{"feat-1"}, "cr-1")
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (dedup)", len(rows))
	}
	if rows[0].Version != "v2" {
		t.Errorf("version = %q, want v2 (is_latest preferred)", rows[0].Version)
	}
	if rows[0].Freshness != "current" {
		t.Errorf("freshness = %q, want current", rows[0].Freshness)
	}
}

// --- GovernanceLevel in context pack (Task 8) ---

// policyV1Snapshot returns a minimal specgate.policy/v1 snapshot JSON for the given level.
func policyV1Snapshot(level string) string {
	return `{"snapshot_schema_version":"specgate.policy/v1","governance_level":"` + level + `","enabled_gates":[],"required_roles":[],"required_topics":[],"approval_policy":"human_required","evidence_policy":"attested_ok"}`
}

func TestContextPackForArtifact_GovernanceLevelFromPolicyV1(t *testing.T) {
	t.Parallel()
	art := &artifact.Artifact{
		ID:                 "art-v1",
		PolicySnapshotJSON: policyV1Snapshot("enhanced"),
		Files: []artifact.File{
			{ArtifactID: "art-v1", Path: "spec.md", Role: artifact.RoleSpec},
		},
	}
	svc := &Service{
		Artifacts: &fakeContextPackArtifactReader{
			art:   art,
			files: map[string]string{"spec.md": "# Spec content"},
		},
	}
	got, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "artifact", ID: "art-v1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.GovernanceLevel != "enhanced" {
		t.Fatalf("GovernanceLevel = %q, want enhanced", got.GovernanceLevel)
	}
}

func TestContextPackListsFrozenApplicableSkillWithoutCatalogReader(t *testing.T) {
	t.Parallel()
	art := &artifact.Artifact{
		ID:                 "art-frozen-skill",
		PolicySnapshotJSON: `{"snapshot_schema_version":"specgate.policy/v1","enabled_gates":["spec_completeness"],"required_roles":[],"required_topics":[],"approval_policy":"human_required","evidence_policy":"attested_ok","gate_skills":{"spec_completeness":"spec-quality-rubric"}}`,
	}
	svc := &Service{Artifacts: &fakeContextPackArtifactReader{art: art}}

	got, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "artifact", ID: art.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Markdown, "## Applicable Skills") || !strings.Contains(got.Markdown, "spec-quality-rubric") {
		t.Fatalf("frozen applicable skill missing without catalog reader:\n%s", got.Markdown)
	}
}

func TestContextPackForArtifactRejectsUnversionedSnapshot(t *testing.T) {
	t.Parallel()
	unversionedSnap := `{"approval_policy":"human_required","enabled_gates":["spec_completeness"]}`
	art := &artifact.Artifact{
		ID:                 "art-unversioned",
		PolicySnapshotJSON: unversionedSnap,
		Files: []artifact.File{
			{ArtifactID: "art-unversioned", Path: "spec.md", Role: artifact.RoleSpec},
		},
	}
	svc := &Service{
		Artifacts: &fakeContextPackArtifactReader{
			art:   art,
			files: map[string]string{"spec.md": "# Unversioned spec"},
		},
	}
	_, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "artifact", ID: "art-unversioned"})
	if !errors.Is(err, governanceprofile.ErrUnsupportedSnapshot) {
		t.Fatalf("error = %v, want ErrUnsupportedSnapshot", err)
	}
}

func TestContextPackForCR_GovernanceLevelFromPolicyV1(t *testing.T) {
	t.Parallel()
	art := &artifact.Artifact{
		ID:                 "art-cr-v1",
		PolicySnapshotJSON: policyV1Snapshot("standard"),
	}
	svc := &Service{
		WorkBoard: &fakeContextPackWorkBoard{
			cr: &workboard.ChangeRequest{
				ID:             "cr-gov",
				FeatureID:      "feat-gov",
				LeadArtifactID: "art-cr-v1",
			},
			feature: &workboard.Feature{ID: "feat-gov"},
			acs: []workboard.AcceptanceCriterion{
				{ID: "ac-gov", Text: "Preserve the governed behavior."},
			},
		},
		Artifacts: &fakeContextPackArtifactReader{art: art},
	}
	got, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "change_request", ID: "cr-gov"})
	if err != nil {
		t.Fatal(err)
	}
	if got.GovernanceLevel != "standard" {
		t.Fatalf("GovernanceLevel = %q, want standard", got.GovernanceLevel)
	}
}

func TestContextPackForCRRejectsUnversionedSnapshot(t *testing.T) {
	t.Parallel()
	unversionedSnap := `{"approval_policy":"human_required","enabled_gates":[]}`
	art := &artifact.Artifact{
		ID:                 "art-cr-unversioned",
		PolicySnapshotJSON: unversionedSnap,
	}
	svc := &Service{
		WorkBoard: &fakeContextPackWorkBoard{
			cr: &workboard.ChangeRequest{
				ID:             "cr-leg",
				FeatureID:      "feat-leg",
				LeadArtifactID: "art-cr-unversioned",
			},
			feature: &workboard.Feature{ID: "feat-leg"},
			acs: []workboard.AcceptanceCriterion{
				{ID: "ac-unversioned", Text: "Preserve the governed behavior."},
			},
		},
		Artifacts: &fakeContextPackArtifactReader{art: art},
	}
	_, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "change_request", ID: "cr-leg"})
	if !errors.Is(err, governanceprofile.ErrUnsupportedSnapshot) {
		t.Fatalf("error = %v, want ErrUnsupportedSnapshot", err)
	}
}
