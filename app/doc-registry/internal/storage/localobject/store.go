package localobject

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Store is a key-addressed local object store for installs that do not
// configure S3. Artifact metadata persists object keys, so unlike blob.Store
// this store must read back by the caller-provided key.
type Store struct {
	root string
}

func NewStore(root string) (*Store, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("local object: resolve root %s: %w", root, err)
	}
	if err := rejectSymlinkRoot(root); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("local object: create root %s: %w", root, err)
	}
	if err := rejectSymlinkRoot(root); err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}

func rejectSymlinkRoot(root string) error {
	for current := filepath.Clean(root); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("local object: inspect root path %s: %w", current, err)
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("local object: symlink root or ancestor rejected: %s", current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
	}
}

func (s *Store) PutObject(_ context.Context, key string, body []byte) error {
	path, err := s.objectPath(key)
	if err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if err := s.ensureDirectories(parent); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("local object %q: symlink target rejected", key)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("local object %q: inspect target: %w", key, err)
	}
	tmp, err := os.CreateTemp(parent, ".specgate-object-*")
	if err != nil {
		return fmt.Errorf("local object: create temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("local object: set temporary file mode: %w", err)
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("local object: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("local object: close temporary file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("local object: rename: %w", err)
	}
	return nil
}

func (s *Store) GetObject(_ context.Context, key string, maxBytes int64) ([]byte, error) {
	path, err := s.objectPath(key)
	if err != nil {
		return nil, err
	}
	if err := s.rejectSymlinkPath(path); err != nil {
		return nil, err
	}
	if maxBytes <= 0 {
		return nil, fmt.Errorf("local object %q: positive read limit required", key)
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("local object %q: not found", key)
	}
	if err != nil {
		return nil, fmt.Errorf("local object %q: open: %w", key, err)
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("local object %q: read: %w", key, err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("local object %q exceeds %d-byte read limit", key, maxBytes)
	}
	return body, nil
}

func (s *Store) DeleteObject(_ context.Context, key string) error {
	path, err := s.objectPath(key)
	if err != nil {
		return err
	}
	if err := s.rejectSymlinkPath(path); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("local object %q: delete: %w", key, err)
	}
	return nil
}

func (s *Store) objectPath(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || filepath.IsAbs(key) || strings.ContainsRune(key, '\\') {
		return "", fmt.Errorf("local object: invalid key %q", key)
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "." || segment == ".." {
			return "", fmt.Errorf("local object: invalid key %q", key)
		}
	}
	clean := filepath.Clean(filepath.FromSlash(key))
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("local object: invalid key %q", key)
	}
	return filepath.Join(s.root, clean), nil
}

func (s *Store) ensureDirectories(path string) error {
	rel, err := filepath.Rel(s.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("local object: directory outside root")
	}
	current := s.root
	if rel == "." {
		return nil
	}
	for _, segment := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o750); err != nil && !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("local object: mkdir %s: %w", current, err)
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return fmt.Errorf("local object: inspect directory %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("local object: unsafe directory %s", current)
		}
	}
	return nil
}

func (s *Store) rejectSymlinkPath(path string) error {
	rel, err := filepath.Rel(s.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("local object: path outside root")
	}
	current := s.root
	for _, segment := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("local object: inspect path %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("local object: symlink path rejected: %s", current)
		}
	}
	return nil
}
