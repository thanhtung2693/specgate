package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/specgate/doc-registry/internal/artifactattachment"
	"github.com/specgate/doc-registry/internal/governancefiles"
)

func TestArtifactAttachmentRepository_CreateListDelete(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewArtifactAttachmentRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		// Two attachments on feature-1 (newest last), one on feature-2.
		mk := func(id, feature string, kind artifactattachment.Kind, at time.Time) artifactattachment.Attachment {
			return artifactattachment.Attachment{
				ID:        id,
				FeatureID: feature,
				Kind:      kind,
				URL:       "https://example.com/" + id,
				Audience:  artifactattachment.AudienceGate,
				CreatedAt: at,
			}
		}
		if _, err := repo.Create(ctx, mk("a1", "feature-1", artifactattachment.KindLink, now)); err != nil {
			t.Fatalf("Create a1: %v", err)
		}
		if _, err := repo.Create(ctx, mk("a2", "feature-1", artifactattachment.KindLink, now.Add(time.Minute))); err != nil {
			t.Fatalf("Create a2: %v", err)
		}
		if _, err := repo.Create(ctx, mk("b1", "feature-2", artifactattachment.KindLink, now)); err != nil {
			t.Fatalf("Create b1: %v", err)
		}

		// ListByFeature is scoped + newest-first.
		items, err := repo.ListByFeature(ctx, "feature-1")
		if err != nil {
			t.Fatalf("ListByFeature: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("len = %d, want 2", len(items))
		}
		if items[0].ID != "a2" || items[1].ID != "a1" {
			t.Fatalf("order = [%s,%s], want [a2,a1]", items[0].ID, items[1].ID)
		}

		// Delete removes the row; second delete is ErrNotFound.
		if err := repo.Delete(ctx, "a1"); err != nil {
			t.Fatalf("Delete a1: %v", err)
		}
		if err := repo.Delete(ctx, "a1"); !errors.Is(err, artifactattachment.ErrNotFound) {
			t.Fatalf("re-delete err = %v, want ErrNotFound", err)
		}
		left, _ := repo.ListByFeature(ctx, "feature-1")
		if len(left) != 1 || left[0].ID != "a2" {
			t.Fatalf("after delete = %+v, want [a2]", left)
		}
	})
}

// A governance file referenced by an artifact attachment must survive the TTL
// purger — otherwise an uploaded screenshot's S3 object is deleted out from
// under a still-live attachment (regression for the missing pin).
func TestArtifactAttachmentRepository_PinsFileFromTTLPurge(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		files := NewGovernanceFilesRepository(gdb)
		atts := NewArtifactAttachmentRepository(gdb)
		ctx := context.Background()
		old := time.Now().UTC().Add(-365 * 24 * time.Hour).Truncate(time.Second)

		mkFile := func(id, key string) {
			if _, err := files.Create(ctx, governancefiles.File{
				ID: id, Name: id + ".png", Mime: "image/png", SizeBytes: 1,
				ObjectKey: key, Status: governancefiles.StatusReady, CreatedAt: old, LastUsedAt: old,
			}); err != nil {
				t.Fatalf("create file %s: %v", id, err)
			}
		}
		mkFile("pf-pinned", "k/pf-pinned.png")
		mkFile("pf-free", "k/pf-free.png")
		if _, err := atts.Create(ctx, artifactattachment.Attachment{
			ID: "att-1", FeatureID: "feat", Kind: artifactattachment.KindImage,
			GovernanceFileID: "pf-pinned", Audience: artifactattachment.AudienceGate, CreatedAt: old,
		}); err != nil {
			t.Fatalf("create attachment: %v", err)
		}

		purged, err := files.DeleteStaleReady(ctx, time.Now().UTC())
		if err != nil {
			t.Fatalf("DeleteStaleReady: %v", err)
		}
		for _, k := range purged {
			if k == "k/pf-pinned.png" {
				t.Fatalf("pinned file was purged: %v", purged)
			}
		}
		if _, err := files.Get(ctx, "pf-pinned"); err != nil {
			t.Fatalf("pinned file gone after purge: %v", err)
		}
		// Control: the unpinned stale file IS purged.
		freePurged := false
		for _, k := range purged {
			if k == "k/pf-free.png" {
				freePurged = true
			}
		}
		if !freePurged {
			t.Fatalf("expected unpinned stale file to be purged; got %v", purged)
		}
	})
}

func TestArtifactAttachmentRepository_GetNotFound(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewArtifactAttachmentRepository(gdb)
		if _, err := repo.Get(context.Background(), "missing"); !errors.Is(err, artifactattachment.ErrNotFound) {
			t.Fatalf("Get missing err = %v, want ErrNotFound", err)
		}
	})
}
