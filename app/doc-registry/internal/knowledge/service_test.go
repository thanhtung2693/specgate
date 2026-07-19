package knowledge

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/knowledgequeue"
)

// fakeEnqueuer captures enqueued ingest tasks (async driver).
type fakeEnqueuer struct{ tasks []knowledgequeue.Task }

func (f *fakeEnqueuer) EnqueueKnowledgeIngest(_ context.Context, t knowledgequeue.Task) error {
	f.tasks = append(f.tasks, t)
	return nil
}

// toggleEmbedder fails while fail is true, so a test can drive an ingest failure
// then flip it to let a retry/worker succeed.
type toggleEmbedder struct {
	dim  int
	fail bool
}

func (e *toggleEmbedder) Embed(_ context.Context, text string, _ EmbeddingPurpose) ([]float32, error) {
	if e.fail {
		return nil, errors.New("embedding provider unavailable")
	}
	v := make([]float32, e.dim)
	if len(text) > 0 {
		v[0] = 1
	}
	return v, nil
}

func TestKnowledgeObjectKeysAlwaysUseDocumentWorkspace(t *testing.T) {
	t.Parallel()
	svc, err := NewService(newMemoryRepo(), &memoryStore{objects: map[string][]byte{}}, &memoryVectors{}, constantEmbedder{dim: 16}, 1024, "")
	if err != nil {
		t.Fatal(err)
	}

	key := svc.rawObjectKey("ws-a", "doc-1", "v1", "input.txt")
	const prefix = "workspaces/ws-a/documents/doc-1/v1/raw/"
	if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, "/input.txt") {
		t.Fatalf("raw object key = %q, want %s<attempt>/input.txt", key, prefix)
	}
	attemptAndName := strings.TrimPrefix(key, prefix)
	if len(strings.Split(attemptAndName, "/")) != 2 {
		t.Fatalf("raw object key = %q, want one unique attempt segment", key)
	}
}

func TestKnowledgeCreateRejectsUnsafeWorkspaceIDBeforeWriting(t *testing.T) {
	t.Parallel()
	for _, workspaceID := range []string{"../ws-b", "ws/a", `ws\a`, "ws..b", "."} {
		workspaceID := workspaceID
		t.Run(workspaceID, func(t *testing.T) {
			t.Parallel()
			repo := newMemoryRepo()
			store := &memoryStore{objects: map[string][]byte{}}
			svc, err := NewService(repo, store, &memoryVectors{}, constantEmbedder{dim: 16}, 1024, "")
			if err != nil {
				t.Fatal(err)
			}
			_, err = svc.CreateText(context.Background(), CreateTextInput{
				Metadata: Metadata{
					WorkspaceID: workspaceID, Title: "Unsafe", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh,
				},
				Content: "must not persist",
			})
			if err == nil {
				t.Fatalf("CreateText accepted unsafe workspace ID %q", workspaceID)
			}
			if len(repo.docs) != 0 || len(store.objects) != 0 {
				t.Fatalf("unsafe create wrote documents=%d objects=%d", len(repo.docs), len(store.objects))
			}
		})
	}
}

func TestServiceCreateTextEnqueuesUnderAsyncDriver(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepo()
	store := &memoryStore{objects: map[string][]byte{}}
	vectors := &memoryVectors{}
	enq := &fakeEnqueuer{}
	svc, err := NewService(repo, store, vectors, constantEmbedder{dim: 16}, 1024, "")
	if err != nil {
		t.Fatal(err)
	}
	svc = svc.WithIngestEnqueuer(enq)
	ctx := context.Background()

	created, err := svc.CreateText(ctx, CreateTextInput{
		Metadata: Metadata{WorkspaceID: "ws-a", Title: "A", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
		Content:  "refunds require reviewer approval",
	})
	if err != nil {
		t.Fatal(err)
	}
	// 202 contract: returns uploaded; ingestion is deferred to the worker.
	if created.Status != StatusUploaded {
		t.Fatalf("create status = %q, want uploaded", created.Status)
	}
	if persisted, _ := repo.Get(ctx, created.DocumentID, created.Version); persisted.Status != StatusUploaded {
		t.Fatalf("async driver must not ingest inline; persisted status = %q", persisted.Status)
	}
	if len(vectors.points) != 0 {
		t.Fatalf("async driver upserted %d vectors at create time, want 0", len(vectors.points))
	}
	if len(enq.tasks) != 1 || enq.tasks[0].DocumentID != created.DocumentID || len(enq.tasks[0].Content) == 0 {
		t.Fatalf("enqueued task = %+v", enq.tasks)
	}

	// Worker runs the enqueued task → walks states to indexed.
	if err := svc.ProcessKnowledgeIngest(ctx, enq.tasks[0]); err != nil {
		t.Fatal(err)
	}
	if persisted, _ := repo.Get(ctx, created.DocumentID, created.Version); persisted.Status != StatusIndexed {
		t.Fatalf("after worker, status = %q, want indexed", persisted.Status)
	}
	if len(vectors.points) == 0 {
		t.Fatal("worker should have upserted vectors")
	}
}

func TestServiceCreateTextRejectsConfiguredSizeLimitBeforeWriting(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepo()
	store := &memoryStore{objects: map[string][]byte{}}
	svc, err := NewService(repo, store, &memoryVectors{}, constantEmbedder{dim: 16}, 4, "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.CreateText(context.Background(), CreateTextInput{
		Metadata: Metadata{WorkspaceID: "ws-a", Title: "A", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
		Content:  "12345",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("CreateText error = %v, want ErrValidation", err)
	}
	if len(store.objects) != 0 {
		t.Fatalf("stored objects = %v, want none", store.objects)
	}
}

func TestServiceRetryReingestsFailedDocumentWithoutDeleting(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepo()
	store := &memoryStore{objects: map[string][]byte{}}
	vectors := &memoryVectors{}
	emb := &toggleEmbedder{dim: 16, fail: true}
	svc, err := NewService(repo, store, vectors, emb, 1024, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Sync driver: create runs ingest inline, which fails on embed.
	created, err := svc.CreateText(ctx, CreateTextInput{
		Metadata: Metadata{WorkspaceID: "ws-a", Title: "A", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
		Content:  "refunds require reviewer approval",
	})
	if err != nil {
		t.Fatalf("create should still return 202, got err: %v", err)
	}
	if persisted, _ := repo.Get(ctx, created.DocumentID, created.Version); persisted.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", persisted.Status)
	}

	// The provider recovers; retry re-ingests the SAME version to indexed.
	emb.fail = false
	out, err := svc.Retry(ctx, created.DocumentID, created.Version)
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != created.Version {
		t.Fatalf("retry changed version %q -> %q", created.Version, out.Version)
	}
	if out.Status != StatusUploaded {
		t.Fatalf("retry returns uploaded snapshot, got %q", out.Status)
	}
	persisted, _ := repo.Get(ctx, created.DocumentID, created.Version)
	if persisted.Status != StatusIndexed {
		t.Fatalf("after retry, status = %q, want indexed", persisted.Status)
	}
	if persisted.ErrorMessage != "" {
		t.Fatalf("retry success should clear the error, got %q", persisted.ErrorMessage)
	}

	// Retrying a non-failed version is rejected.
	if _, err := svc.Retry(ctx, created.DocumentID, created.Version); !errors.Is(err, ErrValidation) {
		t.Fatalf("retry on indexed doc err = %v, want ErrValidation", err)
	}
}

// TestServiceRetryReEnqueuesUnderAsyncDriver: under the redis/async driver, Retry
// on a failed document enqueues a NEW ingest task rather than ingesting inline, and
// no vectors leak until the worker runs it. Only the sync-driver Retry path was
// covered; the enqueuer != nil branch of Retry->startIngest had no test.
func TestServiceRetryReEnqueuesUnderAsyncDriver(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepo()
	store := &memoryStore{objects: map[string][]byte{}}
	vectors := &memoryVectors{}
	emb := &toggleEmbedder{dim: 16, fail: true}
	enq := &fakeEnqueuer{}
	svc, err := NewService(repo, store, vectors, emb, 1024, "")
	if err != nil {
		t.Fatal(err)
	}
	svc = svc.WithIngestEnqueuer(enq)
	ctx := context.Background()

	created, err := svc.CreateText(ctx, CreateTextInput{
		Metadata: Metadata{WorkspaceID: "ws-a", Title: "A", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
		Content:  "refunds require reviewer approval",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(enq.tasks) != 1 {
		t.Fatalf("create enqueued %d tasks, want 1", len(enq.tasks))
	}
	// Worker runs the first task while the provider is down → doc fails, no vectors.
	_ = svc.ProcessKnowledgeIngest(ctx, enq.tasks[0])
	if persisted, _ := repo.Get(ctx, created.DocumentID, created.Version); persisted.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", persisted.Status)
	}
	if len(vectors.points) != 0 {
		t.Fatalf("failed ingest leaked %d vectors, want 0", len(vectors.points))
	}

	// Provider recovers; retry must RE-ENQUEUE (async), not ingest inline.
	emb.fail = false
	out, err := svc.Retry(ctx, created.DocumentID, created.Version)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != StatusUploaded {
		t.Fatalf("async retry returns uploaded, got %q", out.Status)
	}
	if len(enq.tasks) != 2 || enq.tasks[1].DocumentID != created.DocumentID {
		t.Fatalf("retry should enqueue a second task, got %+v", enq.tasks)
	}
	if len(vectors.points) != 0 {
		t.Fatalf("async retry ingested inline (%d vectors); it must defer to the worker", len(vectors.points))
	}
	if persisted, _ := repo.Get(ctx, created.DocumentID, created.Version); persisted.Status != StatusUploaded {
		t.Fatalf("after async retry, status = %q, want uploaded (re-enqueued)", persisted.Status)
	}

	// Worker runs the retry task → indexed, error cleared, vectors upserted.
	if err := svc.ProcessKnowledgeIngest(ctx, enq.tasks[1]); err != nil {
		t.Fatal(err)
	}
	persisted, _ := repo.Get(ctx, created.DocumentID, created.Version)
	if persisted.Status != StatusIndexed || persisted.ErrorMessage != "" {
		t.Fatalf("after worker retry, status=%q err=%q, want indexed + cleared", persisted.Status, persisted.ErrorMessage)
	}
	if len(vectors.points) == 0 {
		t.Fatal("worker retry should have upserted vectors")
	}
}

func TestServiceIngestEmbedFailureLeavesFailedNoVectors(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepo()
	store := &memoryStore{objects: map[string][]byte{}}
	vectors := &memoryVectors{}
	emb := &toggleEmbedder{dim: 16, fail: true}
	svc, err := NewService(repo, store, vectors, emb, 1024, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	created, err := svc.CreateText(ctx, CreateTextInput{
		Metadata: Metadata{WorkspaceID: "ws-a", Title: "A", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
		Content:  "refunds require reviewer approval",
	})
	if err != nil {
		t.Fatal(err)
	}
	persisted, _ := repo.Get(ctx, created.DocumentID, created.Version)
	if persisted.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", persisted.Status)
	}
	if persisted.ErrorMessage == "" {
		t.Fatal("embedding failure must record an error on the failed doc")
	}
	if len(vectors.points) != 0 {
		t.Fatalf("embedding failure left %d half-indexed vectors visible to search, want 0", len(vectors.points))
	}
}

// constantEmbedder is a test double (production resolves a provider embedder from Settings → Models).
type constantEmbedder struct {
	dim int
}

func (c constantEmbedder) Embed(_ context.Context, text string, _ EmbeddingPurpose) ([]float32, error) {
	v := make([]float32, c.dim)
	if len(text) > 0 {
		v[0] = 1
	}
	return v, nil
}

func TestServiceCreateTextAndSearchLatestOnly(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepo()
	store := &memoryStore{objects: map[string][]byte{}}
	vectors := &memoryVectors{}
	svc, err := NewService(repo, store, vectors, constantEmbedder{dim: 16}, 1024, "")
	if err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC) }
	ctx := context.Background()

	v1, err := svc.CreateText(ctx, CreateTextInput{
		Metadata: Metadata{WorkspaceID: "ws-1", Title: "Rules", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
		Content:  "Voucher first, loyalty second.",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Create returns the uploaded snapshot (202 + poll contract); sync ingest
	// still walked the states inline, so the persisted doc is indexed.
	if v1.Version != "v1" || !v1.IsLatest || v1.Status != StatusUploaded {
		t.Fatalf("v1=%+v", v1)
	}
	if got, _ := repo.Get(ctx, v1.DocumentID, v1.Version); got.Status != StatusIndexed {
		t.Fatalf("persisted v1 status = %q, want indexed", got.Status)
	}

	v2, err := svc.CreateText(ctx, CreateTextInput{
		Metadata: Metadata{WorkspaceID: "ws-1", DocumentID: v1.DocumentID, ParentVersion: "v1", Title: "Rules", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
		Content:  "Loyalty first, voucher second.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if v2.Version != "v1.1" || !v2.IsLatest {
		t.Fatalf("v2=%+v", v2)
	}

	results, err := svc.Search(ctx, SearchInput{WorkspaceID: "ws-1", Query: "loyalty", MaxChunks: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Version != "v1.1" {
		t.Fatalf("results=%+v", results)
	}
}

func TestServiceCurateLinksCreatesNewVersion(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepo()
	store := &memoryStore{objects: map[string][]byte{}}
	vectors := &memoryVectors{}
	svc, err := NewService(repo, store, vectors, constantEmbedder{dim: 16}, 1024, "")
	if err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC) }
	ctx := context.Background()

	v1, err := svc.CreateText(ctx, CreateTextInput{
		Metadata: Metadata{WorkspaceID: "ws-1", Title: "Rules", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
		Content:  "Voucher first, loyalty second.",
	})
	if err != nil {
		t.Fatal(err)
	}

	v2, err := svc.CurateLinks(ctx, CurateLinksInput{
		DocumentID:      v1.DocumentID,
		LinkedFeatureID: "feat-checkout",
		UploadedBy:      "curator",
	})
	if err != nil {
		t.Fatal(err)
	}
	if v2.Version != "v1.1" || v2.ParentVersion != "v1" || !v2.IsLatest {
		t.Fatalf("curated version = %+v", v2)
	}
	if v2.LinkedFeatureID != "feat-checkout" || v2.LinkedRequestID != "" || v2.UploadedBy != "curator" {
		t.Fatalf("curated links/actor = %+v", v2)
	}
	old, err := repo.Get(ctx, v1.DocumentID, "v1")
	if err != nil {
		t.Fatal(err)
	}
	if old.IsLatest || old.LinkedFeatureID != "" {
		t.Fatalf("old version mutated = %+v", old)
	}
	persisted, err := repo.Get(ctx, v1.DocumentID, "v1.1")
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != StatusIndexed {
		t.Fatalf("persisted curated status = %q, want indexed", persisted.Status)
	}
	results, err := svc.Search(ctx, SearchInput{WorkspaceID: "ws-1", Query: "loyalty", LinkedFeatureID: "feat-checkout", MaxChunks: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Version != "v1.1" {
		t.Fatalf("linked search results = %+v", results)
	}
}

func TestServiceDetailReturnsFullExtractedText(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepo()
	store := &memoryStore{objects: map[string][]byte{}}
	svc, err := NewService(repo, store, &memoryVectors{}, constantEmbedder{dim: 16}, 8192, "")
	if err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC) }
	ctx := context.Background()

	long := strings.Repeat("x", 8000)
	doc, err := svc.CreateText(ctx, CreateTextInput{
		Metadata: Metadata{WorkspaceID: "ws-1", Title: "Long", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
		Content:  long,
	})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := svc.Detail(ctx, doc.DocumentID, doc.Version)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.ExtractedPreview) != len(long) {
		t.Fatalf("extracted length got %d want %d", len(detail.ExtractedPreview), len(long))
	}
	if detail.ExtractedPreview != long {
		t.Fatal("extracted text mismatch")
	}
}

func TestServiceUploadRejectsNonMarkdownOrTextExtension(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepo()
	store := &memoryStore{objects: map[string][]byte{}}
	svc, err := NewService(repo, store, &memoryVectors{}, constantEmbedder{dim: 16}, 1024, "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.CreateUpload(context.Background(), CreateUploadInput{
		Metadata: Metadata{Title: "Mockup", DocumentType: DocumentTypeDesignReference, AuthorityLevel: AuthorityReference},
		Filename: "mockup.png",
		MimeType: "image/png",
		Body:     []byte{1, 2, 3},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("want ErrValidation, got %v", err)
	}
	if len(store.objects) != 0 {
		t.Fatalf("expected no objects stored, got %d", len(store.objects))
	}
}

func TestServiceCreateTextRequiresWorkspaceID(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepo()
	store := &memoryStore{objects: map[string][]byte{}}
	svc, err := NewService(repo, store, &memoryVectors{}, constantEmbedder{dim: 16}, 1024, "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.CreateText(context.Background(), CreateTextInput{
		Metadata: Metadata{Title: "Rules", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
		Content:  "Use the newest policy.",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

func TestServiceSearchRejectsWorkspaceMismatchedWithTrustedContext(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepo()
	store := &memoryStore{objects: map[string][]byte{}}
	vectors := &memoryVectors{}
	svc, err := NewService(repo, store, vectors, constantEmbedder{dim: 16}, 1024, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := svc.CreateText(ctx, CreateTextInput{
		Metadata: Metadata{WorkspaceID: "ws-b", Title: "B", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
		Content:  "workspace B only",
	}); err != nil {
		t.Fatal(err)
	}

	_, err = svc.Search(WithWorkspace(ctx, "ws-a"), SearchInput{WorkspaceID: "ws-b", Query: "workspace"})
	if err == nil {
		t.Fatal("cross-workspace search succeeded")
	}
}

func TestServiceSearchFiltersWorkspace(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepo()
	store := &memoryStore{objects: map[string][]byte{}}
	vectors := &memoryVectors{}
	svc, err := NewService(repo, store, vectors, constantEmbedder{dim: 16}, 1024, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	_, err = svc.CreateText(ctx, CreateTextInput{
		Metadata: Metadata{WorkspaceID: "ws-a", Title: "A", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
		Content:  "refunds require approval",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateText(ctx, CreateTextInput{
		Metadata: Metadata{WorkspaceID: "ws-b", Title: "B", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
		Content:  "refunds are automatic",
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := svc.Search(ctx, SearchInput{WorkspaceID: "ws-a", Query: "refunds", MaxChunks: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].WorkspaceID != "ws-a" || results[0].Title != "A" {
		t.Fatalf("results=%+v", results)
	}
}

func TestServiceIngestStoresSectionMetadata(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepo()
	store := &memoryStore{objects: map[string][]byte{}}
	vectors := &memoryVectors{}
	svc, err := NewService(repo, store, vectors, constantEmbedder{dim: 16}, 1024, "")
	if err != nil {
		t.Fatal(err)
	}

	created, err := svc.CreateText(context.Background(), CreateTextInput{
		Metadata: Metadata{WorkspaceID: "ws-a", Title: "Policies", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
		Content:  "# Refunds\n\nRefunds require reviewer approval.\n\n# Loyalty\n\nPlatinum tier changes need manual review.",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Create returns the uploaded snapshot; sync ingest ran inline, so re-fetch
	// the persisted version to inspect the stored chunks.
	doc, err := repo.Get(context.Background(), created.DocumentID, created.Version)
	if err != nil {
		t.Fatal(err)
	}

	// Relational chunk columns.
	if len(doc.Chunks) != 2 {
		t.Fatalf("chunks=%d want 2: %+v", len(doc.Chunks), doc.Chunks)
	}
	if doc.Chunks[0].Heading != "Refunds" || doc.Chunks[0].SectionIndex != 0 || doc.Chunks[0].HeadingPathJSON != `["Refunds"]` {
		t.Fatalf("chunk0 metadata: %+v", doc.Chunks[0])
	}
	if doc.Chunks[1].Heading != "Loyalty" || doc.Chunks[1].SectionIndex != 1 {
		t.Fatalf("chunk1 metadata: %+v", doc.Chunks[1])
	}

	// Vector payload metadata.
	if len(vectors.points) != 2 {
		t.Fatalf("points=%d want 2", len(vectors.points))
	}
	if vectors.points[0].Payload["heading"] != "Refunds" || vectors.points[0].Payload["section_index"] != 0 {
		t.Fatalf("point0 payload missing section metadata: %+v", vectors.points[0].Payload)
	}
}

func TestServiceSearchContextModes(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepo()
	repo.docs["doc-x@v1"] = &Document{
		DocumentID: "doc-x", Version: "v1", WorkspaceID: "ws-a", IsLatest: true, Status: StatusIndexed,
	}
	repo.chunks["doc-x@v1"] = []Chunk{
		{ID: "c0", DocumentID: "doc-x", Version: "v1", ChunkIndex: 0, SectionIndex: 0, ChunkText: "Refunds require reviewer approval."},
		{ID: "c1", DocumentID: "doc-x", Version: "v1", ChunkIndex: 1, SectionIndex: 0, ChunkText: "Approvers must record a note."},
		{ID: "c2", DocumentID: "doc-x", Version: "v1", ChunkIndex: 2, SectionIndex: 1, ChunkText: "Platinum changes need review."},
	}
	vectors := &memoryVectors{points: []VectorPoint{
		{ID: "c0", Payload: map[string]any{
			"workspace_id": "ws-a", "document_id": "doc-x", "version": "v1",
			"chunk_index": 0, "section_index": 0, "chunk_text": "Refunds require reviewer approval.",
		}},
	}}
	svc, err := NewService(repo, &memoryStore{objects: map[string][]byte{}}, vectors, constantEmbedder{dim: 16}, 1024, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Section mode: the hit expands to its section siblings, never another section.
	section, err := svc.Search(ctx, SearchInput{WorkspaceID: "ws-a", Query: "refunds", MaxChunks: 5, ContextMode: ContextModeSection})
	if err != nil {
		t.Fatal(err)
	}
	if len(section) != 1 {
		t.Fatalf("results=%d want 1", len(section))
	}
	r := section[0]
	if r.ContextKind != "section" || r.StartChunkIndex != 0 || r.EndChunkIndex != 1 {
		t.Fatalf("section result meta: %+v", r)
	}
	if !strings.Contains(r.ContextText, "reviewer approval") || !strings.Contains(r.ContextText, "record a note") {
		t.Fatalf("section context missing siblings: %q", r.ContextText)
	}
	if strings.Contains(r.ContextText, "Platinum changes") {
		t.Fatalf("section context leaked another section: %q", r.ContextText)
	}

	// Document mode: bounded whole-document context (all sections), labeled document.
	doc, err := svc.Search(ctx, SearchInput{WorkspaceID: "ws-a", Query: "refunds", MaxChunks: 5, ContextMode: ContextModeDocument})
	if err != nil {
		t.Fatal(err)
	}
	if doc[0].ContextKind != "document" || !strings.Contains(doc[0].ContextText, "Platinum changes") {
		t.Fatalf("document result: kind=%q text=%q", doc[0].ContextKind, doc[0].ContextText)
	}

	// Default (chunk) mode: no context expansion.
	chunk, err := svc.Search(ctx, SearchInput{WorkspaceID: "ws-a", Query: "refunds", MaxChunks: 5})
	if err != nil {
		t.Fatal(err)
	}
	if chunk[0].ContextText != "" || chunk[0].ContextKind != "" {
		t.Fatalf("chunk mode should not expand: %+v", chunk[0])
	}
}

type memoryRepo struct {
	docs   map[string]*Document
	chunks map[string][]Chunk
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{docs: map[string]*Document{}, chunks: map[string][]Chunk{}}
}

func (r *memoryRepo) CreateVersion(_ context.Context, doc *Document, _ []Link) error {
	for _, d := range r.docs {
		if d.DocumentID == doc.DocumentID {
			d.IsLatest = false
		}
	}
	cp := *doc
	r.docs[doc.DocumentID+"@"+doc.Version] = &cp
	return nil
}

func (r *memoryRepo) Get(_ context.Context, id, version string) (*Document, error) {
	d, ok := r.docs[id+"@"+version]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *d
	cp.Chunks = r.chunks[id+"@"+version]
	return &cp, nil
}

func (r *memoryRepo) List(context.Context, ListFilter) ([]Document, error) { return nil, nil }
func (r *memoryRepo) Count(context.Context, ListFilter) (int, error)       { return 0, nil }
func (r *memoryRepo) ListByFeatureOrRequest(context.Context, string, []string, string) ([]Document, error) {
	return nil, nil
}
func (r *memoryRepo) History(context.Context, string) ([]Document, error) { return nil, nil }

func (r *memoryRepo) UpdateStatus(_ context.Context, id, version string, status Status, summary, errMsg string) error {
	d, ok := r.docs[id+"@"+version]
	if !ok {
		return ErrNotFound
	}
	d.Status = status
	if summary != "" {
		d.Summary = summary
	}
	d.ErrorMessage = errMsg
	return nil
}

func (r *memoryRepo) ReplaceChunks(_ context.Context, id, version string, chunks []Chunk) error {
	r.chunks[id+"@"+version] = chunks
	return nil
}

func (r *memoryRepo) ChunkCount(_ context.Context, id, version string) (int, error) {
	return len(r.chunks[id+"@"+version]), nil
}

func (r *memoryRepo) ChunksForDocument(_ context.Context, id, version string) ([]Chunk, error) {
	src := r.chunks[id+"@"+version]
	out := make([]Chunk, len(src))
	copy(out, src)
	sort.Slice(out, func(i, j int) bool { return out[i].ChunkIndex < out[j].ChunkIndex })
	return out, nil
}

func (r *memoryRepo) DeleteVersion(context.Context, string, string) error { return nil }

func (r *memoryRepo) LatestVersion(_ context.Context, id string) (string, error) {
	for _, d := range r.docs {
		if d.DocumentID == id && d.IsLatest {
			return d.Version, nil
		}
	}
	return "", ErrNotFound
}

func (r *memoryRepo) NextMinorVersion(_ context.Context, id string) (string, error) {
	latest, err := r.LatestVersion(context.Background(), id)
	if err != nil {
		return "", err
	}
	if latest == "v1" {
		return "v1.1", nil
	}
	return "v1.2", nil
}

type memoryStore struct{ objects map[string][]byte }

func (s *memoryStore) PutObject(_ context.Context, key string, body []byte) error {
	s.objects[key] = append([]byte(nil), body...)
	return nil
}
func (s *memoryStore) GetObject(_ context.Context, key string, _ int64) ([]byte, error) {
	return s.objects[key], nil
}
func (s *memoryStore) DeleteObject(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}

type memoryVectors struct{ points []VectorPoint }

func (v *memoryVectors) EnsureCollection(context.Context) error { return nil }
func (v *memoryVectors) Upsert(_ context.Context, points []VectorPoint) error {
	v.points = append(v.points, points...)
	return nil
}
func (v *memoryVectors) DeleteVersion(_ context.Context, id, version string) error {
	filtered := v.points[:0]
	for _, p := range v.points {
		if p.Payload["document_id"] == id && p.Payload["version"] == version {
			continue
		}
		filtered = append(filtered, p)
	}
	v.points = filtered
	return nil
}
func (v *memoryVectors) Search(_ context.Context, in VectorSearch) ([]VectorResult, error) {
	out := make([]VectorResult, 0, len(v.points))
	for _, p := range v.points {
		if in.WorkspaceID != "" && p.Payload["workspace_id"] != in.WorkspaceID {
			continue
		}
		out = append(out, VectorResult{ID: p.ID, Score: 1, Payload: p.Payload})
	}
	return out, nil
}
