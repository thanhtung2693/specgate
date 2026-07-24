package command

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureSpecgateWorkingDirRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, ".specgate")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	t.Chdir(root)

	if err := ensureSpecgateWorkingDir(); err == nil {
		t.Fatal("symlinked .specgate directory was accepted")
	}
}

func TestWriteDeliveryScaffoldRefusesSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.json")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "completion.json")
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if err := writeDeliveryScaffold(path, []byte("replace"), true); err == nil {
		t.Fatal("forced scaffold write accepted a symlink")
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "keep" {
		t.Fatalf("symlink target changed: %q", body)
	}
}

func TestWriteDeliveryScaffoldForceReplacesRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "completion.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeDeliveryScaffold(path, []byte("new"), true); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "new" {
		t.Fatalf("body = %q, want new", body)
	}
}

func TestWriteDeliveryScaffoldCreatesMissingParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "reports", "completion.json")

	if err := writeDeliveryScaffold(path, []byte("new"), false); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "new" {
		t.Fatalf("body = %q, want new", body)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("parent mode = %04o, want 0700", got)
	}
}
