package governanceprofile

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestKnownApprovalPolicies_ContainsExpectedValues(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"human_required", "self_approve", "auto"} {
		if !KnownApprovalPolicies[v] {
			t.Errorf("KnownApprovalPolicies missing %q", v)
		}
	}
}

func TestKnownEvidencePolicies_ContainsExpectedValues(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"attested_ok", "corroborated_required"} {
		if !KnownEvidencePolicies[v] {
			t.Errorf("KnownEvidencePolicies missing %q", v)
		}
	}
}

func TestValidateDefinition_RejectsUnknownApprovalPolicy(t *testing.T) {
	t.Parallel()
	def := Definition{ApprovalPolicy: "banana"}
	if err := ValidateDefinition(def); !errors.Is(err, ErrInvalidDefinition) {
		t.Errorf("ValidateDefinition unknown approval_policy: err = %v, want ErrInvalidDefinition", err)
	}
}

func TestValidateDefinition_AcceptsKnownApprovalPolicies(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"human_required", "self_approve", "auto"} {
		def := Definition{ApprovalPolicy: v}
		if err := ValidateDefinition(def); err != nil {
			t.Errorf("ValidateDefinition(%q): unexpected error %v", v, err)
		}
	}
}

func TestValidateDefinition_EmptyApprovalPolicy_Accepted(t *testing.T) {
	t.Parallel()
	def := Definition{} // empty is valid — runtime defaults to human_required
	if err := ValidateDefinition(def); err != nil {
		t.Errorf("ValidateDefinition empty approval_policy: unexpected error %v", err)
	}
}

func TestValidateDefinition_RejectsUnknownEvidencePolicy(t *testing.T) {
	t.Parallel()
	def := Definition{EvidencePolicy: "banana"}
	if err := ValidateDefinition(def); !errors.Is(err, ErrInvalidDefinition) {
		t.Errorf("ValidateDefinition unknown evidence_policy: err = %v, want ErrInvalidDefinition", err)
	}
}

func TestValidateDefinition_EmptyEvidencePolicy_Accepted(t *testing.T) {
	t.Parallel()
	def := Definition{}
	if err := ValidateDefinition(def); err != nil {
		t.Errorf("ValidateDefinition empty evidence_policy: unexpected error %v", err)
	}
}

func TestEffectiveApprovalPolicy_DefaultsToHumanRequired(t *testing.T) {
	t.Parallel()
	if got := EffectiveApprovalPolicy(""); got != "human_required" {
		t.Errorf("EffectiveApprovalPolicy(\"\") = %q, want human_required", got)
	}
}

func TestEffectiveApprovalPolicy_ReturnsSetValue(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"self_approve", "auto", "human_required"} {
		if got := EffectiveApprovalPolicy(v); got != v {
			t.Errorf("EffectiveApprovalPolicy(%q) = %q, want %q", v, got, v)
		}
	}
}

func TestEffectiveEvidencePolicy_DefaultsToAttestedOk(t *testing.T) {
	t.Parallel()
	if got := EffectiveEvidencePolicy(""); got != "attested_ok" {
		t.Errorf("EffectiveEvidencePolicy(\"\") = %q, want attested_ok", got)
	}
}

func TestEffectiveEvidencePolicy_ReturnsSetValue(t *testing.T) {
	t.Parallel()
	if got := EffectiveEvidencePolicy("corroborated_required"); got != "corroborated_required" {
		t.Errorf("EffectiveEvidencePolicy(\"corroborated_required\") = %q, want corroborated_required", got)
	}
}

func TestNormalizeDefinition_TrimsApprovalAndEvidencePolicy(t *testing.T) {
	t.Parallel()
	def := Definition{ApprovalPolicy: "  self_approve  ", EvidencePolicy: "  attested_ok  "}
	got := NormalizeDefinition(def)
	if got.ApprovalPolicy != "self_approve" {
		t.Errorf("ApprovalPolicy = %q, want self_approve", got.ApprovalPolicy)
	}
	if got.EvidencePolicy != "attested_ok" {
		t.Errorf("EvidencePolicy = %q, want attested_ok", got.EvidencePolicy)
	}
}

// TestDefinitionDigest_EmptyPolicy_OmittedFromJSON ensures empty approval_policy
// and evidence_policy are omitted so existing stored profile digests stay stable.
func TestDefinitionDigest_EmptyPolicy_OmittedFromJSON(t *testing.T) {
	t.Parallel()
	def := Definition{DisplayName: "x", ChangeType: "y"}
	_, jsonStr, err := DefinitionDigest(def)
	if err != nil {
		t.Fatalf("DefinitionDigest: %v", err)
	}
	if strings.Contains(jsonStr, "approval_policy") {
		t.Errorf("approval_policy should be omitted from JSON when empty; got %s", jsonStr)
	}
	if strings.Contains(jsonStr, "evidence_policy") {
		t.Errorf("evidence_policy should be omitted from JSON when empty; got %s", jsonStr)
	}
}

// --- gate_skills (gate-consumes-Skills) ---

func TestValidateDefinition_AcceptsValidGateSkills(t *testing.T) {
	t.Parallel()
	def := Definition{
		GateSkills: map[string]string{
			"spec_completeness":              "spec-review",
			"acceptance_criteria_verifiable": "acceptance-criteria",
			"delivery_review":                "review-impl", // delivery_review is a valid rubric target though not in enabled_gates
		},
	}
	if err := ValidateDefinition(def); err != nil {
		t.Errorf("ValidateDefinition valid gate_skills: unexpected error %v", err)
	}
}

func TestValidateDefinition_RejectsUnknownGateInGateSkills(t *testing.T) {
	t.Parallel()
	def := Definition{GateSkills: map[string]string{"not_a_gate": "spec-review"}}
	if err := ValidateDefinition(def); !errors.Is(err, ErrInvalidDefinition) {
		t.Errorf("ValidateDefinition unknown gate_skills gate: err = %v, want ErrInvalidDefinition", err)
	}
}

func TestValidateDefinition_RejectsEmptySkillNameInGateSkills(t *testing.T) {
	t.Parallel()
	def := Definition{GateSkills: map[string]string{"spec_completeness": "   "}}
	if err := ValidateDefinition(def); !errors.Is(err, ErrInvalidDefinition) {
		t.Errorf("ValidateDefinition empty gate_skills skill name: err = %v, want ErrInvalidDefinition", err)
	}
}

func TestValidateDefinition_EmptyGateSkills_Accepted(t *testing.T) {
	t.Parallel()
	if err := ValidateDefinition(Definition{}); err != nil {
		t.Errorf("ValidateDefinition empty gate_skills: unexpected error %v", err)
	}
}

func TestNormalizeDefinition_TrimsGateSkills(t *testing.T) {
	t.Parallel()
	def := Definition{GateSkills: map[string]string{
		"  spec_completeness  ": "  spec-review  ",
		"scope_clear":           "   ", // empty value → dropped
	}}
	got := NormalizeDefinition(def)
	if got.GateSkills["spec_completeness"] != "spec-review" {
		t.Errorf("gate_skills not trimmed: %#v", got.GateSkills)
	}
	if _, ok := got.GateSkills["scope_clear"]; ok {
		t.Errorf("empty-value gate_skills entry should be dropped: %#v", got.GateSkills)
	}
}

func TestDefinitionDigest_EmptyGateSkills_OmittedFromJSON(t *testing.T) {
	t.Parallel()
	_, jsonStr, err := DefinitionDigest(Definition{DisplayName: "x", ChangeType: "y"})
	if err != nil {
		t.Fatalf("DefinitionDigest: %v", err)
	}
	if strings.Contains(jsonStr, "gate_skills") {
		t.Errorf("gate_skills should be omitted when empty; got %s", jsonStr)
	}
}

func TestDefinitionDigest_GateSkillsIncludedWhenSet_ChangesDigest(t *testing.T) {
	t.Parallel()
	base := Definition{DisplayName: "x", ChangeType: "y"}
	withSkills := Definition{DisplayName: "x", ChangeType: "y", GateSkills: map[string]string{"spec_completeness": "spec-review"}}
	d1, _, _ := DefinitionDigest(base)
	d2, _, _ := DefinitionDigest(withSkills)
	if d1 == d2 {
		t.Error("setting gate_skills should change the digest")
	}
}

func TestDefinitionDigest_GateSkills_Deterministic(t *testing.T) {
	t.Parallel()
	// Map key order must not affect the digest (encoding/json sorts map keys).
	a := Definition{GateSkills: map[string]string{"spec_completeness": "spec-review", "scope_clear": "prd-review"}}
	b := Definition{GateSkills: map[string]string{"scope_clear": "prd-review", "spec_completeness": "spec-review"}}
	d1, _, _ := DefinitionDigest(a)
	d2, _, _ := DefinitionDigest(b)
	if d1 != d2 {
		t.Errorf("gate_skills digest not deterministic across key order: %s vs %s", d1, d2)
	}
}

func TestDefinitionDigest_PolicyIncludedWhenSet_ChangesDigest(t *testing.T) {
	t.Parallel()
	base := Definition{DisplayName: "x", ChangeType: "y"}
	withPolicy := Definition{DisplayName: "x", ChangeType: "y", ApprovalPolicy: "self_approve"}
	d1, _, _ := DefinitionDigest(base)
	d2, _, _ := DefinitionDigest(withPolicy)
	if d1 == d2 {
		t.Error("setting approval_policy should change the digest")
	}
	_, jsonStr, _ := DefinitionDigest(withPolicy)
	var m map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if m["approval_policy"] != "self_approve" {
		t.Errorf("approval_policy not in digest JSON; got %v", m["approval_policy"])
	}
}
