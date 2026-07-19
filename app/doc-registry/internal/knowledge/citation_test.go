package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestValidateCitation_RequiresExactIndexedKnowledgeSpan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newMemoryRepo()
	repo.docs["doc-1@v1"] = &Document{
		DocumentID: "doc-1", Version: "v1", WorkspaceID: "ws-a", Title: "Access policy",
		AuthorityLevel: AuthorityLow, Status: StatusIndexed, IsLatest: false,
	}
	repo.chunks["doc-1@v1"] = []Chunk{
		{DocumentID: "doc-1", Version: "v1", ChunkIndex: 2, ChunkText: "Admins may approve changes."},
		{DocumentID: "doc-1", Version: "v1", ChunkIndex: 3, ChunkText: "Evidence must be retained."},
	}
	svc, err := NewService(repo, &memoryStore{objects: map[string][]byte{}}, &memoryVectors{}, constantEmbedder{dim: 8}, 1024, "")
	if err != nil {
		t.Fatal(err)
	}

	excerpt := "Admins may approve changes.\n\nEvidence must be retained."
	digest := sha256.Sum256([]byte(excerpt))
	valid := CitationValidationInput{
		WorkspaceID: "ws-a", DocumentID: "doc-1", Version: "v1", StartChunkIndex: 2, EndChunkIndex: 3,
		ExcerptDigest: hex.EncodeToString(digest[:]),
	}
	got, err := svc.ValidateCitation(ctx, valid)
	if err != nil {
		t.Fatalf("ValidateCitation(valid): %v", err)
	}
	if got.URL != "specgate://knowledge/doc-1/v1#chunk-2" || got.Title != "Access policy" || got.AuthorityLevel != AuthorityLow || !got.Stale {
		t.Fatalf("normalized citation = %+v", got)
	}

	for name, in := range map[string]CitationValidationInput{
		"wrong workspace":  {WorkspaceID: "ws-b", DocumentID: "doc-1", Version: "v1", StartChunkIndex: 2, EndChunkIndex: 3},
		"missing document": {WorkspaceID: "ws-a", DocumentID: "doc-2", Version: "v1", StartChunkIndex: 2, EndChunkIndex: 3},
		"missing version":  {WorkspaceID: "ws-a", DocumentID: "doc-1", Version: "v2", StartChunkIndex: 2, EndChunkIndex: 3},
		"missing span":     {WorkspaceID: "ws-a", DocumentID: "doc-1", Version: "v1", StartChunkIndex: 2, EndChunkIndex: 4},
		"bad digest":       {WorkspaceID: "ws-a", DocumentID: "doc-1", Version: "v1", StartChunkIndex: 2, EndChunkIndex: 3, ExcerptDigest: "bad"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.ValidateCitation(ctx, in); err == nil {
				t.Fatal("ValidateCitation succeeded, want validation error")
			}
		})
	}

	repo.docs["doc-1@v1"].Status = StatusFailed
	if _, err := svc.ValidateCitation(ctx, valid); err == nil {
		t.Fatal("ValidateCitation accepted non-indexed document")
	}
}
