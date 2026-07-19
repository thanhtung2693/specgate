package repo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

// NormalizeGitLabAPIURL adds the API v4 path when an integration stores only
// its GitLab host URL.
func NormalizeGitLabAPIURL(apiURL string) string {
	apiURL = strings.TrimSuffix(strings.TrimSpace(apiURL), "/")
	if apiURL == "" {
		return ""
	}
	u, err := url.Parse(apiURL)
	if err != nil {
		return apiURL
	}
	if strings.Trim(u.Path, "/") == "" {
		u.Path = "/api/v4"
		return strings.TrimSuffix(u.String(), "/")
	}
	return apiURL
}

// GitLabProvider reads files through the GitLab REST API.
type GitLabProvider struct {
	cfg       GitLabConfig
	client    *http.Client
	projectID string
}

// NewGitLabProvider creates a GitLab-backed file reader.
func NewGitLabProvider(cfg GitLabConfig) *GitLabProvider {
	return &GitLabProvider{
		cfg:       cfg,
		client:    &http.Client{Timeout: 15 * time.Second},
		projectID: gitlabapi.ProjectAPIPathSegment(cfg.ProjectID),
	}
}

type gitLabFileResponse struct {
	BlobID   string `json:"blob_id"`
	Size     int64  `json:"size"`
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
}

const gitLabFileMetadataMaxBytes = 3 << 20

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

func readGitLabResponse(body io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("gitlab response exceeds %d bytes", maxBytes)
	}
	return data, nil
}

func (g *GitLabProvider) getFileMeta(ctx context.Context, path, ref string) (gitLabFileResponse, error) {
	ref = g.ref(ref)

	u := fmt.Sprintf("%s?ref=%s",
		g.apiURL("/repository/files/"+url.PathEscape(path)),
		url.QueryEscape(ref),
	)

	resp, err := g.doRequest(ctx, u)
	if err != nil {
		return gitLabFileResponse{}, fmt.Errorf("gitlab file meta: %w", err)
	}
	defer resp.Body.Close()

	body, err := readGitLabResponse(resp.Body, gitLabFileMetadataMaxBytes)
	if err != nil {
		return gitLabFileResponse{}, fmt.Errorf("gitlab file meta read: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return gitLabFileResponse{}, fmt.Errorf("%w: %s", ErrFileNotFound, path)
	}
	if resp.StatusCode != http.StatusOK {
		return gitLabFileResponse{}, fmt.Errorf("gitlab file meta: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var meta gitLabFileResponse
	if err := json.Unmarshal(body, &meta); err != nil {
		return gitLabFileResponse{}, fmt.Errorf("gitlab file meta decode: %w", err)
	}
	return meta, nil
}

// GetFileContent returns one file.
func (g *GitLabProvider) GetFileContent(ctx context.Context, path, ref string, maxBytes int64) (*FileContent, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("file size limit must be positive")
	}
	meta, err := g.getFileMeta(ctx, path, ref)
	if err != nil {
		return nil, err
	}

	if meta.Size > maxBytes {
		return nil, fmt.Errorf("file too large (%d bytes, limit %d)", meta.Size, maxBytes)
	}

	inlineContent, hasInlineContent, err := decodeGitLabFileContent(meta)
	if err != nil {
		return nil, err
	}
	if hasInlineContent {
		if int64(len(inlineContent)) > maxBytes {
			return nil, fmt.Errorf("file too large (%d bytes, limit %d)", len(inlineContent), maxBytes)
		}
		return &FileContent{
			Path:    path,
			Content: inlineContent,
			Size:    meta.Size,
			BlobSHA: meta.BlobID,
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

	body, err := readGitLabResponse(resp.Body, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("gitlab file content read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitlab file content: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return &FileContent{
		Path:    path,
		Content: body,
		Size:    meta.Size,
		BlobSHA: meta.BlobID,
	}, nil
}
