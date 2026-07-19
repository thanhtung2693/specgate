package local

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const localSemanticPolicyVersion = "local-standard@v1"

var (
	ErrGateTaskNotFound = errors.New("gate task not found")
	ErrGateTaskExpired  = errors.New("gate task expired")
	ErrGateTaskStale    = errors.New("gate task input is stale")
	ErrGateTaskInvalid  = errors.New("gate task result is invalid")
)

type localGateDefinition struct {
	Key          string `json:"key"`
	Version      string `json:"version"`
	SkillContent string `json:"skill_content"`
}

var localSemanticGates = []localGateDefinition{
	{Key: "spec_completeness", Version: "v1", SkillContent: "Evaluate whether the artifact gives an implementer a minimum executable contract: outcome, scope, non-goals, acceptance criteria, risks or constraints, and verification. Cite concrete sections. Pass only when the contract is implementable; warn for a minor gap; fail when a core element is absent."},
	{Key: "scope_clear", Version: "v1", SkillContent: "Evaluate whether scope is bounded and distinguishable from explicit non-goals. Pass only when an implementer can tell what is in and out; warn for a small ambiguity; fail when scope is conflicting or open-ended."},
	{Key: "acceptance_criteria_verifiable", Version: "v1", SkillContent: "Evaluate every acceptance criterion for an observable pass or fail check. Pass only when every criterion is independently verifiable; warn for a small wording gap; fail when any criterion is subjective or has no observable outcome."},
	{Key: "acceptance_criteria_edge_cases", Version: "v1", SkillContent: "Evaluate whether acceptance criteria cover meaningful failure and edge paths, not only the happy path. Pass when applicable edge behavior is explicit; warn when one likely edge is missing; fail when failure behavior is material and unspecified; return not_applicable only when the change has no meaningful edge path."},
	{Key: "spec_repo_drift", Version: "v1", SkillContent: "Compare the approved artifact with the repository's governed docs named by the artifact and module doc-layering rules. Report semantic contradictions only. The approved artifact wins; do not rewrite out-of-scope docs. Submit examined_docs and repo_commit. Zero findings maps to pass; one or more findings maps to warn; this gate never fails or approves delivery."},
}

func localSkillNameForGate(gate string) string {
	switch gate {
	case "spec_completeness":
		return "spec-review"
	case "scope_clear":
		return "prd-review"
	case "acceptance_criteria_verifiable", "acceptance_criteria_edge_cases":
		return "acceptance-criteria"
	case "spec_repo_drift":
		return "review-impl"
	default:
		return ""
	}
}

type GateTask struct {
	TaskID         string `json:"task_id"`
	WorkspaceID    string `json:"workspace_id"`
	GateKey        string `json:"gate_key"`
	GateVersion    string `json:"gate_version"`
	GateDigest     string `json:"gate_digest"`
	ArtifactID     string `json:"artifact_id"`
	ArtifactDigest string `json:"artifact_digest"`
	PolicyDigest   string `json:"policy_digest"`
	Executor       string `json:"executor"`
	SkillContent   string `json:"skill_content"`
	ExpiresAt      string `json:"expires_at"`
}

type GateResultInput struct {
	Gate        string `json:"gate"`
	GateDigest  string `json:"gate_digest"`
	InputDigest string `json:"input_digest"`
	State       string `json:"state"`
	Summary     string `json:"summary,omitempty"`
	Evaluator   struct {
		Executor string `json:"executor"`
		Name     string `json:"name,omitempty"`
		RunID    string `json:"run_id,omitempty"`
	} `json:"evaluator"`
	Evidence struct {
		ExaminedDocs []string `json:"examined_docs,omitempty"`
		RepoCommit   string   `json:"repo_commit,omitempty"`
	} `json:"evidence,omitempty"`
	Findings []json.RawMessage `json:"findings,omitempty"`
}

type GateResult struct {
	ResultID string `json:"result_id"`
	Trust    string `json:"trust"`
	State    string `json:"state"`
}

type DispatchGateTasksResult struct {
	ArtifactID      string   `json:"artifact_id"`
	CreatedTaskIDs  []string `json:"created_task_ids"`
	SkippedGateKeys []string `json:"skipped_gate_keys"`
	PendingTaskIDs  []string `json:"pending_task_ids"`
}

type gateTaskRow struct {
	GateTask
	ResultID      sql.NullString
	ResultState   sql.NullString
	ResultSummary sql.NullString
	EvaluatorJSON sql.NullString
	EvidenceJSON  sql.NullString
	FindingsJSON  sql.NullString
	SubmittedAt   sql.NullString
}

func localGateDigest(policyDigest string, gate localGateDefinition) string {
	return digestText(strings.Join([]string{policyDigest, gate.Key, gate.Version, gate.SkillContent}, "\n"))
}

func (s *Store) DispatchGateTasks(ctx context.Context, workspaceID, artifactID string) (DispatchGateTasksResult, error) {
	return s.dispatchGateTasks(ctx, workspaceID, artifactID, true)
}

func (s *Store) dispatchReadinessGateTasks(ctx context.Context, workspaceID, artifactID string) (DispatchGateTasksResult, error) {
	return s.dispatchGateTasks(ctx, workspaceID, artifactID, false)
}

func (s *Store) dispatchGateTasks(ctx context.Context, workspaceID, artifactID string, includeDrift bool) (DispatchGateTasksResult, error) {
	artifact, err := s.GetArtifact(ctx, workspaceID, artifactID)
	if err != nil {
		return DispatchGateTasksResult{}, err
	}
	policyDigest := strings.TrimSpace(artifact.PolicyDigest)
	if policyDigest == "" {
		return DispatchGateTasksResult{}, fmt.Errorf("artifact %s is missing frozen policy digest", artifact.ID)
	}
	definitions, err := frozenLocalGateDefinitions(artifact)
	if err != nil {
		return DispatchGateTasksResult{}, err
	}
	now := time.Now().UTC()
	result := DispatchGateTasksResult{ArtifactID: artifact.ID}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	for _, definition := range definitions {
		if definition.Key == "spec_repo_drift" && (!includeDrift || artifact.Status != "approved") {
			continue
		}
		gateDigest := localGateDigest(policyDigest, definition)
		rows, err := queryGateTaskRows(ctx, tx, workspaceID, artifact.ID, definition.Key, gateDigest)
		if err != nil {
			return result, err
		}
		current := false
		for _, row := range rows {
			expiresAt, err := time.Parse(time.RFC3339Nano, row.ExpiresAt)
			if err != nil {
				return result, err
			}
			if row.ResultID.Valid || expiresAt.After(now) {
				current = true
				break
			}
		}
		if current {
			result.SkippedGateKeys = append(result.SkippedGateKeys, definition.Key)
			continue
		}
		id, err := newID()
		if err != nil {
			return result, err
		}
		task := GateTask{TaskID: id, WorkspaceID: workspaceID, GateKey: definition.Key, GateVersion: definition.Version, GateDigest: gateDigest, ArtifactID: artifact.ID, ArtifactDigest: artifact.SnapshotDigest, PolicyDigest: policyDigest, Executor: "ide_agent", SkillContent: definition.SkillContent, ExpiresAt: now.Add(7 * 24 * time.Hour).Format(time.RFC3339Nano)}
		if _, err := tx.ExecContext(ctx, `INSERT INTO local_gate_tasks(id, workspace_id, artifact_id, gate_key, gate_version, gate_digest, artifact_digest, policy_digest, executor, skill_content, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, task.TaskID, task.WorkspaceID, task.ArtifactID, task.GateKey, task.GateVersion, task.GateDigest, task.ArtifactDigest, task.PolicyDigest, task.Executor, task.SkillContent, task.ExpiresAt, now.Format(time.RFC3339Nano)); err != nil {
			return result, err
		}
		result.CreatedTaskIDs = append(result.CreatedTaskIDs, task.TaskID)
	}
	pending, err := pendingTaskIDs(ctx, tx, workspaceID, artifact.ID, now)
	if err != nil {
		return result, err
	}
	result.PendingTaskIDs = pending
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Store) ListGateTasks(ctx context.Context, workspaceID, artifactID string) ([]GateTask, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, workspace_id, gate_key, gate_version, gate_digest, artifact_id, artifact_digest, policy_digest, executor, skill_content, expires_at FROM local_gate_tasks WHERE workspace_id = ? AND artifact_id = ? AND result_id IS NULL AND expires_at > ? ORDER BY created_at, id`, workspaceID, artifactID, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []GateTask
	for rows.Next() {
		var task GateTask
		if err := rows.Scan(&task.TaskID, &task.WorkspaceID, &task.GateKey, &task.GateVersion, &task.GateDigest, &task.ArtifactID, &task.ArtifactDigest, &task.PolicyDigest, &task.Executor, &task.SkillContent, &task.ExpiresAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *Store) GetGateTask(ctx context.Context, workspaceID, taskID string) (GateTask, error) {
	row, err := getGateTaskRow(ctx, s.db, workspaceID, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return GateTask{}, ErrGateTaskNotFound
	}
	return row.GateTask, err
}

func (s *Store) SubmitGateResult(ctx context.Context, workspaceID, taskID string, input GateResultInput) (GateResult, error) {
	for attempt := 0; ; attempt++ {
		result, err := s.submitGateResultOnce(ctx, workspaceID, taskID, input)
		if err == nil || !isSQLiteBusy(err) || attempt == 4 {
			return result, err
		}
		select {
		case <-ctx.Done():
			return GateResult{}, ctx.Err()
		case <-time.After(time.Duration(1<<attempt) * 10 * time.Millisecond):
		}
	}
}

func (s *Store) submitGateResultOnce(ctx context.Context, workspaceID, taskID string, input GateResultInput) (GateResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return GateResult{}, err
	}
	defer tx.Rollback()
	task, err := getGateTaskRow(ctx, tx, workspaceID, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return GateResult{}, ErrGateTaskNotFound
	}
	if err != nil {
		return GateResult{}, err
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, task.ExpiresAt)
	if err != nil {
		return GateResult{}, err
	}
	if !expiresAt.After(time.Now().UTC()) {
		return GateResult{}, ErrGateTaskExpired
	}
	artifact, err := getArtifactTx(ctx, tx, workspaceID, task.ArtifactID)
	if errors.Is(err, sql.ErrNoRows) {
		return GateResult{}, ErrGateTaskStale
	}
	if err != nil {
		return GateResult{}, err
	}
	if artifact.SnapshotDigest != task.ArtifactDigest || artifact.PolicyDigest != task.PolicyDigest {
		return GateResult{}, ErrGateTaskStale
	}
	if input.Gate != task.GateKey || input.GateDigest != task.GateDigest || input.InputDigest != task.ArtifactDigest || input.Evaluator.Executor != task.Executor {
		return GateResult{}, ErrGateTaskInvalid
	}
	if !validGateState(input.State) {
		return GateResult{}, ErrGateTaskInvalid
	}
	state := input.State
	if task.GateKey == "spec_repo_drift" {
		if len(input.Evidence.ExaminedDocs) == 0 || strings.TrimSpace(input.Evidence.RepoCommit) == "" {
			return GateResult{}, fmt.Errorf("%w: spec_repo_drift requires non-empty evidence.examined_docs and evidence.repo_commit", ErrGateTaskInvalid)
		}
		state = "pass"
		if len(input.Findings) > 0 {
			state = "warn"
		}
	}
	resultID, err := newID()
	if err != nil {
		return GateResult{}, err
	}
	evaluatorJSON, err := json.Marshal(input.Evaluator)
	if err != nil {
		return GateResult{}, err
	}
	evidenceJSON, err := json.Marshal(map[string]any{"input_digest": task.ArtifactDigest, "evaluator": input.Evaluator, "evidence": input.Evidence, "findings": input.Findings})
	if err != nil {
		return GateResult{}, err
	}
	findingsJSON, err := json.Marshal(input.Findings)
	if err != nil {
		return GateResult{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `UPDATE local_gate_tasks SET result_id = ?, result_state = ?, result_summary = ?, evaluator_json = ?, evidence_json = ?, findings_json = ?, submitted_at = ? WHERE id = ? AND workspace_id = ?`, resultID, state, strings.TrimSpace(input.Summary), evaluatorJSON, evidenceJSON, findingsJSON, now, task.TaskID, workspaceID); err != nil {
		return GateResult{}, err
	}
	if _, err := recordReadinessRunTx(ctx, tx, workspaceID, artifact); err != nil {
		return GateResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return GateResult{}, err
	}
	return GateResult{ResultID: resultID, Trust: "agent_attested", State: state}, nil
}

func isSQLiteBusy(err error) bool {
	var sqliteError *sqlite.Error
	return errors.As(err, &sqliteError) && sqliteError.Code()&0xff == sqlite3.SQLITE_BUSY
}

func validGateState(state string) bool {
	switch state {
	case "pass", "warn", "fail", "needs_human_review", "not_applicable", "not_run":
		return true
	default:
		return false
	}
}

type gateTaskQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func queryGateTaskRows(ctx context.Context, q gateTaskQuerier, workspaceID, artifactID, gateKey, gateDigest string) ([]gateTaskRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, workspace_id, gate_key, gate_version, gate_digest, artifact_id, artifact_digest, policy_digest, executor, skill_content, expires_at, result_id, result_state, result_summary, evaluator_json, evidence_json, findings_json, submitted_at FROM local_gate_tasks WHERE workspace_id = ? AND artifact_id = ? AND gate_key = ? AND gate_digest = ? ORDER BY created_at, id`, workspaceID, artifactID, gateKey, gateDigest)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []gateTaskRow
	for rows.Next() {
		var task gateTaskRow
		if err := scanGateTaskRow(rows, &task); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func getGateTaskRow(ctx context.Context, q gateTaskQuerier, workspaceID, taskID string) (gateTaskRow, error) {
	var task gateTaskRow
	err := scanGateTaskRow(q.QueryRowContext(ctx, `SELECT id, workspace_id, gate_key, gate_version, gate_digest, artifact_id, artifact_digest, policy_digest, executor, skill_content, expires_at, result_id, result_state, result_summary, evaluator_json, evidence_json, findings_json, submitted_at FROM local_gate_tasks WHERE workspace_id = ? AND id = ?`, workspaceID, taskID), &task)
	return task, err
}

type rowScanner interface{ Scan(...any) error }

func scanGateTaskRow(row rowScanner, task *gateTaskRow) error {
	return row.Scan(&task.TaskID, &task.WorkspaceID, &task.GateKey, &task.GateVersion, &task.GateDigest, &task.ArtifactID, &task.ArtifactDigest, &task.PolicyDigest, &task.Executor, &task.SkillContent, &task.ExpiresAt, &task.ResultID, &task.ResultState, &task.ResultSummary, &task.EvaluatorJSON, &task.EvidenceJSON, &task.FindingsJSON, &task.SubmittedAt)
}

func pendingTaskIDs(ctx context.Context, q gateTaskQuerier, workspaceID, artifactID string, now time.Time) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT id FROM local_gate_tasks WHERE workspace_id = ? AND artifact_id = ? AND result_id IS NULL AND expires_at > ? ORDER BY created_at, id`, workspaceID, artifactID, now.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func semanticChecks(ctx context.Context, q gateTaskQuerier, workspaceID string, artifact Artifact, definitions []localGateDefinition) (map[string]any, error) {
	expected := make(map[string]string, len(definitions))
	checks := make(map[string]any, len(definitions))
	for _, definition := range definitions {
		if definition.Key == "spec_repo_drift" && artifact.Status != "approved" {
			continue
		}
		expected[definition.Key] = localGateDigest(artifact.PolicyDigest, definition)
		checks[definition.Key] = map[string]any{
			"gate": definition.Key, "state": "not_run", "hint": "IDE-agent result required",
		}
	}
	rows, err := q.QueryContext(ctx, `SELECT id, workspace_id, gate_key, gate_version, gate_digest, artifact_id, artifact_digest, policy_digest, executor, skill_content, expires_at, result_id, result_state, result_summary, evaluator_json, evidence_json, findings_json, submitted_at FROM local_gate_tasks WHERE workspace_id = ? AND artifact_id = ? AND artifact_digest = ? AND policy_digest = ? ORDER BY created_at, id`, workspaceID, artifact.ID, artifact.SnapshotDigest, artifact.PolicyDigest)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var task gateTaskRow
		if err := scanGateTaskRow(rows, &task); err != nil {
			return nil, err
		}
		if expected[task.GateKey] == "" || task.GateDigest != expected[task.GateKey] {
			continue
		}
		check := map[string]any{"gate": task.GateKey, "task_id": task.TaskID, "judge_model": "ide_agent", "trust": "agent_attested"}
		if task.ResultID.Valid {
			check["state"] = task.ResultState.String
			check["hint"] = task.ResultSummary.String
			check["evidence"] = json.RawMessage(task.EvidenceJSON.String)
		} else {
			check["state"] = "not_run"
			check["hint"] = "IDE-agent result required"
		}
		checks[task.GateKey] = check
	}
	return checks, rows.Err()
}

func aggregateChecks(checks map[string]any) string {
	states := map[string]bool{}
	for _, raw := range checks {
		check, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		state, _ := check["state"].(string)
		if state == "not_applicable" {
			state = "pass"
		}
		states[state] = true
	}
	for _, state := range []string{"fail", "needs_human_review", "not_run", "warn"} {
		if states[state] {
			return state
		}
	}
	return "pass"
}

func sortedChecks(checks map[string]any) []string {
	keys := make([]string, 0, len(checks))
	for key := range checks {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func getArtifactTx(ctx context.Context, tx *sql.Tx, workspaceID, artifactID string) (Artifact, error) {
	var artifact Artifact
	err := tx.QueryRowContext(ctx, `SELECT id, workspace_id, feature_key, request_type, version, status, snapshot_digest, policy_digest, policy_snapshot_json, created_at FROM artifacts WHERE workspace_id = ? AND id = ?`, workspaceID, artifactID).Scan(&artifact.ID, &artifact.WorkspaceID, &artifact.FeatureKey, &artifact.RequestType, &artifact.Version, &artifact.Status, &artifact.SnapshotDigest, &artifact.PolicyDigest, &artifact.PolicySnapshot, &artifact.CreatedAt)
	if err != nil {
		return Artifact{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT path, role, content, digest FROM artifact_documents WHERE artifact_id = ? ORDER BY path`, artifactID)
	if err != nil {
		return Artifact{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var document ArtifactDocument
		if err := rows.Scan(&document.Path, &document.Role, &document.Content, &document.Digest); err != nil {
			return Artifact{}, err
		}
		artifact.Documents = append(artifact.Documents, document)
	}
	return artifact, rows.Err()
}

func recordReadinessRunTx(ctx context.Context, tx *sql.Tx, workspaceID string, artifact Artifact) (ReadinessRun, error) {
	checks, err := readinessChecks(ctx, tx, workspaceID, artifact)
	if err != nil {
		return ReadinessRun{}, err
	}
	evidence, err := json.Marshal(checks)
	if err != nil {
		return ReadinessRun{}, err
	}
	id, err := newID()
	if err != nil {
		return ReadinessRun{}, err
	}
	run := ReadinessRun{ID: id, ArtifactID: artifact.ID, Aggregate: aggregateChecks(checks), Checks: checks, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if _, err := tx.ExecContext(ctx, `INSERT INTO artifact_readiness_runs(id, artifact_id, workspace_id, aggregate, evidence, created_at) VALUES (?, ?, ?, ?, ?, ?)`, run.ID, run.ArtifactID, workspaceID, run.Aggregate, evidence, run.CreatedAt); err != nil {
		return ReadinessRun{}, err
	}
	return run, nil
}

func readinessChecks(ctx context.Context, q gateTaskQuerier, workspaceID string, artifact Artifact) (map[string]any, error) {
	definitions, err := frozenLocalGateDefinitions(artifact)
	if err != nil {
		return nil, err
	}
	hasSpec := false
	for _, document := range artifact.Documents {
		if document.Role == "spec" {
			hasSpec = true
			break
		}
	}
	checks := map[string]any{
		"has_documents": map[string]any{"gate": "has_documents", "state": "pass", "hint": "artifact contains immutable documents"},
		"has_spec":      map[string]any{"gate": "has_spec", "state": "pass", "hint": "spec document present"},
	}
	if len(artifact.Documents) == 0 {
		checks["has_documents"] = map[string]any{"gate": "has_documents", "state": "fail", "hint": "add immutable documents and publish a new version"}
	}
	if !hasSpec {
		checks["has_spec"] = map[string]any{"gate": "has_spec", "state": "fail", "hint": "add a document with role 'spec' and publish a new version"}
	}
	semantic, err := semanticChecks(ctx, q, workspaceID, artifact, definitions)
	if err != nil {
		return nil, err
	}
	for _, key := range sortedChecks(semantic) {
		checks[key] = semantic[key]
	}
	return checks, nil
}

func (s *Store) ListReadinessRuns(ctx context.Context, workspaceID, artifactID string) ([]ReadinessRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, artifact_id, aggregate, evidence, created_at FROM artifact_readiness_runs WHERE workspace_id = ? AND artifact_id = ? ORDER BY created_at DESC, id DESC`, workspaceID, artifactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []ReadinessRun
	for rows.Next() {
		var run ReadinessRun
		var evidence []byte
		if err := rows.Scan(&run.ID, &run.ArtifactID, &run.Aggregate, &evidence, &run.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(evidence, &run.Checks); err != nil {
			return nil, fmt.Errorf("decode readiness evidence: %w", err)
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}
