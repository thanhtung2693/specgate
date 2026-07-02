package client

// Meta is the response from GET /api/v1/meta.
type Meta struct {
	APIVersion            string          `json:"api_version"`
	ServerVersion         string          `json:"server_version,omitempty"`
	WebURL                string          `json:"web_url,omitempty"`
	RecommendedCLIVersion string          `json:"recommended_cli_version,omitempty"`
	MinimumCLIVersion     string          `json:"minimum_cli_version,omitempty"`
	Capabilities          map[string]bool `json:"capabilities,omitempty"`
	// Legacy build-info fields from early server versions.
	Version string `json:"version,omitempty"`
	Commit  string `json:"commit,omitempty"`
	Date    string `json:"date,omitempty"`
}

// PhaseCounts holds work item counts broken down by phase. Field names + tags
// match the doc-registry GovernanceStatusCounts response.
type PhaseCounts struct {
	Intake  int `json:"intake"`
	Draft   int `json:"draft"`
	Review  int `json:"review"`
	Ready   int `json:"ready"`
	Handoff int `json:"handoff"`
	Total   int `json:"total"`
}

// NeedsAttentionItem is a work item that requires human or agent action. Matches
// the doc-registry GovernanceStatusAttentionItem response.
type NeedsAttentionItem struct {
	ID     string   `json:"change_request_id"`
	Key    string   `json:"key"`
	Title  string   `json:"title"`
	Phase  string   `json:"phase"`
	Issues []string `json:"issues,omitempty"`
}

// GovernanceStatus is the response from GET /api/v1/status.
type GovernanceStatus struct {
	Counts         PhaseCounts          `json:"counts"`
	NeedsAttention []NeedsAttentionItem `json:"attention,omitempty"`
}

// StatsLedgerEntry is one "SpecGate caught something" event in StatsResult.
type StatsLedgerEntry struct {
	OccurredAt       string `json:"occurred_at"`
	ChangeRequestKey string `json:"change_request_key"`
	Kind             string `json:"kind"` // gate_catch | review_catch | ambiguity_block
	Gate             string `json:"gate,omitempty"`
	Detail           string `json:"detail,omitempty"`
}

// StatsResult is the response from GET /api/v1/stats. Field names + tags match
// the doc-registry StatsResult response.
type StatsResult struct {
	WindowDays             int                `json:"window_days"`
	WorkspaceID            string             `json:"workspace_id,omitempty"`
	ReviewedItems          int                `json:"reviewed_items"`
	FirstPass              int                `json:"first_pass"`
	GateCatchesPreBuild    int                `json:"gate_catches_pre_build"`
	ReviewCatchesPostBuild int                `json:"review_catches_post_build"`
	ReviewCatchesFixed     int                `json:"review_catches_fixed"`
	Rework                 int                `json:"rework"`
	ItemsWithRework        int                `json:"items_with_rework"`
	AmbiguityBlocks        int                `json:"ambiguity_blocks"`
	CycleTimeAvgHours      float64            `json:"cycle_time_avg_hours"`
	CycleTimeItems         int                `json:"cycle_time_items"`
	Ledger                 []StatsLedgerEntry `json:"ledger,omitempty"`
}

// IdentityUser is a local SpecGate user used for attribution and workspace membership.
type IdentityUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

// IdentityWorkspace is a local workspace used to scope CLI selection.
type IdentityWorkspace struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// IdentityBootstrapInput creates or finds a local user/workspace pair.
type IdentityBootstrapInput struct {
	WorkspaceName string `json:"workspace_name"`
	DisplayName   string `json:"display_name"`
	Username      string `json:"username"`
	Email         string `json:"email,omitempty"`
}

// IdentitySelection is the current user/workspace pair returned by bootstrap.
type IdentitySelection struct {
	User      IdentityUser      `json:"user"`
	Workspace IdentityWorkspace `json:"workspace"`
}

// WorkItem is a lightweight change request summary used in list responses.
type WorkItem struct {
	ID    string `json:"id"`
	Key   string `json:"key"`
	Title string `json:"title"`
	Phase string `json:"phase"`
}

// WorkItemArchiveResult is the response from POST /api/v1/work-items/{id}/archive.
type WorkItemArchiveResult struct {
	ChangeRequestID  string `json:"change_request_id"`
	ChangeRequestKey string `json:"change_request_key"`
	Archived         bool   `json:"archived"`
	ArchiveReason    string `json:"archive_reason,omitempty"`
}

// ResolvedWork is the response from POST /api/v1/work-items/resolve.
type ResolvedWork struct {
	ChangeRequestID  string `json:"change_request_id"`
	ChangeRequestKey string `json:"change_request_key"`
	FeatureID        string `json:"feature_id"`
	Title            string `json:"title"`
	Phase            string `json:"phase"`
	ContextPackURI   string `json:"context_pack_uri"`
	IssueKey         string `json:"issue_key,omitempty"`
	IssueURL         string `json:"issue_url,omitempty"`
	Lane             string `json:"lane,omitempty"`
}

// ContextPackResult is the response from GET /api/v1/work-items/{id}/context-pack.
type ContextPackResult struct {
	State           string `json:"state"` // "assembled" | "not_generated"
	Markdown        string `json:"markdown"`
	Lane            string `json:"lane,omitempty"`
	ChangeRequestID string `json:"change_request_id,omitempty"`
	ContextPackURI  string `json:"context_pack_uri,omitempty"`
}

// Artifact is the full artifact DTO from GET /api/v1/artifacts/{id}.
type Artifact struct {
	ID                   string `json:"id"`
	FeatureID            string `json:"feature_id"`
	FeatureName          string `json:"feature_name,omitempty"`
	Version              string `json:"version"`
	Status               string `json:"status"`
	RequestType          string `json:"request_type"`
	ImpactLevel          string `json:"impact_level"`
	ArtifactPhase        string `json:"artifact_phase,omitempty"`
	ArtifactCompleteness string `json:"artifact_completeness,omitempty"`
	CreatedBy            string `json:"created_by"`
	ApprovedBy           string `json:"approved_by,omitempty"`
	CreatedAt            string `json:"created_at"`
	UpdatedAt            string `json:"updated_at"`
}

// ArtifactList is the response from GET /api/v1/artifacts.
type ArtifactList struct {
	Items []Artifact `json:"items"`
	Total int        `json:"total"`
}

// ArtifactFilter holds optional filter params for ListArtifacts.
type ArtifactFilter struct {
	FeatureID string
	Status    string
	Limit     int
	Offset    int
}

// ArtifactFile is file metadata from GET /api/v1/artifacts/{id}/files.
type ArtifactFile struct {
	Path      string `json:"path"`
	Role      string `json:"role"`
	SizeBytes int64  `json:"size_bytes"`
}

// ArtifactFileContent is the response from GET /api/v1/artifacts/{id}/files/{key}.
type ArtifactFileContent struct {
	SignedURL string `json:"signed_url"`
	SizeBytes int64  `json:"size_bytes"`
	Content   string `json:"content,omitempty"`
}

// ProposalResult is the response from POST /api/v1/artifacts/{id}/proposals.
type ProposalResult struct {
	Drafted      bool     `json:"drafted"`
	SessionID    string   `json:"session_id"`
	ChangedFiles []string `json:"changed_files,omitempty"`
	Reason       string   `json:"reason,omitempty"`
}

// Profile is the response from GET /api/v1/governance-profiles.
type Profile struct {
	Namespace      string   `json:"namespace"`
	Key            string   `json:"key"`
	FullKey        string   `json:"full_key"`
	Version        string   `json:"version"`
	DisplayName    string   `json:"display_name"`
	ChangeType     string   `json:"change_type"`
	RequiredRoles  []string `json:"required_roles"`
	EnabledGates   []string `json:"enabled_gates"`
	Source         string   `json:"source"`
	ApprovalPolicy string   `json:"approval_policy"`
	EvidencePolicy string   `json:"evidence_policy"`
}

// Skill is the response from GET /api/v1/skills/{id}.
type Skill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// Feature is one governed feature from GET /workboard/features. Artifacts and
// change requests link to a feature by its stable `key`.
type Feature struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Version   int    `json:"version"`
	UpdatedAt string `json:"updated_at"`
}

// GateSummary is one current gate state row from GET /api/v1/work-items/{id}/gates.
type GateSummary struct {
	Gate  string `json:"gate"`
	State string `json:"state"`
	Hint  string `json:"hint,omitempty"`
}

// GatesStatusResult is the response from GET /api/v1/work-items/{id}/gates.
type GatesStatusResult struct {
	ChangeRequestID string        `json:"change_request_id"`
	Gates           []GateSummary `json:"gates"`
}

// GateRun is one row in GateHistoryResult.
type GateRun struct {
	GateRunID string `json:"gate_run_id"`
	Gate      string `json:"gate"`
	State     string `json:"state"`
	Hint      string `json:"hint,omitempty"`
	CreatedAt string `json:"created_at"`
}

// GateHistoryResult is the response from GET /api/v1/work-items/{id}/gate-history.
type GateHistoryResult struct {
	ChangeRequestID string    `json:"change_request_id"`
	Runs            []GateRun `json:"runs"`
}

// AcceptanceCriterion is one row from
// GET /workboard/change-requests/{id}/acceptance-criteria. Its ID is the
// criterion_id delivery reviews and completion reports correlate against.
type AcceptanceCriterion struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// CriterionReview is one criterion row in DeliveryStatusResult.
type CriterionReview struct {
	CriterionID string `json:"criterion_id,omitempty"`
	Text        string `json:"text,omitempty"`
	Verdict     string `json:"verdict,omitempty"`
	Why         string `json:"why,omitempty"`
}

// DeliveryStatusResult is the response from GET /api/v1/work-items/{id}/delivery-status.
type DeliveryStatusResult struct {
	ChangeRequestID string            `json:"change_request_id"`
	GateRunID       string            `json:"gate_run_id,omitempty"`
	Found           bool              `json:"found"`
	Verdict         string            `json:"verdict,omitempty"`
	Hint            string            `json:"hint,omitempty"`
	Confidence      float64           `json:"confidence,omitempty"`
	ReviewedAt      string            `json:"reviewed_at,omitempty"`
	OutstandingMD   string            `json:"outstanding_md,omitempty"`
	PerCriterion    []CriterionReview `json:"per_criterion,omitempty"`
}

// PolicyLineageEntry is one entry in a policy lineage chain.
type PolicyLineageEntry struct {
	Key     string `json:"key,omitempty"`
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"`
}

// PolicyExplanation is the response from GET /api/v1/policies/levels items,
// POST /api/v1/policies/resolve, GET /api/v1/work-items/{id}/policy, and
// GET /api/v1/artifacts/{id}/policy.
type PolicyExplanation struct {
	GovernanceLevel string               `json:"governance_level"`
	Title           string               `json:"title"`
	Summary         string               `json:"summary"`
	Reasons         []string             `json:"reasons,omitempty"`
	Obligations     []string             `json:"obligations,omitempty"`
	PolicyLineage   []PolicyLineageEntry `json:"policy_lineage,omitempty"`
}

// GovernanceLevel is the execution projection for one built-in governance tier
// returned by GET /api/v1/policies/levels.
type GovernanceLevel struct {
	GovernanceLevel  string   `json:"governance_level"`
	DisplayName      string   `json:"display_name"`
	ApprovalPolicy   string   `json:"approval_policy"`
	EvidencePolicy   string   `json:"evidence_policy"`
	RequiredRoles    []string `json:"required_roles"`
	RequiredTopics   []string `json:"required_topics"`
	RequiredEvidence []string `json:"required_evidence"`
	EnabledGates     []string `json:"enabled_gates"`
}

// ResolvePolicyInput is the request body for POST /api/v1/policies/resolve.
type ResolvePolicyInput struct {
	RequestType              string         `json:"request_type"`
	ImpactLevel              string         `json:"impact_level"`
	RequestedGovernanceLevel string         `json:"requested_governance_level,omitempty"`
	ImpactDeclaration        map[string]any `json:"impact_declaration,omitempty"`
}
