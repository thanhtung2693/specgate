package governanceops

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/agentsclient"
	"github.com/specgate/doc-registry/internal/artifact"
)

// fakeAgentsRunner implements AgentsRunner for unit tests.
type fakeAgentsRunner struct {
	verdict  *agentsclient.Verdict
	gatesOut map[string]any
	delivOut map[string]any
	qwOut    map[string]any
	qwErr    error
	qwACs    []AcceptanceCriterionInput
	qwActor  string
	qwWS     string
}

func (f *fakeAgentsRunner) RunReadiness(_ context.Context, artifactID string) (*agentsclient.Verdict, error) {
	if f.verdict == nil {
		return &agentsclient.Verdict{ArtifactID: artifactID}, nil
	}
	return f.verdict, nil
}
func (f *fakeAgentsRunner) RunLLMGates(_ context.Context, _ string) (map[string]any, error) {
	return f.gatesOut, nil
}
func (f *fakeAgentsRunner) ReviewDelivery(_ context.Context, _ string) (map[string]any, error) {
	return f.delivOut, nil
}
func (f *fakeAgentsRunner) CreateQuickWorkItem(_ context.Context, _, _, _, _, _, _ string, acceptanceCriteria []AcceptanceCriterionInput, createdBy string, workspaceID string) (map[string]any, error) {
	f.qwACs = acceptanceCriteria
	f.qwActor = createdBy
	f.qwWS = workspaceID
	if f.qwErr != nil {
		return nil, f.qwErr
	}
	return f.qwOut, nil
}

// --- aggregateReadiness tests (ported from tools/specgate_readiness.go) ---

func TestAggregateReadiness_FailPrecedence(t *testing.T) {
	t.Parallel()
	runs := []agentsclient.ReadinessRun{
		{Gate: "spec_completeness", State: "fail", CreatedAt: "2024-01-01T00:00:00Z"},
		{Gate: "rollback_plan", State: "warn", CreatedAt: "2024-01-01T00:00:00Z"},
	}
	if got := aggregateReadiness(runs); got != "fail" {
		t.Fatalf("aggregate = %q, want fail", got)
	}
}

func TestAggregateReadiness_NeedsHumanReview(t *testing.T) {
	t.Parallel()
	runs := []agentsclient.ReadinessRun{
		{Gate: "spec_completeness", State: "needs_human_review", CreatedAt: "2024-01-01T00:00:00Z"},
		{Gate: "rollback_plan", State: "pass", CreatedAt: "2024-01-01T00:00:00Z"},
	}
	if got := aggregateReadiness(runs); got != "needs_human_review" {
		t.Fatalf("aggregate = %q, want needs_human_review", got)
	}
}

func TestAggregateReadiness_WarnPrecedence(t *testing.T) {
	t.Parallel()
	runs := []agentsclient.ReadinessRun{
		{Gate: "spec_completeness", State: "warn", CreatedAt: "2024-01-01T00:00:00Z"},
		{Gate: "rollback_plan", State: "pass", CreatedAt: "2024-01-01T00:00:00Z"},
	}
	if got := aggregateReadiness(runs); got != "warn" {
		t.Fatalf("aggregate = %q, want warn", got)
	}
}

// TestAggregateReadiness_SpecRepoDriftWarnSurfaces proves AC1 (per spec-repo-drift
// §7): a spec_repo_drift warn readiness run is not dropped by the aggregate — warn
// precedence is honored over a passing gate. Bound to the delivery check
// "aggregate-surfacing-test" so AC1 resolves deterministically.
func TestAggregateReadiness_SpecRepoDriftWarnSurfaces(t *testing.T) {
	t.Parallel()
	runs := []agentsclient.ReadinessRun{
		{Gate: "spec_completeness", State: "pass", CreatedAt: "2024-01-01T00:00:00Z"},
		{Gate: "spec_repo_drift", State: "warn", CreatedAt: "2024-01-01T00:00:00Z"},
	}
	if got := aggregateReadiness(runs); got != "warn" {
		t.Fatalf("aggregate = %q, want warn (drift warn must surface, not be dropped)", got)
	}
}

func TestAggregateReadiness_AllPassIncludingNotApplicable(t *testing.T) {
	t.Parallel()
	runs := []agentsclient.ReadinessRun{
		{Gate: "spec_completeness", State: "pass", CreatedAt: "2024-01-01T00:00:00Z"},
		{Gate: "rollback_plan", State: "not_applicable", CreatedAt: "2024-01-01T00:00:00Z"},
	}
	if got := aggregateReadiness(runs); got != "pass" {
		t.Fatalf("aggregate = %q, want pass", got)
	}
}

func TestAggregateReadiness_EmptyRuns(t *testing.T) {
	t.Parallel()
	if got := aggregateReadiness(nil); got != "not_run" {
		t.Fatalf("aggregate = %q, want not_run", got)
	}
}

func TestAggregateReadiness_LatestRunPerGateWins(t *testing.T) {
	t.Parallel()
	runs := []agentsclient.ReadinessRun{
		{Gate: "spec_completeness", State: "fail", CreatedAt: "2024-01-01T00:00:00Z"},
		// later run for the same gate
		{Gate: "spec_completeness", State: "pass", CreatedAt: "2024-01-02T00:00:00Z"},
	}
	if got := aggregateReadiness(runs); got != "pass" {
		t.Fatalf("aggregate = %q, want pass (latest run wins)", got)
	}
}

func TestAggregateReadiness_PendingOnlyIsNotRun(t *testing.T) {
	t.Parallel()
	runs := []agentsclient.ReadinessRun{
		{Gate: "spec_completeness", State: "pending", CreatedAt: "2024-01-01T00:00:00Z"},
	}
	if got := aggregateReadiness(runs); got != "not_run" {
		t.Fatalf("aggregate = %q, want not_run", got)
	}
}

// --- Service.RunReadiness tests ---

func TestRunReadiness_ReturnsAggregatedResult(t *testing.T) {
	t.Parallel()
	runner := &fakeAgentsRunner{
		verdict: &agentsclient.Verdict{
			ArtifactID:        "art-1",
			EvaluationsPosted: 2,
			ReadinessRuns: []agentsclient.ReadinessRun{
				{Gate: "spec_completeness", State: "pass", CreatedAt: "2024-01-01T00:00:00Z"},
				{Gate: "rollback_plan", State: "warn", CreatedAt: "2024-01-01T00:00:00Z"},
			},
		},
	}
	svc := &Service{AgentsRunner: runner}
	result, err := svc.RunReadiness(context.Background(), "art-1")
	if err != nil {
		t.Fatalf("RunReadiness: %v", err)
	}
	if result.Aggregate != "warn" {
		t.Fatalf("aggregate = %q, want warn", result.Aggregate)
	}
	if result.ArtifactID != "art-1" {
		t.Fatalf("artifact_id = %q, want art-1", result.ArtifactID)
	}
	if result.EvaluationsPosted != 2 {
		t.Fatalf("evaluations_posted = %d, want 2", result.EvaluationsPosted)
	}
}

func TestRunReadinessIncludesPersistedIDEAgentResultsInAggregate(t *testing.T) {
	t.Parallel()
	runner := &fakeAgentsRunner{
		verdict: &agentsclient.Verdict{
			ArtifactID: "art-1",
			ReadinessRuns: []agentsclient.ReadinessRun{
				{Gate: "required_roles_present", State: "pass", CreatedAt: "2026-07-20T13:00:00Z"},
			},
		},
	}
	readiness := &fakeContextPackReadinessReader{runs: map[string][]artifact.ReadinessRun{
		"art-1": {
			{
				Gate:      "scope_clear",
				State:     artifact.ReadinessStateWarn,
				Hint:      "Target user is missing.",
				Executor:  "ide_agent",
				CreatedAt: time.Date(2026, 7, 20, 13, 1, 0, 0, time.UTC),
			},
		},
	}}
	svc := &Service{AgentsRunner: runner, ReadinessRuns: readiness}

	result, err := svc.RunReadiness(context.Background(), "art-1")
	if err != nil {
		t.Fatalf("RunReadiness: %v", err)
	}
	if result.Aggregate != "warn" {
		t.Fatalf("aggregate = %q, want warn from persisted IDE-agent result", result.Aggregate)
	}
	if len(result.ReadinessRuns) != 2 {
		t.Fatalf("readiness runs = %#v, want current platform and persisted IDE-agent results", result.ReadinessRuns)
	}
	for _, run := range result.ReadinessRuns {
		if run.Gate == "scope_clear" && run.Executor != "ide_agent" {
			t.Fatalf("scope_clear executor = %q, want ide_agent trust origin", run.Executor)
		}
	}
}

func TestRunReadinessFailsClosedWhenPersistedResultsAreUnavailable(t *testing.T) {
	t.Parallel()
	svc := &Service{
		AgentsRunner: &fakeAgentsRunner{verdict: &agentsclient.Verdict{
			ArtifactID: "art-1",
			ReadinessRuns: []agentsclient.ReadinessRun{
				{Gate: "required_roles_present", State: "pass"},
			},
		}},
		ReadinessRuns: &fakeContextPackReadinessReader{err: errors.New("read failed")},
	}

	_, err := svc.RunReadiness(context.Background(), "art-1")
	if err == nil || !strings.Contains(err.Error(), "stored readiness") {
		t.Fatalf("RunReadiness error = %v, want stored-readiness failure", err)
	}
}

func TestMergeLatestReadinessRunsKeepsCurrentMetadataOnTimestampTie(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 7, 20, 13, 0, 0, 0, time.UTC)
	runs := mergeLatestReadinessRuns(
		[]agentsclient.ReadinessRun{{
			Gate: "scope_clear", State: "pass", Executor: "platform",
			JudgeModel: "governance-gate-judge", CreatedAt: at.Format(time.RFC3339Nano),
		}},
		[]artifact.ReadinessRun{{
			Gate: "scope_clear", State: artifact.ReadinessStatePass,
			Executor: "platform", CreatedAt: at,
		}},
	)
	if len(runs) != 1 || runs[0].JudgeModel != "governance-gate-judge" {
		t.Fatalf("runs = %#v, want richer current metadata preserved on timestamp tie", runs)
	}
}

func TestMergeLatestReadinessRunsComparesParsedTimestamps(t *testing.T) {
	t.Parallel()
	runs := mergeLatestReadinessRuns(
		[]agentsclient.ReadinessRun{{
			Gate: "scope_clear", State: "fail", CreatedAt: "2026-07-20T13:00:00Z",
		}},
		[]artifact.ReadinessRun{{
			Gate: "scope_clear", State: artifact.ReadinessStatePass,
			Executor:  "platform",
			CreatedAt: time.Date(2026, 7, 20, 13, 0, 0, 100_000_000, time.UTC),
		}},
	)
	if len(runs) != 1 || runs[0].State != "pass" {
		t.Fatalf("runs = %#v, want fractionally newer stored result", runs)
	}
}

func TestMergeLatestReadinessRunsKeepsHumanDecisionsSeparate(t *testing.T) {
	t.Parallel()
	runs := mergeLatestReadinessRuns(
		[]agentsclient.ReadinessRun{{
			Gate: "scope_clear", State: "pass", Executor: "platform", CreatedAt: "2026-07-20T13:00:00Z",
		}},
		[]artifact.ReadinessRun{{
			Gate: "scope_clear", State: artifact.ReadinessStateFail,
			Executor: "human", CreatedAt: time.Date(2026, 7, 20, 13, 1, 0, 0, time.UTC),
		}},
	)
	if len(runs) != 1 || runs[0].State != "pass" {
		t.Fatalf("runs = %#v, want human decision excluded from readiness evidence", runs)
	}
}

func TestRunReadiness_PendingDispatchForcesNotRunWithoutMaskingBlockers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, state, want string }{
		{name: "pass", state: "pass", want: "not_run"},
		{name: "warn", state: "warn", want: "not_run"},
		{name: "fail", state: "fail", want: "fail"},
		{name: "needs_human_review", state: "needs_human_review", want: "needs_human_review"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &Service{AgentsRunner: &fakeAgentsRunner{verdict: &agentsclient.Verdict{
				ArtifactID: "art-1", ReadinessRuns: []agentsclient.ReadinessRun{{Gate: "scope", State: tc.state}},
				DispatchedToIDEAgent: &agentsclient.GateTaskDispatchReceipt{PendingTaskIDs: []string{"task-1"}},
			}}}
			result, err := svc.RunReadiness(context.Background(), "art-1")
			if err != nil || result.Aggregate != tc.want {
				t.Fatalf("aggregate/err = %q/%v, want %q/nil", result.Aggregate, err, tc.want)
			}
			if result.DispatchedToIDEAgent == nil || result.DispatchedToIDEAgent.PendingTaskIDs[0] != "task-1" {
				t.Fatalf("pending receipt not preserved: %+v", result.DispatchedToIDEAgent)
			}
		})
	}
}

func TestRunReadiness_NilAgentsRunnerError(t *testing.T) {
	t.Parallel()
	svc := &Service{}
	_, err := svc.RunReadiness(context.Background(), "art-1")
	if err == nil {
		t.Fatal("expected error for nil AgentsRunner")
	}
}

func TestRunReadiness_EmptyArtifactIDError(t *testing.T) {
	t.Parallel()
	svc := &Service{AgentsRunner: &fakeAgentsRunner{}}
	_, err := svc.RunReadiness(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty artifact_id")
	}
}

// --- Service.RunLLMGates tests ---

func TestRunLLMGates_ReturnsRawResult(t *testing.T) {
	t.Parallel()
	runner := &fakeAgentsRunner{gatesOut: map[string]any{"status": "ok"}}
	svc := &Service{AgentsRunner: runner}
	result, err := svc.RunLLMGates(context.Background(), "cr-1")
	if err != nil {
		t.Fatalf("RunLLMGates: %v", err)
	}
	if result["status"] != "ok" {
		t.Fatalf("result[status] = %v, want ok", result["status"])
	}
}

func TestRunLLMGates_NilRunnerError(t *testing.T) {
	t.Parallel()
	svc := &Service{}
	_, err := svc.RunLLMGates(context.Background(), "cr-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- Service.ReviewDelivery tests ---

func TestReviewDelivery_ReturnsRawResult(t *testing.T) {
	t.Parallel()
	runner := &fakeAgentsRunner{delivOut: map[string]any{"verdict": "pass"}}
	svc := &Service{AgentsRunner: runner}
	result, err := svc.ReviewDelivery(context.Background(), "cr-1")
	if err != nil {
		t.Fatalf("ReviewDelivery: %v", err)
	}
	if result["verdict"] != "pass" {
		t.Fatalf("result[verdict] = %v, want pass", result["verdict"])
	}
}

func TestReviewDelivery_NilRunnerError(t *testing.T) {
	t.Parallel()
	svc := &Service{}
	_, err := svc.ReviewDelivery(context.Background(), "cr-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- Service.CreateQuickWorkItem tests ---

func TestCreateQuickWorkItem_ReturnsRawResult(t *testing.T) {
	t.Parallel()
	runner := &fakeAgentsRunner{qwOut: map[string]any{"change_request_id": "cr-99"}}
	svc := &Service{AgentsRunner: runner}
	result, err := svc.CreateQuickWorkItem(context.Background(), CreateQuickWorkItemInput{
		Title:       "Fix the login bug",
		Description: "Users can't log in",
	})
	if err != nil {
		t.Fatalf("CreateQuickWorkItem: %v", err)
	}
	if result["change_request_id"] != "cr-99" {
		t.Fatalf("result[change_request_id] = %v, want cr-99", result["change_request_id"])
	}
}

func TestCreateQuickWorkItem_ForwardsAcceptanceCriteria(t *testing.T) {
	t.Parallel()
	runner := &fakeAgentsRunner{qwOut: map[string]any{"change_request_id": "cr-99"}}
	svc := &Service{AgentsRunner: runner}
	_, err := svc.CreateQuickWorkItem(context.Background(), CreateQuickWorkItemInput{
		Title:              "Fix the login bug",
		Description:        "Users can't log in",
		AcceptanceCriteria: []AcceptanceCriterionInput{{Text: " Users can sign in with valid credentials. "}, {}, {Text: " Invalid credentials show an error. "}},
	})
	if err != nil {
		t.Fatalf("CreateQuickWorkItem: %v", err)
	}
	texts := make([]string, 0, len(runner.qwACs))
	for _, ac := range runner.qwACs {
		texts = append(texts, ac.Text)
	}
	if got, want := strings.Join(texts, "|"), "Users can sign in with valid credentials.|Invalid credentials show an error."; got != want {
		t.Fatalf("acceptance criteria = %q, want %q", got, want)
	}
}

func TestCreateQuickWorkItem_ForwardsAcceptanceCriterionBindings(t *testing.T) {
	t.Parallel()
	runner := &fakeAgentsRunner{qwOut: map[string]any{"change_request_id": "cr-99"}}
	svc := &Service{AgentsRunner: runner}
	_, err := svc.CreateQuickWorkItem(context.Background(), CreateQuickWorkItemInput{
		Title:       "Fix the login bug",
		Description: "Users can't log in",
		AcceptanceCriteria: []AcceptanceCriterionInput{
			{Text: " Users can sign in with valid credentials. ", VerificationBinding: " integration "},
		},
	})
	if err != nil {
		t.Fatalf("CreateQuickWorkItem: %v", err)
	}
	if len(runner.qwACs) != 1 {
		t.Fatalf("acceptance criteria = %+v", runner.qwACs)
	}
	if got, want := runner.qwACs[0].Text, "Users can sign in with valid credentials."; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if got, want := runner.qwACs[0].VerificationBinding, "integration"; got != want {
		t.Fatalf("verification_binding = %q, want %q", got, want)
	}
}

func TestCreateQuickWorkItem_ForwardsIdentityAttribution(t *testing.T) {
	t.Parallel()
	runner := &fakeAgentsRunner{qwOut: map[string]any{"change_request_id": "cr-99"}}
	svc := &Service{AgentsRunner: runner}
	_, err := svc.CreateQuickWorkItem(context.Background(), CreateQuickWorkItemInput{
		Title:       "Fix the login bug",
		Description: "Users can't log in",
		CreatedBy:   " thanhtung2693 ",
		WorkspaceID: " 5367ce6c-53cd-4891-a56a-229bb25d3f41 ",
	})
	if err != nil {
		t.Fatalf("CreateQuickWorkItem: %v", err)
	}
	if runner.qwActor != "thanhtung2693" {
		t.Fatalf("createdBy = %q, want thanhtung2693", runner.qwActor)
	}
	if runner.qwWS != "5367ce6c-53cd-4891-a56a-229bb25d3f41" {
		t.Fatalf("workspaceID = %q", runner.qwWS)
	}
}

func TestCreateQuickWorkItem_MapsAgentsValidationError(t *testing.T) {
	t.Parallel()
	runner := &fakeAgentsRunner{
		qwErr: &agentsclient.ResponseError{
			StatusCode: 400,
			Body:       `{"detail":"acceptance criteria are required"}`,
		},
	}
	svc := &Service{AgentsRunner: runner}
	_, err := svc.CreateQuickWorkItem(context.Background(), CreateQuickWorkItemInput{
		Title:       "Fix the login bug",
		Description: "Users can't log in",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "acceptance criteria are required") {
		t.Fatalf("error = %q, want agents detail", err.Error())
	}
	if !strings.Contains(err.Error(), ErrValidation.Error()) {
		t.Fatalf("error = %q, want ErrValidation wrapper", err.Error())
	}
}

func TestCreateQuickWorkItem_NilRunnerError(t *testing.T) {
	t.Parallel()
	svc := &Service{}
	_, err := svc.CreateQuickWorkItem(context.Background(), CreateQuickWorkItemInput{Title: "x", Description: "users cannot log in"})
	if err == nil {
		t.Fatal("expected error for nil AgentsRunner")
	}
	if !strings.Contains(err.Error(), "agents service not configured") {
		t.Fatalf("error = %q, want 'agents service not configured'", err.Error())
	}
}

func TestCreateQuickWorkItem_EmptyTitleError(t *testing.T) {
	t.Parallel()
	svc := &Service{AgentsRunner: &fakeAgentsRunner{}}
	_, err := svc.CreateQuickWorkItem(context.Background(), CreateQuickWorkItemInput{Description: "y"})
	if err == nil {
		t.Fatal("expected error for empty title")
	}
}

// --- GovernanceLevel in RunReadiness (Task 8) ---

func TestRunReadiness_GovernanceLevelFromPolicyV1Snapshot(t *testing.T) {
	t.Parallel()
	snap := `{"snapshot_schema_version":"specgate.policy/v1","governance_level":"enhanced","enabled_gates":[],"required_roles":[],"required_topics":[],"approval_policy":"human_required","evidence_policy":"attested_ok"}`
	runner := &fakeAgentsRunner{
		verdict: &agentsclient.Verdict{
			ArtifactID: "art-snap",
			ReadinessRuns: []agentsclient.ReadinessRun{
				{Gate: "spec_completeness", State: "pass", CreatedAt: "2024-01-01T00:00:00Z"},
			},
		},
	}
	artReader := &fakeContextPackArtifactReader{
		art: &artifact.Artifact{
			ID:                 "art-snap",
			PolicySnapshotJSON: snap,
		},
	}
	svc := &Service{AgentsRunner: runner, Artifacts: artReader}
	result, err := svc.RunReadiness(context.Background(), "art-snap")
	if err != nil {
		t.Fatalf("RunReadiness: %v", err)
	}
	if result.GovernanceLevel != "enhanced" {
		t.Fatalf("GovernanceLevel = %q, want enhanced", result.GovernanceLevel)
	}
}

func TestRunReadiness_GovernanceLevelEmptyForUnversionedSnapshot(t *testing.T) {
	t.Parallel()
	snap := `{"approval_policy":"human_required","enabled_gates":["spec_completeness"]}`
	runner := &fakeAgentsRunner{
		verdict: &agentsclient.Verdict{
			ArtifactID: "art-leg",
			ReadinessRuns: []agentsclient.ReadinessRun{
				{Gate: "spec_completeness", State: "pass", CreatedAt: "2024-01-01T00:00:00Z"},
			},
		},
	}
	artReader := &fakeContextPackArtifactReader{
		art: &artifact.Artifact{
			ID:                 "art-leg",
			PolicySnapshotJSON: snap,
		},
	}
	svc := &Service{AgentsRunner: runner, Artifacts: artReader}
	result, err := svc.RunReadiness(context.Background(), "art-leg")
	if err != nil {
		t.Fatalf("RunReadiness: %v", err)
	}
	if result.GovernanceLevel != "" {
		t.Fatalf("GovernanceLevel = %q, want empty for snapshot without governance level", result.GovernanceLevel)
	}
}

func TestRunReadiness_GovernanceLevelEmptyWhenArtifactsNil(t *testing.T) {
	t.Parallel()
	runner := &fakeAgentsRunner{
		verdict: &agentsclient.Verdict{
			ArtifactID: "art-noread",
			ReadinessRuns: []agentsclient.ReadinessRun{
				{Gate: "spec_completeness", State: "pass", CreatedAt: "2024-01-01T00:00:00Z"},
			},
		},
	}
	// Artifacts is nil — governance level is absent but readiness succeeds.
	svc := &Service{AgentsRunner: runner}
	result, err := svc.RunReadiness(context.Background(), "art-noread")
	if err != nil {
		t.Fatalf("RunReadiness: %v", err)
	}
	if result.GovernanceLevel != "" {
		t.Fatalf("GovernanceLevel = %q, want empty when Artifacts is nil", result.GovernanceLevel)
	}
}
