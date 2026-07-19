package artifact

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("artifact not found")
var ErrFileNotFound = errors.New("artifact file not found")
var ErrConflict = errors.New("artifact conflict")
var ErrInvalidPath = errors.New("invalid document path")
var ErrInvalidStatus = errors.New("invalid artifact status transition")
var ErrWorkspaceMismatch = errors.New("artifact workspace does not match request workspace")
var ErrWorkspaceRequired = errors.New("artifact workspace is required")

// ErrStaleBase is returned when an optimistic-lock publish supplies a
// base_version that is no longer the feature's latest version.
var ErrStaleBase = errors.New("stale base version")

// ErrApprovalRequiresHuman is returned when an agent actor tries to approve an
// artifact. Approval is always a human workflow transition.
// NOTE: actor_kind is a cooperative surface check (client-asserted); there is no
// server-side identity. The human surface is expected to perform the approval,
// not a server identity gate.
var ErrApprovalRequiresHuman = errors.New("approval requires a human actor for this profile")

// ErrUnsupportedApprovalPolicy is returned when an immutable artifact has no
// usable approval policy snapshot. The transition fails closed.
var ErrUnsupportedApprovalPolicy = errors.New("artifact has an invalid approval policy snapshot")

// Service is the application-level facade over artifact persistence,
// conflict detection, event emission, and object storage. Implementation
// is filled in by the Implementation Agent per spec.md §6.
type Service interface {
	Publish(ctx context.Context, in PublishInput) (*Artifact, error)
	Get(ctx context.Context, id string) (*Artifact, error)
	List(ctx context.Context, f ListFilter) ([]Artifact, error)
	Count(ctx context.Context, f ListFilter) (int64, error)
	LatestArtifact(ctx context.Context, featureID string) (*Artifact, error)
	NextVersion(ctx context.Context, featureID string) (string, error)
	ResolveNextVersion(ctx context.Context, featureID string, baseVersion string) (string, error)
	UpdateStatus(ctx context.Context, id string, in StatusUpdate) (*Artifact, error)
	Delete(ctx context.Context, id string) error
	FileContent(ctx context.Context, id string, path string) ([]byte, error)
	CheckConflicts(ctx context.Context, services []string) (*ConflictReport, error)
	ListEvents(ctx context.Context, f EventFilter) ([]Event, error)
	RefreshReadinessRuns(ctx context.Context, artifactID string, evaluations []ReadinessEvaluation) ([]ReadinessRun, error)
	ListReadinessRuns(ctx context.Context, artifactID string, limit int) ([]ReadinessRun, error)
}

// DocumentInput is a single document to publish within an artifact package.
type DocumentInput struct {
	Path    string // document path within the artifact, e.g. "prd.md" or "docs/proposal.md"
	Role    string // role hint, normalised via NormalizeRole
	Content []byte
}

type PublishInput struct {
	WorkspaceID string
	FeatureID   string
	Version     string
	// BaseVersion is an optional optimistic lock. When set, Publish rejects the
	// request with ErrStaleBase unless it equals the feature's current latest
	// version. Empty skips the check.
	BaseVersion          string
	Status               Status
	RequestType          RequestType
	ImpactLevel          ImpactLevel
	ArtifactPhase        ArtifactPhase
	ArtifactCompleteness ArtifactCompleteness
	ConfidenceScore      *float64
	AmbiguityScore       *float64
	GovernanceVersion    string
	CreatedBy            string
	ImpactedServices     []ServiceRef
	Documents            []DocumentInput
	// SP0 governance envelope fields.
	SourceKind         string
	SourceID           string
	SourceRevision     string
	Authority          string
	PolicyVersion      string
	PolicyDigest       string
	PolicySnapshotJSON string
	// SP3 lineage fields.
	ParentArtifactID string
	LineageRootID    string
}

type ListFilter struct {
	WorkspaceID string
	FeatureID   string
	Service     string
	Status      Status
	// ExcludeStatus drops one status from results (e.g. superseded for the
	// default "current" list views). Ignored when empty or when Status is set.
	ExcludeStatus Status
	Limit         int
	Offset        int
}

type StatusUpdate struct {
	Status       Status
	Actor        string
	ReviewRating int
	Note         string
	// ActorKind is client-asserted: "human" or "agent". Empty defaults to "human".
	ActorKind string
}

type ConflictReport struct {
	State     string
	Conflicts []Conflict
}

type Conflict struct {
	ID                  string
	Type                string
	Existing            Artifact
	OverlappingServices []string
	ResolutionOptions   []string
}

type EventFilter struct {
	WorkspaceID string
	EventType   string
	ArtifactID  string
	After       time.Time
	Limit       int
}
