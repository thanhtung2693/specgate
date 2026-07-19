package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/workboard"
)

func newArtifact(id, featureID, version string, status artifact.Status, t0 time.Time) *artifact.Artifact {
	return &artifact.Artifact{
		ID:          id,
		WorkspaceID: "ws-test",
		FeatureID:   featureID,
		Version:     version,
		Status:      status,
		RequestType: artifact.RequestTypeNewFeature,
		CreatedBy:   "tester",
		CreatedAt:   t0,
		UpdatedAt:   t0,
	}
}

func TestRepository_WorkspaceScopedArtifacts(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		a := newArtifact("art-ws-a", "feat-shared", "v0.1", artifact.StatusDraft, now)
		a.WorkspaceID = "ws-a"
		b := newArtifact("art-ws-b", "feat-shared", "v0.1", artifact.StatusDraft, now)
		b.WorkspaceID = "ws-b"
		if err := repo.Insert(ctx, a); err != nil {
			t.Fatal(err)
		}
		if err := repo.Insert(ctx, b); err != nil {
			t.Fatal(err)
		}

		got, err := repo.GetInWorkspace(ctx, "ws-a", a.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != a.ID || got.WorkspaceID != "ws-a" {
			t.Fatalf("got artifact %+v, want ws-a artifact", got)
		}
		if _, err := repo.GetInWorkspace(ctx, "ws-a", b.ID); err != ErrNotFound {
			t.Fatalf("cross-workspace get err=%v, want ErrNotFound", err)
		}
		list, err := repo.List(
			artifact.WithWorkspace(ctx, "ws-a"),
			artifact.ListFilter{FeatureID: "feat-shared"},
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].ID != a.ID {
			t.Fatalf("workspace list=%+v, want only %s", list, a.ID)
		}

		wsA := artifact.WithWorkspace(ctx, "ws-a")
		event := artifact.Event{
			ID:         "cross-workspace-status",
			ArtifactID: b.ID,
			EventType:  artifact.EventApproved,
			Payload:    "{}",
			CreatedAt:  now,
		}
		if err := repo.UpdateStatus(wsA, b.ID, artifact.StatusApproved, "approver", event); err != ErrNotFound {
			t.Fatalf("cross-workspace status update err=%v, want ErrNotFound", err)
		}
		if err := repo.Delete(wsA, b.ID); err != ErrNotFound {
			t.Fatalf("cross-workspace delete err=%v, want ErrNotFound", err)
		}
		unchanged, err := repo.GetInWorkspace(ctx, "ws-b", b.ID)
		if err != nil {
			t.Fatal(err)
		}
		if unchanged.Status != artifact.StatusDraft {
			t.Fatalf("cross-workspace artifact status=%q, want draft", unchanged.Status)
		}

		ownEvent := artifact.Event{
			ID:         "same-workspace-status",
			ArtifactID: a.ID,
			EventType:  artifact.EventApproved,
			Payload:    "{}",
			CreatedAt:  now,
		}
		if err := repo.UpdateStatus(wsA, a.ID, artifact.StatusApproved, "approver", ownEvent); err != nil {
			t.Fatal(err)
		}
		events, err := repo.ListEvents(wsA, artifact.EventFilter{WorkspaceID: "ws-b"})
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 0 {
			t.Fatalf("cross-workspace events = %+v, want none", events)
		}
	})
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

		a3 := newArtifact("l3", "feat-x", "v0.9", artifact.StatusSuperseded, now.Add(2*time.Minute))
		if err := repo.Insert(ctx, a3); err != nil {
			t.Fatal(err)
		}
		current, err := repo.List(ctx, artifact.ListFilter{FeatureID: "feat-x", ExcludeStatus: artifact.StatusSuperseded, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(current) != 2 {
			t.Fatalf("excludeStatus len=%d, want 2 (no superseded)", len(current))
		}
		for _, a := range current {
			if a.Status == artifact.StatusSuperseded {
				t.Fatalf("superseded artifact leaked through ExcludeStatus: %+v", a)
			}
		}
		total, err := repo.Count(ctx, artifact.ListFilter{FeatureID: "feat-x", ExcludeStatus: artifact.StatusSuperseded})
		if err != nil {
			t.Fatal(err)
		}
		if total != 2 {
			t.Fatalf("excludeStatus count=%d, want 2", total)
		}
	})
}

func TestRepository_DeleteArtifactGateRows(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewRepository(gdb)
		ctx := context.Background()

		swept := "11111111-1111-1111-1111-111111111111"
		kept := "22222222-2222-2222-2222-222222222222"
		for _, stmt := range []string{
			"INSERT INTO gate_runs (id, workspace_id, subject_kind, subject_id, gate, state) VALUES ('gr-swept', 'ws-default', 'artifact', '" + swept + "', 'spec_completeness', 'pass')",
			"INSERT INTO gate_runs (id, workspace_id, subject_kind, subject_id, gate, state) VALUES ('gr-kept', 'ws-default', 'artifact', '" + kept + "', 'spec_completeness', 'pass')",
			"INSERT INTO gate_tasks (workspace_id, artifact_id, gate_key, gate_version, gate_digest, artifact_digest, policy_digest, executor, expires_at) VALUES ('ws-default', '" + swept + "', 'g', 'v1', 'd', 'd', 'd', 'ide', NOW())",
			"INSERT INTO gate_tasks (workspace_id, artifact_id, gate_key, gate_version, gate_digest, artifact_digest, policy_digest, executor, expires_at) VALUES ('ws-default', '" + kept + "', 'g', 'v1', 'd', 'd', 'd', 'ide', NOW())",
		} {
			if err := gdb.Exec(stmt).Error; err != nil {
				t.Fatal(err)
			}
		}

		if err := repo.DeleteArtifactGateRows(ctx, []string{swept}); err != nil {
			t.Fatalf("DeleteArtifactGateRows: %v", err)
		}

		var runs, tasks int64
		if err := gdb.Raw("SELECT COUNT(*) FROM gate_runs WHERE subject_id = ?", swept).Scan(&runs).Error; err != nil {
			t.Fatal(err)
		}
		if err := gdb.Raw("SELECT COUNT(*) FROM gate_tasks WHERE artifact_id::text = ?", swept).Scan(&tasks).Error; err != nil {
			t.Fatal(err)
		}
		if runs != 0 || tasks != 0 {
			t.Fatalf("swept artifact rows remain: runs=%d tasks=%d", runs, tasks)
		}
		if err := gdb.Raw("SELECT COUNT(*) FROM gate_runs WHERE subject_id = ?", kept).Scan(&runs).Error; err != nil {
			t.Fatal(err)
		}
		if runs != 1 {
			t.Fatalf("kept artifact gate run deleted, count=%d", runs)
		}
		if err := repo.DeleteArtifactGateRows(ctx, nil); err != nil {
			t.Fatalf("nil ids should be a no-op: %v", err)
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
			EventType:  artifact.EventApproved,
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

func TestRepository_UpdateStatus_ApprovalSupersedesPriorVersions(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		// Same feature: an abandoned old draft, the prior approved canonical,
		// and the new version being approved. A different feature must be left alone.
		for _, a := range []*artifact.Artifact{
			newArtifact("old-draft", "feat-x", "v0.1", artifact.StatusDraft, now),
			newArtifact("prev-approved", "feat-x", "v0.2", artifact.StatusApproved, now),
			newArtifact("new-ver", "feat-x", "v0.3", artifact.StatusDraft, now),
			newArtifact("other-feat", "feat-y", "v0.1", artifact.StatusDraft, now),
		} {
			if err := repo.Insert(ctx, a); err != nil {
				t.Fatal(err)
			}
		}

		ev := artifact.Event{ID: "ev-approve", ArtifactID: "new-ver", EventType: artifact.EventApproved, Payload: "{}", CreatedAt: now}
		if err := repo.UpdateStatus(ctx, "new-ver", artifact.StatusApproved, "approver", ev); err != nil {
			t.Fatal(err)
		}

		wantStatus := map[string]artifact.Status{
			"new-ver":       artifact.StatusApproved,   // the approved version
			"old-draft":     artifact.StatusSuperseded, // prior draft superseded
			"prev-approved": artifact.StatusSuperseded, // prior approved superseded
			"other-feat":    artifact.StatusDraft,      // different feature untouched
		}
		for id, want := range wantStatus {
			got, err := repo.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != want {
				t.Fatalf("%s status = %q, want %q", id, got.Status, want)
			}
		}
	})
}

func TestRepository_UpdateStatus_ApprovalDoesNotSupersedeNewerVersions(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		// v0.2 is the current live version; v0.1 was superseded by it. Re-approving
		// the older v0.1 must NOT flip the newer v0.2 to superseded.
		for _, a := range []*artifact.Artifact{
			newArtifact("old-v1", "feat-z", "v0.1", artifact.StatusSuperseded, now),
			newArtifact("live-v2", "feat-z", "v0.2", artifact.StatusApproved, now),
		} {
			if err := repo.Insert(ctx, a); err != nil {
				t.Fatal(err)
			}
		}

		ev := artifact.Event{ID: "ev-reapprove", ArtifactID: "old-v1", EventType: artifact.EventApproved, Payload: "{}", CreatedAt: now}
		if err := repo.UpdateStatus(ctx, "old-v1", artifact.StatusApproved, "approver", ev); err != nil {
			t.Fatal(err)
		}

		live, err := repo.Get(ctx, "live-v2")
		if err != nil {
			t.Fatal(err)
		}
		if live.Status != artifact.StatusApproved {
			t.Fatalf("newer v0.2 status = %q after re-approving older v0.1, want approved (must not be superseded)", live.Status)
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

func TestRepository_DeleteRejectsReferencedArtifact(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		artifactRepo := NewRepository(gdb)
		workboardRepo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(
			artifact.WithWorkspace(context.Background(), "ws-retention"),
			"ws-retention",
		)
		now := time.Now().UTC().Truncate(time.Second)

		feature, err := workboardRepo.CreateFeature(ctx, workboard.Feature{
			Key:    "FEAT-RETENTION",
			Name:   "Retention",
			Status: workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}
		referenced := newArtifact(
			"art-retention-referenced",
			feature.ID,
			"v1.0",
			artifact.StatusSuperseded,
			now,
		)
		referenced.WorkspaceID = "ws-retention"
		if err := artifactRepo.Insert(ctx, referenced); err != nil {
			t.Fatal(err)
		}
		if _, err := workboardRepo.CreateChangeRequest(ctx, workboard.ChangeRequest{
			Key:            "CR-RETENTION",
			FeatureID:      feature.ID,
			WorkType:       workboard.WorkTypeCleanup,
			Title:          "Keep exact governed version",
			LeadArtifactID: referenced.ID,
		}); err != nil {
			t.Fatal(err)
		}

		if err := artifactRepo.Delete(ctx, referenced.ID); err == nil {
			t.Fatal("Delete succeeded for an artifact referenced by governed work")
		}
		if _, err := artifactRepo.Get(ctx, referenced.ID); err != nil {
			t.Fatalf("referenced artifact was deleted: %v", err)
		}
	})
}

func TestRepository_DeleteRechecksReferencesAfterWaitingForLink(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		artifactRepo := NewRepository(gdb)
		workboardRepo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(
			artifact.WithWorkspace(context.Background(), "ws-retention-race"),
			"ws-retention-race",
		)
		now := time.Now().UTC().Truncate(time.Second)

		feature, err := workboardRepo.CreateFeature(ctx, workboard.Feature{
			Key:    "FEAT-RETENTION-RACE",
			Name:   "Retention race",
			Status: workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}
		referenced := newArtifact(
			"art-retention-race",
			feature.ID,
			"v1.0",
			artifact.StatusSuperseded,
			now,
		)
		referenced.WorkspaceID = "ws-retention-race"
		if err := artifactRepo.Insert(ctx, referenced); err != nil {
			t.Fatal(err)
		}

		linkTx := gdb.WithContext(ctx).Begin()
		if linkTx.Error != nil {
			t.Fatal(linkTx.Error)
		}
		defer linkTx.Rollback()
		var locked artifact.Artifact
		if err := linkTx.Clauses(clause.Locking{Strength: "SHARE"}).
			First(&locked, "id = ?", referenced.ID).Error; err != nil {
			t.Fatal(err)
		}

		deleteDone := make(chan error, 1)
		go func() {
			deleteDone <- artifactRepo.Delete(ctx, referenced.ID)
		}()
		select {
		case err := <-deleteDone:
			t.Fatalf("Delete returned before the link transaction released its artifact lock: %v", err)
		case <-time.After(100 * time.Millisecond):
		}

		if err := linkTx.Create(&workboard.ChangeRequest{
			ID:             "cr-retention-race",
			Key:            "CR-RETENTION-RACE",
			FeatureID:      feature.ID,
			WorkType:       workboard.WorkTypeCleanup,
			Title:          "Bind while cleanup waits",
			LeadArtifactID: referenced.ID,
			WorkspaceID:    "ws-retention-race",
			CreatedAt:      now,
			UpdatedAt:      now,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := linkTx.Commit().Error; err != nil {
			t.Fatal(err)
		}

		select {
		case err := <-deleteDone:
			if !errors.Is(err, ErrArtifactReferenced) {
				t.Fatalf("Delete error = %v, want ErrArtifactReferenced", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Delete did not resume after the link transaction committed")
		}
		if _, err := artifactRepo.Get(ctx, referenced.ID); err != nil {
			t.Fatalf("referenced artifact was deleted: %v", err)
		}
	})
}

func TestRepository_DeleteRechecksReferencesAfterWaitingForCanonicalLink(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		artifactRepo := NewRepository(gdb)
		workboardRepo := NewWorkBoardRepository(gdb)
		ctx := workboard.WithWorkspace(
			artifact.WithWorkspace(context.Background(), "ws-canonical-retention-race"),
			"ws-canonical-retention-race",
		)
		now := time.Now().UTC().Truncate(time.Second)

		feature, err := workboardRepo.CreateFeature(ctx, workboard.Feature{
			Key:    "FEAT-CANONICAL-RETENTION-RACE",
			Name:   "Canonical retention race",
			Status: workboard.FeatureStatusPlanned,
		})
		if err != nil {
			t.Fatal(err)
		}
		referenced := newArtifact(
			"art-canonical-retention-race",
			feature.ID,
			"v1.0",
			artifact.StatusApproved,
			now,
		)
		referenced.WorkspaceID = "ws-canonical-retention-race"
		if err := artifactRepo.Insert(ctx, referenced); err != nil {
			t.Fatal(err)
		}

		linkTx := gdb.WithContext(ctx).Begin()
		if linkTx.Error != nil {
			t.Fatal(linkTx.Error)
		}
		defer linkTx.Rollback()
		if err := validateCanonicalArtifact(linkTx, referenced.ID, referenced.WorkspaceID); err != nil {
			t.Fatal(err)
		}

		deleteDone := make(chan error, 1)
		go func() {
			deleteDone <- artifactRepo.Delete(ctx, referenced.ID)
		}()
		select {
		case err := <-deleteDone:
			t.Fatalf("Delete returned before the canonical-link transaction released its artifact lock: %v", err)
		case <-time.After(100 * time.Millisecond):
		}

		if err := linkTx.Model(&workboard.Feature{}).
			Where("id = ? AND workspace_id = ?", feature.ID, referenced.WorkspaceID).
			Update("canonical_artifact_id", referenced.ID).Error; err != nil {
			t.Fatal(err)
		}
		if err := linkTx.Commit().Error; err != nil {
			t.Fatal(err)
		}

		select {
		case err := <-deleteDone:
			if !errors.Is(err, ErrArtifactReferenced) {
				t.Fatalf("Delete error = %v, want ErrArtifactReferenced", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Delete did not resume after the canonical-link transaction committed")
		}
		if _, err := artifactRepo.Get(ctx, referenced.ID); err != nil {
			t.Fatalf("canonical artifact was deleted: %v", err)
		}
	})
}

// Every artifact_events write site must chain (spec §8): publish
// (InsertWithEvent), status update (UpdateStatus), and the supersede cascade.
// The stored rows must re-verify from what Postgres actually persisted (µs
// timestamp precision), not from in-memory values.
func TestRepository_EventWritesAreChained(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		repo := NewRepository(gdb)
		ctx := context.Background()
		now := time.Now().UTC()

		// Timestamps use the real clock at each step: the chain's link order is
		// its created_at order, exactly as production writes behave.
		a := newArtifact("chain-a", "feat-chain", "v0.1", artifact.StatusDraft, now)
		if err := repo.InsertWithEvent(ctx, a, artifact.Event{
			ID: "chain-ev-1", ArtifactID: "chain-a", EventType: artifact.EventPublished,
			Payload: `{"status":"draft"}`, CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
		if err := repo.UpdateStatus(ctx, "chain-a", artifact.StatusApproved, "approver", artifact.Event{
			ID: "chain-ev-2", ArtifactID: "chain-a", EventType: artifact.EventApproved,
			Payload: `{"status":"approved","actor":"approver"}`, CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}

		events, err := repo.ListEvents(ctx, artifact.EventFilter{ArtifactID: "chain-a"})
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 2 {
			t.Fatalf("events = %d, want 2", len(events))
		}
		if events[0].Hash == "" || events[0].PrevHash != "" {
			t.Fatalf("first event not chained as genesis: %+v", events[0])
		}
		if events[1].PrevHash != events[0].Hash {
			t.Fatalf("second event prev %q != first hash %q", events[1].PrevHash, events[1].Hash)
		}
		if report := artifact.VerifyEventChain(events); report.State != artifact.ChainIntact {
			t.Fatalf("stored chain must re-verify: %+v", report)
		}

		// The supersede cascade (approving v0.2 supersedes v0.1) must chain the
		// superseded event onto the OLD artifact's chain.
		b := newArtifact("chain-b", "feat-chain", "v0.2", artifact.StatusDraft, now)
		if err := repo.InsertWithEvent(ctx, b, artifact.Event{
			ID: "chain-ev-3", ArtifactID: "chain-b", EventType: artifact.EventPublished,
			Payload: `{"status":"draft"}`, CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
		if err := repo.UpdateStatus(ctx, "chain-b", artifact.StatusApproved, "approver", artifact.Event{
			ID: "chain-ev-4", ArtifactID: "chain-b", EventType: artifact.EventApproved,
			Payload: `{"status":"approved"}`, CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
		aEvents, err := repo.ListEvents(ctx, artifact.EventFilter{ArtifactID: "chain-a"})
		if err != nil {
			t.Fatal(err)
		}
		if len(aEvents) != 3 {
			t.Fatalf("chain-a events = %d, want 3 (publish, approve, superseded)", len(aEvents))
		}
		if report := artifact.VerifyEventChain(aEvents); report.State != artifact.ChainIntact {
			t.Fatalf("chain with superseded cascade must re-verify: %+v", report)
		}
	})
}
