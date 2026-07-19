package api

import (
	"context"
	"fmt"
	"time"

	"github.com/specgate/doc-registry/internal/knowledge"
)

const defaultGovernanceUploadPutTTL = 15 * time.Minute

func (h *Handlers) GovernanceContextSearch(ctx context.Context, in *GovernanceContextSearchInput) (*GovernanceContextSearchOutput, error) {
	if h.Knowledge == nil {
		return nil, notImplemented("governance_context_search")
	}
	if err := h.requireKnownKnowledgeWorkspace(ctx, in.Body.WorkspaceID); err != nil {
		return nil, err
	}
	ctx = knowledge.WithWorkspace(ctx, in.Body.WorkspaceID)
	dtypes := make([]knowledge.DocumentType, 0, len(in.Body.DocumentTypes))
	for _, t := range in.Body.DocumentTypes {
		dtypes = append(dtypes, knowledge.DocumentType(t))
	}
	auths := make([]knowledge.AuthorityLevel, 0, len(in.Body.AuthorityLevels))
	for _, a := range in.Body.AuthorityLevels {
		auths = append(auths, knowledge.AuthorityLevel(a))
	}
	results, err := h.Knowledge.Search(ctx, knowledge.SearchInput{
		WorkspaceID:     in.Body.WorkspaceID,
		Query:           in.Body.Query,
		LinkedFeatureID: in.Body.LinkedFeatureID,
		LinkedRequestID: in.Body.LinkedRequestID,
		DocumentTypes:   dtypes,
		AuthorityLevels: auths,
		MaxChunks:       in.Body.MaxChunks,
		IncludeHistory:  in.Body.IncludeHistory,
		ContextMode:     knowledge.ContextMode(in.Body.ContextMode),
		ContextMaxChars: in.Body.ContextMaxChars,
	})
	if err != nil {
		return nil, mapKnowledgeError("governance context search", err)
	}
	out := &GovernanceContextSearchOutput{}
	out.Body.Results = []GovernanceContextResultDTO{}
	for _, r := range results {
		out.Body.Results = append(out.Body.Results, GovernanceContextResultDTO{
			Kind:            "knowledge",
			WorkspaceID:     r.WorkspaceID,
			DocumentID:      r.DocumentID,
			Version:         r.Version,
			Title:           r.Title,
			DocumentType:    string(r.DocumentType),
			AuthorityLevel:  string(r.AuthorityLevel),
			ChunkText:       r.ChunkText,
			Score:           r.Score,
			SourceURI:       r.SourceURI,
			ChunkIndex:      r.ChunkIndex,
			URL:             fmt.Sprintf("specgate://knowledge/%s/%s#chunk-%d", r.DocumentID, r.Version, r.ChunkIndex),
			ContextText:     r.ContextText,
			ContextKind:     r.ContextKind,
			Heading:         r.Heading,
			HeadingPath:     r.HeadingPath,
			SectionIndex:    r.SectionIndex,
			StartChunkIndex: r.StartChunkIndex,
			EndChunkIndex:   r.EndChunkIndex,
		})
	}
	return out, nil
}
