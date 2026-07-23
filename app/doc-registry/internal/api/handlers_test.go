package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/identity"
	"github.com/specgate/doc-registry/internal/knowledge"
	"github.com/specgate/doc-registry/internal/storage/db"
	"github.com/specgate/doc-registry/internal/workboard"
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

func TestHandlers_ListWorkspaceMembersMarksCurrentUser(t *testing.T) {
	gdb := newTestGormDB(t)
	identityRepo := db.NewIdentityRepository(gdb)
	ctx := context.Background()
	first, err := identityRepo.Bootstrap(ctx, identity.BootstrapInput{
		WorkspaceName: "SpecGate Platform",
		DisplayName:   "Ada Lovelace",
		Username:      "ada",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := identityRepo.Bootstrap(ctx, identity.BootstrapInput{
		WorkspaceName: "SpecGate Platform",
		DisplayName:   "Grace Hopper",
		Username:      "grace",
	})
	if err != nil {
		t.Fatal(err)
	}

	srv := (&Router{Handlers: &Handlers{Identity: identityRepo}, Config: testConfig()}).Build()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/specgate-platform/members?current_user_id="+second.User.ID, nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var out struct {
		Workspace struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
		} `json:"workspace"`
		CurrentUser struct {
			ID string `json:"id"`
		} `json:"current_user"`
		Members []struct {
			UserID      string `json:"user_id"`
			Username    string `json:"username"`
			DisplayName string `json:"display_name"`
			Current     bool   `json:"current"`
		} `json:"members"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode members: %v body=%s", err, rec.Body.String())
	}
	if out.Workspace.ID != first.Workspace.ID || out.Workspace.Slug != "specgate-platform" || out.CurrentUser.ID != second.User.ID {
		t.Fatalf("workspace fields = %#v", out)
	}
	if len(out.Members) != 2 {
		t.Fatalf("members = %d, want 2", len(out.Members))
	}
	if !out.Members[1].Current || out.Members[1].Username != "grace" {
		t.Fatalf("current marker = %#v", out.Members)
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
			_, err := h.GetArtifactFile(ctx, &GetArtifactFileInput{ID: "x", Path: "prd.md"})
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

func TestMapGovernanceError_UnsupportedPolicySnapshot(t *testing.T) {
	t.Parallel()

	err := mapGovernanceError("context-pack", governanceprofile.ErrUnsupportedSnapshot)
	var em *huma.ErrorModel
	if !errors.As(err, &em) || em.Status != http.StatusConflict {
		t.Fatalf("want 409 huma ErrorModel, got %v", err)
	}
}

func TestMapGovernanceError_WorkboardErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		err    error
		status int
	}{
		{err: workboard.ErrValidation, status: http.StatusBadRequest},
		{err: workboard.ErrConflict, status: http.StatusConflict},
	} {
		err := mapGovernanceError("delivery-decision", tc.err)
		var em *huma.ErrorModel
		if !errors.As(err, &em) || em.Status != tc.status {
			t.Fatalf("want %d huma ErrorModel, got %v", tc.status, err)
		}
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
	_ = w.WriteField("workspace_id", "ws-1")
	_ = w.WriteField("title", "Upload test")
	_ = w.WriteField("document_type", "supporting_doc")
	_ = w.WriteField("authority_level", "reference")
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
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRouter_GovernanceContextSearch_EmptyResultsAreArray(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodPost, "/governance/context/search", bytes.NewBufferString(`{"workspace_id":"ws-1","query":"missing","max_chunks":3}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"results":[]`)) {
		t.Fatalf("expected empty results array, got %s", rec.Body.String())
	}
}

func TestRouter_GovernanceContextSearch_ResultsCarryCitationSchema(t *testing.T) {
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
	rt := &Router{Handlers: &Handlers{Knowledge: knowledgeSvc}, Config: testConfig()}
	srv := rt.Build()

	create := httptest.NewRequest(http.MethodPost, "/documents/text", bytes.NewBufferString(
		`{"workspace_id":"ws-a","title":"Refund Policy","document_type":"policy_doc","authority_level":"high","content":"refunds require reviewer approval"}`))
	create.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	srv.ServeHTTP(createRec, create)
	if createRec.Code != http.StatusAccepted {
		t.Fatalf("create: expected 202, got %d body=%s", createRec.Code, createRec.Body.String())
	}

	search := httptest.NewRequest(http.MethodPost, "/governance/context/search", bytes.NewBufferString(
		`{"workspace_id":"ws-a","query":"refunds","max_chunks":5}`))
	search.Header.Set("Content-Type", "application/json")
	searchRec := httptest.NewRecorder()
	srv.ServeHTTP(searchRec, search)
	if searchRec.Code != http.StatusOK {
		t.Fatalf("search: expected 200, got %d body=%s", searchRec.Code, searchRec.Body.String())
	}
	body := searchRec.Body.String()
	if !strings.Contains(body, `"kind":"knowledge"`) {
		t.Errorf("search result missing kind=knowledge citation: %s", body)
	}
	if !strings.Contains(body, `"url":"specgate://knowledge/`) {
		t.Errorf("search result missing specgate:// citation url: %s", body)
	}
}

func TestRouter_KnowledgeRejectsUnknownWorkspace(t *testing.T) {
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
			Identity:  &fakeIdentityStore{workspaces: map[string]*identity.Workspace{"ws-known": {ID: "ws-known", Slug: "known", Name: "Known"}}},
		},
		Config: testConfig(),
	}
	srv := rt.Build()

	create := httptest.NewRequest(http.MethodPost, "/documents/text", bytes.NewBufferString(
		`{"workspace_id":"ws-missing","title":"Rules","document_type":"policy_doc","authority_level":"high","content":"refunds require reviewer approval"}`))
	create.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	srv.ServeHTTP(createRec, create)
	if createRec.Code != http.StatusNotFound {
		t.Fatalf("create: expected 404, got %d body=%s", createRec.Code, createRec.Body.String())
	}

	search := httptest.NewRequest(http.MethodPost, "/governance/context/search", bytes.NewBufferString(
		`{"workspace_id":"ws-missing","query":"refunds","max_chunks":5}`))
	search.Header.Set("Content-Type", "application/json")
	searchRec := httptest.NewRecorder()
	srv.ServeHTTP(searchRec, search)
	if searchRec.Code != http.StatusNotFound {
		t.Fatalf("search: expected 404, got %d body=%s", searchRec.Code, searchRec.Body.String())
	}
}

func TestHandlers_ListKnowledgeDocumentsTotalIgnoresPageLimit(t *testing.T) {
	t.Parallel()
	repo := &memoryKnowledgeRepo{}
	for _, title := range []string{"Rules", "Runbook"} {
		if err := repo.CreateVersion(context.Background(), &knowledge.Document{
			DocumentID:     title,
			Version:        "v1",
			WorkspaceID:    "ws-a",
			IsLatest:       true,
			Title:          title,
			DocumentType:   knowledge.DocumentTypeSupportingDoc,
			AuthorityLevel: knowledge.AuthorityReference,
			Status:         knowledge.StatusIndexed,
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	service, err := knowledge.NewService(
		repo,
		&memoryKnowledgeStore{objects: map[string][]byte{}},
		&memoryKnowledgeVectors{},
		testEmbedder{dim: 8},
		1024,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	out, err := (&Handlers{Knowledge: service}).ListKnowledgeDocuments(context.Background(), &ListKnowledgeDocumentsInput{
		WorkspaceID: "ws-a",
		Limit:       1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Body.Items) != 1 {
		t.Fatalf("items = %d, want page size 1", len(out.Body.Items))
	}
	if out.Body.Total != 2 {
		t.Fatalf("total = %d, want all matching documents 2", out.Body.Total)
	}
}

func TestRouter_KnowledgeLinkCreatesNewVersion(t *testing.T) {
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
	gdb := newTestGormDB(t)
	workBoard := db.NewWorkBoardRepository(gdb)
	if _, err := workBoard.CreateFeature(context.Background(), workboard.Feature{ID: "feat-checkout", WorkspaceID: "ws-a", Key: "feat-checkout", Name: "Checkout"}); err != nil {
		t.Fatal(err)
	}
	rt := &Router{Handlers: &Handlers{Knowledge: knowledgeSvc, WorkBoard: workBoard}, Config: testConfig()}
	srv := rt.Build()

	create := httptest.NewRequest(http.MethodPost, "/documents/text", bytes.NewBufferString(
		`{"workspace_id":"ws-a","title":"Rules","document_type":"policy_doc","authority_level":"high","content":"refunds require reviewer approval"}`))
	create.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	srv.ServeHTTP(createRec, create)
	if createRec.Code != http.StatusAccepted {
		t.Fatalf("create: expected 202, got %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		DocumentID string `json:"document_id"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create body: %v; %s", err, createRec.Body.String())
	}

	link := httptest.NewRequest(http.MethodPost, "/documents/"+created.DocumentID+"/links", bytes.NewBufferString(
		`{"workspace_id":"ws-a","linked_feature_id":"feat-checkout","uploaded_by":"curator"}`))
	link.Header.Set("Content-Type", "application/json")
	linkRec := httptest.NewRecorder()
	srv.ServeHTTP(linkRec, link)
	if linkRec.Code != http.StatusAccepted {
		t.Fatalf("link: expected 202, got %d body=%s", linkRec.Code, linkRec.Body.String())
	}
	var linked struct {
		Version         string `json:"version"`
		ParentVersion   string `json:"parent_version"`
		LinkedFeatureID string `json:"linked_feature_id"`
	}
	if err := json.Unmarshal(linkRec.Body.Bytes(), &linked); err != nil {
		t.Fatalf("decode link body: %v; %s", err, linkRec.Body.String())
	}
	if linked.Version != "v1.1" || linked.ParentVersion != "v1" || linked.LinkedFeatureID != "feat-checkout" {
		t.Fatalf("linked = %+v", linked)
	}
}

type failingEmbedder struct{}

func (failingEmbedder) Embed(_ context.Context, _ string, _ knowledge.EmbeddingPurpose) ([]float32, error) {
	return nil, errors.New("embedding provider unavailable")
}

func TestRouter_CreateAndRetryReturn202(t *testing.T) {
	t.Parallel()
	knowledgeSvc, err := knowledge.NewService(
		&memoryKnowledgeRepo{},
		&memoryKnowledgeStore{objects: map[string][]byte{}},
		&memoryKnowledgeVectors{},
		failingEmbedder{},
		1024,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	rt := &Router{Handlers: &Handlers{Knowledge: knowledgeSvc}, Config: testConfig()}
	srv := rt.Build()

	create := httptest.NewRequest(http.MethodPost, "/documents/text", bytes.NewBufferString(
		`{"workspace_id":"ws-a","title":"Rules","document_type":"policy_doc","authority_level":"high","content":"refunds require reviewer approval"}`))
	create.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	srv.ServeHTTP(createRec, create)
	if createRec.Code != http.StatusAccepted {
		t.Fatalf("create: expected 202, got %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		DocumentID string `json:"document_id"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create body: %v; %s", err, createRec.Body.String())
	}
	if created.Status != "uploaded" {
		t.Fatalf("create status = %q, want uploaded (202 + poll contract)", created.Status)
	}

	// Sync inline ingest failed on embed → doc is failed → retry endpoint accepts it.
	retry := httptest.NewRequest(http.MethodPost, "/documents/"+created.DocumentID+"/retry?workspace_id=ws-a", nil)
	retryRec := httptest.NewRecorder()
	srv.ServeHTTP(retryRec, retry)
	if retryRec.Code != http.StatusAccepted {
		t.Fatalf("retry: expected 202, got %d body=%s", retryRec.Code, retryRec.Body.String())
	}
}

type memoryKnowledgeRepo struct {
	docs map[string]*knowledge.Document
}

type fakeIdentityStore struct {
	workspaces map[string]*identity.Workspace
}

func (f *fakeIdentityStore) Bootstrap(context.Context, identity.BootstrapInput) (*identity.Selection, error) {
	return nil, nil
}

func (f *fakeIdentityStore) ListUsers(context.Context) ([]identity.User, error) {
	return nil, nil
}

func (f *fakeIdentityStore) ListWorkspaces(context.Context) ([]identity.Workspace, error) {
	out := make([]identity.Workspace, 0, len(f.workspaces))
	for _, workspace := range f.workspaces {
		out = append(out, *workspace)
	}
	return out, nil
}

func (f *fakeIdentityStore) GetUser(context.Context, string) (*identity.User, error) {
	return nil, nil
}

func (f *fakeIdentityStore) GetWorkspace(_ context.Context, idOrSlug string) (*identity.Workspace, error) {
	if f.workspaces == nil {
		return nil, nil
	}
	if workspace, ok := f.workspaces[idOrSlug]; ok {
		return workspace, nil
	}
	for _, workspace := range f.workspaces {
		if workspace.Slug == idOrSlug {
			return workspace, nil
		}
	}
	return nil, nil
}

func (f *fakeIdentityStore) ListWorkspaceMembers(context.Context, string) ([]identity.WorkspaceMemberDetail, error) {
	return nil, nil
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
func (r *memoryKnowledgeRepo) List(_ context.Context, f knowledge.ListFilter) ([]knowledge.Document, error) {
	var out []knowledge.Document
	for _, doc := range r.docs {
		if f.WorkspaceID != "" && doc.WorkspaceID != f.WorkspaceID {
			continue
		}
		if !f.IncludeHistory && !doc.IsLatest {
			continue
		}
		if f.LinkedFeatureID != "" && doc.LinkedFeatureID != f.LinkedFeatureID {
			continue
		}
		if f.LinkedRequestID != "" && doc.LinkedRequestID != f.LinkedRequestID {
			continue
		}
		if f.DocumentType != "" && doc.DocumentType != f.DocumentType {
			continue
		}
		if f.Status != "" && doc.Status != f.Status {
			continue
		}
		out = append(out, *doc)
	}
	if f.Offset >= len(out) {
		return []knowledge.Document{}, nil
	}
	out = out[f.Offset:]
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}
func (r *memoryKnowledgeRepo) Count(ctx context.Context, f knowledge.ListFilter) (int, error) {
	f.Limit = 0
	f.Offset = 0
	docs, err := r.List(ctx, f)
	return len(docs), err
}
func (r *memoryKnowledgeRepo) ListByFeatureOrRequest(context.Context, string, []string, string) ([]knowledge.Document, error) {
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
func (r *memoryKnowledgeRepo) ChunksForDocument(_ context.Context, id, version string) ([]knowledge.Chunk, error) {
	doc, ok := r.docs[id+"@"+version]
	if !ok {
		return nil, knowledge.ErrNotFound
	}
	return doc.Chunks, nil
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
func (s *memoryKnowledgeStore) GetObject(_ context.Context, key string, _ int64) ([]byte, error) {
	return s.objects[key], nil
}
func (s *memoryKnowledgeStore) DeleteObject(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}

type memoryKnowledgeVectors struct{ points []knowledge.VectorPoint }

func (v *memoryKnowledgeVectors) EnsureCollection(context.Context) error { return nil }
func (v *memoryKnowledgeVectors) Upsert(_ context.Context, points []knowledge.VectorPoint) error {
	v.points = append(v.points, points...)
	return nil
}
func (v *memoryKnowledgeVectors) DeleteVersion(_ context.Context, id, version string) error {
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
func (v *memoryKnowledgeVectors) Search(_ context.Context, in knowledge.VectorSearch) ([]knowledge.VectorResult, error) {
	out := make([]knowledge.VectorResult, 0, len(v.points))
	for _, p := range v.points {
		if in.WorkspaceID != "" && p.Payload["workspace_id"] != in.WorkspaceID {
			continue
		}
		out = append(out, knowledge.VectorResult{ID: p.ID, Score: 1, Payload: p.Payload})
	}
	return out, nil
}
