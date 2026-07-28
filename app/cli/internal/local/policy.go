package local

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	localPolicySchemaVersion = "specgate.local_policy/v1"
	localPolicyVersion       = "local-standard"
	localApprovalPolicy      = "human_required"
	localEvidencePolicy      = "attested_ok"
)

type localGateDefinition struct {
	Key          string `json:"key"`
	Version      string `json:"version"`
	SkillContent string `json:"skill_content"`
}

type localPolicyDocument struct {
	SchemaVersion    string                `json:"snapshot_schema_version"`
	PolicyVersion    string                `json:"policy_version"`
	GovernanceLevel  string                `json:"governance_level,omitempty"`
	ReasonCodes      []string              `json:"reason_codes,omitempty"`
	RequiredRoles    []string              `json:"required_roles,omitempty"`
	RequiredTopics   []string              `json:"required_topics,omitempty"`
	RequiredEvidence []string              `json:"required_evidence,omitempty"`
	EnabledGates     []string              `json:"enabled_gates"`
	GateDefinitions  []localGateDefinition `json:"gate_definitions"`
	GateSkills       map[string]string     `json:"gate_skills,omitempty"`
	Approval         string                `json:"approval_policy"`
	Evidence         string                `json:"evidence_policy"`
}

type PolicyRubricProjection struct {
	Gate   string `json:"gate"`
	Skill  string `json:"skill,omitempty"`
	Digest string `json:"digest"`
	Source string `json:"source"`
}

type PolicyProjection struct {
	GovernanceLevel  string                   `json:"governance_level"`
	ReasonCodes      []string                 `json:"reason_codes"`
	RequiredRoles    []string                 `json:"required_roles"`
	RequiredTopics   []string                 `json:"required_topics"`
	RequiredEvidence []string                 `json:"required_evidence"`
	EnabledGates     []string                 `json:"enabled_gates"`
	ApprovalPolicy   string                   `json:"approval_policy"`
	EvidencePolicy   string                   `json:"evidence_policy"`
	PolicyDigest     string                   `json:"policy_digest"`
	Rubrics          []PolicyRubricProjection `json:"rubrics"`
}

var localSemanticGates = []localGateDefinition{
	{Key: "acceptance_criteria_verifiable", Version: "v1", SkillContent: "Evaluate existing acceptance criteria without expanding scope. Pass when each material outcome has an observable result and verification path; warn for one bounded supporting gap; fail when criteria are vague, conflict with approved scope, or cannot map to evidence; use needs_human_review when scope is ambiguous."},
	{Key: "scope_clear", Version: "v1", SkillContent: "Evaluate product intent without designing implementation. Pass when goal, scope, and boundaries are explicit and consistent; warn for one bounded scope or guardrail gap; fail when materially different implementations remain possible; use needs_human_review when intent conflicts."},
	{Key: "spec_completeness", Version: "v1", SkillContent: "Evaluate each required readiness topic without rewriting the specification or adding scope. Pass when the mapped documents provide enough concrete, consistent evidence to build and verify; warn for a bounded contract gap; fail when a required topic is missing; cite the minimum missing evidence."},
	{Key: "spec_repo_drift", Version: "v1", SkillContent: "Compare the approved artifact with the repository's governed docs named by the artifact and module doc-layering rules. Report semantic contradictions only. The approved artifact wins; do not rewrite out-of-scope docs. Submit examined_docs and repo_commit. Zero findings maps to pass; one or more findings maps to warn; this gate never fails or approves delivery."},
}

func localSkillNameForGate(gate string) string {
	switch gate {
	case "spec_completeness":
		return "spec-review"
	case "scope_clear":
		return "prd-review"
	case "acceptance_criteria_verifiable":
		return "acceptance-criteria"
	default:
		return ""
	}
}

func localPolicySnapshot() (string, string, error) {
	policy := localPolicyDocument{
		SchemaVersion:    localPolicySchemaVersion,
		PolicyVersion:    localPolicyVersion,
		GovernanceLevel:  "standard",
		ReasonCodes:      []string{"local_fixed_standard"},
		RequiredRoles:    []string{"plan", "spec"},
		RequiredTopics:   []string{"acceptance_criteria", "outcomes", "scope", "verification"},
		RequiredEvidence: []string{"tests"},
		GateDefinitions:  append([]localGateDefinition(nil), localSemanticGates...),
		Approval:         localApprovalPolicy,
		Evidence:         localEvidencePolicy,
	}
	for _, gate := range policy.GateDefinitions {
		policy.EnabledGates = append(policy.EnabledGates, gate.Key)
		if skill := strings.TrimSpace(localSkillNameForGate(gate.Key)); skill != "" {
			if policy.GateSkills == nil {
				policy.GateSkills = map[string]string{}
			}
			policy.GateSkills[gate.Key] = skill
		}
	}
	sort.Strings(policy.RequiredRoles)
	sort.Strings(policy.RequiredTopics)
	sort.Strings(policy.RequiredEvidence)
	sort.Strings(policy.EnabledGates)
	sort.Slice(policy.GateDefinitions, func(i, j int) bool {
		return policy.GateDefinitions[i].Key < policy.GateDefinitions[j].Key
	})
	body, err := json.Marshal(policy)
	if err != nil {
		return "", "", err
	}
	text := string(body)
	return text, digestText(text), nil
}

// PreviewPolicy returns the exact fixed policy Local mode freezes on publish.
func PreviewPolicy() (PolicyProjection, error) {
	snapshot, digest, err := localPolicySnapshot()
	if err != nil {
		return PolicyProjection{}, err
	}
	var policy localPolicyDocument
	if err := json.Unmarshal([]byte(snapshot), &policy); err != nil {
		return PolicyProjection{}, err
	}
	rubrics := make([]PolicyRubricProjection, 0, len(policy.GateDefinitions))
	for _, gate := range policy.GateDefinitions {
		rubrics = append(rubrics, PolicyRubricProjection{
			Gate: gate.Key, Skill: policy.GateSkills[gate.Key],
			Digest: digestText(gate.SkillContent), Source: "embedded_default",
		})
	}
	return PolicyProjection{
		GovernanceLevel: policy.GovernanceLevel, ReasonCodes: policy.ReasonCodes,
		RequiredRoles: policy.RequiredRoles, RequiredTopics: policy.RequiredTopics,
		RequiredEvidence: policy.RequiredEvidence, EnabledGates: policy.EnabledGates,
		ApprovalPolicy: policy.Approval, EvidencePolicy: policy.Evidence,
		PolicyDigest: digest, Rubrics: rubrics,
	}, nil
}

func parseLocalPolicy(artifact Artifact) (localPolicyDocument, error) {
	policySnapshot := strings.TrimSpace(artifact.PolicySnapshot)
	if policySnapshot == "" {
		return localPolicyDocument{}, fmt.Errorf("artifact %s is missing frozen policy snapshot", artifact.ID)
	}
	if digestText(policySnapshot) != strings.TrimSpace(artifact.PolicyDigest) {
		return localPolicyDocument{}, fmt.Errorf("artifact %s frozen policy digest does not match its snapshot", artifact.ID)
	}
	var policy localPolicyDocument
	if err := json.Unmarshal([]byte(policySnapshot), &policy); err != nil {
		return localPolicyDocument{}, fmt.Errorf("artifact %s has invalid frozen policy snapshot: %w", artifact.ID, err)
	}
	if policy.SchemaVersion != localPolicySchemaVersion ||
		policy.PolicyVersion != localPolicyVersion ||
		policy.Approval != localApprovalPolicy ||
		policy.Evidence != localEvidencePolicy ||
		len(policy.RequiredRoles) == 0 ||
		len(policy.EnabledGates) != len(policy.GateDefinitions) {
		return localPolicyDocument{}, fmt.Errorf("artifact %s has unsupported frozen policy snapshot", artifact.ID)
	}
	enabled := make(map[string]bool, len(policy.EnabledGates))
	for _, key := range policy.EnabledGates {
		key = strings.TrimSpace(key)
		if key == "" || enabled[key] {
			return localPolicyDocument{}, fmt.Errorf("artifact %s has invalid frozen policy gates", artifact.ID)
		}
		enabled[key] = true
	}
	for _, definition := range policy.GateDefinitions {
		if !enabled[definition.Key] || strings.TrimSpace(definition.Version) == "" || strings.TrimSpace(definition.SkillContent) == "" {
			return localPolicyDocument{}, fmt.Errorf("artifact %s has invalid frozen policy gate definition", artifact.ID)
		}
		delete(enabled, definition.Key)
	}
	if len(enabled) != 0 {
		return localPolicyDocument{}, fmt.Errorf("artifact %s frozen policy gates do not match their definitions", artifact.ID)
	}
	return policy, nil
}

func frozenLocalGateDefinitions(artifact Artifact) ([]localGateDefinition, error) {
	policy, err := parseLocalPolicy(artifact)
	if err != nil {
		return nil, err
	}
	return policy.GateDefinitions, nil
}
