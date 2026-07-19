package governanceops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/skills"
	"github.com/specgate/doc-registry/internal/workboard"
	"github.com/specgate/doc-registry/internal/workspace"
)

// --- fakes ---

type fakeFeatureUpserter struct {
	features   map[string]*workboard.Feature
	cleanupErr error
}

func workspaceTestContext() context.Context {
	return workspace.WithID(context.Background(), "ws-test")
}

func TestPublishArtifactRequiresWorkspace(t *testing.T) {
	t.Parallel()
	svc := &Service{
		ArtifactWriter:  &fakeArtifactWriter{},
		FeatureUpserter: &fakeFeatureUpserter{},
		ProfileResolver: &fakeProfileResolver{},
	}
	_, err := svc.PublishArtifact(context.Background(), PublishArtifactInput{FeatureKey: "checkout"})
	if err == nil || !strings.Contains(err.Error(), "workspace_id is required") {
		t.Fatalf("PublishArtifact error = %v, want workspace_id required", err)
	}
}

func (f *fakeFeatureUpserter) UpsertFeatureByKeyInWorkspaceForPublish(_ context.Context, workspaceID, key, name string) (*workboard.Feature, bool, error) {
	if f.features == nil {
		f.features = map[string]*workboard.Feature{}
	}
	if feat, ok := f.features[key]; ok {
		return feat, false, nil
	}
	feat := &workboard.Feature{ID: "feat-" + key, WorkspaceID: workspaceID, Key: key, Name: name}
	f.features[key] = feat
	return feat, true, nil
}

func (f *fakeFeatureUpserter) DeleteCandidateFeatureIfUnreferenced(_ context.Context, id, key string) error {
	if f.cleanupErr != nil {
		return f.cleanupErr
	}
	feat, ok := f.features[key]
	if !ok || feat.ID != id {
		return nil
	}
	if feat.Status != "" && feat.Status != workboard.FeatureStatusCandidate {
		return nil
	}
	delete(f.features, key)
	return nil
}

type fakeArtifactWriter struct {
	latest     *artifact.Artifact
	nextVer    string
	resolveErr error
	publishErr error
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
	if f.publishErr != nil {
		return nil, f.publishErr
	}
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

type frozenRubricProfileResolver struct{}

func (frozenRubricProfileResolver) ResolveProfile(_ context.Context, _ governanceprofile.ResolveInput) (*governanceprofile.ResolvedProfile, error) {
	return &governanceprofile.ResolvedProfile{
		FullKey:      "builtin/policy_v1",
		Version:      "1",
		Digest:       "sha256:definition",
		EnabledGates: []string{"scope_clear"},
		GateSkills:   map[string]string{"scope_clear": "prd-review"},
	}, nil
}

type fakePublishSkillReader struct {
	prompt string
}

func (f *fakePublishSkillReader) List(_ context.Context) ([]skills.Skill, error) {
	return []skills.Skill{{Name: "prd-review", Prompt: f.prompt}}, nil
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

	result, err := svc.PublishArtifact(workspaceTestContext(), PublishArtifactInput{
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

func TestPublishArtifactRejectsOversizedDocumentBeforeCreatingFeature(t *testing.T) {
	t.Parallel()
	features := &fakeFeatureUpserter{}
	svc := newPublishService(&fakeArtifactWriter{}, features, &fakeProfileResolver{}, "")

	_, err := svc.PublishArtifact(workspaceTestContext(), PublishArtifactInput{
		FeatureKey: "oversized-document",
		Documents: []DocumentInput{{
			Path:    "spec.md",
			Role:    "spec",
			Content: strings.Repeat("x", (1<<20)+1),
		}},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("PublishArtifact error = %v, want ErrValidation", err)
	}
	if len(features.features) != 0 {
		t.Fatalf("features = %v, want no feature created", features.features)
	}
}

func TestPublishArtifactRejectsOversizedPackageBeforeCreatingFeature(t *testing.T) {
	t.Parallel()
	features := &fakeFeatureUpserter{}
	svc := newPublishService(&fakeArtifactWriter{}, features, &fakeProfileResolver{}, "")
	documents := make([]DocumentInput, 11)
	for i := range documents {
		documents[i] = DocumentInput{
			Path:    fmt.Sprintf("part-%02d.md", i),
			Role:    "spec",
			Content: strings.Repeat("x", 1<<20),
		}
	}

	_, err := svc.PublishArtifact(workspaceTestContext(), PublishArtifactInput{
		FeatureKey: "oversized-package",
		Documents:  documents,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("PublishArtifact error = %v, want ErrValidation", err)
	}
	if len(features.features) != 0 {
		t.Fatalf("features = %v, want no feature created", features.features)
	}
}

func TestPublishArtifactFreezesSkillRubricIntoPolicySnapshot(t *testing.T) {
	t.Parallel()
	writer := &fakeArtifactWriter{}
	skillReader := &fakePublishSkillReader{prompt: "Original team rubric"}
	svc := &Service{
		ArtifactWriter:  writer,
		FeatureUpserter: &fakeFeatureUpserter{},
		ProfileResolver: frozenRubricProfileResolver{},
		Skills:          skillReader,
	}

	if _, err := svc.PublishArtifact(workspaceTestContext(), PublishArtifactInput{
		FeatureKey: "freeze-rubric",
		Documents:  []DocumentInput{{Path: "spec.md", Role: "spec", Content: "# Spec"}},
	}); err != nil {
		t.Fatal(err)
	}
	skillReader.prompt = "Changed after publication"

	var snapshot struct {
		Digest          string                             `json:"digest"`
		GateDefinitions []governanceprofile.GateDefinition `json:"gate_definitions"`
	}
	if err := json.Unmarshal([]byte(writer.published[0].PolicySnapshotJSON), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.GateDefinitions) != 1 {
		t.Fatalf("gate definitions = %#v", snapshot.GateDefinitions)
	}
	gate := snapshot.GateDefinitions[0]
	if gate.Key != "scope_clear" || gate.Version == "" || gate.SkillName != "prd-review" || gate.SkillContent != "Original team rubric" || gate.SkillDigest == "" {
		t.Fatalf("frozen gate = %#v", gate)
	}
	if snapshot.Digest == "" || writer.published[0].PolicyDigest != snapshot.Digest {
		t.Fatalf("snapshot digest = %q, published digest = %q", snapshot.Digest, writer.published[0].PolicyDigest)
	}
}

func TestPublishArtifact_ForwardsWorkspaceOwnership(t *testing.T) {
	t.Parallel()
	writer := &fakeArtifactWriter{}
	svc := newPublishService(writer, &fakeFeatureUpserter{}, &fakeProfileResolver{}, "https://app.example.com")

	_, err := svc.PublishArtifact(workspace.WithID(context.Background(), "ws-a"), PublishArtifactInput{
		FeatureKey:  "workspace-feature",
		WorkspaceID: "ws-a",
		Documents:   []DocumentInput{{Path: "spec.md", Role: "spec", Content: "# Spec"}},
	})
	if err != nil {
		t.Fatalf("PublishArtifact: %v", err)
	}
	if len(writer.published) != 1 || writer.published[0].WorkspaceID != "ws-a" {
		t.Fatalf("published workspace = %#v, want ws-a", writer.published)
	}
}

func TestPublishArtifactRejectsWorkspaceMismatchedWithTrustedContext(t *testing.T) {
	t.Parallel()
	writer := &fakeArtifactWriter{}
	svc := newPublishService(writer, &fakeFeatureUpserter{}, &fakeProfileResolver{}, "https://app.example.com")

	_, err := svc.PublishArtifact(workspace.WithID(context.Background(), "ws-a"), PublishArtifactInput{
		FeatureKey:  "workspace-feature",
		WorkspaceID: "ws-b",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want workspace validation error", err)
	}
	if len(writer.published) != 0 {
		t.Fatalf("published = %#v, want no artifact", writer.published)
	}
}

func TestPublishArtifact_RemovesNewFeatureWhenPublishFails(t *testing.T) {
	t.Parallel()
	writer := &fakeArtifactWriter{publishErr: errors.New("insert artifact: missing column")}
	features := &fakeFeatureUpserter{}
	svc := newPublishService(writer, features, &fakeProfileResolver{}, "https://app.example.com")

	_, err := svc.PublishArtifact(workspaceTestContext(), PublishArtifactInput{
		FeatureKey:  "schema-fix",
		FeatureName: "Schema Fix",
		Documents: []DocumentInput{
			{Path: "spec.md", Role: "spec", Content: "# Spec"},
		},
	})
	if err == nil {
		t.Fatal("PublishArtifact succeeded, want publish error")
	}
	if _, ok := features.features["schema-fix"]; ok {
		t.Fatalf("feature key schema-fix was left behind after publish failure: %#v", features.features["schema-fix"])
	}
}

func TestPublishArtifact_ReturnsPublishAndCleanupErrors(t *testing.T) {
	t.Parallel()
	publishErr := errors.New("insert artifact: missing column")
	cleanupErr := errors.New("cleanup failed")
	writer := &fakeArtifactWriter{publishErr: publishErr}
	features := &fakeFeatureUpserter{cleanupErr: cleanupErr}
	svc := newPublishService(writer, features, &fakeProfileResolver{}, "https://app.example.com")

	_, err := svc.PublishArtifact(workspaceTestContext(), PublishArtifactInput{
		FeatureKey: "schema-fix",
		Documents: []DocumentInput{
			{Path: "spec.md", Role: "spec", Content: "# Spec"},
		},
	})
	if !errors.Is(err, publishErr) {
		t.Fatalf("err = %v, want publish error in chain", err)
	}
	if !errors.Is(err, cleanupErr) {
		t.Fatalf("err = %v, want cleanup error in chain", err)
	}
}

func TestPublishArtifact_UsesCreatedByWhenProvided(t *testing.T) {
	t.Parallel()
	writer := &fakeArtifactWriter{}
	svc := newPublishService(writer, &fakeFeatureUpserter{}, &fakeProfileResolver{}, "https://app.example.com")

	_, err := svc.PublishArtifact(workspaceTestContext(), PublishArtifactInput{
		FeatureKey: "my-feature",
		CreatedBy:  "thanhtung2693",
		Documents: []DocumentInput{
			{Path: "spec.md", Role: "spec", Content: "# Spec"},
		},
	})
	if err != nil {
		t.Fatalf("PublishArtifact: %v", err)
	}
	if got := writer.published[0].CreatedBy; got != "thanhtung2693" {
		t.Fatalf("CreatedBy = %q, want thanhtung2693", got)
	}
}

func TestPublishArtifact_StaleBaseVersionReturnsErrVersionConflict(t *testing.T) {
	t.Parallel()
	writer := &fakeArtifactWriter{
		resolveErr: artifact.ErrStaleBase,
		latest:     &artifact.Artifact{ID: "art-0", Version: "2"},
	}
	svc := newPublishService(writer, &fakeFeatureUpserter{}, &fakeProfileResolver{}, "https://app.example.com")

	_, err := svc.PublishArtifact(workspaceTestContext(), PublishArtifactInput{
		FeatureKey:  "my-feature",
		BaseVersion: "1",
		Documents:   []DocumentInput{{Path: "spec.md", Role: "spec", Content: "# Spec"}},
	})
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("err = %v, want ErrVersionConflict", err)
	}
}

func TestPublishArtifact_AlwaysLeavesNewArtifactDraft(t *testing.T) {
	t.Parallel()
	writer := &fakeArtifactWriter{}
	profiles := &fakeProfileResolver{approvalPolicy: "auto"}
	svc := newPublishService(writer, &fakeFeatureUpserter{}, profiles, "https://app.example.com")

	result, err := svc.PublishArtifact(workspaceTestContext(), PublishArtifactInput{
		FeatureKey: "feat-auto",
		Documents:  []DocumentInput{{Path: "prd.md", Role: "prd", Content: "# PRD"}},
	})
	if err != nil {
		t.Fatalf("PublishArtifact: %v", err)
	}
	if result.Status != "draft" {
		t.Fatalf("status = %q, want draft", result.Status)
	}
	if len(writer.statusUpd) != 0 {
		t.Fatalf("UpdateStatus calls = %d, want 0", len(writer.statusUpd))
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

	result, err := svc.PublishArtifact(workspaceTestContext(), PublishArtifactInput{
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

func TestPublishArtifact_MissingRolesStillLeavesDraft(t *testing.T) {
	t.Parallel()
	writer := &fakeArtifactWriter{}
	profiles := &customRequiredRolesProfileResolver{
		requiredRoles:  []string{"spec", "plan"},
		approvalPolicy: "human_required",
	}
	svc := newPublishService(writer, &fakeFeatureUpserter{}, profiles, "https://app.example.com")

	result, err := svc.PublishArtifact(workspaceTestContext(), PublishArtifactInput{
		FeatureKey: "feat-auto-incomplete",
		Documents:  []DocumentInput{{Path: "spec.md", Role: "spec", Content: "# Spec"}},
	})
	if err != nil {
		t.Fatalf("PublishArtifact: %v", err)
	}
	if result.Status != "draft" {
		t.Fatalf("status = %q, want draft", result.Status)
	}
	if len(writer.statusUpd) != 0 {
		t.Fatalf("UpdateStatus calls = %d, want 0", len(writer.statusUpd))
	}
	if len(result.MissingRoles) == 0 {
		t.Fatal("expected missing_roles to surface the plan gap")
	}
}

func TestPublishArtifact_MissingFeatureKeyError(t *testing.T) {
	t.Parallel()
	svc := newPublishService(&fakeArtifactWriter{}, &fakeFeatureUpserter{}, &fakeProfileResolver{}, "")
	_, err := svc.PublishArtifact(workspaceTestContext(), PublishArtifactInput{})
	if err == nil {
		t.Fatal("expected error for empty feature_key")
	}
}

func TestPublishArtifact_NilArtifactWriterError(t *testing.T) {
	t.Parallel()
	svc := &Service{FeatureUpserter: &fakeFeatureUpserter{}, ProfileResolver: &fakeProfileResolver{}}
	_, err := svc.PublishArtifact(workspaceTestContext(), PublishArtifactInput{
		FeatureKey: "x",
		Documents:  []DocumentInput{{Path: "prd.md", Content: "# PRD"}},
	})
	if err == nil {
		t.Fatal("expected error for nil artifact writer")
	}
}

type customRequiredRolesProfileResolver struct {
	requiredRoles  []string
	approvalPolicy string
}

func (c *customRequiredRolesProfileResolver) ResolveProfile(_ context.Context, _ governanceprofile.ResolveInput) (*governanceprofile.ResolvedProfile, error) {
	return &governanceprofile.ResolvedProfile{
		FullKey:        "default/standard",
		Version:        "1",
		Digest:         "abc",
		RequiredRoles:  c.requiredRoles,
		ApprovalPolicy: c.approvalPolicy,
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

	result, err := svc.PublishArtifact(workspaceTestContext(), PublishArtifactInput{
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
	snapshotJSON := writer.published[0].PolicySnapshotJSON
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

	_, err := svc.PublishArtifact(workspaceTestContext(), PublishArtifactInput{
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

	result, err := svc.PublishArtifact(workspaceTestContext(), PublishArtifactInput{
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
