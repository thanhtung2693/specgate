package artifact

import (
	"context"
	"errors"
	"testing"
)

// TestResolveNextVersion covers the optimistic-concurrency helper used by the
// Publish path: empty base bumps the latest, a fresh base (equal to
// latest) bumps it, a stale base is rejected, and a first publish gets v0.1.
func TestResolveNextVersion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("no base bumps latest", func(t *testing.T) {
		t.Parallel()
		svc, repo, _ := newTestService(t)
		seedArtifact(repo, "a1", "feat-rn1", "v0.3")

		got, err := svc.ResolveNextVersion(ctx, "feat-rn1", "")
		if err != nil {
			t.Fatalf("ResolveNextVersion: %v", err)
		}
		if got != "v0.4" {
			t.Errorf("version = %q, want v0.4", got)
		}
	})

	t.Run("fresh base (==latest) bumps", func(t *testing.T) {
		t.Parallel()
		svc, repo, _ := newTestService(t)
		seedArtifact(repo, "a1", "feat-rn2", "v0.3")

		got, err := svc.ResolveNextVersion(ctx, "feat-rn2", "v0.3")
		if err != nil {
			t.Fatalf("ResolveNextVersion: %v", err)
		}
		if got != "v0.4" {
			t.Errorf("version = %q, want v0.4", got)
		}
	})

	t.Run("stale base rejected", func(t *testing.T) {
		t.Parallel()
		svc, repo, _ := newTestService(t)
		seedArtifact(repo, "a1", "feat-rn3", "v0.5")

		_, err := svc.ResolveNextVersion(ctx, "feat-rn3", "v0.3")
		if !errors.Is(err, ErrStaleBase) {
			t.Fatalf("err = %v, want ErrStaleBase", err)
		}
	})

	t.Run("first publish gets initial version", func(t *testing.T) {
		t.Parallel()
		svc, _, _ := newTestService(t)

		got, err := svc.ResolveNextVersion(ctx, "feat-rn-empty", "")
		if err != nil {
			t.Fatalf("ResolveNextVersion: %v", err)
		}
		if got != "v0.1" {
			t.Errorf("version = %q, want v0.1", got)
		}
	})

	t.Run("base set but no latest is stale", func(t *testing.T) {
		t.Parallel()
		svc, _, _ := newTestService(t)

		_, err := svc.ResolveNextVersion(ctx, "feat-rn-nolatest", "v0.1")
		if !errors.Is(err, ErrStaleBase) {
			t.Fatalf("err = %v, want ErrStaleBase", err)
		}
	})
}

// TestPublish_BaseVersionGuard covers the HTTP publish path, where the caller
// supplies an explicit Version plus an optional BaseVersion optimistic lock. A
// stale base is rejected before the row is written; a fresh base proceeds.
func TestPublish_BaseVersionGuard(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("stale base rejected", func(t *testing.T) {
		t.Parallel()
		svc, repo, _ := newTestService(t)
		seedArtifact(repo, "a1", "feat-bg1", "v0.5")

		in := minPublishInput()
		in.FeatureID = "feat-bg1"
		in.Version = "v0.4"
		in.BaseVersion = "v0.3" // stale: latest is v0.5
		_, err := svc.Publish(ctx, in)
		if !errors.Is(err, ErrStaleBase) {
			t.Fatalf("err = %v, want ErrStaleBase", err)
		}
	})

	t.Run("fresh base proceeds", func(t *testing.T) {
		t.Parallel()
		svc, repo, _ := newTestService(t)
		seedArtifact(repo, "a1", "feat-bg2", "v0.3")

		in := minPublishInput()
		in.FeatureID = "feat-bg2"
		in.Version = "v0.4"
		in.BaseVersion = "v0.3" // fresh: equals latest
		a, err := svc.Publish(ctx, in)
		if err != nil {
			t.Fatalf("Publish: %v", err)
		}
		if a.Version != "v0.4" {
			t.Errorf("version = %q, want v0.4", a.Version)
		}
	})

	t.Run("base set but no latest is stale", func(t *testing.T) {
		t.Parallel()
		svc, _, _ := newTestService(t)

		in := minPublishInput()
		in.FeatureID = "feat-bg3"
		in.Version = "v0.1"
		in.BaseVersion = "v0.1"
		_, err := svc.Publish(ctx, in)
		if !errors.Is(err, ErrStaleBase) {
			t.Fatalf("err = %v, want ErrStaleBase", err)
		}
	})

	t.Run("empty base skips optimistic check", func(t *testing.T) {
		t.Parallel()
		svc, repo, _ := newTestService(t)
		seedArtifact(repo, "a1", "feat-bg4", "v0.9")

		in := minPublishInput()
		in.FeatureID = "feat-bg4"
		in.Version = "v0.10"
		// BaseVersion intentionally empty: no optimistic check.
		a, err := svc.Publish(ctx, in)
		if err != nil {
			t.Fatalf("Publish: %v", err)
		}
		if a.Version != "v0.10" {
			t.Errorf("version = %q, want v0.10", a.Version)
		}
	})
}
