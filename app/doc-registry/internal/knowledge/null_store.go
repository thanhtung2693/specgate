package knowledge

import (
	"context"
	"fmt"
)

// NullVectorStore is a no-op VectorStore used when KNOWLEDGE_DRIVER=none.
// Vector search is disabled; all operations succeed silently.
type NullVectorStore struct{}

func (NullVectorStore) EnsureCollection(_ context.Context) error           { return nil }
func (NullVectorStore) Upsert(_ context.Context, _ []VectorPoint) error    { return nil }
func (NullVectorStore) DeleteVersion(_ context.Context, _, _ string) error { return nil }
func (NullVectorStore) Search(_ context.Context, _ VectorSearch) ([]VectorResult, error) {
	return nil, nil
}

// NullObjectStore is a no-op ObjectStore used when STORAGE_DRIVER=local.
// Raw document bytes are not stored; vector chunks in pgvector are the source of truth.
type NullObjectStore struct{}

func (NullObjectStore) PutObject(_ context.Context, _ string, _ []byte) error { return nil }
func (NullObjectStore) GetObject(_ context.Context, key string, _ int64) ([]byte, error) {
	return nil, fmt.Errorf("raw document %q not stored locally — use STORAGE_DRIVER=s3", key)
}
func (NullObjectStore) DeleteObject(_ context.Context, _ string) error { return nil }
