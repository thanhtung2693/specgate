package fsutil

import (
	"os"
	"path/filepath"
)

// AtomicWriteFile writes body to a temporary file beside path, applies mode,
// then atomically replaces path.
func AtomicWriteFile(path string, body []byte, mode os.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".specgate-write-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(body); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return ReplaceFile(tempPath, path)
}
