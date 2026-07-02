package localobject_test

import (
	"context"
	"strings"
	"testing"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/storage/localobject"
)

func TestStoreImplementsArtifactObjectStore(t *testing.T) {
	var _ artifact.ObjectStore = (*localobject.Store)(nil)
}

func TestStoreRoundTripByObjectKey(t *testing.T) {
	ctx := context.Background()
	store, err := localobject.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	const key = "artifacts/feat-1/v1/spec.md"
	if err := store.PutObject(ctx, key, []byte("# Spec")); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	body, err := store.GetObject(ctx, key)
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	if string(body) != "# Spec" {
		t.Fatalf("body=%q", body)
	}
	url, err := store.PresignGet(ctx, key)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	if !strings.HasPrefix(url, "file://") {
		t.Fatalf("url=%q, want file URL", url)
	}
}

func TestStoreRejectsUnsafeKeys(t *testing.T) {
	ctx := context.Background()
	store, err := localobject.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	for _, key := range []string{"", "/abs", "../escape", "safe/../../escape", `safe\name`} {
		t.Run(key, func(t *testing.T) {
			if err := store.PutObject(ctx, key, []byte("x")); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
