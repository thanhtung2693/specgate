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
func (e *GeminiEmbedder) Embed(ctx context.Context, text string, purpose EmbeddingPurpose) (vector []float32, err error) {
	defer func() {
		if err == nil {
			return
		}
		// External diagnostics can encode credentials; expose only safe causes.
		switch {
		case errors.Is(err, context.Canceled):
			err = fmt.Errorf("gemini embedContent: %w", context.Canceled)
		case errors.Is(err, context.DeadlineExceeded):
			err = fmt.Errorf("gemini embedContent: %w", context.DeadlineExceeded)
		}
	}()
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
	// Header-only authentication keeps transport URL diagnostics credential-free.
	u, err := url.Parse(e.BaseURL + "/models/" + url.PathEscape(e.Model) + ":embedContent")
	if err != nil {
		return nil, errors.New("gemini embedContent: invalid endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("gemini embedContent: invalid request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", e.APIKey)

	resp, err := e.client().Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err // The deferred boundary replaces the external cause.
		}
		return nil, errors.New("gemini embedContent: transport failed")
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, errors.New("gemini embedContent: response read failed")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Provider text is untrusted even after JSON decoding.
		return nil, fmt.Errorf("gemini embedContent: status %d", resp.StatusCode)
	}
	var parsed geminiEmbedResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, errors.New("gemini embedContent: invalid response JSON")
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, errors.New("gemini embedContent: provider error")
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
