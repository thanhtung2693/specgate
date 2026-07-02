package db

import (
	"context"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/artifactedit"
	"gorm.io/gorm"
)

// Durability is the point: a per-hunk decision and its actor/timestamp must
// survive a process restart. We prove that by
// reading back through a NEW repository instance over the same DB — a
// write-then-read against the same object would pass trivially and prove
// nothing.
func TestArtifactEditRepository_HunkDecisionSurvivesFreshRepo(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		ctx := context.Background()
		repo := NewArtifactEditRepository(gdb)
		now := time.Now().UTC().Truncate(time.Second)

		sess := artifactedit.Session{
			ID:             "aes-durable-" + name,
			BaseArtifactID: "art-durable-1",
			BaseVersion:    "v1.0",
			State:          "active",
			RequestedBy:    "pm@example.com",
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := repo.CreateSession(ctx, sess, map[string]string{"prd": "old metric\n"}, nil); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		if err := repo.UpdateWorkingFile(ctx, sess.ID, "prd", "new metric\n", now.Add(time.Minute)); err != nil {
			t.Fatalf("UpdateWorkingFile: %v", err)
		}
		dec := artifactedit.HunkDecision{
			ID:        "dec-durable-1",
			SessionID: sess.ID,
			HunkID:    "hunk_abc",
			FileKey:   "prd",
			State:     "applied",
			Actor:     "lead@example.com",
			DecidedAt: now.Add(2 * time.Minute),
		}
		if err := repo.AppendHunkDecision(ctx, dec); err != nil {
			t.Fatalf("AppendHunkDecision: %v", err)
		}

		fresh := NewArtifactEditRepository(gdb)
		state, err := fresh.LoadSession(ctx, sess.ID)
		if err != nil {
			t.Fatalf("LoadSession after fresh repo: %v", err)
		}
		if state.Session.RequestedBy != "pm@example.com" || state.Session.BaseVersion != "v1.0" {
			t.Fatalf("session meta not durable: %+v", state.Session)
		}
		if state.Base["prd"] != "old metric\n" {
			t.Fatalf("base file not durable: %q", state.Base["prd"])
		}
		if state.Working["prd"] != "new metric\n" {
			t.Fatalf("working file not durable: %q", state.Working["prd"])
		}
		got, ok := state.Decisions["hunk_abc"]
		if !ok {
			t.Fatalf("hunk decision not durable; decisions=%+v", state.Decisions)
		}
		if got.State != "applied" {
			t.Fatalf("decision state = %q, want applied", got.State)
		}
		if got.Actor != "lead@example.com" {
			t.Fatalf("decision actor = %q, want lead@example.com", got.Actor)
		}
		if got.DecidedAt.IsZero() {
			t.Fatal("decision timestamp not durable")
		}
	})
}

// Decisions are append-only (mirrors the impl-spec "feedback events are
// append-only" / "gate runs are immutable" house style): later decisions for a
// hunk win on read, but earlier rows remain as the audit trail.
func TestArtifactEditRepository_HunkDecisionsAreAppendOnly(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		ctx := context.Background()
		repo := NewArtifactEditRepository(gdb)
		now := time.Now().UTC().Truncate(time.Second)

		sess := artifactedit.Session{
			ID:             "aes-append-" + name,
			BaseArtifactID: "art-append-1",
			State:          "active",
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := repo.CreateSession(ctx, sess, map[string]string{"prd": "base\n"}, nil); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		if err := repo.AppendHunkDecision(ctx, artifactedit.HunkDecision{
			ID: "dec-a", SessionID: sess.ID, HunkID: "hunk_x", FileKey: "prd",
			State: "applied", Actor: "a@example.com", DecidedAt: now.Add(time.Minute),
		}); err != nil {
			t.Fatalf("AppendHunkDecision 1: %v", err)
		}
		if err := repo.AppendHunkDecision(ctx, artifactedit.HunkDecision{
			ID: "dec-b", SessionID: sess.ID, HunkID: "hunk_x", FileKey: "prd",
			State: "rejected", Actor: "b@example.com", DecidedAt: now.Add(2 * time.Minute),
		}); err != nil {
			t.Fatalf("AppendHunkDecision 2: %v", err)
		}

		state, err := repo.LoadSession(ctx, sess.ID)
		if err != nil {
			t.Fatalf("LoadSession: %v", err)
		}
		if got := state.Decisions["hunk_x"]; got.State != "rejected" || got.Actor != "b@example.com" {
			t.Fatalf("latest decision = %+v, want rejected/b@example.com", got)
		}

		var count int64
		if err := gdb.WithContext(ctx).
			Table("artifact_edit_hunk_decisions").
			Where("session_id = ? AND hunk_id = ?", sess.ID, "hunk_x").
			Count(&count).Error; err != nil {
			t.Fatalf("count audit rows: %v", err)
		}
		if count != 2 {
			t.Fatalf("audit row count = %d, want 2 (append-only history)", count)
		}
	})
}

func TestArtifactEditRepository_RevisionRoundTrip(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		ctx := context.Background()
		repo := NewArtifactEditRepository(gdb)
		now := time.Now().UTC().Truncate(time.Second)

		sess := artifactedit.Session{
			ID:             "aes-rev-" + name,
			BaseArtifactID: "art-rev-1",
			State:          "active",
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := repo.CreateSession(ctx, sess, map[string]string{"prd": "base\n"}, nil); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		rev := artifactedit.Revision{
			RevisionID:     "aer-rev-1",
			BaseArtifactID: "art-rev-1",
			State:          "saved",
			SessionID:      sess.ID,
			Summary:        "Updated PRD metric",
			DiffJSON:       `{"summary":"1 file(s) changed"}`,
			CreatedAt:      now,
		}
		if err := repo.CreateRevision(ctx, rev); err != nil {
			t.Fatalf("CreateRevision: %v", err)
		}

		fresh := NewArtifactEditRepository(gdb)
		got, err := fresh.GetRevision(ctx, "aer-rev-1")
		if err != nil {
			t.Fatalf("GetRevision: %v", err)
		}
		if got.Summary != "Updated PRD metric" || got.DiffJSON != `{"summary":"1 file(s) changed"}` {
			t.Fatalf("revision not durable: %+v", got)
		}
		list, err := fresh.ListRevisions(ctx, "art-rev-1")
		if err != nil {
			t.Fatalf("ListRevisions: %v", err)
		}
		if len(list) != 1 || list[0].RevisionID != "aer-rev-1" {
			t.Fatalf("ListRevisions = %+v, want one aer-rev-1", list)
		}
	})
}

func TestArtifactEditRepository_RevisionLineageSurvivesFreshRepo(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		ctx := context.Background()
		repo := NewArtifactEditRepository(gdb)
		now := time.Now().UTC().Truncate(time.Second)

		sess := artifactedit.Session{
			ID:             "aes-lin-" + name,
			BaseArtifactID: "art-lin-1",
			State:          "active",
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := repo.CreateSession(ctx, sess, map[string]string{"prd": "base\n"}, nil); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		rev := artifactedit.Revision{
			RevisionID:            "aer-lin-1",
			BaseArtifactID:        "art-lin-1",
			State:                 "saved",
			SessionID:             sess.ID,
			Summary:               "x",
			DiffJSON:              "{}",
			ParentRevisionID:      "aer-parent",
			LineageRootArtifactID: "art-root",
			CreatedAt:             now,
		}
		if err := repo.CreateRevision(ctx, rev); err != nil {
			t.Fatalf("CreateRevision: %v", err)
		}

		fresh := NewArtifactEditRepository(gdb)
		got, err := fresh.GetRevision(ctx, "aer-lin-1")
		if err != nil {
			t.Fatalf("GetRevision: %v", err)
		}
		if got.ParentRevisionID != "aer-parent" || got.LineageRootArtifactID != "art-root" {
			t.Fatalf("lineage not durable: %+v", got)
		}
	})
}

// A proposal is an edit session tagged with its origin. The review queue lists
// only sourced sessions still awaiting a verdict (state=active); plain edit
// sessions and already-resolved proposals are excluded.
func TestArtifactEditRepository_ListProposals(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		ctx := context.Background()
		repo := NewArtifactEditRepository(gdb)
		now := time.Now().UTC().Truncate(time.Second)

		mk := func(id, kind, sourceID, state string) {
			if err := repo.CreateSession(ctx, artifactedit.Session{
				ID: id, BaseArtifactID: "art-prop", State: "active",
				SourceKind: kind, SourceID: sourceID, CreatedAt: now, UpdatedAt: now,
			}, map[string]string{"prd": "base\n"}, nil); err != nil {
				t.Fatalf("CreateSession %s: %v", id, err)
			}
			if state != "active" {
				if err := repo.SetSessionMeta(ctx, id, artifactedit.SessionMeta{State: state, UpdatedAt: now}); err != nil {
					t.Fatalf("SetSessionMeta %s: %v", id, err)
				}
			}
		}
		mk("aes-prop-pending-"+name, "feedback_event", "pfe-1", "active")
		mk("aes-plain-"+name, "", "", "active")                        // not a proposal
		mk("aes-prop-saved-"+name, "feedback_event", "pfe-2", "saved") // resolved

		props, err := repo.ListProposals(ctx)
		if err != nil {
			t.Fatalf("ListProposals: %v", err)
		}
		if len(props) != 1 {
			t.Fatalf("ListProposals returned %d, want 1 (only sourced+active): %+v", len(props), props)
		}
		got := props[0]
		if got.ID != "aes-prop-pending-"+name {
			t.Fatalf("proposal id = %q", got.ID)
		}
		if got.SourceKind != "feedback_event" || got.SourceID != "pfe-1" {
			t.Fatalf("proposal source = %q/%q, want feedback_event/pfe-1", got.SourceKind, got.SourceID)
		}

		// Source link also round-trips through LoadSession.
		state, err := repo.LoadSession(ctx, "aes-prop-pending-"+name)
		if err != nil {
			t.Fatalf("LoadSession: %v", err)
		}
		if state.Session.SourceKind != "feedback_event" || state.Session.SourceID != "pfe-1" {
			t.Fatalf("loaded source = %q/%q", state.Session.SourceKind, state.Session.SourceID)
		}
	})
}

// CreateSession seeds resolved working overrides atomically: the working side
// reflects the override while base stays the artifact content, and omitted keys
// start equal to base.
func TestArtifactEditRepository_CreateSessionSeedsWorkingOverrides(t *testing.T) {
	forEachDriver(t, func(t *testing.T, name string, gdb *gorm.DB) {
		ctx := context.Background()
		repo := NewArtifactEditRepository(gdb)
		now := time.Now().UTC().Truncate(time.Second)
		id := "aes-seed-" + name
		base := map[string]string{"prd": "base prd\n", "spec": "base spec\n"}
		working := map[string]string{"prd": "resolved prd\n"} // spec omitted → stays base
		if err := repo.CreateSession(ctx, artifactedit.Session{
			ID: id, BaseArtifactID: "art-seed", State: "active", CreatedAt: now, UpdatedAt: now,
		}, base, working); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}

		state, err := repo.LoadSession(ctx, id)
		if err != nil {
			t.Fatalf("LoadSession: %v", err)
		}
		if state.Base["prd"] != "base prd\n" || state.Base["spec"] != "base spec\n" {
			t.Fatalf("base = %+v, want unchanged artifact content", state.Base)
		}
		if state.Working["prd"] != "resolved prd\n" {
			t.Fatalf("working[prd] = %q, want resolved override", state.Working["prd"])
		}
		if state.Working["spec"] != "base spec\n" {
			t.Fatalf("working[spec] = %q, want base (omitted from overrides)", state.Working["spec"])
		}
	})
}

func TestArtifactEditRepository_LoadSessionNotFound(t *testing.T) {
	forEachDriver(t, func(t *testing.T, _ string, gdb *gorm.DB) {
		ctx := context.Background()
		repo := NewArtifactEditRepository(gdb)
		if _, err := repo.LoadSession(ctx, "missing"); err != artifactedit.ErrNotFound {
			t.Fatalf("LoadSession(missing) err = %v, want ErrNotFound", err)
		}
	})
}
