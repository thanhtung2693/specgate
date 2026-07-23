package governanceops

import (
	"github.com/specgate/doc-registry/internal/agentsclient"
	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/governanceprofile"
)

// ResolveWorkRefInput is the input to the unified work-reference resolver.
// Ref accepts a change-request ID, a change-request Key, a full HTTPS issue
// URL matched against a stored tracker link, or a bare tracker key (Provider
// required). Provider is an optional exact validation hint for full URLs.
type ResolveWorkRefInput struct {
	Ref      string `json:"ref"`
	Provider string `json:"provider,omitempty"`
}

// ResolvedWork is the output of ResolveWorkRef.
type ResolvedWork struct {
	ChangeRequestID  string `json:"change_request_id"`
	ChangeRequestKey string `json:"change_request_key"`
	FeatureID        string `json:"feature_id"`
	Title            string `json:"title"`
	Phase            string `json:"phase"`
	IssueKey         string `json:"issue_key,omitempty"`
	IssueURL         string `json:"issue_url,omitempty"`
}

// AuditEvent is one entry in a governance AuditTrail — a single dated action in
// the feature's governance history ("git log for governance").
type AuditEvent struct {
	Timestamp string `json:"timestamp"`  // RFC3339
	Actor     string `json:"actor"`      // who acted (may be empty)
	ActorKind string `json:"actor_kind"` // human | agent | platform | ""
	Action    string `json:"action"`     // e.g. published, needs_changes, gate:<name>, delivery_review, <lifecycle event type>
	Subject   string `json:"subject"`    // what the action was about (artifact id, CR key, ...)
	Verdict   string `json:"verdict"`    // gate/review state (pass, fail, ...), empty for status/lifecycle events
	Trust     string `json:"trust"`      // human | agent_attested | platform | "" (derived from executor)
	Detail    string `json:"detail"`     // free-form hint/note
}

// AuditTrail is the full chronological governance history for a work reference.
// Events are sorted by Timestamp ascending.
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
	// Chain is the tamper-evidence verification report, present only when the
	// caller requested verification (audit --verify, per doc-registry spec §8).
	Chain *artifact.ChainReport `json:"chain,omitempty"`
}

// GovernanceStatusCounts is the phase breakdown in GovernanceStatusResult.
type GovernanceStatusCounts struct {
	Intake    int `json:"intake"`
	Review    int `json:"review"`
	Ready     int `json:"ready"`
	Delivered int `json:"delivered"`
	Total     int `json:"total"`
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
	Executor   string `json:"executor,omitempty"`
	Actor      string `json:"actor,omitempty"`
	Note       string `json:"note,omitempty"`
	Summary    string `json:"summary,omitempty"`
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
	CriterionID         string `json:"criterion_id,omitempty"`
	Text                string `json:"text,omitempty"`
	Verdict             string `json:"verdict,omitempty"`
	Why                 string `json:"why,omitempty"`
	VerificationBinding string `json:"verification_binding,omitempty"`
	TrustTier           string `json:"trust_tier,omitempty"`
}

// CheckResult is one automated-check row in DeliveryStatusResult.Checks.
type CheckResult struct {
	Name   string `json:"name,omitempty"`
	Status string `json:"status,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// PeerReviewState is an informational record of independent evidence review;
// it never authorizes delivery in place of a human decision.
type PeerReviewState struct {
	State      string `json:"state"`
	AgentName  string `json:"agent_name,omitempty"`
	ReviewedAt string `json:"reviewed_at,omitempty"`
}

// DeliveryStatusResult is the output of DeliveryStatus.
type DeliveryStatusResult struct {
	ChangeRequestID           string            `json:"change_request_id"`
	GateRunID                 string            `json:"gate_run_id,omitempty"`
	CompletionFeedbackEventID string            `json:"completion_feedback_event_id,omitempty"`
	Found                     bool              `json:"found"`
	Verdict                   string            `json:"verdict,omitempty"`
	EvidenceVerdict           string            `json:"evidence_verdict,omitempty"`
	ReasonCode                string            `json:"reason_code,omitempty"`
	Hint                      string            `json:"hint,omitempty"`
	Confidence                *float64          `json:"confidence,omitempty"`
	JudgeModel                string            `json:"judge_model,omitempty"`
	EvalSuite                 string            `json:"eval_suite_version,omitempty"`
	ReviewedAt                string            `json:"reviewed_at,omitempty"`
	Executor                  string            `json:"executor,omitempty"`
	Actor                     string            `json:"actor,omitempty"`
	Note                      string            `json:"note,omitempty"`
	Summary                   string            `json:"summary,omitempty"`
	OutstandingMD             string            `json:"outstanding_md,omitempty"`
	AssuranceSources          []string          `json:"assurance_sources,omitempty" doc:"Server-observed repository corroboration from a merged PR/MR."`
	PerCriterion              []CriterionReview `json:"per_criterion,omitempty"`
	Checks                    []CheckResult     `json:"checks,omitempty"`
	GitReceipt                *GitReceipt       `json:"git_receipt,omitempty"`
	PeerReview                PeerReviewState   `json:"peer_review,omitempty"`
}

type DeliveryDecisionInput struct {
	ChangeRequestID           string `json:"change_request_id"`
	Decision                  string `json:"decision"`
	Actor                     string `json:"actor"`
	Note                      string `json:"note,omitempty"`
	ReviewedGateRunID         string `json:"reviewed_gate_run_id,omitempty"`
	CompletionFeedbackEventID string `json:"completion_feedback_event_id,omitempty"`
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
}

// --- Feedback types ---

// GitReceipt binds completion evidence to local repository metadata without
// uploading source contents or a patch.
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

// FeedbackEvidence is one piece of evidence backing a feedback event or criterion.
type FeedbackEvidence struct {
	Kind      string             `json:"kind"`
	Path      string             `json:"path,omitempty"`
	Line      int                `json:"line,omitempty"`
	FileKey   string             `json:"file_key,omitempty"`
	Heading   string             `json:"heading,omitempty"`
	URL       string             `json:"url,omitempty"`
	Grounding *FeedbackGrounding `json:"grounding,omitempty"`
	// Source is server-stamped (provenance); agent-supplied values are stripped by ReportFeedback.
	Source string `json:"source,omitempty"`
}

// FeedbackGrounding is a local-checkout evidence excerpt captured by the CLI
// before a completion report leaves the developer machine.
type FeedbackGrounding struct {
	Status  string `json:"status,omitempty"`
	Excerpt string `json:"excerpt,omitempty"`
	Digest  string `json:"digest,omitempty"`
}

// FeedbackCheck is one automated build-time result reported by a coding agent.
type FeedbackCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Command string `json:"command,omitempty"`
	Detail  string `json:"detail,omitempty"`
	// Source is server-stamped; agent-supplied values are stripped by ReportFeedback.
	Source string `json:"source,omitempty"`
}

// FeedbackCriterion is an agent's per-acceptance-criterion claim.
type FeedbackCriterion struct {
	CriterionID         string            `json:"criterion_id,omitempty"`
	Text                string            `json:"text,omitempty"`
	Claim               string            `json:"claim"` // satisfied | partial | not_done
	VerificationBinding string            `json:"verification_binding,omitempty"`
	Evidence            *FeedbackEvidence `json:"evidence,omitempty"`
}

// FeedbackAgent identifies the coding agent that reported the feedback.
type FeedbackAgent struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// PeerReviewBinding ties a peer review to the exact completion receipt it reviewed.
// It prevents a review of an older checkout from blessing a later completion.
type PeerReviewBinding struct {
	CompletionFeedbackEventID string      `json:"completion_feedback_event_id"`
	GitReceipt                *GitReceipt `json:"git_receipt"`
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
	GitReceipt          *GitReceipt         `json:"git_receipt,omitempty"`
	PeerReviewOf        *PeerReviewBinding  `json:"peer_review_of,omitempty"`
}

// ReportFeedbackResult is the output of Service.ReportFeedback.
type ReportFeedbackResult struct {
	FeedbackEventID string `json:"feedback_event_id"`
	Status          string `json:"status"`
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
	WorkspaceID    string          `json:"workspace_id,omitempty"`
	BaseVersion    string          `json:"base_version,omitempty"`
	Documents      []DocumentInput `json:"documents"`
	SourceKind     string          `json:"source_kind,omitempty"`
	SourceRevision string          `json:"source_revision,omitempty"`
	SourceID       string          `json:"source_id,omitempty"`
	CreatedBy      string          `json:"created_by,omitempty"`
	ImpactLevel    string          `json:"impact_level,omitempty"`
	RequestType    string          `json:"request_type,omitempty"`
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

// --- Agents-backed operations types ---

// ReadinessResult is the output of Service.RunReadiness.
type ReadinessResult struct {
	ArtifactID           string                                `json:"artifact_id"`
	EvaluationsPosted    int                                   `json:"evaluations_posted,omitempty"`
	ReadinessRuns        []agentsclient.ReadinessRun           `json:"readiness_runs"`
	DispatchedToIDEAgent *agentsclient.GateTaskDispatchReceipt `json:"dispatched_to_ide_agent,omitempty"`
	Aggregate            string                                `json:"aggregate"`
	// GovernanceLevel is populated from the artifact's governance snapshot when
	// the snapshot uses specgate.policy/v1 schema. Empty when the snapshot carries
	// no governance level or the artifact reader is not configured.
	GovernanceLevel string `json:"governance_level,omitempty"`
}

// CreateQuickWorkItemInput is the service-level input for Service.CreateQuickWorkItem.
type CreateQuickWorkItemInput struct {
	Title              string                     `json:"title"`
	Description        string                     `json:"description"`
	IssueURL           string                     `json:"issue_url,omitempty"`
	IssueKey           string                     `json:"issue_key,omitempty"`
	FeatureKey         string                     `json:"feature_key,omitempty"`
	FeatureName        string                     `json:"feature_name,omitempty"`
	CreatedBy          string                     `json:"created_by,omitempty"`
	WorkspaceID        string                     `json:"workspace_id,omitempty"`
	AcceptanceCriteria []AcceptanceCriterionInput `json:"acceptance_criteria,omitempty"`
}

type AcceptanceCriterionInput = agentsclient.AcceptanceCriterionInput

// ContextPackResult is the output of Service.ContextPack.
type ContextPackResult struct {
	State               string          `json:"state"` // "assembled" | "not_generated"
	Markdown            string          `json:"markdown"`
	KnowledgeProvenance []ProvenanceRow `json:"knowledge_provenance"`
	Warnings            []Warning       `json:"warnings"`
	ChangeRequestID     string          `json:"change_request_id,omitempty"`
	FeatureID           string          `json:"feature_id,omitempty"`
	SourceArtifactID    string          `json:"source_artifact_id,omitempty"`
	ArtifactID          string          `json:"artifact_id,omitempty"`
	Kind                string          `json:"kind,omitempty"` // "artifact" for artifact-scoped packs
	// GovernanceLevel is populated from the source artifact's governance snapshot
	// when the snapshot uses specgate.policy/v1 schema. Empty when the snapshot
	// carries no governance level.
	GovernanceLevel string `json:"governance_level,omitempty"`
}
