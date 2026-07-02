package governanceops

import (
	"github.com/specgate/doc-registry/internal/agentsclient"
	"github.com/specgate/doc-registry/internal/governanceprofile"
)

// ResolveWorkRefInput is the input to the unified work-reference resolver.
// Ref accepts a change-request ID, a change-request Key, a full HTTPS issue
// URL (provider inferred from host), or a bare tracker key (Provider required).
type ResolveWorkRefInput struct {
	Ref      string `json:"ref"`
	Provider string `json:"provider,omitempty"`
}

// ResolvedWork is the output of ResolveWorkRef and the MCP resolve_work_item tool.
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

// GovernanceStatusCounts is the phase breakdown in GovernanceStatusResult.
type GovernanceStatusCounts struct {
	Intake  int `json:"intake"`
	Draft   int `json:"draft"`
	Review  int `json:"review"`
	Ready   int `json:"ready"`
	Handoff int `json:"handoff"`
	Total   int `json:"total"`
}

// GovernanceStatusAttentionItem is one work item that has stale warnings.
type GovernanceStatusAttentionItem struct {
	ChangeRequestID string   `json:"change_request_id"`
	Key             string   `json:"key"`
	Title           string   `json:"title"`
	Phase           string   `json:"phase"`
	Issues          []string `json:"issues"`
}

// GovernanceStatusResult is the output of GovernanceStatus.
type GovernanceStatusResult struct {
	Summary   string                          `json:"summary"`
	Counts    GovernanceStatusCounts          `json:"counts"`
	Attention []GovernanceStatusAttentionItem `json:"attention"`
}

// GovernanceStatusInput filters the status snapshot.
type GovernanceStatusInput struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
}

// ListWorkItemsInput filters the list_work_items result.
type ListWorkItemsInput struct {
	Ready       bool   `json:"ready,omitempty"`
	HandedOff   bool   `json:"handed_off,omitempty"`
	Mine        bool   `json:"mine,omitempty"`
	WorkType    string `json:"work_type,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

// WorkItemSummary is one row in ListWorkItemsResult.
type WorkItemSummary struct {
	ChangeRequestID  string `json:"change_request_id"`
	ChangeRequestKey string `json:"change_request_key"`
	FeatureID        string `json:"feature_id"`
	Title            string `json:"title"`
	Phase            string `json:"phase"`
	ContextPackURI   string `json:"context_pack_uri"`
	WorkType         string `json:"work_type"`
}

// ListWorkItemsResult is the output of ListWorkItems.
type ListWorkItemsResult struct {
	Items []WorkItemSummary `json:"items"`
}

// GateSummary is one row in WorkStatusResult.Gates.
type GateSummary struct {
	Gate  string `json:"gate"`
	State string `json:"state"`
	Hint  string `json:"hint"`
}

// PendingHumanAction describes a governance gate that needs a human action.
type PendingHumanAction struct {
	Action string `json:"action"`
	Label  string `json:"label"`
	URL    string `json:"url,omitempty"`
}

// DeliveryReviewSummary is the embedded delivery-review snapshot in WorkStatusResult.
type DeliveryReviewSummary struct {
	Verdict    string `json:"verdict"`
	Hint       string `json:"hint"`
	ReviewedAt string `json:"reviewed_at"`
}

// WorkStatusResult is the output of WorkStatus.
type WorkStatusResult struct {
	ChangeRequestID     string                 `json:"change_request_id"`
	Title               string                 `json:"title"`
	Phase               string                 `json:"phase"`
	WorkType            string                 `json:"work_type"`
	Gates               []GateSummary          `json:"gates"`
	ACsDone             int                    `json:"acs_done"`
	ACsTotal            int                    `json:"acs_total"`
	DeliveryReview      *DeliveryReviewSummary `json:"delivery_review,omitempty"`
	PendingHumanActions []PendingHumanAction   `json:"pending_human_actions"`
}

// GateHistoryInput is the input to GateHistory.
type GateHistoryInput struct {
	ChangeRequestID string `json:"change_request_id"`
	Gate            string `json:"gate,omitempty"`
	Limit           int    `json:"limit,omitempty"`
}

// GateRunEntry is one row in GateHistoryResult.
type GateRunEntry struct {
	GateRunID string `json:"gate_run_id"`
	Gate      string `json:"gate"`
	State     string `json:"state"`
	Hint      string `json:"hint"`
	CreatedAt string `json:"created_at"`
}

// GateHistoryResult is the output of GateHistory.
type GateHistoryResult struct {
	ChangeRequestID string         `json:"change_request_id"`
	Runs            []GateRunEntry `json:"runs"`
}

// DeliveryStatusInput is the input to DeliveryStatus.
type DeliveryStatusInput struct {
	ChangeRequestID string `json:"change_request_id"`
	Detail          bool   `json:"detail,omitempty"`
}

// CriterionReview is one criterion row in DeliveryStatusResult.PerCriterion.
type CriterionReview struct {
	CriterionID string `json:"criterion_id,omitempty"`
	Text        string `json:"text,omitempty"`
	Verdict     string `json:"verdict,omitempty"`
	Why         string `json:"why,omitempty"`
}

// CheckResult is one automated-check row in DeliveryStatusResult.Checks.
type CheckResult struct {
	Name   string `json:"name,omitempty"`
	Status string `json:"status,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// DeliveryStatusResult is the output of DeliveryStatus.
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
	Checks          []CheckResult     `json:"checks,omitempty"`
}

// ProvenanceRow is one knowledge document entry in a context pack's
// knowledge_provenance field (per spec §2.1).
type ProvenanceRow struct {
	DocumentID        string `json:"document_id"`
	Title             string `json:"title"`
	Version           string `json:"version"`
	DocumentType      string `json:"document_type"`
	AuthorityLevel    string `json:"authority_level"`
	IsLatest          bool   `json:"is_latest"`
	Freshness         string `json:"freshness"`
	KnowledgeStoreURI string `json:"knowledge_store_uri"`
}

// Warning is one entry in a context pack's warnings field.
type Warning struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	ArtifactID string `json:"artifact_id,omitempty"`
}

// ContextPackInput is the input to Service.ContextPack.
type ContextPackInput struct {
	Kind string `json:"kind"` // "change_request" | "artifact"
	ID   string `json:"id"`
	Lane string `json:"lane,omitempty"` // "" | "fe" | "be"
}

// --- Feedback types ---

// FeedbackEvidence is one piece of evidence backing a feedback event or criterion.
type FeedbackEvidence struct {
	Kind    string `json:"kind"`
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
	FileKey string `json:"file_key,omitempty"`
	Heading string `json:"heading,omitempty"`
	URL     string `json:"url,omitempty"`
	// Source is server-stamped (provenance); agent-supplied values are stripped by ReportFeedback.
	Source string `json:"source,omitempty"`
}

// FeedbackCheck is one automated build-time result reported by a coding agent.
type FeedbackCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	// Source is server-stamped; agent-supplied values are stripped by ReportFeedback.
	Source string `json:"source,omitempty"`
}

// FeedbackCriterion is an agent's per-acceptance-criterion claim.
type FeedbackCriterion struct {
	CriterionID string            `json:"criterion_id,omitempty"`
	Text        string            `json:"text,omitempty"`
	Claim       string            `json:"claim"` // satisfied | partial | not_done
	Evidence    *FeedbackEvidence `json:"evidence,omitempty"`
}

// FeedbackAgent identifies the coding agent that reported the feedback.
type FeedbackAgent struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// ReportFeedbackInput is the service-level input for Service.ReportFeedback.
type ReportFeedbackInput struct {
	ChangeRequestID     string              `json:"change_request_id"`
	ArtifactID          string              `json:"artifact_id,omitempty"`
	EventType           string              `json:"event_type"`
	Severity            string              `json:"severity"`
	Summary             string              `json:"summary"`
	Evidence            []FeedbackEvidence  `json:"evidence,omitempty"`
	SuggestedCorrection string              `json:"suggested_correction,omitempty"`
	AffectedFiles       []string            `json:"affected_files,omitempty"`
	Checks              []FeedbackCheck     `json:"checks,omitempty"`
	Criteria            []FeedbackCriterion `json:"criteria,omitempty"`
	Agent               FeedbackAgent       `json:"agent,omitempty"`
	RunID               string              `json:"run_id,omitempty"`
	DedupeKey           string              `json:"dedupe_key,omitempty"`
}

// ReportFeedbackResult is the output of Service.ReportFeedback.
type ReportFeedbackResult struct {
	FeedbackEventID string `json:"feedback_event_id"`
	Status          string `json:"status"`
	DraftProposal   bool   `json:"draft_proposal"`
}

// ClarificationsInput is the service-level input for Service.Clarifications.
type ClarificationsInput struct {
	ChangeRequestID string `json:"change_request_id"`
	Since           string `json:"since,omitempty"`
}

// ClarificationsResult is the output of Service.Clarifications.
type ClarificationsResult struct {
	ChangeRequestID string              `json:"change_request_id"`
	Found           bool                `json:"found"`
	Clarifications  []ClarificationItem `json:"clarifications"`
}

// ClarificationItem is one answered clarification from a coding-agent blocked-ambiguity event.
type ClarificationItem struct {
	FeedbackEventID string `json:"feedback_event_id"`
	QuestionRef     string `json:"question_ref"`
	QuestionMD      string `json:"question_md"`
	AnswerMD        string `json:"answer_md"`
	Status          string `json:"status"`
	AnsweredAt      string `json:"answered_at"`
}

// --- Artifact publication types ---

// DocumentInput is a single document entry in a publish request.
type DocumentInput struct {
	Path    string `json:"path"`
	Role    string `json:"role,omitempty"`
	Content string `json:"content"`
}

// PublishArtifactInput is the service-level input for Service.PublishArtifact.
type PublishArtifactInput struct {
	FeatureKey     string          `json:"feature_key"`
	FeatureName    string          `json:"feature_name,omitempty"`
	BaseVersion    string          `json:"base_version,omitempty"`
	Documents      []DocumentInput `json:"documents"`
	SourceKind     string          `json:"source_kind,omitempty"`
	SourceRevision string          `json:"source_revision,omitempty"`
	SourceID       string          `json:"source_id,omitempty"`
	ImpactLevel    string          `json:"impact_level,omitempty"`
	RequestType    string          `json:"request_type,omitempty"`
	GatesProfile   string          `json:"gates_profile,omitempty"`
	Authority      string          `json:"authority,omitempty"`
	// RequestedGovernanceLevel is the author's preferred minimum governance tier.
	// It can never lower the recommended level (MaxGovernanceLevel applies).
	RequestedGovernanceLevel string `json:"requested_governance_level,omitempty"`
	// ImpactDeclaration captures the author's self-declared impact signals. It is
	// passed through to ResolveProfile for the built-in policy resolver.
	ImpactDeclaration governanceprofile.ImpactDeclaration `json:"impact_declaration,omitempty"`
}

// PublishArtifactResult is the output of Service.PublishArtifact.
type PublishArtifactResult struct {
	ArtifactID      string   `json:"artifact_id"`
	FeatureKey      string   `json:"feature_key"`
	Version         string   `json:"version"`
	Status          string   `json:"status"`
	ReviewURL       string   `json:"review_url"`
	MissingRoles    []string `json:"missing_roles"`
	ReadinessHint   string   `json:"readiness_hint"`
	ApprovalPolicy  string   `json:"approval_policy,omitempty"`
	EvidencePolicy  string   `json:"evidence_policy,omitempty"`
	WorkType        string   `json:"work_type,omitempty"`
	RiskLevel       string   `json:"risk_level,omitempty"`
	GovernanceLevel string   `json:"governance_level,omitempty"`
	GovernanceWhy   []string `json:"governance_why,omitempty"`
	// PolicyExplanation is a human-readable explanation of the governance decision.
	PolicyExplanation *governanceprofile.Explanation `json:"policy_explanation,omitempty"`
}

// DraftArtifactUpdateInput is the service-level input for Service.DraftArtifactUpdate.
type DraftArtifactUpdateInput struct {
	ArtifactID      string            `json:"artifact_id"`
	ChangeRequestID string            `json:"change_request_id,omitempty"`
	Summary         string            `json:"summary"`
	Files           map[string]string `json:"files"`
	RequestedBy     string            `json:"requested_by,omitempty"`
	DedupeKey       string            `json:"dedupe_key,omitempty"`
}

// DraftArtifactUpdateResult is the output of Service.DraftArtifactUpdate.
type DraftArtifactUpdateResult struct {
	Drafted      bool     `json:"drafted"`
	SessionID    string   `json:"session_id"`
	SourceKind   string   `json:"source_kind,omitempty"`
	SourceID     string   `json:"source_id,omitempty"`
	ChangedFiles []string `json:"changed_files,omitempty"`
	Reason       string   `json:"reason,omitempty"`
}

// --- Agents-backed operations types ---

// ReadinessResult is the output of Service.RunReadiness.
type ReadinessResult struct {
	ArtifactID        string                      `json:"artifact_id"`
	EvaluationsPosted int                         `json:"evaluations_posted,omitempty"`
	ReadinessRuns     []agentsclient.ReadinessRun `json:"readiness_runs"`
	Aggregate         string                      `json:"aggregate"`
	// GovernanceLevel is populated from the artifact's governance snapshot when
	// the snapshot uses specgate.policy/v1 schema. Empty when the snapshot carries
	// no governance level or the artifact reader is not configured.
	GovernanceLevel string `json:"governance_level,omitempty"`
}

// CreateQuickWorkItemInput is the service-level input for Service.CreateQuickWorkItem.
type CreateQuickWorkItemInput struct {
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	IssueURL           string   `json:"issue_url,omitempty"`
	IssueKey           string   `json:"issue_key,omitempty"`
	FeatureKey         string   `json:"feature_key,omitempty"`
	FeatureName        string   `json:"feature_name,omitempty"`
	CreatedBy          string   `json:"created_by,omitempty"`
	WorkspaceID        string   `json:"workspace_id,omitempty"`
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
}

// ContextPackResult is the output of Service.ContextPack.
type ContextPackResult struct {
	State                 string          `json:"state"` // "assembled" | "not_generated"
	Markdown              string          `json:"markdown"`
	KnowledgeProvenance   []ProvenanceRow `json:"knowledge_provenance"`
	Warnings              []Warning       `json:"warnings"`
	Lane                  string          `json:"lane,omitempty"`
	ChangeRequestID       string          `json:"change_request_id,omitempty"`
	FeatureID             string          `json:"feature_id,omitempty"`
	SourceArtifactID      string          `json:"source_artifact_id,omitempty"`
	ContextPackArtifactID string          `json:"context_pack_artifact_id,omitempty"`
	ContextPackURI        string          `json:"context_pack_uri,omitempty"`
	ArtifactID            string          `json:"artifact_id,omitempty"`
	Kind                  string          `json:"kind,omitempty"` // "artifact" for artifact-scoped packs
	// GovernanceLevel is populated from the source artifact's governance snapshot
	// when the snapshot uses specgate.policy/v1 schema. Empty when the snapshot
	// carries no governance level.
	GovernanceLevel string `json:"governance_level,omitempty"`
}
