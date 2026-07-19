package client

// Meta is the response from GET /api/v1/meta.
type Meta struct {
	APIVersion            string                      `json:"api_version"`
	ServerVersion         string                      `json:"server_version,omitempty"`
	WebURL                string                      `json:"web_url,omitempty"`
	RecommendedCLIVersion string                      `json:"recommended_cli_version,omitempty"`
	CapabilityDetails     map[string]CapabilityDetail `json:"capability_details,omitempty"`
}

// CapabilityDetail describes whether an optional Full-mode capability can be
// used now, needs configuration, or is not present in this deployment.
type CapabilityDetail struct {
	State       string `json:"state"`
	Reason      string `json:"reason,omitempty"`
	NextCommand string `json:"next_command,omitempty"`
}

// ComponentHealth is the optional appliance diagnostic response. Origin-only
// self-host servers may not expose this endpoint.
type ComponentHealth struct {
	Status     string                           `json:"status"`
	Version    string                           `json:"version,omitempty"`
	Components map[string]ComponentHealthStatus `json:"components"`
}

type ComponentHealthStatus struct {
	Status     string `json:"status"`
	Version    string `json:"version,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Endpoint   string `json:"endpoint,omitempty"`
	PublicPath string `json:"public_path,omitempty"`
}

type SchemaStatus struct {
	Status  string   `json:"status"`
	Message string   `json:"message"`
	Missing []string `json:"missing,omitempty"`
}

// PhaseCounts holds work item counts broken down by phase. Field names + tags
// match the doc-registry GovernanceStatusCounts response.
type PhaseCounts struct {
	Intake    int `json:"intake"`
	Draft     int `json:"draft"`
	Review    int `json:"review"`
	Ready     int `json:"ready"`
	Delivered int `json:"delivered"`
	Total     int `json:"total"`
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

type WorkspaceMember struct {
	WorkspaceID string `json:"workspace_id"`
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email,omitempty"`
	Role        string `json:"role"`
	CreatedAt   string `json:"created_at,omitempty"`
	Current     bool   `json:"current,omitempty"`
}

type WorkspaceMembersResult struct {
	Workspace   IdentityWorkspace `json:"workspace"`
	CurrentUser IdentityUser      `json:"current_user,omitempty"`
	Members     []WorkspaceMember `json:"members"`
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
	IssueKey         string `json:"issue_key,omitempty"`
	IssueURL         string `json:"issue_url,omitempty"`
}

// ContextPackResult is the response from GET /api/v1/work-items/{id}/context-pack.
type ContextPackResult struct {
	State            string `json:"state"` // "assembled" | "not_generated"
	Markdown         string `json:"markdown"`
	ChangeRequestID  string `json:"change_request_id,omitempty"`
	SourceArtifactID string `json:"source_artifact_id,omitempty"`
	ArtifactID       string `json:"artifact_id,omitempty"`
}

// AuditEvent is one entry in an AuditTrail (GET /api/v1/audit/{ref}).
type AuditEvent struct {
	Timestamp string `json:"timestamp"`
	Actor     string `json:"actor"`
	ActorKind string `json:"actor_kind"`
	Action    string `json:"action"`
	Subject   string `json:"subject"`
	Verdict   string `json:"verdict"`
	Trust     string `json:"trust"`
	Detail    string `json:"detail"`
}

// AuditTrail is the response from GET /api/v1/audit/{ref} — the chronological
// governance history for a work reference (events ascending by timestamp).
type AuditTrail struct {
	Ref              string       `json:"ref"`
	ChangeRequestID  string       `json:"change_request_id"`
	ChangeRequestKey string       `json:"change_request_key"`
	FeatureID        string       `json:"feature_id"`
	FeatureKey       string       `json:"feature_key"`
	FeatureName      string       `json:"feature_name"`
	Title            string       `json:"title"`
	Phase            string       `json:"phase"`
	Events           []AuditEvent `json:"events"`
	Chain            *ChainReport `json:"chain,omitempty"`
}

// ChainReport is the tamper-evidence verification result (audit --verify).
type ChainReport struct {
	State           string `json:"state"` // intact | tampered
	ArtifactID      string `json:"artifact_id,omitempty"`
	FirstBadEventID string `json:"first_bad_event_id,omitempty"`
	ChainedEvents   int    `json:"chained_events"`
}

// Artifact is the full artifact DTO from GET /api/v1/artifacts/{id}.
type Artifact struct {
	ID                   string `json:"id"`
	WorkspaceID          string `json:"workspace_id,omitempty"`
	FeatureID            string `json:"feature_id"`
	FeatureName          string `json:"feature_name,omitempty"`
	Version              string `json:"version"`
	Status               string `json:"status"`
	RequestType          string `json:"request_type"`
	ImpactLevel          string `json:"impact_level"`
	ArtifactPhase        string `json:"artifact_phase,omitempty"`
	ArtifactCompleteness string `json:"artifact_completeness,omitempty"`
	SourceKind           string `json:"source_kind,omitempty"`
	SourceID             string `json:"source_id,omitempty"`
	SourceRevision       string `json:"source_revision,omitempty"`
	SnapshotDigest       string `json:"snapshot_digest,omitempty"`
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
	WorkspaceID string
	FeatureID   string
	Status      string
	// ExcludeStatus drops one status server-side (default "current" list
	// views exclude superseded). Ignored by the server when Status is set.
	ExcludeStatus string
	Limit         int
	Offset        int
}

// ArtifactFile is file metadata from GET /api/v1/artifacts/{id}/files.
type ArtifactFile struct {
	Path          string `json:"path"`
	Role          string `json:"role"`
	SizeBytes     int64  `json:"size_bytes"`
	ContentSHA256 string `json:"content_sha256"`
}

// ArtifactFileContent is the response from GET /api/v1/artifacts/{id}/files/_?path=....
type ArtifactFileContent struct {
	SizeBytes int64  `json:"size_bytes"`
	Content   string `json:"content"`
}

// UpdateArtifactStatusInput is the request body for PATCH /artifacts/{id}/status —
// the same human-decision endpoint the web UI uses. Field names + tags match the
// doc-registry UpdateStatusInput body.
type UpdateArtifactStatusInput struct {
	Status     string `json:"status"`
	ApprovedBy string `json:"approved_by,omitempty"`
	Note       string `json:"note,omitempty"`
	ActorKind  string `json:"actor_kind,omitempty"`
}

// Skill is the response from GET /api/v1/skills/{id}.
type Skill struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// Feature is one governed feature from GET /workboard/features. Artifacts and
// change requests link to a feature by its stable `key`.
type Feature struct {
	ID                  string `json:"id"`
	WorkspaceID         string `json:"workspace_id,omitempty"`
	Key                 string `json:"key"`
	Name                string `json:"name"`
	Status              string `json:"status"`
	Version             int    `json:"version"`
	CanonicalArtifactID string `json:"canonical_artifact_id,omitempty"`
	UpdatedAt           string `json:"updated_at"`
}

// KnowledgeDocument is one Governance Knowledge document version.
type KnowledgeDocument struct {
	DocumentID       string   `json:"document_id"`
	Version          string   `json:"version"`
	WorkspaceID      string   `json:"workspace_id,omitempty"`
	ParentVersion    string   `json:"parent_version,omitempty"`
	IsLatest         bool     `json:"is_latest"`
	Title            string   `json:"title"`
	DocumentType     string   `json:"document_type"`
	AuthorityLevel   string   `json:"authority_level"`
	SourceKind       string   `json:"source_kind"`
	SourceURI        string   `json:"source_uri,omitempty"`
	MimeType         string   `json:"mime_type,omitempty"`
	OriginalFilename string   `json:"original_filename,omitempty"`
	Status           string   `json:"status"`
	LinkedFeatureID  string   `json:"linked_feature_id,omitempty"`
	LinkedRequestID  string   `json:"linked_request_id,omitempty"`
	UploadedBy       string   `json:"uploaded_by,omitempty"`
	CreatedAt        string   `json:"created_at,omitempty"`
	UpdatedAt        string   `json:"updated_at,omitempty"`
	Summary          string   `json:"summary,omitempty"`
	Notes            string   `json:"notes,omitempty"`
	Tags             []string `json:"tags,omitempty"`
	ErrorMessage     string   `json:"error_message,omitempty"`
	ChunkCount       int      `json:"chunk_count,omitempty"`
}

type KnowledgeDocumentList struct {
	Items             []KnowledgeDocument `json:"items"`
	Total             int                 `json:"total"`
	EmbeddingsEnabled bool                `json:"embeddings_enabled"`
}

type KnowledgeDocumentDetail struct {
	Document         KnowledgeDocument   `json:"document"`
	History          []KnowledgeDocument `json:"history,omitempty"`
	ExtractedPreview string              `json:"extracted_preview,omitempty"`
}

type KnowledgeListFilter struct {
	WorkspaceID     string
	LinkedFeatureID string
	LinkedRequestID string
	DocumentType    string
	Status          string
	IncludeHistory  bool
	Limit           int
	Offset          int
}

type KnowledgeCreateTextInput struct {
	WorkspaceID     string   `json:"workspace_id"`
	Title           string   `json:"title"`
	DocumentType    string   `json:"document_type"`
	AuthorityLevel  string   `json:"authority_level"`
	LinkedFeatureID string   `json:"linked_feature_id,omitempty"`
	LinkedRequestID string   `json:"linked_request_id,omitempty"`
	UploadedBy      string   `json:"uploaded_by,omitempty"`
	ActorRole       string   `json:"actor_role,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	Notes           string   `json:"notes,omitempty"`
	Content         string   `json:"content"`
}

type KnowledgeCurateLinksInput struct {
	WorkspaceID      string `json:"workspace_id,omitempty"`
	Version          string `json:"version,omitempty"`
	LinkedFeatureID  string `json:"linked_feature_id,omitempty"`
	LinkedRequestID  string `json:"linked_request_id,omitempty"`
	ClearFeatureLink bool   `json:"clear_feature_link,omitempty"`
	ClearRequestLink bool   `json:"clear_request_link,omitempty"`
	UploadedBy       string `json:"uploaded_by,omitempty"`
	ActorRole        string `json:"actor_role,omitempty"`
	Notes            string `json:"notes,omitempty"`
}

type KnowledgeSearchInput struct {
	WorkspaceID     string   `json:"workspace_id"`
	Query           string   `json:"query"`
	LinkedFeatureID string   `json:"linked_feature_id,omitempty"`
	LinkedRequestID string   `json:"linked_request_id,omitempty"`
	DocumentTypes   []string `json:"document_types,omitempty"`
	AuthorityLevels []string `json:"authority_levels,omitempty"`
	MaxChunks       int      `json:"max_chunks,omitempty"`
	IncludeHistory  bool     `json:"include_history,omitempty"`
	ContextMode     string   `json:"context_mode,omitempty"`
	ContextMaxChars int      `json:"context_max_chars,omitempty"`
}

type KnowledgeSearchResult struct {
	Kind            string   `json:"kind"`
	WorkspaceID     string   `json:"workspace_id"`
	DocumentID      string   `json:"document_id"`
	Version         string   `json:"version"`
	Title           string   `json:"title"`
	DocumentType    string   `json:"document_type"`
	AuthorityLevel  string   `json:"authority_level"`
	ChunkText       string   `json:"chunk_text"`
	Score           float64  `json:"score"`
	SourceURI       string   `json:"source_uri"`
	ChunkIndex      int      `json:"chunk_index"`
	URL             string   `json:"url"`
	ContextText     string   `json:"context_text,omitempty"`
	ContextKind     string   `json:"context_kind,omitempty"`
	Heading         string   `json:"heading,omitempty"`
	HeadingPath     []string `json:"heading_path,omitempty"`
	SectionIndex    int      `json:"section_index,omitempty"`
	StartChunkIndex int      `json:"start_chunk_index,omitempty"`
	EndChunkIndex   int      `json:"end_chunk_index,omitempty"`
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
	// VerificationBinding names a check in checks[] whose result deterministically
	// backs this criterion's delivery-review verdict.
	VerificationBinding string `json:"verification_binding,omitempty"`
}

// GovernanceFeedbackEvent is an append-only coding-agent feedback record.
type GovernanceFeedbackEvent struct {
	ID              string `json:"id"`
	ChangeRequestID string `json:"change_request_id"`
	EventType       string `json:"event_type"`
	PayloadJSON     string `json:"payload_json"`
	CreatedAt       string `json:"created_at"`
}

// CriterionReview is one criterion row in DeliveryStatusResult.
type CriterionReview struct {
	CriterionID         string `json:"criterion_id,omitempty"`
	Text                string `json:"text,omitempty"`
	Verdict             string `json:"verdict,omitempty"`
	Why                 string `json:"why,omitempty"`
	VerificationBinding string `json:"verification_binding,omitempty"`
	TrustTier           string `json:"trust_tier,omitempty"`
}

// CheckResult is one automated check included in the authoritative delivery
// review.
type CheckResult struct {
	Name   string `json:"name,omitempty"`
	Status string `json:"status,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// GitReceipt is the repository identity persisted with completion evidence.
// It contains metadata and a digest only; source contents are never uploaded.
type GitReceipt struct {
	Repository   string   `json:"repository"`
	Availability string   `json:"availability"`
	Branch       string   `json:"branch"`
	BaseRevision string   `json:"base_revision"`
	HeadRevision string   `json:"head_revision"`
	ChangedFiles []string `json:"changed_files"`
	DiffDigest   string   `json:"diff_digest"`
	Warnings     []string `json:"warnings"`
}

type PeerReviewState struct {
	State      string `json:"state"`
	AgentName  string `json:"agent_name,omitempty"`
	ReviewedAt string `json:"reviewed_at,omitempty"`
}

// DeliveryStatusResult is the response from GET /api/v1/work-items/{id}/delivery-status.
type DeliveryStatusResult struct {
	ChangeRequestID  string            `json:"change_request_id"`
	GateRunID        string            `json:"gate_run_id,omitempty"`
	Found            bool              `json:"found"`
	Verdict          string            `json:"verdict,omitempty"`
	EvidenceVerdict  string            `json:"evidence_verdict,omitempty"`
	ReasonCode       string            `json:"reason_code,omitempty"`
	Hint             string            `json:"hint,omitempty"`
	Confidence       *float64          `json:"confidence,omitempty"`
	JudgeModel       string            `json:"judge_model,omitempty"`
	EvalSuite        string            `json:"eval_suite_version,omitempty"`
	ReviewedAt       string            `json:"reviewed_at,omitempty"`
	Executor         string            `json:"executor,omitempty"`
	Actor            string            `json:"actor,omitempty"`
	Note             string            `json:"note,omitempty"`
	Summary          string            `json:"summary,omitempty"`
	OutstandingMD    string            `json:"outstanding_md,omitempty"`
	AssuranceSources []string          `json:"assurance_sources,omitempty"`
	PerCriterion     []CriterionReview `json:"per_criterion,omitempty"`
	Checks           []CheckResult     `json:"checks,omitempty"`
	GitReceipt       *GitReceipt       `json:"git_receipt,omitempty"`
	PeerReview       PeerReviewState   `json:"peer_review,omitempty"`
}

type DeliveryDecisionInput struct {
	Decision string `json:"decision"`
	Actor    string `json:"actor"`
	Note     string `json:"note,omitempty"`
}

type DeliveryDecisionResult struct {
	ChangeRequestID string `json:"change_request_id"`
	GateRunID       string `json:"gate_run_id"`
	Verdict         string `json:"verdict"`
	Hint            string `json:"hint,omitempty"`
	Executor        string `json:"executor"`
	Actor           string `json:"actor,omitempty"`
	Note            string `json:"note,omitempty"`
	Summary         string `json:"summary,omitempty"`
	ReviewedAt      string `json:"reviewed_at,omitempty"`
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
