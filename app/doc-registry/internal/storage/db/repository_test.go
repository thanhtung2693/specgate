package db

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/artifact"
)

func newArtifact(id, featureID, version string, status artifact.Status, t0 time.Time) *artifact.Artifact {
	return &artifact.Artifact{
		ID:          id,
		FeatureID:   featureID,
		Version:     version,
		Status:      status,
		RequestType: artifact.RequestTypeNewFeature,
		CreatedBy:   "tester",
		CreatedAt:   t0,
		UpdatedAt:   t0,
	}
}

func TestRepository_InsertGet(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewRepository(gdb)
		ctx := context.Background()

		now := time.Now().UTC().Truncate(time.Second)
		a := newArtifact("a1", "feat-a", "v1.0", artifact.StatusDraft, now)
		a.Services = []artifact.ServiceRef{
			{ArtifactID: "a1", Name: "checkout", Kind: "service"},
		}

		if err := repo.Insert(ctx, a); err != nil {
			t.Fatal(err)
		}
		got, err := repo.Get(ctx, "a1")
		if err != nil {
			t.Fatal(err)
		}
		if got.FeatureID != "feat-a" || got.Version != "v1.0" {
			t.Fatalf("artifact: %+v", got)
		}
		if len(got.Services) != 1 || got.Services[0].Name != "checkout" {
			t.Fatalf("services: %+v", got.Services)
		}
	})
}

func TestRepository_Get_NotFound(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewRepository(gdb)
		_, err := repo.Get(context.Background(), "missing")
		if err != ErrNotFound {
			t.Fatalf("got %v want ErrNotFound", err)
		}
	})
}

func TestRepository_InsertDuplicateFeatureVersionReturnsConflict(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		if err := repo.Insert(ctx, newArtifact("dup-a", "feat-dup", "v0.1", artifact.StatusDraft, now)); err != nil {
			t.Fatal(err)
		}
		err := repo.Insert(ctx, newArtifact("dup-b", "feat-dup", "v0.1", artifact.StatusDraft, now))
		if err != artifact.ErrConflict {
			t.Fatalf("got %v want artifact.ErrConflict", err)
		}
	})
}

func TestRepository_List(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		a1 := newArtifact("l1", "feat-x", "v1.0", artifact.StatusDraft, now)
		a2 := newArtifact("l2", "feat-x", "v2.0", artifact.StatusApproved, now.Add(time.Minute))
		if err := repo.Insert(ctx, a1); err != nil {
			t.Fatal(err)
		}
		a2.Services = []artifact.ServiceRef{{ArtifactID: "l2", Name: "orders-api", Kind: "service"}}
		if err := repo.Insert(ctx, a2); err != nil {
			t.Fatal(err)
		}

		all, err := repo.List(ctx, artifact.ListFilter{FeatureID: "feat-x", Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 2 {
			t.Fatalf("len=%d", len(all))
		}

		byStatus, err := repo.List(ctx, artifact.ListFilter{FeatureID: "feat-x", Status: artifact.StatusApproved})
		if err != nil {
			t.Fatal(err)
		}
		if len(byStatus) != 1 || byStatus[0].ID != "l2" {
			t.Fatalf("byStatus=%+v", byStatus)
		}

		bySvc, err := repo.List(ctx, artifact.ListFilter{Service: "orders-api"})
		if err != nil {
			t.Fatal(err)
		}
		if len(bySvc) != 1 || bySvc[0].ID != "l2" {
			t.Fatalf("bySvc=%+v", bySvc)
		}
	})
}

func TestRepository_UpdateStatus(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		a := newArtifact("us1", "feat-u", "v1.0", artifact.StatusDraft, now)
		if err := repo.Insert(ctx, a); err != nil {
			t.Fatal(err)
		}

		ev := artifact.Event{
			ID:         "e1",
			ArtifactID: "us1",
			EventType:  "artifact.approved",
			Payload:    "{}",
			CreatedAt:  now,
		}
		if err := repo.UpdateStatus(ctx, "us1", artifact.StatusApproved, "approver", ev); err != nil {
			t.Fatal(err)
		}
		got, err := repo.Get(ctx, "us1")
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != artifact.StatusApproved || got.ApprovedBy != "approver" {
			t.Fatalf("artifact: %+v", got)
		}
	})
}

func TestRepository_UpdateStatus_NotFound(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewRepository(gdb)
		ev := artifact.Event{
			ID:         "e1",
			ArtifactID: "nope",
			EventType:  "x",
			Payload:    "{}",
			CreatedAt:  time.Now().UTC(),
		}
		err := repo.UpdateStatus(context.Background(), "nope", artifact.StatusApproved, "a", ev)
		if err != ErrNotFound {
			t.Fatalf("got %v", err)
		}
	})
}

func TestRepository_FindOverlappingServices(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		a1 := newArtifact("o1", "feat-1", "v1.0", artifact.StatusDraft, now)
		a1.Services = []artifact.ServiceRef{{ArtifactID: "o1", Name: "svc-x", Kind: "service"}}
		a2 := newArtifact("o2", "feat-2", "v1.0", artifact.StatusApproved, now)
		a2.Services = []artifact.ServiceRef{{ArtifactID: "o2", Name: "svc-x", Kind: "service"}}

		if err := repo.Insert(ctx, a1); err != nil {
			t.Fatal(err)
		}
		if err := repo.Insert(ctx, a2); err != nil {
			t.Fatal(err)
		}

		out, err := repo.FindOverlappingServices(ctx, []string{"svc-x"}, "feat-3")
		if err != nil {
			t.Fatal(err)
		}
		if len(out) != 2 {
			t.Fatalf("len=%d", len(out))
		}

		out2, err := repo.FindOverlappingServices(ctx, []string{"svc-x"}, "feat-1")
		if err != nil {
			t.Fatal(err)
		}
		if len(out2) != 1 {
			t.Fatalf("exclude feat-1: len=%d", len(out2))
		}
	})
}

func TestRepository_ListExpiredCandidates(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		oldNeedsChanges := newArtifact("ex-needs-changes", "feat-e", "v1.0", artifact.StatusNeedsChanges, now.Add(-40*24*time.Hour))
		oldNeedsChanges.UpdatedAt = now.Add(-40 * 24 * time.Hour)

		oldSuperseded := newArtifact("ex-sup", "feat-e", "v2.0", artifact.StatusSuperseded, now.Add(-100*24*time.Hour))
		oldSuperseded.UpdatedAt = now.Add(-100 * 24 * time.Hour)

		freshApproved := newArtifact("ex-new-approved", "feat-e", "v3.0", artifact.StatusApproved, now.Add(-10*24*time.Hour))
		freshApproved.UpdatedAt = now.Add(-10 * 24 * time.Hour)

		for _, a := range []*artifact.Artifact{oldNeedsChanges, oldSuperseded, freshApproved} {
			if err := repo.Insert(ctx, a); err != nil {
				t.Fatal(err)
			}
		}

		buckets := []RetentionBucket{
			{Status: artifact.StatusNeedsChanges, Cutoff: now.Add(-30 * 24 * time.Hour)},
			{Status: artifact.StatusSuperseded, Cutoff: now.Add(-90 * 24 * time.Hour)},
			{Status: artifact.StatusApproved, Cutoff: now.Add(-180 * 24 * time.Hour)},
		}
		ids, err := repo.ListExpiredCandidates(ctx, buckets)
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) != 2 {
			t.Fatalf("ids=%v (want 2)", ids)
		}
		got := map[string]bool{}
		for _, id := range ids {
			got[id] = true
		}
		if !got["ex-needs-changes"] || !got["ex-sup"] {
			t.Fatalf("missing expected ids: got=%v", ids)
		}
		if got["ex-new-approved"] {
			t.Fatalf("fresh approved should not be listed: got=%v", ids)
		}
	})
}

func TestRepository_ListExpiredCandidates_EmptyBuckets(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewRepository(gdb)
		ids, err := repo.ListExpiredCandidates(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) != 0 {
			t.Fatalf("expected empty, got %v", ids)
		}
	})
}

func TestRepository_Delete(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		a := newArtifact("d1", "feat-d", "v1.0", artifact.StatusDraft, now)
		if err := repo.Insert(ctx, a); err != nil {
			t.Fatal(err)
		}
		if err := repo.Delete(ctx, "d1"); err != nil {
			t.Fatal(err)
		}
		_, err := repo.Get(ctx, "d1")
		if err != ErrNotFound {
			t.Fatalf("got %v", err)
		}
	})
}
