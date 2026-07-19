package knowledge

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/specgate/doc-registry/internal/knowledgequeue"
)

func TestCreateTextRejectsUnsafeDocumentIDBeforeWriting(t *testing.T) {
	t.Parallel()
	for _, documentID := range []string{".", "..", "../doc", "../../ws-b/documents/doc-1", "doc/child", `doc\child`, "doc..child", "doc\nchild"} {
		documentID := documentID
		t.Run(documentID, func(t *testing.T) {
			t.Parallel()
			repo := newMemoryRepo()
			store := &memoryStore{objects: map[string][]byte{}}
			svc, err := NewService(repo, store, &memoryVectors{}, constantEmbedder{dim: 16}, 1024, "")
			if err != nil {
				t.Fatal(err)
			}

			_, err = svc.CreateText(context.Background(), CreateTextInput{
				Metadata: Metadata{
					WorkspaceID: "ws-a", DocumentID: documentID, Title: "Unsafe",
					DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh,
				},
				Content: "must not persist",
			})
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("CreateText(%q) error = %v, want ErrValidation", documentID, err)
			}
			if len(repo.docs) != 0 || len(store.objects) != 0 {
				t.Fatalf("unsafe create wrote documents=%d objects=%d", len(repo.docs), len(store.objects))
			}
		})
	}
}

func TestCreateConflictCleanupCannotDeleteWinningSourceObject(t *testing.T) {
	t.Parallel()
	baseRepo := newMemoryRepo()
	repo := &rejectDuplicateRepository{Repository: baseRepo}
	store := &memoryStore{objects: map[string][]byte{}}
	svc, err := NewService(repo, store, &memoryVectors{}, constantEmbedder{dim: 16}, 1024, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	meta := Metadata{
		WorkspaceID: "ws-a", DocumentID: "doc-1", NewVersion: "v1", Title: "Rules",
		DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh,
	}

	winner, err := svc.CreateText(ctx, CreateTextInput{Metadata: meta, Content: "winner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateText(ctx, CreateTextInput{Metadata: meta, Content: "loser"}); err == nil {
		t.Fatal("duplicate version create succeeded")
	}
	body, ok := store.objects[winner.SourceURI]
	if !ok {
		t.Fatalf("winner source object %q was deleted by losing create", winner.SourceURI)
	}
	if string(body) != "winner" {
		t.Fatalf("winner source object = %q, want winner", body)
	}
}

func TestDeleteChangesMetadataBeforeBestEffortCleanup(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		deleteErr error
		wantOps   []string
		wantErr   bool
	}{
		{name: "metadata failure preserves external data", deleteErr: errors.New("database unavailable"), wantOps: []string{"metadata"}, wantErr: true},
		{name: "metadata success precedes cleanup", wantOps: []string{"metadata", "vectors", "object", "object", "object"}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ops := []string{}
			baseRepo := newMemoryRepo()
			baseRepo.docs["doc-1@v1"] = &Document{
				DocumentID: "doc-1", Version: "v1", WorkspaceID: "ws-a",
				SourceURI: "workspaces/ws-a/documents/doc-1/v1/raw/source.txt",
			}
			repo := &deleteRecordingRepository{
				Repository: baseRepo,
				delete: func(context.Context, string, string) error {
					ops = append(ops, "metadata")
					return tc.deleteErr
				},
			}
			store := &deleteRecordingObjectStore{
				ObjectStore: &memoryStore{objects: map[string][]byte{}},
				delete: func(context.Context, string) error {
					ops = append(ops, "object")
					return errors.New("best effort object failure")
				},
			}
			vectors := &deleteRecordingVectorStore{
				VectorStore: &memoryVectors{},
				delete: func(context.Context, string, string) error {
					ops = append(ops, "vectors")
					return errors.New("best effort vector failure")
				},
			}
			svc, err := NewService(repo, store, vectors, constantEmbedder{dim: 16}, 1024, "")
			if err != nil {
				t.Fatal(err)
			}

			err = svc.Delete(context.Background(), "doc-1", "v1")
			if (err != nil) != tc.wantErr {
				t.Fatalf("Delete error = %v, wantErr=%v", err, tc.wantErr)
			}
			if strings.Join(ops, ",") != strings.Join(tc.wantOps, ",") {
				t.Fatalf("operations = %v, want %v", ops, tc.wantOps)
			}
		})
	}
}

func TestSearchSkipsOrphanVectorsAfterMetadataDeletion(t *testing.T) {
	t.Parallel()
	baseRepo := newMemoryRepo()
	repo := &deletingRepository{Repository: baseRepo, repo: baseRepo}
	store := &memoryStore{objects: map[string][]byte{}}
	baseVectors := &memoryVectors{}
	failCleanup := false
	vectors := &deleteRecordingVectorStore{
		VectorStore: baseVectors,
		delete: func(ctx context.Context, documentID, version string) error {
			if failCleanup {
				return errors.New("vector store unavailable")
			}
			return baseVectors.DeleteVersion(ctx, documentID, version)
		},
	}
	svc, err := NewService(repo, store, vectors, constantEmbedder{dim: 16}, 1024, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	doc, err := svc.CreateText(ctx, CreateTextInput{
		Metadata: Metadata{WorkspaceID: "ws-a", Title: "Rules", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
		Content:  "refunds require approval",
	})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := baseRepo.Get(ctx, doc.DocumentID, doc.Version)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != StatusIndexed || len(baseVectors.points) == 0 {
		t.Fatalf("fixture did not index document: status=%v points=%d", persisted.Status, len(baseVectors.points))
	}
	failCleanup = true
	if err := svc.Delete(ctx, doc.DocumentID, doc.Version); err != nil {
		t.Fatal(err)
	}

	results, err := svc.Search(ctx, SearchInput{WorkspaceID: "ws-a", Query: "refunds", IncludeHistory: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("search returned metadata-deleted document: %+v", results)
	}
}

func TestSearchSkipsVersionWhenFinalIndexedStatusUpdateFails(t *testing.T) {
	t.Parallel()
	baseRepo := newMemoryRepo()
	repo := &faultRepository{Repository: baseRepo, failStatus: StatusIndexed}
	vectors := &memoryVectors{}
	svc, err := NewService(
		repo,
		&memoryStore{objects: map[string][]byte{}},
		vectors,
		constantEmbedder{dim: 16},
		1024,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	created, err := svc.CreateText(ctx, CreateTextInput{
		Metadata: Metadata{WorkspaceID: "ws-a", Title: "Rules", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
		Content:  "refunds require approval",
	})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := baseRepo.Get(ctx, created.DocumentID, created.Version)
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors.points) == 0 {
		t.Fatal("fixture did not upsert vectors before the final status failure")
	}
	if persisted.Status != StatusFailed {
		t.Fatalf("status=%q, want failed", persisted.Status)
	}

	results, err := svc.Search(ctx, SearchInput{WorkspaceID: "ws-a", Query: "refunds"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("search returned failed metadata version: %+v", results)
	}
}

func TestIngestCleansProcessedDataWhenMetadataWasDeleted(t *testing.T) {
	t.Parallel()
	baseRepo := newMemoryRepo()
	repo := &deleteBeforeIndexedRepository{Repository: baseRepo, repo: baseRepo}
	store := &memoryStore{objects: map[string][]byte{}}
	vectors := &memoryVectors{}
	svc, err := NewService(repo, store, vectors, constantEmbedder{dim: 16}, 1024, "")
	if err != nil {
		t.Fatal(err)
	}

	created, err := svc.CreateText(context.Background(), CreateTextInput{
		Metadata: Metadata{
			WorkspaceID:    "ws-a",
			DocumentID:     "doc-delete-race",
			NewVersion:     "v1",
			Title:          "Delete race",
			DocumentType:   DocumentTypeSRS,
			AuthorityLevel: AuthorityHigh,
		},
		Content: "content written while deletion is in progress",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := baseRepo.Get(context.Background(), created.DocumentID, created.Version); !errors.Is(err, ErrNotFound) {
		t.Fatalf("metadata error = %v, want ErrNotFound", err)
	}
	for key := range store.objects {
		if strings.Contains(key, "/processed/") {
			t.Fatalf("processed object survived metadata deletion: %s", key)
		}
	}
	if len(vectors.points) != 0 {
		t.Fatalf("vectors survived metadata deletion: %d", len(vectors.points))
	}
}

func TestEnqueueFailureMarksPersistedVersionFailed(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepo()
	store := &memoryStore{objects: map[string][]byte{}}
	svc, err := NewService(repo, store, &memoryVectors{}, constantEmbedder{dim: 16}, 1024, "")
	if err != nil {
		t.Fatal(err)
	}
	svc.WithIngestEnqueuer(failingEnqueuer{})

	_, err = svc.CreateText(context.Background(), CreateTextInput{
		Metadata: Metadata{WorkspaceID: "ws-a", Title: "Rules", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
		Content:  "content",
	})
	if err == nil {
		t.Fatal("enqueue failure was not returned")
	}
	for _, doc := range repo.docs {
		if doc.Status != StatusFailed || !strings.Contains(doc.ErrorMessage, "queue unavailable") {
			t.Fatalf("persisted document status=%q error=%q, want failed enqueue error", doc.Status, doc.ErrorMessage)
		}
	}
}

func TestEveryIngestStageFailureMarksVersionFailed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		setup func(*faultRepository, *faultObjectStore, *faultVectorStore)
	}{
		{name: "enter parsing", setup: func(r *faultRepository, _ *faultObjectStore, _ *faultVectorStore) { r.failStatus = StatusParsing }},
		{name: "write extracted text", setup: func(_ *faultRepository, s *faultObjectStore, _ *faultVectorStore) {
			s.failPutContains = "extracted.txt"
		}},
		{name: "replace chunks", setup: func(r *faultRepository, _ *faultObjectStore, _ *faultVectorStore) { r.failReplaceChunks = true }},
		{name: "write chunk object", setup: func(_ *faultRepository, s *faultObjectStore, _ *faultVectorStore) { s.failPutContains = "chunks.json" }},
		{name: "mark chunked", setup: func(r *faultRepository, _ *faultObjectStore, _ *faultVectorStore) { r.failStatus = StatusChunked }},
		{name: "ensure vectors", setup: func(_ *faultRepository, _ *faultObjectStore, v *faultVectorStore) { v.failEnsure = true }},
		{name: "mark embedded", setup: func(r *faultRepository, _ *faultObjectStore, _ *faultVectorStore) { r.failStatus = StatusEmbedded }},
		{name: "clear old vectors", setup: func(_ *faultRepository, _ *faultObjectStore, v *faultVectorStore) { v.failDelete = true }},
		{name: "upsert vectors", setup: func(_ *faultRepository, _ *faultObjectStore, v *faultVectorStore) { v.failUpsert = true }},
		{name: "mark indexed", setup: func(r *faultRepository, _ *faultObjectStore, _ *faultVectorStore) { r.failStatus = StatusIndexed }},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			baseRepo := newMemoryRepo()
			repo := &faultRepository{Repository: baseRepo}
			store := &faultObjectStore{ObjectStore: &memoryStore{objects: map[string][]byte{}}}
			vectors := &faultVectorStore{VectorStore: &memoryVectors{}}
			tc.setup(repo, store, vectors)
			svc, err := NewService(repo, store, vectors, constantEmbedder{dim: 16}, 1024, "")
			if err != nil {
				t.Fatal(err)
			}

			created, err := svc.CreateText(context.Background(), CreateTextInput{
				Metadata: Metadata{WorkspaceID: "ws-a", Title: "Rules", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
				Content:  "content",
			})
			if err != nil {
				t.Fatalf("sync create should return uploaded snapshot: %v", err)
			}
			persisted, err := baseRepo.Get(context.Background(), created.DocumentID, created.Version)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Status != StatusFailed || persisted.ErrorMessage == "" {
				t.Fatalf("status=%q error=%q, want failed with error", persisted.Status, persisted.ErrorMessage)
			}
		})
	}
}

type rejectDuplicateRepository struct{ Repository }

func (r *rejectDuplicateRepository) CreateVersion(ctx context.Context, doc *Document, links []Link) error {
	if _, err := r.Repository.Get(ctx, doc.DocumentID, doc.Version); err == nil {
		return errors.New("duplicate version")
	}
	return r.Repository.CreateVersion(ctx, doc, links)
}

type deleteRecordingRepository struct {
	Repository
	delete func(context.Context, string, string) error
}

func (r *deleteRecordingRepository) DeleteVersion(ctx context.Context, documentID, version string) error {
	return r.delete(ctx, documentID, version)
}

type deletingRepository struct {
	Repository
	repo *memoryRepo
}

func (r *deletingRepository) DeleteVersion(_ context.Context, documentID, version string) error {
	key := documentID + "@" + version
	if _, ok := r.repo.docs[key]; !ok {
		return ErrNotFound
	}
	delete(r.repo.docs, key)
	delete(r.repo.chunks, key)
	return nil
}

type deleteRecordingObjectStore struct {
	ObjectStore
	delete func(context.Context, string) error
}

func (s *deleteRecordingObjectStore) DeleteObject(ctx context.Context, key string) error {
	return s.delete(ctx, key)
}

type deleteRecordingVectorStore struct {
	VectorStore
	delete func(context.Context, string, string) error
}

func (s *deleteRecordingVectorStore) DeleteVersion(ctx context.Context, documentID, version string) error {
	return s.delete(ctx, documentID, version)
}

type failingEnqueuer struct{}

func (failingEnqueuer) EnqueueKnowledgeIngest(context.Context, knowledgequeue.Task) error {
	return errors.New("queue unavailable")
}

type faultRepository struct {
	Repository
	failStatus        Status
	failReplaceChunks bool
}

type deleteBeforeIndexedRepository struct {
	Repository
	repo *memoryRepo
}

func (r *deleteBeforeIndexedRepository) UpdateStatus(
	ctx context.Context,
	documentID string,
	version string,
	status Status,
	summary string,
	errorMessage string,
) error {
	if status == StatusIndexed {
		key := documentID + "@" + version
		delete(r.repo.docs, key)
		delete(r.repo.chunks, key)
		return ErrNotFound
	}
	return r.Repository.UpdateStatus(ctx, documentID, version, status, summary, errorMessage)
}

func (r *faultRepository) UpdateStatus(ctx context.Context, documentID, version string, status Status, summary, errorMessage string) error {
	if status == r.failStatus {
		r.failStatus = ""
		return errors.New("status update failed")
	}
	return r.Repository.UpdateStatus(ctx, documentID, version, status, summary, errorMessage)
}

func (r *faultRepository) ReplaceChunks(ctx context.Context, documentID, version string, chunks []Chunk) error {
	if r.failReplaceChunks {
		return errors.New("replace chunks failed")
	}
	return r.Repository.ReplaceChunks(ctx, documentID, version, chunks)
}

type faultObjectStore struct {
	ObjectStore
	failPutContains string
}

func (s *faultObjectStore) PutObject(ctx context.Context, key string, body []byte) error {
	if s.failPutContains != "" && strings.Contains(key, s.failPutContains) {
		return errors.New("object write failed")
	}
	return s.ObjectStore.PutObject(ctx, key, body)
}

type faultVectorStore struct {
	VectorStore
	failEnsure bool
	failDelete bool
	failUpsert bool
}

func (s *faultVectorStore) EnsureCollection(ctx context.Context) error {
	if s.failEnsure {
		return errors.New("ensure vectors failed")
	}
	return s.VectorStore.EnsureCollection(ctx)
}

func (s *faultVectorStore) DeleteVersion(ctx context.Context, documentID, version string) error {
	if s.failDelete {
		return errors.New("delete vectors failed")
	}
	return s.VectorStore.DeleteVersion(ctx, documentID, version)
}

func (s *faultVectorStore) Upsert(ctx context.Context, points []VectorPoint) error {
	if s.failUpsert {
		return errors.New("upsert vectors failed")
	}
	return s.VectorStore.Upsert(ctx, points)
}
