package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/specgate/doc-registry/internal/config"
	"github.com/specgate/doc-registry/internal/integrations"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
)

// repoFileTestServer wires a REST server backed by Postgres plus a seeded GitLab
// integration whose BaseURL points at a fake GitLab API. The fake returns the
// per-path status the test wants so GetRepoFile's full provider + error-mapping
// path runs end-to-end. project is the project resource external_key.
//
// gitlabStatus controls the fake's response for the repository-files endpoint:
// 200 → file content; 404 → not found; 500 → upstream error.
func repoFileTestServer(t *testing.T, project string, gitlabStatus int) *httptest.Server {
	t.Helper()
	t.Setenv(integrations.SecretKeyEnvVar, "0000000000000000000000000000000000000000000000000000000000000001")

	// Fake GitLab API. The provider hits
	// {BaseURL}/api/v4/projects/{enc_id}/repository/files/{enc_path}.
	gitlab := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/repository/files/") {
			http.Error(w, "unexpected path", http.StatusBadGateway)
			return
		}
		switch gitlabStatus {
		case http.StatusOK:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"file_path":      "README.md",
				"size":           int64(11),
				"encoding":       "base64",
				"content":        "IyBoZWxsbwo=", // "# hello\n"
				"last_commit_id": "deadbeef",
				"blob_id":        "blob1",
			})
		case http.StatusNotFound:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"404 File Not Found"}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
		}
	}))
	t.Cleanup(gitlab.Close)

	db := newTestGormDB(t)

	ctx := context.Background()
	intRepo := storagedb.NewIntegrationRepository(db)
	enc, err := integrations.EncryptSecret("gl_real_token")
	if err != nil {
		t.Fatal(err)
	}
	integration, err := intRepo.CreateIntegration(ctx, integrations.Integration{
		ID:                "gl-1",
		WorkspaceID:       "ws-test",
		Provider:          integrations.ProviderGitLab,
		Name:              "GL",
		Status:            integrations.StatusConnected,
		BaseURL:           gitlab.URL, // host-only; provider appends /api/v4
		APITokenEncrypted: enc,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intRepo.CreateResource(ctx, integrations.Resource{
		ID:            "res-1",
		IntegrationID: integration.ID,
		ResourceType:  integrations.ResourceTypeProject,
		ExternalKey:   project,
		DefaultRef:    "main",
	}); err != nil {
		t.Fatal(err)
	}

	workBoardRepo := storagedb.NewWorkBoardRepository(db)
	handlers := &Handlers{
		Integrations: integrations.NewServiceWithWorkBoard(intRepo, workBoardRepo),
		WorkBoard:    workBoardRepo,
	}
	rt := &Router{
		Handlers: handlers,
		Config:   &config.Config{OpenAPI: config.OpenAPIConfig{Enabled: false}},
	}
	return httptest.NewServer(DevCORS(rt.Build()))
}

func getRepoFile(t *testing.T, baseURL, project, path string) (int, struct {
	Content string `json:"content"`
	Found   bool   `json:"found"`
}) {
	t.Helper()
	var body struct {
		Content string `json:"content"`
		Found   bool   `json:"found"`
	}
	q := url.Values{"workspace_id": {"ws-test"}, "project": {project}, "path": {path}}
	resp, err := http.Get(baseURL + "/repos/file?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, body
}

func TestGetRepoFile_UnknownProjectIs404(t *testing.T) {
	srv := repoFileTestServer(t, "group/project", http.StatusOK)
	defer srv.Close()

	status, _ := getRepoFile(t, srv.URL, "group/does-not-exist", "README.md")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for unknown project", status)
	}
}

func TestGetRepoFileRequiresWorkspace(t *testing.T) {
	srv := repoFileTestServer(t, "group/project", http.StatusOK)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/repos/file?project=group%2Fproject&path=README.md")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}

func TestGetRepoFileDoesNotReadAnotherWorkspace(t *testing.T) {
	srv := repoFileTestServer(t, "group/project", http.StatusOK)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/repos/file?workspace_id=ws-other&project=group%2Fproject&path=README.md")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestGetRepoFile_FoundReturnsContent(t *testing.T) {
	srv := repoFileTestServer(t, "group/project", http.StatusOK)
	defer srv.Close()

	status, body := getRepoFile(t, srv.URL, "group/project", "README.md")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if !body.Found {
		t.Fatalf("found = false, want true")
	}
	if !strings.Contains(body.Content, "# hello") {
		t.Fatalf("content = %q, want decoded file body", body.Content)
	}
}

func TestGetRepoFile_MissingFileIsFoundFalse(t *testing.T) {
	srv := repoFileTestServer(t, "group/project", http.StatusNotFound)
	defer srv.Close()

	status, body := getRepoFile(t, srv.URL, "group/project", "README.md")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (missing file is not an error)", status)
	}
	if body.Found {
		t.Fatalf("found = true, want false for a missing file")
	}
}

func TestGetRepoFile_UpstreamErrorIs502(t *testing.T) {
	srv := repoFileTestServer(t, "group/project", http.StatusInternalServerError)
	defer srv.Close()

	status, _ := getRepoFile(t, srv.URL, "group/project", "README.md")
	if status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 for an upstream GitLab error", status)
	}
}
