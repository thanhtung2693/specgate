package governanceops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/artifactedit"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/workboard"
)

// --- fakes ---

type fakeFeatureUpserter struct {
	features map[string]*workboard.Feature
}

func (f *fakeFeatureUpserter) UpsertFeatureByKey(_ context.Context, key, name string) (*workboard.Feature, error) {
	if f.features == nil {
		f.features = map[string]*workboard.Feature{}
	}
	if feat, ok := f.features[key]; ok {
		return feat, nil
	}
	feat := &workboard.Feature{ID: "feat-" + key, Key: key, Name: name}
	f.features[key] = feat
	return feat, nil
}

type fakeArtifactWriter struct {
	latest     *artifact.Artifact
	nextVer    string
	resolveErr error
	published  []artifact.PublishInput
	statusUpd  []struct {
		id string
		in artifact.StatusUpdate
	}
}

func (f *fakeArtifactWriter) LatestArtifact(_ context.Context, _ string) (*artifact.Artifact, error) {
	return f.latest, nil
}
func (f *fakeArtifactWriter) NextVersion(_ context.Context, _ string) (string, error) {
	if f.nextVer == "" {
		return "1", nil
	}
	return f.nextVer, nil
}
func (f *fakeArtifactWriter) ResolveNextVersion(_ context.Context, _ string, base string) (string, error) {
	if f.resolveErr != nil {
		return "", f.resolveErr
	}
	return base + ".1", nil
}
func (f *fakeArtifactWriter) Publish(_ context.Context, in artifact.PublishInput) (*artifact.Artifact, error) {
	f.published = append(f.published, in)
	return &artifact.Artifact{
		ID:      "art-1",
		Version: in.Version,
		Status:  in.Status,
	}, nil
}
func (f *fakeArtifactWriter) UpdateStatus(_ context.Context, id string, in artifact.StatusUpdate) (*artifact.Artifact, error) {
	f.statusUpd = append(f.statusUpd, struct {
		id string
		in artifact.StatusUpdate
	}{id, in})
	return &artifact.Artifact{ID: id, Status: in.Status}, nil
}

type fakeProfileResolver struct {
	approvalPolicy string
}

func (f *fakeProfileResolver) ResolveProfile(_ context.Context, _ governanceprofile.ResolveInput) (*governanceprofile.ResolvedProfile, error) {
	return &governanceprofile.ResolvedProfile{
		FullKey:        "default/standard",
		Version:        "1",
		Digest:         "abc123",
		RequiredRoles:  []string{},
		ApprovalPolicy: f.approvalPolicy,
	}, nil
}

func newPublishService(writer ArtifactWriter, features FeatureUpserter, profiles ProfileResolver, baseURL string) *Service {
	return &Service{
		ArtifactWriter:  writer,
		FeatureUpserter: features,
		ProfileResolver: profiles,
		AppBaseURL:      baseURL,
	}
}

// --- PublishArtifact tests ---

func TestPublishArtifact_CreatesNewArtifact(t *testing.T) {
	t.Parallel()
	writer := &fakeArtifactWriter{}
	svc := newPublishService(writer, &fakeFeatureUpserter{}, &fakeProfileResolver{}, "https://app.example.com")

	result, err := svc.PublishArtifact(context.Background(), PublishArtifactInput{
		FeatureKey:  "my-feature",
		FeatureName: "My Feature",
		Documents: []DocumentInput{
			{Path: "prd.md", Role: "prd", Content: "# PRD"},
		},
	})
	if err != nil {
		t.Fatalf("PublishArtifact: %v", err)
	}
	if result.ArtifactID == "" {
		t.Fatal("result.ArtifactID is empty")
	}
	if result.Status != "draft" {
		t.Fatalf("status = %q, want draft", result.Status)
	}
	if result.ReviewURL == "" {
		t.Fatal("result.ReviewURL is empty")
	}
	if len(writer.published) != 1 {
		t.Fatalf("published = %d, want 1", len(writer.published))
	}
}

func TestPublishArtifact_StaleBaseVersionReturnsErrVersionConflict(t *testing.T) {
	t.Parallel()
	writer := &fakeArtifactWriter{
		resolveErr: artifact.ErrStaleBase,
		latest:     &artifact.Artifact{ID: "art-0", Version: "2"},
	}
	svc := newPublishService(writer, &fakeFeatureUpserter{}, &fakeProfileResolver{}, "https://app.example.com")

	_, err := svc.PublishArtifact(context.Background(), PublishArtifactInput{
		FeatureKey:  "my-feature",
		BaseVersion: "1",
		Documents:   []DocumentInput{{Path: "spec.md", Role: "spec", Content: "# Spec"}},
	})
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("err = %v, want ErrVersionConflict", err)
	}
}

func TestPublishArtifact_AutoApproveCallsUpdateStatus(t *testing.T) {
	t.Parallel()
	writer := &fakeArtifactWriter{}
	profiles := &fakeProfileResolver{approvalPolicy: "auto"}
	svc := newPublishService(writer, &fakeFeatureUpserter{}, profiles, "https://app.example.com")

	result, err := svc.PublishArtifact(context.Background(), PublishArtifactInput{
		FeatureKey: "feat-auto",
		Documents:  []DocumentInput{{Path: "prd.md", Role: "prd", Content: "# PRD"}},
	})
	if err != nil {
		t.Fatalf("PublishArtifact: %v", err)
	}
	if result.Status != "approved" {
		t.Fatalf("status = %q, want approved", result.Status)
	}
	if len(writer.statusUpd) != 1 {
		t.Fatalf("UpdateStatus calls = %d, want 1", len(writer.statusUpd))
	}
	if writer.statusUpd[0].in.Status != artifact.StatusApproved {
		t.Fatalf("UpdateStatus status = %q, want approved", writer.statusUpd[0].in.Status)
	}
}

func TestPublishArtifact_MissingRolesInResult(t *testing.T) {
	t.Parallel()
	writer := &fakeArtifactWriter{}
	// Return a profile that requires "spec" role
	profiles := &fakeProfileResolver{}
	// Override to include required roles
	customProfiles := &customRequiredRolesProfileResolver{requiredRoles: []string{"spec"}}
	svc := newPublishService(writer, &fakeFeatureUpserter{}, customProfiles, "https://app.example.com")

	result, err := svc.PublishArtifact(context.Background(), PublishArtifactInput{
		FeatureKey: "feat-1",
		// No "spec" role document
		Documents: []DocumentInput{{Path: "prd.md", Role: "prd", Content: "# PRD"}},
	})
	if err != nil {
		t.Fatalf("PublishArtifact: %v", err)
	}
	_ = profiles
	if len(result.MissingRoles) == 0 {
		t.Fatal("expected missing_roles, got empty")
	}
}

func TestPublishArtifact_MissingFeatureKeyError(t *testing.T) {
	t.Parallel()
	svc := newPublishService(&fakeArtifactWriter{}, &fakeFeatureUpserter{}, &fakeProfileResolver{}, "")
	_, err := svc.PublishArtifact(context.Background(), PublishArtifactInput{})
	if err == nil {
		t.Fatal("expected error for empty feature_key")
	}
}

func TestPublishArtifact_NilArtifactWriterError(t *testing.T) {
	t.Parallel()
	svc := &Service{FeatureUpserter: &fakeFeatureUpserter{}, ProfileResolver: &fakeProfileResolver{}}
	_, err := svc.PublishArtifact(context.Background(), PublishArtifactInput{
		FeatureKey: "x",
		Documents:  []DocumentInput{{Path: "prd.md", Content: "# PRD"}},
	})
	if err == nil {
		t.Fatal("expected error for nil artifact writer")
	}
}

type customRequiredRolesProfileResolver struct {
	requiredRoles []string
}

func (c *customRequiredRolesProfileResolver) ResolveProfile(_ context.Context, _ governanceprofile.ResolveInput) (*governanceprofile.ResolvedProfile, error) {
	return &governanceprofile.ResolvedProfile{
		FullKey:       "default/standard",
		Version:       "1",
		Digest:        "abc",
		RequiredRoles: c.requiredRoles,
	}, nil
}

// capturingProfileResolver captures the ResolveInput passed to it and returns a
// configurable ResolvedProfile. Used to assert that ImpactDeclaration and
// RequestedGovernanceLevel are threaded through from PublishArtifactInput.
type capturingProfileResolver struct {
	captured governanceprofile.ResolveInput
	result   *governanceprofile.ResolvedProfile
}

func (c *capturingProfileResolver) ResolveProfile(_ context.Context, in governanceprofile.ResolveInput) (*governanceprofile.ResolvedProfile, error) {
	c.captured = in
	if c.result != nil {
		return c.result, nil
	}
	return &governanceprofile.ResolvedProfile{
		FullKey:        "builtin/policy_v1",
		Version:        "1",
		Digest:         "sha256:abc",
		RequiredRoles:  []string{},
		ApprovalPolicy: "human_required",
	}, nil
}

func newPublishServiceV1(writer ArtifactWriter, features FeatureUpserter, profiles ProfileResolver, baseURL string) *Service {
	return &Service{
		ArtifactWriter:  writer,
		FeatureUpserter: features,
		ProfileResolver: profiles,
		AppBaseURL:      baseURL,
	}
}

func TestPublishArtifact_PersistsPolicyV1Snapshot(t *testing.T) {
	t.Parallel()
	writer := &fakeArtifactWriter{}
	resolver := &capturingProfileResolver{
		result: &governanceprofile.ResolvedProfile{
			FullKey:         "builtin/enhanced",
			Version:         "1",
			Digest:          "sha256:abc",
			GovernanceLevel: governanceprofile.GovernanceEnhanced,
			ApprovalPolicy:  "human_required",
		},
	}
	svc := newPublishServiceV1(writer, &fakeFeatureUpserter{}, resolver, "https://app.example.com")

	result, err := svc.PublishArtifact(context.Background(), PublishArtifactInput{
		FeatureKey:  "payment-fix",
		RequestType: "bugfix",
		ImpactLevel: "high",
		ImpactDeclaration: governanceprofile.ImpactDeclaration{
			ProtectedDomains: []string{"payment"},
		},
		Documents: []DocumentInput{{Path: "spec.md", Role: "spec", Content: "# Spec"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(writer.published) == 0 {
		t.Fatal("no artifact published")
	}
	snapshotJSON := writer.published[0].GatesProfileSnapshotJSON
	if !strings.Contains(snapshotJSON, `"snapshot_schema_version":"specgate.policy/v1"`) {
		t.Fatalf("snapshot does not contain v1 marker; got: %s", snapshotJSON)
	}
	if result.GovernanceLevel != "enhanced" {
		t.Fatalf("GovernanceLevel = %q, want %q", result.GovernanceLevel, "enhanced")
	}
}

func TestPublishArtifact_ThreadsImpactDeclaration(t *testing.T) {
	t.Parallel()
	writer := &fakeArtifactWriter{}
	resolver := &capturingProfileResolver{}
	svc := newPublishServiceV1(writer, &fakeFeatureUpserter{}, resolver, "https://app.example.com")

	_, err := svc.PublishArtifact(context.Background(), PublishArtifactInput{
		FeatureKey:  "feature-x",
		RequestType: "new_feature",
		ImpactLevel: "high",
		ImpactDeclaration: governanceprofile.ImpactDeclaration{
			ProtectedDomainsStatus: governanceprofile.TriYes,
			ProtectedDomains:       []string{"payments"},
		},
		RequestedGovernanceLevel: "enhanced",
		Documents:                []DocumentInput{{Path: "spec.md", Role: "spec", Content: "# Spec"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolver.captured.ImpactDeclaration.ProtectedDomainsStatus != governanceprofile.TriYes {
		t.Fatalf("ImpactDeclaration not threaded: %+v", resolver.captured.ImpactDeclaration)
	}
	if len(resolver.captured.ImpactDeclaration.ProtectedDomains) == 0 {
		t.Fatal("ProtectedDomains not threaded")
	}
	if resolver.captured.RequestedGovernanceLevel != governanceprofile.GovernanceEnhanced {
		t.Fatalf("RequestedGovernanceLevel = %q, want %q", resolver.captured.RequestedGovernanceLevel, governanceprofile.GovernanceEnhanced)
	}
}

func TestPublishArtifact_ExplanationReturnedInResult(t *testing.T) {
	t.Parallel()
	writer := &fakeArtifactWriter{}
	resolver := &capturingProfileResolver{
		result: &governanceprofile.ResolvedProfile{
			FullKey:         "builtin/policy_v1",
			Version:         "1",
			Digest:          "sha256:abc",
			GovernanceLevel: governanceprofile.GovernanceStandard,
			ApprovalPolicy:  "human_required",
			EvidencePolicy:  "attested_ok",
			ReasonCodes:     []string{"default_standard"},
		},
	}
	svc := newPublishServiceV1(writer, &fakeFeatureUpserter{}, resolver, "https://app.example.com")

	result, err := svc.PublishArtifact(context.Background(), PublishArtifactInput{
		FeatureKey: "feature-y",
		Documents:  []DocumentInput{{Path: "spec.md", Role: "spec", Content: "# Spec"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.PolicyExplanation == nil {
		t.Fatal("PolicyExplanation is nil; expected explanation in result")
	}
	if result.PolicyExplanation.Title == "" {
		t.Fatal("PolicyExplanation.Title is empty")
	}
}

// --- DraftArtifactUpdate tests ---

type fakeArtifactReader struct {
	art   *artifact.Artifact
	files map[string]string
}

func (f *fakeArtifactReader) Get(_ context.Context, _ string) (*artifact.Artifact, error) {
	if f.art == nil {
		return nil, errors.New("not found")
	}
	return f.art, nil
}
func (f *fakeArtifactReader) FileContent(_ context.Context, _ string, path string) ([]byte, error) {
	content, ok := f.files[path]
	if !ok {
		return nil, errors.New("file not found")
	}
	return []byte(content), nil
}

type fakeEditStore struct {
	created []artifactedit.Session
}

func (f *fakeEditStore) CreateSession(_ context.Context, session artifactedit.Session, _, _ map[string]string) error {
	f.created = append(f.created, session)
	return nil
}
func (f *fakeEditStore) ListProposals(_ context.Context) ([]artifactedit.Session, error) {
	return f.created, nil
}

func TestDraftArtifactUpdate_CreatesDraftSession(t *testing.T) {
	t.Parallel()
	artReader := &fakeArtifactReader{
		art: &artifact.Artifact{
			ID:      "art-1",
			Version: "1",
			Files:   []artifact.File{{Path: "spec.md"}},
		},
		files: map[string]string{"spec.md": "# Old Spec"},
	}
	editStore := &fakeEditStore{}
	svc := &Service{DraftArtifacts: artReader, EditStore: editStore}

	result, err := svc.DraftArtifactUpdate(context.Background(), DraftArtifactUpdateInput{
		ArtifactID: "art-1",
		Summary:    "Add refund policy",
		Files:      map[string]string{"spec.md": "# New Spec"},
	})
	if err != nil {
		t.Fatalf("DraftArtifactUpdate: %v", err)
	}
	if !result.Drafted {
		t.Fatal("expected drafted=true")
	}
	if result.SessionID == "" {
		t.Fatal("session_id is empty")
	}
	if len(editStore.created) != 1 {
		t.Fatalf("created sessions = %d, want 1", len(editStore.created))
	}
}

func TestDraftArtifactUpdate_DedupesIdenticalRequest(t *testing.T) {
	t.Parallel()
	artReader := &fakeArtifactReader{
		art: &artifact.Artifact{
			ID:      "art-1",
			Version: "1",
			Files:   []artifact.File{{Path: "spec.md"}},
		},
		files: map[string]string{"spec.md": "# Old Spec"},
	}
	editStore := &fakeEditStore{}
	svc := &Service{DraftArtifacts: artReader, EditStore: editStore}

	in := DraftArtifactUpdateInput{
		ArtifactID: "art-1",
		Summary:    "same change",
		Files:      map[string]string{"spec.md": "# New Spec"},
		DedupeKey:  "dedupe-1",
	}
	first, err := svc.DraftArtifactUpdate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.DraftArtifactUpdate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(editStore.created) != 1 {
		t.Fatalf("created sessions = %d, want 1 (deduped)", len(editStore.created))
	}
	if second.Drafted {
		t.Fatal("expected drafted=false on dedup")
	}
	if second.SessionID != first.SessionID {
		t.Fatalf("session IDs differ: first=%s second=%s", first.SessionID, second.SessionID)
	}
}

func TestDraftArtifactUpdate_UnchangedFilesIgnored(t *testing.T) {
	t.Parallel()
	artReader := &fakeArtifactReader{
		art: &artifact.Artifact{
			ID:      "art-1",
			Version: "1",
			Files:   []artifact.File{{Path: "spec.md"}},
		},
		files: map[string]string{"spec.md": "# Exact same content"},
	}
	editStore := &fakeEditStore{}
	svc := &Service{DraftArtifacts: artReader, EditStore: editStore}

	_, err := svc.DraftArtifactUpdate(context.Background(), DraftArtifactUpdateInput{
		ArtifactID: "art-1",
		Summary:    "no change",
		Files:      map[string]string{"spec.md": "# Exact same content"},
	})
	if err == nil {
		t.Fatal("expected error when no files changed")
	}
}

func TestDraftArtifactUpdate_NilStoreErrors(t *testing.T) {
	t.Parallel()
	svc := &Service{}
	_, err := svc.DraftArtifactUpdate(context.Background(), DraftArtifactUpdateInput{
		ArtifactID: "art-1",
		Summary:    "change",
		Files:      map[string]string{"spec.md": "new content"},
	})
	if err == nil {
		t.Fatal("expected error for nil stores")
	}
}
