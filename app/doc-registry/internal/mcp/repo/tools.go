package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	maxReadFileBytes   = 16 * 1024
	maxSnippetBytes    = 8 * 1024
	maxSnippetLines    = 100
	maxSymbolBodyBytes = 16 * 1024
	maxParseBytes      = 1 * 1024 * 1024
	maxSnippetFetch    = 10 * 1024 * 1024
	maxContextPackRead = 4 * 1024
	contextPackTTL     = 10 * time.Minute
)

// ToolHandlers holds the repo tool handler functions.
type ToolHandlers struct {
	providers   map[string]RepoProvider
	defaultRepo string
	defaultRefs map[string]string
	cacheMu     sync.Mutex
	packCache   map[string]contextPackCacheEntry
}

type contextPackCacheEntry struct {
	expiresAt time.Time
	value     string
}

// NewToolHandlers creates handlers for the 7 repo_* tools.
//
// providers maps repo key -> RepoProvider (one entry = single-repo mode, N entries = multi).
// For gitlab rows the repo key is config.project_id.
// defaultRefs maps the same repo key -> per-repo default branch; ref resolution falls back to
// this when the caller's `ref` arg is empty. There is no global ref fallback —
// per-row default_ref carries the responsibility (per spec §7.1).
func NewToolHandlers(providers map[string]RepoProvider, defaultRefs map[string]string) *ToolHandlers {
	out := &ToolHandlers{
		providers:   make(map[string]RepoProvider, len(providers)),
		defaultRefs: make(map[string]string, len(defaultRefs)),
		packCache:   make(map[string]contextPackCacheEntry),
	}
	for repoKey, provider := range providers {
		repoKey = strings.TrimSpace(repoKey)
		if repoKey == "" || provider == nil {
			continue
		}
		out.providers[repoKey] = provider
	}
	for repoKey, ref := range defaultRefs {
		repoKey = strings.TrimSpace(repoKey)
		if repoKey == "" {
			continue
		}
		out.defaultRefs[repoKey] = strings.TrimSpace(ref)
	}
	if len(out.providers) == 1 {
		for repoKey := range out.providers {
			out.defaultRepo = repoKey
		}
	}
	return out
}

func (h *ToolHandlers) availableRepos() []string {
	repos := make([]string, 0, len(h.providers))
	for repoKey := range h.providers {
		repos = append(repos, repoKey)
	}
	sort.Strings(repos)
	return repos
}

// resolveRepo returns the provider + canonical repo key for a tool call. When the
// caller's repo arg is empty/unknown and dispatch can't be unambiguously resolved,
// returns errJSON containing a structured tool result; the handler returns
// (errJSON, nil) so the LLM sees a parseable retry hint (per spec §7.3).
func (h *ToolHandlers) resolveRepo(repo string) (provider RepoProvider, canonicalRepo string, errJSON string) {
	trimmed := strings.TrimSpace(repo)
	if trimmed == "" {
		if len(h.providers) == 1 && h.defaultRepo != "" {
			return h.providers[h.defaultRepo], h.defaultRepo, ""
		}
		return nil, "", mustJSON(map[string]any{
			"error":     "repo required",
			"available": h.availableRepos(),
		})
	}
	prov, ok := h.providers[trimmed]
	if !ok {
		return nil, "", mustJSON(map[string]any{
			"error":     fmt.Sprintf("unknown repo: %s", trimmed),
			"available": h.availableRepos(),
		})
	}
	return prov, trimmed, ""
}

// refFor returns the effective ref: caller arg > per-repo default_ref. When both
// are empty the GitLab provider's own DefaultRef applies downstream.
func (h *ToolHandlers) refFor(repoKey, r string) string {
	if trimmed := strings.TrimSpace(r); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(h.defaultRefs[repoKey])
}

type RepoSearchInput struct {
	Repo       string   `json:"repo,omitempty" doc:"Project ID of the gitlab repo to query. Required when multiple gitlab repos are configured."`
	Query      string   `json:"query"`
	Paths      []string `json:"paths,omitempty"`
	Globs      []string `json:"globs,omitempty"`
	Ref        string   `json:"ref,omitempty"`
	MaxResults int      `json:"max_results,omitempty"`
}

type RepoContextPackInput struct {
	Repo       string   `json:"repo,omitempty" doc:"Project ID of the gitlab repo to query. Required when multiple gitlab repos are configured."`
	Query      string   `json:"query"`
	Paths      []string `json:"paths,omitempty"`
	Globs      []string `json:"globs,omitempty"`
	Ref        string   `json:"ref,omitempty"`
	MaxResults int      `json:"max_results,omitempty"`
}

type RepoListFilesInput struct {
	Repo      string `json:"repo,omitempty" doc:"Project ID of the gitlab repo to query. Required when multiple gitlab repos are configured."`
	Path      string `json:"path,omitempty"`
	Ref       string `json:"ref,omitempty"`
	Recursive bool   `json:"recursive,omitempty"`
	Page      int    `json:"page,omitempty"`
}

type RepoListSymbolsInput struct {
	Repo string `json:"repo,omitempty" doc:"Project ID of the gitlab repo to query. Required when multiple gitlab repos are configured."`
	Path string `json:"path"`
	Ref  string `json:"ref,omitempty"`
}

type RepoGetSymbolInput struct {
	Repo   string `json:"repo,omitempty" doc:"Project ID of the gitlab repo to query. Required when multiple gitlab repos are configured."`
	Path   string `json:"path"`
	Symbol string `json:"symbol"`
	Ref    string `json:"ref,omitempty"`
}

type RepoGetSnippetInput struct {
	Repo   string `json:"repo,omitempty" doc:"Project ID of the gitlab repo to query. Required when multiple gitlab repos are configured."`
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Before int    `json:"before,omitempty"`
	After  int    `json:"after,omitempty"`
	Ref    string `json:"ref,omitempty"`
}

type RepoRelatedTestsInput struct {
	Repo string `json:"repo,omitempty" doc:"Project ID of the gitlab repo to query. Required when multiple gitlab repos are configured."`
	Path string `json:"path"`
	Ref  string `json:"ref,omitempty"`
}

type RepoReadFileInput struct {
	Repo string `json:"repo,omitempty" doc:"Project ID of the gitlab repo to query. Required when multiple gitlab repos are configured."`
	Path string `json:"path"`
	Ref  string `json:"ref,omitempty"`
}

// RepoSearch handles the repo_search tool.
func (h *ToolHandlers) RepoSearch(ctx context.Context, in RepoSearchInput) (string, error) {
	provider, repoKey, errJSON := h.resolveRepo(in.Repo)
	if errJSON != "" {
		return errJSON, nil
	}
	maxResults := in.MaxResults
	if maxResults <= 0 {
		maxResults = 20
	}
	result, err := provider.Search(ctx, in.Query, SearchOpts{
		Paths:      in.Paths,
		Globs:      in.Globs,
		Ref:        h.refFor(repoKey, in.Ref),
		MaxResults: maxResults,
	})
	if err != nil {
		return "", err
	}
	return mustJSON(result), nil
}

// RepoContextPack returns a compact, cached repo orientation bundle for one query.
func (h *ToolHandlers) RepoContextPack(ctx context.Context, in RepoContextPackInput) (string, error) {
	provider, repoKey, errJSON := h.resolveRepo(in.Repo)
	if errJSON != "" {
		return errJSON, nil
	}
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return mustJSON(map[string]any{
			"error": "query is required",
		}), nil
	}
	maxResults := in.MaxResults
	if maxResults <= 0 {
		maxResults = 8
	}
	if maxResults > 20 {
		maxResults = 20
	}
	ref := h.refFor(repoKey, in.Ref)
	cacheKey := contextPackCacheKey(repoKey, ref, query, in.Paths, in.Globs, maxResults)
	if cached, ok := h.getContextPackCache(cacheKey); ok {
		return cached, nil
	}

	search, err := provider.Search(ctx, query, SearchOpts{
		Paths:      in.Paths,
		Globs:      in.Globs,
		Ref:        ref,
		MaxResults: maxResults,
	})
	if err != nil {
		return "", err
	}

	readme := h.firstReadableFile(ctx, provider, ref, []string{"README.md", "docs/README.md"})
	files := uniqueSearchPaths(search.Items, 6)
	out := mustJSON(map[string]any{
		"repo":       repoKey,
		"ref":        ref,
		"query":      query,
		"readme":     readme,
		"matches":    search.Items,
		"files":      files,
		"truncated":  search.Truncated,
		"cached":     false,
		"cache_ttl":  int(contextPackTTL.Seconds()),
		"next_steps": []string{"Use repo_list_symbols or repo_get_snippet only for files that matter."},
	})
	h.setContextPackCache(cacheKey, out)
	return out, nil
}

// RepoListFiles handles the repo_list_files tool.
func (h *ToolHandlers) RepoListFiles(ctx context.Context, in RepoListFilesInput) (string, error) {
	provider, repoKey, errJSON := h.resolveRepo(in.Repo)
	if errJSON != "" {
		return errJSON, nil
	}
	result, err := provider.ListFiles(ctx, in.Path, h.refFor(repoKey, in.Ref), ListOpts{
		Recursive: in.Recursive,
		Page:      in.Page,
	})
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "invalid revision or path") || strings.Contains(msg, "status 404") {
			return mustJSON(map[string]any{
				"path":  in.Path,
				"ref":   h.refFor(repoKey, in.Ref),
				"error": "Invalid revision or path for repo_list_files.",
				"hint": "Verify ref exists (e.g. main/master) and path is relative to repo root; " +
					"use empty path to list root first.",
			}), nil
		}
		return "", err
	}
	return mustJSON(result), nil
}

// RepoListSymbols handles the repo_list_symbols tool.
func (h *ToolHandlers) RepoListSymbols(ctx context.Context, in RepoListSymbolsInput) (string, error) {
	provider, repoKey, errJSON := h.resolveRepo(in.Repo)
	if errJSON != "" {
		return errJSON, nil
	}
	if strings.TrimSpace(in.Path) == "" {
		return mustJSON(map[string]any{
			"error": "path is required — provide a file path (e.g. src/main.go). Use repo_list_files to browse.",
		}), nil
	}
	fc, err := provider.GetFileContent(ctx, in.Path, h.refFor(repoKey, in.Ref), maxParseBytes)
	if err != nil {
		if strings.Contains(err.Error(), "file not found") {
			return mustJSON(map[string]any{
				"path":  in.Path,
				"error": fmt.Sprintf("File not found: %s", in.Path),
				"hint":  "Use repo_list_files to find paths; path must be a file in the repo (not a directory or shorthand like \"agent\").",
			}), nil
		}
		if strings.Contains(err.Error(), "file too large") {
			return mustJSON(map[string]any{
				"error": "File too large for symbol parsing (>1MB) — use repo_get_snippet with a line number from repo_search",
			}), nil
		}
		return "", err
	}

	if !utf8.Valid(fc.Content) {
		return mustJSON(map[string]any{
			"error": "Binary file — cannot display content",
		}), nil
	}

	symbols, lang, err := ParseSymbols(in.Path, fc.Content)
	if err != nil {
		return mustJSON(map[string]any{
			"path":      in.Path,
			"language":  lang,
			"symbols":   []Symbol{},
			"truncated": false,
			"error":     fmt.Sprintf("parser error: %v", err),
		}), nil
	}

	truncated := len(symbols) >= MaxSymbolsPerFile

	return mustJSON(map[string]any{
		"path":      in.Path,
		"language":  lang,
		"symbols":   symbols,
		"truncated": truncated,
	}), nil
}

// RepoGetSymbol handles the repo_get_symbol tool.
func (h *ToolHandlers) RepoGetSymbol(ctx context.Context, in RepoGetSymbolInput) (string, error) {
	provider, repoKey, errJSON := h.resolveRepo(in.Repo)
	if errJSON != "" {
		return errJSON, nil
	}
	if strings.TrimSpace(in.Path) == "" {
		return mustJSON(map[string]any{
			"error": "path is required — provide a file path (e.g. src/main.go). Use repo_list_files to browse.",
		}), nil
	}
	fc, err := provider.GetFileContent(ctx, in.Path, h.refFor(repoKey, in.Ref), maxParseBytes)
	if err != nil {
		if strings.Contains(err.Error(), "file not found") {
			return mustJSON(map[string]any{
				"path":  in.Path,
				"error": fmt.Sprintf("File not found: %s", in.Path),
				"hint":  "Use repo_list_files to find paths; path must be a file in the repo (not a directory or shorthand like \"agent\").",
			}), nil
		}
		if strings.Contains(err.Error(), "file too large") {
			return mustJSON(map[string]any{
				"error": "File too large for symbol parsing (>1MB) — use repo_get_snippet with a line number from repo_search",
			}), nil
		}
		return "", err
	}

	if !utf8.Valid(fc.Content) {
		return mustJSON(map[string]any{
			"error": "Binary file — cannot display content",
		}), nil
	}

	body, sym, found := ExtractSymbolBody(in.Path, fc.Content, in.Symbol)
	if !found {
		return mustJSON(map[string]any{
			"error": fmt.Sprintf("symbol %q not found in %s", in.Symbol, in.Path),
		}), nil
	}

	truncated := false
	if len(body) > maxSymbolBodyBytes {
		body = truncateUTF8(body, maxSymbolBodyBytes)
		truncated = true
	}

	return mustJSON(map[string]any{
		"path":       in.Path,
		"symbol":     sym.Name,
		"kind":       sym.Kind,
		"content":    body,
		"start_line": sym.StartLine,
		"end_line":   sym.EndLine,
		"truncated":  truncated,
	}), nil
}

// RepoGetSnippet handles the repo_get_snippet tool.
func (h *ToolHandlers) RepoGetSnippet(ctx context.Context, in RepoGetSnippetInput) (string, error) {
	provider, repoKey, errJSON := h.resolveRepo(in.Repo)
	if errJSON != "" {
		return errJSON, nil
	}
	if strings.TrimSpace(in.Path) == "" {
		return mustJSON(map[string]any{
			"error": "path is required — provide a file path (e.g. src/main.go). Use repo_list_files to browse.",
		}), nil
	}
	before := in.Before
	if before <= 0 {
		before = 10
	}
	after := in.After
	if after <= 0 {
		after = 10
	}
	if before+after > maxSnippetLines {
		before = maxSnippetLines / 2
		after = maxSnippetLines / 2
	}

	fc, err := provider.GetFileContent(ctx, in.Path, h.refFor(repoKey, in.Ref), maxSnippetFetch)
	if err != nil {
		if strings.Contains(err.Error(), "file not found") {
			return mustJSON(map[string]any{
				"path":  in.Path,
				"error": fmt.Sprintf("File not found: %s", in.Path),
				"hint":  "Use repo_list_files or try docs/README.md / docs/system-overview.md.",
			}), nil
		}
		if strings.Contains(err.Error(), "file too large") {
			return mustJSON(map[string]any{
				"error": "File too large (>10MB) — use repo_search to find specific locations",
			}), nil
		}
		return "", err
	}

	if !utf8.Valid(fc.Content) {
		return mustJSON(map[string]any{
			"error": "Binary file — cannot display content",
		}), nil
	}

	lines := strings.Split(string(fc.Content), "\n")
	numLines := len(lines)
	line := in.Line
	if line < 1 {
		line = 1
	}
	if line > numLines {
		line = numLines
	}
	startLine := line - before
	if startLine < 1 {
		startLine = 1
	}
	endLine := line + after
	if endLine > numLines {
		endLine = numLines
	}
	if startLine > endLine {
		startLine = endLine
	}

	snippet := strings.Join(lines[startLine-1:endLine], "\n")
	truncated := false
	if len(snippet) > maxSnippetBytes {
		snippet = truncateUTF8(snippet, maxSnippetBytes)
		truncated = true
	}

	return mustJSON(map[string]any{
		"path":       in.Path,
		"content":    snippet,
		"start_line": startLine,
		"end_line":   endLine,
		"file_size":  fc.Size,
		"truncated":  truncated,
	}), nil
}

// RepoRelatedTests handles the repo_related_tests tool.
func (h *ToolHandlers) RepoRelatedTests(ctx context.Context, in RepoRelatedTestsInput) (string, error) {
	provider, repoKey, errJSON := h.resolveRepo(in.Repo)
	if errJSON != "" {
		return errJSON, nil
	}
	if strings.TrimSpace(in.Path) == "" {
		return mustJSON(map[string]any{
			"error": "path is required — provide a source file path (e.g. internal/api/handlers.go).",
		}), nil
	}
	candidates := testFileCandidates(in.Path)
	ref := h.refFor(repoKey, in.Ref)

	type testFile struct {
		Path   string `json:"path"`
		Exists bool   `json:"exists"`
	}

	results := make([]testFile, 0, len(candidates))
	for _, candidate := range candidates {
		_, err := provider.GetFileMeta(ctx, candidate, ref)
		results = append(results, testFile{
			Path:   candidate,
			Exists: err == nil,
		})
	}

	return mustJSON(map[string]any{
		"source_path": in.Path,
		"test_files":  results,
	}), nil
}

// RepoReadFile handles the repo_read_file tool.
func (h *ToolHandlers) RepoReadFile(ctx context.Context, in RepoReadFileInput) (string, error) {
	provider, repoKey, errJSON := h.resolveRepo(in.Repo)
	if errJSON != "" {
		return errJSON, nil
	}
	path := filepath.Clean(strings.TrimSpace(in.Path))
	if path == "." || path == "" {
		return mustJSON(map[string]any{
			"error": "path is required — provide a file path (e.g. docs/README.md). Use repo_list_files to browse.",
		}), nil
	}
	if !isAllowedReadFile(path) {
		return "", fmt.Errorf("use repo_get_symbol or repo_get_snippet for source code")
	}

	fc, err := provider.GetFileContent(ctx, path, h.refFor(repoKey, in.Ref), maxSnippetFetch)
	if err != nil {
		if strings.Contains(err.Error(), "file not found") {
			return mustJSON(map[string]any{
				"path":  path,
				"error": fmt.Sprintf("File not found: %s", path),
				"hint":  "Use repo_list_files or try docs/README.md / docs/system-overview.md.",
			}), nil
		}
		if strings.Contains(err.Error(), "file too large") {
			return mustJSON(map[string]any{
				"error": "File too large (>10MB) — use repo_search to find specific locations",
			}), nil
		}
		return "", err
	}

	if !utf8.Valid(fc.Content) {
		return mustJSON(map[string]any{
			"error": "Binary file — cannot display content",
		}), nil
	}

	content := string(fc.Content)
	truncated := false
	if len(content) > maxReadFileBytes {
		content = truncateUTF8(content, maxReadFileBytes)
		truncated = true
	}

	return mustJSON(map[string]any{
		"path":      path,
		"content":   content,
		"size":      fc.Size,
		"truncated": truncated,
	}), nil
}

func contextPackCacheKey(repoKey, ref, query string, paths, globs []string, maxResults int) string {
	cp := append([]string(nil), paths...)
	cg := append([]string(nil), globs...)
	sort.Strings(cp)
	sort.Strings(cg)
	return strings.Join([]string{
		repoKey,
		ref,
		query,
		strings.Join(cp, ","),
		strings.Join(cg, ","),
		fmt.Sprintf("%d", maxResults),
	}, "\x00")
}

func (h *ToolHandlers) getContextPackCache(key string) (string, bool) {
	h.cacheMu.Lock()
	defer h.cacheMu.Unlock()
	entry, ok := h.packCache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		if ok {
			delete(h.packCache, key)
		}
		return "", false
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(entry.value), &m); err != nil {
		return entry.value, true
	}
	m["cached"] = true
	out, err := json.Marshal(m)
	if err != nil {
		return entry.value, true
	}
	return string(out), true
}

func (h *ToolHandlers) setContextPackCache(key, value string) {
	h.cacheMu.Lock()
	defer h.cacheMu.Unlock()
	h.packCache[key] = contextPackCacheEntry{
		expiresAt: time.Now().Add(contextPackTTL),
		value:     value,
	}
}

func (h *ToolHandlers) firstReadableFile(ctx context.Context, provider RepoProvider, ref string, candidates []string) map[string]any {
	for _, path := range candidates {
		fc, err := provider.GetFileContent(ctx, path, ref, maxContextPackRead)
		if err != nil || fc == nil || !utf8.Valid(fc.Content) {
			continue
		}
		content := string(fc.Content)
		truncated := fc.Size > int64(len(fc.Content))
		return map[string]any{
			"path":      path,
			"content":   content,
			"size":      fc.Size,
			"truncated": truncated,
		}
	}
	return map[string]any{}
}

func uniqueSearchPaths(items []SearchItem, limit int) []string {
	seen := map[string]bool{}
	paths := make([]string, 0, limit)
	for _, item := range items {
		path := strings.TrimSpace(item.Path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
		if len(paths) >= limit {
			break
		}
	}
	return paths
}

// isWellKnownConfigFile allows small manifest/config files anywhere in the tree (e.g. ui/package.json).
// Source code (.go, .tsx, …) stays blocked — use repo_get_symbol / repo_get_snippet.
func isWellKnownConfigFile(base string) bool {
	l := strings.ToLower(base)
	switch l {
	case "package.json", "package-lock.json", "npm-shrinkwrap.json",
		"pnpm-lock.yaml", "yarn.lock", "bun.lockb",
		"tsconfig.json", "tsconfig.base.json", "jsconfig.json",
		"pyproject.toml", "poetry.lock", "uv.lock", "requirements.txt", "setup.py", "setup.cfg",
		"go.mod", "go.sum", "cargo.toml", "cargo.lock",
		"docker-compose.yml", "docker-compose.yaml", "compose.yaml", "compose.yml":
		return true
	}
	if strings.HasPrefix(l, "vite.config.") || strings.HasPrefix(l, "vitest.config.") {
		return true
	}
	if strings.HasSuffix(l, ".config.js") || strings.HasSuffix(l, ".config.mjs") || strings.HasSuffix(l, ".config.ts") {
		return true
	}
	return false
}

func isAllowedReadFile(path string) bool {
	base := filepath.Base(path)
	if isWellKnownConfigFile(base) {
		return true
	}
	dir := filepath.Dir(path)

	allowedBasenames := []string{
		"README", "REPO_MAP", "CHANGELOG", "LICENSE",
		".gitignore", ".env.example", "Makefile", "Dockerfile",
	}
	for _, prefix := range allowedBasenames {
		if strings.HasPrefix(strings.ToUpper(base), strings.ToUpper(prefix)) {
			return true
		}
	}

	lBase := strings.ToLower(base)
	if strings.HasPrefix(lBase, "docker-compose") && (strings.HasSuffix(lBase, ".yml") || strings.HasSuffix(lBase, ".yaml")) {
		return true
	}

	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, part := range parts[:max(0, len(parts)-1)] {
		if part == "docs" {
			return true
		}
	}

	if dir == "." || dir == "" {
		ext := strings.ToLower(filepath.Ext(base))
		switch ext {
		case ".md", ".yaml", ".yml", ".json", ".toml":
			return true
		}
	}

	// Config / manifest by extension anywhere (ui/package.json, turbo.json, *.yaml in services/).
	// Source uses .go, .ts, .tsx, .py — not these extensions.
	ext := strings.ToLower(filepath.Ext(base))
	switch ext {
	case ".json", ".yaml", ".yml", ".toml":
		return true
	}

	return false
}

func testFileCandidates(path string) []string {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	dir := filepath.Dir(path)
	name := filepath.Base(base)

	var candidates []string
	switch ext {
	case ".go":
		candidates = append(candidates, base+"_test.go")
	case ".py":
		candidates = append(candidates, filepath.Join(dir, "test_"+name+".py"))
		candidates = append(candidates, filepath.Join(dir, name+"_test.py"))
	case ".ts", ".tsx":
		candidates = append(candidates, base+".test"+ext)
		candidates = append(candidates, base+".spec"+ext)
	case ".js", ".jsx":
		candidates = append(candidates, base+".test"+ext)
		candidates = append(candidates, base+".spec"+ext)
	}
	return candidates
}

func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error":"json marshal failed"}`
	}
	return string(b)
}
