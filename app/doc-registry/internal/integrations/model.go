package integrations

import (
	"errors"
	"time"

	"github.com/specgate/doc-registry/internal/integrations/coretypes"
)

// Transparent aliases keep the parent package's public API and all internal
// references working while the underlying types live in coretypes (a leaf
// package provider subpackages can import without an import cycle).
type (
	Integration        = coretypes.Integration
	Resource           = coretypes.Resource
	OAuthState         = coretypes.OAuthState
	normalizedDelivery = coretypes.NormalizedDelivery
	// Webhook driver I/O (the per-provider WebhookDriver lives in coretypes).
	InboundWebhook  = coretypes.InboundWebhook
	ProviderTarget  = coretypes.ProviderTarget
	ProvisionInput  = coretypes.ProvisionInput
	ProvisionResult = coretypes.ProvisionResult
)

const (
	ProviderGitLab = coretypes.ProviderGitLab
	ProviderGitHub = coretypes.ProviderGitHub
	ProviderLinear = coretypes.ProviderLinear

	StatusConnected = coretypes.StatusConnected
	StatusDisabled  = coretypes.StatusDisabled
	StatusError     = coretypes.StatusError

	AuthMethodPAT   = coretypes.AuthMethodPAT
	AuthMethodOAuth = coretypes.AuthMethodOAuth

	ResourceTypeProject = coretypes.ResourceTypeProject
	ResourceTypeRepo    = coretypes.ResourceTypeRepo
	ResourceTypeTeam    = coretypes.ResourceTypeTeam

	WebhookEventMergeRequest = coretypes.WebhookEventMergeRequest
	WebhookEventTrackerIssue = coretypes.WebhookEventTrackerIssue

	DeliveryStateOpened = coretypes.DeliveryStateOpened
	DeliveryStateMerged = coretypes.DeliveryStateMerged
	DeliveryStateClosed = coretypes.DeliveryStateClosed
)

var (
	ErrNotFound   = coretypes.ErrNotFound
	ErrValidation = coretypes.ErrValidation
	ErrUpstream   = coretypes.ErrUpstream
	// ErrUnauthorized lives in coretypes so webhook drivers can return it;
	// ErrConflict stays parent-only (no subpackage references it).
	ErrUnauthorized = coretypes.ErrUnauthorized
	ErrConflict     = errors.New("integration conflict")
)

const (
	WebhookStatusPending   = "pending"
	WebhookStatusProcessed = "processed"
	WebhookStatusFailed    = "failed"
	WebhookStatusIgnored   = "ignored"
)

type WebhookEvent struct {
	ID              string `json:"id,omitempty" gorm:"column:id;primaryKey"`
	IntegrationID   string `json:"integration_id,omitempty" gorm:"column:integration_id"`
	ResourceID      string `json:"resource_id,omitempty" gorm:"column:resource_id"`
	Provider        string `json:"provider,omitempty" gorm:"column:provider"`
	EventType       string `json:"event_type" gorm:"column:event_type"`
	ExternalEventID string `json:"external_event_id,omitempty" gorm:"column:external_event_id"`
	// PayloadHash is the hex SHA-256 of the raw payload body — a body-derived
	// identity/integrity handle, distinct from the provider's delivery id.
	PayloadHash string `json:"payload_hash,omitempty" gorm:"column:payload_hash"`
	// CorrelationID ties the signal to a SpecGate work item via the universal-floor
	// convention: the exact SpecGate work-reference marker the author declared.
	CorrelationID string     `json:"correlation_id,omitempty" gorm:"column:correlation_id"`
	PayloadJSON   string     `json:"payload_json,omitempty" gorm:"column:payload_json"`
	ReceivedAt    time.Time  `json:"received_at,omitempty" gorm:"column:received_at"`
	ProcessedAt   *time.Time `json:"processed_at,omitempty" gorm:"column:processed_at"`
	Status        string     `json:"status,omitempty" gorm:"column:status"`
	Error         string     `json:"error,omitempty" gorm:"column:error"`
	CreatedAt     time.Time  `json:"created_at,omitempty" gorm:"column:created_at"`
	UpdatedAt     time.Time  `json:"updated_at,omitempty" gorm:"column:updated_at"`
}

func (WebhookEvent) TableName() string { return "integration_webhook_events" }

type WebhookEventFilter struct {
	IntegrationID string
	ResourceID    string
	Status        string
	Limit         int
}

const (
	ExternalTypeMergeRequest = "merge_request"
)

// Tracker-issue link lifecycle states (tracker_links.state).
const (
	TrackerStateOpened  = coretypes.TrackerLifecycleOpened
	TrackerStateClosed  = coretypes.TrackerLifecycleClosed
	TrackerStateRemoved = coretypes.TrackerLifecycleRemoved
)

const (
	FeedbackStatusReceived = "received"
	FeedbackStatusAccepted = "accepted"
	FeedbackStatusRejected = "rejected"
)

// Provider-neutral delivery feedback vocabulary. A GitHub PR and a GitLab MR
// both normalize onto these event types, so downstream consumers never branch
// on provider. delivery.push_seen is the reserved name for direct-push signals;
// no push handler emits it yet, so its constant is added when push lands.
const (
	FeedbackEventPROpened                    = "delivery.pr_opened"
	FeedbackEventPRMerged                    = "delivery.pr_merged"
	FeedbackEventPRUnmatched                 = "delivery.pr_unmatched"
	FeedbackEventPRClosed                    = "delivery.pr_closed"
	FeedbackEventCodingAgentBlockedAmbiguity = "coding_agent.blocked_ambiguity"
	FeedbackEventCodingAgentCompleted        = "coding_agent.completed"
	FeedbackEventCodingAgentDocsUpdated      = "coding_agent.docs_updated"
	FeedbackEventCodingAgentPeerReviewed     = "coding_agent.peer_reviewed"
	FeedbackEventCommentScopeDrift           = "delivery.comment_scope_drift"
	// FeedbackEventTrackerStatusChanged carries an inbound tracker workflow
	// transition (Linear issue state.type). Trackers are optional, so this
	// signal augments — never gates — the git delivery floor; its payload
	// carries the raw tracker state (triage|backlog|unstarted|started|
	// completed|canceled), not an MR-shaped delivery state.
	FeedbackEventTrackerStatusChanged = "delivery.tracker_status_changed"
)

// LinearWebhookInput is the raw transport for POST /integrations/{id}/linear/
// webhook. Linear signs the body with HMAC-SHA256 and sends a bare hex digest
// in Linear-Signature (no sha256= prefix), so the service needs the exact
// bytes (RawPayload) and the recoverable webhook secret to verify.
type LinearWebhookInput struct {
	Signature   string // Linear-Signature: bare hex HMAC-SHA256 of the body
	DeliveryID  string // Linear-Delivery: unique delivery UUID used for deduplication
	PayloadJSON string
}

type LinearWebhookResult struct {
	WebhookEventID   string   `json:"webhook_event_id,omitempty"`
	FeedbackEventIDs []string `json:"feedback_event_ids,omitempty"`
	IntegrationID    string   `json:"integration_id,omitempty"`
	Identifier       string   `json:"identifier,omitempty"`
	TrackerState     string   `json:"tracker_state,omitempty"`
	CorrelationID    string   `json:"correlation_id,omitempty"`
	Status           string   `json:"status"`
	IgnoredReason    string   `json:"ignored_reason,omitempty"`
}

type DeliveryLink struct {
	ID              string    `json:"id" gorm:"column:id;primaryKey"`
	IntegrationID   string    `json:"integration_id" gorm:"column:integration_id"`
	ResourceID      string    `json:"resource_id" gorm:"column:resource_id"`
	FeatureID       string    `json:"feature_id" gorm:"column:feature_id"`
	ChangeRequestID string    `json:"change_request_id" gorm:"column:change_request_id"`
	ExternalType    string    `json:"external_type" gorm:"column:external_type"`
	ExternalID      string    `json:"external_id" gorm:"column:external_id"`
	ExternalIID     string    `json:"external_iid" gorm:"column:external_iid"`
	ExternalKey     string    `json:"external_key" gorm:"column:external_key"`
	URL             string    `json:"url" gorm:"column:url"`
	Title           string    `json:"title" gorm:"column:title"`
	State           string    `json:"state" gorm:"column:state"`
	SourceBranch    string    `json:"source_branch" gorm:"column:source_branch"`
	TargetBranch    string    `json:"target_branch" gorm:"column:target_branch"`
	HeadSHA         string    `json:"head_sha" gorm:"column:head_sha"`
	MergeCommitSHA  string    `json:"merge_commit_sha" gorm:"column:merge_commit_sha"`
	LastEventID     string    `json:"last_event_id" gorm:"column:last_event_id"`
	CreatedAt       time.Time `json:"created_at" gorm:"column:created_at"`
	UpdatedAt       time.Time `json:"updated_at" gorm:"column:updated_at"`
}

func (DeliveryLink) TableName() string { return "integration_delivery_links" }

// TrackerLink is the persisted Linear issue handoff for a work item. The
// selected Linear team resource is immutable handoff context; one work item has
// one primary tracker link in this first-release schema.
type TrackerLink struct {
	ID              string `json:"id" gorm:"column:id;primaryKey"`
	IntegrationID   string `json:"integration_id" gorm:"column:integration_id"`
	ResourceID      string `json:"resource_id" gorm:"column:resource_id"`
	FeatureID       string `json:"feature_id" gorm:"column:feature_id"`
	ChangeRequestID string `json:"change_request_id" gorm:"column:change_request_id"`
	ExternalID      string `json:"external_id" gorm:"column:external_id"`
	ExternalKey     string `json:"external_key" gorm:"column:external_key"`
	URL             string `json:"url" gorm:"column:url"`
	Title           string `json:"title" gorm:"column:title"`
	State           string `json:"state" gorm:"column:state"`
	// TrackerState is the last inbound provider workflow state name written by
	// the webhook path. For Linear this is the full state name (data.state.name,
	// e.g. "In Review", "Done"). The value is used for dedup (emit
	// delivery.tracker_status_changed only on a real transition) and drives the
	// UI badge. See persistedTrackerState in service.go.
	TrackerState string    `json:"tracker_state" gorm:"column:tracker_state"`
	CreatedAt    time.Time `json:"created_at" gorm:"column:created_at"`
	UpdatedAt    time.Time `json:"updated_at" gorm:"column:updated_at"`
}

func (TrackerLink) TableName() string { return "tracker_links" }

type GovernanceFeedbackEvent struct {
	ID              string    `json:"id" gorm:"column:id;primaryKey"`
	WorkspaceID     string    `json:"workspace_id" gorm:"column:workspace_id"`
	IntegrationID   string    `json:"integration_id" gorm:"column:integration_id"`
	ResourceID      string    `json:"resource_id" gorm:"column:resource_id"`
	WebhookEventID  string    `json:"webhook_event_id" gorm:"column:webhook_event_id"`
	DeliveryLinkID  string    `json:"delivery_link_id" gorm:"column:delivery_link_id"`
	FeatureID       string    `json:"feature_id" gorm:"column:feature_id"`
	ChangeRequestID string    `json:"change_request_id" gorm:"column:change_request_id"`
	ArtifactID      string    `json:"artifact_id" gorm:"column:artifact_id"`
	EventType       string    `json:"event_type" gorm:"column:event_type"`
	PayloadJSON     string    `json:"payload_json" gorm:"column:payload_json"`
	Status          string    `json:"status" gorm:"column:status"`
	Reason          string    `json:"reason" gorm:"column:reason"`
	CreatedAt       time.Time `json:"created_at" gorm:"column:created_at"`
	UpdatedAt       time.Time `json:"updated_at" gorm:"column:updated_at"`
}

func (GovernanceFeedbackEvent) TableName() string { return "governance_feedback_events" }

type GovernanceFeedbackFilter struct {
	Status          string
	ChangeRequestID string
	ArtifactID      string
	EventType       string
	Limit           int
}

type GitLabWebhookResult struct {
	WebhookEventID   string   `json:"webhook_event_id,omitempty"`
	DeliveryLinkID   string   `json:"delivery_link_id,omitempty"`
	FeedbackEventIDs []string `json:"feedback_event_ids,omitempty"`
	IntegrationID    string   `json:"integration_id,omitempty"`
	ResourceID       string   `json:"resource_id,omitempty"`
	FeatureID        string   `json:"feature_id,omitempty"`
	ChangeRequestID  string   `json:"change_request_id,omitempty"`
	Status           string   `json:"status"`
	IgnoredReason    string   `json:"ignored_reason,omitempty"`
}
