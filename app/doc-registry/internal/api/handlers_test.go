package api

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/knowledge"
)

type testEmbedder struct {
	dim int
}

func (e testEmbedder) Embed(_ context.Context, _ string, _ knowledge.EmbeddingPurpose) ([]float32, error) {
	v := make([]float32, e.dim)
	return v, nil
}

func TestHandlers_Health(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	out, err := h.Health(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Body.Status != "ok" {
		t.Fatalf("status = %q", out.Body.Status)
	}
}

func TestHandlers_Ready(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	out, err := h.Ready(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Body.Status != "ready" {
		t.Fatalf("status = %q", out.Body.Status)
	}
}

func TestHandlers_NotImplemented(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	ctx := context.Background()

	tests := []struct {
		name string
		run  func() error
	}{
		{"PublishArtifact", func() error {
			_, err := h.PublishArtifact(ctx, &PublishArtifactInput{})
			return err
		}},
		{"ListArtifacts", func() error {
			_, err := h.ListArtifacts(ctx, &ListArtifactsInput{})
			return err
		}},
		{"GetArtifact", func() error {
			_, err := h.GetArtifact(ctx, &GetArtifactInput{ID: "x"})
			return err
		}},
		{"UpdateStatus", func() error {
			_, err := h.UpdateStatus(ctx, &UpdateStatusInput{ID: "x"})
			return err
		}},
		{"GetArtifactFile", func() error {
			_, err := h.GetArtifactFile(ctx, &GetArtifactFileInput{ID: "x", Key: "prd"})
			return err
		}},
		{"DeleteArtifact", func() error {
			_, err := h.DeleteArtifact(ctx, &DeleteArtifactInput{ID: "x"})
			return err
		}},
		{"CheckConflicts", func() error {
			_, err := h.CheckConflicts(ctx, &CheckConflictsInput{})
			return err
		}},
		{"ListEvents", func() error {
			_, err := h.ListEvents(ctx, &ListEventsInput{After: time.Now().Add(-time.Hour)})
			return err
		}},
		{"CreateArtifactEditSession", func() error {
			_, err := h.CreateArtifactEditSession(ctx, &CreateArtifactEditSessionInput{})
			return err
		}},
		{"GetArtifactEditSession", func() error {
			_, err := h.GetArtifactEditSession(ctx, &GetArtifactEditSessionInput{ID: "sess-1"})
			return err
		}},
		{"DeleteArtifactEditSession", func() error {
			_, err := h.DeleteArtifactEditSession(ctx, &DeleteArtifactEditSessionInput{ID: "sess-1"})
			return err
		}},
		{"ListArtifactEditSessionFiles", func() error {
			_, err := h.ListArtifactEditSessionFiles(ctx, &ListArtifactEditSessionFilesInput{ID: "sess-1"})
			return err
		}},
		{"GetArtifactEditSessionFile", func() error {
			_, err := h.GetArtifactEditSessionFile(ctx, &GetArtifactEditSessionFileInput{ID: "sess-1", Key: "prd"})
			return err
		}},
		{"PatchArtifactEditSession", func() error {
			_, err := h.PatchArtifactEditSession(ctx, &PatchArtifactEditSessionInput{ID: "sess-1"})
			return err
		}},
		{"ReplaceArtifactEditSessionFile", func() error {
			_, err := h.ReplaceArtifactEditSessionFile(ctx, &ReplaceArtifactEditSessionFileInput{ID: "sess-1", Key: "prd"})
			return err
		}},
		{"GetArtifactEditSessionDiff", func() error {
			_, err := h.GetArtifactEditSessionDiff(ctx, &GetArtifactEditSessionDiffInput{ID: "sess-1"})
			return err
		}},
		{"SaveArtifactEditSession", func() error {
			_, err := h.SaveArtifactEditSession(ctx, &SaveArtifactEditSessionInput{ID: "sess-1"})
			return err
		}},
		{"GetArtifactSavedRevision", func() error {
			_, err := h.GetArtifactSavedRevision(ctx, &GetArtifactSavedRevisionInput{RevisionID: "rev-1"})
			return err
		}},
		{"GetArtifactSavedRevisionDiff", func() error {
			_, err := h.GetArtifactSavedRevisionDiff(ctx, &GetArtifactSavedRevisionDiffInput{RevisionID: "rev-1"})
			return err
		}},
		{"ListArtifactRevisions", func() error {
			_, err := h.ListArtifactRevisions(ctx, &ListArtifactRevisionsInput{ID: "art-1"})
			return err
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.run()
			if err == nil {
				t.Fatal("expected error")
			}
			var em *huma.ErrorModel
			if !errors.As(err, &em) || em.Status != http.StatusNotImplemented {
				t.Fatalf("want 501 huma ErrorModel, got %v", err)
			}
		})
	}
}

func TestMapArtifactError_Conflict(t *testing.T) {
	t.Parallel()

	err := mapArtifactError("publish artifact", artifact.ErrConflict)
	var em *huma.ErrorModel
	if !errors.As(err, &em) || em.Status != http.StatusConflict {
		t.Fatalf("want 409 huma ErrorModel, got %v", err)
	}
}

func TestRouter_CreateUploadDocument_Multipart(t *testing.T) {
	t.Parallel()
	knowledgeSvc, err := knowledge.NewService(
		&memoryKnowledgeRepo{},
		&memoryKnowledgeStore{objects: map[string][]byte{}},
		&memoryKnowledgeVectors{},
		testEmbedder{dim: 8},
		1024,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	rt := &Router{
		Handlers: &Handlers{
			Knowledge: knowledgeSvc,
		},
		Config: testConfig(),
	}
	srv := rt.Build()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("title", "Upload test")
	_ = w.WriteField("document_type", "supporting_doc")
	_ = w.WriteField("authority_level", "reference")
	_ = w.WriteField("linked_feature_id", "checkout-loyalty")
	fw, err := w.CreateFormFile("file", "note.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte("hello knowledge")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/documents/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnsupportedMediaType {
		t.Fatalf("unexpected 415 body=%s", rec.Body.String())
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

type memoryKnowledgeRepo struct {
	docs map[string]*knowledge.Document
}

func (r *memoryKnowledgeRepo) CreateVersion(_ context.Context, doc *knowledge.Document, _ []knowledge.Link) error {
	if r.docs == nil {
		r.docs = map[string]*knowledge.Document{}
	}
	for _, d := range r.docs {
		if d.DocumentID == doc.DocumentID {
			d.IsLatest = false
		}
	}
	cp := *doc
	r.docs[doc.DocumentID+"@"+doc.Version] = &cp
	return nil
}
func (r *memoryKnowledgeRepo) Get(_ context.Context, id, version string) (*knowledge.Document, error) {
	doc, ok := r.docs[id+"@"+version]
	if !ok {
		return nil, knowledge.ErrNotFound
	}
	cp := *doc
	return &cp, nil
}
func (r *memoryKnowledgeRepo) List(context.Context, knowledge.ListFilter) ([]knowledge.Document, error) {
	return nil, nil
}
func (r *memoryKnowledgeRepo) ListByFeatureOrRequest(context.Context, string, string) ([]knowledge.Document, error) {
	return nil, nil
}
func (r *memoryKnowledgeRepo) History(context.Context, string) ([]knowledge.Document, error) {
	return nil, nil
}
func (r *memoryKnowledgeRepo) UpdateStatus(_ context.Context, id, version string, status knowledge.Status, summary, errMsg string) error {
	doc, ok := r.docs[id+"@"+version]
	if !ok {
		return knowledge.ErrNotFound
	}
	doc.Status = status
	doc.Summary = summary
	doc.ErrorMessage = errMsg
	return nil
}
func (r *memoryKnowledgeRepo) ReplaceChunks(_ context.Context, id, version string, chunks []knowledge.Chunk) error {
	doc, ok := r.docs[id+"@"+version]
	if !ok {
		return knowledge.ErrNotFound
	}
	doc.Chunks = chunks
	return nil
}
func (r *memoryKnowledgeRepo) ChunkCount(_ context.Context, id, version string) (int, error) {
	doc, ok := r.docs[id+"@"+version]
	if !ok {
		return 0, knowledge.ErrNotFound
	}
	return len(doc.Chunks), nil
}
func (r *memoryKnowledgeRepo) DeleteVersion(context.Context, string, string) error { return nil }
func (r *memoryKnowledgeRepo) LatestVersion(_ context.Context, id string) (string, error) {
	for _, doc := range r.docs {
		if doc.DocumentID == id && doc.IsLatest {
			return doc.Version, nil
		}
	}
	return "", knowledge.ErrNotFound
}
func (r *memoryKnowledgeRepo) NextMinorVersion(_ context.Context, id string) (string, error) {
	latest, err := r.LatestVersion(context.Background(), id)
	if err != nil {
		return "", err
	}
	if latest == "v1" {
		return "v1.1", nil
	}
	return "v1.2", nil
}

type memoryKnowledgeStore struct {
	objects map[string][]byte
}

func (s *memoryKnowledgeStore) PutObject(_ context.Context, key string, body []byte) error {
	s.objects[key] = append([]byte(nil), body...)
	return nil
}
func (s *memoryKnowledgeStore) GetObject(_ context.Context, key string) ([]byte, error) {
	return s.objects[key], nil
}
func (s *memoryKnowledgeStore) DeleteObject(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}

type memoryKnowledgeVectors struct{}

func (v *memoryKnowledgeVectors) EnsureCollection(context.Context) error { return nil }
func (v *memoryKnowledgeVectors) Upsert(context.Context, []knowledge.VectorPoint) error {
	return nil
}
func (v *memoryKnowledgeVectors) DeleteVersion(context.Context, string, string) error { return nil }
func (v *memoryKnowledgeVectors) Search(context.Context, knowledge.VectorSearch) ([]knowledge.VectorResult, error) {
	return nil, nil
}
