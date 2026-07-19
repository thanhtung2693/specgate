package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/specgate/doc-registry/internal/knowledgequeue"
	"github.com/specgate/doc-registry/internal/workspace"
)

var (
	ErrNotFound   = errors.New("knowledge document not found")
	ErrValidation = errors.New("knowledge validation failed")
)

const (
	// DefaultSearchLimit is the default number of chunks returned by Search.
	DefaultSearchLimit = 8
	// MaxSearchLimit caps the maximum chunks per search request.
	MaxSearchLimit = 50
)

type Repository interface {
	CreateVersion(context.Context, *Document, []Link) error
	Get(context.Context, string, string) (*Document, error)
	List(context.Context, ListFilter) ([]Document, error)
	// Count returns the number of documents matching the filter, ignoring
	// pagination. It backs the API's total field.
	Count(context.Context, ListFilter) (int, error)
	// ListByFeatureOrRequest returns all document versions in the given
	// workspace where linked_feature_id matches any feature ref OR
	// linked_request_id = requestID. An empty workspaceID returns an empty slice
	// without a DB call (workspace scope is required); either feature/request
	// arg may be empty; both empty returns an empty slice. All versions are
	// returned (not filtered by is_latest) so callers can derive freshness from
	// each row's IsLatest field.
	ListByFeatureOrRequest(context.Context, string, []string, string) ([]Document, error)
	History(context.Context, string) ([]Document, error)
	UpdateStatus(context.Context, string, string, Status, string, string) error
	ReplaceChunks(context.Context, string, string, []Chunk) error
	ChunkCount(context.Context, string, string) (int, error)
	// ChunksForDocument returns all chunks of a version ordered by chunk_index,
	// for bounded section-context expansion of a search hit.
	ChunksForDocument(context.Context, string, string) ([]Chunk, error)
	DeleteVersion(context.Context, string, string) error
	LatestVersion(context.Context, string) (string, error)
	NextMinorVersion(context.Context, string) (string, error)
}

type ObjectStore interface {
	PutObject(ctx context.Context, key string, body []byte) error
	GetObject(ctx context.Context, key string, maxBytes int64) ([]byte, error)
	DeleteObject(ctx context.Context, key string) error
}

type VectorStore interface {
	EnsureCollection(context.Context) error
	Upsert(context.Context, []VectorPoint) error
	DeleteVersion(context.Context, string, string) error
	Search(context.Context, VectorSearch) ([]VectorResult, error)
}

type VectorPoint struct {
	ID      string
	Vector  []float32
	Payload map[string]any
}

type VectorSearch struct {
	WorkspaceID   string
	Vector        []float32
	LinkedFeature string
	LinkedRequest string
	DocumentTypes []string
	Authorities   []string
	LatestOnly    bool
	Limit         int
}

type VectorResult struct {
	ID      string
	Score   float64
	Payload map[string]any
}

type Service struct {
	repo         Repository
	store        ObjectStore
	vectors      VectorStore
	embedder     Embedder
	log          *slog.Logger
	maxFileBytes int64
	keyPrefix    string
	now          func() time.Time
	// enqueuer dispatches ingestion to the async worker. It stays a nil
	// interface under QUEUE_DRIVER=sync (never a typed nil pointer); the call
	// site branches on nil to run ingest inline. Mirrors the webhook queue.
	enqueuer knowledgequeue.Enqueuer
}

// WithIngestEnqueuer enables async ingestion: create/upload persists the version
// (status uploaded) and enqueues a task instead of ingesting inline. Passing a
// nil interface keeps inline (sync) behavior.
func (s *Service) WithIngestEnqueuer(e knowledgequeue.Enqueuer) *Service {
	s.enqueuer = e
	return s
}

func NewService(
	repo Repository,
	store ObjectStore,
	vectors VectorStore,
	embedder Embedder,
	maxFileBytes int64,
	keyPrefix string,
) (*Service, error) {
	if embedder == nil {
		return nil, errors.New("knowledge: embedder is required")
	}
	if maxFileBytes <= 0 {
		return nil, errors.New("knowledge: max file bytes must be positive")
	}
	return &Service{
		repo:         repo,
		store:        store,
		vectors:      vectors,
		embedder:     embedder,
		log:          slog.Default().With("pkg", "knowledge"),
		maxFileBytes: maxFileBytes,
		keyPrefix:    keyPrefix,
		now:          func() time.Time { return time.Now().UTC() },
	}, nil
}

// EmbeddingsEnabled reports whether a real embedding provider is configured.
// When false (no embedding key), search and upload/index are unavailable; the
// UI uses this to warn and disable knowledge upload.
func (s *Service) EmbeddingsEnabled() bool {
	return EmbeddingsEnabled(s.embedder)
}

func (s *Service) readObject(ctx context.Context, key string) ([]byte, error) {
	return s.store.GetObject(ctx, key, s.maxFileBytes)
}

// ValidateCitation reads one exact indexed Knowledge span and returns only
// server-derived evidence. It does not search, embed, or mutate Knowledge.
func (s *Service) ValidateCitation(ctx context.Context, in CitationValidationInput) (Citation, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" || strings.TrimSpace(in.DocumentID) == "" || strings.TrimSpace(in.Version) == "" {
		return Citation{}, validation("workspace id, document id, and version are required")
	}
	if in.StartChunkIndex < 0 || in.EndChunkIndex < in.StartChunkIndex {
		return Citation{}, validation("invalid chunk range")
	}
	doc, err := s.repo.Get(ctx, in.DocumentID, in.Version)
	if err != nil {
		return Citation{}, err
	}
	if doc.WorkspaceID != in.WorkspaceID {
		return Citation{}, validation("citation document is outside workspace")
	}
	if doc.Status != StatusIndexed {
		return Citation{}, validation("citation document is not indexed")
	}
	chunks, err := s.repo.ChunksForDocument(ctx, in.DocumentID, in.Version)
	if err != nil {
		return Citation{}, err
	}
	span := make([]string, 0, in.EndChunkIndex-in.StartChunkIndex+1)
	for want := in.StartChunkIndex; want <= in.EndChunkIndex; want++ {
		found := false
		for _, chunk := range chunks {
			if chunk.ChunkIndex == want {
				span = append(span, chunk.ChunkText)
				found = true
				break
			}
		}
		if !found {
			return Citation{}, validation("citation chunk range does not exist")
		}
	}
	if want := strings.TrimSpace(in.ExcerptDigest); want != "" {
		digest := sha256.Sum256([]byte(strings.Join(span, "\n\n")))
		if !strings.EqualFold(want, hex.EncodeToString(digest[:])) {
			return Citation{}, validation("citation excerpt digest does not match")
		}
	}
	return Citation{
		WorkspaceID: in.WorkspaceID, DocumentID: in.DocumentID, Version: in.Version,
		StartChunkIndex: in.StartChunkIndex, EndChunkIndex: in.EndChunkIndex,
		URL:   fmt.Sprintf("specgate://knowledge/%s/%s#chunk-%d", in.DocumentID, in.Version, in.StartChunkIndex),
		Title: doc.Title, AuthorityLevel: doc.AuthorityLevel, Stale: !doc.IsLatest,
	}, nil
}

func (s *Service) CreateText(ctx context.Context, in CreateTextInput) (*Document, error) {
	if strings.TrimSpace(in.Content) == "" {
		return nil, validation("content cannot be empty")
	}
	if s.maxFileBytes > 0 && int64(len(in.Content)) > s.maxFileBytes {
		return nil, validation("content exceeds configured limit")
	}
	doc, err := s.createBase(ctx, in.Metadata, SourceKindText, "input.txt", "text/plain")
	if err != nil {
		return nil, err
	}
	rawKey := s.rawObjectKey(doc.WorkspaceID, doc.DocumentID, doc.Version, "input.txt")
	doc.SourceURI = rawKey
	doc.MimeType = "text/plain"
	if err := s.store.PutObject(ctx, rawKey, []byte(in.Content)); err != nil {
		return nil, err
	}
	if err := s.repo.CreateVersion(ctx, doc, linksFor(doc)); err != nil {
		if delErr := s.store.DeleteObject(ctx, rawKey); delErr != nil {
			s.log.Warn("cleanup: delete raw object after failed create", "key", rawKey, "err", delErr)
		}
		return nil, err
	}
	return s.startIngestReturningUploaded(ctx, doc, []byte(in.Content))
}

// startIngestReturningUploaded snapshots the just-created version (status
// uploaded), starts ingestion, and returns the uploaded snapshot. The API
// contract is identical across queue drivers: create/upload returns status
// uploaded and callers poll for the terminal state. Under sync the states are
// walked inline before return; under redis a worker walks them later.
func (s *Service) startIngestReturningUploaded(ctx context.Context, doc *Document, raw []byte) (*Document, error) {
	uploaded, err := s.repo.Get(ctx, doc.DocumentID, doc.Version)
	if err != nil {
		return nil, err
	}
	if err := s.startIngest(ctx, doc, raw); err != nil {
		return nil, err
	}
	return uploaded, nil
}

// startIngest dispatches ingestion. When an enqueuer is wired (QUEUE_DRIVER=redis)
// it enqueues a self-contained task; otherwise (sync) it runs ingest inline. An
// inline ingest error is not a create failure — ingest records its own terminal
// state (indexed | failed) and the caller polls status, identical to the async
// path. Only an enqueue failure propagates.
func (s *Service) startIngest(ctx context.Context, doc *Document, raw []byte) error {
	if s.enqueuer != nil {
		err := s.enqueuer.EnqueueKnowledgeIngest(ctx, knowledgequeue.Task{
			WorkspaceID: doc.WorkspaceID,
			DocumentID:  doc.DocumentID,
			Version:     doc.Version,
			Content:     raw,
		})
		if err != nil {
			s.markIngestFailed(ctx, doc, fmt.Errorf("enqueue ingest: %w", err))
		}
		return err
	}
	if err := s.ingest(ctx, doc, raw); err != nil {
		s.log.Warn("inline ingest failed", "document_id", doc.DocumentID, "version", doc.Version, "err", err)
	}
	return nil
}

// ProcessKnowledgeIngest runs an enqueued ingestion. It implements
// knowledgequeue.Processor for the async worker: re-load the version and ingest
// the task's carried content. A returned error lets asynq retry.
func (s *Service) ProcessKnowledgeIngest(ctx context.Context, t knowledgequeue.Task) error {
	workspaceID, valid := workspace.NormalizeID(t.WorkspaceID)
	if !valid {
		return validation("workspace_id is required and must be a safe path segment on ingest task")
	}
	ctx = WithWorkspace(ctx, workspaceID)
	doc, err := s.repo.Get(ctx, t.DocumentID, t.Version)
	if err != nil {
		return err
	}
	if doc.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	return s.ingest(ctx, doc, t.Content)
}

// Retry re-ingests a failed document version without deleting it (per the retry
// endpoint contract). Only failed versions may be retried. Ingestion runs through
// the same queue driver as create/upload; the returned doc is the uploaded
// snapshot so callers poll status the same way.
func (s *Service) Retry(ctx context.Context, documentID, version string) (*Document, error) {
	if version == "" {
		v, err := s.repo.LatestVersion(ctx, documentID)
		if err != nil {
			return nil, err
		}
		version = v
	}
	doc, err := s.repo.Get(ctx, documentID, version)
	if err != nil {
		return nil, err
	}
	if doc.Status != StatusFailed {
		return nil, validation("only failed documents can be retried")
	}
	raw, err := s.readObject(ctx, doc.SourceURI)
	if err != nil {
		return nil, err
	}
	if delErr := s.vectors.DeleteVersion(ctx, documentID, version); delErr != nil {
		s.log.Warn("cleanup: delete vectors before retry", "document_id", documentID, "version", version, "err", delErr)
	}
	if err := s.repo.UpdateStatus(ctx, documentID, version, StatusUploaded, "", ""); err != nil {
		return nil, err
	}
	return s.startIngestReturningUploaded(ctx, doc, raw)
}

func (s *Service) CreateUpload(ctx context.Context, in CreateUploadInput) (*Document, error) {
	if len(in.Body) == 0 {
		return nil, validation("file cannot be empty")
	}
	if s.maxFileBytes > 0 && int64(len(in.Body)) > s.maxFileBytes {
		return nil, validation("file exceeds configured limit")
	}
	if !allowedFilename(in.Filename) {
		return nil, validation("unsupported file type")
	}
	mimeType := strings.TrimSpace(in.MimeType)
	if mimeType == "" {
		mimeType = http.DetectContentType(in.Body)
	}
	doc, err := s.createBase(ctx, in.Metadata, SourceKindUpload, in.Filename, mimeType)
	if err != nil {
		return nil, err
	}
	name := safeFilename(in.Filename)
	rawKey := s.rawObjectKey(doc.WorkspaceID, doc.DocumentID, doc.Version, name)
	doc.SourceURI = rawKey
	doc.MimeType = mimeType
	doc.OriginalFilename = in.Filename
	if err := s.store.PutObject(ctx, rawKey, in.Body); err != nil {
		return nil, err
	}
	if err := s.repo.CreateVersion(ctx, doc, linksFor(doc)); err != nil {
		if delErr := s.store.DeleteObject(ctx, rawKey); delErr != nil {
			s.log.Warn("cleanup: delete raw object after failed create", "key", rawKey, "err", delErr)
		}
		return nil, err
	}
	return s.startIngestReturningUploaded(ctx, doc, in.Body)
}

func (s *Service) CurateLinks(ctx context.Context, in CurateLinksInput) (*Document, error) {
	documentID := strings.TrimSpace(in.DocumentID)
	if documentID == "" {
		return nil, validation("document_id is required")
	}
	version := strings.TrimSpace(in.Version)
	if version == "" {
		v, err := s.repo.LatestVersion(ctx, documentID)
		if err != nil {
			return nil, err
		}
		version = v
	}
	source, err := s.repo.Get(ctx, documentID, version)
	if err != nil {
		return nil, err
	}
	raw, err := s.readObject(ctx, source.SourceURI)
	if err != nil {
		return nil, err
	}
	metadata := Metadata{
		WorkspaceID:     source.WorkspaceID,
		DocumentID:      source.DocumentID,
		ParentVersion:   source.Version,
		Title:           source.Title,
		DocumentType:    source.DocumentType,
		AuthorityLevel:  source.AuthorityLevel,
		LinkedFeatureID: source.LinkedFeatureID,
		LinkedRequestID: source.LinkedRequestID,
		UploadedBy:      source.UploadedBy,
		ActorRole:       in.ActorRole,
		Tags:            tagsFromJSON(source.TagsJSON),
		Notes:           source.Notes,
	}
	if in.ClearFeatureLink {
		metadata.LinkedFeatureID = ""
	} else if strings.TrimSpace(in.LinkedFeatureID) != "" {
		metadata.LinkedFeatureID = strings.TrimSpace(in.LinkedFeatureID)
	}
	if in.ClearRequestLink {
		metadata.LinkedRequestID = ""
	} else if strings.TrimSpace(in.LinkedRequestID) != "" {
		metadata.LinkedRequestID = strings.TrimSpace(in.LinkedRequestID)
	}
	if strings.TrimSpace(in.UploadedBy) != "" {
		metadata.UploadedBy = strings.TrimSpace(in.UploadedBy)
	}
	if strings.TrimSpace(in.Notes) != "" {
		metadata.Notes = strings.TrimSpace(in.Notes)
	}
	if metadata.LinkedFeatureID == source.LinkedFeatureID && metadata.LinkedRequestID == source.LinkedRequestID {
		return nil, validation("curation did not change document links")
	}
	switch source.SourceKind {
	case SourceKindText:
		return s.CreateText(ctx, CreateTextInput{Metadata: metadata, Content: string(raw)})
	case SourceKindUpload:
		return s.CreateUpload(ctx, CreateUploadInput{
			Metadata: metadata,
			Filename: source.OriginalFilename,
			MimeType: source.MimeType,
			Body:     raw,
		})
	default:
		return nil, validation("unsupported source kind")
	}
}

func (s *Service) List(ctx context.Context, f ListFilter) ([]Document, error) {
	return s.repo.List(ctx, f)
}

func (s *Service) Count(ctx context.Context, f ListFilter) (int, error) {
	return s.repo.Count(ctx, f)
}

func (s *Service) Detail(ctx context.Context, documentID, version string) (*Detail, error) {
	if version == "" {
		v, err := s.repo.LatestVersion(ctx, documentID)
		if err != nil {
			return nil, err
		}
		version = v
	}
	doc, err := s.repo.Get(ctx, documentID, version)
	if err != nil {
		return nil, err
	}
	history, err := s.repo.History(ctx, documentID)
	if err != nil {
		return nil, err
	}
	count, err := s.repo.ChunkCount(ctx, documentID, version)
	if err != nil {
		return nil, err
	}
	extracted := ""
	if doc.Status == StatusIndexed || doc.Status == StatusChunked || doc.Status == StatusEmbedded {
		if b, err := s.readObject(ctx, s.processedObjectKey(doc.WorkspaceID, documentID, version, "extracted.txt")); err == nil {
			extracted = string(b)
		}
	}
	return &Detail{Document: *doc, History: history, ChunkCount: count, ExtractedPreview: extracted}, nil
}

func (s *Service) Delete(ctx context.Context, documentID, version string) error {
	if version == "" {
		v, err := s.repo.LatestVersion(ctx, documentID)
		if err != nil {
			return err
		}
		version = v
	}
	doc, err := s.repo.Get(ctx, documentID, version)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteVersion(ctx, documentID, version); err != nil {
		return err
	}
	if delErr := s.vectors.DeleteVersion(ctx, documentID, version); delErr != nil {
		s.log.Warn("cleanup: delete vectors", "document_id", documentID, "version", version, "err", delErr)
	}
	for _, key := range []string{
		doc.SourceURI,
		s.processedObjectKey(doc.WorkspaceID, documentID, version, "extracted.txt"),
		s.processedObjectKey(doc.WorkspaceID, documentID, version, "chunks.json"),
	} {
		if delErr := s.store.DeleteObject(ctx, key); delErr != nil {
			s.log.Warn("cleanup: delete object", "key", key, "err", delErr)
		}
	}
	return nil
}

func (s *Service) Search(ctx context.Context, in SearchInput) ([]SearchResult, error) {
	workspaceID, valid := workspace.NormalizeID(in.WorkspaceID)
	if !valid {
		return nil, validation("workspace_id is required and must be a safe path segment")
	}
	if trustedWorkspaceID, ok := WorkspaceFromContext(ctx); ok && workspaceID != trustedWorkspaceID {
		return nil, validation("workspace_id does not match request workspace")
	}
	if strings.TrimSpace(in.Query) == "" {
		return nil, validation("query cannot be empty")
	}
	limit := in.MaxChunks
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	if limit > MaxSearchLimit {
		limit = MaxSearchLimit
	}
	dtypes := make([]string, 0, len(in.DocumentTypes))
	for _, t := range in.DocumentTypes {
		dtypes = append(dtypes, string(t))
	}
	auths := make([]string, 0, len(in.AuthorityLevels))
	for _, a := range in.AuthorityLevels {
		auths = append(auths, string(a))
	}
	qvec, err := s.embedder.Embed(ctx, in.Query, EmbeddingQuery)
	if err != nil {
		return nil, err
	}
	results, err := s.vectors.Search(ctx, VectorSearch{
		WorkspaceID:   workspaceID,
		Vector:        qvec,
		LinkedFeature: in.LinkedFeatureID,
		LinkedRequest: in.LinkedRequestID,
		DocumentTypes: dtypes,
		Authorities:   auths,
		LatestOnly:    !in.IncludeHistory,
		Limit:         limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(results))
	for _, r := range results {
		p := r.Payload
		documentID := strPayload(p, "document_id")
		version := strPayload(p, "version")
		doc, err := s.repo.Get(ctx, documentID, version)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if doc.WorkspaceID != workspaceID || doc.Status != StatusIndexed || (!in.IncludeHistory && !doc.IsLatest) {
			continue
		}
		out = append(out, SearchResult{
			WorkspaceID:    strPayload(p, "workspace_id"),
			DocumentID:     documentID,
			Version:        version,
			Title:          strPayload(p, "title"),
			DocumentType:   DocumentType(strPayload(p, "document_type")),
			AuthorityLevel: AuthorityLevel(strPayload(p, "authority_level")),
			ChunkText:      strPayload(p, "chunk_text"),
			Score:          r.Score,
			SourceURI:      strPayload(p, "source_uri"),
			ChunkIndex:     intPayload(p, "chunk_index"),
			Heading:        strPayload(p, "heading"),
			HeadingPath:    strSlicePayload(p, "heading_path"),
			SectionIndex:   intPayload(p, "section_index"),
		})
		if len(out) >= limit {
			break
		}
	}
	// Section-context expansion. Vector-score order is final — there is no rerank
	// stage in alpha. Chunk mode returns hit excerpts only; section/document mode
	// attach bounded context.
	mode := normalizeContextMode(in.ContextMode)
	maxChars := normalizeContextMaxChars(in.ContextMaxChars)
	if mode != ContextModeChunk {
		for i := range out {
			chunks, err := s.repo.ChunksForDocument(ctx, out[i].DocumentID, out[i].Version)
			if err != nil {
				return nil, err
			}
			if mode == ContextModeDocument {
				text, truncated := joinChunksCapped(chunks, maxChars)
				out[i].ContextKind = string(ContextModeDocument)
				if truncated {
					out[i].ContextKind = "document_capped"
				}
				out[i].ContextText = text
				continue
			}
			text, start, end := expandSectionContext(chunks, out[i].ChunkIndex, maxChars)
			out[i].ContextKind = string(ContextModeSection)
			out[i].ContextText = text
			out[i].StartChunkIndex = start
			out[i].EndChunkIndex = end
		}
	}
	return out, nil
}

func (s *Service) createBase(ctx context.Context, meta Metadata, kind SourceKind, filename, mimeType string) (*Document, error) {
	if err := validateMetadata(meta); err != nil {
		return nil, err
	}
	now := s.now()
	documentID := strings.TrimSpace(meta.DocumentID)
	version := strings.TrimSpace(meta.NewVersion)
	parent := strings.TrimSpace(meta.ParentVersion)
	if documentID == "" {
		documentID = "doc_" + uuid.NewString()
		if version == "" {
			version = "v1"
		}
	} else if version == "" {
		next, err := s.repo.NextMinorVersion(ctx, documentID)
		if err != nil {
			return nil, err
		}
		version = next
	}
	tags, _ := json.Marshal(cleanTags(meta.Tags))
	return &Document{
		DocumentID:       documentID,
		Version:          version,
		WorkspaceID:      strings.TrimSpace(meta.WorkspaceID),
		ParentVersion:    parent,
		IsLatest:         true,
		Title:            strings.TrimSpace(meta.Title),
		DocumentType:     meta.DocumentType,
		AuthorityLevel:   meta.AuthorityLevel,
		SourceKind:       kind,
		MimeType:         mimeType,
		OriginalFilename: filename,
		Status:           StatusUploaded,
		LinkedFeatureID:  strings.TrimSpace(meta.LinkedFeatureID),
		LinkedRequestID:  strings.TrimSpace(meta.LinkedRequestID),
		UploadedBy:       strings.TrimSpace(meta.UploadedBy),
		CreatedAt:        now,
		UpdatedAt:        now,
		Notes:            strings.TrimSpace(meta.Notes),
		TagsJSON:         string(tags),
	}, nil
}

func (s *Service) ingest(ctx context.Context, doc *Document, raw []byte) error {
	err := s.runIngest(ctx, doc, raw)
	if err != nil {
		s.markIngestFailed(ctx, doc, err)
	}
	return err
}

func (s *Service) runIngest(ctx context.Context, doc *Document, raw []byte) error {
	if err := s.repo.UpdateStatus(ctx, doc.DocumentID, doc.Version, StatusParsing, "", ""); err != nil {
		return fmt.Errorf("mark parsing: %w", err)
	}
	extracted, err := extractText(doc, raw)
	if err != nil {
		return fmt.Errorf("extract text: %w", err)
	}
	extractedKey := s.processedObjectKey(doc.WorkspaceID, doc.DocumentID, doc.Version, "extracted.txt")
	if err := s.store.PutObject(ctx, extractedKey, []byte(extracted)); err != nil {
		return fmt.Errorf("write extracted text: %w", err)
	}
	chunkItems := ChunkDocument(extracted)
	chunks := make([]Chunk, 0, len(chunkItems))
	points := make([]VectorPoint, 0, len(chunkItems))
	now := s.now()
	for i, item := range chunkItems {
		text := item.Text
		vec, err := s.embedder.Embed(ctx, text, EmbeddingDocument)
		if err != nil {
			return fmt.Errorf("embed chunk %d: %w", i, err)
		}
		pointID := uuid.NewString()
		headingPathJSON, _ := json.Marshal(item.HeadingPath)
		chunks = append(chunks, Chunk{
			ID:              uuid.NewString(),
			DocumentID:      doc.DocumentID,
			Version:         doc.Version,
			ChunkIndex:      i,
			ChunkText:       text,
			TokenCount:      tokenCount(text),
			Heading:         item.Heading,
			HeadingPathJSON: string(headingPathJSON),
			SectionIndex:    item.SectionIndex,
			CreatedAt:       now,
		})
		points = append(points, VectorPoint{
			ID:     pointID,
			Vector: vec,
			Payload: map[string]any{
				"workspace_id":      doc.WorkspaceID,
				"document_id":       doc.DocumentID,
				"version":           doc.Version,
				"is_latest":         doc.IsLatest,
				"title":             doc.Title,
				"document_type":     doc.DocumentType,
				"authority_level":   doc.AuthorityLevel,
				"linked_feature_id": doc.LinkedFeatureID,
				"linked_request_id": doc.LinkedRequestID,
				"chunk_index":       i,
				"chunk_text":        text,
				"heading":           item.Heading,
				"heading_path":      item.HeadingPath,
				"section_index":     item.SectionIndex,
				"source_kind":       doc.SourceKind,
				"source_uri":        extractedKey,
				"tags":              tagsFromJSON(doc.TagsJSON),
				"created_at":        doc.CreatedAt.Format(time.RFC3339),
			},
		})
	}
	if err := s.repo.ReplaceChunks(ctx, doc.DocumentID, doc.Version, chunks); err != nil {
		return fmt.Errorf("replace chunks: %w", err)
	}
	chunkBlob, _ := json.MarshalIndent(chunks, "", "  ")
	if err := s.store.PutObject(ctx, s.processedObjectKey(doc.WorkspaceID, doc.DocumentID, doc.Version, "chunks.json"), chunkBlob); err != nil {
		return fmt.Errorf("write chunks: %w", err)
	}
	if err := s.repo.UpdateStatus(ctx, doc.DocumentID, doc.Version, StatusChunked, summaryFor(extracted), ""); err != nil {
		return fmt.Errorf("mark chunked: %w", err)
	}
	if err := s.vectors.EnsureCollection(ctx); err != nil {
		return fmt.Errorf("ensure vector collection: %w", err)
	}
	if err := s.repo.UpdateStatus(ctx, doc.DocumentID, doc.Version, StatusEmbedded, "", ""); err != nil {
		return fmt.Errorf("mark embedded: %w", err)
	}
	if err := s.vectors.DeleteVersion(ctx, doc.DocumentID, doc.Version); err != nil {
		return fmt.Errorf("clear existing vectors: %w", err)
	}
	if err := s.vectors.Upsert(ctx, points); err != nil {
		return fmt.Errorf("upsert vectors: %w", err)
	}
	if err := s.repo.UpdateStatus(ctx, doc.DocumentID, doc.Version, StatusIndexed, summaryFor(extracted), ""); err != nil {
		return fmt.Errorf("mark indexed: %w", err)
	}
	return nil
}

func (s *Service) markIngestFailed(ctx context.Context, doc *Document, ingestErr error) {
	if ingestErr == nil {
		return
	}
	if err := s.repo.UpdateStatus(ctx, doc.DocumentID, doc.Version, StatusFailed, "", ingestErr.Error()); err != nil {
		if errors.Is(err, ErrNotFound) {
			s.cleanupProcessedVersion(ctx, doc)
			return
		}
		s.log.Warn("ingest: mark failed", "document_id", doc.DocumentID, "version", doc.Version, "err", err)
	}
}

func (s *Service) cleanupProcessedVersion(ctx context.Context, doc *Document) {
	if err := s.vectors.DeleteVersion(ctx, doc.DocumentID, doc.Version); err != nil {
		s.log.Warn("ingest cleanup: delete vectors", "document_id", doc.DocumentID, "version", doc.Version, "err", err)
	}
	for _, key := range []string{
		s.processedObjectKey(doc.WorkspaceID, doc.DocumentID, doc.Version, "extracted.txt"),
		s.processedObjectKey(doc.WorkspaceID, doc.DocumentID, doc.Version, "chunks.json"),
	} {
		if err := s.store.DeleteObject(ctx, key); err != nil {
			s.log.Warn("ingest cleanup: delete object", "key", key, "err", err)
		}
	}
}

func validateMetadata(m Metadata) error {
	if _, valid := workspace.NormalizeID(m.WorkspaceID); !valid {
		return validation("workspace_id is required and must be a safe path segment")
	}
	if documentID := strings.TrimSpace(m.DocumentID); documentID != "" {
		if _, valid := workspace.NormalizeID(documentID); !valid {
			return validation("document_id must be a safe opaque path segment")
		}
	}
	if strings.TrimSpace(m.Title) == "" {
		return validation("title is required")
	}
	if !validDocumentType(m.DocumentType) {
		return validation("document_type is required or invalid")
	}
	if !validAuthority(m.AuthorityLevel) {
		return validation("authority_level is required or invalid")
	}
	role := strings.TrimSpace(m.ActorRole)
	if m.AuthorityLevel == AuthoritySourceOfTruth && role != "" && role != "reviewer" && role != "admin" {
		return validation("source_of_truth requires reviewer or admin actor_role")
	}
	if m.NewVersion != "" && !validVersion(m.NewVersion) {
		return validation("new_version must look like v1 or v1.1")
	}
	return nil
}

func validDocumentType(v DocumentType) bool {
	switch v {
	case DocumentTypeProductBrief, DocumentTypeSRS, DocumentTypeDesignReference, DocumentTypeSupportingDoc, DocumentTypeExistingArtifact, DocumentTypeQAFinding, DocumentTypePolicyDoc:
		return true
	default:
		return false
	}
}

func validAuthority(v AuthorityLevel) bool {
	switch v {
	case AuthoritySourceOfTruth, AuthorityHigh, AuthorityReference, AuthorityLow:
		return true
	default:
		return false
	}
}

var versionRE = regexp.MustCompile(`^v\d+(\.\d+)?$`)

func validVersion(v string) bool { return versionRE.MatchString(v) }

func validation(msg string) error { return fmt.Errorf("%w: %s", ErrValidation, msg) }

func allowedFilename(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".txt":
		return true
	default:
		return false
	}
}

func safeFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == "/" || name == "" {
		return "original.file"
	}
	return name
}

func (s *Service) rawObjectKey(workspaceID, documentID, version, filename string) string {
	prefix := workspaceObjectPrefix(workspaceID)
	return fmt.Sprintf("%sdocuments/%s/%s/raw/%s/%s", prefix+s.keyPrefix, documentID, version, uuid.NewString(), filename)
}

func (s *Service) processedObjectKey(workspaceID, documentID, version, filename string) string {
	return fmt.Sprintf("%sdocuments/%s/%s/processed/%s", workspaceObjectPrefix(workspaceID)+s.keyPrefix, documentID, version, filename)
}

func workspaceObjectPrefix(workspaceID string) string {
	return "workspaces/" + strings.TrimSpace(workspaceID) + "/"
}

func extractText(doc *Document, raw []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(doc.OriginalFilename))
	if doc.SourceKind == SourceKindText || ext == ".md" || ext == ".txt" {
		return strings.TrimSpace(string(raw)), nil
	}
	return "", fmt.Errorf("unsupported file type for parsing; raw file was stored")
}

func linksFor(doc *Document) []Link {
	var out []Link
	if doc.LinkedFeatureID != "" {
		out = append(out, Link{ID: uuid.NewString(), DocumentID: doc.DocumentID, Version: doc.Version, EntityType: "feature", EntityID: doc.LinkedFeatureID, RelationType: "primary_context"})
	}
	if doc.LinkedRequestID != "" {
		out = append(out, Link{ID: uuid.NewString(), DocumentID: doc.DocumentID, Version: doc.Version, EntityType: "request", EntityID: doc.LinkedRequestID, RelationType: "primary_context"})
	}
	return out
}

func summaryFor(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	return truncate(text, 240)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func cleanTags(tags []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

func tagsFromJSON(src string) []string {
	var out []string
	_ = json.Unmarshal([]byte(src), &out)
	return out
}

func strPayload(p map[string]any, key string) string {
	if v, ok := p[key].(string); ok {
		return v
	}
	return ""
}

func intPayload(p map[string]any, key string) int {
	switch v := p[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

// strSlicePayload reads a []string payload value. The in-memory store keeps it
// as []string; JSONB round-trips (real pgvector) yield []any of strings.
func strSlicePayload(p map[string]any, key string) []string {
	switch v := p[key].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
