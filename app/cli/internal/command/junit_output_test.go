package command

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJUnitOutputRejectsSymlinkedStateDirectory(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".specgate")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepareJUnitOutput(t.Context(), &Deps{}, root); err == nil {
		t.Fatal("accepted symlinked state directory")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("modified outside directory: %v %v", entries, err)
	}
}

func TestJUnitOutputRequiresGitIgnoredDirectory(t *testing.T) {
	if _, _, err := prepareJUnitOutput(t.Context(), &Deps{}, t.TempDir()); err == nil {
		t.Fatal("accepted output without Git ignore verification")
	}
}
