package knowledge

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewGeminiEmbedderRequiresAPIKey(t *testing.T) {
	t.Parallel()
	_, err := NewGeminiEmbedder("", "gemini-embedding-2-preview", 8)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGeminiEmbedderEmbed_Success(t *testing.T) {
	t.Parallel()
	want := []float64{0.1, 0.2, 0.3}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method %s", r.Method)
		}
		b, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(b, &req); err != nil {
			t.Errorf("decode: %v", err)
		}
		if req["taskType"] != "RETRIEVAL_QUERY" {
			t.Errorf("taskType=%v", req["taskType"])
		}
		if req["outputDimensionality"] != float64(3) {
			t.Errorf("outputDimensionality=%v", req["outputDimensionality"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embedding": map[string]any{"values": want},
		})
	}))
	defer srv.Close()

	e, err := NewGeminiEmbedder("fake-key", "gemini-embedding-2-preview", 3)
	if err != nil {
		t.Fatal(err)
	}
	e.BaseURL = srv.URL
	e.HTTPClient = srv.Client()

	vec, err := e.Embed(t.Context(), "hello", EmbeddingQuery)
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 3 {
		t.Fatalf("len=%d", len(vec))
	}
}
