package artifact

import (
	"context"
	"strings"
	"testing"
	"time"
)

// memRepo is an in-memory Repository used only by service_publish_test.go.
// Methods not needed by Publish/Get tests panic to surface unexpected calls.
type memRepo struct {
	artifacts         map[string]*Artifact
	events            []Event
	readiness         []ReadinessRun
	conflictArtifacts []Artifact // returned by FindOverlappingServices; nil = no conflicts
}

func newMemRepo() *memRepo {
	return &memRepo{artifacts: map[string]*Artifact{}}
}

func (r *memRepo) InsertWithEvent(_ context.Context, a *Artifact, e Event) error {
	cp := *a
	cp.Files = append([]File(nil), a.Files...)
	cp.Services = append([]ServiceRef(nil), a.Services...)
	r.artifacts[a.ID] = &cp
	r.events = append(r.events, e)
	return nil
}

func (r *memRepo) Get(_ context.Context, id string) (*Artifact, error) {
	a, ok := r.artifacts[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *a
	cp.Files = append([]File(nil), a.Files...)
	return &cp, nil
}

func (r *memRepo) List(_ context.Context, f ListFilter) ([]Artifact, error) {
	out := make([]Artifact, 0, len(r.artifacts))
	for _, a := range r.artifacts {
		if f.FeatureID != "" && a.FeatureID != f.FeatureID {
			continue
		}
		out = append(out, *a)
	}
	return out, nil
}
func (r *memRepo) Count(context.Context, ListFilter) (int64, error) { panic("unexpected") }
func (r *memRepo) InsertReadinessRuns(_ context.Context, rows []ReadinessRun) error {
	r.readiness = append(r.readiness, rows...)
	return nil
}
func (r *memRepo) ListReadinessRuns(_ context.Context, artifactID string, limit int) ([]ReadinessRun, error) {
	if limit <= 0 || limit > len(r.readiness) {
		limit = len(r.readiness)
	}
	out := make([]ReadinessRun, 0, limit)
	for _, row := range r.readiness {
		if row.ArtifactID == artifactID {
			out = append(out, row)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (r *memRepo) UpdateStatus(context.Context, string, Status, string, Event) error {
	panic("unexpected")
}
func (r *memRepo) Delete(context.Context, string) error { panic("unexpected") }
func (r *memRepo) FindOverlappingServices(_ context.Context, _ []string, _ string) ([]Artifact, error) {
	return r.conflictArtifacts, nil
}
func (r *memRepo) ListEvents(context.Context, EventFilter) ([]Event, error) {
	return r.events, nil
}
func (r *memRepo) AppendEvent(_ context.Context, ev Event) error {
	r.events = append(r.events, ev)
	return nil
}

// memStore is an in-memory ObjectStore.
type memStore struct {
	objects map[string][]byte
}

func newMemStore() *memStore {
	return &memStore{objects: map[string][]byte{}}
}

func (s *memStore) PutObject(_ context.Context, key string, body []byte) error {
	s.objects[key] = append([]byte(nil), body...)
	return nil
}
func (s *memStore) GetObject(_ context.Context, key string) ([]byte, error) {
	b, ok := s.objects[key]
	if !ok {
		return nil, ErrFileNotFound
	}
	return append([]byte(nil), b...), nil
}
func (s *memStore) PresignGet(_ context.Context, key string) (string, error) {
	return "https://fake-s3/" + key, nil
}
func (s *memStore) DeleteObject(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}

// testObjectKey mirrors the production formula: artifacts/<feature>/<version>/<path>.
func testObjectKey(featureID, version, path string) string {
	return "artifacts/" + featureID + "/" + version + "/" + path
}

// newTestService returns a RegistryService backed by in-memory dependencies.
func newTestService(t *testing.T) (*RegistryService, *memRepo, *memStore) {
	t.Helper()
	repo := newMemRepo()
	store := newMemStore()
	svc := NewService(repo, store, testObjectKey, time.Minute)
	return svc, repo, store
}

// seedArtifact inserts a minimal artifact directly into the in-memory repo so
// version-resolution tests have a "latest" to derive from.
func seedArtifact(r *memRepo, id, featureID, version string) {
	r.artifacts[id] = &Artifact{
		ID:        id,
		FeatureID: featureID,
		Version:   version,
		Status:    StatusDraft,
	}
}

// minPublishInput fills all required fields so tests only set the parts they care about.
func minPublishInput() PublishInput {
	return PublishInput{
		FeatureID:   "checkout-loyalty",
		Version:     "v0.1",
		RequestType: RequestTypeNewFeature,
		ImpactLevel: ImpactLevelLow,
	}
}

func TestPublish_DocumentByPathAndRole_EnvelopeFields(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newTestService(t)

	in := minPublishInput()
	in.SourceKind = "openspec"
	in.SourceRevision = "abc123"
	in.GatesProfile = "generic_change"
	in.GatesProfileVersion = "1"
	in.GatesProfileDigest = "sha256:generic"
	in.GatesProfileSnapshotJSON = `{"key":"generic_change","version":"1"}`
	in.Documents = []DocumentInput{
		{Path: "docs/proposal.md", Role: "spec", Content: []byte("# P")},
	}

	ctx := context.Background()
	a, err := svc.Publish(ctx, in)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Read back from repo to verify stored state.
	stored, err := repo.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// Exactly one file.
	if len(stored.Files) != 1 {
		t.Fatalf("want 1 file, got %d", len(stored.Files))
	}
	f := stored.Files[0]
	if f.Path != "docs/proposal.md" {
		t.Errorf("Path = %q, want %q", f.Path, "docs/proposal.md")
	}
	if f.Role != RoleSpec {
		t.Errorf("Role = %q, want %q", f.Role, RoleSpec)
	}
	wantSuffix := "artifacts/checkout-loyalty/v0.1/docs/proposal.md"
	if !strings.HasSuffix(f.S3Path, wantSuffix) {
		t.Errorf("S3Path = %q, want suffix %q", f.S3Path, wantSuffix)
	}

	// Envelope fields persisted.
	if stored.SourceKind != "openspec" {
		t.Errorf("SourceKind = %q, want %q", stored.SourceKind, "openspec")
	}
	if stored.SourceRevision != "abc123" {
		t.Errorf("SourceRevision = %q, want %q", stored.SourceRevision, "abc123")
	}
	if stored.GatesProfile != "generic_change" {
		t.Errorf("GatesProfile = %q, want %q", stored.GatesProfile, "generic_change")
	}
	if stored.GatesProfileVersion != "1" {
		t.Errorf("GatesProfileVersion = %q, want %q", stored.GatesProfileVersion, "1")
	}
	if stored.GatesProfileDigest != "sha256:generic" {
		t.Errorf("GatesProfileDigest = %q, want %q", stored.GatesProfileDigest, "sha256:generic")
	}
	if stored.GatesProfileSnapshotJSON != `{"key":"generic_change","version":"1"}` {
		t.Errorf("GatesProfileSnapshotJSON = %q", stored.GatesProfileSnapshotJSON)
	}
}

func TestPublish_PathTraversalRejected(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)
	ctx := context.Background()

	cases := []struct {
		name string
		path string
	}{
		{"dotdot-prefix", "../escape.md"},
		{"absolute", "/abs.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := minPublishInput()
			in.Documents = []DocumentInput{
				{Path: tc.path, Role: "spec", Content: []byte("x")},
			}
			_, err := svc.Publish(ctx, in)
			if err == nil {
				t.Fatal("Publish returned nil error, want path rejection error")
			}
		})
	}
}

func TestPublish_EmptyRoleDefaultsToUnspecified(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newTestService(t)
	ctx := context.Background()

	in := minPublishInput()
	in.Version = "v0.2" // distinct version to avoid uniqueness conflicts in parallel
	in.Documents = []DocumentInput{
		{Path: "note.md", Role: "", Content: []byte("hello")},
	}

	a, err := svc.Publish(ctx, in)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	stored, err := repo.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(stored.Files) != 1 {
		t.Fatalf("want 1 file, got %d", len(stored.Files))
	}
	if stored.Files[0].Role != RoleUnspecified {
		t.Errorf("Role = %q, want %q", stored.Files[0].Role, RoleUnspecified)
	}
}

func TestPublish_LineageParentPersisted(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newTestService(t)
	ctx := context.Background()

	in := minPublishInput()
	in.Version = "v0.3"
	in.ParentArtifactID = "a-1"
	in.LineageRootID = "root-0"
	in.Documents = []DocumentInput{
		{Path: "spec.md", Role: "spec", Content: []byte("# S")},
	}

	a, err := svc.Publish(ctx, in)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	stored, err := repo.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.ParentArtifactID != "a-1" {
		t.Errorf("ParentArtifactID = %q, want %q", stored.ParentArtifactID, "a-1")
	}
	if stored.LineageRootID != "root-0" {
		t.Errorf("LineageRootID = %q, want %q", stored.LineageRootID, "root-0")
	}
}

func TestPublish_EmptyLineageRootSelfRoots(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newTestService(t)
	ctx := context.Background()

	in := minPublishInput()
	in.Version = "v0.4"
	// LineageRootID intentionally empty — service should default to artifact's own ID.
	in.Documents = []DocumentInput{
		{Path: "spec.md", Role: "spec", Content: []byte("# First")},
	}

	a, err := svc.Publish(ctx, in)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	stored, err := repo.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.LineageRootID != stored.ID {
		t.Errorf("LineageRootID = %q, want own ID %q", stored.LineageRootID, stored.ID)
	}
	if stored.ParentArtifactID != "" {
		t.Errorf("ParentArtifactID = %q, want empty for first-in-chain", stored.ParentArtifactID)
	}
}

// TestPublish_BlockingConflictKeepsRequestedStatus asserts conflict detection
// is advisory only (spec §10): an overlap with an active artifact no longer
// overrides the requested status at publish time.
func TestPublish_BlockingConflictKeepsRequestedStatus(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newTestService(t)
	// Seed a conflicting artifact that FindOverlappingServices will return.
	repo.conflictArtifacts = []Artifact{
		{
			ID:        "existing-1",
			FeatureID: "other-feature",
			Version:   "v1.0",
			Status:    StatusApproved, // approved → blocking_conflict report
			Services:  []ServiceRef{{Name: "payments-service", Kind: "service"}},
		},
	}
	ctx := context.Background()

	in := minPublishInput()
	in.Status = StatusDraft
	in.ImpactedServices = []ServiceRef{{Name: "payments-service", Kind: "service"}}

	a, err := svc.Publish(ctx, in)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	stored, _ := repo.Get(ctx, a.ID)
	if stored.Status != StatusDraft {
		t.Errorf("Status = %q, want %q", stored.Status, StatusDraft)
	}
}

func TestPublish_NoConflictPreservesStatus(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newTestService(t)
	// No conflicting artifacts (nil → no_conflict).
	ctx := context.Background()

	in := minPublishInput()
	in.Status = StatusDraft
	in.ImpactedServices = []ServiceRef{{Name: "payments-service", Kind: "service"}}

	a, err := svc.Publish(ctx, in)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	stored, _ := repo.Get(ctx, a.ID)
	if stored.Status != StatusDraft {
		t.Errorf("Status = %q, want %q", stored.Status, StatusDraft)
	}
}

func TestPublish_WarningConflictPreservesStatus(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newTestService(t)
	// Seed an artifact that causes only a warning (status=superseded — not draft/approved).
	repo.conflictArtifacts = []Artifact{
		{
			ID:        "existing-2",
			FeatureID: "other-feature",
			Version:   "v1.0",
			Status:    StatusSuperseded, // superseded → only warning_conflict
			Services:  []ServiceRef{{Name: "payments-service", Kind: "service"}},
		},
	}
	ctx := context.Background()

	in := minPublishInput()
	in.Status = StatusDraft
	in.ImpactedServices = []ServiceRef{{Name: "payments-service", Kind: "service"}}

	a, err := svc.Publish(ctx, in)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	stored, _ := repo.Get(ctx, a.ID)
	if stored.Status != StatusDraft {
		t.Errorf("warning conflict should not block: Status = %q, want %q", stored.Status, StatusDraft)
	}
}
