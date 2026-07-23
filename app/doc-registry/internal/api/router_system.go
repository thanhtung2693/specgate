package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// Router wires REST endpoints per spec §6 and registers them on a Huma API
// with generated OpenAPI 3.1 and Scalar reference pages.
//
// Local dev: no HTTP authentication — all routes are open (see docs).
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
