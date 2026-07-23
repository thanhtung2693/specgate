package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// Router wires REST endpoints per spec §6 and registers them on a Huma API
// with generated OpenAPI 3.1 and Scalar reference pages.
//
// Local dev: no HTTP authentication — all routes are open (see docs).
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
