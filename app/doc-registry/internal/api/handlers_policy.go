package api

import (
	"context"
	"encoding/json"

	"github.com/danielgtaylor/huma/v2"

	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/workspace"
)

// CLIListPolicyLevels handles GET /api/v1/policies/levels — returns the
// execution projection for each of the three built-in governance tiers.
func (h *Handlers) CLIListPolicyLevels(_ context.Context, _ *struct{}) (*CLIListPolicyLevelsOutput, error) {
	entries := governanceprofile.ListBuiltInPolicyLevels()
	levels := make([]CLIPolicyLevelDTO, 0, len(entries))
	for _, e := range entries {
		levels = append(levels, CLIPolicyLevelDTO{
			GovernanceLevel:  e.Level,
			DisplayName:      e.Definition.DisplayName,
			ApprovalPolicy:   e.Definition.ApprovalPolicy,
			EvidencePolicy:   e.Definition.EvidencePolicy,
			RequiredRoles:    e.Definition.RequiredRoles,
			RequiredTopics:   e.Definition.RequiredTopics,
			RequiredEvidence: e.Definition.RequiredEvidence,
			EnabledGates:     e.Definition.EnabledGates,
		})
	}
	out := &CLIListPolicyLevelsOutput{}
	out.Body.Levels = levels
	return out, nil
}

// CLIResolvePolicy handles POST /api/v1/policies/resolve — resolves the
// governance level and explanation for a proposed change without persisting
// anything.
func (h *Handlers) CLIResolvePolicy(_ context.Context, in *CLIResolvePolicyInput) (*CLIPolicyOutput, error) {
	p, err := governanceprofile.ResolveBuiltInPolicy(governanceprofile.ResolveInput{
		RequestType:              in.Body.RequestType,
		ImpactLevel:              in.Body.ImpactLevel,
		RequestedGovernanceLevel: governanceprofile.GovernanceLevel(in.Body.RequestedGovernanceLevel),
		ImpactDeclaration:        in.Body.ImpactDeclaration,
	})
	if err != nil {
		return nil, huma.Error500InternalServerError("resolve policy", err)
	}
	out := &CLIPolicyOutput{}
	out.Body = governanceprofile.ExplainSnapshot(*p)
	return out, nil
}

// CLIArtifactPolicy handles GET /api/v1/artifacts/{id}/policy — returns the
// governance explanation for a specific artifact from its persisted snapshot.
func (h *Handlers) CLIArtifactPolicy(ctx context.Context, in *CLIArtifactPolicyInput) (*CLIPolicyOutput, error) {
	if h.Artifacts == nil {
		return nil, huma.Error503ServiceUnavailable("artifacts service not configured")
	}
	art, err := getArtifactForWorkspace(ctx, h.Artifacts, workspace.ID(ctx), in.ID)
	if err != nil {
		return nil, mapArtifactError("get artifact for policy", err)
	}
	p, err := resolvedProfileFromSnapshot(art.PolicySnapshotJSON)
	if err != nil {
		return nil, err
	}
	out := &CLIPolicyOutput{}
	out.Body = governanceprofile.ExplainSnapshot(*p)
	return out, nil
}

// CLIWorkItemPolicy handles GET /api/v1/work-items/{id}/policy — returns the
// governance explanation for the lead artifact of a change request.
func (h *Handlers) CLIWorkItemPolicy(ctx context.Context, in *CLIWorkItemPolicyInput) (*CLIPolicyOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
		return nil, err
	}
	work, err := svc.ResolveWorkRef(ctx, governanceops.ResolveWorkRefInput{Ref: in.ID})
	if err != nil {
		return nil, mapGovernanceError("work-item-policy", err)
	}
	cr, err := svc.WorkBoard.GetChangeRequest(ctx, work.ChangeRequestID)
	if err != nil {
		return nil, mapGovernanceError("work-item-policy", err)
	}
	if cr.LeadArtifactID == "" {
		out := &CLIPolicyOutput{}
		out.Body = governanceprofile.Explanation{
			GovernanceLevel: governanceprofile.GovernanceStandard,
			Title:           "Quick-route governance",
			Summary:         "Approved context-pack handoff plus coding-agent attestation and delivery review.",
			Reasons:         []string{"Quick-route work item has no lead artifact"},
			Obligations: []string{
				"Stay inside the Context Pack scope and acceptance criteria.",
				"Report one completion claim per acceptance criterion.",
				"Run delivery review before treating the work as complete.",
			},
		}
		return out, nil
	}
	if h.Artifacts == nil {
		return nil, huma.Error503ServiceUnavailable("artifacts service not configured")
	}
	art, err := getArtifactForWorkspace(ctx, h.Artifacts, workspace.ID(ctx), cr.LeadArtifactID)
	if err != nil {
		return nil, mapArtifactError("work-item-policy", err)
	}
	p, err := resolvedProfileFromSnapshot(art.PolicySnapshotJSON)
	if err != nil {
		return nil, err
	}
	out := &CLIPolicyOutput{}
	out.Body = governanceprofile.ExplainSnapshot(*p)
	return out, nil
}

// resolvedProfileFromSnapshot parses a raw policy snapshot JSON string into a
// ResolvedProfile so ExplainSnapshot can produce a full Explanation.
func resolvedProfileFromSnapshot(raw string) (*governanceprofile.ResolvedProfile, error) {
	if raw == "" {
		return &governanceprofile.ResolvedProfile{
			GovernanceLevel: governanceprofile.GovernanceStandard,
			ApprovalPolicy:  "human_required",
			EvidencePolicy:  "attested_ok",
		}, nil
	}
	if _, err := governanceprofile.ParseSnapshot(raw); err != nil {
		return nil, huma.Error409Conflict("artifact governance snapshot is incompatible")
	}
	var rp governanceprofile.ResolvedProfile
	if err := json.Unmarshal([]byte(raw), &rp); err != nil {
		return nil, huma.Error409Conflict("artifact governance snapshot is incompatible")
	}
	return &rp, nil
}
