package api

import (
	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/governanceprofile"
)

// --- GET /api/v1/meta ---

// CLIMetaDTO is the response for GET /api/v1/meta.
type CLIMetaDTO struct {
	// APIVersion identifies the REST facade contract; always "specgate.api/v1".
	APIVersion string `json:"api_version" doc:"REST facade contract version."`
	// Build info injected at release time via ldflags.
	Version string `json:"version" doc:"Semantic version or git tag (dev when not set)."`
	Commit  string `json:"commit" doc:"Short git SHA (unknown when not set)."`
	Date    string `json:"date" doc:"ISO-8601 build timestamp (unknown when not set)."`
	// RecommendedCLIVersion is the CLI version users should run against this server.
	RecommendedCLIVersion string `json:"recommended_cli_version,omitempty" doc:"Recommended specgate CLI version for this server."`
	// MinimumCLIVersion is reserved for future hard compatibility floors.
	MinimumCLIVersion string `json:"minimum_cli_version,omitempty" doc:"Minimum supported specgate CLI version, when enforced."`
	// Capabilities advertises optional server-side features.
	Capabilities map[string]bool `json:"capabilities" doc:"Optional feature flags."`
}

type CLIMetaOutput struct {
	Body CLIMetaDTO
}

// --- GET /api/v1/status ---

type CLIStatusInput struct {
	WorkspaceID string `query:"workspace_id" doc:"Optional workspace id filter for local workspace-scoped CLI lists."`
}

type CLIStatusOutput struct {
	Body governanceops.GovernanceStatusResult
}

// --- GET /api/v1/stats ---

type CLIStatsInput struct {
	WorkspaceID string `query:"workspace_id" doc:"Optional workspace id filter; gate runs and feedback events join their change request for the filter."`
	Days        int    `query:"days" doc:"Rolling window size in days. Default 30; clamped to 1..365."`
}

type CLIStatsOutput struct {
	Body governanceops.StatsResult
}

// --- POST /api/v1/work-items/resolve ---

type CLIResolveWorkRefInput struct {
	Body governanceops.ResolveWorkRefInput
}

type CLIResolveWorkRefOutput struct {
	Body governanceops.ResolvedWork
}

// --- GET /api/v1/work-items/{id}/context-pack ---

type CLIContextPackInput struct {
	ID   string `path:"id"`
	Lane string `query:"lane" doc:"Optional lane filter: fe, be, or empty for full pack."`
}

type CLIContextPackOutput struct {
	Body governanceops.ContextPackResult
}

// --- POST /api/v1/work-items/{id}/feedback ---

type CLIFeedbackInput struct {
	ID   string `path:"id"`
	Body governanceops.ReportFeedbackInput
}

type CLIFeedbackOutput struct {
	Body *governanceops.ReportFeedbackResult
}

// --- POST /api/v1/artifacts/publish ---

type CLIPublishArtifactInput struct {
	Body governanceops.PublishArtifactInput
}

type CLIPublishArtifactOutput struct {
	Body *governanceops.PublishArtifactResult
}

// --- POST /api/v1/work-items/{id}/readiness ---

type CLIWorkItemIDInput struct {
	ID string `path:"id"`
}

type CLIReadinessOutput struct {
	Body *governanceops.ReadinessResult
}

// --- POST /api/v1/work-items/{id}/llm-gates and /delivery-review ---

// CLIRawOutput carries an opaque JSON map returned by agents-backed operations.
type CLIRawOutput struct {
	Body map[string]any
}

type CLIArchiveWorkItemInput struct {
	ID   string `path:"id"`
	Body struct {
		Reason string `json:"reason,omitempty"`
		Actor  string `json:"actor,omitempty"`
	}
}

type CLIArchiveWorkItemOutput struct {
	Body governanceops.ResolvedWork
}

// --- POST /api/v1/work-items ---

type CLICreateQuickWorkItemInput struct {
	Body governanceops.CreateQuickWorkItemInput
}

// --- GET /api/v1/skills ---

type CLIListSkillsInput struct {
	Name string `query:"name" doc:"Optional name filter (case-insensitive prefix match)."`
}

type CLIListSkillsOutput struct {
	Body struct {
		Items []SkillDTO `json:"items"`
	}
}

// --- GET /api/v1/skills/{id} ---

type CLIGetSkillInput struct {
	ID string `path:"id"`
}

type CLIGetSkillOutput struct {
	Body SkillDTO
}

// --- GET /api/v1/artifacts ---

type CLIListArtifactsInput struct {
	FeatureID string `query:"feature_id" doc:"Optional feature ID filter."`
	Status    string `query:"status" doc:"Optional status filter." enum:"draft,needs_changes,approved,superseded"`
	Limit     int    `query:"limit" minimum:"1" maximum:"200" default:"50"`
	Offset    int    `query:"offset" minimum:"0" default:"0"`
}

type CLIListArtifactsOutput struct {
	Body struct {
		Items []ArtifactDTO `json:"items"`
		Total int           `json:"total"`
	}
}

// --- GET /api/v1/artifacts/{id} ---

type CLIGetArtifactInput struct {
	ID string `path:"id" doc:"Artifact UUID"`
}

type CLIGetArtifactOutput struct {
	Body ArtifactDTO
}

// --- GET /api/v1/artifacts/{id}/files ---

type CLIListArtifactFilesInput struct {
	ID string `path:"id" doc:"Artifact UUID"`
}

type CLIListArtifactFilesOutput struct {
	Body struct {
		Items []ArtifactFileDTO `json:"items"`
	}
}

// --- POST /api/v1/artifacts/{id}/proposals ---

type CLIDraftProposalInput struct {
	ID   string `path:"id" doc:"Artifact UUID"`
	Body governanceops.DraftArtifactUpdateInput
}

type CLIDraftProposalOutput struct {
	Body *governanceops.DraftArtifactUpdateResult
}

// --- GET /api/v1/work-items/{id}/gates ---

type CLIGatesStatusInput struct {
	ID string `path:"id" doc:"Change request ID"`
}

type CLIGatesStatusOutput struct {
	Body struct {
		ChangeRequestID string                      `json:"change_request_id"`
		Gates           []governanceops.GateSummary `json:"gates"`
	}
}

// --- GET /api/v1/work-items/{id}/gate-history ---

type CLIGateHistoryInput struct {
	ID    string `path:"id" doc:"Change request ID"`
	Gate  string `query:"gate" doc:"Optional gate name filter."`
	Limit int    `query:"limit" minimum:"1" maximum:"200" default:"20"`
}

type CLIGateHistoryOutput struct {
	Body governanceops.GateHistoryResult
}

// --- GET /api/v1/work-items/{id}/delivery-status ---

type CLIDeliveryStatusInput struct {
	ID     string `path:"id" doc:"Change request ID"`
	Detail bool   `query:"detail" doc:"Include per-criterion breakdown."`
}

type CLIDeliveryStatusOutput struct {
	Body governanceops.DeliveryStatusResult
}

// --- GET /api/v1/policies/levels ---

// CLIPolicyLevelDTO is the representation of one built-in governance tier
// returned by GET /api/v1/policies/levels.
type CLIPolicyLevelDTO struct {
	GovernanceLevel  governanceprofile.GovernanceLevel `json:"governance_level"`
	DisplayName      string                            `json:"display_name"`
	ApprovalPolicy   string                            `json:"approval_policy"`
	EvidencePolicy   string                            `json:"evidence_policy"`
	RequiredRoles    []string                          `json:"required_roles"`
	RequiredTopics   []string                          `json:"required_topics"`
	RequiredEvidence []string                          `json:"required_evidence"`
	EnabledGates     []string                          `json:"enabled_gates"`
}

type CLIListPolicyLevelsOutput struct {
	Body struct {
		Levels []CLIPolicyLevelDTO `json:"levels"`
	}
}

// --- POST /api/v1/policies/resolve ---

// CLIResolvePolicyBody is the request body for POST /api/v1/policies/resolve.
type CLIResolvePolicyBody struct {
	RequestType              string                              `json:"request_type" doc:"Work type, e.g. bugfix or new_feature."`
	ImpactLevel              string                              `json:"impact_level" doc:"Impact level: low, medium, or high."`
	RequestedGovernanceLevel string                              `json:"requested_governance_level,omitempty" doc:"Author-requested minimum tier."`
	ImpactDeclaration        governanceprofile.ImpactDeclaration `json:"impact_declaration,omitempty" doc:"Author self-declared impact signals."`
}

type CLIResolvePolicyInput struct {
	Body CLIResolvePolicyBody
}

// CLIPolicyOutput is the response for POST /api/v1/policies/resolve and
// GET /api/v1/artifacts/{id}/policy and GET /api/v1/work-items/{id}/policy.
type CLIPolicyOutput struct {
	Body governanceprofile.Explanation
}

// --- GET /api/v1/artifacts/{id}/policy ---

type CLIArtifactPolicyInput struct {
	ID string `path:"id" doc:"Artifact UUID"`
}

// --- GET /api/v1/work-items/{id}/policy ---

type CLIWorkItemPolicyInput struct {
	ID string `path:"id" doc:"Change request ID"`
}
