package artifact

import (
	"strings"
	"time"
)

type Status string

const (
	StatusDraft        Status = "draft"
	StatusNeedsChanges Status = "needs_changes"
	StatusApproved     Status = "approved"
	StatusSuperseded   Status = "superseded"
)

type RequestType string

const (
	RequestTypeNewFeature    RequestType = "new_feature"
	RequestTypeChangeRequest RequestType = "change_request"
	RequestTypeBugfix        RequestType = "bugfix"
	RequestTypeUnknown       RequestType = "unknown"
)

type ImpactLevel string

const (
	ImpactLevelLow    ImpactLevel = "low"
	ImpactLevelMedium ImpactLevel = "medium"
	ImpactLevelHigh   ImpactLevel = "high"
)

type ArtifactPhase string

const (
	ArtifactPhasePhase1 ArtifactPhase = "phase1"
	ArtifactPhasePhase2 ArtifactPhase = "phase2"
)

type ArtifactCompleteness string

const (
	ArtifactCompletenessPartial ArtifactCompleteness = "partial"
	ArtifactCompletenessFull    ArtifactCompleteness = "full"
)

// Role classifies the intent/audience of a document within an artifact package.
type Role string

const (
	RoleSpec         Role = "spec"
	RoleDesign       Role = "design"
	RolePlan         Role = "plan"
	RoleVerification Role = "verification"
	RoleResearch     Role = "research"
	RoleReference    Role = "reference"
	RoleUnspecified  Role = "unspecified"
)

var knownRoles = map[Role]bool{
	RoleSpec:         true,
	RoleDesign:       true,
	RolePlan:         true,
	RoleVerification: true,
	RoleResearch:     true,
	RoleReference:    true,
	RoleUnspecified:  true,
}

// NormalizeRole maps a raw role string to a canonical Role. Unknown strings
// are mapped to RoleUnspecified; custom:* passthrough is allowed if non-empty
// after the prefix.
func NormalizeRole(s string) Role {
	r := Role(strings.ToLower(strings.TrimSpace(s)))
	if r == "" {
		return RoleUnspecified
	}
	if strings.HasPrefix(string(r), "custom:") && len(r) > len("custom:") {
		return r
	}
	if knownRoles[r] {
		return r
	}
	return RoleUnspecified
}

// Artifact maps to the `artifacts` table (spec §3.2).
type Artifact struct {
	ID                   string               `gorm:"column:id;primaryKey"`
	WorkspaceID          string               `gorm:"column:workspace_id;index:idx_artifacts_workspace;uniqueIndex:uq_artifacts_workspace_feature_version,priority:1"`
	FeatureID            string               `gorm:"column:feature_id;not null;index:idx_artifacts_feature_id;uniqueIndex:uq_artifacts_workspace_feature_version,priority:2"`
	Version              string               `gorm:"column:version;not null;uniqueIndex:uq_artifacts_workspace_feature_version,priority:3"`
	Status               Status               `gorm:"column:status;not null;index:idx_artifacts_status"`
	RequestType          RequestType          `gorm:"column:request_type;not null"`
	ImpactLevel          ImpactLevel          `gorm:"column:impact_level;not null"`
	ArtifactPhase        ArtifactPhase        `gorm:"column:artifact_phase;not null;default:phase1"`
	ArtifactCompleteness ArtifactCompleteness `gorm:"column:artifact_completeness;not null;default:partial"`
	ConfidenceScore      *float64             `gorm:"column:confidence_score"`
	AmbiguityScore       *float64             `gorm:"column:ambiguity_score"`
	GovernanceVersion    string               `gorm:"column:governance_version"`
	CreatedBy            string               `gorm:"column:created_by;not null"`
	ApprovedBy           string               `gorm:"column:approved_by"`
	ApprovedAt           *time.Time           `gorm:"column:approved_at"`
	CreatedAt            time.Time            `gorm:"column:created_at;not null"`
	UpdatedAt            time.Time            `gorm:"column:updated_at;not null"`

	// Governance envelope: provenance/lineage of the published package.
	SourceKind     string `gorm:"column:source_kind"`
	SourceID       string `gorm:"column:source_id"`
	SourceRevision string `gorm:"column:source_revision"`
	SnapshotDigest string `gorm:"column:snapshot_digest"`
	Authority      string `gorm:"column:authority"`
	// SP1 resolved policy snapshot: immutable governance contract for later consumers.
	PolicyVersion      string `gorm:"column:policy_version"`
	PolicyDigest       string `gorm:"column:policy_digest"`
	PolicySnapshotJSON string `gorm:"column:policy_snapshot_json"`

	// SP3 lineage: parent artifact in the version chain and the root of the chain.
	ParentArtifactID string `gorm:"column:parent_artifact_id"`
	LineageRootID    string `gorm:"column:lineage_root_id"`

	Services []ServiceRef `gorm:"foreignKey:ArtifactID;references:ID;constraint:OnDelete:CASCADE"`
	Files    []File       `gorm:"foreignKey:ArtifactID;references:ID;constraint:OnDelete:CASCADE"`
}

func (Artifact) TableName() string { return "artifacts" }

// ServiceRef maps to `artifact_services` (normalized for conflict queries).
type ServiceRef struct {
	ArtifactID string `gorm:"column:artifact_id;primaryKey"`
	Name       string `gorm:"column:name;primaryKey;index:idx_artifact_services_name"`
	Kind       string `gorm:"column:kind;primaryKey"` // "service" | "app"
}

func (ServiceRef) TableName() string { return "artifact_services" }

// File maps to `artifact_files`.
type File struct {
	ArtifactID    string `gorm:"column:artifact_id;primaryKey"`
	Path          string `gorm:"column:path;primaryKey"`
	Role          Role   `gorm:"column:role;not null;default:unspecified"`
	ObjectKey     string `gorm:"column:object_key;not null"`
	SizeBytes     int64  `gorm:"column:size_bytes;not null"`
	ContentSHA256 string `gorm:"column:content_sha256;not null"`
}

func (File) TableName() string { return "artifact_files" }

// Event maps to `artifact_events` (append-only log — spec §3.2, §8).
type Event struct {
	ID         string    `gorm:"column:id;primaryKey"`
	ArtifactID string    `gorm:"column:artifact_id;not null"`
	EventType  string    `gorm:"column:event_type;not null;index:idx_artifact_events_type"`
	Payload    string    `gorm:"column:payload;not null"` // JSON blob
	CreatedAt  time.Time `gorm:"column:created_at;not null;index:idx_artifact_events_created_at"`
	// Tamper-evidence chain (spec §8): hash commits to this row and PrevHash to
	// the artifact's prior event. Empty only for an artifact's first event.
	PrevHash string `gorm:"column:prev_hash" json:"prev_hash,omitempty"`
	Hash     string `gorm:"column:hash" json:"hash,omitempty"`
}

func (Event) TableName() string { return "artifact_events" }

// ArtifactEventType constants for well-known event types in the artifact event log.
// These are used as EventType values in artifact.Event rows. New types must also
// be registered in the EventDTO enum in internal/api/schemas_artifacts.go and
// in docs/spec.md §8.
const (
	EventPublished    = "artifact.published"
	EventApproved     = "artifact.approved"
	EventNeedsChanges = "artifact.needs_changes"
	EventSuperseded   = "artifact.superseded"
)

// EventTypeForStatus maps a status transition to its event type (spec §8).
// An empty result means the status is not a supported transition. Draft is
// publish-time state, recorded by the separate artifact.published event.
func EventTypeForStatus(status Status) string {
	switch status {
	case StatusApproved:
		return EventApproved
	case StatusNeedsChanges:
		return EventNeedsChanges
	case StatusSuperseded:
		return EventSuperseded
	default:
		return ""
	}
}

type ReadinessState string

const (
	ReadinessStatePass             ReadinessState = "pass"
	ReadinessStateWarn             ReadinessState = "warn"
	ReadinessStateFail             ReadinessState = "fail"
	ReadinessStateNeedsHumanReview ReadinessState = "needs_human_review"
	ReadinessStateNotApplicable    ReadinessState = "not_applicable"
	ReadinessStateNotRun           ReadinessState = "not_run"
)

type ReadinessEvaluation struct {
	Gate             string
	State            ReadinessState
	Hint             string
	Confidence       float64
	JudgeModel       string
	EvalSuiteVersion string
	Evidence         string
}

// ReadinessRun is the artifact-scoped readiness view of a gate run. It is a
// plain domain/DTO struct — persistence goes through the unified gate_runs
// table (subject_kind = "artifact"; see internal/workboard.GateRun and the
// storage repository conversion, per spec §3.2).
type ReadinessRun struct {
	ID         string         `json:"id"`
	ArtifactID string         `json:"artifact_id"`
	Gate       string         `json:"gate"`
	State      ReadinessState `json:"state"`
	Hint       string         `json:"hint"`
	// Executor records who evaluated the run: "platform" or "ide_agent"
	// (agent-attested). First-class so history can show trust origin.
	Executor     string    `json:"executor,omitempty"`
	EvidenceJSON string    `json:"evidence_json,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}
