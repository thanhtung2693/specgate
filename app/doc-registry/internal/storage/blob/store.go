package blob

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

type Store interface {
	Put(ctx context.Context, r io.Reader, size int64, contentType string) (string, error)
	Open(ctx context.Context, id string) (io.ReadCloser, error)
	Stat(ctx context.Context, id string) (Meta, error)
	Delete(ctx context.Context, id string) error
}

type Meta struct {
	ID          string
	ContentType string
	Size        int64
	SHA256      string
	Backend     string
}

type LocalStore struct {
	dataRoot string
}

func NewLocalStore(dataRoot string) (*LocalStore, error) {
	if err := os.MkdirAll(dataRoot, 0o750); err != nil {
		return nil, fmt.Errorf("blob: create data root %s: %w", dataRoot, err)
	}
	return &LocalStore{dataRoot: dataRoot}, nil
}

func (s *LocalStore) Put(ctx context.Context, r io.Reader, _ int64, contentType string) (string, error) {
	id := uuid.New().String()
	dest := s.blobPath(id)
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return "", fmt.Errorf("blob: mkdir: %w", err)
	}
	tmp := dest + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return "", fmt.Errorf("blob: create tmp: %w", err)
	}
	defer func() { _ = f.Close(); _ = os.Remove(tmp) }()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), r); err != nil {
		return "", fmt.Errorf("blob: write: %w", err)
	}
	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("blob: sync: %w", err)
	}
	_ = f.Close()
	if err := os.Rename(tmp, dest); err != nil {
		return "", fmt.Errorf("blob: rename: %w", err)
	}
	_ = h.Sum(nil) // digest computed, not persisted (local store only)
	return id, nil
}

func (s *LocalStore) Open(_ context.Context, id string) (io.ReadCloser, error) {
	if err := s.validateID(id); err != nil {
		return nil, err
	}
	f, err := os.Open(s.blobPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("blob %s: not found", id)
	}
	return f, err
}

func (s *LocalStore) Stat(ctx context.Context, id string) (Meta, error) {
	if err := s.validateID(id); err != nil {
		return Meta{}, err
	}
	fi, err := os.Stat(s.blobPath(id))
	if err != nil {
		return Meta{}, fmt.Errorf("blob %s: stat: %w", id, err)
	}
	return Meta{ID: id, Size: fi.Size(), Backend: "local"}, nil
}

func (s *LocalStore) Delete(ctx context.Context, id string) error {
	if err := s.validateID(id); err != nil {
		return err
	}
	if err := os.Remove(s.blobPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("blob %s: delete file: %w", id, err)
	}
	return nil
}

func (s *LocalStore) blobPath(id string) string {
	return filepath.Join(s.dataRoot, id[:2], id)
}

func (s *LocalStore) validateID(id string) error {
	if strings.ContainsAny(id, `/\.`) || len(id) != 36 {
		return fmt.Errorf("blob: invalid id %q", id)
	}
	return nil
}
