package repo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/specgate/doc-registry/internal/gitlabapi"
)

// GitLabConfig holds connection parameters for the GitLab REST API. Bearer
// selects the auth header: an OAuth access token must travel as
// `Authorization: Bearer`, a personal/project access token as `PRIVATE-TOKEN`.
type GitLabConfig struct {
	APIURL     string
	Token      string
	ProjectID  string
	DefaultRef string
	Bearer     bool
}

// GitLabProvider implements RepoProvider via GitLab REST API with caching.
type GitLabProvider struct {
	cfg       GitLabConfig
	client    *http.Client
	projectID string

	metaCache    *MetaCache
	contentCache *LRUCache
}

// NewGitLabProvider creates a new GitLab-backed RepoProvider with caching.
// metaCache: file metadata (blob_sha, size), TTL ~5 min, item-count bounded.
// contentCache: raw file bytes for files ≤1 MB, byte-size bounded at 256 MB.
func NewGitLabProvider(cfg GitLabConfig) *GitLabProvider {
	return &GitLabProvider{
		cfg:          cfg,
		client:       &http.Client{Timeout: 15 * time.Second},
		projectID:    gitlabapi.ProjectAPIPathSegment(cfg.ProjectID),
		metaCache:    NewMetaCache(1000, 5*time.Minute),
		contentCache: NewLRUCache(256*1024*1024, 30*time.Minute),
	}
}

const maxBackendPages = 5
const maxCachedContentBytes = 1 * 1024 * 1024

type gitLabFileResponse struct {
	BlobID       string `json:"blob_id"`
	LastCommitID string `json:"last_commit_id"`
	Size         int64  `json:"size"`
	FileName     string `json:"file_name"`
	FilePath     string `json:"file_path"`
	Encoding     string `json:"encoding"`
	Content      string `json:"content"`
}

func (g *GitLabProvider) ref(r string) string {
	if r == "" {
		return g.cfg.DefaultRef
	}
	return r
}

func (g *GitLabProvider) apiURL(path string) string {
	return fmt.Sprintf("%s/projects/%s%s", strings.TrimSuffix(g.cfg.APIURL, "/"), g.projectID, path)
}

func (g *GitLabProvider) doRequest(ctx context.Context, reqURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	if g.cfg.Bearer {
		req.Header.Set("Authorization", "Bearer "+g.cfg.Token)
	} else {
		req.Header.Set("PRIVATE-TOKEN", g.cfg.Token)
	}
	return g.client.Do(req)
}

// Search implements RepoProvider.Search.
func (g *GitLabProvider) Search(ctx context.Context, query string, opts SearchOpts) (*SearchResult, error) {
	ref := g.ref(opts.Ref)
	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = 20
	}

	var collected []SearchItem
	truncated := false
	truncationReason := ""

	for page := 1; page <= maxBackendPages; page++ {
		u := fmt.Sprintf("%s?scope=blobs&search=%s&ref=%s&per_page=100&page=%d",
			g.apiURL("/search"),
			url.QueryEscape(query),
			url.QueryEscape(ref),
			page,
		)

		resp, err := g.doRequest(ctx, u)
		if err != nil {
			return nil, fmt.Errorf("gitlab search: %w", err)
		}
		nextPage := resp.Header.Get("X-Next-Page")
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("gitlab search read: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("gitlab search: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var results []struct {
			Path      string `json:"path"`
			Data      string `json:"data"`
			StartLine int    `json:"startline"`
		}
		if err := json.Unmarshal(body, &results); err != nil {
			return nil, fmt.Errorf("gitlab search decode: %w", err)
		}

		for _, r := range results {
			if !matchesFilters(r.Path, opts.Paths, opts.Globs) {
				continue
			}
			var line *int
			if r.StartLine > 0 {
				l := r.StartLine
				line = &l
			}
			snippet := extractSnippet(r.Data, r.StartLine)
			collected = append(collected, SearchItem{
				Path:      r.Path,
				Line:      line,
				Snippet:   snippet,
				MatchType: "content",
			})
			if len(collected) >= maxResults {
				truncated = true
				truncationReason = "max_results"
				break
			}
		}

		if len(collected) >= maxResults {
			break
		}

		if nextPage == "" {
			break
		}
		if truncationReason == "max_results" {
			break
		}
	}

	return &SearchResult{
		Items:            collected,
		Truncated:        truncated,
		TruncationReason: truncationReason,
	}, nil
}

// ListFiles implements RepoProvider.ListFiles.
func (g *GitLabProvider) ListFiles(ctx context.Context, path, ref string, opts ListOpts) (*ListResult, error) {
	ref = g.ref(ref)
	page := opts.Page
	if page <= 0 {
		page = 1
	}

	u := fmt.Sprintf("%s?path=%s&ref=%s&per_page=100&page=%d",
		g.apiURL("/repository/tree"),
		url.QueryEscape(path),
		url.QueryEscape(ref),
		page,
	)
	if opts.Recursive {
		u += "&recursive=true"
	}

	resp, err := g.doRequest(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("gitlab list files: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gitlab list files read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitlab list files: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("gitlab list files decode: %w", err)
	}

	items := make([]ListItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, ListItem{
			Name: e.Name,
			Type: e.Type,
			Path: e.Path,
		})
	}

	var nextPage *int
	if np := resp.Header.Get("X-Next-Page"); np != "" {
		if n, err := strconv.Atoi(np); err == nil {
			nextPage = &n
		}
	}

	return &ListResult{
		Items:     items,
		Truncated: len(items) >= 100 || nextPage != nil,
		NextPage:  nextPage,
	}, nil
}

func isFullCommitSHA(ref string) bool {
	if len(ref) != 40 {
		return false
	}
	for _, r := range ref {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			continue
		}
		return false
	}
	return true
}

func (g *GitLabProvider) metaCacheKey(path, revisionID string) string {
	return fmt.Sprintf("%s/%s/%s", g.cfg.ProjectID, revisionID, path)
}

func (g *GitLabProvider) contentCacheKey(path, revisionID, blobSHA string) string {
	return fmt.Sprintf("%s/%s/%s/%s", g.cfg.ProjectID, revisionID, path, blobSHA)
}

func decodeGitLabFileContent(meta gitLabFileResponse) ([]byte, bool, error) {
	if strings.TrimSpace(meta.Content) == "" {
		return nil, false, nil
	}
	if !strings.EqualFold(strings.TrimSpace(meta.Encoding), "base64") {
		return nil, false, nil
	}
	body, err := base64.StdEncoding.DecodeString(meta.Content)
	if err != nil {
		return nil, false, fmt.Errorf("gitlab file content decode: %w", err)
	}
	return body, true, nil
}

func (g *GitLabProvider) getFileMetaWithInlineContent(ctx context.Context, path, ref string) (*FileMeta, []byte, bool, error) {
	ref = g.ref(ref)

	if isFullCommitSHA(ref) {
		if cached, ok := g.metaCache.Get(g.metaCacheKey(path, ref)); ok {
			return cached, nil, false, nil
		}
	}

	u := fmt.Sprintf("%s?ref=%s",
		g.apiURL("/repository/files/"+url.PathEscape(path)),
		url.QueryEscape(ref),
	)

	resp, err := g.doRequest(ctx, u)
	if err != nil {
		return nil, nil, false, fmt.Errorf("gitlab file meta: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, false, fmt.Errorf("gitlab file meta read: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil, false, fmt.Errorf("%w: %s", ErrFileNotFound, path)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, false, fmt.Errorf("gitlab file meta: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var meta gitLabFileResponse
	if err := json.Unmarshal(body, &meta); err != nil {
		return nil, nil, false, fmt.Errorf("gitlab file meta decode: %w", err)
	}

	revisionID := strings.TrimSpace(meta.LastCommitID)
	if revisionID == "" {
		revisionID = ref
	}
	result := &FileMeta{
		Path:       meta.FilePath,
		Size:       meta.Size,
		BlobSHA:    meta.BlobID,
		RevisionID: revisionID,
	}
	if isFullCommitSHA(revisionID) {
		g.metaCache.Put(g.metaCacheKey(path, revisionID), result)
	}
	if isFullCommitSHA(ref) && ref != revisionID {
		g.metaCache.Put(g.metaCacheKey(path, ref), result)
	}
	content, hasContent, err := decodeGitLabFileContent(meta)
	if err != nil {
		return nil, nil, false, err
	}
	if hasContent && meta.Size <= maxCachedContentBytes {
		g.contentCache.Put(g.contentCacheKey(path, revisionID, meta.BlobID), content)
	}
	return result, content, hasContent, nil
}

// GetFileMeta implements RepoProvider.GetFileMeta. Results are cached only under immutable revision IDs.
func (g *GitLabProvider) GetFileMeta(ctx context.Context, path, ref string) (*FileMeta, error) {
	meta, _, _, err := g.getFileMetaWithInlineContent(ctx, path, ref)
	return meta, err
}

// GetFileContent implements RepoProvider.GetFileContent. Files ≤1 MB are cached by revision ID + blob SHA.
func (g *GitLabProvider) GetFileContent(ctx context.Context, path, ref string, maxBytes int64) (*FileContent, error) {
	meta, inlineContent, hasInlineContent, err := g.getFileMetaWithInlineContent(ctx, path, ref)
	if err != nil {
		return nil, err
	}

	if maxBytes > 0 && meta.Size > maxBytes {
		return nil, fmt.Errorf("file too large (%d bytes, limit %d)", meta.Size, maxBytes)
	}

	revisionID := meta.RevisionID
	if revisionID == "" {
		revisionID = g.ref(ref)
	}
	contentKey := g.contentCacheKey(path, revisionID, meta.BlobSHA)

	// Only cache files ≤1 MB (per spec: large files not cached as raw content).
	cacheable := meta.Size <= maxCachedContentBytes

	if cacheable {
		if cached, ok := g.contentCache.Get(contentKey); ok {
			return &FileContent{
				Path:    path,
				Content: cached,
				Size:    meta.Size,
				BlobSHA: meta.BlobSHA,
			}, nil
		}
	}
	if hasInlineContent {
		return &FileContent{
			Path:    path,
			Content: inlineContent,
			Size:    meta.Size,
			BlobSHA: meta.BlobSHA,
		}, nil
	}

	ref = g.ref(ref)
	u := fmt.Sprintf("%s?ref=%s",
		g.apiURL("/repository/files/"+url.PathEscape(path)+"/raw"),
		url.QueryEscape(ref),
	)

	resp, err := g.doRequest(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("gitlab file content: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gitlab file content read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitlab file content: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if cacheable {
		g.contentCache.Put(contentKey, body)
	}

	return &FileContent{
		Path:    path,
		Content: body,
		Size:    meta.Size,
		BlobSHA: meta.BlobSHA,
	}, nil
}

func matchesFilters(path string, paths []string, globs []string) bool {
	if len(paths) > 0 {
		matched := false
		for _, prefix := range paths {
			if strings.HasPrefix(path, prefix) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(globs) > 0 {
		matched := false
		for _, glob := range globs {
			if ok, _ := filepath.Match(glob, path); ok {
				matched = true
				break
			}
			if ok, _ := filepath.Match(glob, filepath.Base(path)); ok {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// extractSnippet returns ~5 lines centered around the match within the data blob.
// startLine is the 1-based line from GitLab; if 0, returns the first 5 lines.
func extractSnippet(data string, startLine int) string {
	lines := strings.Split(data, "\n")
	if len(lines) == 0 {
		return data
	}
	if startLine <= 0 || len(lines) <= 5 {
		if len(lines) > 5 {
			lines = lines[:5]
		}
		return strings.Join(lines, "\n")
	}
	// Center 5 lines around the match (2 before, match, 2 after).
	idx := startLine - 1
	if idx >= len(lines) {
		idx = len(lines) - 1
	}
	start := max(0, idx-2)
	end := min(len(lines), start+5)
	return strings.Join(lines[start:end], "\n")
}
