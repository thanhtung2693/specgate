package pgvector_test

import (
	"context"
	"testing"
	"time"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/specgate/doc-registry/internal/knowledge"
	pgvec "github.com/specgate/doc-registry/internal/storage/pgvector"
)

// startPGVector boots a pgvector container and returns a ready *pgvec.Store.
// The test is skipped if Docker is unavailable.
func startPGVector(t *testing.T, dim int) *pgvec.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	container, err := tcpostgres.Run(
		ctx,
		"pgvector/pgvector:0.8.3-pg18",
		tcpostgres.WithDatabase("pgvectest"),
		tcpostgres.WithUsername("docreg"),
		tcpostgres.WithPassword("docreg"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Skipf("skipping pgvector test (no docker?): %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	dsn, err := container.ConnectionString(context.Background(), "sslmode=disable")
	if err != nil {
		t.Fatalf("pgvector container DSN: %v", err)
	}

	store, err := pgvec.New(context.Background(), dsn, dim)
	if err != nil {
		t.Fatalf("pgvec.New: %v", err)
	}
	return store
}

// TestStore_EnsureCollection_Idempotent verifies that EnsureCollection can be
// called twice without error.
func TestStore_EnsureCollection_Idempotent(t *testing.T) {
	store := startPGVector(t, 4)
	ctx := context.Background()

	if err := store.EnsureCollection(ctx); err != nil {
		t.Fatalf("first EnsureCollection: %v", err)
	}
	if err := store.EnsureCollection(ctx); err != nil {
		t.Fatalf("second EnsureCollection (idempotent): %v", err)
	}
}

// TestStore_UpsertSearchDelete runs the full flow:
//  1. EnsureCollection
//  2. Upsert 3 points with distinct payloads (document_id, version, linked_feature_id)
//  3. Search — verify len > 0 and expected IDs are present
//  4. DeleteVersion — verify deleted points no longer appear
func TestStore_UpsertSearchDelete(t *testing.T) {
	store := startPGVector(t, 4)
	ctx := context.Background()

	if err := store.EnsureCollection(ctx); err != nil {
		t.Fatalf("EnsureCollection: %v", err)
	}

	points := []knowledge.VectorPoint{
		{
			ID:     "chunk-a",
			Vector: []float32{1, 0, 0, 0},
			Payload: map[string]any{
				"workspace_id":      "ws-a",
				"document_id":       "doc1",
				"version":           "v1",
				"linked_feature_id": "feat-1",
				"document_type":     "supporting_doc",
				"authority_level":   "reference",
				"chunk_text":        "hello world",
			},
		},
		{
			ID:     "chunk-b",
			Vector: []float32{0, 1, 0, 0},
			Payload: map[string]any{
				"workspace_id":      "ws-a",
				"document_id":       "doc1",
				"version":           "v1",
				"linked_feature_id": "feat-1",
				"document_type":     "supporting_doc",
				"authority_level":   "reference",
				"chunk_text":        "second chunk",
			},
		},
		{
			ID:     "chunk-c",
			Vector: []float32{0, 0, 1, 0},
			Payload: map[string]any{
				"workspace_id":      "ws-b",
				"document_id":       "doc2",
				"version":           "v1",
				"linked_feature_id": "feat-2",
				"document_type":     "policy_doc",
				"authority_level":   "high",
				"chunk_text":        "unrelated doc",
			},
		},
	}

	if err := store.Upsert(ctx, points); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Search close to chunk-a's vector — should get results
	results, err := store.Search(ctx, knowledge.VectorSearch{
		WorkspaceID: "ws-a",
		Vector:      []float32{1, 0, 0, 0},
		Limit:       5,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result, got 0")
	}

	// Verify chunk-a (exact match) is present
	found := false
	for _, r := range results {
		if r.ID == "chunk-a" {
			found = true
			if r.Score < 0.99 {
				t.Errorf("chunk-a: expected near-1 score, got %f", r.Score)
			}
			if r.Payload["document_id"] != "doc1" {
				t.Errorf("chunk-a: payload document_id=%v, want doc1", r.Payload["document_id"])
			}
		}
	}
	if !found {
		t.Errorf("chunk-a not found in results: %+v", results)
	}

	// Search with linked_feature_id filter — only feat-1 chunks
	filtered, err := store.Search(ctx, knowledge.VectorSearch{
		WorkspaceID:   "ws-a",
		Vector:        []float32{1, 0, 0, 0},
		LinkedFeature: "feat-1",
		Limit:         5,
	})
	if err != nil {
		t.Fatalf("Search (filtered): %v", err)
	}
	for _, r := range filtered {
		if r.Payload["linked_feature_id"] != "feat-1" {
			t.Errorf("filter leak: got chunk with linked_feature_id=%v", r.Payload["linked_feature_id"])
		}
	}

	// Search scoped to ws-a — must never surface the ws-b chunk (chunk-c),
	// even though it is within the vector limit.
	workspaceFiltered, err := store.Search(ctx, knowledge.VectorSearch{
		WorkspaceID: "ws-a",
		Vector:      []float32{1, 0, 0, 0},
		Limit:       5,
	})
	if err != nil {
		t.Fatalf("Search (workspace filtered): %v", err)
	}
	sawWorkspaceHit := false
	for _, r := range workspaceFiltered {
		if r.Payload["workspace_id"] != "ws-a" {
			t.Errorf("workspace leak: got workspace_id=%v", r.Payload["workspace_id"])
		}
		if r.ID == "chunk-c" {
			t.Errorf("ws-b chunk-c leaked into ws-a search")
		}
		if r.ID == "chunk-a" {
			sawWorkspaceHit = true
		}
	}
	if !sawWorkspaceHit {
		t.Errorf("ws-a search should return chunk-a, got %+v", workspaceFiltered)
	}

	// DeleteVersion: remove doc1/v1 chunks
	if err := store.DeleteVersion(ctx, "doc1", "v1"); err != nil {
		t.Fatalf("DeleteVersion: %v", err)
	}

	// After delete, doc1/v1 chunks must not appear
	after, err := store.Search(ctx, knowledge.VectorSearch{
		WorkspaceID: "ws-b",
		Vector:      []float32{1, 0, 0, 0},
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("Search after delete: %v", err)
	}
	for _, r := range after {
		if r.Payload["document_id"] == "doc1" && r.Payload["version"] == "v1" {
			t.Errorf("deleted chunk still present: %+v", r)
		}
	}

	// doc2/v1 (chunk-c) should still be present
	doc2Found := false
	for _, r := range after {
		if r.ID == "chunk-c" {
			doc2Found = true
		}
	}
	if !doc2Found {
		t.Error("chunk-c (doc2/v1) should still be present after deleting doc1/v1")
	}
}
