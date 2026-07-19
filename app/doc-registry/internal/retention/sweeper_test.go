package retention_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/retention"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
)

type fakeCandidates struct {
	buckets []storagedb.RetentionBucket
	ids     []string
	err     error
}

func (f *fakeCandidates) ListExpiredCandidates(_ context.Context, buckets []storagedb.RetentionBucket) ([]string, error) {
	f.buckets = buckets
	return f.ids, f.err
}

type fakeReferenced struct {
	ids map[string]bool
	err error
}

func (f *fakeReferenced) ListReferencedArtifactIDs(_ context.Context) (map[string]bool, error) {
	return f.ids, f.err
}

type fakeDeleter struct {
	deleted []string
	err     error
}

func (f *fakeDeleter) Delete(_ context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.deleted = append(f.deleted, id)
	return nil
}

func TestSweeperSweepsOnlyAllowedBuckets(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	candidates := &fakeCandidates{ids: []string{"art-old-superseded", "art-old-needs-changes"}}
	deleter := &fakeDeleter{}
	s := &retention.Sweeper{
		Candidates: candidates,
		Referenced: &fakeReferenced{ids: map[string]bool{}},
		Artifacts:  deleter,
		Now:        func() time.Time { return now },
	}

	result, err := s.Once(context.Background())
	if err != nil {
		t.Fatalf("Once: %v", err)
	}
	if result.Deleted != 2 {
		t.Fatalf("Deleted = %d, want 2", result.Deleted)
	}

	// Only superseded (90d) and needs_changes (30d) buckets — never approved or draft.
	if len(candidates.buckets) != 2 {
		t.Fatalf("buckets = %+v, want exactly 2", candidates.buckets)
	}
	wantCutoffs := map[artifact.Status]time.Time{
		artifact.StatusSuperseded:   now.Add(-90 * 24 * time.Hour),
		artifact.StatusNeedsChanges: now.Add(-30 * 24 * time.Hour),
	}
	for _, b := range candidates.buckets {
		want, ok := wantCutoffs[b.Status]
		if !ok {
			t.Fatalf("unexpected bucket status %q (approved/draft must never be swept)", b.Status)
		}
		if !b.Cutoff.Equal(want) {
			t.Fatalf("bucket %q cutoff = %v, want %v", b.Status, b.Cutoff, want)
		}
	}
}

type fakeGateRows struct {
	ids []string
}

func (f *fakeGateRows) DeleteArtifactGateRows(_ context.Context, artifactIDs []string) error {
	f.ids = append(f.ids, artifactIDs...)
	return nil
}

func TestSweeperSkipsReferencedArtifacts(t *testing.T) {
	t.Parallel()
	candidates := &fakeCandidates{ids: []string{"art-canonical", "art-pack", "art-free"}}
	deleter := &fakeDeleter{}
	gateRows := &fakeGateRows{}
	s := &retention.Sweeper{
		Candidates: candidates,
		Referenced: &fakeReferenced{ids: map[string]bool{"art-canonical": true, "art-pack": true}},
		Artifacts:  deleter,
		GateRows:   gateRows,
	}

	result, err := s.Once(context.Background())
	if err != nil {
		t.Fatalf("Once: %v", err)
	}
	if result.Deleted != 1 || result.SkippedReferenced != 2 {
		t.Fatalf("result = %+v, want 1 deleted / 2 skipped", result)
	}
	if len(deleter.deleted) != 1 || deleter.deleted[0] != "art-free" {
		t.Fatalf("deleted = %v, want only art-free", deleter.deleted)
	}
	// Gate runs/tasks have no FK cascade — the sweep must clean them up for
	// exactly the artifacts it deleted, never for skipped ones.
	if len(gateRows.ids) != 1 || gateRows.ids[0] != "art-free" {
		t.Fatalf("gate rows cleaned for %v, want only art-free", gateRows.ids)
	}
}

func TestSweeperCountsReferenceCreatedDuringDeleteAsSkipped(t *testing.T) {
	t.Parallel()
	s := &retention.Sweeper{
		Candidates: &fakeCandidates{ids: []string{"art-raced"}},
		Referenced: &fakeReferenced{ids: map[string]bool{}},
		Artifacts:  &fakeDeleter{err: storagedb.ErrArtifactReferenced},
	}

	result, err := s.Once(context.Background())
	if err != nil {
		t.Fatalf("Once: %v", err)
	}
	if result.Deleted != 0 || result.SkippedReferenced != 1 {
		t.Fatalf("result = %+v, want 0 deleted / 1 skipped", result)
	}
}

func TestSweeperRunSkipsTickWhenDisabled(t *testing.T) {
	t.Parallel()
	candidates := &fakeCandidates{ids: []string{"art-1"}}
	deleter := &fakeDeleter{}
	s := &retention.Sweeper{
		Candidates: candidates,
		Referenced: &fakeReferenced{ids: map[string]bool{}},
		Artifacts:  deleter,
		Enabled:    func() bool { return false },
		Interval:   time.Hour,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.Run(ctx) // immediate first tick runs synchronously, then exits on cancelled ctx
	if candidates.buckets != nil {
		t.Fatalf("disabled sweeper must not query candidates, got buckets %+v", candidates.buckets)
	}
	if len(deleter.deleted) != 0 {
		t.Fatalf("disabled sweeper must not delete, got %v", deleter.deleted)
	}
}

func TestSweeperRunTicksWhenEnabled(t *testing.T) {
	t.Parallel()
	candidates := &fakeCandidates{}
	s := &retention.Sweeper{
		Candidates: candidates,
		Referenced: &fakeReferenced{ids: map[string]bool{}},
		Artifacts:  &fakeDeleter{},
		Enabled:    func() bool { return true },
		Interval:   time.Hour,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.Run(ctx)
	if candidates.buckets == nil {
		t.Fatal("enabled sweeper should query candidates on the first tick")
	}
}

func TestSweeperFailsClosedWhenReferenceLookupErrors(t *testing.T) {
	t.Parallel()
	candidates := &fakeCandidates{ids: []string{"art-1"}}
	deleter := &fakeDeleter{}
	s := &retention.Sweeper{
		Candidates: candidates,
		Referenced: &fakeReferenced{err: errors.New("db down")},
		Artifacts:  deleter,
	}

	if _, err := s.Once(context.Background()); err == nil {
		t.Fatal("expected error when reference lookup fails")
	}
	if len(deleter.deleted) != 0 {
		t.Fatalf("nothing must be deleted when protection lookup fails, deleted=%v", deleter.deleted)
	}
}

func TestSweeperOrphanPhaseOptional(t *testing.T) {
	t.Parallel()
	s := &retention.Sweeper{
		Candidates: &fakeCandidates{},
		Referenced: &fakeReferenced{ids: map[string]bool{}},
		Artifacts:  &fakeDeleter{},
	}
	if _, err := s.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
}
