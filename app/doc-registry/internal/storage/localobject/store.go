package localobject

import (
	"context"
	"errors"
	"fmt"
	"net/url"
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
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("local object: create root %s: %w", root, err)
	}
	return &Store{root: root}, nil
}

func (s *Store) PutObject(_ context.Context, key string, body []byte) error {
	path, err := s.objectPath(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("local object: mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o640); err != nil {
		return fmt.Errorf("local object: write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("local object: rename: %w", err)
	}
	return nil
}

func (s *Store) GetObject(_ context.Context, key string) ([]byte, error) {
	path, err := s.objectPath(key)
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("local object %q: not found", key)
	}
	if err != nil {
		return nil, fmt.Errorf("local object %q: read: %w", key, err)
	}
	return body, nil
}

func (s *Store) PresignGet(_ context.Context, key string) (string, error) {
	path, err := s.objectPath(key)
	if err != nil {
		return "", err
	}
	u := url.URL{Scheme: "file", Path: path}
	return u.String(), nil
}

func (s *Store) DeleteObject(_ context.Context, key string) error {
	path, err := s.objectPath(key)
	if err != nil {
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
	clean := filepath.Clean(filepath.FromSlash(key))
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("local object: invalid key %q", key)
	}
	return filepath.Join(s.root, clean), nil
}
