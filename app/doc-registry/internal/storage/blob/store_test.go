package blob_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/doc-registry/internal/storage/blob"
	"github.com/specgate/doc-registry/internal/workspace"
)

func TestIsIDValidatesBlobKeyStructure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		want bool
	}{
		{name: "unscoped UUID", key: "550e8400-e29b-41d4-a716-446655440000", want: false},
		{name: "workspace scoped", key: "workspaces/ws-safe/550e8400-e29b-41d4-a716-446655440000", want: true},
		{name: "UUID shaped text", key: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", want: false},
		{name: "unsafe workspace", key: "workspaces/ws..unsafe/550e8400-e29b-41d4-a716-446655440000", want: false},
		{name: "S3 object key", key: "workspaces/ws-safe/governance/resources/uploads/file.md", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := blob.IsID(test.key); got != test.want {
				t.Errorf("IsID(%q) = %v, want %v", test.key, got, test.want)
			}
		})
	}
}

func TestLocalStore_PutAndOpen(t *testing.T) {
	t.Parallel()
	store, _ := blob.NewLocalStore(t.TempDir())
	ctx := workspace.WithID(context.Background(), "ws-put-open")
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
	ctx := workspace.WithID(context.Background(), "ws-delete")
	data := []byte("delete me")
	id, _ := store.Put(ctx, bytes.NewReader(data), int64(len(data)), "text/plain")
	if err := store.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Open(ctx, id); err == nil {
		t.Error("expected error opening deleted blob")
	}
}

func TestLocalStore_PutRequiresWorkspace(t *testing.T) {
	t.Parallel()
	store, _ := blob.NewLocalStore(t.TempDir())

	if _, err := store.Put(context.Background(), bytes.NewReader([]byte("unscoped")), 8, "text/plain"); err == nil {
		t.Fatal("Put without workspace succeeded")
	}
}

func TestLocalStore_PathContainment(t *testing.T) {
	t.Parallel()
	store, _ := blob.NewLocalStore(t.TempDir())
	if _, err := store.Open(context.Background(), "../../../etc/passwd"); err == nil {
		t.Error("expected error for path-traversal blob id")
	}
}

func TestLocalStore_RejectsTraversalWorkspaceBeforeWrite(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "data")
	store, err := blob.NewLocalStore(root)
	if err != nil {
		t.Fatal(err)
	}

	id, err := store.Put(workspace.WithID(context.Background(), "../../.."), bytes.NewReader([]byte("unsafe")), 6, "text/plain")
	if err == nil {
		escapedPath := filepath.Clean(filepath.Join(root, filepath.FromSlash(id)))
		t.Cleanup(func() { _ = os.Remove(escapedPath) })
		rel, relErr := filepath.Rel(root, escapedPath)
		if relErr != nil || !strings.HasPrefix(rel, "..") {
			t.Fatalf("unsafe workspace wrote unexpected path %q (relative %q, err %v)", escapedPath, rel, relErr)
		}
		t.Fatalf("Put accepted traversal workspace and wrote outside root: id=%q path=%q", id, escapedPath)
	}
}

func TestLocalStore_StoresSafeWorkspaceBlob(t *testing.T) {
	t.Parallel()
	store, err := blob.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := workspace.WithID(context.Background(), "ws-safe")
	id, err := store.Put(ctx, bytes.NewReader([]byte("safe")), 4, "text/plain")
	if err != nil {
		t.Fatalf("Put safe workspace: %v", err)
	}
	if !strings.HasPrefix(id, "workspaces/ws-safe/") {
		t.Fatalf("id = %q, want workspace prefix", id)
	}
	rc, err := store.Open(ctx, id)
	if err != nil {
		t.Fatalf("Open safe workspace blob: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("Close safe workspace blob: %v", err)
	}
}

func TestLocalStore_RejectsUnsafeWorkspacePathComponents(t *testing.T) {
	t.Parallel()
	store, err := blob.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, workspaceID := range []string{"../other", "ws/other", "ws\\other", "ws..other"} {
		if _, err := store.Put(workspace.WithID(context.Background(), workspaceID), bytes.NewReader([]byte("unsafe")), 6, "text/plain"); err == nil {
			t.Errorf("Put accepted unsafe workspace ID %q", workspaceID)
		}
	}
}

func TestLocalStore_RejectsCrossWorkspaceReadsAndDeletes(t *testing.T) {
	t.Parallel()
	store, err := blob.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	owner := workspace.WithID(context.Background(), "ws-owner")
	id, err := store.Put(owner, bytes.NewReader([]byte("private")), 7, "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	other := workspace.WithID(context.Background(), "ws-other")
	if _, err := store.Open(other, id); err == nil {
		t.Fatal("Open from another workspace succeeded")
	}
	if _, err := store.Stat(other, id); err == nil {
		t.Fatal("Stat from another workspace succeeded")
	}
	if err := store.Delete(other, id); err == nil {
		t.Fatal("Delete from another workspace succeeded")
	}
	if _, err := store.Open(owner, id); err != nil {
		t.Fatalf("owner blob was changed after rejected delete: %v", err)
	}
}
