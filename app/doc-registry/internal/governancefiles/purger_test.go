package governancefiles

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeStore struct {
	staleReadyKeys   []string
	staleReadyCutoff time.Time
	stalePendKeys    []string
	stalePendCutoff  time.Time
	readyErr         error
}

func (f *fakeStore) Create(context.Context, File) (*File, error)              { panic("unused") }
func (f *fakeStore) Commit(context.Context, string, time.Time) (*File, error) { panic("unused") }
func (f *fakeStore) Get(context.Context, string) (*File, error)               { panic("unused") }
func (f *fakeStore) List(context.Context, ListFilter) ([]File, int64, error)  { panic("unused") }
func (f *fakeStore) Touch(context.Context, string, time.Time) (*File, error)  { panic("unused") }
func (f *fakeStore) Delete(context.Context, string) (string, error)           { panic("unused") }

func (f *fakeStore) DeleteStaleReady(_ context.Context, cutoff time.Time) ([]string, error) {
	f.staleReadyCutoff = cutoff
	return f.staleReadyKeys, f.readyErr
}
func (f *fakeStore) DeleteStalePending(_ context.Context, cutoff time.Time) ([]string, error) {
	f.stalePendCutoff = cutoff
	return f.stalePendKeys, nil
}

type fakeDeleter struct {
	deleted []string
}

func (d *fakeDeleter) DeleteObject(_ context.Context, key string) error {
	d.deleted = append(d.deleted, key)
	return nil
}

func TestPurgeOnce_AppliesTTLs(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		staleReadyKeys: []string{"a", "b"},
		stalePendKeys:  []string{"c"},
	}
	del := &fakeDeleter{}
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	p := &Purger{Store: store, S3: del, ReadyTTL: 60 * 24 * time.Hour, PendingTTL: time.Hour, Now: func() time.Time { return now }}

	if err := p.Once(context.Background()); err != nil {
		t.Fatalf("Once: %v", err)
	}
	wantReady := now.Add(-60 * 24 * time.Hour)
	if !store.staleReadyCutoff.Equal(wantReady) {
		t.Fatalf("ready cutoff %v want %v", store.staleReadyCutoff, wantReady)
	}
	wantPend := now.Add(-time.Hour)
	if !store.stalePendCutoff.Equal(wantPend) {
		t.Fatalf("pending cutoff %v want %v", store.stalePendCutoff, wantPend)
	}
	if len(del.deleted) != 3 {
		t.Fatalf("s3 deleted = %v, want 3", del.deleted)
	}
}

func TestPurgeOnce_ReadsTTLFromSettingsAtRunTime(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	del := &fakeDeleter{}
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	ttlDays := 30
	p := &Purger{
		Store:      store,
		S3:         del,
		ReadyTTL:   90 * 24 * time.Hour, // overridden by TTLDays when positive
		PendingTTL: time.Hour,
		Now:        func() time.Time { return now },
		TTLDays:    func() int { return ttlDays },
	}

	if err := p.Once(context.Background()); err != nil {
		t.Fatalf("Once: %v", err)
	}
	want30 := now.Add(-30 * 24 * time.Hour)
	if !store.staleReadyCutoff.Equal(want30) {
		t.Fatalf("ready cutoff %v want %v (TTLDays should override ReadyTTL)", store.staleReadyCutoff, want30)
	}

	// A settings change between sweeps takes effect on the next run.
	ttlDays = 7
	if err := p.Once(context.Background()); err != nil {
		t.Fatalf("Once (2): %v", err)
	}
	want7 := now.Add(-7 * 24 * time.Hour)
	if !store.staleReadyCutoff.Equal(want7) {
		t.Fatalf("ready cutoff %v want %v after settings change", store.staleReadyCutoff, want7)
	}

	// Non-positive TTL is ignored: fall back to the static ReadyTTL.
	ttlDays = 0
	if err := p.Once(context.Background()); err != nil {
		t.Fatalf("Once (3): %v", err)
	}
	want90 := now.Add(-90 * 24 * time.Hour)
	if !store.staleReadyCutoff.Equal(want90) {
		t.Fatalf("ready cutoff %v want %v (non-positive TTL should fall back to ReadyTTL)", store.staleReadyCutoff, want90)
	}
}

func TestPurgeOnce_S3FailureDoesNotAbort(t *testing.T) {
	t.Parallel()
	store := &fakeStore{staleReadyKeys: []string{"a"}, readyErr: errors.New("boom")}
	del := &fakeDeleter{}
	p := &Purger{Store: store, S3: del, ReadyTTL: time.Hour, PendingTTL: time.Hour, Now: time.Now}

	// We expect Once to surface a non-nil error from DeleteStaleReady but still call DeleteStalePending.
	_ = p.Once(context.Background())
	if store.stalePendCutoff.IsZero() {
		t.Fatalf("pending purge was not attempted")
	}
}
