package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/policy"
)

// listGateTasksInput has the query param for artifact_id.
type listGateTasksInput struct {
	ArtifactID string `query:"artifact_id" doc:"Filter gate tasks by artifact ID"`
}

// gateTaskBody is the response shape for a single gate task.
type gateTaskBody struct {
	TaskID         string `json:"task_id"`
	GateKey        string `json:"gate_key"`
	GateVersion    string `json:"gate_version"`
	GateDigest     string `json:"gate_digest"`
	ArtifactID     string `json:"artifact_id"`
	ArtifactDigest string `json:"artifact_digest"`
	ProfileDigest  string `json:"profile_digest"`
	Executor       string `json:"executor"`
	SkillContent   string `json:"skill_content,omitempty"`
	ExpiresAt      string `json:"expires_at"`
}

// listGateTasksOutput wraps a slice of tasks.
type listGateTasksOutput struct {
	Body struct {
		Tasks []gateTaskBody `json:"tasks"`
	}
}

// singleGateTaskOutput wraps a single task.
type singleGateTaskOutput struct {
	Body gateTaskBody
}

// submitGateResultInput carries the task_id path param and the result body.
type submitGateResultInput struct {
	TaskID string `path:"task_id"`
	Body   struct {
		Gate        string `json:"gate" doc:"Gate key (namespace/name@version)"`
		GateDigest  string `json:"gate_digest" doc:"Must match frozen task digest"`
		InputDigest string `json:"input_digest,omitempty"`
		State       string `json:"state" enum:"pass,warn,fail,needs_human_review,not_applicable,not_run"`
		Summary     string `json:"summary,omitempty"`
		Evaluator   struct {
			Executor string `json:"executor"`
			Name     string `json:"name,omitempty"`
			RunID    string `json:"run_id,omitempty"`
		} `json:"evaluator"`
		Findings []json.RawMessage `json:"findings,omitempty"`
	}
}

// submitGateResultOutput is the response after a successful result submission.
type submitGateResultOutput struct {
	Body struct {
		ResultID string `json:"result_id"`
		Trust    string `json:"trust"`
		State    string `json:"state"`
	}
}

func (h *Handlers) registerGateTaskRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list_gate_tasks",
		Method:      http.MethodGet,
		Path:        "/api/v1/gate-tasks",
		Summary:     "List pending gate tasks for an artifact (IDE agent pull)",
		Tags:        []string{"gate_tasks"},
	}, h.listGateTasks)

	huma.Register(api, huma.Operation{
		OperationID: "get_gate_task",
		Method:      http.MethodGet,
		Path:        "/api/v1/gate-tasks/{task_id}",
		Summary:     "Get a single gate task with frozen inputs and Skill content",
		Tags:        []string{"gate_tasks"},
	}, h.getGateTask)

	huma.Register(api, huma.Operation{
		OperationID: "submit_gate_result",
		Method:      http.MethodPost,
		Path:        "/api/v1/gate-tasks/{task_id}/result",
		Summary:     "IDE agent submits a GateResult for evaluation",
		Tags:        []string{"gate_tasks"},
	}, h.submitGateResult)

	huma.Register(api, huma.Operation{
		OperationID: "gate_preview",
		Method:      http.MethodGet,
		Path:        "/api/v1/artifacts/{artifact_id}/gate-preview",
		Summary:     "Preview expected gate tasks for an artifact based on stored profile snapshot",
		Tags:        []string{"gate_tasks"},
	}, h.gatePreview)

	huma.Register(api, huma.Operation{
		OperationID:   "dispatch_gate_tasks",
		Method:        http.MethodPost,
		Path:          "/api/v1/artifacts/{artifact_id}/gate-tasks",
		Summary:       "Dispatch ide_agent gate tasks for an artifact's enabled gates",
		Tags:          []string{"gate_tasks"},
		DefaultStatus: http.StatusCreated,
	}, h.dispatchGateTasks)
}

// gateTaskTTL bounds how long a dispatched gate task remains claimable.
const gateTaskTTL = 7 * 24 * time.Hour

// dispatchGateTasksInput carries the artifact_id path param.
type dispatchGateTasksInput struct {
	ArtifactID string `path:"artifact_id"`
}

// dispatchGateTasksOutput reports the tasks created (and gates skipped as already dispatched).
type dispatchGateTasksOutput struct {
	Body struct {
		ArtifactID      string   `json:"artifact_id"`
		CreatedTaskIDs  []string `json:"created_task_ids"`
		SkippedGateKeys []string `json:"skipped_gate_keys"`
	}
}

// dispatchGateTasks persists one ide_agent gate task per enabled LLM gate in the
// artifact's profile snapshot, so a coding agent can claim and evaluate them when
// no platform model is configured. Idempotent per (gate_key, gate_digest).
// per readiness-ide-agent spec §5.1
func (h *Handlers) dispatchGateTasks(ctx context.Context, in *dispatchGateTasksInput) (*dispatchGateTasksOutput, error) {
	if h.Artifacts == nil {
		return nil, huma.Error503ServiceUnavailable("artifact service not configured")
	}
	if h.GateTaskStore == nil {
		return nil, huma.Error503ServiceUnavailable("gate task store not configured")
	}
	art, err := h.Artifacts.Get(ctx, in.ArtifactID)
	if err != nil {
		if errors.Is(err, artifact.ErrNotFound) {
			return nil, huma.Error404NotFound("artifact not found")
		}
		return nil, huma.Error500InternalServerError("get artifact", err)
	}

	out := &dispatchGateTasksOutput{}
	out.Body.ArtifactID = in.ArtifactID
	out.Body.CreatedTaskIDs = []string{}
	out.Body.SkippedGateKeys = []string{}

	if art.GatesProfileSnapshotJSON == "" {
		return out, nil
	}
	snap, err := governanceprofile.ParseSnapshot(art.GatesProfileSnapshotJSON)
	if err != nil {
		// Malformed/unsupported snapshot — nothing to dispatch (mirror gatePreview).
		return out, nil
	}

	// Resolve gate rubric prompts by skill name. If a Skill cannot be loaded,
	// dispatch still provides a small built-in rubric so IDE agents have frozen
	// instructions in the task payload.
	rubricByGate := h.resolveGateRubrics(ctx, snap.GateSkills)

	// Existing (gate_key|gate_digest) tasks for idempotent re-dispatch.
	existing, err := h.GateTaskStore.ListTasksForArtifact(ctx, in.ArtifactID)
	if err != nil {
		return nil, huma.Error500InternalServerError("list gate tasks", err)
	}
	seen := make(map[string]bool, len(existing))
	for _, t := range existing {
		seen[t.GateKey+"|"+t.GateDigest] = true
	}

	profileDigest := art.GatesProfileDigest
	artifactDigest, err := policy.DigestOf(map[string]any{"artifact_id": in.ArtifactID, "version": art.Version})
	if err != nil {
		return nil, huma.Error500InternalServerError("compute artifact digest", err)
	}

	for _, gateKey := range snap.EnabledGates {
		skillContent := rubricByGate[gateKey]
		if skillContent == "" {
			skillContent = defaultGateRubric(gateKey)
		}
		gateDigest, err := policy.DigestOf(map[string]any{
			"gate_key":       gateKey,
			"skill_content":  skillContent,
			"profile_digest": profileDigest,
		})
		if err != nil {
			return nil, huma.Error500InternalServerError("compute gate digest", err)
		}
		if seen[gateKey+"|"+gateDigest] {
			out.Body.SkippedGateKeys = append(out.Body.SkippedGateKeys, gateKey)
			continue
		}
		created, err := h.GateTaskStore.CreateTask(ctx, policy.GateTaskRecord{
			ArtifactID:     in.ArtifactID,
			GateKey:        gateKey,
			GateDigest:     gateDigest,
			ArtifactDigest: artifactDigest,
			ProfileDigest:  profileDigest,
			Executor:       policy.ExecutorIDEAgent,
			SkillContent:   skillContent,
			ExpiresAt:      time.Now().Add(gateTaskTTL).UTC(),
		})
		if err != nil {
			return nil, huma.Error500InternalServerError("create gate task", err)
		}
		out.Body.CreatedTaskIDs = append(out.Body.CreatedTaskIDs, created.ID)
	}
	return out, nil
}

func defaultGateRubric(gateKey string) string {
	switch gateKey {
	case "spec_completeness":
		return "Evaluate whether the artifact includes a minimum executable contract: goal, scope, non-goals, acceptance criteria, constraints or risks, and verification. Pass only when the artifact gives enough concrete context for implementation and review; warn for minor gaps; fail for missing core contract sections."
	case "scope_clear":
		return "Evaluate whether the change scope is clear, bounded, and distinguishable from non-goals. Pass only when an implementer can tell what is in and out of scope; warn for small ambiguity; fail when the scope invites unbounded or conflicting implementation."
	case "acceptance_criteria_verifiable":
		return "Evaluate whether each acceptance criterion is observable and testable. Pass only when every criterion has a clear pass/fail check; warn for minor wording gaps; fail when one or more criteria are vague, subjective, or not tied to observable behavior."
	default:
		return "Evaluate this gate honestly against the artifact content. Return pass, warn, fail, needs_human_review, or not_applicable with specific evidence from the artifact."
	}
}

// resolveGateRubrics maps each gate key to its bound Skill's prompt text.
// Gates with no binding, a missing skill, or a blank prompt are omitted; dispatch
// supplies a built-in rubric for those gates.
func (h *Handlers) resolveGateRubrics(ctx context.Context, gateSkills map[string]string) map[string]string {
	out := map[string]string{}
	if h.Skills == nil || len(gateSkills) == 0 {
		return out
	}
	all, err := h.Skills.List(ctx)
	if err != nil {
		return out
	}
	promptByName := make(map[string]string, len(all))
	for _, s := range all {
		promptByName[s.Name] = s.Prompt
	}
	for gate, skillName := range gateSkills {
		if prompt := promptByName[skillName]; prompt != "" {
			out[gate] = prompt
		}
	}
	return out
}

func (h *Handlers) listGateTasks(ctx context.Context, in *listGateTasksInput) (*listGateTasksOutput, error) {
	if h.GateTaskStore == nil {
		return nil, huma.Error503ServiceUnavailable("gate task store not configured")
	}
	tasks, err := h.GateTaskStore.ListTasksForArtifact(ctx, in.ArtifactID)
	if err != nil {
		return nil, huma.Error500InternalServerError("list gate tasks", err)
	}
	out := &listGateTasksOutput{}
	out.Body.Tasks = make([]gateTaskBody, 0, len(tasks))
	for _, t := range tasks {
		out.Body.Tasks = append(out.Body.Tasks, recordToGateTaskBody(t))
	}
	return out, nil
}

func (h *Handlers) getGateTask(ctx context.Context, in *struct {
	TaskID string `path:"task_id"`
}) (*singleGateTaskOutput, error) {
	if h.GateTaskStore == nil {
		return nil, huma.Error503ServiceUnavailable("gate task store not configured")
	}
	t, err := h.GateTaskStore.GetTask(ctx, in.TaskID)
	if err != nil {
		if errors.Is(err, policy.ErrGateTaskNotFound) {
			return nil, huma.Error404NotFound("gate task not found", err)
		}
		return nil, huma.Error500InternalServerError("get gate task", err)
	}
	return &singleGateTaskOutput{Body: recordToGateTaskBody(*t)}, nil
}

func (h *Handlers) submitGateResult(ctx context.Context, in *submitGateResultInput) (*submitGateResultOutput, error) {
	if h.GateTaskStore == nil {
		return nil, huma.Error503ServiceUnavailable("gate task store not configured")
	}
	evaluatorJSON, _ := json.Marshal(in.Body.Evaluator)
	findingsJSON, _ := json.Marshal(in.Body.Findings)
	rec := policy.GateResultRecord{
		GateDigest:    in.Body.GateDigest,
		Executor:      in.Body.Evaluator.Executor,
		State:         in.Body.State,
		EvaluatorJSON: evaluatorJSON,
		FindingsJSON:  findingsJSON,
	}
	result, err := h.GateTaskStore.SubmitResult(ctx, in.TaskID, rec)
	if err != nil {
		switch {
		case errors.Is(err, policy.ErrGateTaskNotFound):
			return nil, huma.Error404NotFound("gate task not found", err)
		case errors.Is(err, policy.ErrStaleDigest):
			return nil, huma.Error422UnprocessableEntity("stale gate digest", err)
		case errors.Is(err, policy.ErrExecutorMismatch):
			return nil, huma.Error400BadRequest("executor mismatch", err)
		default:
			return nil, huma.Error500InternalServerError("submit gate result", err)
		}
	}
	// G1: translate an ide_agent result into a readiness run so the artifact's
	// readiness aggregation (latest-per-gate) reflects the IDE-agent verdict.
	// Safe to fail-and-retry: result submission is idempotent (one per task).
	// per readiness-ide-agent spec §5.3
	if in.Body.Evaluator.Executor == string(policy.ExecutorIDEAgent) && h.Artifacts != nil {
		task, terr := h.GateTaskStore.GetTask(ctx, in.TaskID)
		if terr != nil {
			return nil, huma.Error500InternalServerError("load gate task for readiness", terr)
		}
		evidence, _ := json.Marshal(map[string]any{"executor": "ide_agent", "findings": in.Body.Findings})
		if _, rerr := h.Artifacts.RefreshReadinessRuns(ctx, task.ArtifactID, []artifact.ReadinessEvaluation{{
			Gate:       task.GateKey,
			State:      artifact.ReadinessState(in.Body.State),
			Hint:       in.Body.Summary,
			JudgeModel: "ide_agent",
			Evidence:   string(evidence),
		}}); rerr != nil {
			return nil, huma.Error500InternalServerError("record readiness run", rerr)
		}
	}

	out := &submitGateResultOutput{}
	out.Body.ResultID = result.ID
	out.Body.Trust = string(result.Trust)
	out.Body.State = result.State
	return out, nil
}

func recordToGateTaskBody(t policy.GateTaskRecord) gateTaskBody {
	return gateTaskBody{
		TaskID:         t.ID,
		GateKey:        t.GateKey,
		GateVersion:    t.GateVersion,
		GateDigest:     t.GateDigest,
		ArtifactID:     t.ArtifactID,
		ArtifactDigest: t.ArtifactDigest,
		ProfileDigest:  t.ProfileDigest,
		Executor:       string(t.Executor),
		SkillContent:   t.SkillContent,
		ExpiresAt:      t.ExpiresAt.Format(time.RFC3339),
	}
}

// gatePreviewInput carries the artifact_id path param.
type gatePreviewInput struct {
	ArtifactID string `path:"artifact_id"`
}

// previewTaskBody is the shape of a single preview gate task (not persisted).
type previewTaskBody struct {
	GateKey      string `json:"gate_key"`
	GateVersion  string `json:"gate_version,omitempty"`
	Executor     string `json:"executor,omitempty"`
	SkillContent string `json:"skill_content,omitempty"`
	Note         string `json:"note"`
}

// gatePreviewOutput wraps the gate preview response.
type gatePreviewOutput struct {
	Body struct {
		ArtifactID   string            `json:"artifact_id"`
		PreviewTasks []previewTaskBody `json:"preview_tasks"`
	}
}

// gatePreview reads the artifact's stored GatesProfileSnapshotJSON, parses the
// governance profile, and returns a list of expected gate task shapes (not persisted).
// per spec §gate-preview
func (h *Handlers) gatePreview(ctx context.Context, in *gatePreviewInput) (*gatePreviewOutput, error) {
	if h.Artifacts == nil {
		return nil, huma.Error503ServiceUnavailable("artifact service not configured")
	}
	art, err := h.Artifacts.Get(ctx, in.ArtifactID)
	if err != nil {
		if errors.Is(err, artifact.ErrNotFound) {
			return nil, huma.Error404NotFound("artifact not found")
		}
		return nil, huma.Error500InternalServerError("get artifact", err)
	}

	out := &gatePreviewOutput{}
	out.Body.ArtifactID = in.ArtifactID
	out.Body.PreviewTasks = []previewTaskBody{}

	if art.GatesProfileSnapshotJSON == "" {
		return out, nil
	}

	// ParseSnapshot handles both legacy (ResolvedProfile) and specgate.policy/v1 shapes.
	snap, err := governanceprofile.ParseSnapshot(art.GatesProfileSnapshotJSON)
	if err != nil {
		// Malformed or unsupported snapshot — return empty preview, don't 500.
		return out, nil
	}

	for _, gateKey := range snap.EnabledGates {
		out.Body.PreviewTasks = append(out.Body.PreviewTasks, previewTaskBody{
			GateKey: gateKey,
			Note:    "preview — not persisted",
		})
	}
	return out, nil
}
