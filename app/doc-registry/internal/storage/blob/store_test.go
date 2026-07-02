package blob_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/specgate/doc-registry/internal/storage/blob"
)

func TestLocalStore_PutAndOpen(t *testing.T) {
	t.Parallel()
	store, _ := blob.NewLocalStore(t.TempDir())
	ctx := context.Background()
	data := []byte("hello blob store")
	id, err := store.Put(ctx, bytes.NewReader(data), int64(len(data)), "text/plain")
	if err != nil || id == "" {
		t.Fatalf("Put: id=%q err=%v", id, err)
	}
	rc, err := store.Open(ctx, id)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close()
	got := new(bytes.Buffer)
	_, _ = got.ReadFrom(rc)
	if got.String() != string(data) {
		t.Errorf("expected %q, got %q", data, got)
	}
}

func TestLocalStore_Delete(t *testing.T) {
	t.Parallel()
	store, _ := blob.NewLocalStore(t.TempDir())
	ctx := context.Background()
	data := []byte("delete me")
	id, _ := store.Put(ctx, bytes.NewReader(data), int64(len(data)), "text/plain")
	if err := store.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Open(ctx, id); err == nil {
		t.Error("expected error opening deleted blob")
	}
}

func TestLocalStore_PathContainment(t *testing.T) {
	t.Parallel()
	store, _ := blob.NewLocalStore(t.TempDir())
	if _, err := store.Open(context.Background(), "../../../etc/passwd"); err == nil {
		t.Error("expected error for path-traversal blob id")
	}
}
