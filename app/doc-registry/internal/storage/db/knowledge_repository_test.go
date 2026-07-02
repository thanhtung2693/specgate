package db

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/knowledge"
)

func TestKnowledgeRepositoryVersionLatest(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		kr := NewKnowledgeRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		v1 := knowledgeDoc("doc_1", "v1", now)
		if err := kr.CreateVersion(ctx, v1, nil); err != nil {
			t.Fatal(err)
		}
		v11 := knowledgeDoc("doc_1", "v1.1", now.Add(time.Minute))
		v11.ParentVersion = "v1"
		if err := kr.CreateVersion(ctx, v11, nil); err != nil {
			t.Fatal(err)
		}

		old, err := kr.Get(ctx, "doc_1", "v1")
		if err != nil {
			t.Fatal(err)
		}
		if old.IsLatest {
			t.Fatal("v1 should not be latest")
		}
		latest, err := kr.Get(ctx, "doc_1", "v1.1")
		if err != nil {
			t.Fatal(err)
		}
		if !latest.IsLatest {
			t.Fatal("v1.1 should be latest")
		}
		next, err := kr.NextMinorVersion(ctx, "doc_1")
		if err != nil {
			t.Fatal(err)
		}
		if next != "v1.2" {
			t.Fatalf("next=%q", next)
		}
	})
}

func TestKnowledgeRepositoryReplaceChunks(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		kr := NewKnowledgeRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		if err := kr.CreateVersion(ctx, knowledgeDoc("doc_2", "v1", now), nil); err != nil {
			t.Fatal(err)
		}
		if err := kr.ReplaceChunks(ctx, "doc_2", "v1", []knowledge.Chunk{
			{ID: "c1", DocumentID: "doc_2", Version: "v1", ChunkIndex: 0, ChunkText: "a", CreatedAt: now},
		}); err != nil {
			t.Fatal(err)
		}
		if err := kr.ReplaceChunks(ctx, "doc_2", "v1", []knowledge.Chunk{
			{ID: "c2", DocumentID: "doc_2", Version: "v1", ChunkIndex: 0, ChunkText: "b", CreatedAt: now},
			{ID: "c3", DocumentID: "doc_2", Version: "v1", ChunkIndex: 1, ChunkText: "c", CreatedAt: now},
		}); err != nil {
			t.Fatal(err)
		}
		count, err := kr.ChunkCount(ctx, "doc_2", "v1")
		if err != nil {
			t.Fatal(err)
		}
		if count != 2 {
			t.Fatalf("count=%d", count)
		}
	})
}

func TestKnowledgeRepositoryListByFeatureOrRequest(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		kr := NewKnowledgeRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		// doc linked by feature only
		docByFeat := knowledgeDoc("doc-feat", "v1", now)
		docByFeat.LinkedFeatureID = "feat-x"
		docByFeat.AuthorityLevel = knowledge.AuthorityHigh
		if err := kr.CreateVersion(ctx, docByFeat, nil); err != nil {
			t.Fatal(err)
		}

		// doc linked by request only
		docByReq := knowledgeDoc("doc-req", "v1", now)
		docByReq.LinkedRequestID = "req-y"
		docByReq.AuthorityLevel = knowledge.AuthoritySourceOfTruth
		if err := kr.CreateVersion(ctx, docByReq, nil); err != nil {
			t.Fatal(err)
		}

		// doc linked by both
		docByBoth := knowledgeDoc("doc-both", "v1", now)
		docByBoth.LinkedFeatureID = "feat-x"
		docByBoth.LinkedRequestID = "req-y"
		docByBoth.AuthorityLevel = knowledge.AuthorityReference
		if err := kr.CreateVersion(ctx, docByBoth, nil); err != nil {
			t.Fatal(err)
		}

		// doc unrelated — must not appear
		docOther := knowledgeDoc("doc-other", "v1", now)
		docOther.LinkedFeatureID = "feat-other"
		if err := kr.CreateVersion(ctx, docOther, nil); err != nil {
			t.Fatal(err)
		}

		// feature+request: should return all three linked docs (OR semantics).
		docs, err := kr.ListByFeatureOrRequest(ctx, "feat-x", "req-y")
		if err != nil {
			t.Fatal(err)
		}
		if len(docs) != 3 {
			t.Fatalf("feat+req: got %d docs, want 3", len(docs))
		}

		// feature only
		docs, err = kr.ListByFeatureOrRequest(ctx, "feat-x", "")
		if err != nil {
			t.Fatal(err)
		}
		ids := docIDs(docs)
		if !contains(ids, "doc-feat") || !contains(ids, "doc-both") {
			t.Errorf("feat-only: expected doc-feat+doc-both, got %v", ids)
		}
		if contains(ids, "doc-req") {
			t.Errorf("feat-only: unexpected doc-req in %v", ids)
		}

		// request only
		docs, err = kr.ListByFeatureOrRequest(ctx, "", "req-y")
		if err != nil {
			t.Fatal(err)
		}
		ids = docIDs(docs)
		if !contains(ids, "doc-req") || !contains(ids, "doc-both") {
			t.Errorf("req-only: expected doc-req+doc-both, got %v", ids)
		}
		if contains(ids, "doc-feat") {
			t.Errorf("req-only: unexpected doc-feat in %v", ids)
		}

		// both empty → zero rows, no DB error
		docs, err = kr.ListByFeatureOrRequest(ctx, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(docs) != 0 {
			t.Errorf("both empty: got %d docs, want 0", len(docs))
		}

		// all-versions: add a v2 for doc-feat (v1 becomes non-latest) and assert
		// both versions are returned (caller needs to derive freshness from IsLatest).
		docByFeatV2 := knowledgeDoc("doc-feat", "v1.1", now.Add(time.Minute))
		docByFeatV2.LinkedFeatureID = "feat-x"
		docByFeatV2.AuthorityLevel = knowledge.AuthorityHigh
		if err := kr.CreateVersion(ctx, docByFeatV2, nil); err != nil {
			t.Fatal(err)
		}
		docs, err = kr.ListByFeatureOrRequest(ctx, "feat-x", "")
		if err != nil {
			t.Fatal(err)
		}
		featDocs := 0
		for _, d := range docs {
			if d.DocumentID == "doc-feat" {
				featDocs++
			}
		}
		if featDocs != 2 {
			t.Errorf("expected 2 versions of doc-feat (all-versions semantics), got %d", featDocs)
		}
	})
}

func docIDs(docs []knowledge.Document) []string {
	ids := make([]string, 0, len(docs))
	for _, d := range docs {
		ids = append(ids, d.DocumentID)
	}
	return ids
}

func contains(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func knowledgeDoc(id, version string, now time.Time) *knowledge.Document {
	return &knowledge.Document{
		DocumentID:     id,
		Version:        version,
		IsLatest:       true,
		Title:          "Checkout SRS",
		DocumentType:   knowledge.DocumentTypeSRS,
		AuthorityLevel: knowledge.AuthorityHigh,
		SourceKind:     knowledge.SourceKindText,
		SourceURI:      "documents/" + id + "/" + version + "/raw/input.txt",
		Status:         knowledge.StatusUploaded,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}
