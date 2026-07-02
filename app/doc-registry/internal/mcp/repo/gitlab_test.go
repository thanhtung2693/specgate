package repo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestGitLab_Search_URLEncoding(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("X-Next-Page", "")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"path":      "cmd/main.go",
				"data":      "func main() {\n\tfmt.Println(\"hello\")\n}",
				"startline": 1,
			},
		})
	}))
	t.Cleanup(srv.Close)

	provider := NewGitLabProvider(GitLabConfig{
		APIURL:     srv.URL,
		Token:      "test-token",
		ProjectID:  "acme/projects/ds-order-api",
		DefaultRef: "main",
	})

	_, err := provider.Search(context.Background(), "main", SearchOpts{MaxResults: 5})
	if err != nil {
		t.Fatal(err)
	}

	// Server sees decoded path; project ID segments remain as path elements.
	wantPath := "/projects/acme/projects/ds-order-api/search"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
}

func TestGitLab_Search_AuthHeader(t *testing.T) {
	t.Parallel()
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		w.Header().Set("X-Next-Page", "")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	t.Cleanup(srv.Close)

	provider := NewGitLabProvider(GitLabConfig{
		APIURL:     srv.URL,
		Token:      "glpat-secret",
		ProjectID:  "grp/proj",
		DefaultRef: "main",
	})

	_, err := provider.Search(context.Background(), "test", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}

	if gotToken != "glpat-secret" {
		t.Errorf("PRIVATE-TOKEN = %q, want %q", gotToken, "glpat-secret")
	}
}

// An OAuth integration (Bearer:true) must send the token as Authorization:
// Bearer, not PRIVATE-TOKEN — GitLab rejects OAuth tokens on PRIVATE-TOKEN (401).
func TestGitLab_Search_OAuthUsesBearer(t *testing.T) {
	t.Parallel()
	var gotAuth, gotPrivate string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPrivate = r.Header.Get("PRIVATE-TOKEN")
		w.Header().Set("X-Next-Page", "")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	t.Cleanup(srv.Close)

	provider := NewGitLabProvider(GitLabConfig{
		APIURL:     srv.URL,
		Token:      "gho-oauth",
		ProjectID:  "grp/proj",
		DefaultRef: "main",
		Bearer:     true,
	})

	if _, err := provider.Search(context.Background(), "test", SearchOpts{}); err != nil {
		t.Fatal(err)
	}

	if gotAuth != "Bearer gho-oauth" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer gho-oauth")
	}
	if gotPrivate != "" {
		t.Errorf("PRIVATE-TOKEN = %q, want empty for OAuth", gotPrivate)
	}
}

func TestGitLab_Search_PostFilters(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Next-Page", "")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"path": "internal/api/handlers.go", "data": "func Handle()", "startline": 10},
			{"path": "internal/mcp/server.go", "data": "func Serve()", "startline": 5},
			{"path": "cmd/main.go", "data": "func main()", "startline": 1},
		})
	}))
	t.Cleanup(srv.Close)

	provider := NewGitLabProvider(GitLabConfig{
		APIURL:     srv.URL,
		Token:      "tok",
		ProjectID:  "grp/proj",
		DefaultRef: "main",
	})

	result, err := provider.Search(context.Background(), "func", SearchOpts{
		Paths:      []string{"internal/mcp/"},
		MaxResults: 20,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(result.Items))
	}
	if result.Items[0].Path != "internal/mcp/server.go" {
		t.Errorf("path = %q", result.Items[0].Path)
	}
}

func TestGitLab_ListFiles(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Next-Page", "2")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "main.go", "type": "blob", "path": "cmd/main.go"},
			{"name": "internal", "type": "tree", "path": "internal"},
		})
	}))
	t.Cleanup(srv.Close)

	provider := NewGitLabProvider(GitLabConfig{
		APIURL:     srv.URL,
		Token:      "tok",
		ProjectID:  "grp/proj",
		DefaultRef: "main",
	})

	result, err := provider.ListFiles(context.Background(), "", "main", ListOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(result.Items))
	}
	if result.NextPage == nil || *result.NextPage != 2 {
		t.Errorf("next_page = %v, want 2", result.NextPage)
	}
}

func TestGitLab_GetFileMeta(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"blob_id":   "abc123",
			"size":      1024,
			"file_name": "main.go",
			"file_path": "cmd/main.go",
		})
	}))
	t.Cleanup(srv.Close)

	provider := NewGitLabProvider(GitLabConfig{
		APIURL:     srv.URL,
		Token:      "tok",
		ProjectID:  "grp/proj",
		DefaultRef: "main",
	})

	meta, err := provider.GetFileMeta(context.Background(), "cmd/main.go", "main")
	if err != nil {
		t.Fatal(err)
	}
	if meta.BlobSHA != "abc123" {
		t.Errorf("BlobSHA = %q", meta.BlobSHA)
	}
	if meta.Size != 1024 {
		t.Errorf("Size = %d", meta.Size)
	}
}

func TestGitLab_GetFileContent(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/raw") {
			_, _ = fmt.Fprint(w, "package main\n\nfunc main() {}\n")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"blob_id":   "sha256abc",
			"size":      30,
			"file_name": "main.go",
			"file_path": "cmd/main.go",
		})
	}))
	t.Cleanup(srv.Close)

	provider := NewGitLabProvider(GitLabConfig{
		APIURL:     srv.URL,
		Token:      "tok",
		ProjectID:  "grp/proj",
		DefaultRef: "main",
	})

	fc, err := provider.GetFileContent(context.Background(), "cmd/main.go", "main", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(fc.Content) == 0 {
		t.Error("expected content")
	}
	if fc.Size != 30 {
		t.Errorf("Size = %d", fc.Size)
	}
}

func TestGitLab_GetFileContent_UsesMetadataContentWithoutRawRequest(t *testing.T) {
	t.Parallel()
	content := []byte("package main\n\nfunc main() {}\n")
	rawCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/raw") {
			rawCalls++
			http.Error(w, "raw should not be called", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"blob_id":   "sha256abc",
			"size":      len(content),
			"file_name": "main.go",
			"file_path": "cmd/main.go",
			"encoding":  "base64",
			"content":   base64.StdEncoding.EncodeToString(content),
		})
	}))
	t.Cleanup(srv.Close)

	provider := NewGitLabProvider(GitLabConfig{
		APIURL:     srv.URL,
		Token:      "tok",
		ProjectID:  "grp/proj",
		DefaultRef: "main",
	})

	fc, err := provider.GetFileContent(context.Background(), "cmd/main.go", "main", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if string(fc.Content) != string(content) {
		t.Fatalf("content = %q, want %q", fc.Content, content)
	}
	if rawCalls != 0 {
		t.Fatalf("raw endpoint called %d time(s), want 0", rawCalls)
	}
}

func TestGitLab_GetFileContent_RevalidatesMutableRefBeforeContentCache(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	metadataCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/raw") {
			http.Error(w, "raw should not be called", http.StatusInternalServerError)
			return
		}

		mu.Lock()
		metadataCalls++
		call := metadataCalls
		mu.Unlock()

		content := []byte("old repo\n")
		blobID := "blob-old"
		revisionID := "1111111111111111111111111111111111111111"
		if call > 1 {
			content = []byte("new repo\n")
			blobID = "blob-new"
			revisionID = "2222222222222222222222222222222222222222"
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"blob_id":        blobID,
			"last_commit_id": revisionID,
			"size":           len(content),
			"file_name":      "README.md",
			"file_path":      "README.md",
			"encoding":       "base64",
			"content":        base64.StdEncoding.EncodeToString(content),
		})
	}))
	t.Cleanup(srv.Close)

	provider := NewGitLabProvider(GitLabConfig{
		APIURL:     srv.URL,
		Token:      "tok",
		ProjectID:  "grp/proj",
		DefaultRef: "main",
	})

	first, err := provider.GetFileContent(context.Background(), "README.md", "main", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.GetFileContent(context.Background(), "README.md", "main", 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	if string(first.Content) != "old repo\n" {
		t.Fatalf("first content = %q, want old repo", first.Content)
	}
	if string(second.Content) != "new repo\n" {
		t.Fatalf("second content = %q, want new repo", second.Content)
	}
	if metadataCalls != 2 {
		t.Fatalf("metadata calls = %d, want 2", metadataCalls)
	}
}

func TestGitLab_GetFileContent_CachesExplicitRevisionMetadata(t *testing.T) {
	t.Parallel()
	content := []byte("stable repo\n")
	revisionID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	lastCommitID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	var mu sync.Mutex
	metadataCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/raw") {
			http.Error(w, "raw should not be called", http.StatusInternalServerError)
			return
		}

		mu.Lock()
		metadataCalls++
		mu.Unlock()

		_ = json.NewEncoder(w).Encode(map[string]any{
			"blob_id":        "blob-stable",
			"last_commit_id": lastCommitID,
			"size":           len(content),
			"file_name":      "README.md",
			"file_path":      "README.md",
			"encoding":       "base64",
			"content":        base64.StdEncoding.EncodeToString(content),
		})
	}))
	t.Cleanup(srv.Close)

	provider := NewGitLabProvider(GitLabConfig{
		APIURL:     srv.URL,
		Token:      "tok",
		ProjectID:  "grp/proj",
		DefaultRef: "main",
	})

	for range 2 {
		fc, err := provider.GetFileContent(context.Background(), "README.md", revisionID, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		if string(fc.Content) != string(content) {
			t.Fatalf("content = %q, want %q", fc.Content, content)
		}
	}
	if metadataCalls != 1 {
		t.Fatalf("metadata calls = %d, want 1", metadataCalls)
	}
}

func TestGitLab_GetFileContent_TooLarge(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"blob_id":   "sha256abc",
			"size":      2 * 1024 * 1024,
			"file_name": "big.go",
			"file_path": "big.go",
		})
	}))
	t.Cleanup(srv.Close)

	provider := NewGitLabProvider(GitLabConfig{
		APIURL:     srv.URL,
		Token:      "tok",
		ProjectID:  "grp/proj",
		DefaultRef: "main",
	})

	_, err := provider.GetFileContent(context.Background(), "big.go", "main", 1<<20)
	if err == nil {
		t.Fatal("expected error for file too large")
	}
}
