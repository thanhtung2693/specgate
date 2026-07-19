package repo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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

func TestGitLab_GetFileContent_RejectsInlineContentLargerThanDeclared(t *testing.T) {
	t.Parallel()
	content := []byte(strings.Repeat("x", 11))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"blob_id":  "sha256abc",
			"size":     1,
			"encoding": "base64",
			"content":  base64.StdEncoding.EncodeToString(content),
		})
	}))
	t.Cleanup(srv.Close)

	provider := NewGitLabProvider(GitLabConfig{APIURL: srv.URL, ProjectID: "grp/proj"})
	if _, err := provider.GetFileContent(context.Background(), "big.go", "main", 10); err == nil {
		t.Fatal("expected error for oversized decoded inline content")
	}
}

func TestGitLab_GetFileContent_RejectsRawContentLargerThanDeclared(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/raw") {
			_, _ = fmt.Fprint(w, strings.Repeat("x", 11))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"blob_id": "sha256abc", "size": 1})
	}))
	t.Cleanup(srv.Close)

	provider := NewGitLabProvider(GitLabConfig{APIURL: srv.URL, ProjectID: "grp/proj"})
	if _, err := provider.GetFileContent(context.Background(), "big.go", "main", 10); err == nil {
		t.Fatal("expected error for oversized raw content")
	}
}
