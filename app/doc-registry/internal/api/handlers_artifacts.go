package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"

	"github.com/specgate/doc-registry/internal/artifact"
)

const maxInlineArtifactFileContentBytes int64 = 1 << 20

// fixedKeyToRole maps a fixed file key to an artifact.Role (fixed-key publish shape).
var fixedKeyToRole = artifact.FixedKeyToRole

// fixedKeyToPath maps a fixed file key to its canonical document path (fixed-key publish shape).
var fixedKeyToPath = artifact.FixedKeyToPath

func (h *Handlers) PublishArtifact(ctx context.Context, in *PublishArtifactInput) (*PublishArtifactOutput, error) {
	if h.Artifacts == nil {
		return nil, notImplemented("publish_artifact")
	}

	var docs []artifact.DocumentInput

	if len(in.Body.Documents) > 0 {
		// New open-document API takes precedence.
		for _, d := range in.Body.Documents {
			p := strings.TrimSpace(d.Path)
			if p == "" {
				return nil, huma.Error422UnprocessableEntity("document path is required", nil)
			}
			// Reject unsafe paths early so the client gets a 422, not a 500
			// (the service enforces the same rule as defense in depth).
			if strings.HasPrefix(p, "/") || strings.Contains(p, "..") || strings.ContainsRune(p, '\\') {
				return nil, huma.Error422UnprocessableEntity("unsafe document path", nil)
			}
			di := artifact.DocumentInput{
				Path: p,
				Role: d.Role,
			}
			if d.Content != "" {
				decoded, err := base64.StdEncoding.DecodeString(d.Content)
				if err != nil {
					return nil, huma.Error400BadRequest("invalid base64 document content", err)
				}
				di.Content = decoded
			} else if d.Ref != "" {
				if h.GovernanceFiles == nil {
					return nil, huma.Error503ServiceUnavailable("governance files store unavailable")
				}
				f, err := h.GovernanceFiles.Get(ctx, d.Ref)
				if err != nil {
					return nil, huma.Error400BadRequest("invalid document ref", err)
				}
				di.ResolvedS3Path = f.ObjectKey
				di.ResolvedSizeBytes = f.SizeBytes
			}
			docs = append(docs, di)
		}
	} else {
		// Fixed-key files/file_refs shape (used by the agents quick-lane publish
		// and the MCP artifact_create tool); each key maps to {path, role}.
		seenKeys := map[string]bool{}
		for key, val := range in.Body.Files {
			decoded, err := base64.StdEncoding.DecodeString(val)
			if err != nil {
				return nil, huma.Error400BadRequest("invalid base64 file content", err)
			}
			seenKeys[key] = true
			docs = append(docs, artifact.DocumentInput{
				Path:    fixedKeyToPath(key),
				Role:    string(fixedKeyToRole(key)),
				Content: decoded,
			})
		}
		for key, fileID := range in.Body.FileRefs {
			if seenKeys[key] {
				return nil, huma.Error400BadRequest("artifact file key provided in both files and file_refs")
			}
			if h.GovernanceFiles == nil {
				return nil, huma.Error503ServiceUnavailable("governance files store unavailable")
			}
			f, err := h.GovernanceFiles.Get(ctx, fileID)
			if err != nil {
				return nil, huma.Error400BadRequest("invalid artifact file reference", err)
			}
			docs = append(docs, artifact.DocumentInput{
				Path:              fixedKeyToPath(key),
				Role:              string(fixedKeyToRole(key)),
				ResolvedS3Path:    f.ObjectKey,
				ResolvedSizeBytes: f.SizeBytes,
			})
		}
	}

	refs := make([]artifact.ServiceRef, 0, len(in.Body.ImpactedServices)+len(in.Body.ImpactedApps))
	for _, svc := range in.Body.ImpactedServices {
		refs = append(refs, artifact.ServiceRef{Name: svc, Kind: "service"})
	}
	for _, app := range in.Body.ImpactedApps {
		refs = append(refs, artifact.ServiceRef{Name: app, Kind: "app"})
	}
	a, err := h.Artifacts.Publish(ctx, artifact.PublishInput{
		FeatureID:            in.Body.FeatureID,
		Version:              in.Body.Version,
		BaseVersion:          in.Body.BaseVersion,
		Status:               artifact.Status(in.Body.Status),
		RequestType:          artifact.RequestType(in.Body.RequestType),
		ImpactLevel:          artifact.ImpactLevel(in.Body.ImpactLevel),
		ArtifactPhase:        artifact.ArtifactPhase(in.Body.ArtifactPhase),
		ArtifactCompleteness: artifact.ArtifactCompleteness(in.Body.ArtifactCompleteness),
		ConfidenceScore:      in.Body.ConfidenceScore,
		AmbiguityScore:       in.Body.AmbiguityScore,
		GovernanceVersion:    in.Body.GovernanceVersion,
		CreatedBy:            "governance-ops",
		ImpactedServices:     refs,
		Documents:            docs,
		SourceKind:           in.Body.SourceKind,
		SourceID:             in.Body.SourceID,
		SourceRevision:       in.Body.SourceRevision,
		Authority:            in.Body.Authority,
		GatesProfile:         in.Body.GatesProfile,
	})
	if err != nil {
		return nil, mapArtifactError("publish artifact", err)
	}
	dto := artifactDTO(a)
	dto.FeatureName = h.resolveFeatureName(ctx, a.FeatureID)
	return &PublishArtifactOutput{Body: dto}, nil
}

func (h *Handlers) ListArtifacts(ctx context.Context, in *ListArtifactsInput) (*ListArtifactsOutput, error) {
	if h.Artifacts == nil {
		return nil, notImplemented("list_artifacts")
	}
	filter := artifact.ListFilter{
		FeatureID: in.FeatureID,
		Service:   in.Service,
		Status:    artifact.Status(in.Status),
		Limit:     in.Limit,
		Offset:    in.Offset,
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
	a, err := h.Artifacts.Get(ctx, in.ID)
	if err != nil {
		return nil, mapArtifactError("get artifact", err)
	}
	dto := artifactDTO(a)
	dto.FeatureName = h.resolveFeatureName(ctx, a.FeatureID)
	return &GetArtifactOutput{Body: dto}, nil
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
	a, err := h.Artifacts.Get(ctx, in.ID)
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
			Path:      f.Path,
			Role:      string(f.Role),
			SizeBytes: f.SizeBytes,
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

func (h *Handlers) DeleteArtifact(ctx context.Context, in *DeleteArtifactInput) (*DeleteArtifactOutput, error) {
	if h.Artifacts == nil {
		return nil, notImplemented("delete_artifact")
	}
	if err := h.Artifacts.Delete(ctx, in.ID); err != nil {
		return nil, mapArtifactError("delete artifact", err)
	}
	out := &DeleteArtifactOutput{}
	out.Body.OK = true
	return out, nil
}

func artifactReadinessRunDTO(in artifact.ReadinessRun) ArtifactReadinessRunDTO {
	return ArtifactReadinessRunDTO{
		ID:           in.ID,
		ArtifactID:   in.ArtifactID,
		Gate:         in.Gate,
		State:        string(in.State),
		Hint:         in.Hint,
		EvidenceJSON: in.EvidenceJSON,
		CreatedAt:    in.CreatedAt,
	}
}

func (h *Handlers) UpdateStatus(ctx context.Context, in *UpdateStatusInput) (*UpdateStatusOutput, error) {
	if h.Artifacts == nil {
		return nil, notImplemented("update_status")
	}
	a, err := h.Artifacts.UpdateStatus(ctx, in.ID, artifact.StatusUpdate{
		Status:       artifact.Status(in.Body.Status),
		Actor:        in.Body.ApprovedBy,
		ReviewRating: in.Body.ReviewRating,
		Note:         in.Body.Note,
		Manifest:     in.Body.Manifest,
		ActorKind:    in.Body.ActorKind,
	})
	if err != nil {
		return nil, mapArtifactError("update status", err)
	}
	dto := artifactDTO(a)
	dto.FeatureName = h.resolveFeatureName(ctx, a.FeatureID)
	return &UpdateStatusOutput{Body: dto}, nil
}

// resolveFilePath returns the document path for a file-get request.
// Priority: ?path query param → fixed {key} mapped through FixedKeyToPath.
func resolveFilePath(key, queryPath string) (string, error) {
	if strings.TrimSpace(queryPath) != "" {
		return strings.TrimSpace(queryPath), nil
	}
	if strings.TrimSpace(key) == "" {
		return "", huma.Error422UnprocessableEntity("path or key is required", nil)
	}
	return fixedKeyToPath(key), nil
}

func (h *Handlers) GetArtifactFile(ctx context.Context, in *GetArtifactFileInput) (*GetArtifactFileOutput, error) {
	if h.Artifacts == nil {
		return nil, notImplemented("get_artifact_file")
	}
	docPath, err := resolveFilePath(in.Key, in.Path)
	if err != nil {
		return nil, err
	}
	f, fileErr := h.Artifacts.SignedFileURL(ctx, in.ID, docPath)
	if errors.Is(fileErr, artifact.ErrNotFound) && strings.TrimSpace(in.Path) == "" {
		// Fixed-key lookup returned not-found. Fall back to a basename match
		// against the artifact's role-tagged files so path-preserving artifacts
		// (e.g. docs/prd.md) are still found when the UI requests key "prd".
		if art, getErr := h.Artifacts.Get(ctx, in.ID); getErr == nil {
			wantBase := baseName(docPath)
			for _, af := range art.Files {
				if baseName(af.Path) == wantBase {
					docPath = af.Path
					f, fileErr = h.Artifacts.SignedFileURL(ctx, in.ID, af.Path)
					break
				}
			}
		}
	}
	if fileErr != nil {
		return nil, mapArtifactError("get artifact file", fileErr)
	}
	out := &GetArtifactFileOutput{}
	out.Body.SignedURL = f.URL
	out.Body.ExpiresAt = f.ExpiresAt
	out.Body.SizeBytes = f.SizeBytes
	if f.SizeBytes <= maxInlineArtifactFileContentBytes {
		body, contentErr := h.Artifacts.FileContent(ctx, in.ID, docPath)
		if contentErr != nil || !utf8.Valid(body) {
			return out, nil
		}
		out.Body.Content = string(body)
	}
	return out, nil
}

// baseName returns the last path segment (filename) of a slash-delimited path.
func baseName(path string) string {
	path = strings.TrimRight(path, "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// ServeArtifactFileContent streams an artifact file's content through the API
// (server-side S3 read) so the browser never needs a presigned URL. The JSON
// GetArtifactFile already inlines content for files <= 1MiB; this is the fallback
// the UI uses for larger files. Artifact files are immutable per version, so the
// response is cacheable with an ETag (id + docPath).
// The document path is resolved from the ?path= query param first, then the
// {key} chi wildcard mapped through FixedKeyToPath.
func (h *Handlers) ServeArtifactFileContent(w http.ResponseWriter, r *http.Request) {
	if h.Artifacts == nil {
		http.NotFound(w, r)
		return
	}
	id := chi.URLParam(r, "id")
	// Prefer ?path= query param to allow slash-containing document paths.
	docPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if docPath == "" {
		key := chi.URLParam(r, "key")
		docPath = fixedKeyToPath(strings.TrimSpace(key))
	}
	etag := `"` + id + "/" + docPath + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body, err := h.Artifacts.FileContent(r.Context(), id, docPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ct := "text/markdown; charset=utf-8"
	if strings.HasSuffix(docPath, ".json") {
		ct = "application/json; charset=utf-8"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	_, _ = w.Write(body)
}

func (h *Handlers) CheckConflicts(ctx context.Context, in *CheckConflictsInput) (*CheckConflictsOutput, error) {
	if h.Artifacts == nil {
		return nil, notImplemented("check_conflicts")
	}
	report, err := h.Artifacts.CheckConflicts(ctx, in.Services)
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
	events, err := h.Artifacts.ListEvents(ctx, artifact.EventFilter{
		EventType:  in.EventType,
		ArtifactID: in.ArtifactID,
		After:      in.After,
		Limit:      in.Limit,
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
		GatesProfile:         a.GatesProfile,
		GatesProfileVersion:  a.GatesProfileVersion,
		GatesProfileDigest:   a.GatesProfileDigest,
		GatesProfileSnapshot: a.GatesProfileSnapshotJSON,
		ExpectedGates:        expectedGatesFromSnapshot(a.GatesProfileSnapshotJSON),
		SourceKind:           a.SourceKind,
		SourceID:             a.SourceID,
		SourceRevision:       a.SourceRevision,
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
// gates-profile snapshot (a marshaled governanceprofile.ResolvedProfile). It
// returns nil for an empty or unparseable snapshot so older artifacts (no
// snapshot) omit the field rather than surfacing an empty list.
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
