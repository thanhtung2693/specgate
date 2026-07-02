package api

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/specgate/doc-registry/internal/buildinfo"
	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/skills"
	"github.com/specgate/doc-registry/internal/workboard"
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
		{sentinel: governanceops.ErrVersionConflict, fn: huma.Error409Conflict},
		{sentinel: governanceops.ErrUnavailable, fn: huma.Error503ServiceUnavailable},
		{sentinel: governanceops.ErrProviderRequired, fn: huma.Error400BadRequest},
	})
}

// CLIMeta handles GET /api/v1/meta — returns build information and server capabilities.
func (h *Handlers) CLIMeta(_ context.Context, _ *struct{}) (*CLIMetaOutput, error) {
	out := &CLIMetaOutput{}
	out.Body.APIVersion = "specgate.api/v1"
	out.Body.Version = buildinfo.Version
	out.Body.Commit = buildinfo.Commit
	out.Body.Date = buildinfo.Date
	out.Body.RecommendedCLIVersion = buildinfo.Version
	// Advertise agents capability based on whether an agents runner is wired up.
	out.Body.Capabilities = map[string]bool{
		"agents": h.Governance != nil && h.Governance.AgentsRunner != nil,
	}
	return out, nil
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
// existing gate runs and feedback events (per spec §6.8).
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
// the context pack for a change request, optionally filtered to a lane.
func (h *Handlers) CLIContextPack(ctx context.Context, in *CLIContextPackInput) (*CLIContextPackOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
		return nil, err
	}
	result, err := svc.ContextPack(ctx, governanceops.ContextPackInput{
		Kind: "change_request",
		ID:   in.ID,
		Lane: in.Lane,
	})
	if err != nil {
		return nil, mapGovernanceError("context-pack", err)
	}
	out := &CLIContextPackOutput{}
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
	result, err := svc.PublishArtifact(ctx, in.Body)
	if err != nil {
		return nil, mapGovernanceError("publish-artifact", err)
	}
	out := &CLIPublishArtifactOutput{}
	out.Body = result
	return out, nil
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

// CLIRunLLMGates handles POST /api/v1/work-items/{id}/llm-gates — triggers LLM
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

// CLICreateQuickWorkItem handles POST /api/v1/work-items — creates a quick-route
// change request from issue content via the agents service.
func (h *Handlers) CLICreateQuickWorkItem(ctx context.Context, in *CLICreateQuickWorkItemInput) (*CLIRawOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
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
	list, err := svc.List(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("list skills", err)
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
	s, err := svc.Get(ctx, in.ID)
	if err != nil {
		if skills.IsNotFound(err) {
			return nil, skillNotFoundError()
		}
		return nil, huma.Error500InternalServerError("get skill", err)
	}
	out := &CLIGetSkillOutput{}
	out.Body = skillDTO(*s)
	return out, nil
}

// CLIListArtifacts handles GET /api/v1/artifacts — lists artifacts with optional filters.
func (h *Handlers) CLIListArtifacts(ctx context.Context, in *CLIListArtifactsInput) (*CLIListArtifactsOutput, error) {
	result, err := h.ListArtifacts(ctx, &ListArtifactsInput{
		FeatureID: in.FeatureID,
		Status:    in.Status,
		Limit:     in.Limit,
		Offset:    in.Offset,
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
	result, err := h.GetArtifact(ctx, &GetArtifactInput{ID: in.ID})
	if err != nil {
		return nil, err
	}
	out := &CLIGetArtifactOutput{}
	out.Body = result.Body
	return out, nil
}

// CLIListArtifactFiles handles GET /api/v1/artifacts/{id}/files — lists artifact file metadata.
func (h *Handlers) CLIListArtifactFiles(ctx context.Context, in *CLIListArtifactFilesInput) (*CLIListArtifactFilesOutput, error) {
	result, err := h.ListArtifactFiles(ctx, &ListArtifactFilesInput{ID: in.ID})
	if err != nil {
		return nil, err
	}
	out := &CLIListArtifactFilesOutput{}
	out.Body.Items = result.Body.Items
	return out, nil
}

// CLIGetArtifactFile handles GET /api/v1/artifacts/{id}/files/{key} — returns file
// content for an artifact. The {key} param is accepted for compatibility; the ?path=
// query param takes precedence and is preferred for slash-containing document paths.
func (h *Handlers) CLIGetArtifactFile(ctx context.Context, in *GetArtifactFileInput) (*GetArtifactFileOutput, error) {
	return h.GetArtifactFile(ctx, in)
}

// CLIDraftProposal handles POST /api/v1/artifacts/{id}/proposals — opens a
// draft-only artifact-edit proposal for a coding agent.
func (h *Handlers) CLIDraftProposal(ctx context.Context, in *CLIDraftProposalInput) (*CLIDraftProposalOutput, error) {
	svc, err := h.requireGovernance()
	if err != nil {
		return nil, err
	}
	in.Body.ArtifactID = in.ID
	result, err := svc.DraftArtifactUpdate(ctx, in.Body)
	if err != nil {
		return nil, mapGovernanceError("draft-proposal", err)
	}
	out := &CLIDraftProposalOutput{}
	out.Body = result
	return out, nil
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
// the latest delivery review verdict for a change request.
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
