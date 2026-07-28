package knowledge

import (
	"context"
	"errors"
	"strings"

	"github.com/specgate/doc-registry/internal/workspace"
)

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
	// Section-context expansion. Vector-score order is final; there is no rerank
	// stage. Chunk mode returns hit excerpts only; section/document mode
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
