package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReplaceFileReplacesExistingContent(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "config")
	temp := filepath.Join(dir, "temp")
	if err := os.WriteFile(dest, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temp, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceFile(temp, dest); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "new" {
		t.Fatalf("body = %q, want new", body)
	}
	if _, err := os.Stat(temp); !os.IsNotExist(err) {
		t.Fatalf("temporary file still exists: %v", err)
	}
}

func TestAtomicWriteFileReplacesContentAndSetsMode(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "config")
	if err := os.WriteFile(dest, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := AtomicWriteFile(dest, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "new" {
		t.Fatalf("body = %q, want new", body)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(dest)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("mode = %o, want 600", got)
		}
	}
	if leftovers, err := filepath.Glob(filepath.Join(dir, ".specgate-write-*")); err != nil {
		t.Fatal(err)
	} else if len(leftovers) != 0 {
		t.Fatalf("temporary files remain: %v", leftovers)
	}
}
