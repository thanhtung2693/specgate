package governancefiles

import (
	"context"
	"fmt"

	"github.com/specgate/doc-registry/internal/storage/blob"
)

type blobObjectDeleter struct {
	store blob.Store
}

// NewBlobObjectDeleter adapts local blob storage to the governance-file TTL
// purger's object cleanup contract.
func NewBlobObjectDeleter(store blob.Store) ObjectDeleter {
	return blobObjectDeleter{store: store}
}

func (d blobObjectDeleter) DeleteObject(ctx context.Context, key string) error {
	return d.store.Delete(ctx, key)
}

type routedObjectDeleter struct {
	blobs    blob.Store
	fallback ObjectDeleter
}

// NewRoutedObjectDeleter deletes local blob IDs locally and delegates every
// other object key to fallback. Local mode may still use S3 presigned uploads
// when S3 is configured, so a TTL sweep can encounter both key forms.
func NewRoutedObjectDeleter(blobs blob.Store, fallback ObjectDeleter) ObjectDeleter {
	return routedObjectDeleter{blobs: blobs, fallback: fallback}
}

func (d routedObjectDeleter) DeleteObject(ctx context.Context, key string) error {
	if blob.IsID(key) {
		return d.blobs.Delete(ctx, key)
	}
	if d.fallback != nil {
		return d.fallback.DeleteObject(ctx, key)
	}
	return fmt.Errorf("governance_files: no object store for key %q", key)
}
