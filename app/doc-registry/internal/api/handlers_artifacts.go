package api

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"

	"github.com/specgate/doc-registry/internal/artifact"
)

func (h *Handlers) ListArtifacts(ctx context.Context, in *ListArtifactsInput) (*ListArtifactsOutput, error) {
	if h.Artifacts == nil {
		return nil, notImplemented("list_artifacts")
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = artifact.WithWorkspace(ctx, workspaceID)
	filter := artifact.ListFilter{
		WorkspaceID:   workspaceID,
		FeatureID:     in.FeatureID,
		Service:       in.Service,
		Status:        artifact.Status(in.Status),
		ExcludeStatus: artifact.Status(in.ExcludeStatus),
		Limit:         in.Limit,
		Offset:        in.Offset,
	}
	items, err := h.Artifacts.List(ctx, filter)
	if err != nil {
		return nil, huma.Error500InternalServerError("list artifacts", err)
	}
	total, err := h.Artifacts.Count(ctx, filter)
	if err != nil {
		return nil, huma.Error500InternalServerError("count artifacts", err)
	}
	featureNames := h.resolveFeatureNamesBulk(ctx)
	out := &ListArtifactsOutput{}
	out.Body.Items = make([]ArtifactDTO, 0, len(items))
	for i := range items {
		dto := artifactDTO(&items[i])
		dto.FeatureName = featureNames[items[i].FeatureID]
		out.Body.Items = append(out.Body.Items, dto)
	}
	out.Body.Total = int(total)
	return out, nil
}

func (h *Handlers) GetArtifact(ctx context.Context, in *GetArtifactInput) (*GetArtifactOutput, error) {
	if h.Artifacts == nil {
		return nil, notImplemented("get_artifact")
	}
	ctx, workspaceID, err := artifactWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	a, err := getArtifactForWorkspace(ctx, h.Artifacts, workspaceID, in.ID)
	if err != nil {
		return nil, mapArtifactError("get artifact", err)
	}
	dto := artifactDTO(a)
	dto.FeatureName = h.resolveFeatureName(ctx, a.FeatureID)
	return &GetArtifactOutput{Body: dto}, nil
}

func artifactWorkspaceContext(ctx context.Context, workspaceID string) (context.Context, string, error) {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return nil, "", err
	}
	return artifact.WithWorkspace(ctx, workspaceID), workspaceID, nil
}

func getArtifactForWorkspace(ctx context.Context, svc artifact.Service, workspaceID, id string) (*artifact.Artifact, error) {
	type workspaceGetter interface {
		GetInWorkspace(context.Context, string, string) (*artifact.Artifact, error)
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, huma.Error400BadRequest("workspace_id is required")
	}
	if scoped, ok := svc.(workspaceGetter); ok {
		return scoped.GetInWorkspace(ctx, workspaceID, id)
	}
	return nil, huma.Error501NotImplemented("workspace-scoped artifact lookup unavailable")
}

// resolveFeatureName returns the display name for a single feature UUID.
// Returns empty string when WorkBoard is unavailable or the feature is not found.
func (h *Handlers) resolveFeatureName(ctx context.Context, featureID string) string {
	if h.WorkBoard == nil || featureID == "" {
		return ""
	}
	feat, err := h.WorkBoard.GetFeature(ctx, featureID)
	if err != nil || feat == nil {
		return ""
	}
	return feat.Name
}

// resolveFeatureNamesBulk fetches all features and returns a featureID→name map.
// Best-effort: returns an empty map when WorkBoard is unavailable.
func (h *Handlers) resolveFeatureNamesBulk(ctx context.Context) map[string]string {
	if h.WorkBoard == nil {
		return map[string]string{}
	}
	features, err := h.WorkBoard.ListFeatures(ctx)
	if err != nil {
		return map[string]string{}
	}
	m := make(map[string]string, len(features))
	for _, f := range features {
		m[f.ID] = f.Name
	}
	return m
}

func (h *Handlers) ListArtifactFiles(ctx context.Context, in *ListArtifactFilesInput) (*ListArtifactFilesOutput, error) {
	if h.Artifacts == nil {
		return nil, notImplemented("list_artifact_files")
	}
	ctx, workspaceID, err := artifactWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	a, err := getArtifactForWorkspace(ctx, h.Artifacts, workspaceID, in.ID)
	if err != nil {
		return nil, mapArtifactError("list artifact files", err)
	}
	out := &ListArtifactFilesOutput{}
	out.Body.Items = make([]ArtifactFileDTO, 0, len(a.Files))
	for _, f := range a.Files {
		if in.Role != "" && string(f.Role) != in.Role {
			continue
		}
		out.Body.Items = append(out.Body.Items, ArtifactFileDTO{
			Path:          f.Path,
			Role:          string(f.Role),
			SizeBytes:     f.SizeBytes,
			ContentSHA256: f.ContentSHA256,
		})
	}
	sort.Slice(out.Body.Items, func(i, j int) bool {
		return out.Body.Items[i].Path < out.Body.Items[j].Path
	})
	return out, nil
}

func (h *Handlers) RefreshArtifactReadinessRuns(ctx context.Context, in *RefreshArtifactReadinessRunsInput) (*ListArtifactReadinessRunsOutput, error) {
	if h.Artifacts == nil {
		return nil, notImplemented("refresh_artifact_readiness_runs")
	}
	ctx, workspaceID, err := artifactWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if _, err := getArtifactForWorkspace(ctx, h.Artifacts, workspaceID, in.ID); err != nil {
		return nil, mapArtifactError("refresh artifact readiness runs", err)
	}
	evals := make([]artifact.ReadinessEvaluation, 0)
	if in.Body != nil {
		for _, row := range in.Body.Evaluations {
			evals = append(evals, artifact.ReadinessEvaluation{
				Gate:             row.Gate,
				State:            artifact.ReadinessState(row.State),
				Hint:             row.Hint,
				Confidence:       row.Confidence,
				JudgeModel:       row.JudgeModel,
				EvalSuiteVersion: row.EvalSuiteVersion,
				Evidence:         row.Evidence,
			})
		}
	}
	items, err := h.Artifacts.RefreshReadinessRuns(ctx, in.ID, evals)
	if err != nil {
		return nil, mapArtifactError("refresh artifact readiness runs", err)
	}
	out := &ListArtifactReadinessRunsOutput{}
	out.Body.Items = make([]ArtifactReadinessRunDTO, 0, len(items))
	for _, item := range items {
		out.Body.Items = append(out.Body.Items, artifactReadinessRunDTO(item))
	}
	return out, nil
}

func (h *Handlers) ListArtifactReadinessRuns(ctx context.Context, in *ListArtifactReadinessRunsInput) (*ListArtifactReadinessRunsOutput, error) {
	if h.Artifacts == nil {
		return nil, notImplemented("list_artifact_readiness_runs")
	}
	ctx, workspaceID, err := artifactWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if _, err := getArtifactForWorkspace(ctx, h.Artifacts, workspaceID, in.ID); err != nil {
		return nil, mapArtifactError("list artifact readiness runs", err)
	}
	items, err := h.Artifacts.ListReadinessRuns(ctx, in.ID, in.Limit)
	if err != nil {
		return nil, mapArtifactError("list artifact readiness runs", err)
	}
	out := &ListArtifactReadinessRunsOutput{}
	out.Body.Items = make([]ArtifactReadinessRunDTO, 0, len(items))
	for _, item := range items {
		out.Body.Items = append(out.Body.Items, artifactReadinessRunDTO(item))
	}
	return out, nil
}

func artifactReadinessRunDTO(in artifact.ReadinessRun) ArtifactReadinessRunDTO {
	return ArtifactReadinessRunDTO{
		ID:           in.ID,
		ArtifactID:   in.ArtifactID,
		Gate:         in.Gate,
		State:        string(in.State),
		Hint:         in.Hint,
		Executor:     in.Executor,
		EvidenceJSON: in.EvidenceJSON,
		CreatedAt:    in.CreatedAt,
	}
}

func (h *Handlers) UpdateStatus(ctx context.Context, in *UpdateStatusInput) (*UpdateStatusOutput, error) {
	if h.Artifacts == nil {
		return nil, notImplemented("update_status")
	}
	ctx, workspaceID, err := artifactWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if _, err := getArtifactForWorkspace(ctx, h.Artifacts, workspaceID, in.ID); err != nil {
		return nil, mapArtifactError("update status", err)
	}
	a, err := h.Artifacts.UpdateStatus(ctx, in.ID, artifact.StatusUpdate{
		Status:       artifact.Status(in.Body.Status),
		Actor:        in.Body.ApprovedBy,
		ReviewRating: in.Body.ReviewRating,
		Note:         in.Body.Note,
		ActorKind:    in.Body.ActorKind,
	})
	if err != nil {
		return nil, mapArtifactError("update status", err)
	}
	dto := artifactDTO(a)
	dto.FeatureName = h.resolveFeatureName(ctx, a.FeatureID)
	return &UpdateStatusOutput{Body: dto}, nil
}

func (h *Handlers) GetArtifactFile(ctx context.Context, in *GetArtifactFileInput) (*GetArtifactFileOutput, error) {
	if h.Artifacts == nil {
		return nil, notImplemented("get_artifact_file")
	}
	ctx, workspaceID, err := artifactWorkspaceContext(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if _, err := getArtifactForWorkspace(ctx, h.Artifacts, workspaceID, in.ID); err != nil {
		return nil, mapArtifactError("get artifact file", err)
	}
	docPath := strings.TrimSpace(in.Path)
	if docPath == "" {
		return nil, huma.Error422UnprocessableEntity("document path is required", nil)
	}
	body, contentErr := h.Artifacts.FileContent(ctx, in.ID, docPath)
	if contentErr != nil {
		return nil, mapArtifactError("get artifact file", contentErr)
	}
	if !utf8.Valid(body) {
		return nil, huma.Error500InternalServerError("artifact file is not valid UTF-8")
	}
	out := &GetArtifactFileOutput{}
	out.Body.SizeBytes = int64(len(body))
	out.Body.Content = string(body)
	return out, nil
}

func (h *Handlers) CheckConflicts(ctx context.Context, in *CheckConflictsInput) (*CheckConflictsOutput, error) {
	if h.Artifacts == nil {
		return nil, notImplemented("check_conflicts")
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	scoped, ok := h.Artifacts.(interface {
		CheckConflictsInWorkspace(context.Context, string, []string) (*artifact.ConflictReport, error)
	})
	if !ok {
		return nil, huma.Error501NotImplemented("workspace-scoped conflict lookup unavailable")
	}
	report, err := scoped.CheckConflictsInWorkspace(ctx, workspaceID, in.Services)
	if err != nil {
		return nil, huma.Error500InternalServerError("check conflicts", err)
	}
	out := &CheckConflictsOutput{}
	out.Body.ConflictState = report.State
	for _, c := range report.Conflicts {
		out.Body.Conflicts = append(out.Body.Conflicts, ConflictDTO{
			ConflictID: c.ID,
			Type:       c.Type,
			FeatureA: ConflictFeatureRefDTO{
				FeatureID: in.CandidateFeatureID,
				Version:   in.CandidateVersion,
				Status:    candidateStatus(in.CandidateStatus),
			},
			FeatureB: ConflictFeatureRefDTO{
				FeatureID: c.Existing.FeatureID,
				Version:   c.Existing.Version,
				Status:    string(c.Existing.Status),
			},
			OverlappingServices: c.OverlappingServices,
			ResolutionOptions:   c.ResolutionOptions,
		})
	}
	return out, nil
}

// candidateStatus returns the provided status or "draft" if empty.
func candidateStatus(s string) string {
	if s == "" {
		return "draft"
	}
	return s
}

func (h *Handlers) ListEvents(ctx context.Context, in *ListEventsInput) (*ListEventsOutput, error) {
	if h.Artifacts == nil {
		return nil, notImplemented("list_events")
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if in.ArtifactID != "" {
		if _, err := getArtifactForWorkspace(ctx, h.Artifacts, workspaceID, in.ArtifactID); err != nil {
			return nil, mapArtifactError("list events", err)
		}
	}
	events, err := h.Artifacts.ListEvents(ctx, artifact.EventFilter{
		WorkspaceID: workspaceID,
		EventType:   in.EventType,
		ArtifactID:  in.ArtifactID,
		After:       in.After,
		Limit:       in.Limit,
	})
	if err != nil {
		return nil, huma.Error500InternalServerError("list events", err)
	}
	out := &ListEventsOutput{}
	for _, ev := range events {
		payload := map[string]any{}
		if ev.Payload != "" {
			if err := json.Unmarshal([]byte(ev.Payload), &payload); err != nil {
				return nil, huma.Error500InternalServerError("parse event payload", err)
			}
		}
		out.Body.Items = append(out.Body.Items, EventDTO{
			ID:         ev.ID,
			ArtifactID: ev.ArtifactID,
			EventType:  ev.EventType,
			Payload:    payload,
			CreatedAt:  ev.CreatedAt,
		})
	}
	return out, nil
}

func artifactDTO(a *artifact.Artifact) ArtifactDTO {
	dto := ArtifactDTO{
		ID:                   a.ID,
		WorkspaceID:          a.WorkspaceID,
		FeatureID:            a.FeatureID,
		Version:              a.Version,
		Status:               string(a.Status),
		RequestType:          string(a.RequestType),
		ImpactLevel:          string(a.ImpactLevel),
		ArtifactPhase:        string(a.ArtifactPhase),
		ArtifactCompleteness: string(a.ArtifactCompleteness),
		ConfidenceScore:      a.ConfidenceScore,
		AmbiguityScore:       a.AmbiguityScore,
		GovernanceVersion:    a.GovernanceVersion,
		PolicyVersion:        a.PolicyVersion,
		PolicyDigest:         a.PolicyDigest,
		PolicySnapshot:       a.PolicySnapshotJSON,
		ExpectedGates:        expectedGatesFromSnapshot(a.PolicySnapshotJSON),
		SourceKind:           a.SourceKind,
		SourceID:             a.SourceID,
		SourceRevision:       a.SourceRevision,
		SnapshotDigest:       a.SnapshotDigest,
		CreatedBy:            a.CreatedBy,
		ApprovedBy:           a.ApprovedBy,
		ApprovedAt:           a.ApprovedAt,
		CreatedAt:            a.CreatedAt,
		UpdatedAt:            a.UpdatedAt,
		ImpactedServices:     make([]ServiceDTO, 0, len(a.Services)),
	}
	for _, svc := range a.Services {
		dto.ImpactedServices = append(dto.ImpactedServices, ServiceDTO{
			Name: svc.Name,
			Kind: svc.Kind,
		})
	}
	return dto
}

// expectedGatesFromSnapshot parses the enabled-gate set from a persisted
// policy snapshot (a marshaled governanceprofile.ResolvedProfile). It
// returns nil for an empty or unparseable snapshot so artifacts without a
// snapshot omit the field rather than surfacing an empty list.
func expectedGatesFromSnapshot(snapshotJSON string) []string {
	snapshotJSON = strings.TrimSpace(snapshotJSON)
	if snapshotJSON == "" {
		return nil
	}
	var snap struct {
		EnabledGates []string `json:"enabled_gates"`
	}
	if err := json.Unmarshal([]byte(snapshotJSON), &snap); err != nil {
		return nil
	}
	if len(snap.EnabledGates) == 0 {
		return nil
	}
	return snap.EnabledGates
}
