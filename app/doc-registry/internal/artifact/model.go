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
	FeatureID            string               `gorm:"column:feature_id;not null;index:idx_artifacts_feature_id;uniqueIndex:uq_feature_version,priority:1"`
	Version              string               `gorm:"column:version;not null;uniqueIndex:uq_feature_version,priority:2"`
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
	Authority      string `gorm:"column:authority"`
	GatesProfile   string `gorm:"column:gates_profile"`
	// SP1 resolved profile snapshot: immutable governance contract for later consumers.
	GatesProfileVersion      string `gorm:"column:gates_profile_version"`
	GatesProfileDigest       string `gorm:"column:gates_profile_digest"`
	GatesProfileSnapshotJSON string `gorm:"column:gates_profile_snapshot_json"`

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
	ArtifactID string `gorm:"column:artifact_id;primaryKey"`
	Path       string `gorm:"column:path;primaryKey"`
	Role       Role   `gorm:"column:role;not null;default:unspecified"`
	S3Path     string `gorm:"column:s3_path;not null"`
	SizeBytes  int64  `gorm:"column:size_bytes;not null"`
}

func (File) TableName() string { return "artifact_files" }

// Event maps to `artifact_events` (append-only log — spec §3.2, §8).
type Event struct {
	ID         string    `gorm:"column:id;primaryKey"`
	ArtifactID string    `gorm:"column:artifact_id;not null"`
	EventType  string    `gorm:"column:event_type;not null;index:idx_artifact_events_type"`
	Payload    string    `gorm:"column:payload;not null"` // JSON blob
	CreatedAt  time.Time `gorm:"column:created_at;not null;index:idx_artifact_events_created_at"`
}

func (Event) TableName() string { return "artifact_events" }

// ArtifactEventType constants for well-known event types in the artifact event log.
// These are used as EventType values in artifact.Event rows. New types must also
// be registered in the EventDTO enum in internal/api/schemas_artifacts.go and
// in docs/spec.md §8.
const (
	EventPublished    = "artifact.published"
	EventNeedsChanges = "artifact.needs_changes"
	EventSuperseded   = "artifact.superseded"
)

// EventTypeForStatus maps a status transition to its event type (spec §8).
// Draft is publish-time only (spec §4): UpdateStatus callers transition to
// approved / needs_changes / superseded, and artifact creation itself is
// recorded as artifact.published, so the default falls back to that.
func EventTypeForStatus(status Status) string {
	switch status {
	case StatusNeedsChanges:
		return EventNeedsChanges
	case StatusSuperseded:
		return EventSuperseded
	default:
		return EventPublished
	}
}

type ReadinessState string

const (
	ReadinessStatePass             ReadinessState = "pass"
	ReadinessStateWarn             ReadinessState = "warn"
	ReadinessStateFail             ReadinessState = "fail"
	ReadinessStateNeedsHumanReview ReadinessState = "needs_human_review"
	ReadinessStateNotApplicable    ReadinessState = "not_applicable"
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

// FixedKeyToRole maps a fixed file key string to a document Role. Used by the
// fixed-key publish shape (files/file_refs maps and the MCP artifact_create tool).
func FixedKeyToRole(key string) Role {
	switch key {
	case "prd", "spec":
		return RoleSpec
	case "design":
		return RoleDesign
	case "implementation_plan", "implementation_plan_fe", "implementation_plan_be",
		"tasks_fe", "tasks_be":
		return RolePlan
	case "tasks_qa":
		return RoleVerification
	case "rollout", "assumptions", "risks", "manifest", "design_refs":
		return RoleReference
	default:
		return RoleUnspecified
	}
}

// FixedKeyToPath maps a fixed file key string to its canonical filename.
// Returns the key unchanged if not a known fixed key (open extension point).
func FixedKeyToPath(key string) string {
	switch key {
	case "prd":
		return "prd.md"
	case "spec":
		return "spec.md"
	case "design":
		return "design.md"
	case "implementation_plan":
		return "implementation-plan.md"
	case "implementation_plan_fe":
		return "implementation-plan-fe.md"
	case "implementation_plan_be":
		return "implementation-plan-be.md"
	case "tasks_fe":
		return "tasks-fe.md"
	case "tasks_be":
		return "tasks-be.md"
	case "tasks_qa":
		return "tasks-qa.md"
	case "rollout":
		return "rollout.md"
	case "assumptions":
		return "assumptions.md"
	case "risks":
		return "risks.md"
	case "manifest":
		return "manifest.json"
	case "design_refs":
		return "design-refs.json"
	default:
		return key
	}
}
