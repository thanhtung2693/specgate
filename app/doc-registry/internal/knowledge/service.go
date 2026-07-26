package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

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
