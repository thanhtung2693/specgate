package knowledge

import (
	"bufio"
	"context"
	"encoding/json"
	"hash/fnv"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode"
)

type retrievalGoldRow struct {
	ID                    string `json:"id"`
	Mode                  string `json:"mode"`
	WorkspaceID           string `json:"workspace_id"`
	Query                 string `json:"query"`
	ExpectedDocumentID    string `json:"expected_document_id"`
	ExpectedChunkContains string `json:"expected_chunk_contains"`
}

func loadRetrievalGold(t *testing.T) []retrievalGoldRow {
	t.Helper()
	path := filepath.Join("..", "..", "evals", "knowledge", "retrieval_gold.jsonl")
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open gold set: %v", err)
	}
	defer file.Close()
	var rows []retrievalGoldRow
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row retrievalGoldRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("parse row: %v", err)
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan gold set: %v", err)
	}
	return rows
}

func TestKnowledgeRetrievalGoldSetIsWellFormed(t *testing.T) {
	rows := loadRetrievalGold(t)
	seen := map[string]bool{}
	for _, row := range rows {
		if strings.TrimSpace(row.ID) == "" ||
			(row.Mode != "offline" && row.Mode != "live") ||
			strings.TrimSpace(row.WorkspaceID) == "" ||
			strings.TrimSpace(row.Query) == "" ||
			strings.TrimSpace(row.ExpectedDocumentID) == "" ||
			strings.TrimSpace(row.ExpectedChunkContains) == "" {
			t.Fatalf("incomplete row: %+v", row)
		}
		if seen[row.ID] {
			t.Fatalf("duplicate row id %q", row.ID)
		}
		seen[row.ID] = true
	}
	if len(seen) < 4 {
		t.Fatalf("gold rows = %d, want at least 4", len(seen))
	}
}

// TestKnowledgeRetrievalGoldOfflineRows is a deterministic END-TO-END plumbing
// eval: it ingests one document per expected document id through the real
// Service (memory repo/store fakes + a cosine-ranking in-memory vector store),
// embedding with a bag-of-words fake embedder, then runs the real Service.Search
// scoped to the row's workspace and asserts the top hit is the expected document
// and its chunk contains the expected substring.
//
// This proves the ingest -> chunk -> embed -> scoped search -> citation pipeline
// works. It does NOT measure semantic or cross-language retrieval quality: the
// bag-of-words embedder only models literal token overlap. Only the opt-in live
// run (TestKnowledgeRetrievalGoldLiveRows) exercises real semantic embeddings.
func TestKnowledgeRetrievalGoldOfflineRows(t *testing.T) {
	svc := newGoldEvalService(t, bagOfWordsEmbedder{dim: 4096})
	rows := loadRetrievalGold(t)
	ran := 0
	for _, row := range rows {
		if row.Mode != "offline" {
			continue
		}
		ran++
		results, err := svc.Search(context.Background(), SearchInput{
			WorkspaceID: row.WorkspaceID,
			Query:       row.Query,
			MaxChunks:   3,
		})
		if err != nil {
			t.Fatalf("%s: %v", row.ID, err)
		}
		if len(results) == 0 || results[0].DocumentID != row.ExpectedDocumentID ||
			!strings.Contains(results[0].ChunkText, row.ExpectedChunkContains) {
			t.Fatalf("%s: top hit = %+v", row.ID, results)
		}
	}
	if ran == 0 {
		t.Fatal("no offline gold rows were exercised")
	}
}

// TestKnowledgeRetrievalGoldLiveRows is the opt-in bilingual (Vietnamese <-> English)
// calibration. It needs a real embedding model, so it is skipped unless
// KNOWLEDGE_LIVE_EVAL=1 (mirrors the gates live_smoke pattern). Only the live run
// can show cross-language retrieval works; the offline fake cannot.
func TestKnowledgeRetrievalGoldLiveRows(t *testing.T) {
	if os.Getenv("KNOWLEDGE_LIVE_EVAL") != "1" {
		t.Skip("set KNOWLEDGE_LIVE_EVAL=1 to run the live bilingual retrieval calibration")
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("live retrieval eval needs OPENAI_API_KEY (configure an embedding model) — skipping")
	}
	embedder, err := NewOpenAIEmbedder(apiKey, "text-embedding-3-small", 1536, "")
	if err != nil {
		t.Fatalf("build live embedder: %v", err)
	}
	svc := newGoldEvalService(t, embedder)
	rows := loadRetrievalGold(t)
	for _, row := range rows {
		if row.Mode != "live" {
			continue
		}
		results, err := svc.Search(context.Background(), SearchInput{
			WorkspaceID: row.WorkspaceID,
			Query:       row.Query,
			MaxChunks:   3,
		})
		if err != nil {
			t.Fatalf("%s: %v", row.ID, err)
		}
		if len(results) == 0 || results[0].DocumentID != row.ExpectedDocumentID {
			t.Fatalf("%s: top hit = %+v", row.ID, results)
		}
	}
}

// newGoldEvalService wires the real Service over memory fakes and seeds the two
// gold documents in ws-eval. The seeded content must contain the gold rows'
// expected_chunk_contains substrings.
func newGoldEvalService(t *testing.T, embedder Embedder) *Service {
	t.Helper()
	repo := newMemoryRepo()
	store := &memoryStore{objects: map[string][]byte{}}
	svc, err := NewService(repo, store, &rankingVectors{}, embedder, 1<<20, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	seed := func(docID, content string) {
		if _, err := svc.CreateText(ctx, CreateTextInput{
			Metadata: Metadata{
				DocumentID:     docID,
				NewVersion:     "v1",
				WorkspaceID:    "ws-eval",
				Title:          docID,
				DocumentType:   DocumentTypeSRS,
				AuthorityLevel: AuthorityHigh,
			},
			Content: content,
		}); err != nil {
			t.Fatalf("seed %s: %v", docID, err)
		}
	}
	seed("doc-refunds", "refunds require reviewer approval before a payout is issued.")
	seed("doc-loyalty", "platinum tier changes need manual review by an admin.")
	return svc
}

// bagOfWordsEmbedder is a deterministic fake: each lowercased word token sets a
// bucket in a fixed-width vector, so a query and a chunk that share tokens get a
// nonzero cosine similarity. It models literal overlap only, not semantics.
type bagOfWordsEmbedder struct{ dim int }

func (e bagOfWordsEmbedder) Embed(_ context.Context, text string, _ EmbeddingPurpose) ([]float32, error) {
	v := make([]float32, e.dim)
	for _, tok := range wordTokens(text) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(tok))
		v[int(h.Sum32()%uint32(e.dim))] = 1
	}
	return v, nil
}

func wordTokens(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// rankingVectors is an in-memory VectorStore that ranks by cosine similarity,
// applying the workspace_id payload filter. Unlike the service_test memory
// double it actually orders results, which the offline retrieval eval needs.
type rankingVectors struct{ points []VectorPoint }

func (v *rankingVectors) EnsureCollection(context.Context) error { return nil }

func (v *rankingVectors) Upsert(_ context.Context, pts []VectorPoint) error {
	v.points = append(v.points, pts...)
	return nil
}

func (v *rankingVectors) DeleteVersion(_ context.Context, id, version string) error {
	kept := v.points[:0]
	for _, p := range v.points {
		if p.Payload["document_id"] == id && p.Payload["version"] == version {
			continue
		}
		kept = append(kept, p)
	}
	v.points = kept
	return nil
}

func (v *rankingVectors) Search(_ context.Context, in VectorSearch) ([]VectorResult, error) {
	type scored struct {
		p     VectorPoint
		score float64
	}
	hits := make([]scored, 0, len(v.points))
	for _, p := range v.points {
		if in.WorkspaceID != "" && p.Payload["workspace_id"] != in.WorkspaceID {
			continue
		}
		hits = append(hits, scored{p: p, score: cosineSimilarity(in.Vector, p.Vector)})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	limit := in.Limit
	if limit <= 0 || limit > len(hits) {
		limit = len(hits)
	}
	out := make([]VectorResult, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, VectorResult{ID: hits[i].p.ID, Score: hits[i].score, Payload: hits[i].p.Payload})
	}
	return out, nil
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
