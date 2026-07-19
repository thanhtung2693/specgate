package governanceprofile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidDefinition is returned when an automatic policy definition
// references a value outside the closed vocabulary.
var ErrInvalidDefinition = errors.New("invalid automatic policy definition")

// Closed first-slice vocabularies. Topic + gate keys mirror the agents readiness
// engine (completeness COMPLETENESS_TOPICS keys and ALL_LLM_GATES); roles mirror
// the artifact document roles. A required set may only reference these — imports
// and built-ins are validated against them so a snapshot can never freeze a key
// the readiness engine cannot act on.
var (
	KnownTopics = map[string]bool{
		"outcomes": true, "scope": true, "non_goals": true, "acceptance_criteria": true,
		"constraints": true, "rollout_rollback": true, "verification": true,
		"users_roles": true, "workflows": true, "screens_states": true, "data_model": true,
		"permissions_security": true, "integrations": true, "edge_cases": true,
		"metrics": true, "observability": true, "phased_tasks": true,
	}
	KnownGates = map[string]bool{
		"rollback_plan_present": true, "acceptance_criteria_edge_cases": true,
		"acceptance_criteria_verifiable": true, "success_metric_measurable": true,
		"scope_clear": true, "implementation_plan_traceable": true, "spec_completeness": true,
		// spec_repo_drift is ide_agent-executed and warn-only: repo docs contradicting
		// the approved spec are only knowable after checkout, so there is no platform-model judge.
		"spec_repo_drift": true,
	}
	KnownRoles    = map[string]bool{"spec": true, "design": true, "plan": true, "verification": true, "research": true, "reference": true}
	KnownEvidence = map[string]bool{"tests": true, "rollout_defined": true}

	// KnownApprovalPolicies is the closed vocabulary for the automatic policy's
	// approval bar. Only a human actor may approve.
	KnownApprovalPolicies = map[string]bool{
		"human_required": true,
	}

	// KnownEvidencePolicies is the closed vocabulary for the automatic policy's evidence bar.
	// attested_ok:           single attester suffices (default).
	// corroborated_required: a delivery pass requires corroborating evidence
	//   (a PR/MR merged webhook bound to the latest completion head, or all criteria resolved by canonical
	//   deterministic bindings); without it the pass is clamped to
	//   needs_human_review. Enforced by the delivery-review resolver
	//   (_resolve_overall in agents quality_gates/delivery_review.py).
	KnownEvidencePolicies = map[string]bool{
		"attested_ok":           true,
		"corroborated_required": true,
	}
)

// DeliveryReviewGateKey is a valid gate_skills target even though it is not in
// KnownGates / enabled_gates — the post-build delivery review runs regardless of
// the readiness gate set, so automatic policy may bind a rubric Skill to it.
const DeliveryReviewGateKey = "delivery_review"

type Source string

const (
	SourceBuiltin Source = "builtin"
)

type Definition struct {
	DisplayName      string   `json:"display_name"`
	ChangeType       string   `json:"change_type"`
	RequiredRoles    []string `json:"required_roles"`
	RequiredTopics   []string `json:"required_topics"`
	RequiredEvidence []string `json:"required_evidence"`
	EnabledGates     []string `json:"enabled_gates"`
	// ApprovalPolicy controls who may approve an artifact under this policy.
	ApprovalPolicy string `json:"approval_policy"`
	// EvidencePolicy controls the delivery evidence bar (see KnownEvidencePolicies):
	// evidence policies may clamp a would-be pass to needs_human_review until
	// the required corroboration or deterministic bindings are present. Enforced
	// by the delivery-review resolver.
	EvidencePolicy string `json:"evidence_policy"`
	// GateSkills binds a rubric Skill (by name) to a gate key: the readiness/delivery
	// judge injects that Skill's prompt as a "Team Policy" rubric. Keys are gate keys
	// (or delivery_review); values are skill names. omitempty + encoding/json's sorted
	// map-key marshaling keep the digest stable and deterministic.
	GateSkills map[string]string `json:"gate_skills,omitempty"`
}

// GateDefinition is the immutable execution contract for one policy gate.
// SkillName remains navigation metadata; SkillContent and SkillDigest are the
// runtime authority frozen when the artifact is published.
type GateDefinition struct {
	Key          string `json:"key"`
	Version      string `json:"version"`
	SkillName    string `json:"skill_name,omitempty"`
	SkillContent string `json:"skill_content,omitempty"`
	SkillDigest  string `json:"skill_digest,omitempty"`
}

type ResolveInput struct {
	RequestType              string
	ImpactLevel              string
	RequestedGovernanceLevel GovernanceLevel
	ImpactDeclaration        ImpactDeclaration
}

type ResolvedProfile struct {
	Namespace                string               `json:"namespace"`
	Key                      string               `json:"key"`
	FullKey                  string               `json:"full_key"`
	Version                  string               `json:"version"`
	DisplayName              string               `json:"display_name"`
	ChangeType               string               `json:"change_type"`
	SnapshotSchemaVersion    string               `json:"snapshot_schema_version,omitempty"`
	WorkType                 string               `json:"work_type,omitempty"`
	RiskLevel                string               `json:"risk_level,omitempty"`
	RequestedGovernanceLevel GovernanceLevel      `json:"requested_governance_level,omitempty"`
	GovernanceLevel          GovernanceLevel      `json:"governance_level,omitempty"`
	ReasonCodes              []string             `json:"reason_codes,omitempty"`
	RequiredRoles            []string             `json:"required_roles"`
	RequiredTopics           []string             `json:"required_topics"`
	RequiredEvidence         []string             `json:"required_evidence"`
	EnabledGates             []string             `json:"enabled_gates"`
	Digest                   string               `json:"digest"`
	Source                   Source               `json:"source"`
	SourceRepo               string               `json:"source_repo,omitempty"`
	SourcePath               string               `json:"source_path,omitempty"`
	ApprovalPolicy           string               `json:"approval_policy"`
	EvidencePolicy           string               `json:"evidence_policy"`
	GateSkills               map[string]string    `json:"gate_skills,omitempty"`
	GateDefinitions          []GateDefinition     `json:"gate_definitions,omitempty"`
	PolicyLineage            []PolicyLineageEntry `json:"policy_lineage,omitempty"`
	AppliedExceptions        []string             `json:"applied_exceptions,omitempty"`
}

func NormalizeDefinition(def Definition) Definition {
	def.DisplayName = strings.TrimSpace(def.DisplayName)
	def.ChangeType = strings.TrimSpace(def.ChangeType)
	def.RequiredRoles = dedupeSorted(def.RequiredRoles)
	def.RequiredTopics = dedupeSorted(def.RequiredTopics)
	def.RequiredEvidence = dedupeSorted(def.RequiredEvidence)
	def.EnabledGates = dedupeSorted(def.EnabledGates)
	def.ApprovalPolicy = strings.TrimSpace(def.ApprovalPolicy)
	def.EvidencePolicy = strings.TrimSpace(def.EvidencePolicy)
	def.GateSkills = normalizeGateSkills(def.GateSkills)
	return def
}

// normalizeGateSkills trims keys + values and drops empty entries. Returns nil for
// an empty result so the field omitempty-omits (keeps digests stable).
func normalizeGateSkills(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for gate, skill := range in {
		gate = strings.TrimSpace(gate)
		skill = strings.TrimSpace(skill)
		if gate == "" || skill == "" {
			continue
		}
		out[gate] = skill
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func DefinitionDigest(def Definition) (string, string, error) {
	normalized := NormalizeDefinition(def)
	b, err := json.Marshal(normalized)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), string(b), nil
}

func (r ResolvedProfile) SnapshotJSON() (string, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// FreezeGateDefinitions resolves mutable Skill names to immutable rubric content
// and refreshes the policy digest so the stored digest covers those exact bytes.
func (r *ResolvedProfile) FreezeGateDefinitions(skillPrompts map[string]string) error {
	keys := append([]string(nil), r.EnabledGates...)
	if _, ok := r.GateSkills[DeliveryReviewGateKey]; ok {
		keys = append(keys, DeliveryReviewGateKey)
	}
	r.GateDefinitions = make([]GateDefinition, 0, len(keys))
	for _, key := range keys {
		skillName := strings.TrimSpace(r.GateSkills[key])
		skillContent := skillPrompts[skillName]
		if skillName != "" && strings.TrimSpace(skillContent) == "" {
			return fmt.Errorf("%w: gate %q requires unavailable Skill %q", ErrInvalidDefinition, key, skillName)
		}
		definition := GateDefinition{
			Key:          key,
			Version:      "v1",
			SkillName:    skillName,
			SkillContent: skillContent,
		}
		if skillContent != "" {
			sum := sha256.Sum256([]byte(skillContent))
			definition.SkillDigest = "sha256:" + hex.EncodeToString(sum[:])
		}
		r.GateDefinitions = append(r.GateDefinitions, definition)
	}

	unsigned := *r
	unsigned.Digest = ""
	body, err := json.Marshal(unsigned)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	r.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return nil
}

func dedupeSorted(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
