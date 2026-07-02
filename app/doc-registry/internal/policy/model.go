package policy

import (
	"encoding/json"
	"fmt"
	"time"
)

// Executor is the type of agent that evaluates a gate.
type Executor string

const (
	ExecutorDeterministic Executor = "deterministic"
	ExecutorIDEAgent      Executor = "ide_agent"
	ExecutorPlatformLLM   Executor = "platform_llm"
	ExecutorHuman         Executor = "human"
	ExecutorExternal      Executor = "external"
)

// Requirement controls whether a gate is mandatory.
type Requirement string

const (
	RequirementAlways         Requirement = "always"
	RequirementWhenApplicable Requirement = "when_applicable"
	RequirementAdvisory       Requirement = "advisory"
)

// Trust is the trust level stamped on a GateResult.
type Trust string

const (
	TrustAgentAttested     Trust = "agent_attested"
	TrustPlatformEvaluated Trust = "platform_evaluated"
	TrustHumanDecision     Trust = "human_decision"
)

// GateRef is a versioned gate reference within a policy.
type GateRef struct {
	Namespace   string
	Name        string
	Version     string
	Digest      string
	Requirement Requirement
	Parameters  json.RawMessage
}

// Key returns "namespace/name@version".
func (r GateRef) Key() string {
	return fmt.Sprintf("%s/%s@%s", r.Namespace, r.Name, r.Version)
}

// DB records

type GateTaskRecord struct {
	ID             string
	ArtifactID     string
	GateKey        string
	GateVersion    string
	GateDigest     string
	ArtifactDigest string
	ProfileDigest  string
	Executor       Executor
	SkillContent   string
	ExpiresAt      time.Time
	CreatedAt      time.Time
}

type GateResultRecord struct {
	ID            string
	TaskID        string
	GateDigest    string // from submitted result — validated against task
	Executor      string // from submitted result evaluator
	State         string // "pass"|"warn"|"fail"|"needs_human_review"|"not_applicable"|"not_run"
	Trust         Trust
	EvaluatorJSON json.RawMessage
	FindingsJSON  json.RawMessage
	SubmittedAt   time.Time
}
