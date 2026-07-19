package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/governancefiles"
	"github.com/specgate/doc-registry/internal/storage/blob"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
	stores3 "github.com/specgate/doc-registry/internal/storage/s3"
)

// getReq builds a GET request, runs srv.ServeHTTP, and returns the recorder.
func getReq(t *testing.T, srv http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	return rr
}

// postJSON builds a POST request with optional JSON body, runs srv.ServeHTTP,
// and returns the recorder.
func postJSON(t *testing.T, srv http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader *strings.Reader
	if body == "" {
		bodyReader = strings.NewReader("{}")
	} else {
		bodyReader = strings.NewReader(body)
	}
	req := httptest.NewRequest("POST", path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	return rr
}

// jsonField parses resp as JSON and returns the top-level string field named key.
func jsonField(t *testing.T, resp, key string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(resp), &m); err != nil {
		t.Fatalf("jsonField: unmarshal: %v; body=%s", err, resp)
	}
	v, ok := m[key]
	if !ok {
		t.Fatalf("jsonField: key %q not found in %s", key, resp)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("jsonField: key %q is not a string in %s", key, resp)
	}
	return s
}

// newTestServer spins up an in-process HTTP handler with a real Postgres DB,
// a fake (presign-only) S3 client, and the GovernanceFiles store wired.
// It is only used by the handlers_governance_files tests.
func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	db := newTestGormDB(t)

	s3c, err := stores3.NewForTest(context.Background(), "http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}

	repo := storagedb.NewGovernanceFilesRepository(db)
	rt := &Router{
		Handlers: &Handlers{
			S3:              s3c,
			GovernanceFiles: repo,
		},
		Config: testConfig(),
	}
	return rt.Build()
}

func TestPresignFile_RejectsBadContentType(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	body := `{"workspace_id":"ws-test","name":"hello.zip","content_type":"application/zip","size_bytes":10}`
	req := httptest.NewRequest("POST", "/governance/files/presign", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != 400 {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestPresignFile_AllowsHtml(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	// text/html is allowed so generated HTML previews can be explicitly pinned.
	rr := postJSON(t, srv, "/governance/files/presign",
		`{"workspace_id":"ws-test","name":"mock.html","content_type":"text/html","size_bytes":2048}`)
	if rr.Code != 200 {
		t.Fatalf("presign html status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestGovernanceFilesAPI_WorkspaceScopesCatalog(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	for _, workspaceID := range []string{"ws-a", "ws-b"} {
		rr := postJSON(t, srv, "/governance/files/presign", `{"workspace_id":"`+workspaceID+`","name":"same.md","content_type":"text/markdown","size_bytes":10}`)
		if rr.Code != http.StatusOK {
			t.Fatalf("presign %s status=%d body=%s", workspaceID, rr.Code, rr.Body.String())
		}
		fileID := jsonField(t, rr.Body.String(), "file_id")
		if rr = postJSON(t, srv, "/governance/files/"+fileID+"/commit?workspace_id="+workspaceID, "{}"); rr.Code != http.StatusOK {
			t.Fatalf("commit %s status=%d body=%s", workspaceID, rr.Code, rr.Body.String())
		}
	}
	rr := getReq(t, srv, "/governance/files?workspace_id=ws-a&limit=10")
	if rr.Code != http.StatusOK {
		t.Fatalf("list ws-a status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Items []GovernanceFileDTO `json:"items"`
		Total int64               `json:"total"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode list: %v; body=%s", err, rr.Body.String())
	}
	if body.Total != 1 || len(body.Items) != 1 {
		t.Fatalf("ws-a list = %+v / total=%d, want one file", body.Items, body.Total)
	}
}

// uploadReq POSTs raw bytes to /governance/files/upload with the given headers.
func uploadReq(t *testing.T, srv http.Handler, contentType, fileName, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/governance/files/upload?workspace_id=ws-test", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if fileName != "" {
		req.Header.Set("X-File-Name", fileName)
	}
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	return rr
}

func TestUploadGovernanceFile_RejectsBadContentType(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	if rr := uploadReq(t, srv, "application/zip", "x.zip", "data"); rr.Code != 400 {
		t.Fatalf("status=%d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestUploadGovernanceFile_RequiresFileName(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	if rr := uploadReq(t, srv, "image/png", "", "data"); rr.Code != 400 {
		t.Fatalf("status=%d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestUploadGovernanceFile_RejectsEmptyBody(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	if rr := uploadReq(t, srv, "image/png", "x.png", ""); rr.Code != 400 {
		t.Fatalf("status=%d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestGovernanceFileWorkspaceRejectsUnsafePathSegments(t *testing.T) {
	t.Parallel()
	for _, workspaceID := range []string{"../ws-b", "ws/a", `ws\a`, "ws..b", "."} {
		rec := httptest.NewRecorder()
		if got, ok := requiredGovernanceFileWorkspace(rec, workspaceID); ok || got != "" {
			t.Errorf("workspace_id %q accepted as %q", workspaceID, got)
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("workspace_id %q status = %d, want 400", workspaceID, rec.Code)
		}
	}
}

func TestUploadGovernanceFileDeletesObjectWhenMetadataCreateFails(t *testing.T) {
	t.Parallel()
	blobs := &uploadTrackingBlobStore{}
	h := &Handlers{
		BlobStore:       blobs,
		GovernanceFiles: &createFailingGovernanceFileStore{err: errors.New("database unavailable")},
	}
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/governance/files/upload?workspace_id=ws-test", strings.NewReader("content")).WithContext(requestContext)
	req.Header.Set("Content-Type", "text/markdown")
	req.Header.Set("X-File-Name", "spec.md")
	rr := httptest.NewRecorder()

	h.UploadGovernanceFile(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500; body=%s", rr.Code, rr.Body.String())
	}
	if blobs.putID == "" || blobs.deletedID != blobs.putID {
		t.Fatalf("blob put=%q deleted=%q", blobs.putID, blobs.deletedID)
	}
	if blobs.deleteContextErr != nil {
		t.Fatalf("cleanup reused canceled request context: %v", blobs.deleteContextErr)
	}
}

type uploadTrackingBlobStore struct {
	putID            string
	deletedID        string
	deleteContextErr error
}

func (s *uploadTrackingBlobStore) Put(ctx context.Context, body io.Reader, _ int64, _ string) (string, error) {
	if governancefiles.WorkspaceID(ctx) != "ws-test" {
		return "", errors.New("missing workspace scope")
	}
	if _, err := io.ReadAll(body); err != nil {
		return "", err
	}
	s.putID = "workspaces/ws-test/00000000-0000-0000-0000-000000000001"
	return s.putID, nil
}

func (*uploadTrackingBlobStore) Open(context.Context, string) (io.ReadCloser, error) {
	panic("unexpected Open")
}

func (*uploadTrackingBlobStore) Stat(context.Context, string) (blob.Meta, error) {
	panic("unexpected Stat")
}

func (s *uploadTrackingBlobStore) Delete(ctx context.Context, id string) error {
	s.deletedID = id
	s.deleteContextErr = ctx.Err()
	return nil
}

type createFailingGovernanceFileStore struct {
	governancefiles.Store
	err error
}

func (s *createFailingGovernanceFileStore) Create(context.Context, governancefiles.File) (*governancefiles.File, error) {
	return nil, s.err
}

func TestPresignFile_HappyPath(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	body := `{"workspace_id":"ws-test","name":"hello.png","content_type":"image/png","size_bytes":1024}`
	req := httptest.NewRequest("POST", "/governance/files/presign", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"file_id"`) ||
		!strings.Contains(rr.Body.String(), `"upload_url"`) ||
		!strings.Contains(rr.Body.String(), `"object_key"`) {
		t.Fatalf("missing fields: %s", rr.Body.String())
	}
	var response struct {
		UploadURL string `json:"upload_url"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	uploadURL, err := url.Parse(response.UploadURL)
	if err != nil {
		t.Fatal(err)
	}
	if signed := uploadURL.Query().Get("X-Amz-SignedHeaders"); !strings.Contains(signed, "content-length") {
		t.Fatalf("presigned upload does not enforce declared size: signed_headers=%q", signed)
	}
}

func TestCommitFile_FlipsToReadyAndScrubsObjectStoreURL(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	// Presign first to create the pending row.
	pres := postJSON(t, srv, "/governance/files/presign",
		`{"workspace_id":"ws-test","name":"x.png","content_type":"image/png","size_bytes":10}`)
	if pres.Code != 200 {
		t.Fatalf("presign status=%d body=%s", pres.Code, pres.Body.String())
	}
	id := jsonField(t, pres.Body.String(), "file_id")

	rr := postJSON(t, srv, "/governance/files/"+id+"/commit?workspace_id=ws-test", "")
	if rr.Code != 200 {
		t.Fatalf("commit status=%d body=%s", rr.Code, rr.Body.String())
	}
	// The response must NOT leak a presigned object-store URL or credentials —
	// readers use the /content proxy. get_url is empty.
	body := rr.Body.String()
	if strings.Contains(body, "X-Amz-") || strings.Contains(body, "minio") {
		t.Fatalf("commit response leaked an object-store URL: %s", body)
	}
	if !strings.Contains(body, `"get_url":""`) {
		t.Fatalf("expected empty get_url, got: %s", body)
	}
}

func TestServeGovernanceFileContent_NotFoundAndCaching(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	// Unknown id → 404 (never leaks an object-store URL).
	if rr := getReq(t, srv, "/governance/files/nope/content?workspace_id=ws-test"); rr.Code != 404 {
		t.Fatalf("missing file status=%d, want 404", rr.Code)
	}
	// Create + commit a row (only ready files have an S3 object to serve), then a
	// conditional GET matching the ETag → 304 without an S3 read (content is
	// immutable per id, so the proxy is cacheable).
	pres := postJSON(t, srv, "/governance/files/presign",
		`{"workspace_id":"ws-test","name":"x.png","content_type":"image/png","size_bytes":10}`)
	id := jsonField(t, pres.Body.String(), "file_id")
	if rr := postJSON(t, srv, "/governance/files/"+id+"/commit?workspace_id=ws-test", ""); rr.Code != 200 {
		t.Fatalf("commit status=%d body=%s", rr.Code, rr.Body.String())
	}

	req := httptest.NewRequest("GET", "/governance/files/"+id+"/content?workspace_id=ws-test", nil)
	req.Header.Set("If-None-Match", `"`+id+`"`)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotModified {
		t.Fatalf("conditional GET status=%d, want 304; body=%s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Cache-Control") == "" || rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing cache/nosniff headers: %+v", rr.Header())
	}
	// Untrusted uploads (html/svg) must be sandboxed so a top-level open cannot
	// script the doc-registry origin.
	if csp := rr.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") {
		t.Fatalf("missing sandbox CSP on content proxy: %q", csp)
	}
}

func TestCommitFile_404OnUnknownID(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rr := postJSON(t, srv, "/governance/files/does-not-exist/commit?workspace_id=ws-test", "")
	if rr.Code != 404 {
		t.Fatalf("status=%d, want 404", rr.Code)
	}
}

func TestListFiles_OrdersByLastUsedAndFiltersByQ(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	// Presign + commit two files.
	for _, name := range []string{"alpha.png", "beta-picture.png"} {
		pres := postJSON(t, srv, "/governance/files/presign",
			`{"workspace_id":"ws-test","name":"`+name+`","content_type":"image/png","size_bytes":1}`)
		id := jsonField(t, pres.Body.String(), "file_id")
		_ = postJSON(t, srv, "/governance/files/"+id+"/commit?workspace_id=ws-test", "")
	}

	rr := getReq(t, srv, "/governance/files?workspace_id=ws-test&limit=10")
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"items"`) ||
		!strings.Contains(rr.Body.String(), `"total":2`) {
		t.Fatalf("body missing fields: %s", rr.Body.String())
	}

	rr = getReq(t, srv, "/governance/files?workspace_id=ws-test&q=picture")
	if !strings.Contains(rr.Body.String(), `"total":1`) ||
		!strings.Contains(rr.Body.String(), "beta-picture.png") {
		t.Fatalf("q filter wrong: %s", rr.Body.String())
	}
}

// deleteReq builds a DELETE request, runs srv.ServeHTTP, and returns the recorder.
func deleteReq(t *testing.T, srv http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("DELETE", path, nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	return rr
}

func TestTouchFile_BumpsLastUsed(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	pres := postJSON(t, srv, "/governance/files/presign",
		`{"workspace_id":"ws-test","name":"x.png","content_type":"image/png","size_bytes":1}`)
	id := jsonField(t, pres.Body.String(), "file_id")
	commit := postJSON(t, srv, "/governance/files/"+id+"/commit?workspace_id=ws-test", "")
	firstUsed := jsonField(t, commit.Body.String(), "last_used_at")

	time.Sleep(1100 * time.Millisecond) // ensure RFC3339 second resolution differs

	rr := postJSON(t, srv, "/governance/files/"+id+"/touch?workspace_id=ws-test", "")
	if rr.Code != 200 {
		t.Fatalf("touch status=%d body=%s", rr.Code, rr.Body.String())
	}
	secondUsed := jsonField(t, rr.Body.String(), "last_used_at")
	if secondUsed == firstUsed {
		t.Fatalf("last_used_at unchanged: %s", secondUsed)
	}
}

func TestDeleteFile_RemovesRowAndS3Object(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	pres := postJSON(t, srv, "/governance/files/presign",
		`{"workspace_id":"ws-test","name":"y.png","content_type":"image/png","size_bytes":1}`)
	id := jsonField(t, pres.Body.String(), "file_id")
	_ = postJSON(t, srv, "/governance/files/"+id+"/commit?workspace_id=ws-test", "")

	rr := deleteReq(t, srv, "/governance/files/"+id+"?workspace_id=ws-test")
	if rr.Code != 204 {
		t.Fatalf("delete status=%d body=%s", rr.Code, rr.Body.String())
	}

	listed := getReq(t, srv, "/governance/files?workspace_id=ws-test&limit=10")
	if !strings.Contains(listed.Body.String(), `"total":0`) {
		t.Fatalf("expected empty list after delete: %s", listed.Body.String())
	}
}

func TestDeleteFile_DeletesLocalBlob(t *testing.T) {
	t.Parallel()

	const key = "workspaces/ws-test/12345678-1234-1234-1234-123456789012"
	files := &workspaceCheckingGovernanceFiles{objectKey: key}
	blobs := &memoryBlobStore{objects: map[string][]byte{key: []byte("local source")}}
	h := &Handlers{GovernanceFiles: files, BlobStore: blobs}
	in := &DeleteFileInput{ID: "file-in-ws-test", WorkspaceID: "ws-test"}

	if _, err := h.DeleteFile(context.Background(), in); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if files.workspaceID != "ws-test" {
		t.Fatalf("delete workspace = %q, want ws-test", files.workspaceID)
	}
	if files.deletedID != in.ID {
		t.Fatalf("deleted file id = %q, want %q", files.deletedID, in.ID)
	}
	if len(blobs.deleted) != 1 || blobs.deleted[0] != key {
		t.Fatalf("deleted blobs = %v, want [%s]", blobs.deleted, key)
	}
	if _, ok := blobs.objects[key]; ok {
		t.Fatalf("local blob %q remains after delete", key)
	}
}

func TestDeleteFile_RejectsReferencedFile(t *testing.T) {
	t.Parallel()

	files := &workspaceCheckingGovernanceFiles{deleteErr: governancefiles.ErrReferenced}
	srv := (&Router{Handlers: &Handlers{GovernanceFiles: files}, Config: testConfig()}).Build()
	rr := deleteReq(t, srv, "/governance/files/file-in-ws-test?workspace_id=ws-test")
	if rr.Code != http.StatusConflict {
		t.Fatalf("delete status=%d, want 409; body=%s", rr.Code, rr.Body.String())
	}
	if files.workspaceID != "ws-test" {
		t.Fatalf("delete workspace = %q, want ws-test", files.workspaceID)
	}
}

// newBlobTestServer spins up an in-process HTTP handler with a real Postgres DB,
// a local BlobStore, and NO reachable S3 endpoint. Any code that falls through to
// S3 would 500, which acts as a free assertion that the BlobStore branch ran.
func newBlobTestServer(t *testing.T) http.Handler {
	t.Helper()
	db := newTestGormDB(t)

	// Fake S3 pointing at a port that will never answer — any accidental S3 call
	// fails immediately, proving the BlobStore branch ran.
	s3c, err := stores3.NewForTest(context.Background(), "http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}

	blobStore, err := blob.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	repo := storagedb.NewGovernanceFilesRepository(db)
	rt := &Router{
		Handlers: &Handlers{
			S3:              s3c,
			BlobStore:       blobStore,
			GovernanceFiles: repo,
		},
		Config: testConfig(),
	}
	return rt.Build()
}

// TestUploadGovernanceFile_BlobStore_RoundTrip verifies that when BlobStore is set:
//   - UploadGovernanceFile returns 200 (proving the blob branch ran, not S3 which is unreachable)
//   - ServeGovernanceFileContent returns 200 and the original body (proving looksLikeBlobID
//     correctly routes UUID-shaped ObjectKeys to the blob store)
func TestUploadGovernanceFile_BlobStore_RoundTrip(t *testing.T) {
	t.Parallel()
	srv := newBlobTestServer(t)

	const content = "# Hello from BlobStore"
	rr := uploadReq(t, srv, "text/markdown", "test.md", content)
	if rr.Code != 200 {
		t.Fatalf("upload status=%d body=%s", rr.Code, rr.Body.String())
	}
	id := jsonField(t, rr.Body.String(), "file_id")
	if id == "" {
		t.Fatalf("no file_id in response: %s", rr.Body.String())
	}

	// Serve the content back and confirm the body matches.
	got := getReq(t, srv, "/governance/files/"+id+"/content?workspace_id=ws-test")
	if got.Code != 200 {
		t.Fatalf("serve status=%d body=%s", got.Code, got.Body.String())
	}
	if got.Body.String() != content {
		t.Fatalf("content mismatch: got %q, want %q", got.Body.String(), content)
	}
}
