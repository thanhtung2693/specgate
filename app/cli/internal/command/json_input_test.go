package command

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadJSONInputFilePreservesEscapedDocument(t *testing.T) {
	content := string(bytes.Repeat([]byte{0}, 1<<20))
	encoded, err := json.Marshal(map[string]any{"content": content})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "escaped.json")
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	data, err := readJSONInputFile(path)
	if err != nil || !bytes.Equal(data, encoded) {
		t.Fatalf("escaped document changed or refused: %v", err)
	}
}

func TestReadJSONInputFilePreservesSymlinkInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.json")
	if err := os.WriteFile(path, []byte(`{"summary":"ok"}`), 0600); err != nil {
		t.Fatal(err)
	}
	link := path + ".link"
	if err := os.Symlink(path, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	data, err := readJSONInputFile(link)
	if err != nil || string(data) != `{"summary":"ok"}` {
		t.Fatalf("symlink input changed or refused: %q %v", data, err)
	}
}
