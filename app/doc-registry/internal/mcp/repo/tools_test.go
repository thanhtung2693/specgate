package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func singleRepoTools(mp RepoProvider) *ToolHandlers {
	return NewToolHandlers(map[string]RepoProvider{"default": mp}, map[string]string{"default": "main"})
}

type mockProvider struct {
	searchFn     func(ctx context.Context, query string, opts SearchOpts) (*SearchResult, error)
	listFilesFn  func(ctx context.Context, path, ref string, opts ListOpts) (*ListResult, error)
	getContentFn func(ctx context.Context, path, ref string, maxBytes int64) (*FileContent, error)
	getMetaFn    func(ctx context.Context, path, ref string) (*FileMeta, error)
}

func (m *mockProvider) Search(ctx context.Context, query string, opts SearchOpts) (*SearchResult, error) {
	return m.searchFn(ctx, query, opts)
}
func (m *mockProvider) ListFiles(ctx context.Context, path, ref string, opts ListOpts) (*ListResult, error) {
	return m.listFilesFn(ctx, path, ref, opts)
}
func (m *mockProvider) GetFileContent(ctx context.Context, path, ref string, maxBytes int64) (*FileContent, error) {
	return m.getContentFn(ctx, path, ref, maxBytes)
}
func (m *mockProvider) GetFileMeta(ctx context.Context, path, ref string) (*FileMeta, error) {
	return m.getMetaFn(ctx, path, ref)
}

func TestRepoSearchTool(t *testing.T) {
	t.Parallel()
	line := 10
	mp := &mockProvider{
		searchFn: func(_ context.Context, query string, opts SearchOpts) (*SearchResult, error) {
			_ = query
			_ = opts
			return &SearchResult{
				Items: []SearchItem{
					{Path: "cmd/main.go", Line: &line, Snippet: "func main()", MatchType: "content"},
				},
				Truncated: false,
			}, nil
		},
	}

	tools := singleRepoTools(mp)
	result, err := tools.RepoSearch(context.Background(), RepoSearchInput{
		Query: "main",
	})
	if err != nil {
		t.Fatal(err)
	}

	var output SearchResult
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Items) != 1 {
		t.Fatalf("got %d items", len(output.Items))
	}
	if output.Items[0].Path != "cmd/main.go" {
		t.Errorf("path = %q", output.Items[0].Path)
	}
}

func TestRepoContextPackCachesResult(t *testing.T) {
	t.Parallel()
	line := 12
	searchCalls := 0
	mp := &mockProvider{
		searchFn: func(_ context.Context, query string, opts SearchOpts) (*SearchResult, error) {
			searchCalls++
			if query != "checkout" {
				t.Errorf("query = %q", query)
			}
			if opts.Ref != "main" {
				t.Errorf("ref = %q", opts.Ref)
			}
			return &SearchResult{
				Items: []SearchItem{
					{Path: "internal/checkout/service.go", Line: &line, Snippet: "Checkout", MatchType: "content"},
				},
			}, nil
		},
		getContentFn: func(_ context.Context, path, ref string, maxBytes int64) (*FileContent, error) {
			if path != "README.md" || ref != "main" {
				t.Errorf("path/ref = %s/%s", path, ref)
			}
			if maxBytes != maxContextPackRead {
				t.Errorf("maxBytes = %d", maxBytes)
			}
			return &FileContent{Path: path, Content: []byte("# Repo\nCheckout docs"), Size: 20}, nil
		},
	}

	tools := singleRepoTools(mp)
	first, err := tools.RepoContextPack(context.Background(), RepoContextPackInput{Query: "checkout"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := tools.RepoContextPack(context.Background(), RepoContextPackInput{Query: "checkout"})
	if err != nil {
		t.Fatal(err)
	}
	if searchCalls != 1 {
		t.Fatalf("search calls = %d, want 1", searchCalls)
	}
	var firstOut, secondOut struct {
		Cached bool     `json:"cached"`
		Files  []string `json:"files"`
		Readme struct {
			Path string `json:"path"`
		} `json:"readme"`
	}
	if err := json.Unmarshal([]byte(first), &firstOut); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(second), &secondOut); err != nil {
		t.Fatal(err)
	}
	if firstOut.Cached {
		t.Fatal("first response should not be cached")
	}
	if !secondOut.Cached {
		t.Fatal("second response should be cached")
	}
	if len(firstOut.Files) != 1 || firstOut.Files[0] != "internal/checkout/service.go" {
		t.Fatalf("files = %+v", firstOut.Files)
	}
	if firstOut.Readme.Path != "README.md" {
		t.Fatalf("readme path = %q", firstOut.Readme.Path)
	}
}

func TestRepoListFilesTool(t *testing.T) {
	t.Parallel()
	mp := &mockProvider{
		listFilesFn: func(_ context.Context, path, ref string, opts ListOpts) (*ListResult, error) {
			_ = path
			_ = ref
			_ = opts
			return &ListResult{
				Items: []ListItem{
					{Name: "main.go", Type: "blob", Path: "cmd/main.go"},
				},
				Truncated: false,
			}, nil
		},
	}

	tools := singleRepoTools(mp)
	result, err := tools.RepoListFiles(context.Background(), RepoListFilesInput{})
	if err != nil {
		t.Fatal(err)
	}

	var output ListResult
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Items) != 1 {
		t.Fatalf("got %d items", len(output.Items))
	}
}

func TestRepoListFilesTool_InvalidRevisionOrPathReturnsSoftError(t *testing.T) {
	t.Parallel()
	mp := &mockProvider{
		listFilesFn: func(_ context.Context, path, ref string, opts ListOpts) (*ListResult, error) {
			_ = path
			_ = ref
			_ = opts
			return nil, fmt.Errorf(`gitlab list files: status 404: {"message":"404 invalid revision or path Not Found"}`)
		},
	}

	tools := singleRepoTools(mp)
	result, err := tools.RepoListFiles(context.Background(), RepoListFilesInput{
		Path: "agent",
		Ref:  "feature/unknown",
	})
	if err != nil {
		t.Fatal(err)
	}

	var out struct {
		Path  string `json:"path"`
		Ref   string `json:"ref"`
		Error string `json:"error"`
		Hint  string `json:"hint"`
	}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatal(err)
	}
	if out.Path != "agent" {
		t.Fatalf("path = %q", out.Path)
	}
	if out.Ref != "feature/unknown" {
		t.Fatalf("ref = %q", out.Ref)
	}
	if out.Error == "" || out.Hint == "" {
		t.Fatalf("expected soft error with hint, got %#v", out)
	}
}

func TestRepoListSymbolsTool(t *testing.T) {
	t.Parallel()
	goSrc := []byte(`package main

func hello() {}

type Config struct{}
`)
	mp := &mockProvider{
		getContentFn: func(_ context.Context, path, ref string, maxBytes int64) (*FileContent, error) {
			_ = ref
			_ = maxBytes
			return &FileContent{
				Path:    path,
				Content: goSrc,
				Size:    int64(len(goSrc)),
				BlobSHA: "abc",
			}, nil
		},
	}

	tools := singleRepoTools(mp)
	result, err := tools.RepoListSymbols(context.Background(), RepoListSymbolsInput{
		Path: "main.go",
	})
	if err != nil {
		t.Fatal(err)
	}

	var output struct {
		Path      string   `json:"path"`
		Language  string   `json:"language"`
		Symbols   []Symbol `json:"symbols"`
		Truncated bool     `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatal(err)
	}
	if output.Language != "go" {
		t.Errorf("language = %q", output.Language)
	}
	if len(output.Symbols) != 2 {
		t.Errorf("got %d symbols, want 2", len(output.Symbols))
	}
}

func TestRepoListSymbolsTool_FileNotFound(t *testing.T) {
	t.Parallel()
	mp := &mockProvider{
		getContentFn: func(_ context.Context, path, ref string, maxBytes int64) (*FileContent, error) {
			_ = ref
			_ = maxBytes
			return nil, fmt.Errorf("file not found: %s", path)
		},
	}
	tools := singleRepoTools(mp)
	result, err := tools.RepoListSymbols(context.Background(), RepoListSymbolsInput{Path: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Path  string `json:"path"`
		Error string `json:"error"`
		Hint  string `json:"hint"`
	}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatal(err)
	}
	if out.Path != "agent" {
		t.Fatalf("path = %q", out.Path)
	}
	if out.Error == "" || out.Hint == "" {
		t.Fatalf("expected soft error with hint, got %#v", out)
	}
}

func TestRepoGetSymbolTool(t *testing.T) {
	t.Parallel()
	goSrc := []byte(`package main

func hello() {
	println("hello world")
}
`)
	mp := &mockProvider{
		getContentFn: func(_ context.Context, path, ref string, maxBytes int64) (*FileContent, error) {
			_ = ref
			_ = maxBytes
			return &FileContent{
				Path:    path,
				Content: goSrc,
				Size:    int64(len(goSrc)),
				BlobSHA: "abc",
			}, nil
		},
	}

	tools := singleRepoTools(mp)
	result, err := tools.RepoGetSymbol(context.Background(), RepoGetSymbolInput{
		Path:   "main.go",
		Symbol: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}

	var output struct {
		Path      string `json:"path"`
		Symbol    string `json:"symbol"`
		Kind      string `json:"kind"`
		Content   string `json:"content"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatal(err)
	}
	if output.Kind != "function" {
		t.Errorf("kind = %q", output.Kind)
	}
	if output.Content == "" {
		t.Error("expected content")
	}
}

func TestRepoGetSymbolTool_FileNotFound(t *testing.T) {
	t.Parallel()
	mp := &mockProvider{
		getContentFn: func(_ context.Context, path, ref string, maxBytes int64) (*FileContent, error) {
			_ = ref
			_ = maxBytes
			return nil, fmt.Errorf("file not found: %s", path)
		},
	}
	tools := singleRepoTools(mp)
	result, err := tools.RepoGetSymbol(context.Background(), RepoGetSymbolInput{
		Path:   "agent",
		Symbol: "Foo",
	})
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Path  string `json:"path"`
		Error string `json:"error"`
		Hint  string `json:"hint"`
	}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatal(err)
	}
	if out.Path != "agent" {
		t.Fatalf("path = %q", out.Path)
	}
	if out.Error == "" || out.Hint == "" {
		t.Fatalf("expected soft error with hint, got %#v", out)
	}
}

func TestRepoGetSnippetTool(t *testing.T) {
	t.Parallel()
	content := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n"
	mp := &mockProvider{
		getContentFn: func(_ context.Context, path, ref string, maxBytes int64) (*FileContent, error) {
			_ = ref
			_ = maxBytes
			return &FileContent{
				Path:    path,
				Content: []byte(content),
				Size:    int64(len(content)),
				BlobSHA: "abc",
			}, nil
		},
	}

	tools := singleRepoTools(mp)
	result, err := tools.RepoGetSnippet(context.Background(), RepoGetSnippetInput{
		Path:   "main.go",
		Line:   5,
		Before: 2,
		After:  2,
	})
	if err != nil {
		t.Fatal(err)
	}

	var output struct {
		Path      string `json:"path"`
		Content   string `json:"content"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
		FileSize  int64  `json:"file_size"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatal(err)
	}
	if output.StartLine != 3 {
		t.Errorf("start_line = %d, want 3", output.StartLine)
	}
	if output.EndLine != 7 {
		t.Errorf("end_line = %d, want 7", output.EndLine)
	}
}

func TestRepoGetSnippetTool_LineBeyondFile(t *testing.T) {
	t.Parallel()
	content := "a\nb\nc"
	mp := &mockProvider{
		getContentFn: func(_ context.Context, path, ref string, maxBytes int64) (*FileContent, error) {
			_ = path
			_ = ref
			_ = maxBytes
			return &FileContent{
				Path:    "f.go",
				Content: []byte(content),
				Size:    int64(len(content)),
			}, nil
		},
	}
	tools := singleRepoTools(mp)
	// Line 240 with a 3-line file used to panic: slice [229:3].
	result, err := tools.RepoGetSnippet(context.Background(), RepoGetSnippetInput{
		Path:   "f.go",
		Line:   240,
		Before: 10,
		After:  10,
	})
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Content   string `json:"content"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatal(err)
	}
	if output.StartLine != 1 || output.EndLine != 3 {
		t.Fatalf("start_line=%d end_line=%d, want 1 and 3", output.StartLine, output.EndLine)
	}
}

func TestRepoRelatedTestsTool(t *testing.T) {
	t.Parallel()
	mp := &mockProvider{
		getMetaFn: func(_ context.Context, path, ref string) (*FileMeta, error) {
			_ = ref
			if path == "server_test.go" {
				return &FileMeta{Path: path, Size: 100, BlobSHA: "x"}, nil
			}
			return nil, fmt.Errorf("file not found: %s", path)
		},
	}

	tools := singleRepoTools(mp)
	result, err := tools.RepoRelatedTests(context.Background(), RepoRelatedTestsInput{
		Path: "server.go",
	})
	if err != nil {
		t.Fatal(err)
	}

	var output struct {
		SourcePath string `json:"source_path"`
		TestFiles  []struct {
			Path   string `json:"path"`
			Exists bool   `json:"exists"`
		} `json:"test_files"`
	}
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatal(err)
	}
	if len(output.TestFiles) == 0 {
		t.Fatal("expected test files")
	}
	if !output.TestFiles[0].Exists {
		t.Error("server_test.go should exist")
	}
}

func TestRepoReadFileTool_Allowed(t *testing.T) {
	t.Parallel()
	content := "# README\n\nThis is a readme."
	var gotMaxBytes int64
	mp := &mockProvider{
		getContentFn: func(_ context.Context, path, ref string, maxBytes int64) (*FileContent, error) {
			_ = ref
			gotMaxBytes = maxBytes
			return &FileContent{
				Path:    path,
				Content: []byte(content),
				Size:    int64(len(content)),
				BlobSHA: "abc",
			}, nil
		},
	}

	tools := singleRepoTools(mp)
	result, err := tools.RepoReadFile(context.Background(), RepoReadFileInput{
		Path: "README.md",
	})
	if err != nil {
		t.Fatal(err)
	}

	var output struct {
		Path      string `json:"path"`
		Content   string `json:"content"`
		Size      int64  `json:"size"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatal(err)
	}
	if output.Content != content {
		t.Errorf("content = %q", output.Content)
	}
	if gotMaxBytes <= maxReadFileBytes {
		t.Errorf("fetch limit = %d, want greater than display limit %d", gotMaxBytes, maxReadFileBytes)
	}
}

func TestRepoReadFileTool_TruncatesLargeAllowedFile(t *testing.T) {
	t.Parallel()
	content := strings.Repeat("a", maxReadFileBytes+128)
	mp := &mockProvider{
		getContentFn: func(_ context.Context, path, ref string, maxBytes int64) (*FileContent, error) {
			_ = ref
			if maxBytes <= maxReadFileBytes {
				return nil, fmt.Errorf("file too large (%d bytes, limit %d)", len(content), maxBytes)
			}
			return &FileContent{
				Path:    path,
				Content: []byte(content),
				Size:    int64(len(content)),
				BlobSHA: "abc",
			}, nil
		},
	}

	tools := singleRepoTools(mp)
	result, err := tools.RepoReadFile(context.Background(), RepoReadFileInput{
		Path: "docs/spec.md",
	})
	if err != nil {
		t.Fatal(err)
	}

	var output struct {
		Content   string `json:"content"`
		Size      int64  `json:"size"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatal(err)
	}
	if !output.Truncated {
		t.Fatal("expected truncated output")
	}
	if len(output.Content) > maxReadFileBytes {
		t.Fatalf("content length = %d, want <= %d", len(output.Content), maxReadFileBytes)
	}
	if output.Size != int64(len(content)) {
		t.Fatalf("size = %d, want %d", output.Size, len(content))
	}
}

func TestRepoReadFileTool_NotFoundReturnsSoftError(t *testing.T) {
	t.Parallel()
	mp := &mockProvider{
		getContentFn: func(_ context.Context, path, ref string, maxBytes int64) (*FileContent, error) {
			_ = ref
			_ = maxBytes
			return nil, fmt.Errorf("file not found: %s", path)
		},
	}

	tools := singleRepoTools(mp)
	result, err := tools.RepoReadFile(context.Background(), RepoReadFileInput{
		Path: "REPO_MAP.md",
	})
	if err != nil {
		t.Fatal(err)
	}

	var output struct {
		Path  string `json:"path"`
		Error string `json:"error"`
		Hint  string `json:"hint"`
	}
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatal(err)
	}
	if output.Path != "REPO_MAP.md" {
		t.Fatalf("path = %q", output.Path)
	}
	if output.Error == "" || output.Hint == "" {
		t.Fatalf("expected soft error with hint, got %#v", output)
	}
}

func TestRepoReadFileTool_Blocked(t *testing.T) {
	t.Parallel()
	mp := &mockProvider{}
	tools := singleRepoTools(mp)

	_, err := tools.RepoReadFile(context.Background(), RepoReadFileInput{
		Path: "internal/api/handlers.go",
	})
	if err == nil {
		t.Fatal("expected error for blocked file")
	}
}

func TestRepoReadFileTool_PackageJsonInSubdirAllowed(t *testing.T) {
	t.Parallel()
	want := []byte(`{"name": "ui"}`)
	mp := &mockProvider{
		getContentFn: func(_ context.Context, path, ref string, maxBytes int64) (*FileContent, error) {
			_ = ref
			_ = maxBytes
			if path != "ui/package.json" {
				t.Fatalf("path = %q", path)
			}
			return &FileContent{
				Path:    path,
				Content: want,
				Size:    int64(len(want)),
				BlobSHA: "abc",
			}, nil
		},
	}
	tools := singleRepoTools(mp)
	result, err := tools.RepoReadFile(context.Background(), RepoReadFileInput{
		Path: "ui/package.json",
		Ref:  "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatal(err)
	}
	if output.Content != string(want) {
		t.Fatalf("content = %q", output.Content)
	}
}

func TestEmptyPathReturnsSoftError(t *testing.T) {
	t.Parallel()
	mp := &mockProvider{}
	tools := singleRepoTools(mp)

	tests := []struct {
		name string
		call func() (string, error)
	}{
		{"ListSymbols", func() (string, error) {
			return tools.RepoListSymbols(context.Background(), RepoListSymbolsInput{Path: ""})
		}},
		{"GetSymbol", func() (string, error) {
			return tools.RepoGetSymbol(context.Background(), RepoGetSymbolInput{Path: "", Symbol: "Foo"})
		}},
		{"GetSnippet", func() (string, error) {
			return tools.RepoGetSnippet(context.Background(), RepoGetSnippetInput{Path: "", Line: 1})
		}},
		{"ReadFile", func() (string, error) {
			return tools.RepoReadFile(context.Background(), RepoReadFileInput{Path: ""})
		}},
		{"RelatedTests", func() (string, error) {
			return tools.RepoRelatedTests(context.Background(), RepoRelatedTestsInput{Path: ""})
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := tt.call()
			if err != nil {
				t.Fatalf("expected soft error (no hard error), got: %v", err)
			}
			var out map[string]any
			if err := json.Unmarshal([]byte(result), &out); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			errMsg, ok := out["error"].(string)
			if !ok || errMsg == "" {
				t.Fatalf("expected error field in response, got: %v", out)
			}
			if !strings.Contains(errMsg, "path is required") {
				t.Errorf("error = %q, want 'path is required'", errMsg)
			}
		})
	}
}

func TestRepoSearchTool_UsesRequestedRepo(t *testing.T) {
	t.Parallel()
	calls := make(map[string]int)
	fe := &mockProvider{
		searchFn: func(_ context.Context, _ string, opts SearchOpts) (*SearchResult, error) {
			calls["123"]++
			if opts.Ref != "fe-main" {
				t.Fatalf("fe ref = %q, want fe-main", opts.Ref)
			}
			return &SearchResult{}, nil
		},
	}
	be := &mockProvider{
		searchFn: func(_ context.Context, _ string, opts SearchOpts) (*SearchResult, error) {
			calls["456"]++
			if opts.Ref != "feature-main" {
				t.Fatalf("be ref = %q, want feature-main", opts.Ref)
			}
			return &SearchResult{}, nil
		},
	}
	tools := NewToolHandlers(
		map[string]RepoProvider{"123": fe, "456": be},
		map[string]string{"123": "fe-main", "456": "feature-main"},
	)
	if _, err := tools.RepoSearch(context.Background(), RepoSearchInput{
		Repo:  "456",
		Query: "main",
	}); err != nil {
		t.Fatal(err)
	}
	if calls["456"] != 1 || calls["123"] != 0 {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestRepoSearchTool_RequiresRepoWhenMultiRepo(t *testing.T) {
	t.Parallel()
	tools := NewToolHandlers(
		map[string]RepoProvider{
			"123": &mockProvider{searchFn: func(_ context.Context, _ string, _ SearchOpts) (*SearchResult, error) {
				return &SearchResult{}, nil
			}},
			"456": &mockProvider{searchFn: func(_ context.Context, _ string, _ SearchOpts) (*SearchResult, error) {
				return &SearchResult{}, nil
			}},
		},
		map[string]string{"123": "main", "456": "main"},
	)
	out, err := tools.RepoSearch(context.Background(), RepoSearchInput{Query: "main"})
	if err != nil {
		t.Fatalf("expected nil error, got %v (out=%s)", err, out)
	}
	if !strings.Contains(out, `"error":"repo required"`) || !strings.Contains(out, `"available":["123","456"]`) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRepoSearchTool_UnknownRepo(t *testing.T) {
	t.Parallel()
	tools := NewToolHandlers(
		map[string]RepoProvider{
			"123": &mockProvider{searchFn: func(_ context.Context, _ string, _ SearchOpts) (*SearchResult, error) {
				return &SearchResult{}, nil
			}},
		},
		map[string]string{"123": "main"},
	)
	out, err := tools.RepoSearch(context.Background(), RepoSearchInput{Repo: "456", Query: "main"})
	if err != nil {
		t.Fatalf("expected nil error, got %v (out=%s)", err, out)
	}
	if !strings.Contains(out, `"error":"unknown repo: 456"`) || !strings.Contains(out, `"available":["123"]`) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRepoSearchTool_RefFallbackOrder(t *testing.T) {
	t.Parallel()
	var refs []string
	mp := &mockProvider{
		searchFn: func(_ context.Context, _ string, opts SearchOpts) (*SearchResult, error) {
			refs = append(refs, opts.Ref)
			return &SearchResult{}, nil
		},
	}
	// Per-spec §7.1, ref order is caller arg > per-repo default_ref. No global fallback.
	tools := NewToolHandlers(
		map[string]RepoProvider{"fe": mp},
		map[string]string{"fe": "repo-main"},
	)
	if _, err := tools.RepoSearch(context.Background(), RepoSearchInput{Repo: "fe", Query: "main"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tools.RepoSearch(context.Background(), RepoSearchInput{Repo: "fe", Query: "main", Ref: "explicit"}); err != nil {
		t.Fatal(err)
	}
	tools2 := NewToolHandlers(
		map[string]RepoProvider{"fe": mp},
		map[string]string{},
	)
	if _, err := tools2.RepoSearch(context.Background(), RepoSearchInput{Repo: "fe", Query: "main"}); err != nil {
		t.Fatal(err)
	}
	if len(refs) != 3 {
		t.Fatalf("refs = %+v", refs)
	}
	if refs[0] != "repo-main" {
		t.Fatalf("first ref = %q (per-repo)", refs[0])
	}
	if refs[1] != "explicit" {
		t.Fatalf("second ref = %q (caller arg)", refs[1])
	}
	if refs[2] != "" {
		t.Fatalf("third ref = %q (no per-repo, no global → empty)", refs[2])
	}
}
