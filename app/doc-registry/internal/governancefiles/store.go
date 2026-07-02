package governancefiles

import (
	"context"
	"time"
)

// Store is the persistence boundary for governance_files.
//
// Implementations live under internal/storage/db; tests use forEachDriver to
// validate via testcontainers-go (per doc-registry/AGENTS.md).
type Store interface {
	// Create inserts a row in status=pending with last_used_at=now.
	Create(ctx context.Context, f File) (*File, error)

	// Commit flips status from pending to ready and refreshes last_used_at.
	// Returns ErrNotFound if the row does not exist or is not pending.
	Commit(ctx context.Context, id string, now time.Time) (*File, error)

	// Get returns a single ready row; ErrNotFound otherwise.
	Get(ctx context.Context, id string) (*File, error)

	// List returns ready rows ordered by last_used_at DESC plus the unfiltered
	// total count for ready rows (after applying the q filter).
	List(ctx context.Context, f ListFilter) ([]File, int64, error)

	// Touch refreshes last_used_at on a ready row.
	// Returns the updated row; ErrNotFound if the row is missing or not ready.
	Touch(ctx context.Context, id string, now time.Time) (*File, error)

	// Delete removes the row regardless of status. Returns the deleted row's
	// object_key so callers can issue a best-effort S3 DeleteObject.
	// Returns ErrNotFound if the row does not exist.
	Delete(ctx context.Context, id string) (string, error)

	// DeleteStaleReady deletes ready rows with last_used_at < cutoff.
	// Returns the deleted rows' object_keys for S3 cleanup.
	DeleteStaleReady(ctx context.Context, cutoff time.Time) ([]string, error)

	// DeleteStalePending deletes pending rows with created_at < cutoff
	// (orphaned presigns where the PUT never completed).
	DeleteStalePending(ctx context.Context, cutoff time.Time) ([]string, error)
}
