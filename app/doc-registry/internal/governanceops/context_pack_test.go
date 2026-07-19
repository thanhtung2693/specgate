package governanceops

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/artifactattachment"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/knowledge"
	"github.com/specgate/doc-registry/internal/workboard"
	"github.com/specgate/doc-registry/internal/workspace"
)

// --- fakes for context-pack tests ---

type fakeContextPackWorkBoard struct {
	cr      *workboard.ChangeRequest
	feature *workboard.Feature
	runs    []workboard.GateRun
	runsErr error
	acs     []workboard.AcceptanceCriterion
	acErr   error
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
	return f.acs, f.acErr
}

func (f *fakeContextPackWorkBoard) ListGateRuns(_ context.Context, _ string, _ int) ([]workboard.GateRun, error) {
	return f.runs, f.runsErr
}

func (f *fakeContextPackWorkBoard) ListStaleWarnings(_ context.Context, _ workboard.StaleWarningFilter) ([]workboard.StaleWarning, error) {
	return nil, nil
}

type fakeContextPackArtifactReader struct {
	art     *artifact.Artifact
	files   map[string]string
	getErr  error
	fileErr error
}

func (f *fakeContextPackArtifactReader) Get(_ context.Context, id string) (*artifact.Artifact, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.art == nil || f.art.ID != id {
		return nil, artifact.ErrNotFound
	}
	return f.art, nil
}

func (f *fakeContextPackArtifactReader) FileContent(_ context.Context, _ string, path string) ([]byte, error) {
	if f.fileErr != nil {
		return nil, f.fileErr
	}
	body, ok := f.files[path]
	if !ok {
		return nil, artifact.ErrFileNotFound
	}
	return []byte(body), nil
}

type fakeContextPackReadinessReader struct {
	runs  map[string][]artifact.ReadinessRun
	calls int
	err   error
}

func (f *fakeContextPackReadinessReader) ListReadinessRuns(_ context.Context, artifactID string, _ int) ([]artifact.ReadinessRun, error) {
	f.calls++
	return f.runs[artifactID], f.err
}

type fakeContextPackKnowledgeReader struct {
	docs        []knowledge.Document
	err         error
	workspaceID string
	featureRefs []string
}

type failingContextPackFeedbackStore struct {
	err error
}

type failingContextPackAttachmentReader struct {
	err error
}

func (f *failingContextPackAttachmentReader) ListByFeature(_ context.Context, _, _ string) ([]artifactattachment.Attachment, error) {
	return nil, f.err
}

func (f *failingContextPackFeedbackStore) CreateGovernanceFeedbackEvent(_ context.Context, _ integrations.GovernanceFeedbackEvent) (*integrations.GovernanceFeedbackEvent, error) {
	return nil, f.err
}

func (f *failingContextPackFeedbackStore) ListGovernanceFeedbackEvents(_ context.Context, _ integrations.GovernanceFeedbackFilter) ([]integrations.GovernanceFeedbackEvent, error) {
	return nil, f.err
}

type failingAuthoritativeContextPackWorkBoard struct {
	*fakeContextPackWorkBoard
	err error
}

func (f *failingAuthoritativeContextPackWorkBoard) AuthoritativeDeliveryReviewRun(_ context.Context, _ string) (*workboard.GateRun, error) {
	return nil, f.err
}

func (f *fakeContextPackKnowledgeReader) ListByFeatureOrRequest(_ context.Context, workspaceID string, featureRefs []string, _ string) ([]knowledge.Document, error) {
	f.workspaceID = workspaceID
	f.featureRefs = append([]string(nil), featureRefs...)
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
		art: &artifact.Artifact{
			ID: "art-lead-1",
			Files: []artifact.File{
				{ArtifactID: "art-lead-1", Path: "specs/product.md", Role: artifact.RoleSpec},
				{ArtifactID: "art-lead-1", Path: "specs/contract.md", Role: artifact.RoleSpec},
				{ArtifactID: "art-lead-1", Path: "plans/frontend.md", Role: artifact.RolePlan},
				{ArtifactID: "art-lead-1", Path: "plans/backend.md", Role: artifact.RolePlan},
				{ArtifactID: "art-lead-1", Path: "verification/boundaries.md", Role: artifact.RoleVerification},
			},
		},
		files: map[string]string{
			"specs/product.md":           "# PRD\n\nproduct intent",
			"specs/contract.md":          "# Spec\n\napi contract",
			"plans/frontend.md":          "Frontend task: build the points panel",
			"plans/backend.md":           "- redeem endpoint + ledger",
			"verification/boundaries.md": "- boundary tests",
		},
	}
	return &Service{
		WorkBoard: &fakeContextPackWorkBoard{
			cr: cr, feature: feature,
			acs: []workboard.AcceptanceCriterion{
				{ID: "ac-1", Text: "Total updates in real time", SortOrder: 0},
				{ID: "ac-2", Text: "Cannot over-redeem", SortOrder: 1},
			},
		},
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

// spec_repo_drift is an artifact-scoped readiness run, never a CR gate_run. The
// pack must pull it from the source artifact's readiness runs and list each
// finding, so the drifted-doc guidance reaches the coding agent on the full
// route (per agents spec §6). Without the reader wired, the warn is dropped.
func TestContextPackSurfacesSpecRepoDriftFromReadinessRuns(t *testing.T) {
	t.Parallel()
	svc := newContextPackTestService()
	// Realistic stored shape: the submit envelope wraps findings under a JSON
	// string `evidence` field (gate-run-v1); top-level findings is the fallback.
	evidence := `{"executor":"ide_agent","evidence":"{\"examined_docs\":[\"app/doc-registry/docs/spec.md\"],\"repo_commit\":\"abc123\",\"findings\":[{\"doc_path\":\"app/doc-registry/docs/spec.md\",\"conflicting_claim\":\"claims an agent can approve an artifact\",\"spec_section\":\"§6.2\"}]}"}`
	svc.ReadinessRuns = &fakeContextPackReadinessReader{
		runs: map[string][]artifact.ReadinessRun{
			"art-lead-1": {{
				ID: "rr-1", ArtifactID: "art-lead-1", Gate: "spec_repo_drift",
				State: "warn", Hint: "1 drift finding", Executor: "ide_agent",
				EvidenceJSON: evidence, CreatedAt: time.Now(),
			}},
		},
	}
	got, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "change_request", ID: "cr-1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"spec_repo_drift", "warn",
		"app/doc-registry/docs/spec.md",
		"claims an agent can approve an artifact",
		"contradicts §6.2",
	} {
		if !strings.Contains(got.Markdown, want) {
			t.Fatalf("markdown missing %q; drift not surfaced.\n---\n%s", want, got.Markdown)
		}
	}
}

func TestContextPackPassesFeatureIDAndKeyToKnowledge(t *testing.T) {
	t.Parallel()
	svc := newContextPackTestService()
	kr := &fakeContextPackKnowledgeReader{}
	svc.Knowledge = kr

	if _, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "change_request", ID: "cr-1"}); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(kr.featureRefs, ","), "feat-1,FEAT-1"; got != want {
		t.Fatalf("feature refs = %q, want %q", got, want)
	}
}

func TestContextPackIncludesAllPublishedPlans(t *testing.T) {
	t.Parallel()
	svc := newContextPackTestService()

	got, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "change_request", ID: "cr-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Markdown, "Frontend task") {
		t.Fatalf("context pack should contain frontend plan:\n%s", got.Markdown)
	}
	if !strings.Contains(got.Markdown, "redeem endpoint") {
		t.Fatalf("context pack should contain backend plan:\n%s", got.Markdown)
	}
}

func TestContextPackDoesNotInferPlanScopeFromDocumentPaths(t *testing.T) {
	t.Parallel()
	svc := newContextPackTestService()
	reader := svc.Artifacts.(*fakeContextPackArtifactReader)
	reader.art.Files = []artifact.File{
		{ArtifactID: reader.art.ID, Path: "tasks-fe.md", Role: artifact.RolePlan},
		{ArtifactID: reader.art.ID, Path: "tasks-be.md", Role: artifact.RolePlan},
	}
	reader.files = map[string]string{
		"tasks-fe.md": "Frontend plan",
		"tasks-be.md": "Backend plan",
	}

	got, err := svc.ContextPack(context.Background(), ContextPackInput{
		Kind: "change_request",
		ID:   "cr-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Markdown, "Frontend plan") || !strings.Contains(got.Markdown, "Backend plan") {
		t.Fatalf("artifact paths must not filter plan content:\n%s", got.Markdown)
	}
}

func TestContextPackDoesNotInferDocumentRoleFromPath(t *testing.T) {
	t.Parallel()
	svc := newContextPackTestService()
	reader := svc.Artifacts.(*fakeContextPackArtifactReader)
	reader.art.Files = []artifact.File{
		{ArtifactID: reader.art.ID, Path: "docs/glossary/notes.md", Role: artifact.Role("custom:notes")},
	}
	reader.files = map[string]string{"docs/glossary/notes.md": "Project terms"}

	got, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "change_request", ID: "cr-1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Markdown, "## Domain Vocabulary") {
		t.Fatalf("document path must not create a semantic section:\n%s", got.Markdown)
	}
	if !strings.Contains(got.Markdown, "## Additional Documents") || !strings.Contains(got.Markdown, "Project terms") {
		t.Fatalf("custom document missing from additional documents:\n%s", got.Markdown)
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

func TestContextPackFailsWhenSourceArtifactReaderIsUnavailable(t *testing.T) {
	t.Parallel()
	svc := &Service{
		WorkBoard: &fakeContextPackWorkBoard{cr: &workboard.ChangeRequest{
			ID:             "cr-source-reader-missing",
			LeadArtifactID: "art-source",
		}},
	}

	_, err := svc.ContextPack(context.Background(), ContextPackInput{
		Kind: "change_request", ID: "cr-source-reader-missing",
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestContextPackFailsWhenSourceArtifactReadFails(t *testing.T) {
	t.Parallel()
	svc := &Service{
		WorkBoard: &fakeContextPackWorkBoard{cr: &workboard.ChangeRequest{
			ID:             "cr-source-read-fails",
			LeadArtifactID: "art-source",
		}},
		Artifacts: &fakeContextPackArtifactReader{getErr: errors.New("artifact registry unavailable")},
	}

	_, err := svc.ContextPack(context.Background(), ContextPackInput{
		Kind: "change_request", ID: "cr-source-read-fails",
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestContextPackFailsWhenSourceArtifactFileReadFails(t *testing.T) {
	t.Parallel()
	svc := newContextPackTestService()
	svc.Artifacts.(*fakeContextPackArtifactReader).fileErr = errors.New("object storage unavailable")

	_, err := svc.ContextPack(context.Background(), ContextPackInput{
		Kind: "change_request", ID: "cr-1",
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestContextPackFailsWhenGateRunReadFails(t *testing.T) {
	t.Parallel()
	svc := newContextPackTestService()
	svc.WorkBoard.(*fakeContextPackWorkBoard).runsErr = errors.New("gate store unavailable")

	_, err := svc.ContextPack(context.Background(), ContextPackInput{
		Kind: "change_request", ID: "cr-1",
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestContextPackFailsWhenAuthoritativeDeliveryReviewReadFails(t *testing.T) {
	t.Parallel()
	svc := newContextPackTestService()
	svc.WorkBoard = &failingAuthoritativeContextPackWorkBoard{
		fakeContextPackWorkBoard: svc.WorkBoard.(*fakeContextPackWorkBoard),
		err:                      errors.New("delivery review store unavailable"),
	}

	_, err := svc.ContextPack(context.Background(), ContextPackInput{
		Kind: "change_request", ID: "cr-1",
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestContextPackFailsWhenCompletionReadFails(t *testing.T) {
	t.Parallel()
	svc := newContextPackTestService()
	svc.FeedbackStore = &failingContextPackFeedbackStore{err: errors.New("feedback store unavailable")}

	_, err := svc.ContextPack(context.Background(), ContextPackInput{
		Kind: "change_request", ID: "cr-1",
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestContextPackFailsWhenReadinessRunReadFails(t *testing.T) {
	t.Parallel()
	svc := newContextPackTestService()
	svc.ReadinessRuns = &fakeContextPackReadinessReader{err: errors.New("readiness store unavailable")}

	_, err := svc.ContextPack(context.Background(), ContextPackInput{
		Kind: "change_request", ID: "cr-1",
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestContextPackFailsWhenReferenceAttachmentReadFails(t *testing.T) {
	t.Parallel()
	svc := newContextPackTestService()
	svc.Attachments = &failingContextPackAttachmentReader{err: errors.New("attachment store unavailable")}

	_, err := svc.ContextPack(context.Background(), ContextPackInput{
		Kind: "change_request", ID: "cr-1",
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestContextPackBuildsQuickHandoffWithoutArtifact(t *testing.T) {
	t.Parallel()
	svc := &Service{
		WorkBoard: &fakeContextPackWorkBoard{
			cr: &workboard.ChangeRequest{
				ID:                 "cr-quick",
				Key:                "CR-QUICK",
				Title:              "Fix local startup",
				IntentMD:           "The command must explain a port collision.",
				WorkType:           workboard.WorkTypeBugFix,
				AcceptanceCriteria: `["The error names the port."]`,
			},
			acs: []workboard.AcceptanceCriterion{
				{ID: "ac-quick", Text: "The error names the port."},
			},
		},
	}

	got, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "change_request", ID: "cr-quick"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "assembled" {
		t.Fatalf("state = %q, want assembled", got.State)
	}
	if !strings.Contains(got.Markdown, "Quick Handoff") || !strings.Contains(got.Markdown, "The error names the port.") {
		t.Fatalf("quick context pack missing persisted work details:\n%s", got.Markdown)
	}
}

func TestContextPackRejectsMissingCanonicalAcceptanceCriteria(t *testing.T) {
	t.Parallel()
	svc := &Service{
		WorkBoard: &fakeContextPackWorkBoard{
			cr: &workboard.ChangeRequest{
				ID:                 "cr-stale-mirror",
				Title:              "Ignore stale mirror",
				WorkType:           workboard.WorkTypeBugFix,
				AcceptanceCriteria: `["This mirror has no stable criterion ID."]`,
			},
		},
	}

	_, err := svc.ContextPack(context.Background(), ContextPackInput{
		Kind: "change_request", ID: "cr-stale-mirror",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

func TestContextPackPropagatesCanonicalAcceptanceCriteriaReadFailure(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("acceptance criteria store unavailable")
	svc := newContextPackTestService()
	svc.WorkBoard.(*fakeContextPackWorkBoard).acErr = wantErr

	_, err := svc.ContextPack(context.Background(), ContextPackInput{
		Kind: "change_request", ID: "cr-1",
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

func TestContextPackUsesLeadArtifactForQuickRoute(t *testing.T) {
	t.Parallel()
	const stored = "# Implementation Context Pack\n\n## What To Build\n\nFix the issue.\n\n## Acceptance Criteria\n\n- _No acceptance criteria captured._\n\n## Risks And Guardrails\n\n- Stay scoped."
	svc := &Service{
		WorkBoard: &fakeContextPackWorkBoard{
			cr: &workboard.ChangeRequest{
				ID:             "cr-quick",
				FeatureID:      "feat-1",
				LeadArtifactID: "pack-1",
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
	if strings.Count(got.Markdown, "# Implementation Context Pack") != 2 {
		t.Fatalf("derived pack should wrap the lead artifact once:\n%s", got.Markdown)
	}
	if !strings.Contains(got.Markdown, "- Removed readiness links open the first document.") {
		t.Fatalf("normalized acceptance criteria missing:\n%s", got.Markdown)
	}
}

func TestContextPackUsesLeadArtifactWithoutFeature(t *testing.T) {
	t.Parallel()
	const stored = "# Implementation Context Pack\n\n## Acceptance Criteria\n\n- _No acceptance criteria captured._"
	evidence, _ := json.Marshal(map[string]any{
		"criteria": []map[string]any{
			{"text": "Delivered work no longer appears in needs-attention list.", "verdict": "unclear", "why": "coding-agent claim: unsatisfied"},
			{"text": "Status badge updates after delivery review refresh.", "verdict": "met", "why": "coding-agent claim: satisfied"},
		},
		"checks": []map[string]any{
			{"name": "pnpm vitest statusBadges", "status": "fail", "detail": "delivered item still appears in needs-attention list after refresh"},
		},
	})
	wrapper, _ := json.Marshal(map[string]any{"evidence": string(evidence)})
	svc := &Service{
		WorkBoard: &fakeContextPackWorkBoard{
			cr: &workboard.ChangeRequest{
				ID:             "cr-featureless",
				LeadArtifactID: "pack-featureless",
			},
			acs: []workboard.AcceptanceCriterion{
				{ID: "ac-1", Text: "Quick path stays featureless.", SortOrder: 0},
			},
			runs: []workboard.GateRun{
				{
					Gate:         "delivery_review",
					State:        workboard.NextActionStateNeedsHumanReview,
					Hint:         "1 criterion still unclear",
					EvidenceJSON: string(wrapper),
					CreatedAt:    time.Unix(200, 0),
				},
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
	for _, want := range []string{
		"## Outstanding Review Feedback",
		"Delivered work no longer appears in needs-attention list.",
		"coding-agent claim: unsatisfied",
		"Check failed: pnpm vitest statusBadges",
		"delivered item still appears in needs-attention list after refresh",
		"Reviewer summary: 1 criterion still unclear",
	} {
		if !strings.Contains(got.Markdown, want) {
			t.Fatalf("stored quick pack missing %q\n---\n%s", want, got.Markdown)
		}
	}
}

func TestContextPackOutstandingFeedbackUsesHumanTrustPrecedence(t *testing.T) {
	t.Parallel()
	const stored = "# Implementation Context Pack\n\n## Acceptance Criteria\n\n- _No acceptance criteria captured._"
	evidence, _ := json.Marshal(map[string]any{
		"criteria": []map[string]any{
			{"text": "Human-approved work should remain delivered.", "verdict": "unmet", "why": "platform reviewer did not trust the evidence"},
		},
	})
	wrapper, _ := json.Marshal(map[string]any{"evidence": string(evidence)})
	svc := &Service{
		WorkBoard: &fakeContextPackWorkBoard{
			cr: &workboard.ChangeRequest{
				ID:             "cr-human-clear-context",
				LeadArtifactID: "pack-human-clear",
			},
			acs: []workboard.AcceptanceCriterion{
				{ID: "ac-human-clear", Text: "Human-approved work should remain delivered."},
			},
			runs: []workboard.GateRun{
				{
					Gate:      "delivery_review",
					State:     workboard.NextActionStatePass,
					Hint:      "human reviewer cleared delivery",
					Executor:  workboard.GateRunExecutorHuman,
					CreatedAt: time.Unix(200, 0),
				},
				{
					Gate:         "delivery_review",
					State:        workboard.NextActionStateFail,
					Hint:         "platform reviewer would fail",
					Executor:     workboard.GateRunExecutorPlatform,
					EvidenceJSON: string(wrapper),
					CreatedAt:    time.Unix(300, 0),
				},
			},
		},
		Artifacts: &fakeContextPackArtifactReader{
			art: &artifact.Artifact{
				ID: "pack-human-clear",
				Files: []artifact.File{
					{ArtifactID: "pack-human-clear", Path: "implementation-plan.md", Role: artifact.RolePlan},
				},
			},
			files: map[string]string{"implementation-plan.md": stored},
		},
	}

	got, err := svc.ContextPack(context.Background(), ContextPackInput{Kind: "change_request", ID: "cr-human-clear-context"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Markdown, "## Outstanding Review Feedback") ||
		strings.Contains(got.Markdown, "platform reviewer would fail") {
		t.Fatalf("context pack should not surface later platform failure after human approval:\n%s", got.Markdown)
	}
}

func TestContextPackUsesNewCompletionReviewAfterHumanReworkDecision(t *testing.T) {
	t.Parallel()
	const stored = "# Implementation Context Pack\n\n## Acceptance Criteria\n\n- Correct the regression."
	oldDetail, _ := json.Marshal(map[string]any{
		"completion_feedback_event_id": "completion-1",
		"criteria": []map[string]any{{
			"text": "Correct the regression.", "verdict": "unmet", "why": "first attempt failed",
		}},
	})
	oldHuman, _ := json.Marshal(map[string]any{
		"completion_feedback_event_id": "completion-1",
		"evidence":                     string(oldDetail),
		"decision":                     "reject",
	})
	newDetail, _ := json.Marshal(map[string]any{
		"completion_feedback_event_id": "completion-2",
		"criteria": []map[string]any{{
			"text": "Correct the regression.", "verdict": "met",
		}},
	})
	newPlatform, _ := json.Marshal(map[string]any{
		"completion_feedback_event_id": "completion-2",
		"evidence":                     string(newDetail),
	})
	svc := &Service{
		WorkBoard: &fakeContextPackWorkBoard{
			cr: &workboard.ChangeRequest{
				ID: "cr-new-cycle-context", LeadArtifactID: "pack-new-cycle",
			},
			acs: []workboard.AcceptanceCriterion{
				{ID: "ac-new-cycle", Text: "Correct the regression."},
			},
			runs: []workboard.GateRun{
				{
					ID: "human-reject-completion-1", Gate: "delivery_review",
					State: workboard.NextActionStateFail, Executor: workboard.GateRunExecutorHuman,
					EvidenceJSON: string(oldHuman), CreatedAt: time.Unix(100, 0),
				},
				{
					ID: "platform-pass-completion-2", Gate: "delivery_review",
					State: workboard.NextActionStatePass, Executor: workboard.GateRunExecutorPlatform,
					EvidenceJSON: string(newPlatform), CreatedAt: time.Unix(200, 0),
				},
			},
		},
		Artifacts: &fakeContextPackArtifactReader{
			art: &artifact.Artifact{
				ID: "pack-new-cycle",
				Files: []artifact.File{{
					ArtifactID: "pack-new-cycle", Path: "implementation-plan.md", Role: artifact.RolePlan,
				}},
			},
			files: map[string]string{"implementation-plan.md": stored},
		},
	}

	got, err := svc.ContextPack(context.Background(), ContextPackInput{
		Kind: "change_request", ID: "cr-new-cycle-context",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Markdown, "## Outstanding Review Feedback") ||
		strings.Contains(got.Markdown, "first attempt failed") {
		t.Fatalf("old completion feedback leaked into new delivery cycle:\n%s", got.Markdown)
	}
}

func TestContextPackSuppressesOldFeedbackWhileNewCompletionAwaitsReview(t *testing.T) {
	t.Parallel()
	const stored = "# Implementation Context Pack\n\n## Acceptance Criteria\n\n- Correct the regression."
	oldDetail, _ := json.Marshal(map[string]any{
		"completion_feedback_event_id": "completion-1",
		"criteria": []map[string]any{{
			"text": "Correct the regression.", "verdict": "unmet", "why": "first attempt failed",
		}},
	})
	oldHuman, _ := json.Marshal(map[string]any{
		"completion_feedback_event_id": "completion-1",
		"evidence":                     string(oldDetail),
		"decision":                     "reject",
	})
	svc := &Service{
		WorkBoard: &fakeContextPackWorkBoard{
			cr: &workboard.ChangeRequest{
				ID: "cr-review-pending-context", LeadArtifactID: "pack-review-pending",
			},
			acs: []workboard.AcceptanceCriterion{
				{ID: "ac-review-pending", Text: "Correct the regression."},
			},
			runs: []workboard.GateRun{{
				ID: "human-reject-completion-1", Gate: "delivery_review",
				State: workboard.NextActionStateFail, Executor: workboard.GateRunExecutorHuman,
				EvidenceJSON: string(oldHuman), CreatedAt: time.Unix(100, 0),
			}},
		},
		FeedbackStore: &fakeFeedbackStore{created: []integrations.GovernanceFeedbackEvent{{
			ID: "completion-2", ChangeRequestID: "cr-review-pending-context",
			EventType:   integrations.FeedbackEventCodingAgentCompleted,
			PayloadJSON: `{"summary":"corrected","agent":{"name":"builder"}}`,
			CreatedAt:   time.Unix(200, 0),
		}}},
		Artifacts: &fakeContextPackArtifactReader{
			art: &artifact.Artifact{
				ID: "pack-review-pending",
				Files: []artifact.File{{
					ArtifactID: "pack-review-pending", Path: "implementation-plan.md",
					Role: artifact.RolePlan,
				}},
			},
			files: map[string]string{"implementation-plan.md": stored},
		},
	}

	got, err := svc.ContextPack(context.Background(), ContextPackInput{
		Kind: "change_request", ID: "cr-review-pending-context",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Markdown, "## Outstanding Review Feedback") ||
		strings.Contains(got.Markdown, "first attempt failed") {
		t.Fatalf("old completion feedback leaked while the new completion awaits review:\n%s", got.Markdown)
	}
}

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
