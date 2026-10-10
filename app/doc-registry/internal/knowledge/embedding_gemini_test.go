package knowledge

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
		if r.Header.Get("x-goog-api-key") != "fake-key" || r.URL.RawQuery != "" {
			t.Error("embedding authentication must use the header, not the URL")
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

type geminiFailureTransport struct{ reflectedCause error }

func (transport geminiFailureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if transport.reflectedCause != nil {
		return nil, fmt.Errorf("provider key %s: %w", req.Header.Get("x-goog-api-key"), transport.reflectedCause)
	}
	return nil, context.DeadlineExceeded
}

func TestGeminiReflectedTransportErrorsKeepOnlySafeCancellationCauses(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			t.Parallel()
			e, err := NewGeminiEmbedder("synthetic-google-secret", "gemini-embedding-2-preview", 3)
			if err != nil {
				t.Fatal(err)
			}
			e.HTTPClient = &http.Client{Transport: geminiFailureTransport{reflectedCause: cause}}
			_, err = e.Embed(t.Context(), "hello", EmbeddingDocument)
			if !errors.Is(err, cause) {
				t.Fatalf("lost cancellation classification: %v", err)
			}
			for current := err; current != nil; current = errors.Unwrap(current) {
				if strings.Contains(current.Error(), e.APIKey) {
					t.Fatal("returned error chain retains embedding credential")
				}
			}
		})
	}
}

func TestGeminiEmbeddingTransportFailureDoesNotExposeAPIKey(t *testing.T) {
	t.Parallel()
	e, err := NewGeminiEmbedder("synthetic-google-secret", "gemini-embedding-2-preview", 3)
	if err != nil {
		t.Fatal(err)
	}
	e.HTTPClient = &http.Client{Transport: geminiFailureTransport{}}
	_, err = e.Embed(t.Context(), "hello", EmbeddingDocument)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected preserved timeout cause, got %v", err)
	}
	if strings.Contains(err.Error(), "synthetic-google-secret") {
		t.Fatal("transport diagnostic exposes embedding credential")
	}
}

func TestGeminiEmbeddingProviderErrorsDoNotExposeAPIKey(t *testing.T) {
	t.Parallel()
	const key = "synthetic secret/+"
	for _, status := range []int{http.StatusBadRequest, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
					"message": "invalid key " + key + " encoded " + url.QueryEscape(key),
				}})
			}))
			defer srv.Close()
			e, err := NewGeminiEmbedder(key, "gemini-embedding-2-preview", 3)
			if err != nil {
				t.Fatal(err)
			}
			e.BaseURL, e.HTTPClient = srv.URL, srv.Client()
			_, err = e.Embed(t.Context(), "hello", EmbeddingDocument)
			want := "gemini embedContent: provider error"
			if status != http.StatusOK {
				want = "gemini embedContent: status 400"
			}
			if err == nil || err.Error() != want {
				t.Fatalf("expected safe provider diagnostic, got %v", err)
			}
			for _, secret := range []string{key, url.QueryEscape(key)} {
				if strings.Contains(err.Error(), secret) {
					t.Fatal("provider diagnostic exposes embedding credential")
				}
			}
		})
	}
}

func TestGeminiIngestFailurePersistsCredentialFreeDiagnostics(t *testing.T) {
	t.Parallel()
	for _, async := range []bool{false, true} {
		name := "inline"
		if async {
			name = "worker"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e, err := NewGeminiEmbedder("synthetic-google-secret", "gemini-embedding-2-preview", 3)
			if err != nil {
				t.Fatal(err)
			}
			e.HTTPClient = &http.Client{Transport: geminiFailureTransport{}}
			repo := newMemoryRepo()
			svc, err := NewService(repo, &memoryStore{objects: map[string][]byte{}}, &memoryVectors{}, e, 1024, "")
			if err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			svc.log = slog.New(slog.NewTextHandler(&logs, nil))
			enqueuer := &fakeEnqueuer{}
			if async {
				svc.WithIngestEnqueuer(enqueuer)
			}
			created, err := svc.CreateText(t.Context(), CreateTextInput{
				Metadata: Metadata{WorkspaceID: "ws-a", Title: "A", DocumentType: DocumentTypeSRS, AuthorityLevel: AuthorityHigh},
				Content:  "refunds require reviewer approval",
			})
			if err != nil {
				t.Fatal(err)
			}
			if async {
				if len(enqueuer.tasks) != 1 {
					t.Fatal("expected one queued ingest task")
				}
				err = svc.ProcessKnowledgeIngest(t.Context(), enqueuer.tasks[0])
				if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), e.APIKey) {
					t.Fatal("worker must return a credential-free timeout")
				}
			}
			persisted, err := repo.Get(t.Context(), created.DocumentID, created.Version)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Status != StatusFailed || !strings.Contains(persisted.ErrorMessage, "deadline exceeded") {
				t.Fatal("ingest must preserve truthful failed diagnostics")
			}
			if strings.Contains(persisted.ErrorMessage+logs.String(), e.APIKey) {
				t.Fatal("persisted or logged ingest error exposes embedding credential")
			}
		})
	}
}

func TestGeminiProviderJSONEscapesCannotBypassCredentialRedaction(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"invalid key \u0073ynthetic-google-secret"}}`)
	}))
	defer srv.Close()
	e, err := NewGeminiEmbedder("synthetic-google-secret", "gemini-embedding-2-preview", 3)
	if err != nil {
		t.Fatal(err)
	}
	e.BaseURL, e.HTTPClient = srv.URL, srv.Client()
	_, err = e.Embed(t.Context(), "hello", EmbeddingDocument)
	if err == nil || err.Error() != "gemini embedContent: status 400" {
		t.Fatalf("expected safe provider diagnostic, got %v", err)
	}
}

func TestGeminiProviderEncodedCredentialIsNotReturned(t *testing.T) {
	t.Parallel()
	const key = "synthetic-google-secret"
	encoded := base64.StdEncoding.EncodeToString([]byte(key))
	for _, status := range []int{http.StatusUnauthorized, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "credential " + encoded}})
			}))
			defer srv.Close()
			e, err := NewGeminiEmbedder(key, "gemini-embedding-2-preview", 3)
			if err != nil {
				t.Fatal(err)
			}
			e.BaseURL, e.HTTPClient = srv.URL, srv.Client()
			_, err = e.Embed(t.Context(), "hello", EmbeddingDocument)
			want := "gemini embedContent: provider error"
			if status == http.StatusUnauthorized {
				want = "gemini embedContent: status 401"
			}
			if err == nil || err.Error() != want {
				t.Fatalf("expected status diagnostic, got %v", err)
			}
			if strings.Contains(err.Error(), encoded) {
				t.Fatal("encoded credential escaped the provider boundary")
			}
		})
	}
}

func TestGeminiTransportOpaqueDiagnosticIsNotReturned(t *testing.T) {
	t.Parallel()
	e, err := NewGeminiEmbedder("synthetic-google-secret", "gemini-embedding-2-preview", 3)
	if err != nil {
		t.Fatal(err)
	}
	e.HTTPClient = &http.Client{Transport: geminiFailureTransport{reflectedCause: errors.New("opaque sensitive diagnostic")}}
	_, err = e.Embed(t.Context(), "hello", EmbeddingDocument)
	if err == nil || err.Error() != "gemini embedContent: transport failed" || errors.Unwrap(err) != nil {
		t.Fatalf("expected safe cause-free transport error, got %v", err)
	}
}
