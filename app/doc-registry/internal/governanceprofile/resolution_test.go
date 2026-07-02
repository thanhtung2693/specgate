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

	// The fixture lives at the monorepo-level docs/ directory:
	//   specgate/docs/conformance/governance-policy-v1/resolution-cases.json
	// From the package (internal/governanceprofile/), that is 4 levels up to specgate/,
	// then into docs/conformance/...
	fixturesPath := filepath.Join("..", "..", "..", "..", "docs", "conformance", "governance-policy-v1", "resolution-cases.json")
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

// TestBuiltInLevels_UseKnownVocabulary verifies that every builtInLevel definition
// passes vocabulary validation, preventing bad keys from entering execution projections.
func TestBuiltInLevels_UseKnownVocabulary(t *testing.T) {
	t.Parallel()
	for level, def := range builtInLevels {
		if err := ValidateDefinition(def); err != nil {
			t.Errorf("builtInLevels[%q] fails vocabulary validation: %v", level, err)
		}
	}
}
