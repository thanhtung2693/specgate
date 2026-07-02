package governanceops

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/knowledge"
	"github.com/specgate/doc-registry/internal/workboard"
)

// --- fakes for context-pack tests ---

type fakeContextPackWorkBoard struct {
	cr      *workboard.ChangeRequest
	feature *workboard.Feature
	runs    []workboard.GateRun
	acs     []workboard.AcceptanceCriterion
}

func (f *fakeContextPackWorkBoard) ListChangeRequests(_ context.Context, _ bool) ([]workboard.ChangeRequest, error) {
	return nil, nil
}

func (f *fakeContextPackWorkBoard) GetChangeRequest(_ context.Context, _ string) (*workboard.ChangeRequest, error) {
	if f.cr == nil {
		return nil, workboard.ErrNotFound
	}
	return f.cr, nil
}

func (f *fakeContextPackWorkBoard) GetFeature(_ context.Context, _ string) (*workboard.Feature, error) {
	if f.feature == nil {
		return nil, workboard.ErrNotFound
	}
	return f.feature, nil
}

func (f *fakeContextPackWorkBoard) ListAcceptanceCriteria(_ context.Context, _ string) ([]workboard.AcceptanceCriterion, error) {
	return f.acs, nil
}

func (f *fakeContextPackWorkBoard) ListGateRuns(_ context.Context, _ string, _ int) ([]workboard.GateRun, error) {
	return f.runs, nil
}

func (f *fakeContextPackWorkBoard) ListStaleWarnings(_ context.Context, _ workboard.StaleWarningFilter) ([]workboard.StaleWarning, error) {
	return nil, nil
}

type fakeContextPackArtifactReader struct {
	art   *artifact.Artifact
	files map[string]string
}

func (f *fakeContextPackArtifactReader) Get(_ context.Context, id string) (*artifact.Artifact, error) {
	if f.art == nil || f.art.ID != id {
		return nil, artifact.ErrNotFound
	}
	return f.art, nil
}

func (f *fakeContextPackArtifactReader) FileContent(_ context.Context, _ string, path string) ([]byte, error) {
	body, ok := f.files[path]
	if !ok {
		return nil, artifact.ErrFileNotFound
	}
	return []byte(body), nil
}

type fakeContextPackKnowledgeReader struct {
	docs []knowledge.Document
	err  error
}

func (f *fakeContextPackKnowledgeReader) ListByFeatureOrRequest(_ context.Context, _, _ string) ([]knowledge.Document, error) {
	return f.docs, f.err
}

func newContextPackTestService() *Service {
	cr := &workboard.ChangeRequest{
		ID:                 "cr-1",
		Key:                "CR-1",
		FeatureID:          "feat-1",
		Title:              "Improve checkout",
		WorkType:           workboard.WorkTypeFeatureChange,
		IntentMD:           "Improve flow",
		LeadArtifactID:     "art-lead-1",
		AcceptanceCriteria: `["Total updates in real time","Cannot over-redeem"]`,
	}
	feature := &workboard.Feature{
		ID: "feat-1", Key: "FEAT-1", Name: "Checkout",
	}
	arts := &fakeContextPackArtifactReader{
		art: &artifact.Artifact{ID: "art-lead-1"},
		files: map[string]string{
			artifact.FixedKeyToPath("prd"):      "# PRD\n\nproduct intent",
			artifact.FixedKeyToPath("spec"):     "# Spec\n\napi contract",
			artifact.FixedKeyToPath("tasks_fe"): "Frontend task: build the points panel",
			artifact.FixedKeyToPath("tasks_be"): "- redeem endpoint + ledger",
			artifact.FixedKeyToPath("tasks_qa"): "- boundary tests",
		},
	}
	return &Service{
		WorkBoard: &fakeContextPackWorkBoard{cr: cr, feature: feature},
		Artifacts: arts,
	}
}

// --- parity test: the service assembles a pack with correct state and markdown ---

func TestContextPackReturnsRenderedMarkdownAndWarnings(t *testing.T) {
	t.Parallel()
	svc := newContextPackTestService()
	got, err := svc.ContextPack(context.Background(), ContextPackInput{
		Kind: "change_request",
		ID:   "cr-1",
		Lane: "fe",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "assembled" {
		t.Fatalf("state = %q, want assembled", got.State)
	}
	if !strings.Contains(got.Markdown, "Frontend") {
		t.Fatalf("markdown missing FE lane content: %s", got.Markdown)
	}
	if got.KnowledgeProvenance == nil {
		t.Fatal("KnowledgeProvenance must be non-nil")
	}
	if got.Warnings == nil {
		t.Fatal("Warnings must be non-nil")
	}
}

func TestContextPackLaneScopesTasks(t *testing.T) {
	t.Parallel()
	svc := newContextPackTestService()

	fe, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "change_request", ID: "cr-1", Lane: "fe"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fe.Markdown, "Frontend task") {
		t.Fatalf("fe lane should contain fe content:\n%s", fe.Markdown)
	}
	if strings.Contains(fe.Markdown, "redeem endpoint") {
		t.Fatalf("fe lane should omit be content:\n%s", fe.Markdown)
	}

	be, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "change_request", ID: "cr-1", Lane: "be"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(be.Markdown, "redeem endpoint") {
		t.Fatalf("be lane should contain be content:\n%s", be.Markdown)
	}
	if strings.Contains(be.Markdown, "Frontend task") {
		t.Fatalf("be lane should omit fe content:\n%s", be.Markdown)
	}
}

func TestContextPackNotGeneratedWhenNoArtifact(t *testing.T) {
	t.Parallel()
	svc := &Service{
		WorkBoard: &fakeContextPackWorkBoard{
			cr:      &workboard.ChangeRequest{ID: "cr-2", FeatureID: "feat-1"},
			feature: &workboard.Feature{ID: "feat-1"},
		},
		Artifacts: &fakeContextPackArtifactReader{}, // no artifact
	}
	got, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "change_request", ID: "cr-2"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "not_generated" {
		t.Fatalf("state = %q, want not_generated", got.State)
	}
}

func TestContextPackUsesStoredQuickPackArtifact(t *testing.T) {
	t.Parallel()
	const stored = "# Implementation Context Pack\n\n## What To Build\n\nFix the issue.\n\n## Acceptance Criteria\n\n- _No acceptance criteria captured._\n\n## Risks And Guardrails\n\n- Stay scoped."
	svc := &Service{
		WorkBoard: &fakeContextPackWorkBoard{
			cr: &workboard.ChangeRequest{
				ID:                    "cr-quick",
				FeatureID:             "feat-1",
				ContextPackArtifactID: "pack-1",
			},
			feature: &workboard.Feature{ID: "feat-1"},
			acs: []workboard.AcceptanceCriterion{
				{ID: "ac-1", Text: "Removed readiness links open the first document.", SortOrder: 0},
			},
		},
		Artifacts: &fakeContextPackArtifactReader{
			art: &artifact.Artifact{
				ID: "pack-1",
				Files: []artifact.File{
					{ArtifactID: "pack-1", Path: "implementation-plan.md", Role: artifact.RolePlan},
				},
			},
			files: map[string]string{"implementation-plan.md": stored},
		},
	}

	got, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "change_request", ID: "cr-quick"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "assembled" {
		t.Fatalf("state = %q, want assembled", got.State)
	}
	if got.SourceArtifactID != "pack-1" {
		t.Fatalf("source artifact = %q, want pack-1", got.SourceArtifactID)
	}
	if strings.Count(got.Markdown, "# Implementation Context Pack") != 1 {
		t.Fatalf("stored pack should be returned without nesting:\n%s", got.Markdown)
	}
	if !strings.Contains(got.Markdown, "- Removed readiness links open the first document.") {
		t.Fatalf("normalized acceptance criteria missing:\n%s", got.Markdown)
	}
	if strings.Contains(got.Markdown, "No acceptance criteria captured") {
		t.Fatalf("stale acceptance-criteria placeholder was not replaced:\n%s", got.Markdown)
	}
}

func TestContextPackUsesStoredQuickPackArtifactWithoutFeature(t *testing.T) {
	t.Parallel()
	const stored = "# Implementation Context Pack\n\n## Acceptance Criteria\n\n- _No acceptance criteria captured._"
	svc := &Service{
		WorkBoard: &fakeContextPackWorkBoard{
			cr: &workboard.ChangeRequest{
				ID:                    "cr-featureless",
				ContextPackArtifactID: "pack-featureless",
			},
			acs: []workboard.AcceptanceCriterion{
				{ID: "ac-1", Text: "Quick path stays featureless.", SortOrder: 0},
			},
		},
		Artifacts: &fakeContextPackArtifactReader{
			art: &artifact.Artifact{
				ID: "pack-featureless",
				Files: []artifact.File{
					{ArtifactID: "pack-featureless", Path: "implementation-plan.md", Role: artifact.RolePlan},
				},
			},
			files: map[string]string{"implementation-plan.md": stored},
		},
	}

	got, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "change_request", ID: "cr-featureless"})
	if err != nil {
		t.Fatal(err)
	}
	if got.FeatureID != "" {
		t.Fatalf("FeatureID = %q, want empty", got.FeatureID)
	}
	if got.State != "assembled" {
		t.Fatalf("state = %q, want assembled", got.State)
	}
	if got.SourceArtifactID != "pack-featureless" {
		t.Fatalf("source artifact = %q, want pack-featureless", got.SourceArtifactID)
	}
	if !strings.Contains(got.Markdown, "- Quick path stays featureless.") {
		t.Fatalf("normalized acceptance criteria missing:\n%s", got.Markdown)
	}
}

func TestContextPackForArtifact(t *testing.T) {
	t.Parallel()
	art := &artifact.Artifact{
		ID: "art-x",
		Files: []artifact.File{
			{ArtifactID: "art-x", Path: "spec.md", Role: artifact.RoleSpec},
		},
	}
	svc := &Service{
		Artifacts: &fakeContextPackArtifactReader{
			art:   art,
			files: map[string]string{"spec.md": "# The Spec"},
		},
	}
	got, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "artifact", ID: "art-x"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "assembled" {
		t.Fatalf("state = %q, want assembled", got.State)
	}
	if !strings.Contains(got.Markdown, "## Spec") {
		t.Fatalf("markdown missing Spec section:\n%s", got.Markdown)
	}
}

// --- buildKnowledgeProvenance unit tests (moved from mcp package) ---

func TestBuildKnowledgeProvenance_Empty(t *testing.T) {
	t.Parallel()
	rows, warns := buildKnowledgeProvenance(context.Background(), &fakeContextPackKnowledgeReader{}, "feat-1", "cr-1")
	if len(rows) != 0 {
		t.Errorf("rows = %d, want 0", len(rows))
	}
	if len(warns) != 0 {
		t.Errorf("warns = %v, want none", warns)
	}
}

func TestBuildKnowledgeProvenance_NilReader(t *testing.T) {
	t.Parallel()
	rows, warns := buildKnowledgeProvenance(context.Background(), nil, "feat-1", "cr-1")
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
	rows, warns := buildKnowledgeProvenance(context.Background(), &fakeContextPackKnowledgeReader{docs: []knowledge.Document{doc}}, "feat-1", "cr-1")
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
	rows, warns := buildKnowledgeProvenance(context.Background(), &fakeContextPackKnowledgeReader{docs: []knowledge.Document{doc}}, "feat-1", "cr-1")
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
	rows, _ := buildKnowledgeProvenance(context.Background(), &fakeContextPackKnowledgeReader{docs: docs}, "feat-1", "cr-1")
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
	rows, warns := buildKnowledgeProvenance(context.Background(), kr, "feat-1", "cr-1")
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
	rows, _ := buildKnowledgeProvenance(context.Background(), &fakeContextPackKnowledgeReader{docs: docs}, "feat-1", "cr-1")
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
		ID:                       "art-v1",
		GatesProfileSnapshotJSON: policyV1Snapshot("enhanced"),
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

func TestContextPackForArtifact_GovernanceLevelEmptyForUnversionedSnapshot(t *testing.T) {
	t.Parallel()
	// Snapshot without a governance_level field.
	unversionedSnap := `{"approval_policy":"human_required","enabled_gates":["spec_completeness"]}`
	art := &artifact.Artifact{
		ID:                       "art-unversioned",
		GatesProfileSnapshotJSON: unversionedSnap,
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
	got, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "artifact", ID: "art-unversioned"})
	if err != nil {
		t.Fatal(err)
	}
	if got.GovernanceLevel != "" {
		t.Fatalf("GovernanceLevel = %q, want empty for snapshot without governance level", got.GovernanceLevel)
	}
}

func TestContextPackForCR_GovernanceLevelFromPolicyV1(t *testing.T) {
	t.Parallel()
	art := &artifact.Artifact{
		ID:                       "art-cr-v1",
		GatesProfileSnapshotJSON: policyV1Snapshot("standard"),
	}
	svc := &Service{
		WorkBoard: &fakeContextPackWorkBoard{
			cr: &workboard.ChangeRequest{
				ID:             "cr-gov",
				FeatureID:      "feat-gov",
				LeadArtifactID: "art-cr-v1",
			},
			feature: &workboard.Feature{ID: "feat-gov"},
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

func TestContextPackForCR_GovernanceLevelEmptyForUnversionedSnapshot(t *testing.T) {
	t.Parallel()
	unversionedSnap := `{"approval_policy":"self_approve","enabled_gates":[]}`
	art := &artifact.Artifact{
		ID:                       "art-cr-unversioned",
		GatesProfileSnapshotJSON: unversionedSnap,
	}
	svc := &Service{
		WorkBoard: &fakeContextPackWorkBoard{
			cr: &workboard.ChangeRequest{
				ID:             "cr-leg",
				FeatureID:      "feat-leg",
				LeadArtifactID: "art-cr-unversioned",
			},
			feature: &workboard.Feature{ID: "feat-leg"},
		},
		Artifacts: &fakeContextPackArtifactReader{art: art},
	}
	got, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "change_request", ID: "cr-leg"})
	if err != nil {
		t.Fatal(err)
	}
	if got.GovernanceLevel != "" {
		t.Fatalf("GovernanceLevel = %q, want empty for snapshot without governance level", got.GovernanceLevel)
	}
}
