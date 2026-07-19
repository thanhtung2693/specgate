package localobject_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/storage/localobject"
)

func TestStoreImplementsArtifactObjectStore(t *testing.T) {
	var _ artifact.ObjectStore = (*localobject.Store)(nil)
}

func safeTempRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temporary root: %v", err)
	}
	return root
}

func TestStoreRoundTripByObjectKey(t *testing.T) {
	ctx := context.Background()
	store, err := localobject.NewStore(safeTempRoot(t))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	const key = "artifacts/feat-1/v1/spec.md"
	if err := store.PutObject(ctx, key, []byte("# Spec")); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	body, err := store.GetObject(ctx, key, 1<<20)
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	if string(body) != "# Spec" {
		t.Fatalf("body=%q", body)
	}
}

func TestStoreRejectsUnsafeKeys(t *testing.T) {
	ctx := context.Background()
	store, err := localobject.NewStore(safeTempRoot(t))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	for _, key := range []string{"", "/abs", "../escape", "safe/../../escape", "safe/./object", "safe/../object", `safe\name`} {
		t.Run(key, func(t *testing.T) {
			if err := store.PutObject(ctx, key, []byte("x")); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestStoreRejectsCrossWorkspaceTraversalWithoutTouchingTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := localobject.NewStore(safeTempRoot(t))
	if err != nil {
		t.Fatal(err)
	}

	const target = "workspaces/ws-b/documents/doc-1/v1/raw/source.txt"
	if err := store.PutObject(ctx, target, []byte("workspace-b")); err != nil {
		t.Fatal(err)
	}
	traversal := "workspaces/ws-a/documents/../../ws-b/documents/doc-1/v1/raw/source.txt"
	if err := store.PutObject(ctx, traversal, []byte("workspace-a")); err == nil {
		t.Fatal("cross-workspace overwrite succeeded")
	}
	if err := store.DeleteObject(ctx, traversal); err == nil {
		t.Fatal("cross-workspace delete succeeded")
	}
	body, err := store.GetObject(ctx, target, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "workspace-b" {
		t.Fatalf("target body = %q, want workspace-b", body)
	}
}

func TestStoreGetObjectRejectsBodyAboveCallerLimit(t *testing.T) {
	t.Parallel()
	store, err := localobject.NewStore(safeTempRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.PutObject(ctx, "safe/object", []byte("four")); err != nil {
		t.Fatal(err)
	}

	if _, err := store.GetObject(ctx, "safe/object", 3); err == nil {
		t.Fatal("expected oversized object read to fail")
	}
}

func TestStoreRejectsSymlinkedKeyAncestor(t *testing.T) {
	t.Parallel()
	root := safeTempRoot(t)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "workspaces"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "workspaces", "ws-a")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	outsideObject := filepath.Join(outside, "artifact.txt")
	if err := os.WriteFile(outsideObject, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := localobject.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const key = "workspaces/ws-a/artifact.txt"

	if err := store.PutObject(ctx, key, []byte("overwrite")); err == nil {
		t.Fatal("PutObject followed a symlinked key ancestor")
	}
	if _, err := store.GetObject(ctx, key, 1024); err == nil {
		t.Fatal("GetObject followed a symlinked key ancestor")
	}
	if err := store.DeleteObject(ctx, key); err == nil {
		t.Fatal("DeleteObject followed a symlinked key ancestor")
	}
	body, err := os.ReadFile(outsideObject)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "outside" {
		t.Fatalf("outside object = %q, want unchanged", body)
	}
}

func TestStorePutDoesNotFollowPredictableTemporarySymlink(t *testing.T) {
	t.Parallel()
	root := safeTempRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "safe"), 0o750); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "user-data.txt")
	if err := os.WriteFile(outside, []byte("user data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "safe", "object.tmp")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	store, err := localobject.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.PutObject(context.Background(), "safe/object", []byte("stored")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "user data" {
		t.Fatalf("temporary symlink target = %q, want unchanged", body)
	}
}

func TestNewStoreRejectsSymlinkRoot(t *testing.T) {
	t.Parallel()
	base := safeTempRoot(t)
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "root-link")
	if err := os.Symlink(target, root); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := localobject.NewStore(root); err == nil {
		t.Fatal("NewStore accepted a symlink root")
	}
}

func TestNewStoreRejectsSymlinkAncestor(t *testing.T) {
	t.Parallel()
	base := safeTempRoot(t)
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}
	ancestor := filepath.Join(base, "ancestor-link")
	if err := os.Symlink(target, ancestor); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := localobject.NewStore(filepath.Join(ancestor, "objects")); err == nil {
		t.Fatal("NewStore accepted a symlink root ancestor")
	}
}
