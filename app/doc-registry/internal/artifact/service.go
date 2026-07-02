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

// ErrStaleBase is returned when an optimistic-lock publish supplies a
// base_version that is no longer the feature's latest version.
var ErrStaleBase = errors.New("stale base version")

// ErrApprovalRequiresHuman is returned when an agent actor tries to approve an
// artifact whose profile has approval_policy=human_required.
// NOTE: actor_kind is a cooperative surface check (client-asserted); there is no
// server-side identity. The human surface is expected to perform the approval,
// not a server identity gate.
var ErrApprovalRequiresHuman = errors.New("approval requires a human actor for this profile")

// Service is the application-level facade over artifact persistence,
// conflict detection, event emission, and S3 storage. Implementation
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
	SignedFileURL(ctx context.Context, id string, path string) (*SignedFile, error)
	FileContent(ctx context.Context, id string, path string) ([]byte, error)
	CheckConflicts(ctx context.Context, services []string) (*ConflictReport, error)
	ListEvents(ctx context.Context, f EventFilter) ([]Event, error)
	RefreshReadinessRuns(ctx context.Context, artifactID string, evaluations []ReadinessEvaluation) ([]ReadinessRun, error)
	ListReadinessRuns(ctx context.Context, artifactID string, limit int) ([]ReadinessRun, error)
}

// ReadinessService is the subset of Service used by readiness gate operations.
type ReadinessService interface {
	RefreshReadinessRuns(ctx context.Context, artifactID string, evaluations []ReadinessEvaluation) ([]ReadinessRun, error)
	ListReadinessRuns(ctx context.Context, artifactID string, limit int) ([]ReadinessRun, error)
}

// DocumentInput is a single document to publish within an artifact package.
// Either Content (upload bytes) or a pre-resolved ref (ResolvedS3Path) must be set.
type DocumentInput struct {
	Path    string // document path within the artifact, e.g. "prd.md" or "docs/proposal.md"
	Role    string // role hint, normalised via NormalizeRole
	Content []byte // upload bytes; takes precedence over a resolved ref
	// Set by the HTTP layer when publishing an existing governance file by reference.
	ResolvedS3Path    string
	ResolvedSizeBytes int64
}

type PublishInput struct {
	FeatureID string
	Version   string
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
	SourceKind               string
	SourceID                 string
	SourceRevision           string
	Authority                string
	GatesProfile             string
	GatesProfileVersion      string
	GatesProfileDigest       string
	GatesProfileSnapshotJSON string
	// SP3 lineage fields.
	ParentArtifactID string
	LineageRootID    string
}

type ListFilter struct {
	FeatureID string
	Service   string
	Status    Status
	Limit     int
	Offset    int
}

type StatusUpdate struct {
	Status       Status
	Actor        string
	ReviewRating int
	Note         string
	Manifest     string
	// ActorKind is client-asserted: "human" or "agent". Empty defaults to "human".
	ActorKind string
}

type SignedFile struct {
	URL       string
	ExpiresAt time.Time
	SizeBytes int64
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
	EventType  string
	ArtifactID string
	After      time.Time
	Limit      int
}
