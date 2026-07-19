package command

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenRouterModelOptions_TextOutputOnly(t *testing.T) {
	t.Parallel()
	models := []openRouterModel{
		{
			ID:   "openai/gpt-text",
			Name: "OpenAI Text",
			Architecture: struct {
				OutputModalities []string `json:"output_modalities"`
			}{
				OutputModalities: []string{"text"},
			},
		},
		{
			ID:   "google/image-output",
			Name: "Google Image Output",
			Architecture: struct {
				OutputModalities []string `json:"output_modalities"`
			}{
				OutputModalities: []string{"image", "text"},
			},
		},
		{
			ID:   "vendor/audio-output",
			Name: "Audio Output",
			Architecture: struct {
				OutputModalities []string `json:"output_modalities"`
			}{
				OutputModalities: []string{"audio"},
			},
		},
		{
			ID:   "vendor/unknown",
			Name: "Unknown metadata",
			Architecture: struct {
				OutputModalities []string `json:"output_modalities"`
			}{},
		},
	}

	options := openRouterModelOptions(models)
	if len(options) != 2 {
		t.Fatalf("options len = %d, want text model plus manual entry: %#v", len(options), options)
	}
	if options[0].Value != "openai/gpt-text" {
		t.Fatalf("first option = %#v, want text model", options[0])
	}
	if options[1].Value != "" {
		t.Fatalf("last option should be manual entry, got %#v", options[1])
	}
}

func TestOpenRouterModelOptions_DoesNotInferFromLegacyModality(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"vendor/legacy","name":"Legacy metadata","architecture":{"modality":"text->text"}}]}`))
	}))
	t.Cleanup(server.Close)

	models, err := fetchOpenRouterModelsFrom(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	options := openRouterModelOptions(models)
	if len(options) != 1 || options[0].Value != "" {
		t.Fatalf("options = %#v, want manual entry only", options)
	}
}

func TestFetchOpenRouterModelsFrom_ParsesBoundedResponse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"openai/gpt-text","name":"OpenAI Text"}]}`))
	}))
	t.Cleanup(server.Close)

	models, err := fetchOpenRouterModelsFrom(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatalf("fetchOpenRouterModelsFrom() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "openai/gpt-text" {
		t.Fatalf("models = %#v", models)
	}
}

func TestFetchOpenRouterModelsFrom_RejectsOversizedResponse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":"`))
		_, _ = w.Write([]byte(strings.Repeat("x", maxOpenRouterModelsResponseBytes)))
		_, _ = w.Write([]byte(`"}`))
	}))
	t.Cleanup(server.Close)

	_, err := fetchOpenRouterModelsFrom(context.Background(), server.Client(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("fetchOpenRouterModelsFrom() error = %v, want size limit", err)
	}
}
