package knowledge

import (
	"context"
	"fmt"
	"sync"
)

// EmbeddingConfig is the resolved embedding model selection from settings.
// Empty Provider/Model/APIKey means embeddings are not configured (disabled).
type EmbeddingConfig struct {
	Provider string // "openai" | "google_genai" | "openrouter"
	Model    string
	APIKey   string
}

func (c EmbeddingConfig) configured() bool {
	return c.Provider != "" && c.Model != "" && c.APIKey != ""
}

// EmbeddingResolver reads the current embedding selection (from the settings
// store). It is called on each Embed so a change in Settings → Models takes
// effect without restarting the server.
type EmbeddingResolver func() EmbeddingConfig

// SettingsEmbedder is an Embedder that resolves its provider/model/key from
// settings at call time and delegates to the matching provider embedder. When
// nothing is configured it reports disabled (ErrEmbeddingsDisabled), so the
// server boots with no embedding key and the UI can warn + disable upload.
type SettingsEmbedder struct {
	resolve EmbeddingResolver
	dim     int

	mu       sync.Mutex
	sig      string
	delegate Embedder
}

// NewSettingsEmbedder builds a settings-backed embedder. dim is the configured
// pgvector column width (KNOWLEDGE_EMBEDDING_DIM); providers that support it
// produce vectors at this width so the collection stays stable across models.
func NewSettingsEmbedder(resolve EmbeddingResolver, dim int) *SettingsEmbedder {
	return &SettingsEmbedder{resolve: resolve, dim: dim}
}

// Enabled reports whether a usable embedding model is configured.
func (e *SettingsEmbedder) Enabled() bool {
	return e.resolve().configured()
}

// Embed implements Embedder.
func (e *SettingsEmbedder) Embed(ctx context.Context, text string, purpose EmbeddingPurpose) ([]float32, error) {
	delegate, err := e.current()
	if err != nil {
		return nil, err
	}
	return delegate.Embed(ctx, text, purpose)
}

// current returns the provider embedder for the active settings, rebuilding it
// only when the selection changes.
func (e *SettingsEmbedder) current() (Embedder, error) {
	cfg := e.resolve()
	if !cfg.configured() {
		return nil, ErrEmbeddingsDisabled
	}
	sig := cfg.Provider + "|" + cfg.Model + "|" + cfg.APIKey

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.delegate != nil && sig == e.sig {
		return e.delegate, nil
	}
	delegate, err := buildProviderEmbedder(cfg, e.dim)
	if err != nil {
		return nil, err
	}
	e.sig = sig
	e.delegate = delegate
	return delegate, nil
}

func buildProviderEmbedder(cfg EmbeddingConfig, dim int) (Embedder, error) {
	switch cfg.Provider {
	case "google_genai":
		return NewGeminiEmbedder(cfg.APIKey, cfg.Model, dim)
	case "openai":
		return NewOpenAIEmbedder(cfg.APIKey, cfg.Model, dim, "https://api.openai.com/v1")
	case "openrouter":
		return NewOpenAIEmbedder(cfg.APIKey, cfg.Model, dim, "https://openrouter.ai/api/v1")
	default:
		return nil, fmt.Errorf("unsupported embedding provider %q", cfg.Provider)
	}
}
