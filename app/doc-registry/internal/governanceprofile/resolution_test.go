package governanceprofile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// ---- Conformance matrix ----

// conformanceFixtures is the top-level envelope of resolution-cases.json.
type conformanceFixtures struct {
	SchemaVersion string            `json:"schema_version"`
	Cases         []conformanceCase `json:"cases"`
}

type conformanceCase struct {
	Name  string `json:"name"`
	Input struct {
		RequestType              string            `json:"request_type"`
		ImpactLevel              string            `json:"impact_level"`
		RequestedGovernanceLevel GovernanceLevel   `json:"requested_governance_level,omitempty"`
		ImpactDeclaration        ImpactDeclaration `json:"impact_declaration"`
	} `json:"input"`
	Want struct {
		GovernanceLevel GovernanceLevel `json:"governance_level"`
		ReasonCodes     []string        `json:"reason_codes"`
	} `json:"want"`
}

func TestResolutionConformance(t *testing.T) {
	t.Parallel()

	fixturesPath := filepath.Join("testdata", "resolution-cases.json")
	data, err := os.ReadFile(fixturesPath)
	if err != nil {
		t.Fatalf("read conformance fixtures: %v", err)
	}
	var fixtures conformanceFixtures
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatalf("parse conformance fixtures: %v", err)
	}
	if len(fixtures.Cases) == 0 {
		t.Fatal("no conformance cases found")
	}

	for _, tc := range fixtures.Cases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveBuiltInPolicy(ResolveInput{
				RequestType:              tc.Input.RequestType,
				ImpactLevel:              tc.Input.ImpactLevel,
				RequestedGovernanceLevel: tc.Input.RequestedGovernanceLevel,
				ImpactDeclaration:        tc.Input.ImpactDeclaration,
			})
			if err != nil {
				t.Fatalf("ResolveBuiltInPolicy: %v", err)
			}
			if got.GovernanceLevel != tc.Want.GovernanceLevel {
				t.Errorf("level = %q, want %q", got.GovernanceLevel, tc.Want.GovernanceLevel)
			}
			if !reasonSetEqual(got.ReasonCodes, tc.Want.ReasonCodes) {
				t.Errorf("reasons = %v, want %v", got.ReasonCodes, tc.Want.ReasonCodes)
			}
		})
	}
}

// reasonSetEqual compares two string slices as unordered sets.
func reasonSetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sa := make([]string, len(a))
	copy(sa, a)
	sort.Strings(sa)
	sb := make([]string, len(b))
	copy(sb, b)
	sort.Strings(sb)
	for i := range sa {
		if sa[i] != sb[i] {
			return false
		}
	}
	return true
}

// ---- High-risk bugfix regression ----

func TestResolveBuiltInPolicy_HighRiskBugfixIsEnhanced(t *testing.T) {
	t.Parallel()
	got, err := ResolveBuiltInPolicy(ResolveInput{
		RequestType: "bugfix",
		ImpactLevel: "high",
		ImpactDeclaration: ImpactDeclaration{
			IrreversibleOrComplexRollback: TriYes,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.GovernanceLevel != GovernanceEnhanced {
		t.Fatalf("level = %q, want enhanced", got.GovernanceLevel)
	}
}

func TestResolveBuiltInPolicy_MissingProtectedDomainStatusIsConservative(t *testing.T) {
	t.Parallel()
	for _, declaration := range []ImpactDeclaration{
		{},
		{ProtectedDomainsStatus: ""},
	} {
		got, err := ResolveBuiltInPolicy(ResolveInput{
			RequestType:       "change_request",
			ImpactLevel:       "medium",
			ImpactDeclaration: declaration,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.GovernanceLevel != GovernanceEnhanced {
			t.Fatalf("level = %q, want enhanced for declaration %+v", got.GovernanceLevel, declaration)
		}
		if !reasonSetEqual(got.ReasonCodes, []string{"unknown_protected_domain"}) {
			t.Fatalf("reasons = %v, want [unknown_protected_domain]", got.ReasonCodes)
		}
	}
}

func TestResolveBuiltInPolicy_RequestedLevelCannotLowerEnhancedRecommendation(t *testing.T) {
	t.Parallel()
	got, err := ResolveBuiltInPolicy(ResolveInput{
		RequestType:              "change_request",
		ImpactLevel:              "medium",
		RequestedGovernanceLevel: GovernanceLight,
		ImpactDeclaration:        ImpactDeclaration{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.GovernanceLevel != GovernanceEnhanced {
		t.Fatalf("level = %q, want enhanced", got.GovernanceLevel)
	}
}

// TestSpecRepoDriftGateRegistered proves AC1: spec_repo_drift is a registered gate
// key and reaches the resolved enabled-gates set for a governed feature spec
// (new_feature / medium → standard, the profile the test feature resolves to).
func TestSpecRepoDriftGateRegistered(t *testing.T) {
	t.Parallel()

	if !KnownGates["spec_repo_drift"] {
		t.Fatal("spec_repo_drift is not registered in KnownGates")
	}

	got, err := ResolveBuiltInPolicy(ResolveInput{
		RequestType: "new_feature",
		ImpactLevel: "medium",
		ImpactDeclaration: ImpactDeclaration{
			ProtectedDomainsStatus: TriNo,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.GovernanceLevel != GovernanceStandard {
		t.Fatalf("level = %q, want standard", got.GovernanceLevel)
	}
	found := false
	for _, g := range got.EnabledGates {
		if g == "spec_repo_drift" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("spec_repo_drift not in resolved enabled_gates %v", got.EnabledGates)
	}
}

func TestBuiltInLevels_ExposeAutomaticGateSkills(t *testing.T) {
	t.Parallel()
	entries := ListBuiltInPolicyLevels()
	wantRubric := map[string]string{
		"spec_completeness":              "spec-review",
		"scope_clear":                    "prd-review",
		"success_metric_measurable":      "prd-review",
		"acceptance_criteria_verifiable": "acceptance-criteria",
		"acceptance_criteria_edge_cases": "acceptance-criteria",
		"implementation_plan_traceable":  "task-breakdown",
		"rollback_plan_present":          "rollout-risk",
		"delivery_review":                "review-impl",
	}
	for _, entry := range entries {
		if len(entry.Definition.GateSkills) == 0 {
			t.Fatalf("%s gate_skills empty", entry.Level)
		}
		enabled := map[string]bool{}
		for _, gate := range entry.Definition.EnabledGates {
			enabled[gate] = true
		}
		for gate, skill := range entry.Definition.GateSkills {
			if wantRubric[gate] != skill {
				t.Fatalf("%s gate_skills[%q] = %q, want %q", entry.Level, gate, skill, wantRubric[gate])
			}
			if gate != DeliveryReviewGateKey && !enabled[gate] {
				t.Fatalf("%s binds %q without enabling it", entry.Level, gate)
			}
		}
	}
}
