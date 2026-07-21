package policy

import (
	"encoding/json"
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

// Trust is the trust level stamped on a GateResult.
type Trust string

const (
	TrustAgentAttested     Trust = "agent_attested"
	TrustPlatformEvaluated Trust = "platform_evaluated"
	TrustHumanDecision     Trust = "human_decision"
)

// DB records

type GateTaskRecord struct {
	ID             string
	WorkspaceID    string
	ArtifactID     string
	GateKey        string
	GateVersion    string
	GateDigest     string
	ArtifactDigest string
	PolicyDigest   string
	Executor       Executor
	SkillContent   string
	ExpiresAt      time.Time
	CreatedAt      time.Time
}

type GateResultRecord struct {
	ID            string
	TaskID        string
	GateDigest    string // from submitted result — validated against task
	InputDigest   string // submitted artifact/input digest; must match the frozen task
	Executor      string // from submitted result evaluator
	State         string // "pass"|"warn"|"fail"|"needs_human_review"|"not_applicable"|"not_run"
	Summary       string
	Trust         Trust
	EvaluatorJSON json.RawMessage
	EvidenceJSON  json.RawMessage
	FindingsJSON  json.RawMessage
	SubmittedAt   time.Time
}
