package governanceops

import (
	"context"
	"errors"

	"github.com/specgate/doc-registry/internal/agentsclient"
	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/artifactattachment"
	"github.com/specgate/doc-registry/internal/artifactedit"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/knowledge"
	"github.com/specgate/doc-registry/internal/skills"
	"github.com/specgate/doc-registry/internal/workboard"
)

var (
	ErrNotFound         = errors.New("governance resource not found")
	ErrProviderRequired = errors.New("provider is required for a bare tracker key")
	ErrUnavailable      = errors.New("governance operation unavailable")
	// ErrVersionConflict is returned by PublishArtifact when base_version is stale.
	ErrVersionConflict = errors.New("version conflict: base_version is stale")
)

// WorkBoardReader is the workboard surface the governance service needs.
type WorkBoardReader interface {
	ListChangeRequests(ctx context.Context, includeArchived bool) ([]workboard.ChangeRequest, error)
	GetChangeRequest(ctx context.Context, id string) (*workboard.ChangeRequest, error)
	GetFeature(ctx context.Context, id string) (*workboard.Feature, error)
	ListAcceptanceCriteria(ctx context.Context, id string) ([]workboard.AcceptanceCriterion, error)
	ListGateRuns(ctx context.Context, id string, limit int) ([]workboard.GateRun, error)
	ListStaleWarnings(ctx context.Context, filter workboard.StaleWarningFilter) ([]workboard.StaleWarning, error)
}

// TrackerReader is the tracker-link read surface the governance service needs.
type TrackerReader interface {
	List(ctx context.Context) ([]integrations.Integration, error)
	ListTrackerLinks(ctx context.Context, changeRequestID string) ([]integrations.TrackerLink, error)
}

// ContextPackArtifactReader reads artifact files for context-pack assembly.
type ContextPackArtifactReader interface {
	Get(ctx context.Context, id string) (*artifact.Artifact, error)
	FileContent(ctx context.Context, id string, path string) ([]byte, error)
}

// ContextPackAttachmentReader lists a feature's reference attachments for
// context-pack assembly (nil disables the section).
type ContextPackAttachmentReader interface {
	ListByFeature(ctx context.Context, featureID string) ([]artifactattachment.Attachment, error)
}

// ContextPackSkillReader lists registered Skills for context-pack assembly
// (nil disables the Applicable Skills section).
type ContextPackSkillReader interface {
	List(ctx context.Context) ([]skills.Skill, error)
}

// ContextPackKnowledgeReader queries knowledge documents for provenance rows
// (nil disables the knowledge_provenance section).
type ContextPackKnowledgeReader interface {
	ListByFeatureOrRequest(ctx context.Context, featureID, requestID string) ([]knowledge.Document, error)
}

// FeedbackStore is the write surface for governance feedback events.
type FeedbackStore interface {
	CreateGovernanceFeedbackEvent(context.Context, integrations.GovernanceFeedbackEvent) (*integrations.GovernanceFeedbackEvent, error)
	ListGovernanceFeedbackEvents(context.Context, integrations.GovernanceFeedbackFilter) ([]integrations.GovernanceFeedbackEvent, error)
}

// ArtifactWriter is the write surface for artifact publication and status transitions.
type ArtifactWriter interface {
	LatestArtifact(ctx context.Context, featureID string) (*artifact.Artifact, error)
	NextVersion(ctx context.Context, featureID string) (string, error)
	ResolveNextVersion(ctx context.Context, featureID string, baseVersion string) (string, error)
	Publish(ctx context.Context, in artifact.PublishInput) (*artifact.Artifact, error)
	UpdateStatus(ctx context.Context, id string, in artifact.StatusUpdate) (*artifact.Artifact, error)
}

// FeatureUpserter upserts workboard features by stable key.
type FeatureUpserter interface {
	UpsertFeatureByKey(ctx context.Context, key, name string) (*workboard.Feature, error)
}

// ProfileResolver resolves governance profiles for artifact publication.
type ProfileResolver interface {
	ResolveProfile(ctx context.Context, in governanceprofile.ResolveInput) (*governanceprofile.ResolvedProfile, error)
}

// ArtifactEditStore is the write surface for artifact edit sessions.
type ArtifactEditStore interface {
	CreateSession(ctx context.Context, session artifactedit.Session, baseFiles, workingFiles map[string]string) error
	ListProposals(ctx context.Context) ([]artifactedit.Session, error)
}

// AgentsRunner is the agents-service surface for readiness, quality gates,
// delivery review, and quick work-item creation.
type AgentsRunner interface {
	RunReadiness(ctx context.Context, artifactID string) (*agentsclient.Verdict, error)
	RunLLMGates(ctx context.Context, changeRequestID string) (map[string]any, error)
	ReviewDelivery(ctx context.Context, changeRequestID string) (map[string]any, error)
	CreateQuickWorkItem(ctx context.Context, title, description, issueURL, issueKey, featureKey, featureName string, acceptanceCriteria []string, createdBy string, workspaceID string) (map[string]any, error)
}

// Service is the shared application layer for governance operations. It is
// consumed by both the MCP adapter and the REST CLI facades so both surfaces
// execute identical behavior.
type Service struct {
	WorkBoard  WorkBoardReader
	Trackers   TrackerReader
	AppBaseURL string
	// Context-pack assembly readers. nil fields disable the corresponding section.
	Artifacts   ContextPackArtifactReader
	Attachments ContextPackAttachmentReader
	Skills      ContextPackSkillReader
	Knowledge   ContextPackKnowledgeReader

	// Governance write surfaces. nil fields disable the corresponding operation.
	FeedbackStore  FeedbackStore
	FeedbackNotify func(changeRequestID, eventType string) // nil-safe

	// Artifact publication and draft-proposal write surfaces.
	ArtifactWriter  ArtifactWriter
	FeatureUpserter FeatureUpserter
	ProfileResolver ProfileResolver
	// DraftArtifacts is the artifact read surface for seeding draft proposals.
	// Shares ContextPackArtifactReader; nil disables DraftArtifactUpdate.
	DraftArtifacts ContextPackArtifactReader
	EditStore      ArtifactEditStore

	// AgentsRunner provides agents-backed operations (readiness, gates, delivery,
	// quick work items). nil disables all agents-backed tools.
	AgentsRunner AgentsRunner

	// StatsSource is the read surface for the governance stats projection
	// (see stats.go). nil disables Stats.
	StatsSource StatsReader
}
