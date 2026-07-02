package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/specgate/doc-registry/internal/knowledge"
)

type mockKnowledgeService struct {
	searchFn func(ctx context.Context, in knowledge.SearchInput) ([]knowledge.SearchResult, error)
}

func (m *mockKnowledgeService) Search(ctx context.Context, in knowledge.SearchInput) ([]knowledge.SearchResult, error) {
	return m.searchFn(ctx, in)
}

func TestKnowledgeSearch_Basic(t *testing.T) {
	t.Parallel()
	svc := &mockKnowledgeService{
		searchFn: func(_ context.Context, in knowledge.SearchInput) ([]knowledge.SearchResult, error) {
			_ = in
			return []knowledge.SearchResult{
				{
					DocumentID:     "doc1",
					Version:        "v1",
					Title:          "Test Doc",
					DocumentType:   knowledge.DocumentTypeSRS,
					AuthorityLevel: knowledge.AuthorityHigh,
					ChunkText:      "some chunk text",
					Score:          0.95,
				},
			}, nil
		},
	}

	handler := NewKnowledgeSearchHandler(svc)
	result, err := handler(context.Background(), KnowledgeSearchInput{
		Query: "test query",
		Limit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}

	var output struct {
		Items []struct {
			ChunkText    string  `json:"chunk_text"`
			Score        float64 `json:"score"`
			DocumentID   string  `json:"document_id"`
			DocumentType string  `json:"document_type"`
		} `json:"items"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Items) != 1 {
		t.Fatalf("got %d items", len(output.Items))
	}
	if output.Items[0].Score != 0.95 {
		t.Errorf("score = %f", output.Items[0].Score)
	}
	var detailed struct {
		Items []struct {
			DocumentID     string `json:"document_id"`
			Version        string `json:"version"`
			Title          string `json:"title"`
			DocumentType   string `json:"document_type"`
			AuthorityLevel string `json:"authority_level"`
			ChunkText      string `json:"chunk_text"`
			SourceURI      string `json:"source_uri"`
			ChunkIndex     int    `json:"chunk_index"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(result), &detailed); err != nil {
		t.Fatal(err)
	}
	if detailed.Items[0].Title != "Test Doc" {
		t.Errorf("title = %q, want Test Doc", detailed.Items[0].Title)
	}
	if detailed.Items[0].Version != "v1" {
		t.Errorf("version = %q, want v1", detailed.Items[0].Version)
	}
	if detailed.Items[0].AuthorityLevel != string(knowledge.AuthorityHigh) {
		t.Errorf("authority_level = %q, want %q", detailed.Items[0].AuthorityLevel, knowledge.AuthorityHigh)
	}
	if output.Truncated {
		t.Error("should not be truncated")
	}
}

func TestKnowledgeSearch_Truncated(t *testing.T) {
	t.Parallel()
	svc := &mockKnowledgeService{
		searchFn: func(_ context.Context, in knowledge.SearchInput) ([]knowledge.SearchResult, error) {
			results := make([]knowledge.SearchResult, in.MaxChunks)
			for i := range results {
				results[i] = knowledge.SearchResult{DocumentID: "doc", ChunkText: "chunk"}
			}
			return results, nil
		},
	}

	handler := NewKnowledgeSearchHandler(svc)
	result, err := handler(context.Background(), KnowledgeSearchInput{
		Query: "test",
		Limit: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	var output struct {
		Items     []json.RawMessage `json:"items"`
		Truncated bool              `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatal(err)
	}
	if !output.Truncated {
		t.Error("expected truncated=true")
	}
	if len(output.Items) != 3 {
		t.Errorf("got %d items, want 3", len(output.Items))
	}
}
