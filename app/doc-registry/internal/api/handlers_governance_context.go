package api

import (
	"context"
	"time"

	"github.com/specgate/doc-registry/internal/knowledge"
)

const defaultGovernanceUploadPutTTL = 15 * time.Minute

func (h *Handlers) GovernanceContextSearch(ctx context.Context, in *GovernanceContextSearchInput) (*GovernanceContextSearchOutput, error) {
	if h.Knowledge == nil {
		return nil, notImplemented("governance_context_search")
	}
	dtypes := make([]knowledge.DocumentType, 0, len(in.Body.DocumentTypes))
	for _, t := range in.Body.DocumentTypes {
		dtypes = append(dtypes, knowledge.DocumentType(t))
	}
	auths := make([]knowledge.AuthorityLevel, 0, len(in.Body.AuthorityLevels))
	for _, a := range in.Body.AuthorityLevels {
		auths = append(auths, knowledge.AuthorityLevel(a))
	}
	results, err := h.Knowledge.Search(ctx, knowledge.SearchInput{
		Query:           in.Body.Query,
		LinkedFeatureID: in.Body.LinkedFeatureID,
		LinkedRequestID: in.Body.LinkedRequestID,
		DocumentTypes:   dtypes,
		AuthorityLevels: auths,
		MaxChunks:       in.Body.MaxChunks,
		IncludeHistory:  in.Body.IncludeHistory,
	})
	if err != nil {
		return nil, mapKnowledgeError("governance context search", err)
	}
	out := &GovernanceContextSearchOutput{}
	for _, r := range results {
		out.Body.Results = append(out.Body.Results, GovernanceContextResultDTO{
			DocumentID:     r.DocumentID,
			Version:        r.Version,
			Title:          r.Title,
			DocumentType:   string(r.DocumentType),
			AuthorityLevel: string(r.AuthorityLevel),
			ChunkText:      r.ChunkText,
			Score:          r.Score,
			SourceURI:      r.SourceURI,
			ChunkIndex:     r.ChunkIndex,
		})
	}
	return out, nil
}
