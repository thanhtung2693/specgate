package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// GeminiEmbedder calls the Google AI Gemini embedContent API (e.g. gemini-embedding-2-preview).
type GeminiEmbedder struct {
	APIKey     string
	Model      string
	Dim        int
	BaseURL    string
	HTTPClient *http.Client
}

// NewGeminiEmbedder returns an embedder for models named gemini-embedding-*.
// apiKey is typically GOOGLE_API_KEY (Google AI Studio). dim is passed as outputDimensionality
// when > 0; it must match KNOWLEDGE_EMBEDDING_DIM and the pgvector column width.
func NewGeminiEmbedder(apiKey, model string, dim int) (*GeminiEmbedder, error) {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return nil, errors.New("a Google API key is required for Gemini embedding models (set it in Settings → Models)")
	}
	m := strings.TrimSpace(model)
	if m == "" {
		return nil, errors.New("embedding model is required")
	}
	if dim <= 0 {
		dim = DefaultEmbeddingDim
	}
	base := strings.TrimRight(strings.TrimSpace(geminiGenerativeBaseURL()), "/")
	return &GeminiEmbedder{
		APIKey:     key,
		Model:      m,
		Dim:        dim,
		BaseURL:    base,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func geminiGenerativeBaseURL() string {
	if u := strings.TrimSpace(os.Getenv("GEMINI_API_BASE_URL")); u != "" {
		return u
	}
	return "https://generativelanguage.googleapis.com/v1beta"
}

// Embed implements Embedder.
func (e *GeminiEmbedder) Embed(ctx context.Context, text string, purpose EmbeddingPurpose) ([]float32, error) {
	taskType := "RETRIEVAL_DOCUMENT"
	if purpose == EmbeddingQuery {
		taskType = "RETRIEVAL_QUERY"
	}
	body := map[string]any{
		"taskType": taskType,
		"content": map[string]any{
			"parts": []map[string]any{{"text": text}},
		},
	}
	if e.Dim > 0 {
		body["outputDimensionality"] = e.Dim
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	// POST .../models/{model}:embedContent?key=...
	u, err := url.Parse(e.BaseURL + "/models/" + url.PathEscape(e.Model) + ":embedContent")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("key", e.APIKey)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", e.APIKey)

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
		return nil, fmt.Errorf("gemini embedContent: status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var parsed geminiEmbedResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("gemini embedContent: decode: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, fmt.Errorf("gemini embedContent: %s", parsed.Error.Message)
	}
	vals := parsed.Embedding.Values
	if len(vals) == 0 {
		return nil, errors.New("gemini embedContent: empty embedding")
	}
	out := make([]float32, len(vals))
	for i, v := range vals {
		out[i] = float32(v)
	}
	if e.Dim > 0 && len(out) != e.Dim {
		return nil, fmt.Errorf("gemini embedContent: got dimension %d, want %d (check KNOWLEDGE_EMBEDDING_DIM)", len(out), e.Dim)
	}
	return out, nil
}

type geminiEmbedResponse struct {
	Embedding struct {
		Values []float64 `json:"values"`
	} `json:"embedding"`
	Error *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error,omitempty"`
}

func (e *GeminiEmbedder) client() *http.Client {
	if e.HTTPClient != nil {
		return e.HTTPClient
	}
	return http.DefaultClient
}
