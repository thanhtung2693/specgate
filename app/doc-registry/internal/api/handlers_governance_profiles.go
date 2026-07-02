package api

import (
	"context"
	"encoding/json"

	"github.com/danielgtaylor/huma/v2"

	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/governanceprofile"
)

// GovernanceProfileDTO is the API representation of a resolved governance
// profile (built-in or imported). approval_policy / evidence_policy carry the
// EFFECTIVE value (default applied), so the catalog never shows an empty cell
// and the UI does not re-derive the safe default. Per spec §6.
type GovernanceProfileDTO struct {
	Namespace        string   `json:"namespace"`
	Key              string   `json:"key"`
	FullKey          string   `json:"full_key"`
	Version          string   `json:"version"`
	DisplayName      string   `json:"display_name"`
	ChangeType       string   `json:"change_type"`
	RequiredRoles    []string `json:"required_roles"`
	RequiredTopics   []string `json:"required_topics"`
	RequiredEvidence []string `json:"required_evidence"`
	EnabledGates     []string `json:"enabled_gates"`
	RendererKey      string   `json:"renderer_key,omitempty"`
	Digest           string   `json:"digest"`
	Source           string   `json:"source" enum:"builtin,import"`
	ApprovalPolicy   string   `json:"approval_policy" enum:"human_required,self_approve,auto"`
	EvidencePolicy   string   `json:"evidence_policy" enum:"attested_ok,corroborated_required"`
}

func governanceProfileDTO(p governanceprofile.ResolvedProfile) GovernanceProfileDTO {
	return GovernanceProfileDTO{
		Namespace:        p.Namespace,
		Key:              p.Key,
		FullKey:          p.FullKey,
		Version:          p.Version,
		DisplayName:      p.DisplayName,
		ChangeType:       p.ChangeType,
		RequiredRoles:    p.RequiredRoles,
		RequiredTopics:   p.RequiredTopics,
		RequiredEvidence: p.RequiredEvidence,
		EnabledGates:     p.EnabledGates,
		RendererKey:      p.RendererKey,
		Digest:           p.Digest,
		Source:           string(p.Source),
		// Surface the effective policy so empty/legacy imports read as the safe
		// default rather than an empty value.
		ApprovalPolicy: governanceprofile.EffectiveApprovalPolicy(p.ApprovalPolicy),
		EvidencePolicy: governanceprofile.EffectiveEvidencePolicy(p.EvidencePolicy),
	}
}

func governanceProfileDTOs(list []governanceprofile.ResolvedProfile) []GovernanceProfileDTO {
	out := make([]GovernanceProfileDTO, len(list))
	for i := range list {
		out[i] = governanceProfileDTO(list[i])
	}
	return out
}

// ListGovernanceProfilesOutput is the body for GET /governance-profiles.
type ListGovernanceProfilesOutput struct {
	Body struct {
		Items []GovernanceProfileDTO `json:"items"`
	} `json:"body"` // explicit tag so clients see lowercase `body`
}

func (h *Handlers) requireGovernanceProfiles() (*governanceprofile.Service, error) {
	if h.GovernanceProfiles == nil {
		return nil, huma.Error503ServiceUnavailable("governance profiles not configured")
	}
	return h.GovernanceProfiles, nil
}

// ListGovernanceProfiles returns the built-in + imported governance profiles
// (the SpecGate change-type bar registry) for the read-only management UI.
func (h *Handlers) ListGovernanceProfiles(ctx context.Context, _ *struct{}) (*ListGovernanceProfilesOutput, error) {
	svc, err := h.requireGovernanceProfiles()
	if err != nil {
		return nil, err
	}
	list, err := svc.ListProfiles(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("list governance profiles", err)
	}
	out := &ListGovernanceProfilesOutput{}
	out.Body.Items = governanceProfileDTOs(list)
	return out, nil
}

// CLIListPolicyLevels handles GET /api/v1/policies/levels — returns the
// execution projection for each of the three built-in governance tiers.
// The response is pure: no DB access, no snapshot parsing.
func (h *Handlers) CLIListPolicyLevels(_ context.Context, _ *struct{}) (*CLIListPolicyLevelsOutput, error) {
	entries := governanceprofile.ListBuiltInPolicyLevels()
	levels := make([]CLIPolicyLevelDTO, 0, len(entries))
	for _, e := range entries {
		levels = append(levels, CLIPolicyLevelDTO{
			GovernanceLevel:  e.Level,
			DisplayName:      e.Definition.DisplayName,
			ApprovalPolicy:   governanceprofile.EffectiveApprovalPolicy(e.Definition.ApprovalPolicy),
			EvidencePolicy:   governanceprofile.EffectiveEvidencePolicy(e.Definition.EvidencePolicy),
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
// anything. This is a dry run: it accepts the same classification fields as
// artifact publish but writes nothing. Per spec §6 (policy/v1 resolver).
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
// Returns 404 when the artifact is not found, 409 when the snapshot schema
// is unrecognised (fail-closed).
func (h *Handlers) CLIArtifactPolicy(ctx context.Context, in *CLIArtifactPolicyInput) (*CLIPolicyOutput, error) {
	if h.Artifacts == nil {
		return nil, huma.Error503ServiceUnavailable("artifacts service not configured")
	}
	art, err := h.Artifacts.Get(ctx, in.ID)
	if err != nil {
		return nil, mapArtifactError("get artifact for policy", err)
	}
	p, err := resolvedProfileFromSnapshot(art.GatesProfileSnapshotJSON)
	if err != nil {
		return nil, err
	}
	out := &CLIPolicyOutput{}
	out.Body = governanceprofile.ExplainSnapshot(*p)
	return out, nil
}

// CLIWorkItemPolicy handles GET /api/v1/work-items/{id}/policy — returns the
// governance explanation for the lead artifact of a change request. Quick-route
// items may have no lead artifact; for those, return the streamlined context-pack
// policy rather than a 404 so IDE agents still get actionable guidance.
func (h *Handlers) CLIWorkItemPolicy(ctx context.Context, in *CLIWorkItemPolicyInput) (*CLIPolicyOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
		return nil, err
	}
	// Resolve the work ref to get the canonical change request ID.
	work, err := svc.ResolveWorkRef(ctx, governanceops.ResolveWorkRefInput{Ref: in.ID})
	if err != nil {
		return nil, mapGovernanceError("work-item-policy", err)
	}
	// Retrieve the change request to read its lead artifact ID.
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
	art, err := h.Artifacts.Get(ctx, cr.LeadArtifactID)
	if err != nil {
		return nil, mapArtifactError("work-item-policy", err)
	}
	p, err := resolvedProfileFromSnapshot(art.GatesProfileSnapshotJSON)
	if err != nil {
		return nil, err
	}
	out := &CLIPolicyOutput{}
	out.Body = governanceprofile.ExplainSnapshot(*p)
	return out, nil
}

// resolvedProfileFromSnapshot parses a raw gates-profile snapshot JSON string
// into a ResolvedProfile so ExplainSnapshot can produce a full Explanation
// (including ReasonCodes and PolicyLineage that ParsedSnapshot drops).
//
// Strategy:
//  1. Call ParseSnapshot first for the fail-closed version gate — returns
//     ErrUnsupportedSnapshot for unrecognised schema versions.
//  2. If the snapshot passes the version gate, unmarshal directly into
//     ResolvedProfile: both the legacy and v1 shapes carry matching JSON tags
//     for every field ExplainSnapshot needs.
//
// An empty snapshot is treated as standard governance (no explanation data).
func resolvedProfileFromSnapshot(raw string) (*governanceprofile.ResolvedProfile, error) {
	if raw == "" {
		// No snapshot stored — return a bare standard profile.
		return &governanceprofile.ResolvedProfile{
			GovernanceLevel: governanceprofile.GovernanceStandard,
			ApprovalPolicy:  "human_required",
			EvidencePolicy:  "attested_ok",
		}, nil
	}
	// Version gate (fail-closed).
	if _, err := governanceprofile.ParseSnapshot(raw); err != nil {
		return nil, huma.Error409Conflict("artifact governance snapshot is incompatible")
	}
	// Full unmarshal for ExplainSnapshot (retains ReasonCodes, PolicyLineage).
	var rp governanceprofile.ResolvedProfile
	if err := json.Unmarshal([]byte(raw), &rp); err != nil {
		return nil, huma.Error409Conflict("artifact governance snapshot is incompatible")
	}
	return &rp, nil
}
