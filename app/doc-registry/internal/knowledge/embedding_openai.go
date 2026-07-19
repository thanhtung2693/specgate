package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIEmbedder calls the OpenAI-compatible embeddings API (POST /embeddings).
// It serves both the OpenAI provider and OpenRouter, which share the same wire
// shape — only the base URL and key differ.
type OpenAIEmbedder struct {
	APIKey     string
	Model      string
	Dim        int
	BaseURL    string // e.g. https://api.openai.com/v1 or https://openrouter.ai/api/v1
	HTTPClient *http.Client
}

// NewOpenAIEmbedder returns an embedder for an OpenAI-compatible endpoint. dim is
// sent as the `dimensions` parameter when > 0 (text-embedding-3-* support it) so
// the vector width matches KNOWLEDGE_EMBEDDING_DIM and the pgvector column width.
func NewOpenAIEmbedder(apiKey, model string, dim int, baseURL string) (*OpenAIEmbedder, error) {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return nil, errors.New("an API key is required for OpenAI-compatible embedding models")
	}
	m := strings.TrimSpace(model)
	if m == "" {
		return nil, errors.New("embedding model is required")
	}
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return nil, errors.New("embedding base URL is required")
	}
	return &OpenAIEmbedder{
		APIKey:     key,
		Model:      m,
		Dim:        dim,
		BaseURL:    base,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
	}, nil
}

// Embed implements Embedder. OpenAI-compatible embeddings do not distinguish
// document vs query, so purpose is ignored.
func (e *OpenAIEmbedder) Embed(ctx context.Context, text string, _ EmbeddingPurpose) ([]float32, error) {
	payload := map[string]any{
		"model": e.Model,
		"input": text,
	}
	if e.Dim > 0 {
		payload["dimensions"] = e.Dim
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.BaseURL+"/embeddings", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.APIKey)

	resp, err := e.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embeddings: status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var parsed openAIEmbedResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("embeddings: decode: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, fmt.Errorf("embeddings: %s", parsed.Error.Message)
	}
	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return nil, errors.New("embeddings: empty embedding")
	}
	vals := parsed.Data[0].Embedding
	out := make([]float32, len(vals))
	for i, v := range vals {
		out[i] = float32(v)
	}
	if e.Dim > 0 && len(out) != e.Dim {
		return nil, fmt.Errorf("embeddings: got dimension %d, want %d (check KNOWLEDGE_EMBEDDING_DIM / re-index after a model change)", len(out), e.Dim)
	}
	return out, nil
}

type openAIEmbedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (e *OpenAIEmbedder) client() *http.Client {
	if e.HTTPClient != nil {
		return e.HTTPClient
	}
	return http.DefaultClient
}
