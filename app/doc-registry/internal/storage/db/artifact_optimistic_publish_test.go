package db

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/artifact"
)

// noopObjectStore satisfies artifact.ObjectStore for publish tests that carry no
// document bytes.
type noopObjectStore struct{}

func (noopObjectStore) PutObject(context.Context, string, []byte) error { return nil }
func (noopObjectStore) GetObject(context.Context, string, int64) ([]byte, error) {
	return nil, errors.New("noop")
}
func (noopObjectStore) DeleteObject(context.Context, string) error { return nil }

func newPublishService(gdb *gorm.DB) *artifact.RegistryService {
	repo := NewRepository(gdb)
	objectKey := func(featureID, version, path string) string {
		return "artifacts/" + featureID + "/" + version + "/" + path
	}
	return artifact.NewService(repo, noopObjectStore{}, objectKey)
}

func basePublishInput(featureID, version string) artifact.PublishInput {
	return artifact.PublishInput{
		WorkspaceID: "ws-test",
		FeatureID:   featureID,
		Version:     version,
		Status:      artifact.StatusDraft,
		RequestType: artifact.RequestTypeNewFeature,
		ImpactLevel: artifact.ImpactLevelLow,
	}
}

// TestPublish_SameDerivedVersionHitsUniqueIndex proves the existing
// (feature_id, version) unique index already prevents silent clobber: two
// publishes for the SAME feature both derived from the same base resolve to the
// same version; the first lands, the second hits ErrConflict. base_version is
// left empty on the Publish calls so the optimistic guard does not fire — this
// isolates the raw unique-index backstop.
func TestPublish_SameDerivedVersionHitsUniqueIndex(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		svc := newPublishService(gdb)
		ctx := artifact.WithWorkspace(context.Background(), "ws-test")

		// Seed the feature's latest at v0.1.
		if _, err := svc.Publish(ctx, basePublishInput("feat-clobber", "v0.1")); err != nil {
			t.Fatalf("seed publish: %v", err)
		}

		// Two agents both resolve the next version from the same base (latest=v0.1).
		v1, err := svc.ResolveNextVersion(ctx, "feat-clobber", "v0.1")
		if err != nil {
			t.Fatalf("resolve 1: %v", err)
		}
		v2, err := svc.ResolveNextVersion(ctx, "feat-clobber", "v0.1")
		if err != nil {
			t.Fatalf("resolve 2: %v", err)
		}
		if v1 != "v0.2" || v2 != "v0.2" {
			t.Fatalf("both should derive v0.2, got %q and %q", v1, v2)
		}

		// First publish at v0.2 lands.
		if _, err := svc.Publish(ctx, basePublishInput("feat-clobber", v1)); err != nil {
			t.Fatalf("first publish at %q: %v", v1, err)
		}
		// Second publish at the same version hits the unique-index backstop.
		_, err = svc.Publish(ctx, basePublishInput("feat-clobber", v2))
		if !errors.Is(err, artifact.ErrConflict) {
			t.Fatalf("second publish err = %v, want ErrConflict", err)
		}
	})
}

// TestPublish_DistinctFeaturesShareVersionString proves feature_key isolation:
// two different feature keys can both hold the same version string.
func TestPublish_DistinctFeaturesShareVersionString(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		svc := newPublishService(gdb)
		ctx := artifact.WithWorkspace(context.Background(), "ws-test")

		if _, err := svc.Publish(ctx, basePublishInput("feat-iso-a", "v0.1")); err != nil {
			t.Fatalf("publish A: %v", err)
		}
		if _, err := svc.Publish(ctx, basePublishInput("feat-iso-b", "v0.1")); err != nil {
			t.Fatalf("publish B: %v", err)
		}
	})
}

// TestPublish_OmittedBaseVersionSkipsCheck proves the unchecked path is
// unchanged: with no base_version, sequential publishes at bumped versions
// succeed exactly as before.
func TestPublish_OmittedBaseVersionSkipsCheck(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		svc := newPublishService(gdb)
		ctx := artifact.WithWorkspace(context.Background(), "ws-test")

		for _, v := range []string{"v0.1", "v0.2", "v0.3"} {
			in := basePublishInput("feat-legacy", v)
			// BaseVersion intentionally empty.
			if _, err := svc.Publish(ctx, in); err != nil {
				t.Fatalf("publish %q: %v", v, err)
			}
		}

		latest, err := svc.LatestArtifact(ctx, "feat-legacy")
		if err != nil {
			t.Fatalf("latest: %v", err)
		}
		if latest == nil || latest.Version != "v0.3" {
			t.Fatalf("latest = %v, want v0.3", latest)
		}
	})
}

// TestPublish_StaleBaseVersionRejected proves the optimistic guard rejects a
// stale base at the database level before the row is written.
func TestPublish_StaleBaseVersionRejected(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		svc := newPublishService(gdb)
		ctx := artifact.WithWorkspace(context.Background(), "ws-test")

		if _, err := svc.Publish(ctx, basePublishInput("feat-stale", "v0.1")); err != nil {
			t.Fatalf("seed publish: %v", err)
		}
		if _, err := svc.Publish(ctx, basePublishInput("feat-stale", "v0.2")); err != nil {
			t.Fatalf("second publish: %v", err)
		}

		// Latest is now v0.2; a publish claiming base v0.1 is stale.
		in := basePublishInput("feat-stale", "v0.3")
		in.BaseVersion = "v0.1"
		_, err := svc.Publish(ctx, in)
		if !errors.Is(err, artifact.ErrStaleBase) {
			t.Fatalf("err = %v, want ErrStaleBase", err)
		}
	})
}
