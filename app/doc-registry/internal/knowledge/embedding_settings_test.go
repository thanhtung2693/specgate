package knowledge

import (
	"context"
	"errors"
	"testing"
)

func TestSettingsEmbedder_DisabledWhenUnconfigured(t *testing.T) {
	cases := map[string]EmbeddingConfig{
		"all empty":   {},
		"no key":      {Provider: "openai", Model: "text-embedding-3-small"},
		"no model":    {Provider: "openai", APIKey: "sk-x"},
		"no provider": {Model: "text-embedding-3-small", APIKey: "sk-x"},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			e := NewSettingsEmbedder(func() EmbeddingConfig { return cfg }, 1024)
			if e.Enabled() {
				t.Fatalf("Enabled()=true, want false for %+v", cfg)
			}
			if EmbeddingsEnabled(e) {
				t.Fatalf("EmbeddingsEnabled()=true, want false for %+v", cfg)
			}
			if _, err := e.Embed(context.Background(), "hi", EmbeddingDocument); !errors.Is(err, ErrEmbeddingsDisabled) {
				t.Fatalf("Embed err=%v, want ErrEmbeddingsDisabled", err)
			}
		})
	}
}

func TestSettingsEmbedder_EnabledWhenConfigured(t *testing.T) {
	e := NewSettingsEmbedder(func() EmbeddingConfig {
		return EmbeddingConfig{Provider: "openai", Model: "text-embedding-3-small", APIKey: "sk-x"}
	}, 1024)
	if !e.Enabled() {
		t.Fatal("Enabled()=false, want true")
	}
	if !EmbeddingsEnabled(e) {
		t.Fatal("EmbeddingsEnabled()=false, want true")
	}
}

func TestBuildProviderEmbedder_Routing(t *testing.T) {
	cases := []struct {
		provider string
		ok       bool
	}{
		{"google_genai", true},
		{"openai", true},
		{"openrouter", true},
		{"anthropic", false}, // no embeddings API
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.provider, func(t *testing.T) {
			_, err := buildProviderEmbedder(EmbeddingConfig{Provider: c.provider, Model: "m", APIKey: "k"}, 1024)
			if c.ok && err != nil {
				t.Fatalf("provider %q: unexpected err %v", c.provider, err)
			}
			if !c.ok && err == nil {
				t.Fatalf("provider %q: want error, got nil", c.provider)
			}
		})
	}
}
