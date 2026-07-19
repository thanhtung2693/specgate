package policy_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/policy"
)

func TestValidateAndStampResultRejectsWrongInputDigest(t *testing.T) {
	t.Parallel()
	task := policy.GateTaskRecord{
		ID:             "task-1",
		GateDigest:     "sha256:gate",
		ArtifactDigest: "sha256:artifact",
		Executor:       policy.ExecutorIDEAgent,
		ExpiresAt:      time.Now().Add(time.Hour),
	}
	_, err := policy.ValidateAndStampResult(&task, policy.GateResultRecord{
		GateDigest:  task.GateDigest,
		InputDigest: "sha256:wrong",
		Executor:    string(policy.ExecutorIDEAgent),
		State:       "pass",
	})
	if !errors.Is(err, policy.ErrInputDigestMismatch) {
		t.Fatalf("err = %v, want input digest mismatch", err)
	}
}

func TestValidateAndStampResultRejectsExpiredTask(t *testing.T) {
	t.Parallel()
	task := policy.GateTaskRecord{
		ID:             "task-1",
		GateDigest:     "sha256:gate",
		ArtifactDigest: "sha256:artifact",
		Executor:       policy.ExecutorIDEAgent,
		ExpiresAt:      time.Now().Add(-time.Second),
	}
	_, err := policy.ValidateAndStampResult(&task, policy.GateResultRecord{
		GateDigest:  task.GateDigest,
		InputDigest: task.ArtifactDigest,
		Executor:    string(policy.ExecutorIDEAgent),
		State:       "pass",
	})
	if !errors.Is(err, policy.ErrGateTaskExpired) {
		t.Fatalf("err = %v, want gate task expired", err)
	}
}

func TestValidateAndStampResultStampsTrustAndJSONDefaults(t *testing.T) {
	t.Parallel()
	task := policy.GateTaskRecord{
		ID:             "task-1",
		GateDigest:     "sha256:gate",
		ArtifactDigest: "sha256:artifact",
		Executor:       policy.ExecutorIDEAgent,
		ExpiresAt:      time.Now().Add(time.Hour),
	}
	result, err := policy.ValidateAndStampResult(&task, policy.GateResultRecord{
		GateDigest:  task.GateDigest,
		InputDigest: task.ArtifactDigest,
		Executor:    string(task.Executor),
		State:       "pass",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Trust != policy.TrustAgentAttested || result.TaskID != task.ID {
		t.Fatalf("result = %+v, want agent-attested result for task %q", result, task.ID)
	}
	if !bytes.Equal(result.EvaluatorJSON, []byte(`{}`)) || !bytes.Equal(result.FindingsJSON, []byte(`[]`)) {
		t.Fatalf("JSON defaults = %s, %s; want {}, []", result.EvaluatorJSON, result.FindingsJSON)
	}
}
