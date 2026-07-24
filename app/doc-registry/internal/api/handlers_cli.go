package api

import (
	"context"
	"errors"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/specgate/doc-registry/internal/buildinfo"
	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/settings"
	"github.com/specgate/doc-registry/internal/skills"
	"github.com/specgate/doc-registry/internal/workboard"
	"github.com/specgate/doc-registry/internal/workspace"
)

// requireGovernance returns HTTP 503 if the Governance service is not configured.
func (h *Handlers) requireGovernance() (*governanceops.Service, error) {
	if h.Governance == nil {
		return nil, huma.Error503ServiceUnavailable("governance service not configured")
	}
	return h.Governance, nil
}

// mapGovernanceError maps governanceops sentinel errors to huma HTTP errors.
func mapGovernanceError(op string, err error) error {
	return mapHTTPError(op, err, []sentinelMapping{
		{sentinel: governanceops.ErrNotFound, fn: huma.Error404NotFound},
		{sentinel: workboard.ErrNotFound, fn: huma.Error404NotFound},
		{sentinel: governanceops.ErrVersionConflict, fn: huma.Error409Conflict},
		{sentinel: workboard.ErrConflict, fn: huma.Error409Conflict},
		{sentinel: governanceprofile.ErrUnsupportedSnapshot, fn: huma.Error409Conflict},
		{sentinel: governanceprofile.ErrInvalidSnapshot, fn: huma.Error409Conflict},
		{sentinel: governanceops.ErrUnavailable, fn: huma.Error503ServiceUnavailable},
		{sentinel: governanceops.ErrValidation, fn: huma.Error400BadRequest},
		{sentinel: workboard.ErrValidation, fn: huma.Error400BadRequest},
	})
}

// CLIMeta handles GET /api/v1/meta — returns build information and server capabilities.
func (h *Handlers) CLIMeta(ctx context.Context, _ *struct{}) (*CLIMetaOutput, error) {
	out := &CLIMetaOutput{}
	out.Body.APIVersion = "specgate.api/v1"
	out.Body.ServerVersion = buildinfo.Version
	out.Body.RecommendedCLIVersion = buildinfo.Version
	out.Body.WebURL = strings.TrimSpace(h.AppBaseURL)
	out.Body.CapabilityDetails = h.cliCapabilityDetails(ctx)
	return out, nil
}

func (h *Handlers) cliCapabilityDetails(ctx context.Context) map[string]CapabilityDetail {
	hasAgents := h.Governance != nil && h.Governance.AgentsRunner != nil
	details := map[string]CapabilityDetail{
		"core":         {State: CapabilityStateAvailable},
		"agents":       capabilityPresence(hasAgents, "agents service is not configured"),
		"web_ui":       capabilityPresence(strings.TrimSpace(h.AppBaseURL) != "", "web UI origin is not configured"),
		"integrations": capabilityPresence(h.Integrations != nil, "integration service is not configured"),
		"knowledge":    capabilityPresence(h.Knowledge != nil, "knowledge service is not configured"),
	}
	details["governance_chat"] = h.governanceChatCapability(ctx)
	if !hasAgents {
		details["platform_model"] = CapabilityDetail{
			State:  CapabilityStateUnavailable,
			Reason: "agents service is not configured",
		}
	} else {
		details["platform_model"] = h.modelCapability()
	}
	details["semantic_search"] = h.embeddingCapability()
	return details
}

func (h *Handlers) governanceChatCapability(ctx context.Context) CapabilityDetail {
	if h.GovernanceChatHealth == nil {
		return CapabilityDetail{State: CapabilityStateUnavailable, Reason: "governance chat service is not configured"}
	}
	configured, err := h.GovernanceChatHealth(ctx)
	if err != nil {
		return CapabilityDetail{State: CapabilityStateUnavailable, Reason: "governance chat health is unavailable"}
	}
	if !configured {
		return CapabilityDetail{
			State:  CapabilityStateConfigurationRequired,
			Reason: "governance chat support model is not configured",
		}
	}
	return CapabilityDetail{State: CapabilityStateAvailable}
}

func capabilityPresence(present bool, reason string) CapabilityDetail {
	if present {
		return CapabilityDetail{State: CapabilityStateAvailable}
	}
	return CapabilityDetail{State: CapabilityStateUnavailable, Reason: reason}
}

func (h *Handlers) modelCapability() CapabilityDetail {
	if h.Settings == nil {
		return modelConfigurationRequired("model settings are not configured")
	}
	all := h.Settings.GetAll()
	if all[settings.KeyGovernanceModelEnabled] != "true" {
		return CapabilityDetail{State: CapabilityStateUnavailable, Reason: "platform model is disabled"}
	}
	provider := strings.TrimSpace(all[settings.KeyGovernanceModelProvider])
	model := strings.TrimSpace(all[settings.KeyGovernanceModel])
	if provider == "" || model == "" || strings.TrimSpace(all[providerAPIKey(provider)]) == "" {
		return modelConfigurationRequired("choose a model provider, model, and API key")
	}
	return CapabilityDetail{State: CapabilityStateAvailable}
}

func modelConfigurationRequired(reason string) CapabilityDetail {
	return CapabilityDetail{
		State:       CapabilityStateConfigurationRequired,
		Reason:      reason,
		NextCommand: "specgate model set",
	}
}

func (h *Handlers) embeddingCapability() CapabilityDetail {
	if h.Knowledge == nil {
		return CapabilityDetail{State: CapabilityStateUnavailable, Reason: "knowledge service is not configured"}
	}
	if h.Settings == nil {
		return CapabilityDetail{State: CapabilityStateConfigurationRequired, Reason: "embedding settings are not configured"}
	}
	all := h.Settings.GetAll()
	provider := strings.TrimSpace(all[settings.KeyEmbeddingModelProvider])
	model := strings.TrimSpace(all[settings.KeyEmbeddingModel])
	if provider == "" || model == "" || strings.TrimSpace(all[providerAPIKey(provider)]) == "" {
		return CapabilityDetail{
			State:  CapabilityStateConfigurationRequired,
			Reason: "choose an embedding provider, model, and API key",
		}
	}
	return CapabilityDetail{State: CapabilityStateAvailable}
}

func providerAPIKey(provider string) string {
	switch provider {
	case "openai":
		return settings.KeyOpenAIAPIKey
	case "google", "google_genai":
		return settings.KeyGoogleAPIKey
	case "anthropic":
		return settings.KeyAnthropicAPIKey
	case "openrouter":
		return settings.KeyOpenRouterAPIKey
	default:
		return ""
	}
}

// CLIStatus handles GET /api/v1/status — returns workboard phase counts.
func (h *Handlers) CLIStatus(ctx context.Context, in *CLIStatusInput) (*CLIStatusOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
		return nil, err
	}
	result, err := svc.GovernanceStatus(ctx, governanceops.GovernanceStatusInput{
		WorkspaceID: strings.TrimSpace(in.WorkspaceID),
	})
	if err != nil {
		return nil, mapGovernanceError("governance-status", err)
	}
	out := &CLIStatusOutput{}
	out.Body = result
	return out, nil
}

// CLIStats handles GET /api/v1/stats — governance-value stats projected from
// existing gate runs and feedback events.
func (h *Handlers) CLIStats(ctx context.Context, in *CLIStatsInput) (*CLIStatsOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
		return nil, err
	}
	result, err := svc.Stats(ctx, governanceops.StatsInput{
		WorkspaceID: strings.TrimSpace(in.WorkspaceID),
		Days:        in.Days,
	})
	if err != nil {
		return nil, mapGovernanceError("stats", err)
	}
	out := &CLIStatsOutput{}
	out.Body = result
	return out, nil
}

// CLIResolveWorkRef handles POST /api/v1/work-items/resolve — resolves a work
// reference (CR ID, CR key, issue URL, or bare tracker key) to a canonical work item.
func (h *Handlers) CLIResolveWorkRef(ctx context.Context, in *CLIResolveWorkRefInput) (*CLIResolveWorkRefOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
		return nil, err
	}
	result, err := svc.ResolveWorkRef(ctx, in.Body)
	if err != nil {
		return nil, mapGovernanceError("resolve-work-ref", err)
	}
	out := &CLIResolveWorkRefOutput{}
	out.Body = result
	return out, nil
}

// CLIContextPack handles GET /api/v1/work-items/{id}/context-pack — assembles
// the context pack for a change request.
func (h *Handlers) CLIContextPack(ctx context.Context, in *CLIContextPackInput) (*CLIContextPackOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
		return nil, err
	}
	result, err := svc.ContextPack(ctx, governanceops.ContextPackInput{
		Kind: "change_request",
		ID:   in.ID,
	})
	if err != nil {
		return nil, mapGovernanceError("context-pack", err)
	}
	out := &CLIContextPackOutput{}
	out.Body = result
	return out, nil
}

// CLIAudit handles GET /api/v1/audit/{ref} — assembles the chronological
// governance audit trail for a work reference. Unresolvable ref → 404.
func (h *Handlers) CLIAudit(ctx context.Context, in *CLIAuditInput) (*CLIAuditOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
		return nil, err
	}
	result, err := svc.AuditTrail(ctx, governanceops.ResolveWorkRefInput{Ref: in.Ref}, in.Verify)
	if err != nil {
		return nil, mapGovernanceError("audit", err)
	}
	out := &CLIAuditOutput{}
	out.Body = result
	return out, nil
}

// CLIReportFeedback handles POST /api/v1/work-items/{id}/feedback — records a
// coding-agent feedback event. Agent-supplied source provenance is stripped.
func (h *Handlers) CLIReportFeedback(ctx context.Context, in *CLIFeedbackInput) (*CLIFeedbackOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
		return nil, err
	}
	// Path param is authoritative; override any body change_request_id.
	in.Body.ChangeRequestID = in.ID
	result, err := svc.ReportFeedback(ctx, in.Body)
	if err != nil {
		return nil, mapGovernanceError("report-feedback", err)
	}
	out := &CLIFeedbackOutput{}
	out.Body = result
	return out, nil
}

// CLIPublishArtifact handles POST /api/v1/artifacts/publish — publishes an
// artifact version. Returns 409 Conflict when base_version is stale.
func (h *Handlers) CLIPublishArtifact(ctx context.Context, in *CLIPublishArtifactInput) (*CLIPublishArtifactOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
		return nil, err
	}
	if err := applyCLIWorkspace(ctx, &in.Body.WorkspaceID); err != nil {
		return nil, err
	}
	for _, document := range in.Body.Documents {
		if unsafeCLIArtifactDocumentPath(document.Path) {
			return nil, huma.Error422UnprocessableEntity("unsafe document path", nil)
		}
	}
	result, err := svc.PublishArtifact(ctx, in.Body)
	if err != nil {
		return nil, mapGovernanceError("publish-artifact", err)
	}
	out := &CLIPublishArtifactOutput{}
	out.Body = result
	return out, nil
}

func unsafeCLIArtifactDocumentPath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "/") || strings.ContainsRune(value, 0) || strings.ContainsRune(value, '\\') {
		return true
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

// CLIRunReadiness handles POST /api/v1/work-items/{id}/readiness — runs the
// readiness gates for an artifact via the agents service. Returns 503 when the
// agents service is not configured.
func (h *Handlers) CLIRunReadiness(ctx context.Context, in *CLIWorkItemIDInput) (*CLIReadinessOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
		return nil, err
	}
	result, err := svc.RunReadiness(ctx, in.ID)
	if err != nil {
		return nil, mapGovernanceError("run-readiness", err)
	}
	out := &CLIReadinessOutput{}
	out.Body = result
	return out, nil
}

// CLIRunLLMGates handles POST /api/v1/work-items/{id}/llm-gates — triggers
// quality gates for a change request. Returns 503 when agents service is absent.
func (h *Handlers) CLIRunLLMGates(ctx context.Context, in *CLIWorkItemIDInput) (*CLIRawOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
		return nil, err
	}
	result, err := svc.RunLLMGates(ctx, in.ID)
	if err != nil {
		return nil, mapGovernanceError("run-llm-gates", err)
	}
	out := &CLIRawOutput{}
	out.Body = result
	return out, nil
}

// CLITriggerDeliveryReview handles POST /api/v1/work-items/{id}/delivery-review
// — triggers the delivery review for a change request. Returns 503 without agents.
func (h *Handlers) CLITriggerDeliveryReview(ctx context.Context, in *CLIWorkItemIDInput) (*CLIRawOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
		return nil, err
	}
	result, err := svc.ReviewDelivery(ctx, in.ID)
	if err != nil {
		return nil, mapGovernanceError("trigger-delivery-review", err)
	}
	out := &CLIRawOutput{}
	out.Body = result
	return out, nil
}

// CLIDeliveryDecision handles POST /api/v1/work-items/{id}/delivery-decision
// — records a human delivery approve/reject decision as a delivery_review gate run.
func (h *Handlers) CLIDeliveryDecision(ctx context.Context, in *CLIDeliveryDecisionInput) (*CLIDeliveryDecisionOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
		return nil, err
	}
	result, err := svc.DecideDelivery(ctx, governanceops.DeliveryDecisionInput{
		ChangeRequestID:           in.ID,
		Decision:                  in.Body.Decision,
		Actor:                     in.Body.Actor,
		Note:                      in.Body.Note,
		ReviewedGateRunID:         in.Body.ReviewedGateRunID,
		CompletionFeedbackEventID: in.Body.CompletionFeedbackEventID,
	})
	if err != nil {
		return nil, mapGovernanceError("delivery-decision", err)
	}
	if result.Executor == workboard.GateRunExecutorHuman &&
		result.Verdict == string(workboard.NextActionStatePass) &&
		h.Integrations != nil {
		// Terminal tracker transitions follow human acceptance, never a
		// platform evidence verdict. The integration method is best-effort and
		// deliberately cannot make the durable decision write fail.
		h.Integrations.AutoTransitionIssueOnDeliveryPass(ctx, in.ID)
	}
	out := &CLIDeliveryDecisionOutput{}
	out.Body = result
	return out, nil
}

// CLICreateQuickWorkItem handles POST /api/v1/work-items — creates a quick-route
// change request from issue content via the agents service.
// CLIWorkCreate handles POST /api/v1/work-items/create — the full-route
// sibling of quick creation: a feature-backed work item bound to the feature's
// approved canonical spec (per spec §6).
func (h *Handlers) CLIWorkCreate(ctx context.Context, in *CLIWorkCreateInput) (*CLIWorkCreateOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
		return nil, err
	}
	if err := applyCLIWorkspace(ctx, &in.Body.WorkspaceID); err != nil {
		return nil, err
	}
	result, err := svc.CreateWorkItem(ctx, in.Body)
	if err != nil {
		// Per the approved work-create-cli spec: a feature without a canonical
		// artifact (and other semantically-invalid-but-well-formed inputs) is
		// 422, not the service-wide 400 validation mapping.
		if errors.Is(err, governanceops.ErrValidation) {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		return nil, mapGovernanceError("work-create", err)
	}
	out := &CLIWorkCreateOutput{}
	out.Body = result
	return out, nil
}

func (h *Handlers) CLICreateQuickWorkItem(ctx context.Context, in *CLICreateQuickWorkItemInput) (*CLIRawOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
		return nil, err
	}
	if err := applyCLIWorkspace(ctx, &in.Body.WorkspaceID); err != nil {
		return nil, err
	}
	result, err := svc.CreateQuickWorkItem(ctx, in.Body)
	if err != nil {
		return nil, mapGovernanceError("create-quick-work-item", err)
	}
	out := &CLIRawOutput{}
	out.Body = result
	return out, nil
}

// CLIArchiveWorkItem handles POST /api/v1/work-items/{id}/archive — resolves a
// work ref, archives the backing ChangeRequest, and returns the canonical work
// item identity so CLI flows can archive by CR key, URL, or tracker key.
func (h *Handlers) CLIArchiveWorkItem(ctx context.Context, in *CLIArchiveWorkItemInput) (*CLIArchiveWorkItemOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
		return nil, err
	}
	writer, ok := svc.WorkBoard.(interface {
		UpdateChangeRequest(context.Context, workboard.ChangeRequest) (*workboard.ChangeRequest, error)
	})
	if !ok {
		return nil, huma.Error503ServiceUnavailable("workboard archive operation not configured")
	}
	work, err := svc.ResolveWorkRef(ctx, governanceops.ResolveWorkRefInput{Ref: in.ID})
	if err != nil {
		return nil, mapGovernanceError("archive-work-item", err)
	}
	actor := strings.TrimSpace(in.Body.Actor)
	if actor == "" {
		actor = "specgate-cli"
	}
	_, err = writer.UpdateChangeRequest(ctx, workboard.ChangeRequest{
		ID:            work.ChangeRequestID,
		Archived:      true,
		ArchiveReason: strings.TrimSpace(in.Body.Reason),
		ArchivedBy:    actor,
	})
	if err != nil {
		return nil, mapWorkBoardError("archive-work-item", err)
	}
	out := &CLIArchiveWorkItemOutput{}
	out.Body = work
	return out, nil
}

// CLIListSkills handles GET /api/v1/skills — lists skills with optional name filter.
func (h *Handlers) CLIListSkills(ctx context.Context, in *CLIListSkillsInput) (*CLIListSkillsOutput, error) {
	svc, err := h.requireSkills()
	if err != nil {
		return nil, err
	}
	ctx = skills.WithWorkspace(ctx, in.WorkspaceID)
	list, err := svc.List(ctx)
	if err != nil {
		return nil, mapSkillError("list skills", err)
	}
	out := &CLIListSkillsOutput{}
	if in.Name != "" {
		needle := strings.ToLower(in.Name)
		filtered := make([]skills.Skill, 0)
		for _, s := range list {
			if strings.HasPrefix(strings.ToLower(s.Name), needle) {
				filtered = append(filtered, s)
			}
		}
		out.Body.Items = skillDTOs(filtered)
	} else {
		out.Body.Items = skillDTOs(list)
	}
	return out, nil
}

// CLIGetSkill handles GET /api/v1/skills/{id} — returns a single skill by ID.
func (h *Handlers) CLIGetSkill(ctx context.Context, in *CLIGetSkillInput) (*CLIGetSkillOutput, error) {
	svc, err := h.requireSkills()
	if err != nil {
		return nil, err
	}
	ctx = skills.WithWorkspace(ctx, in.WorkspaceID)
	s, err := svc.Get(ctx, in.ID)
	if err != nil {
		return nil, mapSkillError("get skill", err)
	}
	out := &CLIGetSkillOutput{}
	out.Body = skillDTO(*s)
	return out, nil
}

// CLIListArtifacts handles GET /api/v1/artifacts — lists artifacts with optional filters.
func (h *Handlers) CLIListArtifacts(ctx context.Context, in *CLIListArtifactsInput) (*CLIListArtifactsOutput, error) {
	result, err := h.ListArtifacts(ctx, &ListArtifactsInput{
		WorkspaceID:   workspace.ID(ctx),
		FeatureID:     in.FeatureID,
		Status:        in.Status,
		ExcludeStatus: in.ExcludeStatus,
		Limit:         in.Limit,
		Offset:        in.Offset,
	})
	if err != nil {
		return nil, err
	}
	out := &CLIListArtifactsOutput{}
	out.Body.Items = result.Body.Items
	out.Body.Total = result.Body.Total
	return out, nil
}

// CLIGetArtifact handles GET /api/v1/artifacts/{id} — returns a single artifact.
func (h *Handlers) CLIGetArtifact(ctx context.Context, in *CLIGetArtifactInput) (*CLIGetArtifactOutput, error) {
	result, err := h.GetArtifact(ctx, &GetArtifactInput{ID: in.ID, WorkspaceID: workspace.ID(ctx)})
	if err != nil {
		return nil, err
	}
	out := &CLIGetArtifactOutput{}
	out.Body = result.Body
	return out, nil
}

// CLIListArtifactFiles handles GET /api/v1/artifacts/{id}/files — lists artifact file metadata.
func (h *Handlers) CLIListArtifactFiles(ctx context.Context, in *CLIListArtifactFilesInput) (*CLIListArtifactFilesOutput, error) {
	result, err := h.ListArtifactFiles(ctx, &ListArtifactFilesInput{ID: in.ID, WorkspaceID: workspace.ID(ctx)})
	if err != nil {
		return nil, err
	}
	out := &CLIListArtifactFilesOutput{}
	out.Body.Items = result.Body.Items
	return out, nil
}

// CLIGetArtifactFile handles GET /api/v1/artifacts/{id}/files/_?path=... .
func (h *Handlers) CLIGetArtifactFile(ctx context.Context, in *GetArtifactFileInput) (*GetArtifactFileOutput, error) {
	return h.GetArtifactFile(ctx, in)
}

// CLIGatesStatus handles GET /api/v1/work-items/{id}/gates — returns the
// persisted gate state for a change request without re-running gates.
func (h *Handlers) CLIGatesStatus(ctx context.Context, in *CLIGatesStatusInput) (*CLIGatesStatusOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
		return nil, err
	}
	ws, err := svc.WorkStatus(ctx, governanceops.ResolveWorkRefInput{Ref: in.ID})
	if err != nil {
		return nil, mapGovernanceError("gates-status", err)
	}
	out := &CLIGatesStatusOutput{}
	out.Body.ChangeRequestID = ws.ChangeRequestID
	out.Body.Gates = ws.Gates
	return out, nil
}

// CLIGateHistory handles GET /api/v1/work-items/{id}/gate-history — returns the
// history of gate runs for a change request.
func (h *Handlers) CLIGateHistory(ctx context.Context, in *CLIGateHistoryInput) (*CLIGateHistoryOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
		return nil, err
	}
	result, err := svc.GateHistory(ctx, governanceops.GateHistoryInput{
		ChangeRequestID: in.ID,
		Gate:            in.Gate,
		Limit:           in.Limit,
	})
	if err != nil {
		return nil, mapGovernanceError("gate-history", err)
	}
	out := &CLIGateHistoryOutput{}
	out.Body = result
	return out, nil
}

// CLIDeliveryStatus handles GET /api/v1/work-items/{id}/delivery-status — returns
// the authoritative delivery review verdict for a change request.
func (h *Handlers) CLIDeliveryStatus(ctx context.Context, in *CLIDeliveryStatusInput) (*CLIDeliveryStatusOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
		return nil, err
	}
	result, err := svc.DeliveryStatus(ctx, governanceops.DeliveryStatusInput{
		ChangeRequestID: in.ID,
		Detail:          in.Detail,
	})
	if err != nil {
		return nil, mapGovernanceError("delivery-status", err)
	}
	out := &CLIDeliveryStatusOutput{}
	out.Body = result
	return out, nil
}
