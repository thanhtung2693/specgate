package knowledge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// constantEmbedder is a test double (production resolves a provider embedder from Settings → Model).
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
		Metadata: Metadata{Title: "Rules", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
		Content:  "Voucher first, loyalty second.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if v1.Version != "v1" || !v1.IsLatest || v1.Status != StatusIndexed {
		t.Fatalf("v1=%+v", v1)
	}

	v2, err := svc.CreateText(ctx, CreateTextInput{
		Metadata: Metadata{DocumentID: v1.DocumentID, ParentVersion: "v1", Title: "Rules", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
		Content:  "Loyalty first, voucher second.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if v2.Version != "v1.1" || !v2.IsLatest {
		t.Fatalf("v2=%+v", v2)
	}

	results, err := svc.Search(ctx, SearchInput{Query: "loyalty", MaxChunks: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Version != "v1.1" {
		t.Fatalf("results=%+v", results)
	}
}

func TestServiceDetailReturnsFullExtractedText(t *testing.T) {
	t.Parallel()
	repo := newMemoryRepo()
	store := &memoryStore{objects: map[string][]byte{}}
	svc, err := NewService(repo, store, &memoryVectors{}, constantEmbedder{dim: 16}, 1024, "")
	if err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC) }
	ctx := context.Background()

	long := strings.Repeat("x", 8000)
	doc, err := svc.CreateText(ctx, CreateTextInput{
		Metadata: Metadata{Title: "Long", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
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
func (r *memoryRepo) ListByFeatureOrRequest(context.Context, string, string) ([]Document, error) {
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
	if errMsg != "" {
		d.ErrorMessage = errMsg
	}
	return nil
}

func (r *memoryRepo) ReplaceChunks(_ context.Context, id, version string, chunks []Chunk) error {
	r.chunks[id+"@"+version] = chunks
	return nil
}

func (r *memoryRepo) ChunkCount(_ context.Context, id, version string) (int, error) {
	return len(r.chunks[id+"@"+version]), nil
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
func (s *memoryStore) GetObject(_ context.Context, key string) ([]byte, error) {
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
func (v *memoryVectors) Search(_ context.Context, _ VectorSearch) ([]VectorResult, error) {
	out := make([]VectorResult, 0, len(v.points))
	for _, p := range v.points {
		out = append(out, VectorResult{ID: p.ID, Score: 1, Payload: p.Payload})
	}
	return out, nil
}
