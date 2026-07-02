package knowledge

import (
	"context"
	"errors"
)

// ErrEmbeddingsDisabled is returned by the disabled embedder when no embedding
// provider is configured (no GOOGLE_API_KEY / GEMINI_API_KEY). It lets Doc
// Registry boot without an embedding key: knowledge read paths keep working,
// while search and indexing report this clearly instead of crashing the server.
var ErrEmbeddingsDisabled = errors.New("knowledge embeddings are not configured (set GOOGLE_API_KEY or GEMINI_API_KEY)")

// DefaultEmbeddingDim is the default vector size when KNOWLEDGE_EMBEDDING_DIM is unset or invalid
// (Gemini embedContent outputDimensionality).
const DefaultEmbeddingDim = 1024

// EmbeddingPurpose selects retrieval behavior for providers that distinguish document vs query
// (e.g. Gemini embedContent taskType).
type EmbeddingPurpose int

const (
	// EmbeddingDocument is for indexing stored chunks.
	EmbeddingDocument EmbeddingPurpose = iota
	// EmbeddingQuery is for search queries.
	EmbeddingQuery
)

// Embedder turns text into a dense vector for the configured vector store.
type Embedder interface {
	Embed(ctx context.Context, text string, purpose EmbeddingPurpose) ([]float32, error)
}

// disabledEmbedder is the no-op Embedder used when no embedding key is set. Its
// Embed always fails with ErrEmbeddingsDisabled, so the server boots and serves
// knowledge read paths while search/indexing are soft-disabled.
type disabledEmbedder struct{}

func (disabledEmbedder) Embed(context.Context, string, EmbeddingPurpose) ([]float32, error) {
	return nil, ErrEmbeddingsDisabled
}

// EmbeddingsEnabled reports whether embeddings are usable. A settings-backed
// embedder answers from its current configuration; the static no-op is always
// disabled; any other concrete embedder is enabled. The knowledge UI uses this
// to warn and disable upload when embeddings are not configured.
func EmbeddingsEnabled(e Embedder) bool {
	if en, ok := e.(interface{ Enabled() bool }); ok {
		return en.Enabled()
	}
	_, disabled := e.(disabledEmbedder)
	return !disabled
}
