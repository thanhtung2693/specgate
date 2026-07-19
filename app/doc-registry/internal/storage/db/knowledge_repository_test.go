package db

import (
	"context"
	"errors"
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
		docByFeat.WorkspaceID = "ws-x"
		docByFeat.LinkedFeatureID = "feat-x"
		docByFeat.AuthorityLevel = knowledge.AuthorityHigh
		if err := kr.CreateVersion(ctx, docByFeat, nil); err != nil {
			t.Fatal(err)
		}

		// doc linked by request only
		docByReq := knowledgeDoc("doc-req", "v1", now)
		docByReq.WorkspaceID = "ws-x"
		docByReq.LinkedRequestID = "req-y"
		docByReq.AuthorityLevel = knowledge.AuthoritySourceOfTruth
		if err := kr.CreateVersion(ctx, docByReq, nil); err != nil {
			t.Fatal(err)
		}

		// doc linked by both
		docByBoth := knowledgeDoc("doc-both", "v1", now)
		docByBoth.WorkspaceID = "ws-x"
		docByBoth.LinkedFeatureID = "feat-x"
		docByBoth.LinkedRequestID = "req-y"
		docByBoth.AuthorityLevel = knowledge.AuthorityReference
		if err := kr.CreateVersion(ctx, docByBoth, nil); err != nil {
			t.Fatal(err)
		}

		// doc unrelated — must not appear
		docOther := knowledgeDoc("doc-other", "v1", now)
		docOther.WorkspaceID = "ws-x"
		docOther.LinkedFeatureID = "feat-other"
		if err := kr.CreateVersion(ctx, docOther, nil); err != nil {
			t.Fatal(err)
		}

		// doc in another workspace, linked to feat-x — must never appear for ws-x.
		docCrossWs := knowledgeDoc("doc-cross-ws", "v1", now)
		docCrossWs.WorkspaceID = "ws-other"
		docCrossWs.LinkedFeatureID = "feat-x"
		if err := kr.CreateVersion(ctx, docCrossWs, nil); err != nil {
			t.Fatal(err)
		}

		// feature+request: should return all three linked docs (OR semantics),
		// and never the other-workspace doc.
		docs, err := kr.ListByFeatureOrRequest(ctx, "ws-x", []string{"feat-x"}, "req-y")
		if err != nil {
			t.Fatal(err)
		}
		if len(docs) != 3 {
			t.Fatalf("feat+req: got %d docs, want 3", len(docs))
		}
		if contains(docIDs(docs), "doc-cross-ws") {
			t.Errorf("workspace leak: doc-cross-ws returned for ws-x: %v", docIDs(docs))
		}

		// feature only
		docs, err = kr.ListByFeatureOrRequest(ctx, "ws-x", []string{"feat-x"}, "")
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
		if contains(ids, "doc-cross-ws") {
			t.Errorf("feat-only: workspace leak doc-cross-ws in %v", ids)
		}

		// feature ID + key refs: curator commands may store either; Context Pack
		// provenance passes both refs explicitly.
		docByFeatKey := knowledgeDoc("doc-feat-key", "v1", now)
		docByFeatKey.WorkspaceID = "ws-x"
		docByFeatKey.LinkedFeatureID = "FEAT-X"
		if err := kr.CreateVersion(ctx, docByFeatKey, nil); err != nil {
			t.Fatal(err)
		}
		docs, err = kr.ListByFeatureOrRequest(ctx, "ws-x", []string{"feat-x", "FEAT-X"}, "")
		if err != nil {
			t.Fatal(err)
		}
		ids = docIDs(docs)
		if !contains(ids, "doc-feat") || !contains(ids, "doc-feat-key") {
			t.Errorf("feature refs: expected id+key linked docs, got %v", ids)
		}

		// request only
		docs, err = kr.ListByFeatureOrRequest(ctx, "ws-x", nil, "req-y")
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

		// empty workspace → zero rows, no DB call (workspace scope required).
		docs, err = kr.ListByFeatureOrRequest(ctx, "", []string{"feat-x"}, "req-y")
		if err != nil {
			t.Fatal(err)
		}
		if len(docs) != 0 {
			t.Errorf("empty workspace: got %d docs, want 0", len(docs))
		}

		// feature+request both empty → zero rows, no DB error
		docs, err = kr.ListByFeatureOrRequest(ctx, "ws-x", nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(docs) != 0 {
			t.Errorf("both empty: got %d docs, want 0", len(docs))
		}

		// all-versions: add a v2 for doc-feat (v1 becomes non-latest) and assert
		// both versions are returned (caller needs to derive freshness from IsLatest).
		docByFeatV2 := knowledgeDoc("doc-feat", "v1.1", now.Add(time.Minute))
		docByFeatV2.WorkspaceID = "ws-x"
		docByFeatV2.LinkedFeatureID = "feat-x"
		docByFeatV2.AuthorityLevel = knowledge.AuthorityHigh
		if err := kr.CreateVersion(ctx, docByFeatV2, nil); err != nil {
			t.Fatal(err)
		}
		docs, err = kr.ListByFeatureOrRequest(ctx, "ws-x", []string{"feat-x"}, "")
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

func TestKnowledgeRepositoryListScopesWorkspace(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		kr := NewKnowledgeRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		docA := knowledgeDoc("doc-ws-a", "v1", now)
		docA.WorkspaceID = "ws-a"
		docA.LinkedFeatureID = "feat-x"
		if err := kr.CreateVersion(ctx, docA, nil); err != nil {
			t.Fatal(err)
		}
		docB := knowledgeDoc("doc-ws-b", "v1", now)
		docB.WorkspaceID = "ws-b"
		docB.LinkedFeatureID = "feat-x"
		if err := kr.CreateVersion(ctx, docB, nil); err != nil {
			t.Fatal(err)
		}
		docA2 := knowledgeDoc("doc-ws-a-2", "v1", now.Add(time.Second))
		docA2.WorkspaceID = "ws-a"
		docA2.LinkedFeatureID = "feat-y"
		if err := kr.CreateVersion(ctx, docA2, nil); err != nil {
			t.Fatal(err)
		}

		docs, err := kr.List(ctx, knowledge.ListFilter{WorkspaceID: "ws-a", LinkedFeatureID: "feat-x"})
		if err != nil {
			t.Fatal(err)
		}
		if len(docs) != 1 || docs[0].WorkspaceID != "ws-a" {
			t.Fatalf("docs=%+v", docs)
		}
		total, err := kr.Count(ctx, knowledge.ListFilter{WorkspaceID: "ws-a", Limit: 1, Offset: 1})
		if err != nil {
			t.Fatal(err)
		}
		if total != 2 {
			t.Fatalf("total = %d, want 2 matching documents despite pagination", total)
		}
	})
}

func TestKnowledgeRepositoryContextScopeGuardsDetailAndMutation(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		kr := NewKnowledgeRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		docA := knowledgeDoc("doc-context-a", "v1", now)
		docA.WorkspaceID = "ws-a"
		if err := kr.CreateVersion(ctx, docA, nil); err != nil {
			t.Fatal(err)
		}
		docB := knowledgeDoc("doc-context-b", "v1", now)
		docB.WorkspaceID = "ws-b"
		if err := kr.CreateVersion(ctx, docB, nil); err != nil {
			t.Fatal(err)
		}

		wsA := knowledge.WithWorkspace(ctx, "ws-a")
		if _, err := kr.Get(wsA, docB.DocumentID, docB.Version); err != knowledge.ErrNotFound {
			t.Fatalf("cross-workspace Get error = %v, want ErrNotFound", err)
		}
		if _, err := kr.LatestVersion(wsA, docB.DocumentID); err != knowledge.ErrNotFound {
			t.Fatalf("cross-workspace LatestVersion error = %v, want ErrNotFound", err)
		}
		if err := kr.UpdateStatus(wsA, docB.DocumentID, docB.Version, knowledge.StatusFailed, "", "blocked"); err != knowledge.ErrNotFound {
			t.Fatalf("cross-workspace UpdateStatus error = %v, want ErrNotFound", err)
		}
		if err := kr.DeleteVersion(wsA, docB.DocumentID, docB.Version); err != knowledge.ErrNotFound {
			t.Fatalf("cross-workspace DeleteVersion error = %v, want ErrNotFound", err)
		}
		if err := kr.CreateVersion(wsA, &knowledge.Document{DocumentID: "doc-context-c", Version: "v1", WorkspaceID: "ws-b"}, nil); !errors.Is(err, knowledge.ErrValidation) {
			t.Fatalf("mismatched CreateVersion error = %v, want validation", err)
		}
		for name, query := range map[string]func() (int, error){
			"list": func() (int, error) {
				rows, err := kr.List(wsA, knowledge.ListFilter{WorkspaceID: "ws-b"})
				return len(rows), err
			},
			"count": func() (int, error) {
				return kr.Count(wsA, knowledge.ListFilter{WorkspaceID: "ws-b"})
			},
		} {
			t.Run(name, func(t *testing.T) {
				got, err := query()
				if err != nil {
					t.Fatal(err)
				}
				if got != 0 {
					t.Fatalf("cross-workspace %s returned %d rows", name, got)
				}
			})
		}
	})
}

func TestMigrationKnowledgeChunkSections(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		cols := tableColumns(t, "document_chunks", gdb, name)
		for _, c := range []string{"heading", "heading_path", "section_index"} {
			if !cols[c] {
				t.Fatalf("document_chunks.%s missing; columns=%v", c, cols)
			}
		}
	})
}

func TestKnowledgeRepositoryPersistsChunkSectionMetadata(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		kr := NewKnowledgeRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		doc := knowledgeDoc("doc-sec", "v1", now)
		doc.WorkspaceID = "ws-a"
		if err := kr.CreateVersion(ctx, doc, nil); err != nil {
			t.Fatal(err)
		}
		if err := kr.ReplaceChunks(ctx, "doc-sec", "v1", []knowledge.Chunk{
			{ID: "c0", DocumentID: "doc-sec", Version: "v1", ChunkIndex: 0, ChunkText: "# Refunds\nbody", Heading: "Refunds", HeadingPathJSON: `["Refunds"]`, SectionIndex: 0, CreatedAt: now},
			{ID: "c1", DocumentID: "doc-sec", Version: "v1", ChunkIndex: 1, ChunkText: "more refunds detail", Heading: "Refunds", HeadingPathJSON: `["Refunds"]`, SectionIndex: 0, CreatedAt: now},
			{ID: "c2", DocumentID: "doc-sec", Version: "v1", ChunkIndex: 2, ChunkText: "# Loyalty\nbody", Heading: "Loyalty", HeadingPathJSON: `["Loyalty"]`, SectionIndex: 1, CreatedAt: now},
		}); err != nil {
			t.Fatal(err)
		}

		got, err := kr.Get(ctx, "doc-sec", "v1")
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Chunks) != 3 {
			t.Fatalf("chunks=%d want 3", len(got.Chunks))
		}
		byIndex := map[int]knowledge.Chunk{}
		for _, c := range got.Chunks {
			byIndex[c.ChunkIndex] = c
		}
		if byIndex[0].Heading != "Refunds" || byIndex[0].SectionIndex != 0 || byIndex[0].HeadingPathJSON != `["Refunds"]` {
			t.Fatalf("chunk0 metadata not persisted: %+v", byIndex[0])
		}
		if byIndex[2].Heading != "Loyalty" || byIndex[2].SectionIndex != 1 || byIndex[2].HeadingPathJSON != `["Loyalty"]` {
			t.Fatalf("chunk2 metadata not persisted: %+v", byIndex[2])
		}
	})
}

func TestKnowledgeRepositoryChunksForDocumentOrdered(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		kr := NewKnowledgeRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		doc := knowledgeDoc("doc-chunks", "v1", now)
		doc.WorkspaceID = "ws-a"
		if err := kr.CreateVersion(ctx, doc, nil); err != nil {
			t.Fatal(err)
		}
		// Insert out of chunk_index order; ChunksForDocument must return them sorted.
		if err := kr.ReplaceChunks(ctx, "doc-chunks", "v1", []knowledge.Chunk{
			{ID: "c1", DocumentID: "doc-chunks", Version: "v1", ChunkIndex: 1, ChunkText: "second", Heading: "S", HeadingPathJSON: `["S"]`, SectionIndex: 0, CreatedAt: now},
			{ID: "c0", DocumentID: "doc-chunks", Version: "v1", ChunkIndex: 0, ChunkText: "first", Heading: "S", HeadingPathJSON: `["S"]`, SectionIndex: 0, CreatedAt: now},
		}); err != nil {
			t.Fatal(err)
		}
		chunks, err := kr.ChunksForDocument(ctx, "doc-chunks", "v1")
		if err != nil {
			t.Fatal(err)
		}
		if len(chunks) != 2 || chunks[0].ChunkIndex != 0 || chunks[1].ChunkIndex != 1 {
			t.Fatalf("chunks = %+v, want ordered by chunk_index", chunks)
		}
		if chunks[0].SectionIndex != 0 || chunks[0].Heading != "S" {
			t.Fatalf("chunk section metadata missing: %+v", chunks[0])
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
		WorkspaceID:    "ws-test",
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
