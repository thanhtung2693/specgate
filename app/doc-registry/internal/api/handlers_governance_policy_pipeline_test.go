package api

// TestGovernancePolicyPipeline exercises the publish→resolve→explain pipeline
// end-to-end using in-memory fakes (no Docker, no DB required).
//
// Pipeline steps:
//  1. Publish an artifact with ImpactDeclaration
//  2. GET /api/v1/artifacts/{id}/policy — verify GovernanceLevel is "enhanced"

import (
	"context"
	"testing"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/skills"
	"github.com/specgate/doc-registry/internal/workboard"
	"github.com/specgate/doc-registry/internal/workspace"
)

// pipelineProfileResolver delegates directly to ResolveBuiltInPolicy — no DB, no key lookup.
type pipelineProfileResolver struct{}

func (pipelineProfileResolver) ResolveProfile(_ context.Context, in governanceprofile.ResolveInput) (*governanceprofile.ResolvedProfile, error) {
	return governanceprofile.ResolveBuiltInPolicy(in)
}

type pipelineSkillReader struct{}

func (pipelineSkillReader) List(_ context.Context) ([]skills.Skill, error) {
	names := []string{
		"spec-review",
		"prd-review",
		"acceptance-criteria",
		"task-breakdown",
		"rollout-risk",
		"review-impl",
	}
	out := make([]skills.Skill, 0, len(names))
	for _, name := range names {
		out = append(out, skills.Skill{Name: name, Prompt: "Frozen " + name + " rubric"})
	}
	return out, nil
}

// pipelineFeatureUpserter is a minimal in-memory feature upserter for the pipeline test.
type pipelineFeatureUpserter struct {
	features map[string]*workboard.Feature
}

func newPipelineFeatureUpserter() *pipelineFeatureUpserter {
	return &pipelineFeatureUpserter{features: map[string]*workboard.Feature{}}
}

func (u *pipelineFeatureUpserter) UpsertFeatureByKeyInWorkspaceForPublish(_ context.Context, workspaceID, key, name string) (*workboard.Feature, bool, error) {
	if f, ok := u.features[key]; ok {
		return f, false, nil
	}
	f := &workboard.Feature{ID: "feat-" + key, WorkspaceID: workspaceID, Key: key, Name: name}
	u.features[key] = f
	return f, true, nil
}

func (u *pipelineFeatureUpserter) DeleteCandidateFeatureIfUnreferenced(_ context.Context, id, key string) error {
	if f, ok := u.features[key]; ok && f.ID == id {
		delete(u.features, key)
	}
	return nil
}

// pipelineArtifactWriter adapts memArtifactRepo to the governanceops.ArtifactWriter interface.
// It delegates to artifact.NewService which already satisfies all required methods.
type pipelineArtifactWriter struct {
	svc artifact.Service
}

func (w *pipelineArtifactWriter) LatestArtifact(ctx context.Context, featureID string) (*artifact.Artifact, error) {
	return w.svc.LatestArtifact(ctx, featureID)
}
func (w *pipelineArtifactWriter) NextVersion(ctx context.Context, featureID string) (string, error) {
	return w.svc.NextVersion(ctx, featureID)
}
func (w *pipelineArtifactWriter) ResolveNextVersion(ctx context.Context, featureID, baseVersion string) (string, error) {
	return w.svc.ResolveNextVersion(ctx, featureID, baseVersion)
}
func (w *pipelineArtifactWriter) Publish(ctx context.Context, in artifact.PublishInput) (*artifact.Artifact, error) {
	return w.svc.Publish(ctx, in)
}
func (w *pipelineArtifactWriter) UpdateStatus(ctx context.Context, id string, in artifact.StatusUpdate) (*artifact.Artifact, error) {
	return w.svc.UpdateStatus(ctx, id, in)
}

func TestGovernancePolicyPipeline(t *testing.T) {
	t.Parallel()
	ctx := workspace.WithID(context.Background(), "ws-test")

	// --- wire in-memory infra ---
	repo := newMemArtifactRepo()
	store := newMemArtifactStore()
	artSvc := artifact.NewService(repo, store, func(featureID, version, path string) string {
		return "artifacts/" + featureID + "/" + version + "/" + path
	})

	publishSvc := &governanceops.Service{
		ArtifactWriter:  &pipelineArtifactWriter{svc: artSvc},
		FeatureUpserter: newPipelineFeatureUpserter(),
		ProfileResolver: pipelineProfileResolver{},
		Skills:          pipelineSkillReader{},
		AppBaseURL:      "https://app.example.com",
	}

	h := &Handlers{Artifacts: artSvc}

	// --- Step 1: publish a high-impact bugfix touching a protected domain ---
	publishResult, err := publishSvc.PublishArtifact(ctx, governanceops.PublishArtifactInput{
		FeatureKey:  "payment-hotfix-pipeline",
		FeatureName: "Payment Hotfix Pipeline",
		RequestType: "bugfix",
		ImpactLevel: "high",
		ImpactDeclaration: governanceprofile.ImpactDeclaration{
			ProtectedDomainsStatus: governanceprofile.TriYes,
			ProtectedDomains:       []string{"payment"},
		},
		Documents: []governanceops.DocumentInput{
			{Path: "spec.md", Role: "spec", Content: "# Payment hotfix spec"},
		},
	})
	if err != nil {
		t.Fatalf("Step 1 PublishArtifact: %v", err)
	}
	artifactID := publishResult.ArtifactID
	if artifactID == "" {
		t.Fatal("Step 1: ArtifactID is empty")
	}

	// --- Step 2: GET /api/v1/artifacts/{id}/policy — verify enhanced level ---
	policyOut, err := h.CLIArtifactPolicy(ctx, &CLIArtifactPolicyInput{ID: artifactID})
	if err != nil {
		t.Fatalf("Step 2 CLIArtifactPolicy: %v", err)
	}
	if policyOut.Body.GovernanceLevel != governanceprofile.GovernanceEnhanced {
		t.Fatalf("Step 2: GovernanceLevel = %q, want %q (enhanced); published with high-impact protected domain",
			policyOut.Body.GovernanceLevel, governanceprofile.GovernanceEnhanced)
	}
	if policyOut.Body.Title == "" {
		t.Fatal("Step 2: Explanation.Title is empty")
	}

}
