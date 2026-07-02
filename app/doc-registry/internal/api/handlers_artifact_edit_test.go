package api

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/artifactedit"
	"github.com/specgate/doc-registry/internal/integrations"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
	"github.com/specgate/doc-registry/internal/workboard"
)

type fakeArtifactEditArtifactService struct {
	artifact          *artifact.Artifact
	files             map[string][]byte
	fileContentCalls  int
	publishCalls      int
	updateStatusCalls int
}

func (s *fakeArtifactEditArtifactService) Publish(_ context.Context, in artifact.PublishInput) (*artifact.Artifact, error) {
	s.publishCalls++
	return &artifact.Artifact{
		ID:        "mat-art-" + in.Version,
		FeatureID: in.FeatureID,
		Version:   in.Version,
		Status:    artifact.StatusDraft,
	}, nil
}
func (s *fakeArtifactEditArtifactService) Get(_ context.Context, id string) (*artifact.Artifact, error) {
	if s.artifact == nil || s.artifact.ID != id {
		return nil, artifact.ErrNotFound
	}
	cp := *s.artifact
	cp.Files = append([]artifact.File(nil), s.artifact.Files...)
	return &cp, nil
}
func (s *fakeArtifactEditArtifactService) List(context.Context, artifact.ListFilter) ([]artifact.Artifact, error) {
	return nil, nil
}
func (s *fakeArtifactEditArtifactService) Count(context.Context, artifact.ListFilter) (int64, error) {
	return 0, nil
}
func (s *fakeArtifactEditArtifactService) LatestArtifact(context.Context, string) (*artifact.Artifact, error) {
	return nil, nil
}
func (s *fakeArtifactEditArtifactService) NextVersion(context.Context, string) (string, error) {
	return "v0.1", nil
}
func (s *fakeArtifactEditArtifactService) ResolveNextVersion(context.Context, string, string) (string, error) {
	return "v0.1", nil
}
func (s *fakeArtifactEditArtifactService) UpdateStatus(context.Context, string, artifact.StatusUpdate) (*artifact.Artifact, error) {
	s.updateStatusCalls++
	return nil, errors.New("not implemented")
}
func (s *fakeArtifactEditArtifactService) Delete(context.Context, string) error {
	return nil
}
func (s *fakeArtifactEditArtifactService) SignedFileURL(_ context.Context, _ string, path string) (*artifact.SignedFile, error) {
	body, ok := s.files[path]
	if !ok {
		return nil, artifact.ErrNotFound
	}
	return &artifact.SignedFile{
		URL:       "https://signed.example/" + path,
		ExpiresAt: time.Now().UTC().Add(time.Minute),
		SizeBytes: int64(len(body)),
	}, nil
}
func (s *fakeArtifactEditArtifactService) FileContent(_ context.Context, _ string, path string) ([]byte, error) {
	s.fileContentCalls++
	body, ok := s.files[path]
	if !ok {
		return nil, artifact.ErrNotFound
	}
	return append([]byte(nil), body...), nil
}
func (s *fakeArtifactEditArtifactService) CheckConflicts(context.Context, []string) (*artifact.ConflictReport, error) {
	return nil, nil
}
func (s *fakeArtifactEditArtifactService) ListEvents(context.Context, artifact.EventFilter) ([]artifact.Event, error) {
	return nil, nil
}
func (s *fakeArtifactEditArtifactService) RefreshReadinessRuns(context.Context, string, []artifact.ReadinessEvaluation) ([]artifact.ReadinessRun, error) {
	return nil, nil
}
func (s *fakeArtifactEditArtifactService) ListReadinessRuns(context.Context, string, int) ([]artifact.ReadinessRun, error) {
	return nil, nil
}
func TestGetArtifactFile_IncludesInlineContentFallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	artifactID := "art-file-content-" + strings.ReplaceAll(t.Name(), "/", "-")
	svc := &fakeArtifactEditArtifactService{
		artifact: &artifact.Artifact{ID: artifactID},
		files: map[string][]byte{
			"prd.md": []byte("# PRD\n\nInline fallback\n"),
		},
	}
	h := &Handlers{Artifacts: svc, ArtifactEdit: artifactedit.NewMemoryStore()}

	out, err := h.GetArtifactFile(ctx, &GetArtifactFileInput{ID: artifactID, Key: "prd"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Body.SignedURL == "" {
		t.Fatal("expected signed URL fallback")
	}
	if out.Body.Content != "# PRD\n\nInline fallback\n" {
		t.Fatalf("content = %q", out.Body.Content)
	}
	if out.Body.SizeBytes == 0 {
		t.Fatal("expected size bytes")
	}
}

func TestGetArtifactFile_SkipsInlineContentForLargeFiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	artifactID := "art-file-large-" + strings.ReplaceAll(t.Name(), "/", "-")
	svc := &fakeArtifactEditArtifactService{
		artifact: &artifact.Artifact{ID: artifactID},
		files: map[string][]byte{
			"spec.md": make([]byte, maxInlineArtifactFileContentBytes+1),
		},
	}
	h := &Handlers{Artifacts: svc, ArtifactEdit: artifactedit.NewMemoryStore()}

	out, err := h.GetArtifactFile(ctx, &GetArtifactFileInput{ID: artifactID, Key: "spec"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Body.Content != "" {
		t.Fatalf("content = %q, want omitted", out.Body.Content)
	}
	if svc.fileContentCalls != 0 {
		t.Fatalf("FileContent called %d time(s), want 0", svc.fileContentCalls)
	}
}

func TestGetArtifactFile_SkipsInlineContentForInvalidUTF8(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	artifactID := "art-file-invalid-utf8-" + strings.ReplaceAll(t.Name(), "/", "-")
	svc := &fakeArtifactEditArtifactService{
		artifact: &artifact.Artifact{ID: artifactID},
		files: map[string][]byte{
			"spec.md": {0xff, 0xfe, 0xfd},
		},
	}
	h := &Handlers{Artifacts: svc, ArtifactEdit: artifactedit.NewMemoryStore()}

	out, err := h.GetArtifactFile(ctx, &GetArtifactFileInput{ID: artifactID, Key: "spec"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Body.Content != "" {
		t.Fatalf("content = %q, want omitted", out.Body.Content)
	}
	if svc.fileContentCalls != 1 {
		t.Fatalf("FileContent called %d time(s), want 1", svc.fileContentCalls)
	}
}

func TestArtifactEditSession_PatchDiffSaveRevision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	artifactID := "art-edit-test-" + strings.ReplaceAll(t.Name(), "/", "-")
	svc := &fakeArtifactEditArtifactService{
		artifact: &artifact.Artifact{
			ID:        artifactID,
			FeatureID: "feed-freshness",
			Version:   "v1.0",
			Files: []artifact.File{
				{ArtifactID: artifactID, Path: "prd.md", S3Path: "s3/prd.md", SizeBytes: 12},
			},
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
		files: map[string][]byte{
			"prd.md": []byte("old metric\n"),
		},
	}
	h := &Handlers{Artifacts: svc, ArtifactEdit: artifactedit.NewMemoryStore()}

	opened, err := h.CreateArtifactEditSession(ctx, &CreateArtifactEditSessionInput{})
	if err == nil || opened != nil {
		t.Fatal("expected missing base artifact id to fail")
	}
	create := &CreateArtifactEditSessionInput{}
	create.Body.BaseArtifactID = artifactID
	opened, err = h.CreateArtifactEditSession(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := opened.Body.ID
	if sessionID == "" || opened.Body.State != "active" {
		t.Fatalf("unexpected session: %+v", opened.Body)
	}

	patch := &PatchArtifactEditSessionInput{ID: sessionID}
	patch.Body.FileKey = "prd.md"
	patch.Body.Operations = []map[string]any{{
		"op":    "replace",
		"path":  "/success_metrics/0",
		"value": "freshness under five minutes",
	}}
	patched, err := h.PatchArtifactEditSession(ctx, patch)
	if err != nil {
		t.Fatal(err)
	}
	if !patched.Body.Modified || patched.Body.Content != "freshness under five minutes" {
		t.Fatalf("unexpected patched file: %+v", patched.Body)
	}

	diff, err := h.GetArtifactEditSessionDiff(ctx, &GetArtifactEditSessionDiffInput{ID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	if diff.Body.Summary != "1 file(s) changed" || !strings.Contains(diff.Body.UnifiedDiff, "+freshness") {
		t.Fatalf("unexpected diff: %+v", diff.Body)
	}
	if len(diff.Body.Files) != 1 || len(diff.Body.Files[0].Hunks) != 1 {
		t.Fatalf("expected one file hunk, got %+v", diff.Body.Files)
	}
	firstHunkID := diff.Body.Files[0].Hunks[0].ID
	if firstHunkID == "" || !strings.HasPrefix(firstHunkID, "hunk_") {
		t.Fatalf("unexpected hunk id: %+v", diff.Body.Files[0].Hunks[0])
	}
	// The hunk carries its own content so a reviewer can render and decide it
	// without re-deriving from the file-level unified diff.
	firstHunk := diff.Body.Files[0].Hunks[0]
	if firstHunk.FileKey != "prd.md" {
		t.Fatalf("hunk file_key = %q, want prd.md", firstHunk.FileKey)
	}
	if firstHunk.BaseText != "old metric\n" {
		t.Fatalf("hunk base_text = %q, want %q", firstHunk.BaseText, "old metric\n")
	}
	if firstHunk.WorkingText != "freshness under five minutes" {
		t.Fatalf("hunk working_text = %q, want %q", firstHunk.WorkingText, "freshness under five minutes")
	}
	diff2, err := h.GetArtifactEditSessionDiff(ctx, &GetArtifactEditSessionDiffInput{ID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	if got := diff2.Body.Files[0].Hunks[0].ID; got != firstHunkID {
		t.Fatalf("hunk id should be stable, got %q want %q", got, firstHunkID)
	}
	if got := diff2.Body.Files[0].Hunks[0].State; got != "pending" {
		t.Fatalf("default hunk state = %q, want pending", got)
	}
	decide := &PatchArtifactEditSessionInput{ID: sessionID}
	decide.Body.HunkID = firstHunkID
	decide.Body.HunkState = "applied"
	if _, err := h.PatchArtifactEditSession(ctx, decide); err != nil {
		t.Fatal(err)
	}
	diff3, err := h.GetArtifactEditSessionDiff(ctx, &GetArtifactEditSessionDiffInput{ID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	if got := diff3.Body.Files[0].Hunks[0].State; got != "applied" {
		t.Fatalf("hunk state = %q, want applied", got)
	}
	bad := &PatchArtifactEditSessionInput{ID: sessionID}
	bad.Body.HunkID = "hunk_missing"
	bad.Body.HunkState = "rejected"
	if _, err := h.PatchArtifactEditSession(ctx, bad); err == nil {
		t.Fatal("expected missing hunk decision to fail")
	}

	save := &SaveArtifactEditSessionInput{ID: sessionID}
	save.Body.Summary = "Updated PRD metric"
	saved, err := h.SaveArtifactEditSession(ctx, save)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Body.RevisionID == "" || saved.Body.State != "saved" {
		t.Fatalf("unexpected revision: %+v", saved.Body)
	}
	got, err := h.GetArtifactSavedRevision(ctx, &GetArtifactSavedRevisionInput{RevisionID: saved.Body.RevisionID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Body.BaseArtifactID != artifactID {
		t.Fatalf("unexpected saved revision: %+v", got.Body)
	}
	listed, err := h.ListArtifactRevisions(ctx, &ListArtifactRevisionsInput{ID: artifactID})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Body.Items) != 1 || listed.Body.Items[0].RevisionID != saved.Body.RevisionID {
		t.Fatalf("unexpected revision list: %+v", listed.Body.Items)
	}
}

func TestCreateArtifactEditSession_SeedsResolvedWorkingFiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	artifactID := "art-edit-seed-" + strings.ReplaceAll(t.Name(), "/", "-")
	svc := &fakeArtifactEditArtifactService{
		artifact: &artifact.Artifact{
			ID:      artifactID,
			Version: "v1.0",
			Files: []artifact.File{
				{ArtifactID: artifactID, Path: "prd.md", S3Path: "s3/prd.md", SizeBytes: 9},
			},
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
		files: map[string][]byte{
			"prd.md": []byte("base body\n"),
		},
	}
	h := &Handlers{Artifacts: svc, ArtifactEdit: artifactedit.NewMemoryStore()}

	create := &CreateArtifactEditSessionInput{}
	create.Body.BaseArtifactID = artifactID
	create.Body.SourceKind = "stale_fix"
	create.Body.SourceID = "pfe-1"
	create.Body.WorkingFiles = []ArtifactEditWorkingFileInput{
		{Key: "prd.md", Content: "resolved body\n"},
	}
	opened, err := h.CreateArtifactEditSession(ctx, create)
	if err != nil {
		t.Fatal(err)
	}

	// The session is born already-resolved: working = override, base = artifact
	// content. The diff shows the file modified with no follow-up write.
	diff, err := h.GetArtifactEditSessionDiff(ctx, &GetArtifactEditSessionDiffInput{ID: opened.Body.ID})
	if err != nil {
		t.Fatal(err)
	}
	if diff.Body.Summary != "1 file(s) changed" {
		t.Fatalf("summary = %q, want %q", diff.Body.Summary, "1 file(s) changed")
	}
	if len(diff.Body.Files) != 1 || len(diff.Body.Files[0].Hunks) != 1 {
		t.Fatalf("expected one file hunk, got %+v", diff.Body.Files)
	}
	hunk := diff.Body.Files[0].Hunks[0]
	if hunk.BaseText != "base body\n" {
		t.Fatalf("base_text = %q, want %q", hunk.BaseText, "base body\n")
	}
	if hunk.WorkingText != "resolved body\n" {
		t.Fatalf("working_text = %q, want %q", hunk.WorkingText, "resolved body\n")
	}
}

func TestArtifactEditSession_SaveHonorsRejectAndRecordsLineage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	artifactID := "art-edit-reject-" + strings.ReplaceAll(t.Name(), "/", "-")
	svc := &fakeArtifactEditArtifactService{
		artifact: &artifact.Artifact{
			ID:        artifactID,
			FeatureID: "feed-freshness",
			Version:   "v1.0",
			Files: []artifact.File{
				{ArtifactID: artifactID, Path: "prd.md", SizeBytes: 8},
				{ArtifactID: artifactID, Path: "spec.md", SizeBytes: 9},
			},
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
		files: map[string][]byte{
			"prd.md":  []byte("old prd\n"),
			"spec.md": []byte("old spec\n"),
		},
	}
	h := &Handlers{Artifacts: svc, ArtifactEdit: artifactedit.NewMemoryStore()}

	create := &CreateArtifactEditSessionInput{}
	create.Body.BaseArtifactID = artifactID
	create.Body.BaseRevisionID = "rev-parent"
	opened, err := h.CreateArtifactEditSession(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	sid := opened.Body.ID

	for _, kv := range []struct{ key, val string }{{"prd.md", "new prd\n"}, {"spec.md", "new spec\n"}} {
		p := &PatchArtifactEditSessionInput{ID: sid}
		p.Body.FileKey = kv.key
		p.Body.Patch = kv.val
		if _, err := h.PatchArtifactEditSession(ctx, p); err != nil {
			t.Fatalf("patch %s: %v", kv.key, err)
		}
	}

	diff, err := h.GetArtifactEditSessionDiff(ctx, &GetArtifactEditSessionDiffInput{ID: sid})
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Body.Files) != 2 {
		t.Fatalf("expected 2 modified files, got %+v", diff.Body.Files)
	}
	var prdHunk string
	for _, f := range diff.Body.Files {
		if f.Key == "prd.md" {
			prdHunk = f.Hunks[0].ID
		}
	}
	if prdHunk == "" {
		t.Fatalf("no prd.md hunk in %+v", diff.Body.Files)
	}

	reject := &PatchArtifactEditSessionInput{ID: sid}
	reject.Body.HunkID = prdHunk
	reject.Body.HunkState = "rejected"
	reject.Body.DecidedBy = "lead@example.com"
	if _, err := h.PatchArtifactEditSession(ctx, reject); err != nil {
		t.Fatal(err)
	}

	saved, err := h.SaveArtifactEditSession(ctx, &SaveArtifactEditSessionInput{ID: sid})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Body.State != "saved" || saved.Body.RevisionID == "" {
		t.Fatalf("unexpected save: %+v", saved.Body)
	}
	if saved.Body.ParentRevisionID != "rev-parent" {
		t.Fatalf("parent_revision_id = %q, want rev-parent", saved.Body.ParentRevisionID)
	}
	if saved.Body.LineageRootArtifactID != artifactID {
		t.Fatalf("lineage_root_artifact_id = %q, want %q", saved.Body.LineageRootArtifactID, artifactID)
	}
	// Saving a draft revision must not overwrite the approved artifact.
	if svc.publishCalls != 0 || svc.updateStatusCalls != 0 {
		t.Fatalf("save mutated artifacts: publish=%d updateStatus=%d", svc.publishCalls, svc.updateStatusCalls)
	}
	if saved.Body.ArtifactID != "" {
		t.Fatalf("draft revision should not be materialized as an artifact, got %q", saved.Body.ArtifactID)
	}

	// The rejected prd hunk is excluded from the saved revision; only spec remains.
	rdiff, err := h.GetArtifactSavedRevisionDiff(ctx, &GetArtifactSavedRevisionDiffInput{RevisionID: saved.Body.RevisionID})
	if err != nil {
		t.Fatal(err)
	}
	if len(rdiff.Body.Files) != 1 || rdiff.Body.Files[0].Key != "spec.md" {
		t.Fatalf("saved diff should contain only spec.md, got %+v", rdiff.Body.Files)
	}
	if strings.Contains(rdiff.Body.UnifiedDiff, "new prd") {
		t.Fatalf("rejected prd content leaked into saved revision diff: %q", rdiff.Body.UnifiedDiff)
	}
	// Saved revisions keep only hunk metadata; the per-hunk base/working text is
	// a live-session concern and is not persisted into diff_json.
	for _, f := range rdiff.Body.Files {
		for _, hunk := range f.Hunks {
			if hunk.BaseText != "" || hunk.WorkingText != "" || hunk.FileKey != "" {
				t.Fatalf("saved revision hunk should carry metadata only, got %+v", hunk)
			}
		}
	}
}

func TestArtifactEditProposals_QueueAndResolve(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	artifactID := "art-proposal-" + strings.ReplaceAll(t.Name(), "/", "-")
	svc := &fakeArtifactEditArtifactService{
		artifact: &artifact.Artifact{
			ID: artifactID, FeatureID: "feat", Version: "v1.0",
			Files: []artifact.File{{ArtifactID: artifactID, Path: "prd.md", SizeBytes: 4}},
		},
		files: map[string][]byte{"prd.md": []byte("old\n")},
	}
	h := &Handlers{Artifacts: svc, ArtifactEdit: artifactedit.NewMemoryStore()}

	// A proposal is an edit session tagged with its origin.
	create := &CreateArtifactEditSessionInput{}
	create.Body.BaseArtifactID = artifactID
	create.Body.SourceKind = "feedback_event"
	create.Body.SourceID = "pfe-1"
	opened, err := h.CreateArtifactEditSession(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Body.SourceKind != "feedback_event" || opened.Body.SourceID != "pfe-1" {
		t.Fatalf("created proposal source = %q/%q", opened.Body.SourceKind, opened.Body.SourceID)
	}

	// A plain (unsourced) session must not appear in the queue.
	plain := &CreateArtifactEditSessionInput{}
	plain.Body.BaseArtifactID = artifactID
	if _, err := h.CreateArtifactEditSession(ctx, plain); err != nil {
		t.Fatal(err)
	}

	q, err := h.ListArtifactEditProposals(ctx, &ListArtifactEditProposalsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Body.Items) != 1 || q.Body.Items[0].ID != opened.Body.ID {
		t.Fatalf("proposal queue = %+v, want only %s", q.Body.Items, opened.Body.ID)
	}

	// Approve = save (creates a draft revision); the proposal leaves the queue.
	if _, err := h.SaveArtifactEditSession(ctx, &SaveArtifactEditSessionInput{ID: opened.Body.ID}); err != nil {
		t.Fatal(err)
	}
	q2, err := h.ListArtifactEditProposals(ctx, &ListArtifactEditProposalsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(q2.Body.Items) != 0 {
		t.Fatalf("approved proposal should leave the queue, got %+v", q2.Body.Items)
	}
}

// TestArtifactEditProposal_ApproveMergedDeliveryAdvancesFeatureToActive proves that
// approving a delivery.pr_merged-sourced proposal advances the linked Feature from
// planned → active, closing the state-movement half of the reconciliation loop.
func TestArtifactEditProposal_ApproveMergedDeliveryAdvancesFeatureToActive(t *testing.T) {
	ctx := context.Background()
	db := newTestGormDB(t)

	intRepo := storagedb.NewIntegrationRepository(db)
	workBoardRepo := storagedb.NewWorkBoardRepository(db)

	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID: "int-feature-advance", Provider: integrations.ProviderGitLab, Name: "GL", Status: integrations.StatusConnected,
	})
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}

	feature, err := workBoardRepo.CreateFeature(ctx, workboard.Feature{
		ID: "feat-loyalty", Key: "FEAT-LOYALTY", Name: "Loyalty checkout", Status: "planned", Version: 1,
	})
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}

	feedback, err := intRepo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
		IntegrationID: integration.ID,
		FeatureID:     feature.ID,
		EventType:     integrations.FeedbackEventPRMerged,
		Status:        integrations.FeedbackStatusPending,
	})
	if err != nil {
		t.Fatalf("CreateGovernanceFeedbackEvent: %v", err)
	}

	artifactID := "art-feature-advance-" + strings.ReplaceAll(t.Name(), "/", "-")
	svc := &fakeArtifactEditArtifactService{
		artifact: &artifact.Artifact{
			ID: artifactID, FeatureID: "feat", Version: "v1.0",
			Files: []artifact.File{{ArtifactID: artifactID, Path: "spec.md", SizeBytes: 4}},
		},
		files: map[string][]byte{"spec.md": []byte("spec\n")},
	}
	h := &Handlers{
		Artifacts:    svc,
		ArtifactEdit: artifactedit.NewMemoryStore(),
		Integrations: integrations.NewService(intRepo),
		WorkBoard:    workBoardRepo,
	}

	create := &CreateArtifactEditSessionInput{}
	create.Body.BaseArtifactID = artifactID
	create.Body.SourceKind = "feedback_event"
	create.Body.SourceID = feedback.ID
	opened, err := h.CreateArtifactEditSession(ctx, create)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := h.SaveArtifactEditSession(ctx, &SaveArtifactEditSessionInput{ID: opened.Body.ID}); err != nil {
		t.Fatal(err)
	}

	// Feature must advance from planned → active after the merged-delivery proposal is approved.
	got, err := workBoardRepo.GetFeature(ctx, feature.ID)
	if err != nil {
		t.Fatalf("GetFeature: %v", err)
	}
	if got.Status != "active" {
		t.Fatalf("expected Feature status active after approve, got %q", got.Status)
	}
}

// TestArtifactEditProposal_ReconcilesFeedbackOnVerdict proves the reconciliation
// moat closes: a feedback-sourced proposal, once a human approves (save) or
// rejects (discard) it, reconciles the originating feedback signal — pending →
// processed on approve, pending → ignored on reject.
func TestArtifactEditProposal_ReconcilesFeedbackOnVerdict(t *testing.T) {
	for _, tc := range []struct {
		name       string
		approve    bool
		wantStatus string
	}{
		{name: "approve marks processed", approve: true, wantStatus: integrations.FeedbackStatusProcessed},
		{name: "reject marks ignored", approve: false, wantStatus: integrations.FeedbackStatusIgnored},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := newTestGormDB(t)
			intRepo := storagedb.NewIntegrationRepository(db)
			integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
				ID:       "int-1",
				Provider: integrations.ProviderGitLab,
				Name:     "Reconcile GitLab",
				Status:   integrations.StatusConnected,
			})
			if err != nil {
				t.Fatalf("CreateIntegration: %v", err)
			}
			feedback, err := intRepo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
				IntegrationID:   integration.ID,
				ChangeRequestID: "cr-loyalty-v1",
				EventType:       integrations.FeedbackEventPRMerged,
				Status:          integrations.FeedbackStatusPending,
			})
			if err != nil {
				t.Fatalf("CreateGovernanceFeedbackEvent: %v", err)
			}

			artifactID := "art-" + strings.ReplaceAll(t.Name(), "/", "-")
			svc := &fakeArtifactEditArtifactService{
				artifact: &artifact.Artifact{
					ID: artifactID, FeatureID: "feat", Version: "v1.0",
					Files: []artifact.File{{ArtifactID: artifactID, Path: "prd.md", SizeBytes: 4}},
				},
				files: map[string][]byte{"prd.md": []byte("old\n")},
			}
			h := &Handlers{
				Artifacts:    svc,
				ArtifactEdit: artifactedit.NewMemoryStore(),
				Integrations: integrations.NewService(intRepo),
			}

			create := &CreateArtifactEditSessionInput{}
			create.Body.BaseArtifactID = artifactID
			create.Body.SourceKind = "feedback_event"
			create.Body.SourceID = feedback.ID
			opened, err := h.CreateArtifactEditSession(ctx, create)
			if err != nil {
				t.Fatal(err)
			}
			// Draft content into the proposal so it carries a real diff.
			replace := &ReplaceArtifactEditSessionFileInput{ID: opened.Body.ID, Key: "prd.md"}
			replace.Body.Content = "new\n"
			if _, err := h.ReplaceArtifactEditSessionFile(ctx, replace); err != nil {
				t.Fatal(err)
			}

			if tc.approve {
				if _, err := h.SaveArtifactEditSession(ctx, &SaveArtifactEditSessionInput{ID: opened.Body.ID}); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := h.DeleteArtifactEditSession(ctx, &DeleteArtifactEditSessionInput{ID: opened.Body.ID}); err != nil {
					t.Fatal(err)
				}
			}

			got, err := intRepo.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{Status: tc.wantStatus, Limit: 10})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].ID != feedback.ID {
				t.Fatalf("expected feedback %s to be %s, got %#v", feedback.ID, tc.wantStatus, got)
			}
		})
	}
}

// TestArtifactEditProposal_NonFeedbackVerdictDoesNotReconcile locks the safety
// FR-8.6 (stale_fix) inherits from gate_fix: a proposal whose source_kind is not
// "feedback_event" surfaces in the review queue and resolves cleanly on a verdict,
// but the verdict must NOT mutate any feedback signal — reconciliation keys on
// source_kind, not source_id. The test arms the trap by tagging the session with a
// real pending feedback event's id while leaving source_kind as stale_fix/gate_fix;
// if the reconciliation guard ever drops the source_kind check, the event would
// flip to processed/ignored and this fails.
func TestArtifactEditProposal_NonFeedbackVerdictDoesNotReconcile(t *testing.T) {
	for _, tc := range []struct {
		name       string
		sourceKind string
		approve    bool
	}{
		{name: "stale_fix approve", sourceKind: "stale_fix", approve: true},
		{name: "stale_fix reject", sourceKind: "stale_fix", approve: false},
		{name: "gate_fix approve", sourceKind: "gate_fix", approve: true},
		{name: "gate_fix reject", sourceKind: "gate_fix", approve: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := newTestGormDB(t)
			intRepo := storagedb.NewIntegrationRepository(db)
			integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
				ID:       "int-nonfeedback",
				Provider: integrations.ProviderGitLab,
				Name:     "NonFeedback GitLab",
				Status:   integrations.StatusConnected,
			})
			if err != nil {
				t.Fatalf("CreateIntegration: %v", err)
			}
			// A real pending feedback event whose id we (mis)use as the proposal's
			// source_id. The verdict must leave it untouched.
			feedback, err := intRepo.CreateGovernanceFeedbackEvent(ctx, integrations.GovernanceFeedbackEvent{
				IntegrationID:   integration.ID,
				ChangeRequestID: "cr-nonfeedback",
				EventType:       integrations.FeedbackEventPRMerged,
				Status:          integrations.FeedbackStatusPending,
			})
			if err != nil {
				t.Fatalf("CreateGovernanceFeedbackEvent: %v", err)
			}

			artifactID := "art-nonfeedback-" + strings.ReplaceAll(t.Name(), "/", "-")
			svc := &fakeArtifactEditArtifactService{
				artifact: &artifact.Artifact{
					ID: artifactID, FeatureID: "feat", Version: "v1.0",
					Files: []artifact.File{{ArtifactID: artifactID, Path: "prd.md", SizeBytes: 4}},
				},
				files: map[string][]byte{"prd.md": []byte("old\n")},
			}
			h := &Handlers{
				Artifacts:    svc,
				ArtifactEdit: artifactedit.NewMemoryStore(),
				Integrations: integrations.NewService(intRepo),
			}

			// Open a non-feedback proposal tagged with the feedback event's id.
			create := &CreateArtifactEditSessionInput{}
			create.Body.BaseArtifactID = artifactID
			create.Body.SourceKind = tc.sourceKind
			create.Body.SourceID = feedback.ID
			opened, err := h.CreateArtifactEditSession(ctx, create)
			if err != nil {
				t.Fatal(err)
			}

			// It must surface in the proposal review queue regardless of source_kind.
			q, err := h.ListArtifactEditProposals(ctx, &ListArtifactEditProposalsInput{})
			if err != nil {
				t.Fatal(err)
			}
			if len(q.Body.Items) != 1 || q.Body.Items[0].ID != opened.Body.ID {
				t.Fatalf("proposal queue = %+v, want only %s (%s)", q.Body.Items, opened.Body.ID, tc.sourceKind)
			}

			// Draft real content so the proposal carries a diff.
			replace := &ReplaceArtifactEditSessionFileInput{ID: opened.Body.ID, Key: "prd.md"}
			replace.Body.Content = "new\n"
			if _, err := h.ReplaceArtifactEditSessionFile(ctx, replace); err != nil {
				t.Fatal(err)
			}

			// Approve = save (draft revision); reject = discard. Both must succeed
			// without error for a non-feedback source.
			if tc.approve {
				if _, err := h.SaveArtifactEditSession(ctx, &SaveArtifactEditSessionInput{ID: opened.Body.ID}); err != nil {
					t.Fatalf("approve (save) errored for %s: %v", tc.sourceKind, err)
				}
			} else {
				if _, err := h.DeleteArtifactEditSession(ctx, &DeleteArtifactEditSessionInput{ID: opened.Body.ID}); err != nil {
					t.Fatalf("reject (discard) errored for %s: %v", tc.sourceKind, err)
				}
			}

			// The resolved proposal leaves the queue.
			q2, err := h.ListArtifactEditProposals(ctx, &ListArtifactEditProposalsInput{})
			if err != nil {
				t.Fatal(err)
			}
			if len(q2.Body.Items) != 0 {
				t.Fatalf("resolved %s proposal should leave the queue, got %+v", tc.sourceKind, q2.Body.Items)
			}

			// The feedback event must remain pending — the verdict on a non-feedback
			// proposal reconciles nothing.
			pending, err := intRepo.ListGovernanceFeedbackEvents(ctx, integrations.GovernanceFeedbackFilter{
				Status: integrations.FeedbackStatusPending, Limit: 10,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(pending) != 1 || pending[0].ID != feedback.ID {
				t.Fatalf("feedback %s must stay pending after a %s verdict, got %#v", feedback.ID, tc.sourceKind, pending)
			}
		})
	}
}

func TestCreateArtifactEditSession_SkipsMissingBaseFiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	artifactID := "art-edit-missing-file-" + strings.ReplaceAll(t.Name(), "/", "-")
	svc := &fakeArtifactEditArtifactService{
		artifact: &artifact.Artifact{
			ID:      artifactID,
			Version: "v1",
			Files: []artifact.File{
				{ArtifactID: artifactID, Path: "prd.md", SizeBytes: 12},
				{ArtifactID: artifactID, Path: "spec.md", SizeBytes: 16},
			},
		},
		files: map[string][]byte{
			"prd.md": []byte("# PRD\n\nAvailable\n"),
		},
	}
	h := &Handlers{Artifacts: svc, ArtifactEdit: artifactedit.NewMemoryStore()}

	create := &CreateArtifactEditSessionInput{}
	create.Body.ArtifactID = artifactID
	out, err := h.CreateArtifactEditSession(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := out.Body.ID
	if sessionID == "" {
		t.Fatal("expected session id")
	}

	files, err := h.ListArtifactEditSessionFiles(ctx, &ListArtifactEditSessionFilesInput{ID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	if len(files.Body.Items) != 1 {
		t.Fatalf("files len = %d, want 1", len(files.Body.Items))
	}
	if files.Body.Items[0].Key != "prd.md" {
		t.Fatalf("file key = %q, want prd.md", files.Body.Items[0].Key)
	}
}

func TestArtifactEditSession_SaveReturnsStaleBaseWhenBaseVersionDrifts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	artifactID := "art-edit-stale-base-" + strings.ReplaceAll(t.Name(), "/", "-")
	svc := &fakeArtifactEditArtifactService{
		artifact: &artifact.Artifact{
			ID:        artifactID,
			FeatureID: "feed-freshness",
			Version:   "v1.0",
			Files: []artifact.File{
				{ArtifactID: artifactID, Path: "prd.md", S3Path: "s3/prd.md", SizeBytes: 12},
			},
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
		files: map[string][]byte{
			"prd.md": []byte("old metric\n"),
		},
	}
	h := &Handlers{Artifacts: svc, ArtifactEdit: artifactedit.NewMemoryStore()}
	create := &CreateArtifactEditSessionInput{}
	create.Body.BaseArtifactID = artifactID
	opened, err := h.CreateArtifactEditSession(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate base artifact being superseded after session opened.
	svc.artifact.Version = "v1.1"
	save := &SaveArtifactEditSessionInput{ID: opened.Body.ID}
	out, err := h.SaveArtifactEditSession(ctx, save)
	if err != nil {
		t.Fatal(err)
	}
	if out.Body.State != "stale_base" {
		t.Fatalf("state=%q want stale_base", out.Body.State)
	}
	if out.Body.RevisionID != "" {
		t.Fatalf("revision id should be empty on stale base, got %q", out.Body.RevisionID)
	}
}

func TestArtifactEditSession_CompareToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	artifactID := "art-compare-token-" + strings.ReplaceAll(t.Name(), "/", "-")
	svc := &fakeArtifactEditArtifactService{
		artifact: &artifact.Artifact{
			ID:      artifactID,
			Version: "v1.0",
			Files: []artifact.File{
				{ArtifactID: artifactID, Path: "prd.md", S3Path: "s3/prd.md", SizeBytes: 3},
			},
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
		files: map[string][]byte{"prd.md": []byte("ok\n")},
	}
	h := &Handlers{Artifacts: svc, ArtifactEdit: artifactedit.NewMemoryStore()}

	create := &CreateArtifactEditSessionInput{}
	create.Body.BaseArtifactID = artifactID
	opened, err := h.CreateArtifactEditSession(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	token := opened.Body.CompareToken
	if token == "" {
		t.Fatal("compare_token must be non-empty after create")
	}

	// Wrong token → stale_base.
	saveWrong := &SaveArtifactEditSessionInput{ID: opened.Body.ID}
	saveWrong.Body.CompareToken = "wrong-token"
	out, err := h.SaveArtifactEditSession(ctx, saveWrong)
	if err != nil {
		t.Fatal(err)
	}
	if out.Body.State != "stale_base" {
		t.Fatalf("expected stale_base on wrong token, got %q", out.Body.State)
	}

	// Create fresh session and save with correct token → saved.
	opened2, err := h.CreateArtifactEditSession(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	saveCorrect := &SaveArtifactEditSessionInput{ID: opened2.Body.ID}
	saveCorrect.Body.CompareToken = opened2.Body.CompareToken
	out2, err := h.SaveArtifactEditSession(ctx, saveCorrect)
	if err != nil {
		t.Fatal(err)
	}
	if out2.Body.State != "saved" {
		t.Fatalf("expected saved with correct token, got %q", out2.Body.State)
	}
}

// TestSaveArtifactEditSession_ProposalMaterializesArtifact verifies that
// approving a feedback_event proposal creates a new draft artifact (per spec §1).
func TestSaveArtifactEditSession_ProposalMaterializesArtifact(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	artifactID := "art-materialize-" + strings.ReplaceAll(t.Name(), "/", "-")
	svc := &fakeArtifactEditArtifactService{
		artifact: &artifact.Artifact{
			ID:          artifactID,
			FeatureID:   "feat-mat",
			Version:     "v1.0",
			RequestType: artifact.RequestTypeChangeRequest,
			ImpactLevel: artifact.ImpactLevelMedium,
			Files: []artifact.File{
				{ArtifactID: artifactID, Path: "spec.md"},
			},
		},
		files: map[string][]byte{
			"spec.md": []byte("old spec\n"),
		},
	}
	h := &Handlers{Artifacts: svc, ArtifactEdit: artifactedit.NewMemoryStore()}

	create := &CreateArtifactEditSessionInput{}
	create.Body.BaseArtifactID = artifactID
	create.Body.SourceKind = "feedback_event"
	create.Body.SourceID = "evt-123"
	opened, err := h.CreateArtifactEditSession(ctx, create)
	if err != nil {
		t.Fatal(err)
	}

	patch := &PatchArtifactEditSessionInput{ID: opened.Body.ID}
	patch.Body.FileKey = "spec.md"
	patch.Body.Patch = "new spec\n"
	if _, err := h.PatchArtifactEditSession(ctx, patch); err != nil {
		t.Fatal(err)
	}

	saved, err := h.SaveArtifactEditSession(ctx, &SaveArtifactEditSessionInput{ID: opened.Body.ID})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Body.State != "saved" {
		t.Fatalf("expected saved, got %q", saved.Body.State)
	}
	if saved.Body.MaterializedArtifactID == "" {
		t.Fatal("expected materialized_artifact_id to be set for proposal session")
	}
	if svc.publishCalls != 1 {
		t.Fatalf("expected 1 Publish call, got %d", svc.publishCalls)
	}
}

// TestSaveArtifactEditSession_CodingAgentUpdateMaterializes verifies that approving
// a coding_agent_update session also creates a new draft artifact (per spec §1).
func TestSaveArtifactEditSession_CodingAgentUpdateMaterializes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	artifactID := "art-cau-mat-" + strings.ReplaceAll(t.Name(), "/", "-")
	svc := &fakeArtifactEditArtifactService{
		artifact: &artifact.Artifact{
			ID:          artifactID,
			FeatureID:   "feat-cau",
			Version:     "v0.2",
			RequestType: artifact.RequestTypeChangeRequest,
			ImpactLevel: artifact.ImpactLevelLow,
			Files: []artifact.File{
				{ArtifactID: artifactID, Path: "docs/spec.md"},
			},
		},
		files: map[string][]byte{
			"docs/spec.md": []byte("# old spec\n"),
		},
	}
	h := &Handlers{Artifacts: svc, ArtifactEdit: artifactedit.NewMemoryStore()}

	create := &CreateArtifactEditSessionInput{}
	create.Body.BaseArtifactID = artifactID
	create.Body.SourceKind = "coding_agent_update"
	create.Body.SourceID = "cau-agent-1"
	opened, err := h.CreateArtifactEditSession(ctx, create)
	if err != nil {
		t.Fatal(err)
	}

	patch := &PatchArtifactEditSessionInput{ID: opened.Body.ID}
	patch.Body.FileKey = "docs/spec.md"
	patch.Body.Patch = "# updated spec\n"
	if _, err := h.PatchArtifactEditSession(ctx, patch); err != nil {
		t.Fatal(err)
	}

	saved, err := h.SaveArtifactEditSession(ctx, &SaveArtifactEditSessionInput{ID: opened.Body.ID})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Body.State != "saved" {
		t.Fatalf("expected saved, got %q", saved.Body.State)
	}
	if saved.Body.MaterializedArtifactID == "" {
		t.Fatal("expected materialized_artifact_id to be set for coding_agent_update session")
	}
	if svc.publishCalls != 1 {
		t.Fatalf("expected 1 Publish call, got %d", svc.publishCalls)
	}
}

// TestSaveArtifactEditSession_NonProposalNoMaterialize verifies that saving a
// regular (non-proposal) session does NOT create a new artifact (per spec §1).
func TestSaveArtifactEditSession_NonProposalNoMaterialize(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	artifactID := "art-no-mat-" + strings.ReplaceAll(t.Name(), "/", "-")
	svc := &fakeArtifactEditArtifactService{
		artifact: &artifact.Artifact{
			ID:        artifactID,
			FeatureID: "feat-no-mat",
			Version:   "v1.0",
			Files: []artifact.File{
				{ArtifactID: artifactID, Path: "spec.md"},
			},
		},
		files: map[string][]byte{
			"spec.md": []byte("old spec\n"),
		},
	}
	h := &Handlers{Artifacts: svc, ArtifactEdit: artifactedit.NewMemoryStore()}

	create := &CreateArtifactEditSessionInput{}
	create.Body.BaseArtifactID = artifactID
	// No SourceKind → not a proposal.
	opened, err := h.CreateArtifactEditSession(ctx, create)
	if err != nil {
		t.Fatal(err)
	}

	saved, err := h.SaveArtifactEditSession(ctx, &SaveArtifactEditSessionInput{ID: opened.Body.ID})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Body.MaterializedArtifactID != "" {
		t.Fatalf("expected no materialized_artifact_id for non-proposal, got %q", saved.Body.MaterializedArtifactID)
	}
	if svc.publishCalls != 0 {
		t.Fatalf("expected 0 Publish calls for non-proposal, got %d", svc.publishCalls)
	}
}
