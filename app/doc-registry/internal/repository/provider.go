package repo

import "errors"

// ErrFileNotFound is returned when the requested path does not exist at the
// given ref. Callers map this to a not-found result
// (e.g. found:false) rather than a transport/upstream error.
var ErrFileNotFound = errors.New("file not found")

// FileContent holds raw file bytes plus metadata.
type FileContent struct {
	Path    string
	Content []byte
	Size    int64
	BlobSHA string
}
