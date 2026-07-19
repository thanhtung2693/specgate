package workboard

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrNotFound          = errors.New("workboard record not found")
	ErrWorkspaceRequired = errors.New("workspace is required")
	// ErrValidation indicates a caller-supplied payload is invalid (e.g.,
	// missing required field). Mapped to HTTP 400 by the API layer so the
	// agent can surface a useful message rather than a 500.
	ErrValidation = errors.New("workboard validation error")
)

type Store interface {
	CreateFeature(context.Context, Feature) (*Feature, error)
	ListFeatures(context.Context) ([]Feature, error)
	GetFeature(context.Context, string) (*Feature, error)
	GetFeatureByKey(context.Context, string) (*Feature, error)
	UpdateFeature(context.Context, Feature) (*Feature, error)
	SetFeatureCanonicalArtifact(context.Context, string, string, string) (*Feature, error)
	UpsertFeatureByKey(ctx context.Context, key, name string) (*Feature, error)
	CreateChangeRequest(context.Context, ChangeRequest) (*ChangeRequest, error)
	ListChangeRequests(context.Context, bool) ([]ChangeRequest, error)
	GetChangeRequest(context.Context, string) (*ChangeRequest, error)
	UpdateChangeRequest(context.Context, ChangeRequest) (*ChangeRequest, error)
	UnarchiveChangeRequest(ctx context.Context, id string, actor string) (*ChangeRequest, error)
	ListStaleWarnings(context.Context, StaleWarningFilter) ([]StaleWarning, error)
	NextActions(context.Context, string) ([]NextAction, error)
	ListAcceptanceCriteria(context.Context, string) ([]AcceptanceCriterion, error)
	RefreshGateRuns(context.Context, RefreshGateRunsInput) ([]GateRun, error)
	ListGateRuns(context.Context, string, int) ([]GateRun, error)
	// ListLifecycleEvents returns lifecycle events for one entity
	// (entity_kind, entity_id) ordered by created_at ascending. Used by the
	// governance audit trail.
	ListLifecycleEvents(ctx context.Context, entityKind, entityID string, limit int) ([]LifecycleEvent, error)
}

type FeatureStatus string

const (
	FeatureStatusCandidate  FeatureStatus = "candidate"
	FeatureStatusPlanned    FeatureStatus = "planned"
	FeatureStatusActive     FeatureStatus = "active"
	FeatureStatusRejected   FeatureStatus = "rejected"
	FeatureStatusDeprecated FeatureStatus = "deprecated"
	// FeatureStatusArchived is the curator-controlled end state: the feature is
	// retired from default lists and pickers but its record and history remain.
	FeatureStatusArchived FeatureStatus = "archived"
)

type WorkType string

const (
	WorkTypeNewFeature    WorkType = "new_feature"
	WorkTypeFeatureChange WorkType = "feature_change"
	WorkTypeBugFix        WorkType = "bug_fix"
	WorkTypeResearch      WorkType = "research"
	WorkTypeDocumentation WorkType = "documentation"
	WorkTypeCleanup       WorkType = "cleanup"
)

type WarningCode string

const (
	WarningCanonicalArtifactMissing    WarningCode = "canonical_artifact_missing"
	WarningCanonicalArtifactUnapproved WarningCode = "canonical_artifact_unapproved"
	WarningCanonicalArtifactSuperseded WarningCode = "canonical_artifact_superseded"
	WarningCanonicalPromotionAvailable WarningCode = "canonical_promotion_available"
	WarningLeadArtifactSuperseded      WarningCode = "lead_artifact_superseded"
	WarningLinkedKnowledgeNewer        WarningCode = "linked_knowledge_newer"
	WarningFeatureDeprecated           WarningCode = "feature_deprecated"
	// WarningDeliveryInProgress signals that the feature has at least one open
	// MR/PR linked via integration_delivery_links (state = "opened").
	WarningDeliveryInProgress WarningCode = "delivery_in_progress"
	// WarningTrackerStatusConflict signals that the inbound tracker status
	// (Linear issue state.type) contradicts the git delivery evidence for
	// the change request. Tracker status augments — never overrides — the
	// artifact-derived phase, so a contradiction surfaces as a warning.
	WarningTrackerStatusConflict WarningCode = "tracker_status_conflict"
	// WarningTrackerPriorityUrgent signals that the latest tracker event for the
	// change request carries a priority of 1 (urgent) or 2 (high), but the CR has
	// not yet reached delivery review. The combination suggests the CR should be
	// expedited through implementation.
	WarningTrackerPriorityUrgent WarningCode = "tracker_priority_urgent"
	// WarningDeliveryStale signals that the authoritative delivery_review gate run
	// for a change request is still failing after the configured SLA threshold.
	WarningDeliveryStale WarningCode = "delivery_stale"
)

// BoardPhase is the derived work-item board phase. It is computed on read and
// never persisted. The richer read path can distinguish Draft and Review when
// governance-thread / lead-artifact state is available; DerivePhase remains the
// pointer-only fallback for callers that do not hydrate related rows.
type BoardPhase string

const (
	BoardPhaseIntake BoardPhase = "Intake"
	BoardPhaseDraft  BoardPhase = "Draft"
	BoardPhaseReview BoardPhase = "Review"
	BoardPhaseReady  BoardPhase = "Ready"
	// BoardPhaseDelivered is derived when the current completion has an
	// authoritative human delivery approval. It is the not-yet-archived
	// intermediate truth between review and archive; auto-archive stays
	// optional. Only the DB-backed read path can derive it (it needs gate_runs),
	// so the pointer-only DerivePhase fallback never returns it.
	BoardPhaseDelivered BoardPhase = "Delivered"
)

type AcceptanceCriterionSource string

const (
	AcceptanceCriterionSourceLLM   AcceptanceCriterionSource = "llm"
	AcceptanceCriterionSourceHuman AcceptanceCriterionSource = "human"
)

type NextActionState string

const (
	NextActionStatePass             NextActionState = "pass"
	NextActionStateWarn             NextActionState = "warn"
	NextActionStatePending          NextActionState = "pending"
	NextActionStateFail             NextActionState = "fail"
	NextActionStateNeedsHumanReview NextActionState = "needs_human_review"
	NextActionStateNotApplicable    NextActionState = "not_applicable"
)

type GateEvaluation struct {
	Gate             string          `json:"gate"`
	State            NextActionState `json:"state"`
	Hint             string          `json:"hint,omitempty"`
	Confidence       float64         `json:"confidence,omitempty"`
	JudgeModel       string          `json:"judge_model,omitempty"`
	EvalSuiteVersion string          `json:"eval_suite_version,omitempty"`
	// Evidence is a short quote or section the judge cited for its verdict.
	// Persisted in the gate run's evidence_json for audit; model-judged gates only.
	Evidence string `json:"evidence,omitempty"`
}

type RefreshGateRunsInput struct {
	ChangeRequestID string           `json:"change_request_id"`
	Evaluations     []GateEvaluation `json:"evaluations,omitempty"`
	// EvaluationsOnly persists only the supplied evaluation gates. Delivery
	// review uses this after the quality-gate refresh so one submission does
	// not append a duplicate deterministic snapshot.
	EvaluationsOnly bool `json:"evaluations_only,omitempty"`
}

type Feature struct {
	ID                  string        `gorm:"column:id;primaryKey" json:"id,omitempty"`
	WorkspaceID         string        `gorm:"column:workspace_id;index:idx_features_workspace;uniqueIndex:uq_features_workspace_key,priority:1" json:"workspace_id,omitempty"`
	Key                 string        `gorm:"column:key;not null;uniqueIndex:uq_features_workspace_key,priority:2" json:"key,omitempty"`
	Name                string        `gorm:"column:name;not null" json:"name,omitempty"`
	Summary             string        `gorm:"column:summary" json:"summary,omitempty"`
	Status              FeatureStatus `gorm:"column:status;not null;index:idx_features_status" json:"status,omitempty"`
	Version             int           `gorm:"column:version;not null" json:"version,omitempty"`
	CanonicalArtifactID string        `gorm:"column:canonical_artifact_id" json:"canonical_artifact_id,omitempty"`
	SourceDocumentIDs   string        `gorm:"column:source_document_ids_json;not null;default:'[]'" json:"source_document_ids_json,omitempty"`
	SourceArtifactIDs   string        `gorm:"column:source_artifact_ids_json;not null;default:'[]'" json:"source_artifact_ids_json,omitempty"`
	CreatedAt           time.Time     `gorm:"column:created_at;not null" json:"created_at,omitempty"`
	UpdatedAt           time.Time     `gorm:"column:updated_at;not null" json:"updated_at,omitempty"`
}

func (Feature) TableName() string { return "features" }

type ChangeRequest struct {
	ID                 string     `gorm:"column:id;primaryKey" json:"id,omitempty"`
	Key                string     `gorm:"column:key;not null;uniqueIndex:uq_change_requests_workspace_key,priority:2" json:"key,omitempty"`
	FeatureID          string     `gorm:"column:feature_id;index:idx_change_requests_feature" json:"feature_id,omitempty"`
	WorkspaceID        string     `gorm:"column:workspace_id;index:idx_change_requests_workspace;uniqueIndex:uq_change_requests_workspace_key,priority:1" json:"workspace_id,omitempty"`
	WorkType           WorkType   `gorm:"column:work_type;not null;index:idx_change_requests_work_type" json:"work_type,omitempty"`
	Title              string     `gorm:"column:title;not null" json:"title,omitempty"`
	IntentMD           string     `gorm:"column:intent_md" json:"intent_md,omitempty"`
	AcceptanceCriteria string     `gorm:"column:acceptance_criteria_json;not null;default:'[]'" json:"acceptance_criteria_json,omitempty"`
	NonGoals           string     `gorm:"column:non_goals_json;not null;default:'[]'" json:"non_goals_json,omitempty"`
	OpenQuestions      string     `gorm:"column:open_questions_json;not null;default:'[]'" json:"open_questions_json,omitempty"`
	SourceRefs         string     `gorm:"column:source_refs_json;not null;default:'[]'" json:"source_refs_json,omitempty"`
	LeadArtifactID     string     `gorm:"column:lead_artifact_id" json:"lead_artifact_id,omitempty"`
	GovernanceThreadID string     `gorm:"column:governance_thread_id" json:"governance_thread_id,omitempty"`
	Archived           bool       `gorm:"column:archived;not null;default:false" json:"archived,omitempty"`
	ArchivedAt         *time.Time `gorm:"column:archived_at" json:"archived_at,omitempty"`
	ArchivedBy         string     `gorm:"column:archived_by" json:"archived_by,omitempty"`
	ArchiveReason      string     `gorm:"column:archive_reason" json:"archive_reason,omitempty"`
	CreatedBy          string     `gorm:"column:created_by" json:"created_by,omitempty"`
	CreatedAt          time.Time  `gorm:"column:created_at;not null" json:"created_at,omitempty"`
	UpdatedAt          time.Time  `gorm:"column:updated_at;not null" json:"updated_at,omitempty"`
	// Phase is derived from the artifact pointers on read and never persisted
	// (gorm:"-"). See BoardPhase.
	Phase BoardPhase `gorm:"-" json:"phase,omitempty"`
	// TrackerStatus is the raw state.type of the most recent inbound tracker
	// (delivery.tracker_status_changed) feedback event correlated to this CR,
	// or empty when none. Derived on read; never persisted.
	TrackerStatus string `gorm:"-" json:"tracker_status,omitempty"`
	// DeliveryReview is the authoritative delivery_review gate-run snapshot.
	// Derived on read; never persisted.
	DeliveryReview *DeliveryReviewSnapshot `gorm:"-" json:"delivery_review,omitempty"`
}

func (ChangeRequest) TableName() string { return "change_requests" }

type DeliveryReviewSnapshot struct {
	Verdict    string    `json:"verdict"`
	Hint       string    `json:"hint,omitempty"`
	ReviewedAt time.Time `json:"reviewed_at,omitempty"`
	Executor   string    `json:"executor,omitempty"`
	Actor      string    `json:"actor,omitempty"`
	Note       string    `json:"note,omitempty"`
	Summary    string    `json:"summary,omitempty"`
}

// DerivePhase computes the fallback board phase. A quick-route bug fix is ready
// from its persisted intent and acceptance criteria; full-route work needs an
// artifact. A richer read path may upgrade full-route work to Draft or Review
// when governance-thread or lead-artifact status is available.
func (cr ChangeRequest) DerivePhase() BoardPhase {
	if cr.IsQuickRoute() {
		return BoardPhaseReady
	}
	if cr.LeadArtifactID != "" {
		return BoardPhaseReady
	}
	return BoardPhaseIntake
}

// IsQuickRoute reports whether the change request is quick-route work: a
// bug-fix without a lead artifact. It may still link a Feature for product
// context. Quick-route items never grow a working spec, so the
// full-artifact-flow gates do not apply to them (see NextActions).
func (cr ChangeRequest) IsQuickRoute() bool {
	return cr.LeadArtifactID == "" && cr.WorkType == WorkTypeBugFix
}

type AcceptanceCriterion struct {
	ID              string                    `gorm:"column:id;primaryKey" json:"id"`
	ChangeRequestID string                    `gorm:"column:change_request_id;not null;index:idx_acceptance_criteria_cr" json:"change_request_id"`
	Text            string                    `gorm:"column:text;not null" json:"text"`
	Done            bool                      `gorm:"column:done;not null" json:"done"`
	Source          AcceptanceCriterionSource `gorm:"column:source;not null" json:"source"`
	SortOrder       int                       `gorm:"column:sort_order;not null" json:"sort_order"`
	// VerificationBinding names a check in the delivery report's checks[] that
	// deterministically backs this criterion's delivery-review verdict. Empty ⇒
	// the criterion is judged by the LLM / agent claim.
	VerificationBinding string    `gorm:"column:verification_binding;not null;default:''" json:"verification_binding,omitempty"`
	CreatedAt           time.Time `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt           time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (AcceptanceCriterion) TableName() string { return "acceptance_criteria" }

type LifecycleEvent struct {
	ID          string    `gorm:"column:id;primaryKey" json:"id"`
	WorkspaceID string    `gorm:"column:workspace_id;not null" json:"workspace_id,omitempty"`
	EntityKind  string    `gorm:"column:entity_kind;not null" json:"entity_kind"`
	EntityID    string    `gorm:"column:entity_id;not null" json:"entity_id"`
	EventType   string    `gorm:"column:event_type;not null" json:"event_type"`
	Actor       string    `gorm:"column:actor" json:"actor,omitempty"`
	PayloadJSON string    `gorm:"column:payload_json;not null;default:'{}'" json:"payload_json"`
	CreatedAt   time.Time `gorm:"column:created_at;not null" json:"created_at"`
}

func (LifecycleEvent) TableName() string { return "workboard_lifecycle_events" }

type NextAction struct {
	Gate  string          `json:"gate"`
	State NextActionState `json:"state"`
	Hint  string          `json:"hint"`
}

// GateRun subject kinds: what entity a run evaluates (per spec §3.2 gate_runs).
const (
	GateRunSubjectChangeRequest = "change_request"
	GateRunSubjectArtifact      = "artifact"
)

// GateRun executors: who produced the verdict. Platform-side evaluations
// (deterministic next-actions, platform model judges, artifact readiness) use
// "platform"; IDE-agent gate-task results carry the submitting executor
// (e.g. "ide_agent", matching policy.Executor values).
const (
	GateRunExecutorPlatform = "platform"
	GateRunExecutorIDEAgent = "ide_agent"
	GateRunExecutorHuman    = "human"
)

type DeliveryDecision string

const (
	DeliveryDecisionApprove DeliveryDecision = "approve"
	DeliveryDecisionReject  DeliveryDecision = "reject"
)

type DeliveryDecisionInput struct {
	ChangeRequestID           string           `json:"change_request_id"`
	ReviewedGateRunID         string           `json:"reviewed_gate_run_id"`
	CompletionFeedbackEventID string           `json:"completion_feedback_event_id"`
	Decision                  DeliveryDecision `json:"decision"`
	Actor                     string           `json:"actor"`
	Note                      string           `json:"note,omitempty"`
}

// GateRun is one persisted gate evaluation snapshot in the unified gate_runs
// table (per spec §3.2). SubjectKind scopes the run to a change request
// (workboard gates, delivery_review) or an artifact (readiness gates,
// IDE-agent gate-task results). SubjectID serializes as change_request_id to
// keep the workboard wire contract; artifact-scoped rows are never returned
// by the workboard endpoints.
type GateRun struct {
	ID           string          `gorm:"column:id;primaryKey" json:"id"`
	WorkspaceID  string          `gorm:"column:workspace_id;index:idx_gate_runs_workspace_subject" json:"workspace_id,omitempty"`
	SubjectKind  string          `gorm:"column:subject_kind;not null;index:idx_gate_runs_subject_created" json:"-"`
	SubjectID    string          `gorm:"column:subject_id;not null;index:idx_gate_runs_subject_created" json:"change_request_id"`
	Gate         string          `gorm:"column:gate;not null" json:"gate"`
	State        NextActionState `gorm:"column:state;not null" json:"state"`
	Hint         string          `gorm:"column:hint;not null" json:"hint"`
	Executor     string          `gorm:"column:executor;not null" json:"executor"`
	EvidenceJSON string          `gorm:"column:evidence_json;not null;default:'{}'" json:"evidence_json,omitempty"`
	// CompletionFeedbackEventID is the indexed delivery-cycle binding. It
	// duplicates the audit value in EvidenceJSON so authority queries do not
	// depend on JSON-dialect behavior or unbounded history scans.
	CompletionFeedbackEventID string    `gorm:"column:completion_feedback_event_id;not null;default:'';index:idx_gate_runs_delivery_cycle" json:"-"`
	CreatedAt                 time.Time `gorm:"column:created_at;not null;index:idx_gate_runs_subject_created,sort:desc" json:"created_at"`
}

func (GateRun) TableName() string { return "gate_runs" }

// DeliveryDecisionSummary renders the one-line human-decision summary for a
// delivery_review gate run. Single-sourced so the list (storage/db) and detail
// (governanceops) read paths cannot drift. actor/note are trimmed here, so
// callers may pass raw values; returns "" for non-human runs.
func DeliveryDecisionSummary(run GateRun, actor, note string) string {
	if run.Executor != GateRunExecutorHuman {
		return ""
	}
	summary := "delivery rejected"
	if run.State == NextActionStatePass {
		summary = "delivery accepted"
	}
	if actor = strings.TrimSpace(actor); actor != "" {
		summary += " by " + actor
	}
	if note = strings.TrimSpace(note); note != "" {
		summary += ": " + note
	}
	return summary
}

type StaleWarning struct {
	Code            WarningCode `json:"code"`
	Severity        string      `json:"severity"`
	Message         string      `json:"message"`
	FeatureID       string      `json:"feature_id,omitempty"`
	ChangeRequestID string      `json:"change_request_id,omitempty"`
	ArtifactID      string      `json:"artifact_id,omitempty"`
}

type StaleWarningFilter struct {
	FeatureID       string
	ChangeRequestID string
	WorkspaceID     string
}

// StatsGateRun is a gate run joined with its change request, used by the
// governance stats projection. The join supplies
// the CR key for ledger display and the CR creation time for cycle-time math.
type StatsGateRun struct {
	RunID            string          `gorm:"column:run_id" json:"run_id"`
	ChangeRequestID  string          `gorm:"column:change_request_id" json:"change_request_id"`
	ChangeRequestKey string          `gorm:"column:change_request_key" json:"change_request_key"`
	Gate             string          `gorm:"column:gate" json:"gate"`
	State            NextActionState `gorm:"column:state" json:"state"`
	Hint             string          `gorm:"column:hint" json:"hint"`
	RunCreatedAt     time.Time       `gorm:"column:run_created_at" json:"run_created_at"`
	CRCreatedAt      time.Time       `gorm:"column:cr_created_at" json:"cr_created_at"`
}

// StatsFeedbackEvent is a governance feedback event joined with its change
// request, used by the governance stats projection.
type StatsFeedbackEvent struct {
	EventID          string    `gorm:"column:event_id" json:"event_id"`
	ChangeRequestID  string    `gorm:"column:change_request_id" json:"change_request_id"`
	ChangeRequestKey string    `gorm:"column:change_request_key" json:"change_request_key"`
	Detail           string    `gorm:"column:detail" json:"detail"`
	CreatedAt        time.Time `gorm:"column:created_at" json:"created_at"`
}
