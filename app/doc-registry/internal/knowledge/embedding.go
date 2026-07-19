package knowledge

import (
	"context"
	"errors"
)

// ErrEmbeddingsDisabled is returned when no embedding provider is configured.
// It lets Doc
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

// EmbeddingsEnabled reports whether embeddings are usable. A settings-backed
// embedder answers from its current configuration; any other concrete embedder
// is enabled. The knowledge UI uses this to warn and disable upload when
// embeddings are not configured.
func EmbeddingsEnabled(e Embedder) bool {
	if en, ok := e.(interface{ Enabled() bool }); ok {
		return en.Enabled()
	}
	return true
}
