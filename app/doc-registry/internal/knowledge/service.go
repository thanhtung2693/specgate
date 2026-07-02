package knowledge

import (
	"context"
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
	// ListByFeatureOrRequest returns all document versions where
	// linked_feature_id = featureID OR linked_request_id = requestID.
	// Either arg may be empty; both empty returns nil without a DB call.
	// All versions are returned (not filtered by is_latest) so callers can
	// derive freshness from each row's IsLatest field.
	ListByFeatureOrRequest(context.Context, string, string) ([]Document, error)
	History(context.Context, string) ([]Document, error)
	UpdateStatus(context.Context, string, string, Status, string, string) error
	ReplaceChunks(context.Context, string, string, []Chunk) error
	ChunkCount(context.Context, string, string) (int, error)
	DeleteVersion(context.Context, string, string) error
	LatestVersion(context.Context, string) (string, error)
	NextMinorVersion(context.Context, string) (string, error)
}

type ObjectStore interface {
	PutObject(ctx context.Context, key string, body []byte) error
	GetObject(ctx context.Context, key string) ([]byte, error)
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

func (s *Service) CreateText(ctx context.Context, in CreateTextInput) (*Document, error) {
	if strings.TrimSpace(in.Content) == "" {
		return nil, validation("content cannot be empty")
	}
	doc, err := s.createBase(ctx, in.Metadata, SourceKindText, "input.txt", "text/plain")
	if err != nil {
		return nil, err
	}
	rawKey := s.rawObjectKey(doc.DocumentID, doc.Version, "input.txt")
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
	if err := s.ingest(ctx, doc, []byte(in.Content)); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, doc.DocumentID, doc.Version)
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
	rawKey := s.rawObjectKey(doc.DocumentID, doc.Version, name)
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
	if err := s.ingest(ctx, doc, in.Body); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, doc.DocumentID, doc.Version)
}

func (s *Service) List(ctx context.Context, f ListFilter) ([]Document, error) {
	return s.repo.List(ctx, f)
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
		if b, err := s.store.GetObject(ctx, s.processedObjectKey(documentID, version, "extracted.txt")); err == nil {
			extracted = string(b)
		}
	}
	return &Detail{Document: *doc, History: history, ChunkCount: count, ExtractedPreview: extracted}, nil
}

func (s *Service) Reindex(ctx context.Context, documentID, version string) (*Document, error) {
	doc, err := s.repo.Get(ctx, documentID, version)
	if err != nil {
		return nil, err
	}
	body, err := s.store.GetObject(ctx, doc.SourceURI)
	if err != nil {
		return nil, err
	}
	if delErr := s.vectors.DeleteVersion(ctx, documentID, version); delErr != nil {
		s.log.Warn("cleanup: delete vectors before reindex", "document_id", documentID, "version", version, "err", delErr)
	}
	if err := s.ingest(ctx, doc, body); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, documentID, version)
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
	if delErr := s.vectors.DeleteVersion(ctx, documentID, version); delErr != nil {
		s.log.Warn("cleanup: delete vectors", "document_id", documentID, "version", version, "err", delErr)
	}
	for _, key := range []string{
		doc.SourceURI,
		s.processedObjectKey(documentID, version, "extracted.txt"),
		s.processedObjectKey(documentID, version, "chunks.json"),
	} {
		if delErr := s.store.DeleteObject(ctx, key); delErr != nil {
			s.log.Warn("cleanup: delete object", "key", key, "err", delErr)
		}
	}
	return s.repo.DeleteVersion(ctx, documentID, version)
}

func (s *Service) Search(ctx context.Context, in SearchInput) ([]SearchResult, error) {
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
		if !in.IncludeHistory {
			doc, err := s.repo.Get(ctx, documentID, version)
			if err != nil || !doc.IsLatest {
				continue
			}
		}
		out = append(out, SearchResult{
			DocumentID:     documentID,
			Version:        version,
			Title:          strPayload(p, "title"),
			DocumentType:   DocumentType(strPayload(p, "document_type")),
			AuthorityLevel: AuthorityLevel(strPayload(p, "authority_level")),
			ChunkText:      strPayload(p, "chunk_text"),
			Score:          r.Score,
			SourceURI:      strPayload(p, "source_uri"),
			ChunkIndex:     intPayload(p, "chunk_index"),
		})
		if len(out) >= limit {
			break
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
	if err := s.repo.UpdateStatus(ctx, doc.DocumentID, doc.Version, StatusParsing, "", ""); err != nil {
		return err
	}
	extracted, err := extractText(doc, raw)
	if err != nil {
		msg := err.Error()
		if statusErr := s.repo.UpdateStatus(ctx, doc.DocumentID, doc.Version, StatusFailed, "", msg); statusErr != nil {
			s.log.Warn("ingest: mark failed after extract error", "document_id", doc.DocumentID, "err", statusErr)
		}
		return nil
	}
	extractedKey := s.processedObjectKey(doc.DocumentID, doc.Version, "extracted.txt")
	if err := s.store.PutObject(ctx, extractedKey, []byte(extracted)); err != nil {
		return err
	}
	chunkTexts := ChunkText(extracted)
	chunks := make([]Chunk, 0, len(chunkTexts))
	points := make([]VectorPoint, 0, len(chunkTexts))
	now := s.now()
	for i, text := range chunkTexts {
		vec, err := s.embedder.Embed(ctx, text, EmbeddingDocument)
		if err != nil {
			msg := err.Error()
			if statusErr := s.repo.UpdateStatus(ctx, doc.DocumentID, doc.Version, StatusFailed, "", msg); statusErr != nil {
				s.log.Warn("ingest: mark failed after embed error", "document_id", doc.DocumentID, "err", statusErr)
			}
			return err
		}
		pointID := uuid.NewString()
		chunks = append(chunks, Chunk{
			ID:         uuid.NewString(),
			DocumentID: doc.DocumentID,
			Version:    doc.Version,
			ChunkIndex: i,
			ChunkText:  text,
			TokenCount: tokenCount(text),
			CreatedAt:  now,
		})
		points = append(points, VectorPoint{
			ID:     pointID,
			Vector: vec,
			Payload: map[string]any{
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
				"source_kind":       doc.SourceKind,
				"source_uri":        extractedKey,
				"tags":              tagsFromJSON(doc.TagsJSON),
				"created_at":        doc.CreatedAt.Format(time.RFC3339),
			},
		})
	}
	if err := s.repo.ReplaceChunks(ctx, doc.DocumentID, doc.Version, chunks); err != nil {
		return err
	}
	chunkBlob, _ := json.MarshalIndent(chunks, "", "  ")
	if err := s.store.PutObject(ctx, s.processedObjectKey(doc.DocumentID, doc.Version, "chunks.json"), chunkBlob); err != nil {
		return err
	}
	if err := s.repo.UpdateStatus(ctx, doc.DocumentID, doc.Version, StatusChunked, summaryFor(extracted), ""); err != nil {
		return err
	}
	if err := s.vectors.EnsureCollection(ctx); err != nil {
		return err
	}
	if err := s.repo.UpdateStatus(ctx, doc.DocumentID, doc.Version, StatusEmbedded, "", ""); err != nil {
		return err
	}
	if err := s.vectors.DeleteVersion(ctx, doc.DocumentID, doc.Version); err != nil {
		return err
	}
	if err := s.vectors.Upsert(ctx, points); err != nil {
		return err
	}
	return s.repo.UpdateStatus(ctx, doc.DocumentID, doc.Version, StatusIndexed, summaryFor(extracted), "")
}

func validateMetadata(m Metadata) error {
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

func (s *Service) rawObjectKey(documentID, version, filename string) string {
	if filename == "input.txt" {
		return fmt.Sprintf("%sdocuments/%s/%s/raw/input.txt", s.keyPrefix, documentID, version)
	}
	return fmt.Sprintf("%sdocuments/%s/%s/raw/%s", s.keyPrefix, documentID, version, filename)
}

func (s *Service) processedObjectKey(documentID, version, filename string) string {
	return fmt.Sprintf("%sdocuments/%s/%s/processed/%s", s.keyPrefix, documentID, version, filename)
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
