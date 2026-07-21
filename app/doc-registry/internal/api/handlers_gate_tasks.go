package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/policy"
)

// listGateTasksInput has the query param for artifact_id.
type listGateTasksInput struct {
	WorkspaceID string `query:"workspace_id" required:"true"`
	ArtifactID  string `query:"artifact_id" doc:"Filter gate tasks by artifact ID"`
}

// gateTaskBody is the response shape for a single gate task.
type gateTaskBody struct {
	TaskID         string `json:"task_id"`
	WorkspaceID    string `json:"workspace_id"`
	GateKey        string `json:"gate_key"`
	GateVersion    string `json:"gate_version"`
	GateDigest     string `json:"gate_digest"`
	ArtifactID     string `json:"artifact_id"`
	ArtifactDigest string `json:"artifact_digest"`
	PolicyDigest   string `json:"policy_digest"`
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
	TaskID      string `path:"task_id"`
	WorkspaceID string `query:"workspace_id" required:"true"`
	Body        struct {
		Gate        string `json:"gate" doc:"Frozen gate key; must match the task"`
		GateDigest  string `json:"gate_digest" doc:"Must match frozen task digest"`
		InputDigest string `json:"input_digest,omitempty"`
		State       string `json:"state" enum:"pass,warn,fail,needs_human_review,not_applicable,not_run"`
		Summary     string `json:"summary,omitempty"`
		Evaluator   struct {
			Executor string `json:"executor"`
			Name     string `json:"name,omitempty"`
			RunID    string `json:"run_id,omitempty"`
		} `json:"evaluator"`
		// Evidence carries the spec_repo_drift attestation (design §4). Optional for
		// other gates; required for spec_repo_drift.
		Evidence struct {
			ExaminedDocs []string `json:"examined_docs,omitempty" doc:"Repo-relative doc paths the agent read (spec_repo_drift attestation)"`
			RepoCommit   string   `json:"repo_commit,omitempty" doc:"Checkout commit SHA the examination ran against (spec_repo_drift attestation)"`
		} `json:"evidence,omitempty"`
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
		Summary:     "Preview expected gate tasks for an artifact based on stored policy snapshot",
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
	ArtifactID  string `path:"artifact_id"`
	WorkspaceID string `query:"workspace_id" required:"true"`
}

// dispatchGateTasksOutput reports the tasks created (and gates skipped as already dispatched).
type dispatchGateTasksOutput struct {
	Body struct {
		ArtifactID      string   `json:"artifact_id"`
		CreatedTaskIDs  []string `json:"created_task_ids"`
		SkippedGateKeys []string `json:"skipped_gate_keys"`
		PendingTaskIDs  []string `json:"pending_task_ids"`
	}
}

// dispatchGateTasks persists one ide_agent task per enabled model-judged gate in
// the artifact's policy snapshot, so a coding agent can claim and evaluate them
// when no platform model is configured. Idempotent per (gate_key, gate_digest).
// per readiness-ide-agent spec §5.1
func (h *Handlers) dispatchGateTasks(ctx context.Context, in *dispatchGateTasksInput) (*dispatchGateTasksOutput, error) {
	if h.Artifacts == nil {
		return nil, huma.Error503ServiceUnavailable("artifact service not configured")
	}
	if h.GateTaskStore == nil {
		return nil, huma.Error503ServiceUnavailable("gate task store not configured")
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = policy.WithWorkspace(ctx, workspaceID)
	ctx = artifact.WithWorkspace(ctx, workspaceID)
	art, err := getArtifactForWorkspace(ctx, h.Artifacts, workspaceID, in.ArtifactID)
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
	out.Body.PendingTaskIDs = []string{}

	if art.PolicySnapshotJSON == "" {
		return out, nil
	}
	snap, err := governanceprofile.ParseSnapshot(art.PolicySnapshotJSON)
	if err != nil {
		// Malformed/unsupported snapshot — nothing to dispatch (mirror gatePreview).
		return out, nil
	}

	policyDigest := art.PolicyDigest
	artifactDigest := art.SnapshotDigest
	if artifactDigest == "" {
		return nil, huma.Error500InternalServerError("artifact missing snapshot digest")
	}

	for _, gateKey := range snap.EnabledGates {
		if gateKey == "spec_repo_drift" && art.Status != artifact.StatusApproved {
			continue
		}
		definition := frozenGateDefinition(snap, gateKey)
		skillContent := definition.SkillContent
		if skillContent == "" {
			skillContent = defaultGateRubric(gateKey)
		}
		gateDigest, err := policy.DigestOf(map[string]any{
			"gate_key":      gateKey,
			"gate_version":  definition.Version,
			"skill_content": skillContent,
			"policy_digest": policyDigest,
		})
		if err != nil {
			return nil, huma.Error500InternalServerError("compute gate digest", err)
		}
		now := time.Now().UTC()
		created, wasCreated, err := h.GateTaskStore.CreateTaskIfCurrentMissing(ctx, policy.GateTaskRecord{
			WorkspaceID:    workspaceID,
			ArtifactID:     in.ArtifactID,
			GateKey:        gateKey,
			GateVersion:    definition.Version,
			GateDigest:     gateDigest,
			ArtifactDigest: artifactDigest,
			PolicyDigest:   policyDigest,
			Executor:       policy.ExecutorIDEAgent,
			SkillContent:   skillContent,
			ExpiresAt:      now.Add(gateTaskTTL),
		}, now)
		if err != nil {
			return nil, huma.Error500InternalServerError("create gate task", err)
		}
		if !wasCreated {
			out.Body.SkippedGateKeys = append(out.Body.SkippedGateKeys, gateKey)
			continue
		}
		out.Body.CreatedTaskIDs = append(out.Body.CreatedTaskIDs, created.ID)
	}
	pending, err := h.GateTaskStore.ListTasksForArtifact(ctx, in.ArtifactID)
	if err != nil {
		return nil, huma.Error500InternalServerError("list gate tasks", err)
	}
	for _, task := range pending {
		out.Body.PendingTaskIDs = append(out.Body.PendingTaskIDs, task.ID)
	}
	return out, nil
}

// driftVerdict enforces the spec_repo_drift attestation contract (design §4) and
// maps the verdict (§4/§5): a valid attestation requires a non-empty examined_docs[]
// and a repo_commit; zero findings → pass, one or more → warn. Drift never fails or
// blocks. An invalid attestation is rejected (non-nil error) so the caller returns 400.
func driftVerdict(examinedDocs []string, repoCommit string, findingsCount int) (string, error) {
	if len(examinedDocs) == 0 {
		return "", fmt.Errorf("evidence.examined_docs[] must be non-empty")
	}
	if repoCommit == "" {
		return "", fmt.Errorf("evidence.repo_commit is required")
	}
	if findingsCount == 0 {
		return "pass", nil
	}
	return "warn", nil
}

func defaultGateRubric(gateKey string) string {
	switch gateKey {
	case "spec_completeness":
		return "Evaluate whether the artifact includes a minimum executable contract: goal, scope, non-goals, acceptance criteria, constraints or risks, and verification. Pass only when the artifact gives enough concrete context for implementation and review; warn for minor gaps; fail for missing core contract sections."
	case "scope_clear":
		return "Evaluate whether the change scope is clear, bounded, and distinguishable from non-goals. Pass only when an implementer can tell what is in and out of scope; warn for small ambiguity; fail when the scope invites unbounded or conflicting implementation."
	case "acceptance_criteria_verifiable":
		return "Evaluate whether each acceptance criterion is observable and testable. Pass only when every criterion has a clear pass/fail check; warn for minor wording gaps; fail when one or more criteria are vague, subjective, or not tied to observable behavior."
	case "spec_repo_drift":
		return "Detect where the repository's governed docs contradict the approved spec artifact. " +
			"Examine the doc-layering convention for each module the spec governs: the module's docs/spec.md, " +
			"docs/prd.md, and README.md, plus any repo docs the spec text itself names. Judge semantic " +
			"contradictions against the frozen approved spec content — report a finding when a doc's claim " +
			"conflicts with the spec. Two rules bind you: (1) the approved spec outranks the drifted doc — do " +
			"not follow the doc or rewrite it to match your reading; (2) do not rewrite repo docs outside the " +
			"current work item's scope — report the drift as a finding instead. Drift warns, never blocks."
	default:
		return "Evaluate this gate honestly against the artifact content. Return pass, warn, fail, needs_human_review, or not_applicable with specific evidence from the artifact."
	}
}

func frozenGateDefinition(snapshot governanceprofile.ParsedSnapshot, gateKey string) governanceprofile.GateDefinition {
	for _, definition := range snapshot.GateDefinitions {
		if definition.Key == gateKey {
			if definition.Version == "" {
				definition.Version = "v1"
			}
			return definition
		}
	}
	return governanceprofile.GateDefinition{Key: gateKey, Version: "v1"}
}

func (h *Handlers) listGateTasks(ctx context.Context, in *listGateTasksInput) (*listGateTasksOutput, error) {
	if h.GateTaskStore == nil {
		return nil, huma.Error503ServiceUnavailable("gate task store not configured")
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = policy.WithWorkspace(ctx, workspaceID)
	ctx = artifact.WithWorkspace(ctx, workspaceID)
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
	TaskID      string `path:"task_id"`
	WorkspaceID string `query:"workspace_id" required:"true"`
}) (*singleGateTaskOutput, error) {
	if h.GateTaskStore == nil {
		return nil, huma.Error503ServiceUnavailable("gate task store not configured")
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = policy.WithWorkspace(ctx, workspaceID)
	ctx = artifact.WithWorkspace(ctx, workspaceID)
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
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = policy.WithWorkspace(ctx, workspaceID)
	ctx = artifact.WithWorkspace(ctx, workspaceID)
	// Load the task up front so drift validation and the readiness fold share one
	// authoritative gate key (the client-supplied Gate field is not trusted here).
	task, terr := h.GateTaskStore.GetTask(ctx, in.TaskID)
	if terr != nil {
		if errors.Is(terr, policy.ErrGateTaskNotFound) {
			return nil, huma.Error404NotFound("gate task not found", terr)
		}
		return nil, huma.Error500InternalServerError("load gate task", terr)
	}
	if strings.TrimSpace(in.Body.Gate) != task.GateKey {
		return nil, huma.Error400BadRequest("gate does not match frozen task")
	}
	state := in.Body.State
	// spec_repo_drift: enforce the attestation contract and map the verdict from
	// findings — never trusting the client-submitted state. per spec-repo-drift §4/§5
	if task.GateKey == "spec_repo_drift" {
		v, verr := driftVerdict(in.Body.Evidence.ExaminedDocs, in.Body.Evidence.RepoCommit, len(in.Body.Findings))
		if verr != nil {
			return nil, huma.Error400BadRequest("invalid spec_repo_drift attestation", verr)
		}
		state = v
	}
	evaluatorJSON, _ := json.Marshal(in.Body.Evaluator)
	evidenceJSON, _ := json.Marshal(in.Body.Evidence)
	findingsJSON, _ := json.Marshal(in.Body.Findings)
	rec := policy.GateResultRecord{
		GateDigest:    in.Body.GateDigest,
		InputDigest:   in.Body.InputDigest,
		Executor:      in.Body.Evaluator.Executor,
		State:         state,
		Summary:       in.Body.Summary,
		EvaluatorJSON: evaluatorJSON,
		EvidenceJSON:  evidenceJSON,
		FindingsJSON:  findingsJSON,
	}
	result, err := h.GateTaskStore.SubmitResult(ctx, in.TaskID, rec)
	if err != nil {
		switch {
		case errors.Is(err, policy.ErrGateTaskNotFound):
			return nil, huma.Error404NotFound("gate task not found", err)
		case errors.Is(err, policy.ErrStaleDigest):
			return nil, huma.Error422UnprocessableEntity("stale gate digest", err)
		case errors.Is(err, policy.ErrInputDigestMismatch):
			return nil, huma.Error422UnprocessableEntity("stale input digest", err)
		case errors.Is(err, policy.ErrGateTaskExpired):
			return nil, huma.Error422UnprocessableEntity("gate task expired", err)
		case errors.Is(err, policy.ErrExecutorMismatch):
			return nil, huma.Error400BadRequest("executor mismatch", err)
		default:
			return nil, huma.Error500InternalServerError("submit gate result", err)
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
		WorkspaceID:    t.WorkspaceID,
		GateKey:        t.GateKey,
		GateVersion:    t.GateVersion,
		GateDigest:     t.GateDigest,
		ArtifactID:     t.ArtifactID,
		ArtifactDigest: t.ArtifactDigest,
		PolicyDigest:   t.PolicyDigest,
		Executor:       string(t.Executor),
		SkillContent:   t.SkillContent,
		ExpiresAt:      t.ExpiresAt.Format(time.RFC3339),
	}
}

// gatePreviewInput carries the artifact_id path param.
type gatePreviewInput struct {
	ArtifactID  string `path:"artifact_id"`
	WorkspaceID string `query:"workspace_id" required:"true"`
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

// gatePreview reads the artifact's stored PolicySnapshotJSON, parses the
// automatic governance policy, and returns a list of expected gate task shapes (not persisted).
// per spec §gate-preview
func (h *Handlers) gatePreview(ctx context.Context, in *gatePreviewInput) (*gatePreviewOutput, error) {
	if h.Artifacts == nil {
		return nil, huma.Error503ServiceUnavailable("artifact service not configured")
	}
	workspaceID, err := requireWorkspaceID(in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ctx = policy.WithWorkspace(ctx, workspaceID)
	ctx = artifact.WithWorkspace(ctx, workspaceID)
	art, err := getArtifactForWorkspace(ctx, h.Artifacts, workspaceID, in.ArtifactID)
	if err != nil {
		if errors.Is(err, artifact.ErrNotFound) {
			return nil, huma.Error404NotFound("artifact not found")
		}
		return nil, huma.Error500InternalServerError("get artifact", err)
	}

	out := &gatePreviewOutput{}
	out.Body.ArtifactID = in.ArtifactID
	out.Body.PreviewTasks = []previewTaskBody{}

	if art.PolicySnapshotJSON == "" {
		return out, nil
	}

	// ParseSnapshot accepts the required versioned policy snapshot.
	snap, err := governanceprofile.ParseSnapshot(art.PolicySnapshotJSON)
	if err != nil {
		// Malformed or unsupported snapshot — return empty preview, don't 500.
		return out, nil
	}

	for _, gateKey := range snap.EnabledGates {
		definition := frozenGateDefinition(snap, gateKey)
		skillContent := definition.SkillContent
		if skillContent == "" {
			skillContent = defaultGateRubric(gateKey)
		}
		out.Body.PreviewTasks = append(out.Body.PreviewTasks, previewTaskBody{
			GateKey:      gateKey,
			GateVersion:  definition.Version,
			Executor:     string(policy.ExecutorIDEAgent),
			SkillContent: skillContent,
		})
	}
	return out, nil
}
