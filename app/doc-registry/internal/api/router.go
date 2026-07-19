package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"github.com/specgate/doc-registry/internal/config"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/observability"
)

// Router wires REST endpoints per spec §6 and registers them on a Huma API
// with generated OpenAPI 3.1 and Scalar reference pages.
//
// Local dev: no HTTP authentication — all routes are open (see docs).
type Router struct {
	Handlers *Handlers
	Config   *config.Config
	// SentryMiddleware optional — when set, replaces chi Recoverer for panic reporting (spec §13).
	SentryMiddleware func(http.Handler) http.Handler
	// Logger is used for per-request access logging. Nil disables HTTP logging
	// (kept optional so tests can build a Router without threading a logger).
	Logger *zerolog.Logger
}

func (rt *Router) Build() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	if rt.SentryMiddleware != nil {
		r.Use(rt.SentryMiddleware)
		r.Use(observability.SentryRequestIDTags())
	} else {
		r.Use(middleware.Recoverer)
	}
	if rt.Logger != nil {
		r.Use(RequestLogger(*rt.Logger))
	}
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(cliWorkspaceMiddleware)

	// Resolve the OAuth callback base URL per request (env override else derived
	// from the request) so both the authorize and callback handlers agree.
	var oauthCallbackOverride string
	if rt.Handlers != nil {
		oauthCallbackOverride = rt.Handlers.OAuthCallbackBaseURL
	}
	r.Use(oauthCallbackBaseMiddleware(oauthCallbackOverride))

	humaCfg := huma.DefaultConfig("Doc Registry", "1.0.0")
	if rt.Config.OpenAPI.Enabled {
		humaCfg.DocsPath = "/docs"
		humaCfg.DocsRenderer = huma.DocsRendererScalar
		humaCfg.OpenAPIPath = "/openapi"
	} else {
		// Empty paths disable the reference UI and OpenAPI document. Routes still register
		// and serve traffic normally.
		humaCfg.DocsPath = ""
		humaCfg.OpenAPIPath = ""
	}

	api := humachi.New(r, humaCfg)
	rt.registerRoutes(api)
	rt.registerOAuthRoutes(r)
	registerAgentPackageRoutes(r)
	r.Get("/cli/install.sh", serveCLIInstallScript)
	if rt.Handlers != nil {
		// Governance file bodies are larger binary uploads, so they retain raw
		// content routes outside the JSON API.
		r.Get("/governance/files/{id}/content", rt.Handlers.ServeGovernanceFileContent)
		r.Post("/governance/files/upload", rt.Handlers.UploadGovernanceFile)
	}

	return r
}

func (rt *Router) registerOAuthRoutes(r chi.Router) {
	h := rt.Handlers

	// GET /integrations/oauth-callback is a redirect handler (302 response) that
	// cannot be expressed as a huma JSON operation, so it lives here on the plain
	// chi router. The authorize and disconnect endpoints are registered via huma
	// in registerRoutes for OpenAPI visibility.
	r.Get("/integrations/oauth-callback", func(w http.ResponseWriter, req *http.Request) {
		if h == nil || h.Integrations == nil {
			http.Error(w, "integrations service not configured", http.StatusServiceUnavailable)
			return
		}
		// The callback is served by the backend, which in dev is a different
		// origin than the SPA, so redirect to the UI's public origin
		// (APP_BASE_URL) joined with the validated app-relative target.
		// Empty AppBaseURL keeps it relative (same-origin / reverse-proxy prod).
		redirectToApp := func(rel string) {
			loc := rel
			if base := strings.TrimRight(strings.TrimSpace(h.AppBaseURL), "/"); base != "" {
				loc = base + rel
			}
			w.Header().Set("Location", loc)
			w.WriteHeader(http.StatusFound)
		}
		result, err := h.Integrations.CompleteOAuthCallback(req.Context(), req.URL.Query().Get("state"), req.URL.Query().Get("code"), oauthCallbackBaseFromContext(req.Context()))
		if err != nil {
			// Log the real error server-side so it is visible in container logs
			// even though we never leak the detail into the redirect URL.
			if rt.Logger != nil {
				rt.Logger.Error().Err(err).Str("path", req.URL.Path).Msg("oauth callback failed")
			}
			redirectToApp(integrations.OAuthErrorRedirect())
			return
		}
		redirectToApp(integrations.OAuthResultRedirect(result.RedirectTarget, "oauth", "connected"))
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (rt *Router) registerRoutes(api huma.API) {
	rt.registerSystemRoutes(api)
	rt.registerSkillSettingsRoutes(api)
	rt.registerIntegrationRoutes(api)
	rt.registerGovernanceRoutes(api)
	rt.registerArtifactRoutes(api)
	rt.registerWorkBoardRoutes(api)
	rt.registerKnowledgeRoutes(api)
	rt.registerCLIRoutes(api)
	rt.registerOptionalRoutes(api)
}

func (rt *Router) registerSystemRoutes(api huma.API) {
	h := rt.Handlers

	huma.Register(api, huma.Operation{
		OperationID: "maintenance_cleanup",
		Method:      http.MethodPost,
		Path:        "/maintenance/cleanup",
		Summary:     "Housekeeping cleanup: immediate retention sweep, demo seed removal, archived change-request purge (per spec §9; never touches approved/draft artifacts, active features, or non-archived work)",
		Tags:        []string{"system"},
	}, h.MaintenanceCleanup)

	huma.Register(api, huma.Operation{
		OperationID: "maintenance_demo_remove",
		Method:      http.MethodPost,
		Path:        "/maintenance/demo-remove",
		Summary:     "Remove bundled demo seed data: the mirror of the demo seed, idempotent, touching only the fixed demo IDs (per spec §9). Returns per-category deletion counts",
		Tags:        []string{"system"},
	}, h.MaintenanceDemoRemove)

	huma.Register(api, huma.Operation{
		OperationID: "health",
		Method:      http.MethodGet,
		Path:        "/healthz",
		Summary:     "Liveness probe",
		Tags:        []string{"system"},
	}, h.Health)

	huma.Register(api, huma.Operation{
		OperationID: "ready",
		Method:      http.MethodGet,
		Path:        "/readyz",
		Summary:     "Readiness probe",
		Tags:        []string{"system"},
	}, h.Ready)

	huma.Register(api, huma.Operation{
		OperationID: "schema_status",
		Method:      http.MethodGet,
		Path:        "/api/v1/schema/status",
		Summary:     "Database schema compatibility check for current server code",
		Tags:        []string{"system"},
	}, h.SchemaStatusCheck)
}

func (rt *Router) registerSkillSettingsRoutes(api huma.API) {
	h := rt.Handlers

	huma.Register(api, huma.Operation{
		OperationID: "list_skills",
		Method:      http.MethodGet,
		Path:        "/skills",
		Summary:     "List user-defined skills",
		Tags:        []string{"skills"},
	}, h.ListSkills)

	huma.Register(api, huma.Operation{
		OperationID: "create_skill",
		Method:      http.MethodPost,
		Path:        "/skills",
		Summary:     "Create a skill",
		Tags:        []string{"skills"},
	}, h.CreateSkill)

	huma.Register(api, huma.Operation{
		OperationID: "update_skill",
		Method:      http.MethodPut,
		Path:        "/skills/{id}",
		Summary:     "Replace a skill",
		Tags:        []string{"skills"},
	}, h.UpdateSkill)

	huma.Register(api, huma.Operation{
		OperationID: "delete_skill",
		Method:      http.MethodDelete,
		Path:        "/skills/{id}",
		Summary:     "Delete a skill",
		Tags:        []string{"skills"},
	}, h.DeleteSkill)

	huma.Register(api, huma.Operation{
		OperationID: "get_settings",
		Method:      http.MethodGet,
		Path:        "/settings",
		Summary:     "List settings (secrets masked except for the trusted governance service)",
		Tags:        []string{"settings"},
	}, h.GetSettings)

	huma.Register(api, huma.Operation{
		OperationID: "update_settings",
		Method:      http.MethodPut,
		Path:        "/settings",
		Summary:     "Bulk update settings",
		Tags:        []string{"settings"},
	}, h.UpdateSettings)
}

func (rt *Router) registerIntegrationRoutes(api huma.API) {
	h := rt.Handlers

	huma.Register(api, huma.Operation{
		OperationID: "list_integrations",
		Method:      http.MethodGet,
		Path:        "/integrations",
		Summary:     "List native workflow integrations",
		Tags:        []string{"integrations"},
	}, h.ListIntegrations)

	huma.Register(api, huma.Operation{
		OperationID: "create_integration",
		Method:      http.MethodPost,
		Path:        "/integrations",
		Summary:     "Create a native workflow integration",
		Tags:        []string{"integrations"},
	}, h.CreateIntegration)

	huma.Register(api, huma.Operation{
		OperationID: "get_integration",
		Method:      http.MethodGet,
		Path:        "/integrations/{id}",
		Summary:     "Get a native workflow integration",
		Tags:        []string{"integrations"},
	}, h.GetIntegration)

	huma.Register(api, huma.Operation{
		OperationID: "update_integration",
		Method:      http.MethodPut,
		Path:        "/integrations/{id}",
		Summary:     "Replace a native workflow integration",
		Tags:        []string{"integrations"},
	}, h.UpdateIntegration)

	huma.Register(api, huma.Operation{
		OperationID:   "delete_integration",
		Method:        http.MethodDelete,
		Path:          "/integrations/{id}",
		Summary:       "Delete a native workflow integration (cascades credentials, resources, webhook events, delivery links, governance feedback)",
		Tags:          []string{"integrations"},
		DefaultStatus: 204,
	}, h.DeleteIntegration)

	huma.Register(api, huma.Operation{
		OperationID: "list_integration_resources",
		Method:      http.MethodGet,
		Path:        "/integrations/{id}/resources",
		Summary:     "List linked external resources for an integration",
		Tags:        []string{"integrations"},
	}, h.ListIntegrationResources)

	huma.Register(api, huma.Operation{
		OperationID: "list_integration_repos",
		Method:      http.MethodGet,
		Path:        "/integrations/{id}/repos",
		Summary:     "List repos/projects the integration's token can access (GitLab/GitHub), for the connect-time picker",
		Tags:        []string{"integrations"},
	}, h.ListIntegrationRepos)

	huma.Register(api, huma.Operation{
		OperationID: "list_linear_teams",
		Method:      http.MethodGet,
		Path:        "/integrations/{id}/linear/teams",
		Summary:     "List Linear teams the integration credential can access, for the team/project picker",
		Tags:        []string{"integrations"},
	}, h.ListLinearTeams)

	huma.Register(api, huma.Operation{
		OperationID: "list_linear_projects",
		Method:      http.MethodGet,
		Path:        "/integrations/{id}/linear/projects",
		Summary:     "List Linear projects for one selected team, for the optional project picker",
		Tags:        []string{"integrations"},
	}, h.ListLinearProjects)

	huma.Register(api, huma.Operation{
		OperationID: "create_integration_resource",
		Method:      http.MethodPost,
		Path:        "/integrations/{id}/resources",
		Summary:     "Create a linked external resource for an integration",
		Tags:        []string{"integrations"},
	}, h.CreateIntegrationResource)

	huma.Register(api, huma.Operation{
		OperationID: "reprovision_integration_resource_webhook",
		Method:      http.MethodPost,
		Path:        "/integrations/{id}/resources/{resource_id}/reprovision-webhook",
		Summary:     "(Re)register the provider webhook for an existing resource; records webhook_last_error on failure (resource kept)",
		Tags:        []string{"integrations"},
	}, h.ReprovisionIntegrationResourceWebhook)

	huma.Register(api, huma.Operation{
		OperationID:   "delete_integration_resource",
		Method:        http.MethodDelete,
		Path:          "/integrations/{id}/resources/{resource_id}",
		Summary:       "Delete a linked external resource for an integration (strictly removes provider webhook first when managed)",
		Tags:          []string{"integrations"},
		DefaultStatus: http.StatusNoContent,
	}, h.DeleteIntegrationResource)

	huma.Register(api, huma.Operation{
		OperationID: "list_integration_webhook_events",
		Method:      http.MethodGet,
		Path:        "/integrations/{id}/webhook-events",
		Summary:     "List recorded webhook events for an integration",
		Tags:        []string{"integrations"},
	}, h.ListIntegrationWebhookEvents)

	huma.Register(api, huma.Operation{
		OperationID: "record_integration_webhook_event",
		Method:      http.MethodPost,
		Path:        "/integrations/{id}/webhook-events",
		Summary:     "Record a webhook event for an integration",
		Tags:        []string{"integrations"},
	}, h.RecordIntegrationWebhookEvent)

	huma.Register(api, huma.Operation{
		OperationID:  "handle_gitlab_resource_webhook",
		Method:       http.MethodPost,
		Path:         "/integrations/{id}/resources/{resource_id}/gitlab/webhook",
		Summary:      "Receive a resource-scoped GitLab webhook and emit governance feedback (verifies the resource signing token)",
		Tags:         []string{"integrations"},
		MaxBodyBytes: gitLabWebhookMaxBodyBytes,
	}, h.HandleGitLabResourceWebhook)

	huma.Register(api, huma.Operation{
		OperationID:  "handle_github_resource_webhook",
		Method:       http.MethodPost,
		Path:         "/integrations/{id}/resources/{resource_id}/github/webhook",
		Summary:      "Receive a resource-scoped GitHub webhook and emit governance feedback (verifies the resource secret)",
		Tags:         []string{"integrations"},
		MaxBodyBytes: gitLabWebhookMaxBodyBytes,
	}, h.HandleGitHubResourceWebhook)

	huma.Register(api, huma.Operation{
		OperationID:  "handle_linear_resource_webhook",
		Method:       http.MethodPost,
		Path:         "/integrations/{id}/resources/{resource_id}/linear/webhook",
		Summary:      "Receive a resource-scoped Linear webhook and emit tracker feedback (verifies the per-resource secret)",
		Tags:         []string{"integrations"},
		MaxBodyBytes: gitLabWebhookMaxBodyBytes,
	}, h.HandleLinearResourceWebhook)

	huma.Register(api, huma.Operation{
		OperationID:   "set_api_token",
		Method:        http.MethodPut,
		Path:          "/integrations/{id}/api-token",
		Summary:       "Set or rotate the provider API token for outbound calls (GitHub / GitLab / Linear; write-only, stored encrypted)",
		Tags:          []string{"integrations"},
		DefaultStatus: http.StatusNoContent,
	}, h.SetApiToken)

	huma.Register(api, huma.Operation{
		OperationID: "begin_integration_oauth",
		Method:      http.MethodPost,
		Path:        "/integrations/{id}/oauth/authorize",
		Summary:     "Begin OAuth re-authorization for an existing integration; returns the provider authorize URL",
		Tags:        []string{"integrations"},
	}, h.BeginIntegrationOAuth)

	huma.Register(api, huma.Operation{
		OperationID: "begin_pending_integration_oauth",
		Method:      http.MethodPost,
		Path:        "/integrations/oauth/begin",
		Summary:     "Begin OAuth for a not-yet-created integration (create-on-callback); returns the provider authorize URL",
		Tags:        []string{"integrations"},
	}, h.BeginPendingIntegrationOAuth)

	huma.Register(api, huma.Operation{
		OperationID:   "disconnect_integration_oauth",
		Method:        http.MethodPost,
		Path:          "/integrations/{id}/oauth/disconnect",
		Summary:       "Disconnect OAuth and clear the stored OAuth grant for an integration",
		Tags:          []string{"integrations"},
		DefaultStatus: http.StatusNoContent,
	}, h.DisconnectIntegrationOAuth)

	huma.Register(api, huma.Operation{
		OperationID: "get_repo_file",
		Method:      http.MethodGet,
		Path:        "/repos/file",
		Summary:     "Read one repository file through the integration-credentialed GitLab provider (token stays server-side)",
		Tags:        []string{"integrations"},
	}, h.GetRepoFile)
}

func (rt *Router) registerGovernanceRoutes(api huma.API) {
	h := rt.Handlers

	huma.Register(api, huma.Operation{
		OperationID: "list_governance_feedback_events",
		Method:      http.MethodGet,
		Path:        "/governance/feedback-events",
		Summary:     "List integration feedback events for governance-ops",
		Tags:        []string{"governance"},
	}, h.ListGovernanceFeedbackEvents)

	huma.Register(api, huma.Operation{
		OperationID: "update_governance_feedback_event_status",
		Method:      http.MethodPost,
		Path:        "/governance/feedback-events/{id}/status",
		Summary:     "Set a feedback event's triage status (resolve/dismiss)",
		Tags:        []string{"governance"},
	}, h.UpdateGovernanceFeedbackEventStatus)

	huma.Register(api, huma.Operation{
		OperationID: "list_governance_threads",
		Method:      http.MethodGet,
		Path:        "/governance/threads",
		Summary:     "List lightweight governance-chat thread summaries",
		Tags:        []string{"governance"},
	}, h.ListGovernanceThreads)

	huma.Register(api, huma.Operation{
		OperationID: "upsert_governance_thread",
		Method:      http.MethodPut,
		Path:        "/governance/threads/{thread_id}",
		Summary:     "Upsert a lightweight governance-chat thread summary",
		Tags:        []string{"governance"},
	}, h.UpsertGovernanceThread)

	huma.Register(api, huma.Operation{
		OperationID:   "delete_governance_thread",
		Method:        http.MethodDelete,
		Path:          "/governance/threads/{thread_id}",
		Summary:       "Archive a lightweight governance-chat thread summary",
		Tags:          []string{"governance"},
		DefaultStatus: 204,
	}, h.DeleteGovernanceThread)

	huma.Register(api, huma.Operation{
		OperationID: "governance_presign_file",
		Method:      http.MethodPost,
		Path:        "/governance/files/presign",
		Summary:     "Presign object-store upload for an internal governance file",
		Tags:        []string{"governance"},
	}, h.PresignFile)

	huma.Register(api, huma.Operation{
		OperationID: "governance_commit_file",
		Method:      http.MethodPost,
		Path:        "/governance/files/{id}/commit",
		Summary:     "Mark a presigned governance file as uploaded (agent flow); no object-store URL returned",
		Tags:        []string{"governance"},
	}, h.CommitFile)

	huma.Register(api, huma.Operation{
		OperationID: "governance_list_files",
		Method:      http.MethodGet,
		Path:        "/governance/files",
		Summary:     "List internal governance files (ready, by last_used_at desc)",
		Tags:        []string{"governance"},
	}, h.ListFiles)

	huma.Register(api, huma.Operation{
		OperationID: "governance_touch_file",
		Method:      http.MethodPost,
		Path:        "/governance/files/{id}/touch",
		Summary:     "Refresh last_used_at on an internal governance file; no object-store URL returned",
		Tags:        []string{"governance"},
	}, h.TouchFile)

	huma.Register(api, huma.Operation{
		OperationID:   "governance_delete_file",
		Method:        http.MethodDelete,
		Path:          "/governance/files/{id}",
		Summary:       "Delete an internal governance file (row + best-effort object)",
		Tags:          []string{"governance"},
		DefaultStatus: 204,
	}, h.DeleteFile)

	huma.Register(api, huma.Operation{
		OperationID: "governance_context_search",
		Method:      http.MethodPost,
		Path:        "/governance/context/search",
		Summary:     "Search indexed Governance Knowledge chunks",
		Tags:        []string{"governance"},
	}, h.GovernanceContextSearch)

	huma.Register(api, huma.Operation{
		OperationID: "check_conflicts",
		Method:      http.MethodGet,
		Path:        "/conflicts",
		Summary:     "Check conflicts for impacted services (Governance)",
		Tags:        []string{"conflicts"},
	}, h.CheckConflicts)

	huma.Register(api, huma.Operation{
		OperationID: "list_events",
		Method:      http.MethodGet,
		Path:        "/events",
		Summary:     "Poll the artifact event log",
		Tags:        []string{"events"},
	}, h.ListEvents)
}

func (rt *Router) registerArtifactRoutes(api huma.API) {
	h := rt.Handlers

	huma.Register(api, huma.Operation{
		OperationID: "list_artifacts",
		Method:      http.MethodGet,
		Path:        "/artifacts",
		Summary:     "List artifacts with optional filters",
		Tags:        []string{"artifacts"},
	}, h.ListArtifacts)

	huma.Register(api, huma.Operation{
		OperationID: "get_artifact",
		Method:      http.MethodGet,
		Path:        "/artifacts/{id}",
		Summary:     "Get an artifact by ID",
		Tags:        []string{"artifacts"},
	}, h.GetArtifact)

	huma.Register(api, huma.Operation{
		OperationID: "update_status",
		Method:      http.MethodPatch,
		Path:        "/artifacts/{id}/status",
		Summary:     "Record an artifact approval or request for changes",
		Tags:        []string{"artifacts"},
	}, h.UpdateStatus)

	huma.Register(api, huma.Operation{
		OperationID: "list_artifact_files",
		Method:      http.MethodGet,
		Path:        "/artifacts/{id}/files",
		Summary:     "List an artifact's documents with role metadata",
		Tags:        []string{"artifacts"},
	}, h.ListArtifactFiles)

	huma.Register(api, huma.Operation{
		OperationID: "refresh_artifact_readiness_runs",
		Method:      http.MethodPost,
		Path:        "/artifacts/{id}/readiness-runs/refresh",
		Summary:     "Persist artifact-scoped readiness evaluations",
		Tags:        []string{"artifacts"},
	}, h.RefreshArtifactReadinessRuns)

	huma.Register(api, huma.Operation{
		OperationID: "list_artifact_readiness_runs",
		Method:      http.MethodGet,
		Path:        "/artifacts/{id}/readiness-runs",
		Summary:     "List persisted artifact-scoped readiness runs",
		Tags:        []string{"artifacts"},
	}, h.ListArtifactReadinessRuns)

	huma.Register(api, huma.Operation{
		OperationID: "get_artifact_file",
		Method:      http.MethodGet,
		Path:        "/artifacts/{id}/files/_",
		Summary:     "Get a signed object-store URL for an artifact file",
		Tags:        []string{"artifacts"},
	}, h.GetArtifactFile)

	huma.Register(api, huma.Operation{
		OperationID: "create_feature_attachment",
		Method:      http.MethodPost,
		Path:        "/features/{id}/attachments",
		Summary:     "Pin a reference attachment (link/file/image) to a feature",
		Tags:        []string{"artifacts"},
	}, h.CreateFeatureAttachment)

	huma.Register(api, huma.Operation{
		OperationID: "list_feature_attachments",
		Method:      http.MethodGet,
		Path:        "/features/{id}/attachments",
		Summary:     "List a feature's reference attachments (newest first)",
		Tags:        []string{"artifacts"},
	}, h.ListFeatureAttachments)

	huma.Register(api, huma.Operation{
		OperationID: "delete_feature_attachment",
		Method:      http.MethodDelete,
		Path:        "/attachments/{id}",
		Summary:     "Delete a feature reference attachment",
		Tags:        []string{"artifacts"},
	}, h.DeleteFeatureAttachment)
}

func (rt *Router) registerWorkBoardRoutes(api huma.API) {
	h := rt.Handlers

	huma.Register(api, huma.Operation{
		OperationID: "linear_handoff_change_request",
		Method:      http.MethodPost,
		Path:        "/workboard/change-requests/{id}/linear-handoff",
		Summary:     "Create or return the selected-team Linear issue for a Ready work item",
		Tags:        []string{"governance"},
	}, h.HandoffLinear)

	huma.Register(api, huma.Operation{
		OperationID: "list_change_request_tracker_links",
		Method:      http.MethodGet,
		Path:        "/workboard/change-requests/{id}/tracker-links",
		Summary:     "List the tracker issue links a handoff created for a work item",
		Tags:        []string{"governance"},
	}, h.ListChangeRequestTrackerLinks)

	huma.Register(api, huma.Operation{
		OperationID: "list_change_request_delivery_links",
		Method:      http.MethodGet,
		Path:        "/workboard/change-requests/{id}/delivery-links",
		Summary:     "List persisted repository delivery links for a work item",
		Tags:        []string{"governance"},
	}, h.ListChangeRequestDeliveryLinks)

	huma.Register(api, huma.Operation{OperationID: "list_workboard_features", Method: http.MethodGet, Path: "/workboard/features", Summary: "List governance Features", Tags: []string{"workboard"}}, h.ListFeatures)
	huma.Register(api, huma.Operation{OperationID: "create_workboard_feature", Method: http.MethodPost, Path: "/workboard/features", Summary: "Create governance Feature", Tags: []string{"workboard"}}, h.CreateFeature)
	huma.Register(api, huma.Operation{OperationID: "upsert_workboard_feature_by_key", Method: http.MethodPost, Path: "/workboard/features/upsert-by-key", Summary: "Upsert governance Feature by stable key (create-or-get, idempotent)", Tags: []string{"workboard"}}, h.UpsertFeatureByKey)
	huma.Register(api, huma.Operation{OperationID: "get_workboard_feature", Method: http.MethodGet, Path: "/workboard/features/{id}", Summary: "Get governance Feature", Tags: []string{"workboard"}}, h.GetFeature)
	huma.Register(api, huma.Operation{OperationID: "patch_workboard_feature", Method: http.MethodPatch, Path: "/workboard/features/{id}", Summary: "Patch governance Feature", Tags: []string{"workboard"}}, h.PatchFeature)
	huma.Register(api, huma.Operation{OperationID: "promote_artifact_to_canonical", Method: http.MethodPost, Path: "/workboard/artifacts/{id}/promote-canonical", Summary: "Promote an approved artifact to its feature's canonical", Tags: []string{"workboard"}}, h.PromoteArtifactToCanonical)

	huma.Register(api, huma.Operation{OperationID: "list_change_requests", Method: http.MethodGet, Path: "/workboard/change-requests", Summary: "List ChangeRequests (archived hidden by default; set include_archived=true to include)", Tags: []string{"workboard"}}, h.ListChangeRequests)
	huma.Register(api, huma.Operation{OperationID: "create_change_request", Method: http.MethodPost, Path: "/workboard/change-requests", Summary: "Create ChangeRequest", Tags: []string{"workboard"}}, h.CreateChangeRequest)
	huma.Register(api, huma.Operation{OperationID: "get_change_request", Method: http.MethodGet, Path: "/workboard/change-requests/{id}", Summary: "Get ChangeRequest", Tags: []string{"workboard"}}, h.GetChangeRequest)
	huma.Register(api, huma.Operation{OperationID: "list_change_request_acceptance_criteria", Method: http.MethodGet, Path: "/workboard/change-requests/{id}/acceptance-criteria", Summary: "List ChangeRequest acceptance criteria", Tags: []string{"workboard"}}, h.ListAcceptanceCriteria)
	huma.Register(api, huma.Operation{OperationID: "change_request_next_actions", Method: http.MethodGet, Path: "/workboard/change-requests/{id}/next-actions", Summary: "List derived next actions for ChangeRequest gates", Tags: []string{"workboard"}}, h.NextActions)
	huma.Register(api, huma.Operation{OperationID: "refresh_change_request_gate_runs", Method: http.MethodPost, Path: "/workboard/change-requests/{id}/gate-runs/refresh", Summary: "Persist and return a gate snapshot for ChangeRequest", Tags: []string{"workboard"}}, h.RefreshGateRuns)
	huma.Register(api, huma.Operation{OperationID: "list_change_request_gate_runs", Method: http.MethodGet, Path: "/workboard/change-requests/{id}/gate-runs", Summary: "List persisted gate snapshots for ChangeRequest", Tags: []string{"workboard"}}, h.ListGateRuns)
	huma.Register(api, huma.Operation{OperationID: "patch_change_request", Method: http.MethodPatch, Path: "/workboard/change-requests/{id}", Summary: "Patch ChangeRequest", Tags: []string{"workboard"}}, h.PatchChangeRequest)
	huma.Register(api, huma.Operation{OperationID: "unarchive_change_request", Method: http.MethodPost, Path: "/workboard/change-requests/{id}/unarchive", Summary: "Restore an archived ChangeRequest (audited)", Tags: []string{"workboard"}}, h.UnarchiveChangeRequest)

	huma.Register(api, huma.Operation{
		OperationID: "list_workboard_stale_warnings",
		Method:      http.MethodGet,
		Path:        "/workboard/stale-warnings",
		Summary:     "List centralized WorkBoard attention warnings",
		Tags:        []string{"workboard"},
	}, h.ListWorkBoardStaleWarnings)
}

func (rt *Router) registerKnowledgeRoutes(api huma.API) {
	h := rt.Handlers
	maxBodyBytes := int64(11 << 20)
	if rt.Config != nil && rt.Config.Knowledge.MaxFileBytes > 0 {
		maxBodyBytes = rt.Config.Knowledge.MaxFileBytes + (1 << 20)
	}

	huma.Register(api, huma.Operation{
		OperationID:   "upload_document",
		Method:        http.MethodPost,
		Path:          "/documents/upload",
		Summary:       "Upload a Governance Knowledge document file",
		Description:   "Persists the version (status uploaded) and starts ingestion via the queue driver; returns 202. Poll GET /documents/{id} for the terminal status.",
		Tags:          []string{"documents"},
		DefaultStatus: http.StatusAccepted,
		MaxBodyBytes:  maxBodyBytes,
	}, h.CreateUploadDocument)

	huma.Register(api, huma.Operation{
		OperationID:   "create_text_document",
		Method:        http.MethodPost,
		Path:          "/documents/text",
		Summary:       "Create a Governance Knowledge text document",
		Description:   "Persists the version (status uploaded) and starts ingestion via the queue driver; returns 202. Poll GET /documents/{id} for the terminal status.",
		Tags:          []string{"documents"},
		DefaultStatus: http.StatusAccepted,
		MaxBodyBytes:  maxBodyBytes,
	}, h.CreateTextDocument)

	huma.Register(api, huma.Operation{
		OperationID: "list_documents",
		Method:      http.MethodGet,
		Path:        "/documents",
		Summary:     "List Governance Knowledge documents",
		Tags:        []string{"documents"},
	}, h.ListKnowledgeDocuments)

	huma.Register(api, huma.Operation{
		OperationID: "get_document",
		Method:      http.MethodGet,
		Path:        "/documents/{document_id}",
		Summary:     "Get Governance Knowledge document detail",
		Tags:        []string{"documents"},
	}, h.GetKnowledgeDocument)

	huma.Register(api, huma.Operation{
		OperationID:   "create_document_version",
		Method:        http.MethodPost,
		Path:          "/documents/{document_id}/versions",
		Summary:       "Create a new Governance Knowledge text version",
		Description:   "Persists the version (status uploaded) and starts ingestion via the queue driver; returns 202. Poll GET /documents/{id} for the terminal status.",
		Tags:          []string{"documents"},
		DefaultStatus: http.StatusAccepted,
		MaxBodyBytes:  maxBodyBytes,
	}, h.CreateKnowledgeVersion)

	huma.Register(api, huma.Operation{
		OperationID:   "curate_document_links",
		Method:        http.MethodPost,
		Path:          "/documents/{document_id}/links",
		Summary:       "Create a new Governance Knowledge version with updated feature/request links",
		Description:   "Copies the selected/latest source version, changes only curation link metadata, persists the new version (status uploaded), and starts ingestion via the queue driver.",
		Tags:          []string{"documents"},
		DefaultStatus: http.StatusAccepted,
	}, h.CurateKnowledgeLinks)

	huma.Register(api, huma.Operation{
		OperationID:   "retry_document_ingest",
		Method:        http.MethodPost,
		Path:          "/documents/{document_id}/retry",
		Summary:       "Re-ingest a failed Governance Knowledge document version",
		Description:   "Re-runs ingestion for a document currently in status failed, without deleting the version; returns 202. Poll GET /documents/{id} for the terminal status.",
		Tags:          []string{"documents"},
		DefaultStatus: http.StatusAccepted,
	}, h.RetryKnowledgeDocument)

	huma.Register(api, huma.Operation{
		OperationID: "delete_document_version",
		Method:      http.MethodDelete,
		Path:        "/documents/{document_id}",
		Summary:     "Delete a Governance Knowledge document version",
		Tags:        []string{"documents"},
	}, h.DeleteKnowledgeDocument)
}

func (rt *Router) registerCLIRoutes(api huma.API) {
	h := rt.Handlers

	// --- /api/v1/ CLI REST facades (per spec §6 versioned endpoints) ---

	huma.Register(api, huma.Operation{
		OperationID: "v1_get_meta",
		Method:      http.MethodGet,
		Path:        "/api/v1/meta",
		Summary:     "Build metadata (version, commit, date)",
		Tags:        []string{"meta"},
	}, h.CLIMeta)

	huma.Register(api, huma.Operation{
		OperationID: "v1_get_governance_status",
		Method:      http.MethodGet,
		Path:        "/api/v1/status",
		Summary:     "Governance board phase counts and attention items",
		Tags:        []string{"governance"},
	}, h.CLIStatus)

	huma.Register(api, huma.Operation{
		OperationID: "v1_get_stats",
		Method:      http.MethodGet,
		Path:        "/api/v1/stats",
		Summary:     "Governance-value stats projected from existing gate runs and feedback events",
		Tags:        []string{"governance"},
	}, h.CLIStats)

	huma.Register(api, huma.Operation{
		OperationID: "v1_resolve_work_ref",
		Method:      http.MethodPost,
		Path:        "/api/v1/work-items/resolve",
		Summary:     "Resolve a work reference (CR ID, key, issue URL, or bare tracker key)",
		Tags:        []string{"work-items"},
	}, h.CLIResolveWorkRef)

	huma.Register(api, huma.Operation{
		OperationID: "v1_get_context_pack",
		Method:      http.MethodGet,
		Path:        "/api/v1/work-items/{id}/context-pack",
		Summary:     "Assemble the context pack for a change request",
		Tags:        []string{"work-items"},
	}, h.CLIContextPack)

	huma.Register(api, huma.Operation{
		OperationID: "v1_get_audit_trail",
		Method:      http.MethodGet,
		Path:        "/api/v1/audit/{ref}",
		Summary:     "Assemble the chronological governance audit trail for a work reference",
		Tags:        []string{"work-items"},
	}, h.CLIAudit)

	huma.Register(api, huma.Operation{
		OperationID: "v1_report_feedback",
		Method:      http.MethodPost,
		Path:        "/api/v1/work-items/{id}/feedback",
		Summary:     "Record a coding-agent feedback event",
		Tags:        []string{"work-items"},
	}, h.CLIReportFeedback)

	huma.Register(api, huma.Operation{
		OperationID: "v1_run_readiness",
		Method:      http.MethodPost,
		Path:        "/api/v1/work-items/{id}/readiness",
		Summary:     "Run readiness gates for an artifact via the agents service",
		Tags:        []string{"work-items"},
	}, h.CLIRunReadiness)

	huma.Register(api, huma.Operation{
		OperationID: "v1_run_llm_gates",
		Method:      http.MethodPost,
		Path:        "/api/v1/work-items/{id}/llm-gates",
		Summary:     "Trigger quality gates for a change request",
		Tags:        []string{"work-items"},
	}, h.CLIRunLLMGates)

	huma.Register(api, huma.Operation{
		OperationID: "v1_trigger_delivery_review",
		Method:      http.MethodPost,
		Path:        "/api/v1/work-items/{id}/delivery-review",
		Summary:     "Trigger the delivery review for a change request",
		Tags:        []string{"work-items"},
	}, h.CLITriggerDeliveryReview)

	huma.Register(api, huma.Operation{
		OperationID: "v1_delivery_decision",
		Method:      http.MethodPost,
		Path:        "/api/v1/work-items/{id}/delivery-decision",
		Summary:     "Record a human delivery approve or reject decision",
		Tags:        []string{"work-items"},
	}, h.CLIDeliveryDecision)

	huma.Register(api, huma.Operation{
		OperationID: "v1_create_work_item",
		Method:      http.MethodPost,
		Path:        "/api/v1/work-items",
		Summary:     "Create a quick-route change request from issue content",
		Tags:        []string{"work-items"},
	}, h.CLICreateQuickWorkItem)

	huma.Register(api, huma.Operation{
		OperationID: "v1_create_feature_work_item",
		Method:      http.MethodPost,
		Path:        "/api/v1/work-items/create",
		Summary:     "Create a feature-backed work item bound to the feature's canonical spec",
		Tags:        []string{"work-items"},
	}, h.CLIWorkCreate)

	huma.Register(api, huma.Operation{
		OperationID: "v1_archive_work_item",
		Method:      http.MethodPost,
		Path:        "/api/v1/work-items/{id}/archive",
		Summary:     "Archive a work item by CR ID, key, issue URL, or bare tracker key",
		Tags:        []string{"work-items"},
	}, h.CLIArchiveWorkItem)

	huma.Register(api, huma.Operation{
		OperationID: "v1_bootstrap_identity",
		Method:      http.MethodPost,
		Path:        "/api/v1/identity/bootstrap",
		Summary:     "Create or resolve the local user and workspace selection",
		Tags:        []string{"identity"},
	}, h.BootstrapIdentity)

	huma.Register(api, huma.Operation{
		OperationID: "v1_list_identity_users",
		Method:      http.MethodGet,
		Path:        "/api/v1/users",
		Summary:     "List local users",
		Tags:        []string{"identity"},
	}, h.ListIdentityUsers)

	huma.Register(api, huma.Operation{
		OperationID: "v1_get_identity_user",
		Method:      http.MethodGet,
		Path:        "/api/v1/users/{id}",
		Summary:     "Get a local user by ID or username",
		Tags:        []string{"identity"},
	}, h.GetIdentityUser)

	huma.Register(api, huma.Operation{
		OperationID: "v1_list_identity_workspaces",
		Method:      http.MethodGet,
		Path:        "/api/v1/workspaces",
		Summary:     "List workspaces",
		Tags:        []string{"identity"},
	}, h.ListIdentityWorkspaces)

	huma.Register(api, huma.Operation{
		OperationID: "v1_get_identity_workspace",
		Method:      http.MethodGet,
		Path:        "/api/v1/workspaces/{id}",
		Summary:     "Get a workspace by ID or slug",
		Tags:        []string{"identity"},
	}, h.GetIdentityWorkspace)

	huma.Register(api, huma.Operation{
		OperationID: "v1_list_workspace_members",
		Method:      http.MethodGet,
		Path:        "/api/v1/workspaces/{id}/members",
		Summary:     "List workspace members for cooperative audit visibility",
		Tags:        []string{"identity"},
	}, h.ListWorkspaceMembers)

	huma.Register(api, huma.Operation{
		OperationID:  "v1_publish_artifact",
		Method:       http.MethodPost,
		Path:         "/api/v1/artifacts/publish",
		Summary:      "Publish an artifact version; returns 409 when base_version is stale",
		Tags:         []string{"artifacts"},
		MaxBodyBytes: 12 << 20,
	}, h.CLIPublishArtifact)

	huma.Register(api, huma.Operation{
		OperationID: "v1_list_skills",
		Method:      http.MethodGet,
		Path:        "/api/v1/skills",
		Summary:     "List user-defined skills with optional name filter",
		Tags:        []string{"skills"},
	}, h.CLIListSkills)

	huma.Register(api, huma.Operation{
		OperationID: "v1_get_skill",
		Method:      http.MethodGet,
		Path:        "/api/v1/skills/{id}",
		Summary:     "Get a user-defined skill by ID",
		Tags:        []string{"skills"},
	}, h.CLIGetSkill)

	huma.Register(api, huma.Operation{
		OperationID: "v1_list_artifacts",
		Method:      http.MethodGet,
		Path:        "/api/v1/artifacts",
		Summary:     "List artifacts with optional feature_id / status filter",
		Tags:        []string{"artifacts"},
	}, h.CLIListArtifacts)

	huma.Register(api, huma.Operation{
		OperationID: "v1_get_artifact",
		Method:      http.MethodGet,
		Path:        "/api/v1/artifacts/{id}",
		Summary:     "Get a single artifact by ID",
		Tags:        []string{"artifacts"},
	}, h.CLIGetArtifact)

	huma.Register(api, huma.Operation{
		OperationID: "v1_list_artifact_files",
		Method:      http.MethodGet,
		Path:        "/api/v1/artifacts/{id}/files",
		Summary:     "List file metadata for an artifact",
		Tags:        []string{"artifacts"},
	}, h.CLIListArtifactFiles)

	huma.Register(api, huma.Operation{
		OperationID: "v1_get_artifact_file",
		Method:      http.MethodGet,
		Path:        "/api/v1/artifacts/{id}/files/_",
		Summary:     "Get artifact file content by explicit ?path=",
		Tags:        []string{"artifacts"},
	}, h.CLIGetArtifactFile)

	// Policy read and dry-run surfaces. per spec §6.
	huma.Register(api, huma.Operation{
		OperationID: "v1_list_policy_levels",
		Method:      http.MethodGet,
		Path:        "/api/v1/policies/levels",
		Summary:     "List all built-in governance policy tiers with their execution projections",
		Tags:        []string{"policies"},
	}, h.CLIListPolicyLevels)

	huma.Register(api, huma.Operation{
		OperationID: "v1_resolve_policy",
		Method:      http.MethodPost,
		Path:        "/api/v1/policies/resolve",
		Summary:     "Dry-run: resolve governance level and explanation for a proposed change (no persistence)",
		Tags:        []string{"policies"},
	}, h.CLIResolvePolicy)

	huma.Register(api, huma.Operation{
		OperationID: "v1_get_artifact_policy",
		Method:      http.MethodGet,
		Path:        "/api/v1/artifacts/{id}/policy",
		Summary:     "Get governance policy explanation for a published artifact",
		Tags:        []string{"artifacts"},
	}, h.CLIArtifactPolicy)

	huma.Register(api, huma.Operation{
		OperationID: "v1_get_work_item_policy",
		Method:      http.MethodGet,
		Path:        "/api/v1/work-items/{id}/policy",
		Summary:     "Get governance policy explanation for a work item",
		Tags:        []string{"work-items"},
	}, h.CLIWorkItemPolicy)

	huma.Register(api, huma.Operation{
		OperationID: "v1_get_work_item_gates",
		Method:      http.MethodGet,
		Path:        "/api/v1/work-items/{id}/gates",
		Summary:     "Get persisted gate state for a change request",
		Tags:        []string{"work-items"},
	}, h.CLIGatesStatus)

	huma.Register(api, huma.Operation{
		OperationID: "v1_get_work_item_gate_history",
		Method:      http.MethodGet,
		Path:        "/api/v1/work-items/{id}/gate-history",
		Summary:     "Get gate run history for a change request",
		Tags:        []string{"work-items"},
	}, h.CLIGateHistory)

	huma.Register(api, huma.Operation{
		OperationID: "v1_get_work_item_delivery_status",
		Method:      http.MethodGet,
		Path:        "/api/v1/work-items/{id}/delivery-status",
		Summary:     "Get the authoritative delivery review verdict for a change request",
		Tags:        []string{"work-items"},
	}, h.CLIDeliveryStatus)
}

func (rt *Router) registerOptionalRoutes(api huma.API) {
	h := rt.Handlers

	// Gate task routes (IDE agent pull/submit)
	if h.GateTaskStore != nil {
		h.registerGateTaskRoutes(api)
	}
}
