package tools

import (
	"context"
	"encoding/json"

	"github.com/specgate/doc-registry/internal/knowledge"
)

// KnowledgeSearcher is the subset of knowledge.Service needed by the MCP tool.
type KnowledgeSearcher interface {
	Search(ctx context.Context, in knowledge.SearchInput) ([]knowledge.SearchResult, error)
}

// KnowledgeSearchInput matches the search_knowledge tool schema.
type KnowledgeSearchInput struct {
	Query           string   `json:"query"`
	LinkedFeatureID string   `json:"linked_feature_id,omitempty"`
	LinkedRequestID string   `json:"linked_request_id,omitempty"`
	DocumentTypes   []string `json:"document_types,omitempty"`
	AuthorityLevels []string `json:"authority_levels,omitempty"`
	Limit           int      `json:"limit,omitempty"`
	MaxChunkChars   int      `json:"max_chunk_chars,omitempty"`
}

type knowledgeResultItem struct {
	ChunkText      string  `json:"chunk_text"`
	Score          float64 `json:"score"`
	DocumentID     string  `json:"document_id"`
	Version        string  `json:"version"`
	Title          string  `json:"title"`
	DocumentType   string  `json:"document_type"`
	AuthorityLevel string  `json:"authority_level"`
	SourceURI      string  `json:"source_uri"`
	ChunkIndex     int     `json:"chunk_index"`
}

// NewKnowledgeSearchHandler returns a handler function for the search_knowledge tool.
func NewKnowledgeSearchHandler(svc KnowledgeSearcher) func(ctx context.Context, in KnowledgeSearchInput) (string, error) {
	return func(ctx context.Context, in KnowledgeSearchInput) (string, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 5
		}
		if limit > 20 {
			limit = 20
		}
		maxChunkChars := in.MaxChunkChars
		if maxChunkChars <= 0 {
			maxChunkChars = 800
		}
		if maxChunkChars > 3000 {
			maxChunkChars = 3000
		}

		queryLimit := limit + 1

		dtypes := make([]knowledge.DocumentType, 0, len(in.DocumentTypes))
		for _, t := range in.DocumentTypes {
			dtypes = append(dtypes, knowledge.DocumentType(t))
		}
		auths := make([]knowledge.AuthorityLevel, 0, len(in.AuthorityLevels))
		for _, a := range in.AuthorityLevels {
			auths = append(auths, knowledge.AuthorityLevel(a))
		}

		results, err := svc.Search(ctx, knowledge.SearchInput{
			Query:           in.Query,
			LinkedFeatureID: in.LinkedFeatureID,
			LinkedRequestID: in.LinkedRequestID,
			DocumentTypes:   dtypes,
			AuthorityLevels: auths,
			MaxChunks:       queryLimit,
			IncludeHistory:  false,
		})
		if err != nil {
			return "", err
		}

		truncated := len(results) > limit
		if truncated {
			results = results[:limit]
		}

		items := make([]knowledgeResultItem, 0, len(results))
		for _, r := range results {
			chunkText := r.ChunkText
			if len(chunkText) > maxChunkChars {
				chunkText = chunkText[:maxChunkChars] + "…"
			}
			items = append(items, knowledgeResultItem{
				ChunkText:      chunkText,
				Score:          r.Score,
				DocumentID:     r.DocumentID,
				Version:        r.Version,
				Title:          r.Title,
				DocumentType:   string(r.DocumentType),
				AuthorityLevel: string(r.AuthorityLevel),
				SourceURI:      r.SourceURI,
				ChunkIndex:     r.ChunkIndex,
			})
		}

		out, _ := json.Marshal(map[string]any{
			"items":     items,
			"truncated": truncated,
		})
		return string(out), nil
	}
}
