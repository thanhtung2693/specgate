package governanceprofile

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestKnownApprovalPolicies_ContainsExpectedValues(t *testing.T) {
	t.Parallel()
	if len(KnownApprovalPolicies) != 1 || !KnownApprovalPolicies["human_required"] {
		t.Fatalf("KnownApprovalPolicies = %#v", KnownApprovalPolicies)
	}
}

func TestKnownEvidencePolicies_ContainsExpectedValues(t *testing.T) {
	t.Parallel()
	if len(KnownEvidencePolicies) != 2 {
		t.Fatalf("KnownEvidencePolicies = %#v", KnownEvidencePolicies)
	}
	for _, v := range []string{"attested_ok", "corroborated_required"} {
		if !KnownEvidencePolicies[v] {
			t.Errorf("KnownEvidencePolicies missing %q", v)
		}
	}
}

func TestNormalizeDefinition_TrimsApprovalAndEvidencePolicy(t *testing.T) {
	t.Parallel()
	def := Definition{ApprovalPolicy: "  human_required  ", EvidencePolicy: "  attested_ok  "}
	got := NormalizeDefinition(def)
	if got.ApprovalPolicy != "human_required" {
		t.Errorf("ApprovalPolicy = %q, want human_required", got.ApprovalPolicy)
	}
	if got.EvidencePolicy != "attested_ok" {
		t.Errorf("EvidencePolicy = %q, want attested_ok", got.EvidencePolicy)
	}
}

func TestDefinitionDigest_KeepsPolicyFieldsInContract(t *testing.T) {
	t.Parallel()
	def := Definition{DisplayName: "x", ChangeType: "y"}
	_, jsonStr, err := DefinitionDigest(def)
	if err != nil {
		t.Fatalf("DefinitionDigest: %v", err)
	}
	if !strings.Contains(jsonStr, `"approval_policy":""`) {
		t.Errorf("approval_policy missing from definition JSON: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"evidence_policy":""`) {
		t.Errorf("evidence_policy missing from definition JSON: %s", jsonStr)
	}
}

// --- gate_skills (gate-consumes-Skills) ---

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

func TestFreezeGateDefinitions_RejectsMissingBoundSkill(t *testing.T) {
	t.Parallel()
	resolved := ResolvedProfile{
		EnabledGates: []string{"scope_clear"},
		GateSkills:   map[string]string{"scope_clear": "prd-review"},
	}

	err := resolved.FreezeGateDefinitions(nil)
	if !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("FreezeGateDefinitions: err = %v, want ErrInvalidDefinition", err)
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
	withPolicy := Definition{DisplayName: "x", ChangeType: "y", ApprovalPolicy: "human_required"}
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
	if m["approval_policy"] != "human_required" {
		t.Errorf("approval_policy not in digest JSON; got %v", m["approval_policy"])
	}
}
