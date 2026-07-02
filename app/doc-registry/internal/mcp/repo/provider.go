package repo

import (
	"context"
	"errors"
)

// ErrFileNotFound is returned by GetFileContent / GetFileMeta when the requested
// path does not exist at the given ref. Callers map this to a not-found result
// (e.g. found:false) rather than a transport/upstream error.
var ErrFileNotFound = errors.New("file not found")

// RepoProvider abstracts repository access. Phase 1: GitLab REST.
// Future: LocalCloneProvider, CIIndexProvider.
type RepoProvider interface {
	Search(ctx context.Context, query string, opts SearchOpts) (*SearchResult, error)
	ListFiles(ctx context.Context, path, ref string, opts ListOpts) (*ListResult, error)
	GetFileContent(ctx context.Context, path, ref string, maxBytes int64) (*FileContent, error)
	GetFileMeta(ctx context.Context, path, ref string) (*FileMeta, error)
}

// SearchOpts configures a search call.
type SearchOpts struct {
	Paths      []string
	Globs      []string
	Ref        string
	MaxResults int
}

type SearchItem struct {
	Path      string `json:"path"`
	Line      *int   `json:"line"`
	Snippet   string `json:"snippet"`
	MatchType string `json:"match_type"`
}

type SearchResult struct {
	Items            []SearchItem `json:"items"`
	Truncated        bool         `json:"truncated"`
	TruncationReason string       `json:"truncation_reason,omitempty"`
}

type ListOpts struct {
	Recursive bool
	Page      int
}

// ListItem is a single tree entry.
type ListItem struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Path string `json:"path"`
}

type ListResult struct {
	Items     []ListItem `json:"items"`
	Truncated bool       `json:"truncated"`
	NextPage  *int       `json:"next_page,omitempty"`
}

// FileContent holds raw file bytes plus metadata.
type FileContent struct {
	Path    string
	Content []byte
	Size    int64
	BlobSHA string
}

// FileMeta holds file metadata without content.
type FileMeta struct {
	Path        string
	Size        int64
	BlobSHA     string
	RevisionID  string
	ContentType string
}
