// Package retention enforces the artifact retention windows from
// doc-registry docs/spec.md §9. The sweeper deletes only superseded and
// needs_changes artifacts past their windows; approved and draft artifacts
// are never auto-deleted, and artifacts still referenced by a feature or
// change request are skipped.
package retention

import (
	"context"
	"errors"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/specgate/doc-registry/internal/artifact"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
)

// Retention windows per spec §9.
const (
	SupersededRetention   = 90 * 24 * time.Hour
	NeedsChangesRetention = 30 * 24 * time.Hour
)

// CandidateLister lists artifact IDs past retention for the given buckets.
type CandidateLister interface {
	ListExpiredCandidates(ctx context.Context, buckets []storagedb.RetentionBucket) ([]string, error)
}

// ReferencedArtifactLister returns artifact IDs still referenced by workboard
// rows (feature canonical artifacts and change-request lead artifacts).
// Referenced artifacts are never swept.
type ReferencedArtifactLister interface {
	ListReferencedArtifactIDs(ctx context.Context) (map[string]bool, error)
}

// ArtifactDeleter deletes one artifact including its blob objects.
type ArtifactDeleter interface {
	Delete(ctx context.Context, id string) error
}

// GateRowDeleter removes gate runs and gate tasks that reference deleted
// artifacts. Artifact deletion has no FK cascade for these tables, so the
// sweep cleans them up explicitly to avoid unbounded orphan growth.
type GateRowDeleter interface {
	DeleteArtifactGateRows(ctx context.Context, artifactIDs []string) error
}

type Sweeper struct {
	Candidates CandidateLister
	Referenced ReferencedArtifactLister
	Artifacts  ArtifactDeleter
	GateRows   GateRowDeleter
	Interval   time.Duration
	Now        func() time.Time // injectable for tests
	// Enabled, when set, is read on every tick so a settings change takes
	// effect without restart. Nil means always enabled. Once ignores it —
	// explicit cleanup calls sweep unconditionally.
	Enabled func() bool
}

type SweepResult struct {
	Deleted           int
	SkippedReferenced int
}

// Once runs a single sweep. If the referenced-artifact lookup fails the sweep
// fails closed: nothing is deleted.
func (s *Sweeper) Once(ctx context.Context) (SweepResult, error) {
	now := s.now()
	buckets := []storagedb.RetentionBucket{
		{Status: artifact.StatusSuperseded, Cutoff: now.Add(-SupersededRetention)},
		{Status: artifact.StatusNeedsChanges, Cutoff: now.Add(-NeedsChangesRetention)},
	}
	ids, err := s.Candidates.ListExpiredCandidates(ctx, buckets)
	if err != nil {
		return SweepResult{}, err
	}
	if len(ids) == 0 {
		return SweepResult{}, nil
	}
	referenced, err := s.Referenced.ListReferencedArtifactIDs(ctx)
	if err != nil {
		return SweepResult{}, err
	}
	var result SweepResult
	var deleted []string
	sweep := func(candidateIDs []string, counter *int) error {
		for _, id := range candidateIDs {
			if err := ctx.Err(); err != nil {
				return err
			}
			if referenced[id] {
				result.SkippedReferenced++
				continue
			}
			if err := s.Artifacts.Delete(ctx, id); err != nil {
				if errors.Is(err, storagedb.ErrArtifactReferenced) {
					result.SkippedReferenced++
					continue
				}
				log.Warn().Err(err).Str("artifact_id", id).Msg("retention: delete artifact")
				continue
			}
			deleted = append(deleted, id)
			*counter++
		}
		return nil
	}
	if err := sweep(ids, &result.Deleted); err != nil {
		return result, err
	}
	if s.GateRows != nil && len(deleted) > 0 {
		if err := s.GateRows.DeleteArtifactGateRows(ctx, deleted); err != nil {
			log.Warn().Err(err).Msg("retention: delete artifact gate rows")
		}
	}
	return result, nil
}

// Run blocks until ctx is cancelled, ticking on Interval (immediate first tick).
func (s *Sweeper) Run(ctx context.Context) {
	if s.Interval <= 0 {
		s.Interval = 24 * time.Hour
	}
	t := time.NewTicker(s.Interval)
	defer t.Stop()
	s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tick(ctx)
		}
	}
}

func (s *Sweeper) tick(ctx context.Context) {
	if s.Enabled != nil && !s.Enabled() {
		return
	}
	result, err := s.Once(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("retention: sweep failed")
		return
	}
	if result.Deleted > 0 || result.SkippedReferenced > 0 {
		log.Info().
			Int("deleted", result.Deleted).
			Int("skipped_referenced", result.SkippedReferenced).
			Msg("retention: sweep complete")
	}
}

func (s *Sweeper) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}
