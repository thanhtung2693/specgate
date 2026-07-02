package api

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/artifactattachment"
	"github.com/specgate/doc-registry/internal/artifactedit"
	"github.com/specgate/doc-registry/internal/config"
	"github.com/specgate/doc-registry/internal/governancefiles"
	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/governancethreads"
	"github.com/specgate/doc-registry/internal/identity"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/knowledge"
	"github.com/specgate/doc-registry/internal/mcp"
	"github.com/specgate/doc-registry/internal/notifications"
	"github.com/specgate/doc-registry/internal/policy"
	"github.com/specgate/doc-registry/internal/settings"
	"github.com/specgate/doc-registry/internal/skills"
	blob "github.com/specgate/doc-registry/internal/storage/blob"
	stores3 "github.com/specgate/doc-registry/internal/storage/s3"
	"github.com/specgate/doc-registry/internal/workboard"
)

// Handlers carries the dependencies needed to serve HTTP requests. Individual
// operations live in domain-specific files (handlers_artifacts.go,
// handlers_knowledge.go, handlers_governance.go, handlers_mcp_servers.go,
// handlers_settings.go, handlers_skills.go, handlers_artifact_edit.go.
type Handlers struct {
	Artifacts artifact.Service
	// ArtifactEdit persists durable artifact edit sessions (base/working file
	// snapshots, per-hunk decisions, saved revisions). Nil disables the routes.
	ArtifactEdit artifactedit.Store
	// WorkBoard owns durable Feature/ChangeRequest records for the artifact-native planning board.
	WorkBoard workboard.Store
	Knowledge *knowledge.Service
	// S3 is optional for artifact routes; required for governance image presign.
	S3 *stores3.Client
	// TTL for internal governance-file presigned PUT URLs (zero uses handler default).
	GovernanceUploadPutTTL time.Duration
	// BlobStore is used for governance file storage when STORAGE_DRIVER=local.
	// When nil, falls back to S3 (STORAGE_DRIVER=s3).
	BlobStore blob.Store
	// GovernanceFiles persists internal governance-file blobs used by explicit pins and compatibility flows.
	GovernanceFiles governancefiles.Store
	// ArtifactAttachments persists feature-scoped reference attachments (links,
	// files, screenshots) surfaced to quality gates and the coding-agent handoff.
	ArtifactAttachments artifactattachment.Store
	// GovernanceThreads persists lightweight governance-chat sidebar summaries.
	GovernanceThreads governancethreads.Store
	// GovernanceUploadMaxBytes rejects presign requests above this size (0 disables).
	GovernanceUploadMaxBytes int64
	// S3KeyPrefix is prepended to every generated S3 key so a shared bucket
	// keeps doc-registry content under a dedicated directory (default
	// "doc-registry/"). Empty disables.
	S3KeyPrefix string

	// Settings service for runtime MCP configuration and GET/PUT /settings.
	Settings *settings.Service
	// MCPBootEnabled is mcp.enabled at process start (for GET /mcp/info restart_required).
	MCPBootEnabled bool
	// Skills optional user-defined skills; nil when not configured.
	Skills *skills.Service
	// Integrations persists native source-control and workflow integrations.
	Integrations *integrations.Service
	// GovernanceProfiles resolves and imports SpecGate governance profiles.
	GovernanceProfiles *governanceprofile.Service
	// Identity stores local users and workspaces for attribution and selection.
	Identity identity.Store

	// AppBaseURL is the SpecGate UI origin embedded in the work-item permalink of an
	// outbound tracker issue (config APP_BASE_URL). Empty falls back to the
	// dev default in the integrations envelope builder.
	AppBaseURL string

	// Readiness delegates the in-IDE readiness check to the agents service.
	// nil (AGENTS_BASE_URL unset) → specgate_check_readiness is not registered.
	Readiness mcp.ReadinessRunner
	// LLMGates delegates running LLM quality gates for a CR to the agents
	// service. nil (AGENTS_BASE_URL unset) → run_llm_gates is not registered.
	LLMGates mcp.LLMGatesRunner
	// DeliveryReview delegates triggering the delivery review for a CR to the
	// agents service. nil (AGENTS_BASE_URL unset) → trigger_delivery_review is not registered.
	DeliveryReview mcp.DeliveryReviewTrigger
	// QuickWorkItem delegates quick-route CR creation from issue content to the
	// agents service. nil (AGENTS_BASE_URL unset) → create_quick_work_item is not registered.
	QuickWorkItem mcp.QuickWorkItemCreator

	// Governance is the shared application layer for governance operations,
	// consumed by the /api/v1/ CLI REST facades. nil disables those routes.
	Governance *governanceops.Service

	// GateTaskStore manages IDE-agent gate task lifecycle (pull/submit). Nil disables the routes.
	GateTaskStore policy.GateTaskStore

	// Config carries the process-level configuration for handlers that need it
	// (e.g. webhook secret validation). When nil, features guarded by config
	// fields degrade gracefully (typically returning 501).
	Config *config.Config

	// OAuthCallbackBaseURL is the public origin OAuth providers redirect back to
	// (config OAUTH_PUBLIC_CALLBACK_BASE_URL). The /integrations/oauth-callback
	// path is appended when building the provider authorize/token redirect_uri.
	OAuthCallbackBaseURL string

	// Notifications publishes compact invalidation signals for optional
	// consumers. Nil keeps the API poll-only.
	Notifications notifications.Publisher
}

func notImplemented(op string) error {
	return huma.Error501NotImplemented(op + " not implemented")
}

// requireService returns an HTTP 503 error if svc is nil.
func (h *Handlers) requireService(svc any, name string) error {
	// Guard against both a true-nil interface and a typed-nil pointer (e.g. a
	// nil *Service boxed into `any`, which is a non-nil interface) so an unwired
	// service returns 503 instead of panicking.
	if svc == nil {
		return huma.Error503ServiceUnavailable(name + " service not configured")
	}
	if v := reflect.ValueOf(svc); v.Kind() == reflect.Ptr && v.IsNil() {
		return huma.Error503ServiceUnavailable(name + " service not configured")
	}
	return nil
}

// sentinelMapping maps a sentinel error to an HTTP error constructor.
// hideErr suppresses forwarding the underlying error (e.g. for 401 responses
// that must not reveal whether a resource exists).
type sentinelMapping struct {
	sentinel error
	fn       func(string, ...error) huma.StatusError
	hideErr  bool
}

// mapHTTPError iterates mappings in order and returns the first matching HTTP
// error. Falls through to 500 if no sentinel matches.
func mapHTTPError(op string, err error, mappings []sentinelMapping) error {
	for _, m := range mappings {
		if errors.Is(err, m.sentinel) {
			if m.hideErr {
				return m.fn(op)
			}
			return m.fn(op, err)
		}
	}
	return huma.Error500InternalServerError(op, err)
}

// HealthResponse is used by both /healthz and /readyz.
type HealthResponse struct {
	Body struct {
		Status string `json:"status" example:"ok"`
	}
}

func (h *Handlers) Health(ctx context.Context, in *struct{}) (*HealthResponse, error) {
	out := &HealthResponse{}
	out.Body.Status = "ok"
	return out, nil
}

// McpInfo returns MCP server metadata and tool catalog for the UI (GET /mcp/info).
func (h *Handlers) McpInfo(ctx context.Context, in *struct{}) (*McpInfoResponse, error) {
	_ = in
	out := &McpInfoResponse{}
	if h.Settings != nil {
		out.Body.Addr = h.Settings.Get(settings.KeyMCPAddr)
		out.Body.RestartRequired = h.MCPBootEnabled != mcp.ResolveEnabled(h.Settings)
	}
	// repo_* tools are advertised iff at least one GitLab integration repo is
	// configured (mirrors runtime registration in NewMCPServer — repo providers
	// come solely from integrations).
	gitLabForCatalog := h.hasGitLabRepoConfigs(ctx)
	for _, t := range mcp.InfoToolCatalog(gitLabForCatalog) {
		out.Body.Tools = append(out.Body.Tools, McpToolDTO{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}
	if h.Skills != nil {
		for _, r := range mcp.InfoResourceCatalog(true) {
			out.Body.Resources = append(out.Body.Resources, McpResourceDTO{
				URI:         r.URI,
				URITemplate: r.URITemplate,
				Name:        r.Name,
				Description: r.Description,
				MimeType:    r.MimeType,
			})
		}
	}
	return out, nil
}

// hasGitLabRepoConfigs reports whether any connected GitLab integration exposes
// a repo-reading config. It mirrors the runtime repo_* tool registration, which
// builds repo providers solely from GitLab integrations.
func (h *Handlers) hasGitLabRepoConfigs(ctx context.Context) bool {
	if h.Integrations == nil {
		return false
	}
	configs, err := h.Integrations.ListGitLabRepoConfigs(ctx)
	return err == nil && len(configs) > 0
}

func (h *Handlers) Ready(ctx context.Context, in *struct{}) (*HealthResponse, error) {
	out := &HealthResponse{}
	out.Body.Status = "ready"
	return out, nil
}

func mapArtifactError(op string, err error) error {
	return mapHTTPError(op, err, []sentinelMapping{
		{artifact.ErrNotFound, huma.Error404NotFound, false},
		{artifact.ErrFileNotFound, huma.Error404NotFound, false},
		{artifact.ErrConflict, huma.Error409Conflict, false},
		{artifact.ErrStaleBase, huma.Error409Conflict, false},
		{artifact.ErrInvalidPath, huma.Error422UnprocessableEntity, false},
		{artifact.ErrApprovalRequiresHuman, huma.Error403Forbidden, false},
	})
}

func mapKnowledgeError(op string, err error) error {
	return mapHTTPError(op, err, []sentinelMapping{
		{knowledge.ErrNotFound, huma.Error404NotFound, false},
		{knowledge.ErrValidation, huma.Error400BadRequest, false},
	})
}

func mapWorkBoardError(op string, err error) error {
	return mapHTTPError(op, err, []sentinelMapping{
		{workboard.ErrNotFound, huma.Error404NotFound, false},
		{workboard.ErrValidation, huma.Error400BadRequest, false},
	})
}
