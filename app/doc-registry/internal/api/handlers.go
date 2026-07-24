package api

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/artifactattachment"
	"github.com/specgate/doc-registry/internal/config"
	"github.com/specgate/doc-registry/internal/governancefiles"
	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/identity"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/knowledge"
	"github.com/specgate/doc-registry/internal/policy"
	"github.com/specgate/doc-registry/internal/settings"
	"github.com/specgate/doc-registry/internal/skills"
	blob "github.com/specgate/doc-registry/internal/storage/blob"
	stores3 "github.com/specgate/doc-registry/internal/storage/s3"
	"github.com/specgate/doc-registry/internal/workboard"
	"github.com/specgate/doc-registry/internal/workspace"
)

// Handlers carries the dependencies needed to serve HTTP requests. Individual
// operations live in domain-specific files (handlers_artifacts.go,
// handlers_knowledge.go, handlers_governance.go,
// handlers_settings.go, and handlers_skills.go.
type Handlers struct {
	Artifacts artifact.Service
	// WorkBoard owns durable Feature/ChangeRequest records for the artifact-native workboard.
	WorkBoard workboard.Store
	Knowledge *knowledge.Service
	// S3 is optional for artifact routes; required for governance image presign.
	S3 *stores3.Client
	// TTL for internal governance-file presigned PUT URLs (zero uses handler default).
	GovernanceUploadPutTTL time.Duration
	// BlobStore is used for governance file storage when STORAGE_DRIVER=local.
	// When nil, falls back to S3 (STORAGE_DRIVER=s3).
	BlobStore blob.Store
	// GovernanceFiles persists internal governance-file blobs used by explicit
	// feature attachments.
	GovernanceFiles governancefiles.Store
	// ArtifactAttachments persists feature-scoped reference attachments (links,
	// files, screenshots) surfaced to quality gates and the coding-agent handoff.
	ArtifactAttachments artifactattachment.Store
	// GovernanceUploadMaxBytes rejects uploads above this size.
	GovernanceUploadMaxBytes int64
	// S3KeyPrefix is prepended to every generated S3 key so a shared bucket
	// keeps doc-registry content under a dedicated directory (default
	// "doc-registry/"). Empty disables.
	S3KeyPrefix string

	// Settings service for GET/PUT /settings.
	Settings *settings.Service
	// Skills optional user-defined skills; nil when not configured.
	Skills *skills.Service
	// Integrations persists native source-control and workflow integrations.
	Integrations *integrations.Service
	// Identity stores local users and workspaces for attribution and selection.
	Identity identity.Store
	// SeedWorkspaceSkills installs missing built-in rubric Skills after identity
	// bootstrap creates or reuses a workspace.
	SeedWorkspaceSkills func(context.Context, string) error

	// AppBaseURL is the SpecGate UI origin used for generated app links.
	// Empty falls back to the dev default in link builders that allow it.
	AppBaseURL string
	// SchemaStatus checks whether the live DB has columns required by current publish code.
	SchemaStatus func(context.Context) (SchemaStatusDTO, error)

	// Governance is the shared application layer for governance operations,
	// consumed by the /api/v1/ CLI REST facades. nil disables those routes.
	Governance *governanceops.Service
	// GovernanceChatHealth reports whether the optional env-configured support
	// model is usable. Nil means the chat service is not present.
	GovernanceChatHealth func(context.Context) (bool, error)

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

	// MaintenanceCleanupFn runs the housekeeping workspace cleanup (retention
	// sweep + demo removal + archived change-request purge). Wired in main.go;
	// nil disables POST /maintenance/cleanup.
	MaintenanceCleanupFn func(context.Context, string) (MaintenanceCleanupCounts, error)

	// MaintenanceDemoRemoveFn removes the bundled demo seed data. Wired in
	// main.go; nil disables POST /maintenance/demo-remove.
	MaintenanceDemoRemoveFn func(context.Context, string) (MaintenanceDemoRemoveCounts, error)
}

func notImplemented(op string) error {
	return huma.Error501NotImplemented(op + " not implemented")
}

func requireWorkspaceID(workspaceID string) (string, error) {
	workspaceID, valid := workspace.NormalizeID(workspaceID)
	if !valid {
		return "", huma.Error400BadRequest("workspace_id is required and must be a safe path segment")
	}
	return workspaceID, nil
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

type SchemaStatusDTO struct {
	Status  string   `json:"status" example:"ok"`
	Message string   `json:"message"`
	Missing []string `json:"missing,omitempty"`
}

type SchemaStatusResponse struct {
	Body SchemaStatusDTO
}

func (h *Handlers) Health(ctx context.Context, in *struct{}) (*HealthResponse, error) {
	out := &HealthResponse{}
	out.Body.Status = "ok"
	return out, nil
}

func (h *Handlers) SchemaStatusCheck(ctx context.Context, in *struct{}) (*SchemaStatusResponse, error) {
	_ = in
	out := &SchemaStatusResponse{}
	if h.SchemaStatus == nil {
		out.Body = SchemaStatusDTO{Status: "unknown", Message: "database schema checker is not configured"}
		return out, nil
	}
	status, err := h.SchemaStatus(ctx)
	if err != nil {
		return nil, err
	}
	out.Body = status
	return out, nil
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
		{artifact.ErrWorkspaceMismatch, huma.Error400BadRequest, false},
		{artifact.ErrStaleBase, huma.Error409Conflict, false},
		{artifact.ErrInvalidPath, huma.Error422UnprocessableEntity, false},
		{artifact.ErrInvalidStatus, huma.Error422UnprocessableEntity, false},
		{artifact.ErrApprovalRequiresHuman, huma.Error403Forbidden, false},
		{artifact.ErrUnsupportedApprovalPolicy, huma.Error409Conflict, false},
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
		{workboard.ErrWorkspaceRequired, huma.Error400BadRequest, false},
		{workboard.ErrValidation, huma.Error400BadRequest, false},
	})
}
