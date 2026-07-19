package governancefiles

import (
	"context"
	"strings"
	"testing"

	"github.com/specgate/doc-registry/internal/storage/blob"
	"github.com/specgate/doc-registry/internal/workspace"
)

type recordingObjectDeleter struct{ deleted []string }

func (d *recordingObjectDeleter) DeleteObject(_ context.Context, key string) error {
	d.deleted = append(d.deleted, key)
	return nil
}

func TestBlobObjectDeleter_DeletesLocalBlob(t *testing.T) {
	t.Parallel()

	store, err := blob.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key, err := store.Put(
		workspace.WithID(context.Background(), "ws-test"),
		strings.NewReader("source"),
		6,
		"text/markdown",
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := NewBlobObjectDeleter(store).DeleteObject(context.Background(), key); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	if _, err := store.Open(context.Background(), key); err == nil {
		t.Fatalf("blob %q remains after delete", key)
	}
}

func TestRoutedObjectDeleter_UsesFallbackForS3Key(t *testing.T) {
	t.Parallel()

	store, err := blob.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fallback := &recordingObjectDeleter{}
	key := "workspaces/ws-test/governance/resources/uploads/file.md"

	if err := NewRoutedObjectDeleter(store, fallback).DeleteObject(context.Background(), key); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	if len(fallback.deleted) != 1 || fallback.deleted[0] != key {
		t.Fatalf("fallback deleted = %v, want [%s]", fallback.deleted, key)
	}
}

func TestRoutedObjectDeleter_UsesFallbackForUUIDShapedNonBlobKey(t *testing.T) {
	t.Parallel()

	store, err := blob.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fallback := &recordingObjectDeleter{}
	key := "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"

	if err := NewRoutedObjectDeleter(store, fallback).DeleteObject(context.Background(), key); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	if len(fallback.deleted) != 1 || fallback.deleted[0] != key {
		t.Fatalf("fallback deleted = %v, want [%s]", fallback.deleted, key)
	}
}
