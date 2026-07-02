package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/artifactattachment"
	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/knowledge"
	"github.com/specgate/doc-registry/internal/skills"
	"github.com/specgate/doc-registry/internal/workboard"
)

// --- fakes (package-level so they can be shared across test helpers) ---

type fakeKnowledgeReader struct {
	docs []knowledge.Document
	err  error
}

func (f *fakeKnowledgeReader) ListByFeatureOrRequest(_ context.Context, _, _ string) ([]knowledge.Document, error) {
	return f.docs, f.err
}

type fakeContextPackAttachments struct {
	byFeature map[string][]artifactattachment.Attachment
}

func (f fakeContextPackAttachments) ListByFeature(_ context.Context, featureID string) ([]artifactattachment.Attachment, error) {
	return f.byFeature[featureID], nil
}

type fakeContextPackSkillsReader struct {
	list []skills.Skill
	err  error
}

func (f fakeContextPackSkillsReader) List(context.Context) ([]skills.Skill, error) {
	return f.list, f.err
}

// fakeContextPackSource implements governanceops.WorkBoardReader for context-pack tests.
type fakeContextPackSource struct {
	cr       *workboard.ChangeRequest
	feature  *workboard.Feature
	warnings []workboard.StaleWarning
	gateRuns []workboard.GateRun
	err      error
}

func (f fakeContextPackSource) ListChangeRequests(_ context.Context, _ bool) ([]workboard.ChangeRequest, error) {
	return nil, nil
}

func (f fakeContextPackSource) GetChangeRequest(context.Context, string) (*workboard.ChangeRequest, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.cr, nil
}

func (f fakeContextPackSource) GetFeature(context.Context, string) (*workboard.Feature, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.feature, nil
}

func (f fakeContextPackSource) ListAcceptanceCriteria(_ context.Context, _ string) ([]workboard.AcceptanceCriterion, error) {
	return nil, nil
}

func (f fakeContextPackSource) ListStaleWarnings(context.Context, workboard.StaleWarningFilter) ([]workboard.StaleWarning, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.warnings, nil
}

func (f fakeContextPackSource) ListGateRuns(context.Context, string, int) ([]workboard.GateRun, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.gateRuns, nil
}

type fakeContextPackArtifacts struct {
	pack  *artifact.Artifact
	files map[string]string
}

func (f fakeContextPackArtifacts) Get(_ context.Context, id string) (*artifact.Artifact, error) {
	if f.pack == nil || f.pack.ID != id {
		return nil, artifact.ErrNotFound
	}
	return f.pack, nil
}

func (f fakeContextPackArtifacts) FileContent(_ context.Context, _ string, path string) ([]byte, error) {
	body, ok := f.files[path]
	if !ok {
		return nil, artifact.ErrFileNotFound
	}
	return []byte(body), nil
}

// newContextPackService wires test fakes into a governanceops.Service for assembly tests.
func newContextPackService(
	source fakeContextPackSource,
	arts fakeContextPackArtifacts,
	atts fakeContextPackAttachments,
	skillReader ContextPackSkillReader,
	kr ContextPackKnowledgeReader,
) *governanceops.Service {
	return &governanceops.Service{
		WorkBoard:   source,
		Artifacts:   arts,
		Attachments: atts,
		Skills:      skillReader,
		Knowledge:   kr,
	}
}

func assembleSource() (fakeContextPackSource, fakeContextPackArtifacts, fakeContextPackAttachments) {
	source := fakeContextPackSource{
		cr: &workboard.ChangeRequest{
			ID: "cr-1", Key: "CR-1", FeatureID: "feat-1", Title: "Improve checkout",
			WorkType: workboard.WorkTypeFeatureChange, IntentMD: "Improve flow",
			AcceptanceCriteria: `["Total updates in real time","Cannot over-redeem"]`,
			LeadArtifactID:     "art-lead-1",
		},
		feature: &workboard.Feature{
			ID: "feat-1", Key: "FEAT-1", Name: "Checkout", Status: workboard.FeatureStatusPlanned,
		},
	}
	arts := fakeContextPackArtifacts{
		pack: &artifact.Artifact{ID: "art-lead-1"},
		files: map[string]string{
			artifact.FixedKeyToPath("prd"):      "# PRD\n\nproduct intent",
			artifact.FixedKeyToPath("spec"):     "# Spec\n\napi contract",
			artifact.FixedKeyToPath("tasks_fe"): "- build the points panel",
			artifact.FixedKeyToPath("tasks_be"): "- redeem endpoint + ledger",
			artifact.FixedKeyToPath("tasks_qa"): "- boundary tests",
			artifact.FixedKeyToPath("manifest"): `{"impacted_services":["checkout-service"],"design_refs":[{"type":"figma","url":"https://figma.com/x"}]}`,
		},
	}
	atts := fakeContextPackAttachments{byFeature: map[string][]artifactattachment.Attachment{
		"FEAT-1": {
			{Kind: artifactattachment.KindLink, URL: "https://stripe.com/docs", Title: "Discount API", Audience: artifactattachment.AudienceCodingAgent},
			{Kind: artifactattachment.KindLink, URL: "https://bug.example/repro", Title: "Repro", Audience: artifactattachment.AudienceGate},
		},
	}}
	return source, arts, atts
}

// --- parseContextPackURI tests (URI parsing stays in the MCP package) ---

func TestParseContextPackURI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		uri      string
		wantKind string
		wantID   string
		wantLane string
		ok       bool
	}{
		// CR-scoped
		{"specgate://context-pack/cr-123", "change_request", "cr-123", "", true},
		{"specgate://context-pack/cr-123/fe", "change_request", "cr-123", "fe", true},
		{"specgate://context-pack/cr-123/be", "change_request", "cr-123", "be", true},
		// Artifact-scoped
		{"specgate://context-pack/artifact/art-456", "artifact", "art-456", "", true},
		// Invalid
		{"specgate://context-pack/cr-123/qa", "", "", "", false},
		{"specgate://context-pack/", "", "", "", false},
		{"specgate://context-pack", "", "", "", false},
		{"specgate://context-pack/a/b/c", "", "", "", false},
		{"specgate://context-pack/artifact/art/extra", "", "", "", false},
		{"specgate://skills/x", "", "", "", false},
	}
	for _, tc := range tests {
		ref, ok := parseContextPackURI(tc.uri)
		if ok != tc.ok {
			t.Fatalf("parseContextPackURI(%q) ok=%v, want %v", tc.uri, ok, tc.ok)
		}
		if ok {
			if ref.Kind != tc.wantKind || ref.ID != tc.wantID || ref.Lane != tc.wantLane {
				t.Fatalf("parseContextPackURI(%q)={Kind:%q, ID:%q, Lane:%q}, want {Kind:%q, ID:%q, Lane:%q}",
					tc.uri, ref.Kind, ref.ID, ref.Lane, tc.wantKind, tc.wantID, tc.wantLane)
			}
		}
	}
}

// --- context-pack assembly tests (via governanceops.Service) ---

func TestContextPackPayload_AssemblesFromSourceArtifact(t *testing.T) {
	t.Parallel()
	source, arts, atts := assembleSource()
	svc := newContextPackService(source, arts, atts, nil, nil)
	got, err := svc.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "change_request", ID: "cr-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "assembled" {
		t.Fatalf("state=%v, want assembled", got.State)
	}
	for _, want := range []string{
		"## Spec", "product intent", "api contract",
		"## Implementation Plan",
		"points panel", "redeem endpoint",
		"## Acceptance Criteria", "Cannot over-redeem",
		"## Reference Attachments", "https://stripe.com/docs",
	} {
		if !strings.Contains(got.Markdown, want) {
			t.Fatalf("assembled markdown missing %q\n---\n%s", want, got.Markdown)
		}
	}
	// gate-only attachments must NOT reach the coding-agent handoff.
	if strings.Contains(got.Markdown, "bug.example") {
		t.Fatalf("gate-only attachment leaked into the handoff:\n%s", got.Markdown)
	}
	if got.SourceArtifactID != "art-lead-1" || got.Lane != "" {
		t.Fatalf("unexpected metadata: source=%q lane=%q", got.SourceArtifactID, got.Lane)
	}
}

func TestContextPackPayload_OutstandingReviewFeedbackOnFail(t *testing.T) {
	t.Parallel()
	source, arts, atts := assembleSource()
	import_json, _ := marshalIndentedJSON(map[string]any{
		"criteria": []map[string]any{
			{"text": "Total updates in real time", "verdict": "met"},
			{"text": "Cannot over-redeem", "verdict": "unmet", "why": "redeem allows negative balance"},
		},
		"checks": []map[string]any{{"name": "tests", "status": "fail", "detail": "2 failing"}},
	})
	wrapper, _ := marshalIndentedJSON(map[string]any{"evidence": import_json})
	source.gateRuns = []workboard.GateRun{
		{Gate: "scope_clear", State: "pass", CreatedAt: time.Unix(100, 0)},
		{Gate: "delivery_review", State: "fail", Hint: "1 of 2 met", EvidenceJSON: wrapper, CreatedAt: time.Unix(200, 0)},
	}
	svc := newContextPackService(source, arts, atts, nil, nil)
	got, err := svc.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "change_request", ID: "cr-1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Outstanding Review Feedback", "Cannot over-redeem", "redeem allows negative balance",
		"Check failed: tests", "2 failing", "Reviewer summary: 1 of 2 met",
	} {
		if !strings.Contains(got.Markdown, want) {
			t.Fatalf("re-handoff pack missing %q\n---\n%s", want, got.Markdown)
		}
	}
	if strings.Contains(got.Markdown, "Total updates in real time** (met)") {
		t.Fatalf("met criterion should not appear as outstanding:\n%s", got.Markdown)
	}

	// A passing review (newest) → no outstanding section.
	source.gateRuns = append(source.gateRuns, workboard.GateRun{
		Gate: "delivery_review", State: "pass", CreatedAt: time.Unix(300, 0),
	})
	svc2 := newContextPackService(source, arts, atts, nil, nil)
	pass, _ := svc2.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "change_request", ID: "cr-1"})
	if strings.Contains(pass.Markdown, "Outstanding Review Feedback") {
		t.Fatalf("a passing latest review should add no outstanding section:\n%s", pass.Markdown)
	}
}

func TestContextPackPayload_UnresolvedQualityGates(t *testing.T) {
	t.Parallel()
	source, arts, atts := assembleSource()
	source.gateRuns = []workboard.GateRun{
		{Gate: "scope_clear", State: "pass", CreatedAt: time.Unix(100, 0)},
		{Gate: "acceptance_criteria_verifiable", State: "warn", Hint: "Restate AC-2 as: latency < 200ms p95", CreatedAt: time.Unix(110, 0)},
		{Gate: "rollback_plan_present", State: "fail", Hint: "No rollback noted", CreatedAt: time.Unix(120, 0)},
		{Gate: "delivery_review", State: "fail", Hint: "x", CreatedAt: time.Unix(130, 0)},
	}
	svc := newContextPackService(source, arts, atts, nil, nil)
	got, err := svc.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "change_request", ID: "cr-1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Unresolved Quality Gates", "acceptance_criteria_verifiable", "Restate AC-2 as: latency < 200ms p95",
		"rollback_plan_present", "No rollback noted",
	} {
		if !strings.Contains(got.Markdown, want) {
			t.Fatalf("pack missing unresolved-gate %q\n---\n%s", want, got.Markdown)
		}
	}
	if strings.Contains(got.Markdown, "scope_clear** (pass)") {
		t.Fatalf("passing gate should not be listed as unresolved:\n%s", got.Markdown)
	}
	gatesIdx := strings.Index(got.Markdown, "## Unresolved Quality Gates")
	if dr := strings.Index(got.Markdown, "delivery_review"); dr >= gatesIdx && gatesIdx >= 0 {
		t.Fatalf("delivery_review must not appear in the Unresolved Quality Gates section:\n%s", got.Markdown)
	}
}

func TestContextPackPayload_LaneScopesTasks(t *testing.T) {
	t.Parallel()
	source, arts, atts := assembleSource()

	svc := newContextPackService(source, arts, atts, nil, nil)
	fe, err := svc.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "change_request", ID: "cr-1", Lane: "fe"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fe.Markdown, "points panel") {
		t.Fatalf("fe lane should contain fe plan content:\n%s", fe.Markdown)
	}
	if strings.Contains(fe.Markdown, "redeem endpoint") {
		t.Fatalf("fe lane should omit be plan content:\n%s", fe.Markdown)
	}

	be, err := svc.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "change_request", ID: "cr-1", Lane: "be"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(be.Markdown, "redeem endpoint") {
		t.Fatalf("be lane should contain be plan content:\n%s", be.Markdown)
	}
	if strings.Contains(be.Markdown, "points panel") {
		t.Fatalf("be lane should omit fe plan content:\n%s", be.Markdown)
	}
}

func TestContextPackPayload_QuickLaneFallback(t *testing.T) {
	t.Parallel()
	source := fakeContextPackSource{
		cr:      &workboard.ChangeRequest{ID: "cr-q", Key: "CR-Q", FeatureID: "feat-1", Title: "Quick", LeadArtifactID: "art-quick"},
		feature: &workboard.Feature{ID: "feat-1", Key: "FEAT-1", Name: "Checkout"},
	}
	arts := fakeContextPackArtifacts{
		pack: &artifact.Artifact{ID: "art-quick"},
		files: map[string]string{
			artifact.FixedKeyToPath("implementation_plan"): "# Quick Handoff\n\neverything in one doc",
		},
	}
	svc := newContextPackService(source, arts, fakeContextPackAttachments{}, nil, nil)
	got, err := svc.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "change_request", ID: "cr-q"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Markdown != "# Quick Handoff\n\neverything in one doc" {
		t.Fatalf("quick-lane pack should serve implementation_plan verbatim, got %q", got.Markdown)
	}
}

func TestContextPackPayload_NotGeneratedWhenNoSource(t *testing.T) {
	t.Parallel()
	source := fakeContextPackSource{
		cr:      &workboard.ChangeRequest{ID: "cr-2", Key: "CR-2", FeatureID: "feat-1", Title: "No artifact"},
		feature: &workboard.Feature{ID: "feat-1", Key: "FEAT-1", Name: "Checkout"},
	}
	svc := newContextPackService(source, fakeContextPackArtifacts{}, fakeContextPackAttachments{}, nil, nil)
	got, err := svc.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "change_request", ID: "cr-2"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "not_generated" {
		t.Fatalf("state=%v, want not_generated", got.State)
	}
	if got.Markdown != "" {
		t.Fatalf("markdown must be empty with no source artifact, got %q", got.Markdown)
	}
}

func TestContextPackPayloadNotFound(t *testing.T) {
	t.Parallel()
	svc := newContextPackService(fakeContextPackSource{err: workboard.ErrNotFound}, fakeContextPackArtifacts{}, fakeContextPackAttachments{}, nil, nil)
	_, err := svc.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "change_request", ID: "missing"})
	if !errors.Is(err, workboard.ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestContextPackPayload_ArtifactScoped_RoleSections(t *testing.T) {
	t.Parallel()
	art := &artifact.Artifact{
		ID: "art-role-1",
		Files: []artifact.File{
			{ArtifactID: "art-role-1", Path: "spec.md", Role: artifact.RoleSpec},
			{ArtifactID: "art-role-1", Path: "design.md", Role: artifact.RoleDesign},
			{ArtifactID: "art-role-1", Path: "plan.md", Role: artifact.RolePlan},
		},
	}
	arts := fakeContextPackArtifacts{
		pack: art,
		files: map[string]string{
			"spec.md":   "# The Spec\n\ncontract here",
			"design.md": "# Design\n\nwireframes",
			"plan.md":   "# Plan\n\ntasks go here",
		},
	}
	svc := &governanceops.Service{Artifacts: arts}
	got, err := svc.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "artifact", ID: "art-role-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "assembled" {
		t.Fatalf("state=%v, want assembled", got.State)
	}
	for _, want := range []string{
		"## Spec", "contract here",
		"## Design", "wireframes",
		"## Implementation Plan", "tasks go here",
	} {
		if !strings.Contains(got.Markdown, want) {
			t.Fatalf("artifact-scoped pack missing %q\n---\n%s", want, got.Markdown)
		}
	}
	if strings.Contains(got.Markdown, "## Execution Brief") {
		t.Fatalf("artifact-scoped pack must not have Execution Brief:\n%s", got.Markdown)
	}
}

func TestContextPackPayload_ArtifactScoped_AdditionalDocuments(t *testing.T) {
	t.Parallel()
	art := &artifact.Artifact{
		ID: "art-custom-1",
		Files: []artifact.File{
			{ArtifactID: "art-custom-1", Path: "spec.md", Role: artifact.RoleSpec},
			{ArtifactID: "art-custom-1", Path: "extra.md", Role: artifact.RoleUnspecified},
			{ArtifactID: "art-custom-1", Path: "rx-guide.md", Role: artifact.Role("custom:rx")},
		},
	}
	arts := fakeContextPackArtifacts{
		pack: art,
		files: map[string]string{
			"spec.md":     "# Spec",
			"extra.md":    "extra content here",
			"rx-guide.md": "rx custom doc",
		},
	}
	svc := &governanceops.Service{Artifacts: arts}
	got, err := svc.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "artifact", ID: "art-custom-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Markdown, "## Additional Documents") {
		t.Fatalf("pack missing Additional Documents section:\n%s", got.Markdown)
	}
	if !strings.Contains(got.Markdown, "extra content here") {
		t.Fatalf("pack missing unspecified doc content:\n%s", got.Markdown)
	}
	if !strings.Contains(got.Markdown, "rx custom doc") {
		t.Fatalf("pack missing custom:rx doc content:\n%s", got.Markdown)
	}
}

func TestContextPackPayload_DomainVocabularySection(t *testing.T) {
	t.Parallel()
	art := &artifact.Artifact{
		ID: "art-vocab-1",
		Files: []artifact.File{
			{ArtifactID: "art-vocab-1", Path: "spec.md", Role: artifact.RoleSpec},
			{ArtifactID: "art-vocab-1", Path: "glossary.md", Role: artifact.RoleReference},
			{ArtifactID: "art-vocab-1", Path: "domain/ubiquitous-language.md", Role: artifact.RoleUnspecified},
			{ArtifactID: "art-vocab-1", Path: "notes.md", Role: artifact.RoleUnspecified},
		},
	}
	arts := fakeContextPackArtifacts{
		pack: art,
		files: map[string]string{
			"spec.md":                       "# Spec\n\nUse points carefully.",
			"glossary.md":                   "- **Points ledger**: append-only account of point mutations.",
			"domain/ubiquitous-language.md": "- **Redemption**: spending points for discount value.",
			"notes.md":                      "general notes",
		},
	}
	svc := &governanceops.Service{Artifacts: arts}
	got, err := svc.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "artifact", ID: "art-vocab-1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Domain Vocabulary",
		"### domain/ubiquitous-language.md",
		"Redemption",
		"### glossary.md",
		"Points ledger",
		"## Additional Documents",
		"general notes",
	} {
		if !strings.Contains(got.Markdown, want) {
			t.Fatalf("pack missing %q\n---\n%s", want, got.Markdown)
		}
	}
	additionalIdx := strings.Index(got.Markdown, "## Additional Documents")
	if additionalIdx < 0 {
		t.Fatalf("expected Additional Documents section:\n%s", got.Markdown)
	}
	if strings.Contains(got.Markdown[additionalIdx:], "Redemption") || strings.Contains(got.Markdown[additionalIdx:], "Points ledger") {
		t.Fatalf("domain vocabulary should not be duplicated in Additional Documents:\n%s", got.Markdown)
	}
}

func TestContextPackPayload_OmitsDomainVocabularyWhenAbsent(t *testing.T) {
	t.Parallel()
	art := &artifact.Artifact{
		ID: "art-no-vocab-1",
		Files: []artifact.File{
			{ArtifactID: "art-no-vocab-1", Path: "spec.md", Role: artifact.RoleSpec},
			{ArtifactID: "art-no-vocab-1", Path: "reference.md", Role: artifact.RoleReference},
		},
	}
	arts := fakeContextPackArtifacts{
		pack: art,
		files: map[string]string{
			"spec.md":      "# Spec\n\ncontract",
			"reference.md": "reference material",
		},
	}
	svc := &governanceops.Service{Artifacts: arts}
	got, err := svc.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "artifact", ID: "art-no-vocab-1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Markdown, "## Domain Vocabulary") {
		t.Fatalf("pack should omit Domain Vocabulary when no vocabulary files exist:\n%s", got.Markdown)
	}
	if !strings.Contains(got.Markdown, "## Reference") || !strings.Contains(got.Markdown, "reference material") {
		t.Fatalf("normal reference files should still render:\n%s", got.Markdown)
	}
}

func TestContextPackPayload_RendererKeyDispatch(t *testing.T) {
	t.Parallel()
	makeSnap := func(rendererKey string) string {
		profile := governanceprofile.ResolvedProfile{
			RendererKey:   rendererKey,
			RequiredRoles: []string{"spec"},
		}
		b, _ := marshalIndentedJSON(profile)
		return b
	}
	for _, key := range []string{"", "default_context_pack"} {
		art := &artifact.Artifact{
			ID:                       "art-dispatch",
			GatesProfileSnapshotJSON: makeSnap(key),
			Files: []artifact.File{
				{ArtifactID: "art-dispatch", Path: "spec.md", Role: artifact.RoleSpec},
			},
		}
		arts := fakeContextPackArtifacts{
			pack:  art,
			files: map[string]string{"spec.md": "spec body"},
		}
		svc := &governanceops.Service{Artifacts: arts}
		got, err := svc.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "artifact", ID: "art-dispatch"})
		if err != nil {
			t.Fatalf("renderer_key=%q: %v", key, err)
		}
		if !strings.Contains(got.Markdown, "## Spec") || !strings.Contains(got.Markdown, "spec body") {
			t.Fatalf("renderer_key=%q: role-based renderer not used:\n%s", key, got.Markdown)
		}
	}
}

func TestContextPackPayload_ArtifactNotFound(t *testing.T) {
	t.Parallel()
	svc := &governanceops.Service{Artifacts: fakeContextPackArtifacts{}} // pack == nil
	_, err := svc.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "artifact", ID: "missing-art"})
	if err == nil {
		t.Fatal("expected error for missing artifact")
	}
	if !errors.Is(err, workboard.ErrNotFound) {
		t.Fatalf("err=%v, want ErrNotFound", err)
	}
}

func TestContextPackPayload_ApplicableSkills(t *testing.T) {
	t.Parallel()
	snap := func(gateSkills map[string]string) string {
		profile := governanceprofile.ResolvedProfile{
			RendererKey: "default_context_pack",
			GateSkills:  gateSkills,
		}
		b, _ := marshalIndentedJSON(profile)
		return b
	}
	art := &artifact.Artifact{
		ID: "art-skills-1",
		GatesProfileSnapshotJSON: snap(map[string]string{
			"spec_completeness": "spec-review",
			"scope_clear":       "spec-review",
			"delivery_review":   "review-impl",
		}),
		Files: []artifact.File{{ArtifactID: "art-skills-1", Path: "spec.md", Role: artifact.RoleSpec}},
	}
	arts := fakeContextPackArtifacts{
		pack:  art,
		files: map[string]string{"spec.md": "spec body"},
	}
	skillReader := fakeContextPackSkillsReader{list: []skills.Skill{
		{ID: "sk-spec", Name: "spec-review", Description: "Reviewing specs"},
		{ID: "sk-impl", Name: "review-impl", Description: "Reviewing delivery"},
		{ID: "sk-other", Name: "unrelated", Description: "Not bound"},
	}}

	svc := &governanceops.Service{Artifacts: arts, Skills: skillReader}
	got, err := svc.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "artifact", ID: "art-skills-1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Applicable Skills",
		"spec-review", "Reviewing specs", "specgate://skills/sk-spec",
		"review-impl", "Reviewing delivery", "specgate://skills/sk-impl",
	} {
		if !strings.Contains(got.Markdown, want) {
			t.Fatalf("pack missing applicable-skill %q\n---\n%s", want, got.Markdown)
		}
	}
	if strings.Contains(got.Markdown, "unrelated") {
		t.Fatalf("unbound skill leaked into the pack:\n%s", got.Markdown)
	}
	if c := strings.Count(got.Markdown, "specgate://skills/sk-spec"); c != 1 {
		t.Fatalf("spec-review pointer should appear once (deduped), got %d:\n%s", c, got.Markdown)
	}

	// No gate_skills bound → no Applicable Skills section.
	art.GatesProfileSnapshotJSON = snap(nil)
	svc2 := &governanceops.Service{Artifacts: arts, Skills: skillReader}
	bare, _ := svc2.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "artifact", ID: "art-skills-1"})
	if strings.Contains(bare.Markdown, "## Applicable Skills") {
		t.Fatalf("no gate_skills should yield no Applicable Skills section:\n%s", bare.Markdown)
	}

	// nil skillReader → section omitted.
	art.GatesProfileSnapshotJSON = snap(map[string]string{"spec_completeness": "spec-review"})
	svc3 := &governanceops.Service{Artifacts: arts}
	noReader, _ := svc3.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "artifact", ID: "art-skills-1"})
	if strings.Contains(noReader.Markdown, "## Applicable Skills") {
		t.Fatalf("nil skill reader should omit the Applicable Skills section:\n%s", noReader.Markdown)
	}
}

func TestContextPackPayload_CanonicalFilesAbsent(t *testing.T) {
	t.Parallel()
	source := fakeContextPackSource{
		cr: &workboard.ChangeRequest{
			ID: "cr-cf-1", FeatureID: "feat-cf-1", Title: "T",
			LeadArtifactID: "art-cf-1",
		},
		feature: &workboard.Feature{ID: "feat-cf-1", Key: "F"},
	}
	arts := fakeContextPackArtifacts{
		pack: &artifact.Artifact{
			ID: "art-cf-1",
			Files: []artifact.File{
				{ArtifactID: "art-cf-1", Path: "docs/spec.md"},
				{ArtifactID: "art-cf-1", Path: "docs/prd.md"},
			},
		},
		files: map[string]string{
			"docs/spec.md": "# Spec content",
			"docs/prd.md":  "# PRD content",
		},
	}
	svc := newContextPackService(source, arts, fakeContextPackAttachments{}, nil, nil)
	got, err := svc.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "change_request", ID: "cr-cf-1"})
	if err != nil {
		t.Fatal(err)
	}
	// canonical_files should not appear (removed in typed result — no such field)
	_ = got
}

func TestContextPackPayload_KnowledgeProvenance(t *testing.T) {
	t.Parallel()
	source, arts, atts := assembleSource()
	kr := &fakeKnowledgeReader{docs: []knowledge.Document{
		{
			DocumentID:     "doc-srs",
			Title:          "Payments SRS v2",
			Version:        "v2",
			DocumentType:   knowledge.DocumentTypeSRS,
			AuthorityLevel: knowledge.AuthoritySourceOfTruth,
			IsLatest:       true,
			CreatedAt:      time.Now(),
		},
		{
			DocumentID:     "doc-old-design",
			Title:          "Old Design Doc v1",
			Version:        "v1",
			DocumentType:   knowledge.DocumentTypeDesignReference,
			AuthorityLevel: knowledge.AuthorityReference,
			IsLatest:       false,
			CreatedAt:      time.Now().Add(-time.Hour),
		},
	}}
	svc := newContextPackService(source, arts, atts, nil, kr)
	got, err := svc.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "change_request", ID: "cr-1"})
	if err != nil {
		t.Fatal(err)
	}

	provRows := got.KnowledgeProvenance
	if len(provRows) != 2 {
		t.Fatalf("knowledge_provenance len = %d, want 2", len(provRows))
	}
	if provRows[0].Freshness != "current" {
		t.Errorf("rows[0].freshness = %q, want current", provRows[0].Freshness)
	}
	if provRows[1].Freshness != "stale" {
		t.Errorf("rows[1].freshness = %q, want stale", provRows[1].Freshness)
	}

	if !strings.Contains(got.Markdown, "### Knowledge References") {
		t.Errorf("markdown missing ### Knowledge References section")
	}
	if !strings.Contains(got.Markdown, "Payments SRS v2") {
		t.Errorf("markdown missing current doc title")
	}
	if !strings.Contains(got.Markdown, "stale — newer version available") {
		t.Errorf("markdown missing stale freshness label")
	}
}

func TestContextPackPayload_KnowledgeProvenanceEmpty(t *testing.T) {
	t.Parallel()
	source, arts, atts := assembleSource()
	kr := &fakeKnowledgeReader{docs: []knowledge.Document{}}
	svc := newContextPackService(source, arts, atts, nil, kr)
	got, err := svc.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "change_request", ID: "cr-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.KnowledgeProvenance == nil {
		t.Fatal("knowledge_provenance should not be nil (spec §2.2 never null)")
	}
	if len(got.KnowledgeProvenance) != 0 {
		t.Errorf("knowledge_provenance len = %d, want 0", len(got.KnowledgeProvenance))
	}
	if strings.Contains(got.Markdown, "### Knowledge References") {
		t.Error("markdown should not have Knowledge References section when empty")
	}
}

func TestContextPackPayload_KnowledgeProvenanceError(t *testing.T) {
	t.Parallel()
	source, arts, atts := assembleSource()
	kr := &fakeKnowledgeReader{err: errors.New("db timeout")}
	svc := newContextPackService(source, arts, atts, nil, kr)
	got, err := svc.ContextPack(context.Background(), governanceops.ContextPackInput{Kind: "change_request", ID: "cr-1"})
	if err != nil {
		t.Fatal("pack read should succeed despite repo error:", err)
	}
	if got.State != "assembled" {
		t.Errorf("state = %v, want assembled", got.State)
	}
	found := false
	for _, w := range got.Warnings {
		if w.Code == "knowledge_provenance_unavailable" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("knowledge_provenance_unavailable warning missing; warnings = %v", got.Warnings)
	}
}
