package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// Router wires REST endpoints per spec §6 and registers them on a Huma API
// with generated OpenAPI 3.1 and Scalar reference pages.
//
// Local dev: no HTTP authentication — all routes are open (see docs).
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
