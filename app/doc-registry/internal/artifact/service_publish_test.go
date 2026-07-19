package artifact

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// memRepo is an in-memory Repository used only by service_publish_test.go.
// Methods not needed by Publish/Get tests panic to surface unexpected calls.
type memRepo struct {
	artifacts         map[string]*Artifact
	events            []Event
	readiness         []ReadinessRun
	conflictArtifacts []Artifact // returned by FindOverlappingServices; nil = no conflicts
	insertErr         error
	deleteErr         error
}

func newMemRepo() *memRepo {
	return &memRepo{artifacts: map[string]*Artifact{}}
}

func (r *memRepo) InsertWithEvent(_ context.Context, a *Artifact, e Event) error {
	if r.insertErr != nil {
		return r.insertErr
	}
	for _, existing := range r.artifacts {
		if existing.FeatureID == a.FeatureID && existing.Version == a.Version {
			return fmt.Errorf("duplicate artifact version")
		}
	}
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
func (r *memRepo) Delete(_ context.Context, id string) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	if _, ok := r.artifacts[id]; !ok {
		return ErrNotFound
	}
	delete(r.artifacts, id)
	return nil
}
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
	objects       map[string][]byte
	putCalls      int
	putErrAt      int
	deleteCtxErrs []error
}

func newMemStore() *memStore {
	return &memStore{objects: map[string][]byte{}}
}

func (s *memStore) PutObject(_ context.Context, key string, body []byte) error {
	s.putCalls++
	if s.putErrAt > 0 && s.putCalls == s.putErrAt {
		return errors.New("put failed")
	}
	s.objects[key] = append([]byte(nil), body...)
	return nil
}
func (s *memStore) GetObject(_ context.Context, key string, _ int64) ([]byte, error) {
	b, ok := s.objects[key]
	if !ok {
		return nil, ErrFileNotFound
	}
	return append([]byte(nil), b...), nil
}
func (s *memStore) DeleteObject(ctx context.Context, key string) error {
	s.deleteCtxErrs = append(s.deleteCtxErrs, ctx.Err())
	delete(s.objects, key)
	return nil
}

// testObjectKey mirrors the production formula before the service adds workspace scope.
func testObjectKey(artifactID, version, path string) string {
	return "artifacts/" + artifactID + "/" + version + "/" + path
}

// newTestService returns a RegistryService backed by in-memory dependencies.
func newTestService(t *testing.T) (*RegistryService, *memRepo, *memStore) {
	t.Helper()
	repo := newMemRepo()
	store := newMemStore()
	svc := NewService(repo, store, testObjectKey)
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
		WorkspaceID: "test-workspace",
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
	in.PolicyVersion = "1"
	in.PolicyDigest = "sha256:generic"
	in.PolicySnapshotJSON = `{"policy_version":"1"}`
	in.Documents = []DocumentInput{
		{Path: "docs//proposal.md", Role: "spec", Content: []byte("# P")},
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
	if !strings.Contains(f.ObjectKey, "/docs/proposal.md") || strings.Contains(f.ObjectKey, "/checkout-loyalty/v0.1/") {
		t.Errorf("ObjectKey = %q, want artifact-id scope and preserved path", f.ObjectKey)
	}
	wantFileHash := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("# P")))
	if f.ContentSHA256 != wantFileHash {
		t.Errorf("ContentSHA256 = %q, want %q", f.ContentSHA256, wantFileHash)
	}
	if !strings.HasPrefix(stored.SnapshotDigest, "sha256:") {
		t.Errorf("SnapshotDigest = %q, want sha256 digest", stored.SnapshotDigest)
	}

	// Envelope fields persisted.
	if stored.SourceKind != "openspec" {
		t.Errorf("SourceKind = %q, want %q", stored.SourceKind, "openspec")
	}
	if stored.SourceRevision != "abc123" {
		t.Errorf("SourceRevision = %q, want %q", stored.SourceRevision, "abc123")
	}
	if stored.PolicyVersion != "1" {
		t.Errorf("PolicyVersion = %q, want %q", stored.PolicyVersion, "1")
	}
	if stored.PolicyDigest != "sha256:generic" {
		t.Errorf("PolicyDigest = %q, want %q", stored.PolicyDigest, "sha256:generic")
	}
	if stored.PolicySnapshotJSON != `{"policy_version":"1"}` {
		t.Errorf("PolicySnapshotJSON = %q", stored.PolicySnapshotJSON)
	}
}

func TestPublish_WorkspacePrefixesObjectKeys(t *testing.T) {
	t.Parallel()
	svc, _, store := newTestService(t)
	in := minPublishInput()
	in.WorkspaceID = "workspace-a"
	in.Documents = []DocumentInput{{Path: "spec.md", Content: []byte("# spec")}}

	// The workspace id on the publish contract, rather than an optional caller
	// context, must determine the object prefix. Internal publication paths use
	// this service directly as well as through HTTP middleware.
	if _, err := svc.Publish(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	for key := range store.objects {
		if !strings.HasPrefix(key, "workspaces/workspace-a/") {
			t.Fatalf("object key = %q, want workspace prefix", key)
		}
	}
}

func TestPublish_RequiresWorkspace(t *testing.T) {
	t.Parallel()
	svc, repo, store := newTestService(t)
	in := minPublishInput()
	in.WorkspaceID = ""
	in.Documents = []DocumentInput{{Path: "spec.md", Content: []byte("# spec")}}

	if _, err := svc.Publish(context.Background(), in); !errors.Is(err, ErrWorkspaceRequired) {
		t.Fatalf("Publish error = %v, want ErrWorkspaceRequired", err)
	}
	if len(repo.artifacts) != 0 || len(store.objects) != 0 {
		t.Fatalf("unscoped publish wrote artifacts=%d objects=%d", len(repo.artifacts), len(store.objects))
	}
}

func TestPublish_RejectsUnsafeWorkspaceIDBeforeWriting(t *testing.T) {
	t.Parallel()
	for _, workspaceID := range []string{"../workspace-b", "workspace/a", `workspace\a`, "workspace..b", "."} {
		workspaceID := workspaceID
		t.Run(workspaceID, func(t *testing.T) {
			t.Parallel()
			svc, repo, store := newTestService(t)
			in := minPublishInput()
			in.WorkspaceID = workspaceID
			in.Documents = []DocumentInput{{Path: "spec.md", Content: []byte("# spec")}}

			if _, err := svc.Publish(context.Background(), in); err == nil {
				t.Fatalf("Publish accepted unsafe workspace ID %q", workspaceID)
			}
			if len(repo.artifacts) != 0 || len(store.objects) != 0 {
				t.Fatalf("unsafe publish wrote artifacts=%d objects=%d", len(repo.artifacts), len(store.objects))
			}
		})
	}
}

func TestPublish_RejectsWorkspaceMismatchedWithTrustedContext(t *testing.T) {
	t.Parallel()
	svc, repo, store := newTestService(t)
	in := minPublishInput()
	in.WorkspaceID = "workspace-b"
	in.Documents = []DocumentInput{{Path: "spec.md", Content: []byte("# spec")}}

	if _, err := svc.Publish(WithWorkspace(context.Background(), "workspace-a"), in); err == nil {
		t.Fatal("cross-workspace publish succeeded")
	}
	if len(repo.artifacts) != 0 || len(store.objects) != 0 {
		t.Fatalf("cross-workspace publish wrote artifacts=%d objects=%d", len(repo.artifacts), len(store.objects))
	}
}

func TestPublish_ConflictingVersionDoesNotShareBlobKey(t *testing.T) {
	t.Parallel()

	svc, repo, store := newTestService(t)
	ctx := context.Background()
	first, err := svc.Publish(ctx, PublishInput{
		WorkspaceID: "workspace-a",
		FeatureID:   "same-feature", Version: "v0.1", RequestType: RequestTypeNewFeature,
		ImpactLevel: ImpactLevelLow, Documents: []DocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("first")}},
	})
	if err != nil {
		t.Fatalf("first publish: %v", err)
	}
	second, err := svc.Publish(ctx, PublishInput{
		WorkspaceID: "workspace-a",
		FeatureID:   "same-feature", Version: "v0.1", RequestType: RequestTypeNewFeature,
		ImpactLevel: ImpactLevelLow, Documents: []DocumentInput{{Path: "spec.md", Role: "spec", Content: []byte("second")}},
	})
	if err == nil {
		t.Fatal("second publish unexpectedly succeeded in in-memory repository")
	}
	stored, err := repo.Get(ctx, first.ID)
	if err != nil {
		t.Fatalf("read first artifact: %v", err)
	}
	body, err := store.GetObject(ctx, stored.Files[0].ObjectKey, stored.Files[0].SizeBytes)
	if err != nil {
		t.Fatalf("read first blob: %v", err)
	}
	if string(body) != "first" {
		t.Fatalf("first blob = %q, want first", body)
	}
	if second != nil {
		t.Fatalf("conflicting publish returned artifact %q", second.ID)
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
		{"dotdot-middle", "docs/../escape.md"},
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

func TestPublish_ValidatesAllPathsBeforeUploading(t *testing.T) {
	t.Parallel()
	svc, repo, store := newTestService(t)
	in := minPublishInput()
	in.Documents = []DocumentInput{
		{Path: "spec.md", Content: []byte("valid")},
		{Path: "../escape.md", Content: []byte("invalid")},
	}

	if _, err := svc.Publish(context.Background(), in); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("Publish error = %v, want ErrInvalidPath", err)
	}
	if len(repo.artifacts) != 0 || len(store.objects) != 0 || store.putCalls != 0 {
		t.Fatalf("invalid package wrote artifacts=%d objects=%d puts=%d", len(repo.artifacts), len(store.objects), store.putCalls)
	}
}

func TestPublish_CleansUploadedObjectsAfterPutFailure(t *testing.T) {
	t.Parallel()
	svc, repo, store := newTestService(t)
	store.putErrAt = 2
	in := minPublishInput()
	in.Documents = []DocumentInput{
		{Path: "spec.md", Content: []byte("first")},
		{Path: "design.md", Content: []byte("second")},
	}

	if _, err := svc.Publish(context.Background(), in); err == nil {
		t.Fatal("Publish succeeded, want object-store failure")
	}
	if len(repo.artifacts) != 0 || len(store.objects) != 0 {
		t.Fatalf("failed publish left artifacts=%d objects=%d", len(repo.artifacts), len(store.objects))
	}
}

func TestPublish_CleansObjectsWithLiveContextAfterCanceledInsert(t *testing.T) {
	t.Parallel()
	svc, repo, store := newTestService(t)
	repo.insertErr = context.Canceled
	in := minPublishInput()
	in.Documents = []DocumentInput{{Path: "spec.md", Content: []byte("body")}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := svc.Publish(ctx, in); !errors.Is(err, context.Canceled) {
		t.Fatalf("Publish error = %v, want context.Canceled", err)
	}
	if len(store.objects) != 0 {
		t.Fatalf("failed publish left %d objects", len(store.objects))
	}
	if len(store.deleteCtxErrs) != 1 || store.deleteCtxErrs[0] != nil {
		t.Fatalf("cleanup context errors = %v, want detached live context", store.deleteCtxErrs)
	}
}

func TestDelete_PreservesObjectsWhenMetadataDeleteFails(t *testing.T) {
	t.Parallel()
	svc, repo, store := newTestService(t)
	in := minPublishInput()
	in.Documents = []DocumentInput{{Path: "spec.md", Content: []byte("body")}}
	published, err := svc.Publish(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	repo.deleteErr = errors.New("database unavailable")

	if err := svc.Delete(context.Background(), published.ID); err == nil {
		t.Fatal("Delete succeeded, want repository failure")
	}
	if len(store.objects) != 1 {
		t.Fatalf("metadata delete failure removed objects: %d remain", len(store.objects))
	}
	if _, err := repo.Get(context.Background(), published.ID); err != nil {
		t.Fatalf("metadata missing after failed delete: %v", err)
	}
}

func TestDelete_RemovesObjectsAfterMetadata(t *testing.T) {
	t.Parallel()
	svc, repo, store := newTestService(t)
	in := minPublishInput()
	in.Documents = []DocumentInput{{Path: "spec.md", Content: []byte("body")}}
	published, err := svc.Publish(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Delete(context.Background(), published.ID); err != nil {
		t.Fatal(err)
	}
	if len(store.objects) != 0 {
		t.Fatalf("successful delete left %d objects", len(store.objects))
	}
	if _, err := repo.Get(context.Background(), published.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get error = %v, want ErrNotFound", err)
	}
}

func TestFileContent_AllowsEmptyDocument(t *testing.T) {
	t.Parallel()
	svc, _, _ := newTestService(t)
	in := minPublishInput()
	in.Documents = []DocumentInput{{Path: "empty.md", Content: []byte{}}}
	published, err := svc.Publish(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}

	body, err := svc.FileContent(context.Background(), published.ID, "empty.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 0 {
		t.Fatalf("empty document body = %q", body)
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
