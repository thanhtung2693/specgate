package api

import (
	"context"
	"testing"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/workboard"
	"github.com/specgate/doc-registry/internal/workspace"
)

func TestCLIListPolicyLevels_ReturnsThreeLevels(t *testing.T) {
	t.Parallel()
	h := &Handlers{}

	out, err := h.CLIListPolicyLevels(context.Background(), &struct{}{})
	if err != nil {
		t.Fatalf("CLIListPolicyLevels: %v", err)
	}
	if len(out.Body.Levels) != 3 {
		t.Fatalf("levels count = %d, want 3", len(out.Body.Levels))
	}
	byLevel := map[string]CLIPolicyLevelDTO{}
	for _, l := range out.Body.Levels {
		byLevel[string(l.GovernanceLevel)] = l
	}
	for _, lv := range []string{"light", "standard", "enhanced"} {
		if _, ok := byLevel[lv]; !ok {
			t.Errorf("level %q missing from response", lv)
		}
	}
	if len(byLevel["enhanced"].EnabledGates) == 0 {
		t.Fatal("enhanced EnabledGates is empty")
	}
}

func TestCLIWorkItemPolicy_QuickRouteWithoutLeadArtifact(t *testing.T) {
	t.Parallel()
	governance := &cliTestWorkBoard{
		crs: []workboard.ChangeRequest{{
			ID:                 "cr-quick",
			Key:                "CR-QUICK",
			FeatureID:          "feat-quick",
			Title:              "Quick bug fix",
			AcceptanceCriteria: `["Done"]`,
		}},
		features: map[string]*workboard.Feature{
			"feat-quick": {ID: "feat-quick", Key: "FEAT-QUICK"},
		},
	}
	h := &Handlers{Governance: &governanceops.Service{WorkBoard: governance}}

	out, err := h.CLIWorkItemPolicy(context.Background(), &CLIWorkItemPolicyInput{ID: "CR-QUICK"})
	if err != nil {
		t.Fatalf("CLIWorkItemPolicy: %v", err)
	}
	if out.Body.GovernanceLevel != governanceprofile.GovernanceStandard {
		t.Fatalf("governance level = %q, want standard", out.Body.GovernanceLevel)
	}
}

func TestCLIResolvePolicy_LowRiskBugfix(t *testing.T) {
	t.Parallel()
	h := &Handlers{}

	out, err := h.CLIResolvePolicy(context.Background(), &CLIResolvePolicyInput{
		Body: CLIResolvePolicyBody{
			RequestType: "bugfix",
			ImpactLevel: "low",
			ImpactDeclaration: governanceprofile.ImpactDeclaration{
				ProtectedDomainsStatus: governanceprofile.TriNo,
			},
		},
	})
	if err != nil {
		t.Fatalf("CLIResolvePolicy: %v", err)
	}
	if out.Body.GovernanceLevel != governanceprofile.GovernanceLight {
		t.Fatalf("governance_level = %q, want light", out.Body.GovernanceLevel)
	}
}

func TestCLIArtifactPolicy_NoSnapshot(t *testing.T) {
	t.Parallel()
	h := &Handlers{}

	if _, err := h.CLIArtifactPolicy(context.Background(), &CLIArtifactPolicyInput{ID: "art-1"}); err == nil {
		t.Fatal("expected error when Artifacts service is not configured")
	}
}

type workspaceArtifactPolicyService struct {
	fakeArtifactService
	art *artifact.Artifact
}

func (s *workspaceArtifactPolicyService) Get(_ context.Context, _ string) (*artifact.Artifact, error) {
	return s.art, nil
}

func (s *workspaceArtifactPolicyService) GetInWorkspace(_ context.Context, workspaceID, _ string) (*artifact.Artifact, error) {
	if s.art == nil || s.art.WorkspaceID != workspaceID {
		return nil, artifact.ErrNotFound
	}
	return s.art, nil
}

func TestCLIArtifactPolicyRejectsForeignWorkspaceArtifact(t *testing.T) {
	t.Parallel()
	h := &Handlers{Artifacts: &workspaceArtifactPolicyService{
		art: &artifact.Artifact{ID: "art-b", WorkspaceID: "ws-b"},
	}}

	if _, err := h.CLIArtifactPolicy(workspace.WithID(context.Background(), "ws-a"), &CLIArtifactPolicyInput{ID: "art-b"}); err == nil {
		t.Fatal("foreign artifact policy read succeeded")
	}
}
