package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

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
