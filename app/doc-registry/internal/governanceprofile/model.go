package governanceprofile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ErrInvalidDefinition is returned when a profile definition references a value
// outside the closed first-slice vocabulary.
var ErrInvalidDefinition = errors.New("invalid profile definition")

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
	}
	KnownRoles     = map[string]bool{"spec": true, "design": true, "plan": true, "verification": true, "research": true, "reference": true}
	KnownEvidence  = map[string]bool{"tests": true, "rollout_defined": true}
	KnownRenderers = map[string]bool{"default_context_pack": true}

	// KnownApprovalPolicies is the closed vocabulary for the per-profile approval bar.
	// human_required: only a human actor may approve (cooperative check — client-asserted).
	// self_approve:   any actor kind may approve.
	// auto:           specgate_publish auto-approves via the canonical UpdateStatus path.
	KnownApprovalPolicies = map[string]bool{
		"human_required": true,
		"self_approve":   true,
		"auto":           true,
	}

	// KnownEvidencePolicies is the closed vocabulary for the per-profile evidence bar.
	// attested_ok:           single attester suffices (default).
	// corroborated_required: two independent reviewers required (not enforced in Slice A).
	KnownEvidencePolicies = map[string]bool{
		"attested_ok":           true,
		"corroborated_required": true,
	}
)

// DeliveryReviewGateKey is a valid gate_skills target even though it is not in
// KnownGates / enabled_gates — the post-build delivery review runs regardless of
// the readiness gate set, so a profile may bind a rubric Skill to it.
const DeliveryReviewGateKey = "delivery_review"

// ValidateDefinition checks a normalized definition against the closed vocabularies.
func ValidateDefinition(def Definition) error {
	check := func(field string, values []string, known map[string]bool) error {
		bad := make([]string, 0)
		for _, v := range values {
			if !known[v] {
				bad = append(bad, v)
			}
		}
		if len(bad) > 0 {
			sort.Strings(bad)
			return fmt.Errorf("%w: unknown %s %v", ErrInvalidDefinition, field, bad)
		}
		return nil
	}
	if err := check("required_topics", def.RequiredTopics, KnownTopics); err != nil {
		return err
	}
	if err := check("enabled_gates", def.EnabledGates, KnownGates); err != nil {
		return err
	}
	if err := check("required_roles", def.RequiredRoles, KnownRoles); err != nil {
		return err
	}
	if err := check("required_evidence", def.RequiredEvidence, KnownEvidence); err != nil {
		return err
	}
	if def.RendererKey != "" && !KnownRenderers[def.RendererKey] {
		return fmt.Errorf("%w: unknown renderer_key %q", ErrInvalidDefinition, def.RendererKey)
	}
	if def.ApprovalPolicy != "" && !KnownApprovalPolicies[def.ApprovalPolicy] {
		return fmt.Errorf("%w: unknown approval_policy %q", ErrInvalidDefinition, def.ApprovalPolicy)
	}
	if def.EvidencePolicy != "" && !KnownEvidencePolicies[def.EvidencePolicy] {
		return fmt.Errorf("%w: unknown evidence_policy %q", ErrInvalidDefinition, def.EvidencePolicy)
	}
	// gate_skills binds a rubric Skill (by name) to a gate. Keys must be a known
	// gate or delivery_review; values (skill names) must be non-empty.
	badGates := make([]string, 0)
	for gate, skill := range def.GateSkills {
		if !KnownGates[gate] && gate != DeliveryReviewGateKey {
			badGates = append(badGates, gate)
		}
		if strings.TrimSpace(skill) == "" {
			return fmt.Errorf("%w: gate_skills[%q] has an empty skill name", ErrInvalidDefinition, gate)
		}
	}
	if len(badGates) > 0 {
		sort.Strings(badGates)
		return fmt.Errorf("%w: unknown gate_skills gates %v", ErrInvalidDefinition, badGates)
	}
	return nil
}

// EffectiveApprovalPolicy returns v if set; otherwise "human_required" (safe default).
// Empty → human_required ensures unclassified or legacy snapshots keep the strictest bar.
func EffectiveApprovalPolicy(v string) string {
	if v == "" {
		return "human_required"
	}
	return v
}

// EffectiveEvidencePolicy returns v if set; otherwise "attested_ok" (default bar).
func EffectiveEvidencePolicy(v string) string {
	if v == "" {
		return "attested_ok"
	}
	return v
}

type Source string

const (
	SourceBuiltin Source = "builtin"
	SourceImport  Source = "import"
)

type Status string

const (
	StatusActive     Status = "active"
	StatusSuperseded Status = "superseded"
)

type Profile struct {
	ID             string    `gorm:"column:id;primaryKey"`
	Namespace      string    `gorm:"column:namespace;not null;uniqueIndex:uq_governance_profile_key_version,priority:1"`
	Key            string    `gorm:"column:key;not null;uniqueIndex:uq_governance_profile_key_version,priority:2"`
	Version        string    `gorm:"column:version;not null;uniqueIndex:uq_governance_profile_key_version,priority:3"`
	DisplayName    string    `gorm:"column:display_name;not null"`
	ChangeType     string    `gorm:"column:change_type;not null"`
	DefinitionJSON string    `gorm:"column:definition_json;not null"`
	Digest         string    `gorm:"column:digest;not null"`
	Source         Source    `gorm:"column:source;not null"`
	SourceRepo     string    `gorm:"column:source_repo;not null"`
	SourcePath     string    `gorm:"column:source_path;not null"`
	Status         Status    `gorm:"column:status;not null;index:idx_governance_profiles_status"`
	ImportedAt     time.Time `gorm:"column:imported_at;not null"`
	CreatedAt      time.Time `gorm:"column:created_at;not null"`
	UpdatedAt      time.Time `gorm:"column:updated_at;not null"`
}

func (Profile) TableName() string { return "governance_profiles" }

type Definition struct {
	DisplayName      string   `json:"display_name"`
	ChangeType       string   `json:"change_type"`
	RequiredRoles    []string `json:"required_roles"`
	RequiredTopics   []string `json:"required_topics"`
	RequiredEvidence []string `json:"required_evidence"`
	EnabledGates     []string `json:"enabled_gates"`
	RendererKey      string   `json:"renderer_key,omitempty"`
	// ApprovalPolicy controls who may approve an artifact with this profile.
	// omitempty: empty → omitted → existing stored digests remain stable.
	ApprovalPolicy string `json:"approval_policy,omitempty"`
	// EvidencePolicy controls the corroboration bar (not enforced in Slice A).
	// omitempty for the same digest-stability reason.
	EvidencePolicy string `json:"evidence_policy,omitempty"`
	// GateSkills binds a rubric Skill (by name) to a gate key: the readiness/delivery
	// judge injects that Skill's prompt as a "Team Policy" rubric. Keys are gate keys
	// (or delivery_review); values are skill names. omitempty + encoding/json's sorted
	// map-key marshaling keep the digest stable and deterministic.
	GateSkills map[string]string `json:"gate_skills,omitempty"`
}

type ImportInput struct {
	Namespace   string
	Key         string
	Version     string
	Definition  Definition
	SourceRepo  string
	SourcePath  string
	DisplayName string
	ChangeType  string
}

type ResolveInput struct {
	RequestedKey             string
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
	RendererKey              string               `json:"renderer_key,omitempty"`
	Digest                   string               `json:"digest"`
	Source                   Source               `json:"source"`
	ApprovalPolicy           string               `json:"approval_policy,omitempty"`
	EvidencePolicy           string               `json:"evidence_policy,omitempty"`
	GateSkills               map[string]string    `json:"gate_skills,omitempty"`
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
	def.RendererKey = strings.TrimSpace(def.RendererKey)
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

func ParseFullKey(full string) (namespace, key string) {
	full = strings.TrimSpace(full)
	if full == "" {
		return "", ""
	}
	if !strings.Contains(full, "/") {
		return "", full
	}
	parts := strings.SplitN(full, "/", 2)
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func FullKey(namespace, key string) string {
	namespace = strings.TrimSpace(namespace)
	key = strings.TrimSpace(key)
	if namespace == "" {
		return key
	}
	return fmt.Sprintf("%s/%s", namespace, key)
}

func (r ResolvedProfile) SnapshotJSON() (string, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return string(b), nil
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
