package governancefiles

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

// ObjectDeleter is the subset of S3 client used by the purger (kept narrow for tests).
type ObjectDeleter interface {
	DeleteObject(ctx context.Context, key string) error
}

// Purger runs a periodic TTL sweep: ready rows older than ReadyTTL, plus
// pending rows older than PendingTTL (orphaned presigns). Per spec §5.4.
type Purger struct {
	Store      Store
	S3         ObjectDeleter
	ReadyTTL   time.Duration
	PendingTTL time.Duration
	Interval   time.Duration
	Now        func() time.Time // injectable for tests
	// TTLDays, when set, is read on every sweep so a settings change takes
	// effect without restart. It overrides ReadyTTL; a non-positive result is
	// ignored (falls back to ReadyTTL) to avoid deleting every ready row.
	TTLDays func() int
}

// Once runs a single sweep. Errors from one phase do not block the other.
func (p *Purger) Once(ctx context.Context) error {
	now := p.now()
	var firstErr error

	readyTTL := p.ReadyTTL
	if p.TTLDays != nil {
		if d := p.TTLDays(); d > 0 {
			readyTTL = time.Duration(d) * 24 * time.Hour
		}
	}

	keys, err := p.Store.DeleteStaleReady(ctx, now.Add(-readyTTL))
	if err != nil {
		log.Error().Err(err).Msg("governance_files: delete stale ready")
		firstErr = err
	}
	for _, k := range keys {
		if err := p.S3.DeleteObject(ctx, k); err != nil {
			log.Warn().Err(err).Str("key", k).Msg("governance_files: s3 delete (ready)")
		}
	}

	keys, err = p.Store.DeleteStalePending(ctx, now.Add(-p.PendingTTL))
	if err != nil {
		log.Error().Err(err).Msg("governance_files: delete stale pending")
		if firstErr == nil {
			firstErr = err
		}
	}
	for _, k := range keys {
		if err := p.S3.DeleteObject(ctx, k); err != nil {
			log.Warn().Err(err).Str("key", k).Msg("governance_files: s3 delete (pending)")
		}
	}
	return firstErr
}

// Run blocks until ctx is cancelled, ticking on Interval (immediate first tick).
func (p *Purger) Run(ctx context.Context) {
	if p.Interval <= 0 {
		p.Interval = 24 * time.Hour
	}
	t := time.NewTicker(p.Interval)
	defer t.Stop()
	if err := p.Once(ctx); err != nil {
		log.Warn().Err(err).Msg("governance_files: initial purge")
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := p.Once(ctx); err != nil {
				log.Warn().Err(err).Msg("governance_files: purge tick")
			}
		}
	}
}

func (p *Purger) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now().UTC()
}
