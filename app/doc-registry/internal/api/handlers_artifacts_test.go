package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/governancefiles"
	"github.com/specgate/doc-registry/internal/governanceops"
	blob "github.com/specgate/doc-registry/internal/storage/blob"
)

func TestArtifactDTO_ExpectedGates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		snapshot  string
		wantGates []string // nil means the field should be nil
	}{
		{
			name:      "valid snapshot surfaces enabled gates",
			snapshot:  `{"enabled_gates":["spec_completeness","delivery_review"],"required_roles":["spec"]}`,
			wantGates: []string{"spec_completeness", "delivery_review"},
		},
		{name: "empty string yields nil gates", snapshot: ""},
		{name: "whitespace yields nil gates", snapshot: "   "},
		{name: "invalid json yields nil gates", snapshot: "not json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := &artifact.Artifact{ID: "a-1", PolicySnapshotJSON: tt.snapshot}
			dto := artifactDTO(a)
			if tt.wantGates == nil {
				if dto.ExpectedGates != nil {
					t.Errorf("ExpectedGates = %v, want nil", dto.ExpectedGates)
				}
			} else {
				if !equalStrings(dto.ExpectedGates, tt.wantGates) {
					t.Errorf("ExpectedGates = %v, want %v", dto.ExpectedGates, tt.wantGates)
				}
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type memoryBlobStore struct {
	objects map[string][]byte
	deleted []string
}

func (s *memoryBlobStore) Put(context.Context, io.Reader, int64, string) (string, error) {
	panic("unexpected Put")
}
func (s *memoryBlobStore) Open(_ context.Context, id string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(string(s.objects[id]))), nil
}
func (*memoryBlobStore) Stat(context.Context, string) (blob.Meta, error) { panic("unexpected Stat") }
func (s *memoryBlobStore) Delete(_ context.Context, id string) error {
	s.deleted = append(s.deleted, id)
	delete(s.objects, id)
	return nil
}

type workspaceCheckingGovernanceFiles struct {
	workspaceID, objectKey, deletedID string
	deleteErr                         error
}

func (s *workspaceCheckingGovernanceFiles) Create(context.Context, governancefiles.File) (*governancefiles.File, error) {
	panic("unexpected Create")
}
func (s *workspaceCheckingGovernanceFiles) Commit(context.Context, string, time.Time) (*governancefiles.File, error) {
	panic("unexpected Commit")
}
func (s *workspaceCheckingGovernanceFiles) Get(ctx context.Context, _ string) (*governancefiles.File, error) {
	s.workspaceID = governancefiles.WorkspaceID(ctx)
	key := s.objectKey
	if key == "" {
		key = "governance/ws-test/spec.md"
	}
	return &governancefiles.File{ID: "file-in-ws-test", WorkspaceID: s.workspaceID, ObjectKey: key, Status: governancefiles.StatusReady}, nil
}
func (s *workspaceCheckingGovernanceFiles) List(context.Context, governancefiles.ListFilter) ([]governancefiles.File, int64, error) {
	panic("unexpected List")
}
func (s *workspaceCheckingGovernanceFiles) Touch(context.Context, string, time.Time) (*governancefiles.File, error) {
	panic("unexpected Touch")
}
func (s *workspaceCheckingGovernanceFiles) Delete(ctx context.Context, id string) (string, error) {
	s.workspaceID, s.deletedID = governancefiles.WorkspaceID(ctx), id
	if s.deleteErr != nil {
		return "", s.deleteErr
	}
	return s.objectKey, nil
}
func (s *workspaceCheckingGovernanceFiles) DeleteStaleReady(context.Context, time.Time) ([]string, error) {
	panic("unexpected DeleteStaleReady")
}
func (s *workspaceCheckingGovernanceFiles) DeleteStalePending(context.Context, time.Time) ([]string, error) {
	panic("unexpected DeleteStalePending")
}

type fakeArtifactService struct{}

func (fakeArtifactService) Publish(context.Context, artifact.PublishInput) (*artifact.Artifact, error) {
	panic("unexpected Publish call")
}
func (fakeArtifactService) Get(context.Context, string) (*artifact.Artifact, error) {
	panic("unexpected Get call")
}
func (fakeArtifactService) List(context.Context, artifact.ListFilter) ([]artifact.Artifact, error) {
	panic("unexpected List call")
}
func (fakeArtifactService) Count(context.Context, artifact.ListFilter) (int64, error) {
	panic("unexpected Count call")
}
func (fakeArtifactService) LatestArtifact(context.Context, string) (*artifact.Artifact, error) {
	return nil, nil
}
func (fakeArtifactService) NextVersion(context.Context, string) (string, error) { return "v0.1", nil }
func (fakeArtifactService) ResolveNextVersion(context.Context, string, string) (string, error) {
	return "v0.1", nil
}
func (fakeArtifactService) UpdateStatus(context.Context, string, artifact.StatusUpdate) (*artifact.Artifact, error) {
	panic("unexpected UpdateStatus call")
}
func (fakeArtifactService) Delete(context.Context, string) error { panic("unexpected Delete call") }
func (fakeArtifactService) FileContent(context.Context, string, string) ([]byte, error) {
	panic("unexpected FileContent call")
}
func (fakeArtifactService) CheckConflicts(context.Context, []string) (*artifact.ConflictReport, error) {
	panic("unexpected CheckConflicts call")
}
func (fakeArtifactService) ListEvents(context.Context, artifact.EventFilter) ([]artifact.Event, error) {
	panic("unexpected ListEvents call")
}
func (fakeArtifactService) RefreshReadinessRuns(context.Context, string, []artifact.ReadinessEvaluation) ([]artifact.ReadinessRun, error) {
	panic("unexpected RefreshReadinessRuns call")
}
func (fakeArtifactService) ListReadinessRuns(context.Context, string, int) ([]artifact.ReadinessRun, error) {
	panic("unexpected ListReadinessRuns call")
}

// ---------------------------------------------------------------------------
// Publish tests cover explicit documents, path safety, and slashed file reads.
// ---------------------------------------------------------------------------

// memArtifactRepo is an in-memory artifact.Repository for handler tests.
type memArtifactRepo struct {
	artifacts map[string]*artifact.Artifact
	readiness []artifact.ReadinessRun
}

func newMemArtifactRepo() *memArtifactRepo {
	return &memArtifactRepo{artifacts: map[string]*artifact.Artifact{}}
}

func (r *memArtifactRepo) InsertWithEvent(_ context.Context, a *artifact.Artifact, _ artifact.Event) error {
	cp := *a
	cp.Files = append([]artifact.File(nil), a.Files...)
	r.artifacts[a.ID] = &cp
	return nil
}
func (r *memArtifactRepo) Get(_ context.Context, id string) (*artifact.Artifact, error) {
	a, ok := r.artifacts[id]
	if !ok {
		return nil, artifact.ErrNotFound
	}
	cp := *a
	cp.Files = append([]artifact.File(nil), a.Files...)
	return &cp, nil
}
func (r *memArtifactRepo) List(context.Context, artifact.ListFilter) ([]artifact.Artifact, error) {
	return nil, nil
}
func (r *memArtifactRepo) Count(context.Context, artifact.ListFilter) (int64, error) { return 0, nil }
func (r *memArtifactRepo) InsertReadinessRuns(_ context.Context, rows []artifact.ReadinessRun) error {
	r.readiness = append(r.readiness, rows...)
	return nil
}
func (r *memArtifactRepo) ListReadinessRuns(_ context.Context, artifactID string, limit int) ([]artifact.ReadinessRun, error) {
	if limit <= 0 {
		limit = 50
	}
	out := make([]artifact.ReadinessRun, 0, len(r.readiness))
	for _, row := range r.readiness {
		if row.ArtifactID == artifactID {
			out = append(out, row)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (r *memArtifactRepo) UpdateStatus(_ context.Context, id string, status artifact.Status, _ string, _ artifact.Event) error {
	a, ok := r.artifacts[id]
	if !ok {
		return artifact.ErrNotFound
	}
	a.Status = status
	return nil
}
func (r *memArtifactRepo) Delete(context.Context, string) error { return nil }
func (r *memArtifactRepo) FindOverlappingServices(context.Context, []string, string) ([]artifact.Artifact, error) {
	return nil, nil
}
func (r *memArtifactRepo) ListEvents(context.Context, artifact.EventFilter) ([]artifact.Event, error) {
	return nil, nil
}

// memArtifactStore is an in-memory artifact.ObjectStore.
type memArtifactStore struct {
	objects map[string][]byte
}

func newMemArtifactStore() *memArtifactStore {
	return &memArtifactStore{objects: map[string][]byte{}}
}
func (s *memArtifactStore) PutObject(_ context.Context, key string, body []byte) error {
	s.objects[key] = append([]byte(nil), body...)
	return nil
}
func (s *memArtifactStore) GetObject(_ context.Context, key string, _ int64) ([]byte, error) {
	b, ok := s.objects[key]
	if !ok {
		return nil, artifact.ErrFileNotFound
	}
	return append([]byte(nil), b...), nil
}
func (s *memArtifactStore) DeleteObject(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}

// newPublishTestServer wires a real RegistryService (in-memory) into a test Router.
// Returns the HTTP handler and the underlying repo so callers can inspect stored state.
func newPublishTestServer(t *testing.T) (http.Handler, *memArtifactRepo) {
	t.Helper()
	repo := newMemArtifactRepo()
	store := newMemArtifactStore()
	svc := artifact.NewService(repo, store, func(featureID, version, path string) string {
		return "artifacts/" + featureID + "/" + version + "/" + path
	})
	rt := &Router{
		Handlers: &Handlers{
			Artifacts: svc,
			Governance: &governanceops.Service{
				ArtifactWriter:  svc,
				FeatureUpserter: &cliTestFeatureUpserter{},
				ProfileResolver: &cliTestProfileResolver{},
			},
		},
		Config: testConfig(),
	}
	return rt.Build(), repo
}

// minPublishBody returns a JSON publish body string with only required fields set,
// plus optional extra.
func minPublishBody(extra string) string {
	base := `{"workspace_id":"ws-test","feature_key":"test-feature","request_type":"new_feature","impact_level":"low"`
	if extra != "" {
		return base + "," + extra + "}"
	}
	return base + "}"
}

func b64(s string) string { return s }

func TestPublishArtifact_FixedFilesMapIsRejected(t *testing.T) {
	t.Parallel()
	srv, repo := newPublishTestServer(t)

	body := minPublishBody(`"files":{"spec":"` + b64("# Spec content") + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/publish?workspace_id=ws-test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rr.Code, rr.Body.String())
	}
	if n := len(repo.artifacts); n != 0 {
		t.Fatalf("stored %d artifacts, want 0", n)
	}
}

func TestPublishArtifact_PreservesEmptyDocument(t *testing.T) {
	t.Parallel()
	srv, repo := newPublishTestServer(t)
	body := minPublishBody(`"documents":[{"path":"empty.md","role":"reference","content":""}]`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/publish?workspace_id=ws-test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if len(repo.artifacts) != 1 {
		t.Fatalf("stored artifacts = %d, want 1", len(repo.artifacts))
	}
	for _, published := range repo.artifacts {
		if len(published.Files) != 1 || published.Files[0].Path != "empty.md" || published.Files[0].SizeBytes != 0 {
			t.Fatalf("files = %+v, want one zero-byte empty.md", published.Files)
		}
	}
}

func TestArtifactReadinessRunsLifecycle(t *testing.T) {
	t.Parallel()
	srv, repo := newPublishTestServer(t)

	body := minPublishBody(`"documents":[{"path":"spec.md","role":"spec","content":"` + b64("# Spec content") + `"}]`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/publish?workspace_id=ws-test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("publish status = %d body=%s", rr.Code, rr.Body.String())
	}
	var pubResp struct {
		ID string `json:"artifact_id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &pubResp); err != nil {
		t.Fatalf("unmarshal publish: %v", err)
	}
	if _, err := repo.Get(context.Background(), pubResp.ID); err != nil {
		t.Fatalf("stored artifact missing: %v", err)
	}

	refresh := serveJSON(srv, http.MethodPost, "/artifacts/"+pubResp.ID+"/readiness-runs/refresh?workspace_id=ws-test", `{
	  "evaluations": [{
	    "gate": "spec_completeness",
	    "state": "warn",
	    "hint": "missing constraints",
	    "confidence": 0.8,
	    "judge_model": "governance-gate-judge",
	    "eval_suite_version": "spec-completeness-v1",
	    "evidence": "{\"topics\":[]}"
	  }]
	}`)
	if refresh.Code != http.StatusOK {
		t.Fatalf("refresh status = %d body=%s", refresh.Code, refresh.Body.String())
	}

	list := serveJSON(srv, http.MethodGet, "/artifacts/"+pubResp.ID+"/readiness-runs?workspace_id=ws-test&limit=10", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", list.Code, list.Body.String())
	}
	var out struct {
		Items []artifact.ReadinessRun `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal list: %v body=%s", err, list.Body.String())
	}
	if len(out.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(out.Items))
	}
	if out.Items[0].Gate != "spec_completeness" {
		t.Fatalf("gate = %q, want spec_completeness", out.Items[0].Gate)
	}
	if out.Items[0].Executor != "platform" {
		t.Fatalf("executor = %q, want platform", out.Items[0].Executor)
	}
}

func TestPublishArtifact_RejectsFilesAlongsideDocuments(t *testing.T) {
	t.Parallel()
	srv, _ := newPublishTestServer(t)

	extra := `"documents":[{"path":"a.md","role":"plan","content":"` + b64("plan content") + `"}],` +
		`"files":{"spec":"` + b64("spec content") + `"}`
	body := minPublishBody(extra)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/publish?workspace_id=ws-test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rr.Code, rr.Body.String())
	}
}

// TestPublishArtifact_UnsafePath_Returns422 verifies that a document with a
// path-traversal path returns a 422 (client error), not a 500.
func TestPublishArtifact_UnsafePath_Returns422(t *testing.T) {
	t.Parallel()
	srv, _ := newPublishTestServer(t)

	cases := []struct {
		name string
		path string
	}{
		{"dotdot", "../x.md"},
		{"absolute", "/etc/passwd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			extra := `"documents":[{"path":"` + tc.path + `","role":"spec","content":"` + b64("x") + `"}]`
			body := minPublishBody(extra)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/publish?workspace_id=ws-test", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)

			// Must be a 4xx client error, not a 5xx server error.
			if rr.Code < 400 || rr.Code >= 500 {
				t.Errorf("status = %d, want 4xx client error; body=%s", rr.Code, rr.Body.String())
			}
		})
	}
}

func TestPublishArtifact_AllowsDotsInsideFilename(t *testing.T) {
	t.Parallel()
	srv, _ := newPublishTestServer(t)
	extra := `"documents":[{"path":"docs/spec..draft.md","role":"spec","content":"` + b64("x") + `"}]`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/publish?workspace_id=ws-test", strings.NewReader(minPublishBody(extra)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.ServeHTTP(rr, req)

	if rr.Code < 200 || rr.Code >= 300 {
		t.Fatalf("status = %d, want 2xx; body=%s", rr.Code, rr.Body.String())
	}
}

// TestPublishArtifact_SlashedPathFileGet publishes a document at
// "docs/proposal.md" then retrieves it via ?path= query param (the route that
// avoids URL-segment mangling for slash-containing paths).
func TestPublishArtifact_SlashedPathFileGet(t *testing.T) {
	t.Parallel()
	srv, _ := newPublishTestServer(t)

	// Publish a document with a slashed path.
	docContent := "# Proposal"
	extra := `"documents":[{"path":"docs/proposal.md","role":"spec","content":"` + b64(docContent) + `"}]`
	body := minPublishBody(extra)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/publish?workspace_id=ws-test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("publish status = %d; body=%s", rr.Code, rr.Body.String())
	}

	var resp struct {
		ID string `json:"artifact_id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal publish response: %v", err)
	}

	// GET /artifacts/{id}/files/_?path=docs/proposal.md
	getURL := "/artifacts/" + resp.ID + "/files/_?workspace_id=ws-test&path=docs%2Fproposal.md"
	getReq := httptest.NewRequest(http.MethodGet, getURL, nil)
	getRR := httptest.NewRecorder()
	srv.ServeHTTP(getRR, getReq)

	if getRR.Code != http.StatusOK {
		t.Fatalf("file-get status = %d; body=%s", getRR.Code, getRR.Body.String())
	}
	if !strings.Contains(getRR.Body.String(), docContent) {
		t.Errorf("file-get response missing content %q; body=%s", docContent, getRR.Body.String())
	}
}

// TestListArtifactFiles exercises GET /artifacts/{id}/files:
// - returns all documents with path, role, and size_bytes > 0
// - ?role= filter returns only matching documents
// - unknown id returns 404
// - the explicit single-file route (/files/_?path=...) works (no collision)
func TestListArtifactFiles(t *testing.T) {
	t.Parallel()
	srv, _ := newPublishTestServer(t)

	// Publish an artifact with two documents using explicit roles.
	extra := `"documents":[` +
		`{"path":"spec.md","role":"spec","content":"` + b64("# Spec content") + `"},` +
		`{"path":"docs/plan.md","role":"plan","content":"` + b64("# Plan content") + `"}` +
		`]`
	body := minPublishBody(extra)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/publish?workspace_id=ws-test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("publish status = %d; body=%s", rr.Code, rr.Body.String())
	}
	var pubResp struct {
		ID string `json:"artifact_id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &pubResp); err != nil {
		t.Fatalf("unmarshal publish response: %v", err)
	}

	type fileItem struct {
		Path      string `json:"path"`
		Role      string `json:"role"`
		SizeBytes int64  `json:"size_bytes"`
	}
	type listResp struct {
		Items []fileItem `json:"items"`
	}

	// GET /artifacts/{id}/files — expect 200 with both documents.
	t.Run("list_all", func(t *testing.T) {
		t.Parallel()
		listReq := httptest.NewRequest(http.MethodGet, "/artifacts/"+pubResp.ID+"/files?workspace_id=ws-test", nil)
		listRR := httptest.NewRecorder()
		srv.ServeHTTP(listRR, listReq)
		if listRR.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", listRR.Code, listRR.Body.String())
		}
		var resp listResp
		if err := json.Unmarshal(listRR.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v; body=%s", err, listRR.Body.String())
		}
		if len(resp.Items) != 2 {
			t.Fatalf("items = %d, want 2; items=%+v", len(resp.Items), resp.Items)
		}
		// Items sorted by path: docs/plan.md < spec.md.
		if resp.Items[0].Path != "docs/plan.md" {
			t.Errorf("items[0].Path = %q, want %q", resp.Items[0].Path, "docs/plan.md")
		}
		if resp.Items[0].Role != "plan" {
			t.Errorf("items[0].Role = %q, want %q", resp.Items[0].Role, "plan")
		}
		if resp.Items[0].SizeBytes <= 0 {
			t.Errorf("items[0].SizeBytes = %d, want > 0", resp.Items[0].SizeBytes)
		}
		if resp.Items[1].Path != "spec.md" {
			t.Errorf("items[1].Path = %q, want %q", resp.Items[1].Path, "spec.md")
		}
		if resp.Items[1].Role != "spec" {
			t.Errorf("items[1].Role = %q, want %q", resp.Items[1].Role, "spec")
		}
		if resp.Items[1].SizeBytes <= 0 {
			t.Errorf("items[1].SizeBytes = %d, want > 0", resp.Items[1].SizeBytes)
		}
	})

	// GET /artifacts/{id}/files?role=spec — expect only the spec document.
	t.Run("filter_by_role", func(t *testing.T) {
		t.Parallel()
		listReq := httptest.NewRequest(http.MethodGet, "/artifacts/"+pubResp.ID+"/files?workspace_id=ws-test&role=spec", nil)
		listRR := httptest.NewRecorder()
		srv.ServeHTTP(listRR, listReq)
		if listRR.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", listRR.Code, listRR.Body.String())
		}
		var resp listResp
		if err := json.Unmarshal(listRR.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v; body=%s", err, listRR.Body.String())
		}
		if len(resp.Items) != 1 {
			t.Fatalf("items = %d, want 1; items=%+v", len(resp.Items), resp.Items)
		}
		if resp.Items[0].Path != "spec.md" {
			t.Errorf("items[0].Path = %q, want %q", resp.Items[0].Path, "spec.md")
		}
		if resp.Items[0].Role != "spec" {
			t.Errorf("items[0].Role = %q, want %q", resp.Items[0].Role, "spec")
		}
	})

	// GET /artifacts/unknown-id/files — expect 404.
	t.Run("unknown_id", func(t *testing.T) {
		t.Parallel()
		listReq := httptest.NewRequest(http.MethodGet, "/artifacts/unknown-id/files?workspace_id=ws-test", nil)
		listRR := httptest.NewRecorder()
		srv.ServeHTTP(listRR, listReq)
		if listRR.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body=%s", listRR.Code, listRR.Body.String())
		}
	})

	// Confirm existing single-file route still works (no collision).
	t.Run("single_file_route_no_collision", func(t *testing.T) {
		t.Parallel()
		getURL := "/artifacts/" + pubResp.ID + "/files/_?workspace_id=ws-test&path=spec.md"
		getReq := httptest.NewRequest(http.MethodGet, getURL, nil)
		getRR := httptest.NewRecorder()
		srv.ServeHTTP(getRR, getReq)
		if getRR.Code != http.StatusOK {
			t.Fatalf("single-file route status = %d, want 200; body=%s", getRR.Code, getRR.Body.String())
		}
	})

	t.Run("fixed_file_key_route_is_removed", func(t *testing.T) {
		t.Parallel()
		getURL := "/artifacts/" + pubResp.ID + "/files/spec?workspace_id=ws-test"
		getReq := httptest.NewRequest(http.MethodGet, getURL, nil)
		getRR := httptest.NewRecorder()
		srv.ServeHTTP(getRR, getReq)
		if getRR.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body=%s", getRR.Code, getRR.Body.String())
		}
	})
}
