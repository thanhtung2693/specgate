package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/artifactedit"
	"github.com/specgate/doc-registry/internal/integrations"
)

type artifactEditDiff struct {
	Summary     string
	Files       []ArtifactEditDiffFileDTO
	UnifiedDiff string
}

// revisionDiffPayload is the JSON shape persisted in a revision's diff_json
// column so the rendered diff survives a restart without recomputing against a
// base artifact that may have moved on.
type revisionDiffPayload struct {
	Summary     string                    `json:"summary"`
	Files       []ArtifactEditDiffFileDTO `json:"files,omitempty"`
	UnifiedDiff string                    `json:"unified_diff,omitempty"`
}

func (h *Handlers) CreateArtifactEditSession(
	ctx context.Context,
	in *CreateArtifactEditSessionInput,
) (*CreateArtifactEditSessionOutput, error) {
	if h.ArtifactEdit == nil || h.Artifacts == nil {
		return nil, notImplemented("create_artifact_edit_session")
	}
	baseID := strings.TrimSpace(in.Body.BaseArtifactID)
	if baseID == "" {
		baseID = strings.TrimSpace(in.Body.ArtifactID)
	}
	if baseID == "" {
		return nil, huma.Error400BadRequest("base artifact id is required")
	}
	base, err := h.Artifacts.Get(ctx, baseID)
	if err != nil {
		return nil, mapArtifactError("get base artifact", err)
	}
	now := time.Now().UTC()
	baseFiles := map[string]string{}
	for _, f := range base.Files {
		content, err := h.Artifacts.FileContent(ctx, base.ID, f.Path)
		if err != nil {
			continue
		}
		baseFiles[f.Path] = string(content)
	}
	// Resolved working overrides seed the session already-resolved (one atomic
	// request, no create-then-write window). Only keys present in the base
	// artifact are honored — a stray key cannot invent a file.
	workingFiles := map[string]string{}
	for _, wf := range in.Body.WorkingFiles {
		key := strings.TrimSpace(wf.Key)
		if _, ok := baseFiles[key]; !ok {
			return nil, huma.Error400BadRequest("working file key not in base artifact: " + key)
		}
		workingFiles[key] = wf.Content
	}
	session := artifactedit.Session{
		ID:             "aes_" + uuid.NewString(),
		BaseArtifactID: base.ID,
		BaseVersion:    base.Version,
		BaseRevisionID: strings.TrimSpace(in.Body.BaseRevisionID),
		State:          "active",
		RequestedBy:    strings.TrimSpace(in.Body.RequestedBy),
		SourceKind:     strings.TrimSpace(in.Body.SourceKind),
		SourceID:       strings.TrimSpace(in.Body.SourceID),
		CompareToken:   uuid.NewString(),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := h.ArtifactEdit.CreateSession(ctx, session, baseFiles, workingFiles); err != nil {
		return nil, mapArtifactEditError("create artifact edit session", err)
	}
	return &CreateArtifactEditSessionOutput{Body: artifactEditSessionDTO(session)}, nil
}

func (h *Handlers) GetArtifactEditSession(
	ctx context.Context,
	in *GetArtifactEditSessionInput,
) (*GetArtifactEditSessionOutput, error) {
	if h.ArtifactEdit == nil {
		return nil, notImplemented("get_artifact_edit_session")
	}
	state, err := h.ArtifactEdit.LoadSession(ctx, in.ID)
	if err != nil {
		return nil, mapArtifactEditError("get artifact edit session", err)
	}
	return &GetArtifactEditSessionOutput{Body: artifactEditSessionDTO(state.Session)}, nil
}

func (h *Handlers) DeleteArtifactEditSession(
	ctx context.Context,
	in *DeleteArtifactEditSessionInput,
) (*DeleteArtifactEditSessionOutput, error) {
	if h.ArtifactEdit == nil {
		return nil, notImplemented("delete_artifact_edit_session")
	}
	out := &DeleteArtifactEditSessionOutput{}
	out.Body.OK = true
	state, err := h.ArtifactEdit.LoadSession(ctx, in.ID)
	if err != nil {
		if errors.Is(err, artifactedit.ErrNotFound) {
			return out, nil
		}
		return nil, mapArtifactEditError("delete artifact edit session", err)
	}
	if err := h.ArtifactEdit.SetSessionMeta(ctx, in.ID, artifactedit.SessionMeta{
		State:           "discarded",
		SavedRevisionID: state.Session.SavedRevisionID,
		LastDiffSummary: state.Session.LastDiffSummary,
		UpdatedAt:       time.Now().UTC(),
	}); err != nil {
		return nil, mapArtifactEditError("delete artifact edit session", err)
	}
	h.reconcileProposalVerdict(ctx, state.Session, integrations.FeedbackStatusIgnored,
		"artifact-update proposal rejected")
	return out, nil
}

func (h *Handlers) ListArtifactEditSessionFiles(
	ctx context.Context,
	in *ListArtifactEditSessionFilesInput,
) (*ListArtifactEditSessionFilesOutput, error) {
	if h.ArtifactEdit == nil {
		return nil, notImplemented("list_artifact_edit_session_files")
	}
	state, err := h.ArtifactEdit.LoadSession(ctx, in.ID)
	if err != nil {
		return nil, mapArtifactEditError("list artifact edit session files", err)
	}
	keys := sortedKeys(state.Working)
	out := &ListArtifactEditSessionFilesOutput{}
	out.Body.Items = make([]ArtifactEditSessionFileDTO, 0, len(keys))
	for _, key := range keys {
		out.Body.Items = append(out.Body.Items, fileDTO(state.Base, state.Working, key))
	}
	return out, nil
}

func (h *Handlers) GetArtifactEditSessionFile(
	ctx context.Context,
	in *GetArtifactEditSessionFileInput,
) (*GetArtifactEditSessionFileOutput, error) {
	if h.ArtifactEdit == nil {
		return nil, notImplemented("get_artifact_edit_session_file")
	}
	state, err := h.ArtifactEdit.LoadSession(ctx, in.ID)
	if err != nil {
		return nil, mapArtifactEditError("get artifact edit session file", err)
	}
	key := strings.TrimSpace(in.Key)
	if _, ok := state.Working[key]; !ok {
		return nil, huma.Error404NotFound("artifact edit session file not found")
	}
	return &GetArtifactEditSessionFileOutput{Body: fileDTO(state.Base, state.Working, key)}, nil
}

func (h *Handlers) PatchArtifactEditSession(
	ctx context.Context,
	in *PatchArtifactEditSessionInput,
) (*PatchArtifactEditSessionOutput, error) {
	if h.ArtifactEdit == nil {
		return nil, notImplemented("patch_artifact_edit_session")
	}
	hunkID := strings.TrimSpace(in.Body.HunkID)
	hunkState := strings.TrimSpace(in.Body.HunkState)
	key := strings.TrimSpace(in.Body.FileKey)
	if key == "" {
		key = strings.TrimSpace(in.Body.Key)
	}
	state, err := h.ArtifactEdit.LoadSession(ctx, in.ID)
	if err != nil {
		return nil, mapArtifactEditError("patch artifact edit session", err)
	}
	now := time.Now().UTC()

	if hunkID != "" || hunkState != "" {
		if hunkID == "" || hunkState == "" {
			return nil, huma.Error400BadRequest("hunk_id and hunk_state are required together")
		}
		if hunkState != "pending" && hunkState != "applied" && hunkState != "rejected" {
			return nil, huma.Error400BadRequest("invalid hunk_state")
		}
		fileKey, ok := hunkFileKey(state, hunkID)
		if !ok {
			return nil, huma.Error404NotFound("hunk not found")
		}
		decision := artifactedit.HunkDecision{
			ID:        "aehd_" + uuid.NewString(),
			SessionID: in.ID,
			HunkID:    hunkID,
			FileKey:   fileKey,
			State:     hunkState,
			Actor:     strings.TrimSpace(in.Body.DecidedBy),
			DecidedAt: now,
		}
		if err := h.ArtifactEdit.AppendHunkDecision(ctx, decision); err != nil {
			return nil, mapArtifactEditError("record hunk decision", err)
		}
		return &PatchArtifactEditSessionOutput{Body: fileDTO(state.Base, state.Working, fileKey)}, nil
	}

	if key == "" {
		return nil, huma.Error400BadRequest("file key is required")
	}
	if _, ok := state.Working[key]; !ok {
		return nil, huma.Error404NotFound("artifact edit session file not found")
	}
	updated := patchContent(state.Working[key], in.Body.Patch, in.Body.Operations, in.Body.Instruction)
	if err := h.ArtifactEdit.UpdateWorkingFile(ctx, in.ID, key, updated, now); err != nil {
		return nil, mapArtifactEditError("patch artifact edit session", err)
	}
	state.Working[key] = updated
	if err := h.touchSessionDiff(ctx, in.ID, state, now); err != nil {
		return nil, err
	}
	return &PatchArtifactEditSessionOutput{Body: fileDTO(state.Base, state.Working, key)}, nil
}

func (h *Handlers) ReplaceArtifactEditSessionFile(
	ctx context.Context,
	in *ReplaceArtifactEditSessionFileInput,
) (*ReplaceArtifactEditSessionFileOutput, error) {
	if h.ArtifactEdit == nil {
		return nil, notImplemented("replace_artifact_edit_session_file")
	}
	key := strings.TrimSpace(in.Key)
	state, err := h.ArtifactEdit.LoadSession(ctx, in.ID)
	if err != nil {
		return nil, mapArtifactEditError("replace artifact edit session file", err)
	}
	if _, ok := state.Working[key]; !ok {
		return nil, huma.Error404NotFound("artifact edit session file not found")
	}
	now := time.Now().UTC()
	if err := h.ArtifactEdit.UpdateWorkingFile(ctx, in.ID, key, in.Body.Content, now); err != nil {
		return nil, mapArtifactEditError("replace artifact edit session file", err)
	}
	state.Working[key] = in.Body.Content
	if err := h.touchSessionDiff(ctx, in.ID, state, now); err != nil {
		return nil, err
	}
	return &ReplaceArtifactEditSessionFileOutput{Body: fileDTO(state.Base, state.Working, key)}, nil
}

func (h *Handlers) GetArtifactEditSessionDiff(
	ctx context.Context,
	in *GetArtifactEditSessionDiffInput,
) (*GetArtifactEditSessionDiffOutput, error) {
	if h.ArtifactEdit == nil {
		return nil, notImplemented("get_artifact_edit_session_diff")
	}
	state, err := h.ArtifactEdit.LoadSession(ctx, in.ID)
	if err != nil {
		return nil, mapArtifactEditError("get artifact edit session diff", err)
	}
	diff := buildArtifactEditDiff(state)
	out := &GetArtifactEditSessionDiffOutput{}
	out.Body.Summary = diff.Summary
	out.Body.Files = diff.Files
	out.Body.UnifiedDiff = diff.UnifiedDiff
	return out, nil
}

func (h *Handlers) SaveArtifactEditSession(
	ctx context.Context,
	in *SaveArtifactEditSessionInput,
) (*SaveArtifactEditSessionOutput, error) {
	if h.ArtifactEdit == nil || h.Artifacts == nil {
		return nil, notImplemented("save_artifact_edit_session")
	}
	state, err := h.ArtifactEdit.LoadSession(ctx, in.ID)
	if err != nil {
		return nil, mapArtifactEditError("save artifact edit session", err)
	}
	now := time.Now().UTC()
	// If the client provided a compare_token, verify it matches the session's
	// stored token. A mismatch means the client's view is stale.
	if t := strings.TrimSpace(in.Body.CompareToken); t != "" && t != state.Session.CompareToken {
		if metaErr := h.ArtifactEdit.SetSessionMeta(ctx, in.ID, artifactedit.SessionMeta{
			State:           "stale_base",
			SavedRevisionID: state.Session.SavedRevisionID,
			LastDiffSummary: state.Session.LastDiffSummary,
			UpdatedAt:       now,
		}); metaErr != nil {
			return nil, mapArtifactEditError("save artifact edit session", metaErr)
		}
		return &SaveArtifactEditSessionOutput{Body: ArtifactSavedRevisionDTO{
			BaseArtifactID: state.Session.BaseArtifactID,
			State:          "stale_base",
			SessionID:      state.Session.ID,
			CreatedAt:      now,
		}}, nil
	}
	base, err := h.Artifacts.Get(ctx, state.Session.BaseArtifactID)
	if err != nil || base.Version != state.Session.BaseVersion {
		if metaErr := h.ArtifactEdit.SetSessionMeta(ctx, in.ID, artifactedit.SessionMeta{
			State:           "stale_base",
			SavedRevisionID: state.Session.SavedRevisionID,
			LastDiffSummary: state.Session.LastDiffSummary,
			UpdatedAt:       now,
		}); metaErr != nil {
			return nil, mapArtifactEditError("save artifact edit session", metaErr)
		}
		return &SaveArtifactEditSessionOutput{Body: ArtifactSavedRevisionDTO{
			BaseArtifactID: state.Session.BaseArtifactID,
			State:          "stale_base",
			SessionID:      state.Session.ID,
			CreatedAt:      now,
		}}, nil
	}
	// Honor per-hunk decisions: a rejected hunk reverts to base, so its content
	// is excluded from the saved revision.
	resolved := &artifactedit.SessionState{
		Session:   state.Session,
		Base:      state.Base,
		Working:   effectiveWorking(state),
		Decisions: state.Decisions,
	}
	diff := buildArtifactEditDiff(resolved)
	diffJSON, err := json.Marshal(revisionDiffPayload{
		Summary:     diff.Summary,
		Files:       hunkMetadataOnly(diff.Files),
		UnifiedDiff: diff.UnifiedDiff,
	})
	if err != nil {
		return nil, mapArtifactEditError("save artifact edit session", err)
	}
	// Lineage: chain to the previous saved revision from this session if any,
	// otherwise to the draft revision the session was opened against. The root
	// is the artifact the edit chain descends from.
	parentRevisionID := strings.TrimSpace(state.Session.SavedRevisionID)
	if parentRevisionID == "" {
		parentRevisionID = strings.TrimSpace(state.Session.BaseRevisionID)
	}
	revisionID := "aer_" + uuid.NewString()
	revision := artifactedit.Revision{
		RevisionID:            revisionID,
		BaseArtifactID:        state.Session.BaseArtifactID,
		State:                 "saved",
		SessionID:             state.Session.ID,
		Summary:               strings.TrimSpace(in.Body.Summary),
		DiffJSON:              string(diffJSON),
		ParentRevisionID:      parentRevisionID,
		LineageRootArtifactID: state.Session.BaseArtifactID,
		CreatedAt:             now,
	}
	// Materialize: when this is a reconciliation proposal or a coding-agent spec
	// update (per spec §1), create a new draft artifact from the effective working
	// files so the approved changes enter the normal review flow and become canonical.
	if (state.Session.SourceKind == "feedback_event" || state.Session.SourceKind == "coding_agent_update") && h.Artifacts != nil {
		if matID := h.materializeRevision(ctx, state, revisionID); matID != "" {
			revision.ArtifactID = matID
		}
	}
	if err := h.ArtifactEdit.CreateRevision(ctx, revision); err != nil {
		return nil, mapArtifactEditError("save artifact edit session", err)
	}
	if err := h.ArtifactEdit.SetSessionMeta(ctx, in.ID, artifactedit.SessionMeta{
		State:           "saved",
		SavedRevisionID: revisionID,
		LastDiffSummary: diff.Summary,
		UpdatedAt:       now,
	}); err != nil {
		return nil, mapArtifactEditError("save artifact edit session", err)
	}
	h.reconcileProposalVerdict(ctx, state.Session, integrations.FeedbackStatusProcessed,
		"artifact-update proposal approved (revision "+revisionID+")")
	dto := artifactSavedRevisionDTO(revision)
	dto.MaterializedArtifactID = revision.ArtifactID
	return &SaveArtifactEditSessionOutput{Body: dto}, nil
}

func (h *Handlers) GetArtifactSavedRevision(
	ctx context.Context,
	in *GetArtifactSavedRevisionInput,
) (*GetArtifactSavedRevisionOutput, error) {
	if h.ArtifactEdit == nil {
		return nil, notImplemented("get_artifact_saved_revision")
	}
	rev, err := h.ArtifactEdit.GetRevision(ctx, in.RevisionID)
	if err != nil {
		return nil, mapArtifactEditError("get artifact saved revision", err)
	}
	return &GetArtifactSavedRevisionOutput{Body: artifactSavedRevisionDTO(*rev)}, nil
}

func (h *Handlers) GetArtifactSavedRevisionDiff(
	ctx context.Context,
	in *GetArtifactSavedRevisionDiffInput,
) (*GetArtifactSavedRevisionDiffOutput, error) {
	if h.ArtifactEdit == nil {
		return nil, notImplemented("get_artifact_saved_revision_diff")
	}
	rev, err := h.ArtifactEdit.GetRevision(ctx, in.RevisionID)
	if err != nil {
		return nil, mapArtifactEditError("get artifact saved revision diff", err)
	}
	var payload revisionDiffPayload
	if strings.TrimSpace(rev.DiffJSON) != "" {
		_ = json.Unmarshal([]byte(rev.DiffJSON), &payload)
	}
	out := &GetArtifactSavedRevisionDiffOutput{}
	out.Body.Summary = payload.Summary
	out.Body.Files = payload.Files
	out.Body.UnifiedDiff = payload.UnifiedDiff
	return out, nil
}

func (h *Handlers) ListArtifactRevisions(
	ctx context.Context,
	in *ListArtifactRevisionsInput,
) (*ListArtifactRevisionsOutput, error) {
	if h.ArtifactEdit == nil {
		return nil, notImplemented("list_artifact_revisions")
	}
	rows, err := h.ArtifactEdit.ListRevisions(ctx, in.ID)
	if err != nil {
		return nil, mapArtifactEditError("list artifact revisions", err)
	}
	out := &ListArtifactRevisionsOutput{}
	for _, rev := range rows {
		out.Body.Items = append(out.Body.Items, artifactSavedRevisionDTO(rev))
	}
	return out, nil
}

// touchSessionDiff recomputes the diff summary after a working-file change and
// persists it (the working content was already written by the caller).
func (h *Handlers) touchSessionDiff(
	ctx context.Context,
	sessionID string,
	state *artifactedit.SessionState,
	now time.Time,
) error {
	diff := buildArtifactEditDiff(state)
	if err := h.ArtifactEdit.SetSessionMeta(ctx, sessionID, artifactedit.SessionMeta{
		State:           state.Session.State,
		SavedRevisionID: state.Session.SavedRevisionID,
		LastDiffSummary: diff.Summary,
		UpdatedAt:       now,
	}); err != nil {
		return mapArtifactEditError("update artifact edit session", err)
	}
	return nil
}

// reconcileProposalVerdict closes the reconciliation loop: when the resolved
// session is a feedback-sourced proposal, the human verdict is recorded back
// onto the originating feedback signal (approve → processed, reject → ignored).
// On approve, if the signal was a delivery.pr_merged event and its linked Feature
// is still planned, the Feature advances to active — "only then does state move"
// per the moat contract. All side-effects are best-effort (non-fatal).
func (h *Handlers) reconcileProposalVerdict(ctx context.Context, session artifactedit.Session, status, reason string) {
	if h.Integrations == nil || session.SourceKind != "feedback_event" || session.SourceID == "" {
		return
	}
	event, err := h.Integrations.ReconcileFeedbackEvent(ctx, session.SourceID, status, reason)
	if err != nil || event == nil {
		return
	}
	// On approve: if the feedback signal was a merged delivery, advance the linked
	// Feature from planned → active so product state reflects the shipped work.
	if status == integrations.FeedbackStatusProcessed &&
		event.EventType == integrations.FeedbackEventPRMerged &&
		event.FeatureID != "" &&
		h.WorkBoard != nil {
		feat, err := h.WorkBoard.GetFeature(ctx, event.FeatureID)
		if err == nil && feat != nil && feat.Status == "planned" {
			feat.Status = "active"
			_, _ = h.WorkBoard.UpdateFeature(ctx, *feat)
		}
	}
}

// materializeRevision creates a new draft artifact from the session's effective
// working files so an approved reconciliation proposal can enter the normal
// review flow. Best-effort: failures are logged and an empty string is returned
// so the revision save is never blocked (per spec §1).
func (h *Handlers) materializeRevision(ctx context.Context, state *artifactedit.SessionState, revisionID string) string {
	base, err := h.Artifacts.Get(ctx, state.Session.BaseArtifactID)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Str("base_artifact_id", state.Session.BaseArtifactID).
			Msg("materializeRevision: failed to read base artifact")
		return ""
	}
	version, err := h.Artifacts.NextVersion(ctx, base.FeatureID)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Str("feature_id", base.FeatureID).
			Msg("materializeRevision: failed to get next version")
		return ""
	}
	docs := make([]artifact.DocumentInput, 0, len(state.Working))
	for _, f := range base.Files {
		content, ok := state.Working[f.Path]
		if !ok {
			continue
		}
		docs = append(docs, artifact.DocumentInput{
			Path:    f.Path,
			Role:    string(f.Role),
			Content: []byte(content),
		})
	}
	services := make([]artifact.ServiceRef, 0, len(base.Services))
	for _, s := range base.Services {
		services = append(services, artifact.ServiceRef{Name: s.Name, Kind: s.Kind})
	}
	newArt, err := h.Artifacts.Publish(ctx, artifact.PublishInput{
		FeatureID:            base.FeatureID,
		Version:              version,
		Status:               artifact.StatusDraft,
		RequestType:          base.RequestType,
		ImpactLevel:          base.ImpactLevel,
		ImpactedServices:     services,
		Documents:            docs,
		SourceKind:           "reconciliation",
		SourceID:             revisionID,
		ArtifactCompleteness: artifact.ArtifactCompletenessFull,
	})
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Str("revision_id", revisionID).
			Msg("materializeRevision: failed to publish draft artifact")
		return ""
	}
	return newArt.ID
}

func artifactEditSessionDTO(s artifactedit.Session) ArtifactEditSessionDTO {
	return ArtifactEditSessionDTO{
		ID:              s.ID,
		BaseArtifactID:  s.BaseArtifactID,
		BaseVersion:     s.BaseVersion,
		BaseRevisionID:  s.BaseRevisionID,
		State:           s.State,
		SavedRevisionID: s.SavedRevisionID,
		LastDiffSummary: s.LastDiffSummary,
		SourceKind:      s.SourceKind,
		SourceID:        s.SourceID,
		CompareToken:    s.CompareToken,
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
	}
}

// ListArtifactEditProposals returns the review queue: artifact-update proposals
// (edit sessions tagged with an origin) still awaiting a human verdict. Approve
// is an ordinary session save (draft revision); reject is a discard.
func (h *Handlers) ListArtifactEditProposals(
	ctx context.Context,
	_ *ListArtifactEditProposalsInput,
) (*ListArtifactEditProposalsOutput, error) {
	if h.ArtifactEdit == nil {
		return nil, notImplemented("list_artifact_edit_proposals")
	}
	rows, err := h.ArtifactEdit.ListProposals(ctx)
	if err != nil {
		return nil, mapArtifactEditError("list artifact edit proposals", err)
	}
	out := &ListArtifactEditProposalsOutput{}
	out.Body.Items = make([]ArtifactEditSessionDTO, 0, len(rows))
	for _, s := range rows {
		out.Body.Items = append(out.Body.Items, artifactEditSessionDTO(s))
	}
	return out, nil
}

func artifactSavedRevisionDTO(r artifactedit.Revision) ArtifactSavedRevisionDTO {
	return ArtifactSavedRevisionDTO{
		RevisionID:            r.RevisionID,
		BaseArtifactID:        r.BaseArtifactID,
		ArtifactID:            r.ArtifactID,
		State:                 r.State,
		SessionID:             r.SessionID,
		ParentRevisionID:      r.ParentRevisionID,
		LineageRootArtifactID: r.LineageRootArtifactID,
		CreatedAt:             r.CreatedAt,
	}
}

func mapArtifactEditError(op string, err error) error {
	if errors.Is(err, artifactedit.ErrNotFound) {
		return huma.Error404NotFound("artifact edit session not found")
	}
	return huma.Error500InternalServerError(op, err)
}

func fileDTO(base, working map[string]string, key string) ArtifactEditSessionFileDTO {
	return ArtifactEditSessionFileDTO{
		Key:      key,
		Content:  working[key],
		Modified: working[key] != base[key],
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func patchContent(current, patch string, operations []map[string]any, instruction string) string {
	if strings.TrimSpace(patch) != "" {
		return patch
	}
	for _, op := range operations {
		if value, ok := op["value"]; ok {
			return fmt.Sprint(value)
		}
	}
	if strings.TrimSpace(instruction) == "" {
		return current
	}
	if strings.TrimSpace(current) == "" {
		return instruction
	}
	return strings.TrimRight(current, "\n") + "\n\n" + instruction + "\n"
}

func buildArtifactEditDiff(state *artifactedit.SessionState) artifactEditDiff {
	keys := sortedKeys(state.Working)
	diff := artifactEditDiff{}
	var b strings.Builder
	for _, key := range keys {
		base := state.Base[key]
		working := state.Working[key]
		if base == working {
			continue
		}
		hunkID := stableHunkID(state.Session.ID, key, base, working)
		diff.Files = append(diff.Files, ArtifactEditDiffFileDTO{
			Key:      key,
			Status:   "modified",
			Modified: true,
			Hunks: []ArtifactEditDiffHunkDTO{{
				ID:          hunkID,
				State:       hunkStateForID(state.Decisions, hunkID),
				FileKey:     key,
				BaseText:    base,
				WorkingText: working,
			}},
		})
		b.WriteString("--- ")
		b.WriteString(key)
		b.WriteString("\n+++ ")
		b.WriteString(key)
		b.WriteString("\n@@\n")
		writeUnifiedLines(&b, "-", base)
		writeUnifiedLines(&b, "+", working)
	}
	if len(diff.Files) == 0 {
		diff.Summary = "No changes"
		return diff
	}
	diff.Summary = fmt.Sprintf("%d file(s) changed", len(diff.Files))
	diff.UnifiedDiff = b.String()
	return diff
}

// hunkMetadataOnly drops each hunk's base/working text. The per-hunk content is
// only needed for live review of an active session; a saved revision keeps the
// file-level unified_diff for history, so persisting the text would bloat
// diff_json without a consumer.
func hunkMetadataOnly(files []ArtifactEditDiffFileDTO) []ArtifactEditDiffFileDTO {
	if len(files) == 0 {
		return files
	}
	out := make([]ArtifactEditDiffFileDTO, len(files))
	for i, file := range files {
		hunks := make([]ArtifactEditDiffHunkDTO, len(file.Hunks))
		for j, hunk := range file.Hunks {
			hunks[j] = ArtifactEditDiffHunkDTO{ID: hunk.ID, State: hunk.State}
		}
		file.Hunks = hunks
		out[i] = file
	}
	return out
}

// effectiveWorking returns the working set with rejected hunks reverted to base,
// so a saved revision reflects only applied/pending changes.
func effectiveWorking(state *artifactedit.SessionState) map[string]string {
	out := make(map[string]string, len(state.Working))
	for key, working := range state.Working {
		base := state.Base[key]
		if base != working {
			hunkID := stableHunkID(state.Session.ID, key, base, working)
			if d, ok := state.Decisions[hunkID]; ok && d.State == "rejected" {
				out[key] = base
				continue
			}
		}
		out[key] = working
	}
	return out
}

func stableHunkID(sessionID, key, base, working string) string {
	sum := sha256.Sum256([]byte(sessionID + "\n" + key + "\n" + base + "\n" + working))
	return "hunk_" + hex.EncodeToString(sum[:8])
}

func hunkStateForID(decisions map[string]artifactedit.HunkDecision, hunkID string) string {
	d, ok := decisions[hunkID]
	if !ok {
		return "pending"
	}
	switch d.State {
	case "applied", "rejected", "pending":
		return d.State
	default:
		return "pending"
	}
}

// hunkFileKey returns the file a hunk id belongs to in the current diff, or
// false if the id is not part of the working set (e.g. the file was edited
// further and the id no longer matches).
func hunkFileKey(state *artifactedit.SessionState, hunkID string) (string, bool) {
	diff := buildArtifactEditDiff(state)
	for _, file := range diff.Files {
		for _, hunk := range file.Hunks {
			if hunk.ID == hunkID {
				return file.Key, true
			}
		}
	}
	return "", false
}

func writeUnifiedLines(b *strings.Builder, prefix, text string) {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for _, line := range lines {
		b.WriteString(prefix)
		b.WriteString(line)
		b.WriteString("\n")
	}
}
