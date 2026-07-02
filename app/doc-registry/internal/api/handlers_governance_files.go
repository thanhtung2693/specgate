package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/specgate/doc-registry/internal/governancefiles"
	stores3 "github.com/specgate/doc-registry/internal/storage/s3"
)

// UploadGovernanceFile accepts file bytes through the API and writes them to S3
// server-side, so the BROWSER never receives (or PUTs to) a presigned
// object-store URL — no MinIO/S3 endpoint or credentials are exposed client-side.
// This is the upload mirror of ServeGovernanceFileContent. (Agents still use the
// presign → PUT flow from inside the container, where S3 is reachable.)
//
// The raw body is the file content; Content-Type is the mime; X-File-Name (URL-
// encoded) carries the original filename. Creates the row, puts the object, and
// commits to ready in one call.
func (h *Handlers) UploadGovernanceFile(w http.ResponseWriter, r *http.Request) {
	if h.GovernanceFiles == nil || (h.BlobStore == nil && h.S3 == nil) {
		http.Error(w, "governance uploads unavailable", http.StatusServiceUnavailable)
		return
	}
	// Strip any charset/parameters for the allowlist check + stored mime.
	ct := strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0])
	if !allowedGovernanceFileContentType(ct) {
		http.Error(w, "content_type must be image/*, audio/*, text/markdown, or text/html", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.Header.Get("X-File-Name"))
	if decoded, err := url.QueryUnescape(name); err == nil {
		name = strings.TrimSpace(decoded)
	}
	if name == "" {
		http.Error(w, "X-File-Name header is required", http.StatusBadRequest)
		return
	}
	if h.GovernanceUploadMaxBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, h.GovernanceUploadMaxBytes)
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "file exceeds GOVERNANCE_UPLOAD_MAX_BYTES or read failed", http.StatusRequestEntityTooLarge)
		return
	}
	if len(body) == 0 {
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

	id := uuid.NewString()
	now := time.Now().UTC()

	var objectKey string
	if h.BlobStore != nil {
		blobID, err := h.BlobStore.Put(r.Context(), bytes.NewReader(body), int64(len(body)), ct)
		if err != nil {
			log.Ctx(r.Context()).Error().Err(err).Msg("governance_files: blob store upload failed")
			http.Error(w, "store object failed", http.StatusInternalServerError)
			return
		}
		objectKey = blobID
	} else {
		objectKey = governanceFilesObjectKey(h.S3KeyPrefix, id, ct, name)
		if err := h.S3.PutObjectWithContentType(r.Context(), objectKey, body, ct); err != nil {
			log.Ctx(r.Context()).Error().Err(err).Str("key", objectKey).Msg("governance_files: api upload put failed")
			http.Error(w, "store object failed", http.StatusInternalServerError)
			return
		}
	}

	if _, err := h.GovernanceFiles.Create(r.Context(), governancefiles.File{
		ID:         id,
		Name:       name,
		Mime:       ct,
		SizeBytes:  int64(len(body)),
		ObjectKey:  objectKey,
		Status:     governancefiles.StatusPending,
		CreatedAt:  now,
		LastUsedAt: now,
	}); err != nil {
		http.Error(w, "create governance file row", http.StatusInternalServerError)
		return
	}
	f, err := h.GovernanceFiles.Commit(r.Context(), id, now)
	if err != nil {
		http.Error(w, "commit governance file", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, governanceFilesDTO(f))
}

func (h *Handlers) PresignFile(ctx context.Context, in *PresignFileInput) (*PresignFileOutput, error) {
	if h.S3 == nil {
		return nil, huma.Error503ServiceUnavailable("governance uploads require S3")
	}
	if h.GovernanceFiles == nil {
		return nil, huma.Error503ServiceUnavailable("governance files store unavailable")
	}
	ct := strings.TrimSpace(in.Body.ContentType)
	if !allowedGovernanceFileContentType(ct) {
		return nil, huma.Error400BadRequest(
			"content_type must be image/*, audio/*, text/markdown, or text/x-markdown",
		)
	}
	if in.Body.SizeBytes <= 0 {
		return nil, huma.Error400BadRequest("size_bytes must be greater than 0")
	}
	if h.GovernanceUploadMaxBytes > 0 && in.Body.SizeBytes > h.GovernanceUploadMaxBytes {
		return nil, huma.Error413RequestEntityTooLarge("file exceeds GOVERNANCE_UPLOAD_MAX_BYTES")
	}
	name := strings.TrimSpace(in.Body.Name)
	if name == "" {
		return nil, huma.Error400BadRequest("name is required")
	}

	id := uuid.NewString()
	key := governanceFilesObjectKey(h.S3KeyPrefix, id, ct, name)

	now := time.Now().UTC()
	if _, err := h.GovernanceFiles.Create(ctx, governancefiles.File{
		ID:         id,
		Name:       name,
		Mime:       ct,
		SizeBytes:  in.Body.SizeBytes,
		ObjectKey:  key,
		Status:     governancefiles.StatusPending,
		CreatedAt:  now,
		LastUsedAt: now,
	}); err != nil {
		return nil, huma.Error500InternalServerError("create governance file row", err)
	}

	putTTL := h.GovernanceUploadPutTTL
	if putTTL <= 0 {
		putTTL = defaultGovernanceUploadPutTTL
	}
	uploadURL, err := h.S3.PresignPut(ctx, key, ct, putTTL)
	if err != nil {
		return nil, huma.Error500InternalServerError("presign upload", err)
	}

	out := &PresignFileOutput{}
	out.Body.FileID = id
	out.Body.UploadURL = uploadURL
	out.Body.ObjectKey = key
	return out, nil
}

// allowedGovernanceFileContentType narrows the internal governance-file upload allowlist
// to what the spec calls out (image/*, audio/*, markdown). The shared
// stores3.AllowedGovernanceUploadContentType is broader (also pdf, text/plain) and
// is used by the other upload surfaces.
func allowedGovernanceFileContentType(contentType string) bool {
	c := strings.ToLower(strings.TrimSpace(contentType))
	if c == "" {
		return false
	}
	if strings.HasPrefix(c, "image/") || strings.HasPrefix(c, "audio/") {
		return true
	}
	switch c {
	case "text/markdown", "text/x-markdown", "text/html":
		return true
	default:
		return false
	}
}

// governanceFilesObjectKey returns {prefix}governance/resources/uploads/{id}{ext}
// where ext prefers the original filename suffix, falling back to the
// content-type map. The optional prefix (e.g. "doc-registry/") namespaces
// uploads when the bucket is shared.
func governanceFilesObjectKey(prefix, id, contentType, name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		// Reuse the existing per-content-type extension table by routing through
		// GovernanceUploadObjectKey and stripping its uuid prefix.
		gen := stores3.GovernanceUploadObjectKey("", contentType)
		ext = strings.ToLower(filepath.Ext(gen))
		if ext == "" {
			ext = ".bin"
		}
	}
	return prefix + "governance/resources/uploads/" + id + ext
}

func (h *Handlers) CommitFile(ctx context.Context, in *CommitFileInput) (*CommitFileOutput, error) {
	if h.S3 == nil || h.GovernanceFiles == nil {
		return nil, huma.Error503ServiceUnavailable("governance files unavailable")
	}
	now := time.Now().UTC()
	f, err := h.GovernanceFiles.Commit(ctx, in.ID, now)
	if err != nil {
		if errors.Is(err, governancefiles.ErrNotFound) {
			return nil, huma.Error404NotFound("governance file not found")
		}
		return nil, huma.Error500InternalServerError("commit governance file", err)
	}
	return &CommitFileOutput{Body: governanceFilesDTO(f)}, nil
}

func (h *Handlers) ListFiles(ctx context.Context, in *ListFilesInput) (*ListFilesOutput, error) {
	if h.S3 == nil || h.GovernanceFiles == nil {
		return nil, huma.Error503ServiceUnavailable("governance files unavailable")
	}
	items, total, err := h.GovernanceFiles.List(ctx, governancefiles.ListFilter{
		Q: in.Q, Limit: in.Limit, Offset: in.Offset,
	})
	if err != nil {
		return nil, huma.Error500InternalServerError("list governance files", err)
	}

	out := &ListFilesOutput{}
	out.Body.Total = total
	out.Body.Items = make([]GovernanceFileDTO, 0, len(items))
	for i := range items {
		out.Body.Items = append(out.Body.Items, governanceFilesDTO(&items[i]))
	}
	return out, nil
}

func (h *Handlers) TouchFile(ctx context.Context, in *TouchFileInput) (*TouchFileOutput, error) {
	if h.S3 == nil || h.GovernanceFiles == nil {
		return nil, huma.Error503ServiceUnavailable("governance files unavailable")
	}
	now := time.Now().UTC()
	f, err := h.GovernanceFiles.Touch(ctx, in.ID, now)
	if err != nil {
		if errors.Is(err, governancefiles.ErrNotFound) {
			return nil, huma.Error404NotFound("governance file not found")
		}
		return nil, huma.Error500InternalServerError("touch governance file", err)
	}
	return &TouchFileOutput{Body: governanceFilesDTO(f)}, nil
}

func (h *Handlers) DeleteFile(ctx context.Context, in *DeleteFileInput) (*DeleteFileOutput, error) {
	if h.GovernanceFiles == nil {
		return nil, huma.Error503ServiceUnavailable("governance files unavailable")
	}
	key, err := h.GovernanceFiles.Delete(ctx, in.ID)
	if err != nil {
		if errors.Is(err, governancefiles.ErrNotFound) {
			return nil, huma.Error404NotFound("governance file not found")
		}
		return nil, huma.Error500InternalServerError("delete governance file", err)
	}
	if h.S3 != nil {
		// Best-effort per spec §5.4 — DELETE still returns 204 even if S3 delete fails.
		if err := h.S3.DeleteObject(ctx, key); err != nil {
			log.Ctx(ctx).Warn().Err(err).Str("key", key).Msg("governance_files: s3 delete failed")
		}
	}
	return &DeleteFileOutput{}, nil
}

// ServeGovernanceFileContent streams a governance file's bytes through the API (reading
// the S3 object server-side) so the browser never needs a presigned object-store
// URL. Content is immutable per file id, so it is cacheable with an ETag (the id);
// no-store would force a multi-MB re-buffer on every attachment preview render.
func (h *Handlers) ServeGovernanceFileContent(w http.ResponseWriter, r *http.Request) {
	if h.GovernanceFiles == nil || (h.BlobStore == nil && h.S3 == nil) {
		http.NotFound(w, r)
		return
	}
	f, err := h.GovernanceFiles.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil || f == nil {
		http.NotFound(w, r)
		return
	}
	etag := `"` + f.ID + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Uploaded content is untrusted (allowlist includes text/html and image/svg+xml,
	// both of which can carry script). `sandbox allow-scripts` forces an opaque
	// origin even on a top-level open, so a malicious upload cannot script the
	// doc-registry origin — while still rendering inside sandboxed HTML previews.
	// Images via <img> ignore it; markdown is inert.
	w.Header().Set("Content-Security-Policy", "sandbox allow-scripts")
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	var body []byte
	if h.BlobStore != nil && looksLikeBlobID(f.ObjectKey) {
		rc, err := h.BlobStore.Open(r.Context(), f.ObjectKey)
		if err != nil {
			http.Error(w, "file content not available", http.StatusNotFound)
			return
		}
		defer rc.Close()
		body, err = io.ReadAll(rc)
		if err != nil {
			http.Error(w, "file content read failed", http.StatusInternalServerError)
			return
		}
	} else if h.S3 != nil {
		var err error
		body, err = h.S3.GetObject(r.Context(), f.ObjectKey)
		if err != nil {
			http.Error(w, "file content not available", http.StatusNotFound)
			return
		}
	} else {
		http.Error(w, "file content not available", http.StatusNotFound)
		return
	}
	if ct := strings.TrimSpace(f.Mime); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	_, _ = w.Write(body)
}

// governanceFilesDTO no longer carries a presigned object-store URL: browser and
// agent readers both fetch content through the API content proxy
// (/governance/files/{id}/content), so no MinIO/S3 endpoint or credential is ever
// returned to a client. GetURL stays in the struct (empty) for wire-shape
// stability.
func governanceFilesDTO(f *governancefiles.File) GovernanceFileDTO {
	return GovernanceFileDTO{
		FileID:     f.ID,
		Name:       f.Name,
		Mime:       f.Mime,
		SizeBytes:  f.SizeBytes,
		CreatedAt:  f.CreatedAt.UTC().Format(time.RFC3339),
		LastUsedAt: f.LastUsedAt.UTC().Format(time.RFC3339),
	}
}

// looksLikeBlobID reports whether s is a UUID (36 chars, 4 hyphens) — used to
// distinguish BlobStore IDs from S3 object keys in ObjectKey.
func looksLikeBlobID(s string) bool {
	return len(s) == 36 && strings.Count(s, "-") == 4
}
