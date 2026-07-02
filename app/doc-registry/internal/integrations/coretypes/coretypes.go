// Package coretypes holds the leaf domain types, sentinel errors, provider
// constants, and the outbound-tracker registry that both the parent
// integrations package and per-provider subpackages reference without importing
// the parent (which would create an import cycle). The parent re-exports these
// via transparent type aliases so its public API and ~15 external callers are
// unaffected.
package coretypes

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

const (
	ProviderGitLab = "gitlab"
	ProviderGitHub = "github"
	ProviderLinear = "linear"
)

const (
	StatusConnected = "connected"
	StatusDisabled  = "disabled"
	StatusError     = "error"
)

const (
	AuthMethodPAT   = "pat"
	AuthMethodOAuth = "oauth"
)

const (
	ResourceTypeProject = "project"
	ResourceTypeRepo    = "repo"
	ResourceTypeTeam    = "team"
)

const (
	WebhookEventMergeRequest = "merge_request"
	WebhookEventPush         = "push"
	WebhookEventIssue        = "issue"
	WebhookEventComment      = "comment"
	// WebhookEventTrackerIssue is the provider-neutral event type for an inbound
	// tracker issue signal (a GitLab Issue Hook or a Linear Issue webhook). It
	// augments the MR/PR delivery floor rather than gating it.
	WebhookEventTrackerIssue = "tracker_issue"
	// WebhookEventWorkflowRun is the provider-neutral event type for a GitHub
	// Actions workflow_run.completed result. It stamps ci_run evidence on the
	// matching CR when the workflow conclusion is "success".
	WebhookEventWorkflowRun = "workflow_run"
)

const (
	DeliveryStateOpened   = "opened"
	DeliveryStateMerged   = "merged"
	DeliveryStateClosed   = "closed"
	DeliveryStateCIPassed = "ci_passed"
)

var (
	ErrNotFound   = errors.New("integration not found")
	ErrValidation = errors.New("integration validation")
	// ErrUpstream marks a failure talking to an external provider API (e.g. a
	// Linear GraphQL transport error, non-2xx, or `errors` array) as distinct
	// from a local config/validation error. The HTTP layer maps it to 502.
	ErrUpstream = errors.New("integration upstream")
	// ErrUnauthorized marks a failed inbound-webhook authentication (bad
	// signature/token). Lives here so per-provider webhook drivers can return it.
	// The HTTP layer maps it to 401.
	ErrUnauthorized = errors.New("integration unauthorized")
)

// Integration is both the storage row and the public DTO. `omitempty` on every
// optional field is intentional: it tells Huma's schema generator (which auto-
// derives required-property lists from JSON tags) to mark these as optional in
// the OpenAPI schema, so `POST /integrations` accepts a minimal `{provider,
// name}` body. Service-level normalization fills sensible defaults for status
// and config_json (see normalizeIntegration); migration column defaults pin
// the same invariants at the storage layer.
type Integration struct {
	ID         string `json:"id,omitempty" gorm:"column:id;primaryKey"`
	Provider   string `json:"provider" gorm:"column:provider"`
	Name       string `json:"name" gorm:"column:name"`
	Status     string `json:"status,omitempty" gorm:"column:status"`
	BaseURL    string `json:"base_url,omitempty" gorm:"column:base_url"`
	ConfigJSON string `json:"config_json,omitempty" gorm:"column:config_json"`
	// APITokenEncrypted stores the AES-256-GCM ciphertext of a provider API
	// token (Linear's hosted-MCP / GraphQL token). Recoverable like the
	// webhook secret because outbound calls need the plaintext; never returned
	// in plaintext. HasAPIToken below is the read-side "configured" signal.
	// Set or rotate via PUT /integrations/{id}/api-token.
	APITokenEncrypted string `json:"-" gorm:"column:api_token_encrypted"`
	AuthMethod        string `json:"auth_method,omitempty" gorm:"column:auth_method"`
	// WebhookSecretEncrypted stores the AES-256-GCM ciphertext of a
	// per-integration inbound-webhook secret (GitLab/GitHub only — Linear uses a
	// provider-managed env secret). Recoverable so the user can reveal/copy it
	// into the provider's webhook settings; never returned in plaintext on the
	// integration DTO. HasWebhookSecret below is the read-side presence signal.
	// Read/rotate via /integrations/{id}/webhook-secret[/rotate].
	WebhookSecretEncrypted string `json:"-" gorm:"column:webhook_secret_encrypted"`
	// OAuth grant material is recoverable because the runtime must exchange it
	// for upstream API calls and refresh operations. Presence flags / metadata
	// are exposed; plaintext tokens never cross the API boundary.
	OAuthAccessTokenEncrypted  string     `json:"-" gorm:"column:oauth_access_token_encrypted"`
	OAuthRefreshTokenEncrypted string     `json:"-" gorm:"column:oauth_refresh_token_encrypted"`
	OAuthExpiresAt             *time.Time `json:"oauth_expires_at,omitempty" gorm:"column:oauth_expires_at"`
	OAuthTokenType             string     `json:"oauth_token_type,omitempty" gorm:"column:oauth_token_type"`
	OAuthScope                 string     `json:"oauth_scope,omitempty" gorm:"column:oauth_scope"`
	OAuthAccountID             string     `json:"oauth_account_id,omitempty" gorm:"column:oauth_account_id"`
	OAuthAccountName           string     `json:"oauth_account_name,omitempty" gorm:"column:oauth_account_name"`
	OAuthAccountEmail          string     `json:"oauth_account_email,omitempty" gorm:"column:oauth_account_email"`
	OAuthHostKey               string     `json:"oauth_host_key,omitempty" gorm:"column:oauth_host_key"`
	// HasAPIToken is a derived, write-only-safe presence flag: true when
	// APITokenEncrypted is non-empty. It is not a column (gorm:"-"); the
	// repository populates it on read so the UI can render "configured"
	// without the ciphertext ever crossing the API boundary.
	HasAPIToken       bool       `json:"has_api_token,omitempty" gorm:"-"`
	HasOAuthToken     bool       `json:"has_oauth_token,omitempty" gorm:"-"`
	HasWebhookSecret  bool       `json:"has_webhook_secret,omitempty" gorm:"-"`
	LastHealthCheckAt *time.Time `json:"last_health_check_at,omitempty" gorm:"column:last_health_check_at"`
	LastError         string     `json:"last_error,omitempty" gorm:"column:last_error"`
	CreatedAt         time.Time  `json:"created_at,omitempty" gorm:"column:created_at"`
	UpdatedAt         time.Time  `json:"updated_at,omitempty" gorm:"column:updated_at"`
}

func (Integration) TableName() string { return "integrations" }

// Resource — same dual-purpose model + DTO pattern as Integration. `omitempty`
// keeps the Huma schema permissive: clients only need `resource_type` and
// `external_key` on POST; integration_id is taken from the path.
type Resource struct {
	ID                     string    `json:"id,omitempty" gorm:"column:id;primaryKey"`
	IntegrationID          string    `json:"integration_id,omitempty" gorm:"column:integration_id"`
	ResourceType           string    `json:"resource_type" gorm:"column:resource_type"`
	ExternalID             string    `json:"external_id,omitempty" gorm:"column:external_id"`
	ExternalKey            string    `json:"external_key" gorm:"column:external_key"`
	DisplayName            string    `json:"display_name,omitempty" gorm:"column:display_name"`
	DefaultRef             string    `json:"default_ref,omitempty" gorm:"column:default_ref"`
	ConfigJSON             string    `json:"config_json,omitempty" gorm:"column:config_json"`
	WebhookSecretEncrypted string    `json:"-" gorm:"column:webhook_secret_encrypted"`
	HasWebhookSecret       bool      `json:"has_webhook_secret,omitempty" gorm:"-"`
	CreatedAt              time.Time `json:"created_at,omitempty" gorm:"column:created_at"`
	UpdatedAt              time.Time `json:"updated_at,omitempty" gorm:"column:updated_at"`
}

func (Resource) TableName() string { return "integration_resources" }

type OAuthState struct {
	ID             string `json:"id,omitempty" gorm:"column:id;primaryKey"`
	State          string `json:"state,omitempty" gorm:"column:state"`
	IntegrationID  string `json:"integration_id,omitempty" gorm:"column:integration_id"`
	Provider       string `json:"provider,omitempty" gorm:"column:provider"`
	HostKey        string `json:"host_key,omitempty" gorm:"column:host_key"`
	RedirectTarget string `json:"redirect_target,omitempty" gorm:"column:redirect_target"`
	// CodeVerifier is the PKCE code verifier minted at authorize time and
	// replayed (as code_verifier) during the token exchange. Never returned.
	CodeVerifier string `json:"-" gorm:"column:code_verifier"`
	// Pending* carry the to-be-created integration spec for the create-on-callback
	// flow: a fresh OAuth connect has no integration row yet (IntegrationID is
	// empty), so the callback creates the integration from these. Empty for the
	// reconnect path (an existing IntegrationID).
	PendingName       string     `json:"-" gorm:"column:pending_name"`
	PendingBaseURL    string     `json:"-" gorm:"column:pending_base_url"`
	PendingConfigJSON string     `json:"-" gorm:"column:pending_config_json"`
	ExpiresAt         time.Time  `json:"expires_at,omitempty" gorm:"column:expires_at"`
	ConsumedAt        *time.Time `json:"consumed_at,omitempty" gorm:"column:consumed_at"`
	CreatedAt         time.Time  `json:"created_at,omitempty" gorm:"column:created_at"`
	UpdatedAt         time.Time  `json:"updated_at,omitempty" gorm:"column:updated_at"`
}

func (OAuthState) TableName() string { return "integration_oauth_states" }

// TrackerIssue is the created issue's stable handle returned to the caller.
// ID is the provider's immutable issue id (Linear issue UUID); it survives a
// title/identifier change, so the inbound webhook correlates the persisted
// DeliveryLink by it. Identifier is the human key (e.g. ENG-123 / #45).
type TrackerIssue struct {
	ID         string `json:"id,omitempty"`
	Identifier string `json:"identifier"`
	URL        string `json:"url"`
}

// NormalizedDelivery is the provider-neutral shape consumed by the parent's
// commitDelivery ingest pipeline. A GitHub PR and a GitLab MR both map onto
// these fields and surface as the same delivery.pr_* feedback vocabulary.
//
// Fields are exported because provider subpackages (e.g. github) construct
// values here while the parent's pipeline reads them — neither can reach the
// other's unexported identifiers.
type NormalizedDelivery struct {
	Provider        string
	EventType       string
	ExternalEventID string
	RawPayload      string

	ProjectID  int
	ProjectKey string

	ExternalID     string
	IID            int
	ExternalKey    string
	Title          string
	Description    string
	URL            string
	Action         string
	RawState       string
	SourceBranch   string
	TargetBranch   string
	MergeCommitSHA string

	DeliveryState string

	// Priority is the tracker issue priority (Linear: 0=no priority, 1=urgent,
	// 2=high, 3=normal, 4=low). Populated only for tracker-issue events;
	// zero for all MR/PR delivery events.
	Priority int
}

// NormalizedComment is the provider-neutral shape for inbound code/tracker
// comments that may indicate source-of-truth drift.
type NormalizedComment struct {
	Provider        string
	EventType       string
	ExternalEventID string
	RawPayload      string

	ProjectID  int
	ProjectKey string

	ExternalID    string
	ExternalKey   string
	Title         string
	Body          string
	URL           string
	Author        string
	CorrelationID string
}

// Handoff is the provider-neutral input an outbound tracker adapter needs to
// file one issue: the integration + its resources (for destination
// resolution), the in-process-decrypted API token, and the rendered envelope.
// Fields are exported for the same cross-package reason as NormalizedDelivery.
type Handoff struct {
	Integration *Integration
	Resources   []Resource
	APIToken    string
	Title       string
	Body        string
}

// TrackerAdapter is the strategy seam for outbound issue creation. Each tracker
// provider (Linear, GitLab, GitHub) implements it; the registry below is the
// single source of truth for "which providers do outbound trackers"
// (providerSupportsAPIToken derives from it). The method is exported so adapter
// types defined in any package satisfy the interface.
type TrackerAdapter interface {
	CreateIssue(ctx context.Context, h Handoff) (*TrackerIssue, error)
}

var (
	trackerMu       sync.RWMutex
	trackerAdapters = map[string]TrackerAdapter{}
)

// RegisterTracker wires a provider's outbound adapter into the registry. Each
// provider package calls this from init(); it is the only place a new provider
// is added to the handoff path.
func RegisterTracker(provider string, a TrackerAdapter) {
	trackerMu.Lock()
	defer trackerMu.Unlock()
	trackerAdapters[provider] = a
}

// LookupTracker returns the adapter registered for a provider, if any.
func LookupTracker(provider string) (TrackerAdapter, bool) {
	trackerMu.RLock()
	defer trackerMu.RUnlock()
	a, ok := trackerAdapters[provider]
	return a, ok
}

// IntegrationConfigString reads a string field from the integration's
// config_json.
func IntegrationConfigString(integration *Integration, key string) string {
	raw := strings.TrimSpace(integration.ConfigJSON)
	if raw == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}
