package governanceprofile

import (
	"sort"
)

// TriState is a three-value type for impact declaration fields where the answer
// may be "yes", "no", or "unknown" (safe default — unknown escalates protected-domain
// checks to enhanced without requiring a named domain).
type TriState string

const (
	TriYes     TriState = "yes"
	TriNo      TriState = "no"
	TriUnknown TriState = "unknown"
)

// ImpactDeclaration captures the author's self-declared impact signals for a change.
// It is embedded in ResolveInput and drives the built-in governance-level recommendation.
type ImpactDeclaration struct {
	// ProtectedDomainsStatus captures whether the author knows if this change
	// touches a protected domain. "unknown" is the safe default — it escalates
	// to enhanced without requiring a named domain (per design §6.2.3).
	ProtectedDomainsStatus        TriState `json:"protected_domains_status,omitempty"`
	ProtectedDomains              []string `json:"protected_domains,omitempty"`
	AffectedSystems               []string `json:"affected_systems,omitempty"`
	DataOrSchemaChange            TriState `json:"data_or_schema_change,omitempty"`
	ExternalContractChange        TriState `json:"external_contract_change,omitempty"`
	IrreversibleOrComplexRollback TriState `json:"irreversible_or_complex_rollback,omitempty"`
	BroadBlastRadius              TriState `json:"broad_blast_radius,omitempty"`
	ExpectedPaths                 []string `json:"expected_paths,omitempty"`
}

// governanceLevelRank maps governance levels to a numeric rank for ordering.
// Lower rank = less governance. Used by MaxGovernanceLevel.
var governanceLevelRank = map[GovernanceLevel]int{
	"":                 0,
	GovernanceLight:    1,
	GovernanceStandard: 2,
	GovernanceEnhanced: 3,
}

// MaxGovernanceLevel returns the higher of the two governance levels. An empty
// requested level is treated as the lowest (it never lowers the recommendation).
func MaxGovernanceLevel(recommended, requested GovernanceLevel) GovernanceLevel {
	rr := governanceLevelRank[recommended]
	req := governanceLevelRank[requested]
	if req > rr {
		return requested
	}
	return recommended
}

// sortedKnownGateKeys returns all KnownGates keys in sorted order.
// Enhanced governance enables all known gates — this helper ensures the set
// stays in sync with KnownGates without manual duplication.
func sortedKnownGateKeys() []string {
	keys := make([]string, 0, len(KnownGates))
	for k := range KnownGates {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// builtInLevels defines the execution projection for each built-in governance tier.
// Keys MUST reference only KnownGates, KnownRoles, KnownTopics, KnownEvidence,
// KnownRenderers, KnownApprovalPolicies, and KnownEvidencePolicies — enforced by
// TestBuiltInLevels_UseKnownVocabulary.
var builtInLevels = map[GovernanceLevel]Definition{
	GovernanceLight: {
		DisplayName:      "Light governance",
		RequiredRoles:    []string{"spec", "verification"},
		RequiredTopics:   []string{"outcomes", "scope", "acceptance_criteria", "verification"},
		RequiredEvidence: []string{"tests"},
		EnabledGates:     []string{"spec_completeness", "acceptance_criteria_verifiable"},
		ApprovalPolicy:   "self_approve",
		EvidencePolicy:   "attested_ok",
		RendererKey:      "default_context_pack",
	},
	GovernanceStandard: {
		DisplayName:      "Standard governance",
		RequiredRoles:    []string{"spec", "plan", "verification"},
		RequiredTopics:   []string{"outcomes", "scope", "acceptance_criteria", "verification"},
		RequiredEvidence: []string{"tests"},
		EnabledGates:     []string{"spec_completeness", "scope_clear", "acceptance_criteria_verifiable"},
		ApprovalPolicy:   "human_required",
		EvidencePolicy:   "attested_ok",
		RendererKey:      "default_context_pack",
	},
	GovernanceEnhanced: {
		DisplayName:      "Enhanced governance",
		RequiredRoles:    []string{"spec", "design", "plan", "verification"},
		RequiredTopics:   []string{"outcomes", "scope", "non_goals", "acceptance_criteria", "constraints", "rollout_rollback", "verification"},
		RequiredEvidence: []string{"tests", "rollout_defined"},
		EnabledGates:     sortedKnownGateKeys(),
		ApprovalPolicy:   "human_required",
		EvidencePolicy:   "corroborated_required",
		RendererKey:      "default_context_pack",
	},
}

// BuiltInLevelEntry is one entry in the ordered slice returned by
// ListBuiltInPolicyLevels — it binds a GovernanceLevel to its Definition.
type BuiltInLevelEntry struct {
	Level      GovernanceLevel
	Definition Definition
}

// builtInLevelOrder is the canonical display order for the three built-in tiers.
var builtInLevelOrder = []GovernanceLevel{
	GovernanceLight,
	GovernanceStandard,
	GovernanceEnhanced,
}

// ListBuiltInPolicyLevels returns the execution projection for each of the
// three built-in governance tiers in canonical order (light → standard → enhanced).
// It is a pure read of the static builtInLevels map; no DB access is performed.
func ListBuiltInPolicyLevels() []BuiltInLevelEntry {
	entries := make([]BuiltInLevelEntry, 0, len(builtInLevelOrder))
	for _, lv := range builtInLevelOrder {
		def := NormalizeDefinition(builtInLevels[lv])
		entries = append(entries, BuiltInLevelEntry{Level: lv, Definition: def})
	}
	return entries
}

// criticalUnknown returns true when the author does not know whether the change
// touches a protected domain. This is the sole trigger for unknown → enhanced
// escalation. Other unknown tri-state fields (schema change, rollback
// complexity, etc.) do not individually escalate to enhanced — they merely prevent
// light (already handled: no enhanced reasons + not a low-risk bugfix → standard).
func criticalUnknown(d ImpactDeclaration) bool {
	return d.ProtectedDomainsStatus == TriUnknown
}

// RecommendGovernanceLevel applies deterministic escalation rules to
// return a recommended level and the reason codes that drove it.
func RecommendGovernanceLevel(in ResolveInput) (GovernanceLevel, []string) {
	reasons := []string{}
	if in.ImpactLevel == "high" {
		reasons = append(reasons, "high_impact")
	}
	// ProtectedDomainsStatus == "yes" or named domains present → protected_domain.
	// ProtectedDomainsStatus == "unknown" → unknown_protected_domain (handled by
	// criticalUnknown below). Empty ProtectedDomains with status "no" → no escalation.
	d := in.ImpactDeclaration
	if d.ProtectedDomainsStatus == TriYes || (d.ProtectedDomainsStatus != TriNo && len(d.ProtectedDomains) > 0) {
		reasons = append(reasons, "protected_domain")
	}
	if d.DataOrSchemaChange == TriYes {
		reasons = append(reasons, "data_or_schema_change")
	}
	if d.ExternalContractChange == TriYes {
		reasons = append(reasons, "external_contract_change")
	}
	if d.IrreversibleOrComplexRollback == TriYes {
		reasons = append(reasons, "complex_rollback")
	}
	if d.BroadBlastRadius == TriYes {
		reasons = append(reasons, "broad_blast_radius")
	}
	if criticalUnknown(d) {
		reasons = append(reasons, "unknown_protected_domain")
	}
	if len(reasons) > 0 {
		return GovernanceEnhanced, dedupeSorted(reasons)
	}
	if in.RequestType == "bugfix" && in.ImpactLevel == "low" {
		return GovernanceLight, []string{"low_risk_bugfix"}
	}
	return GovernanceStandard, []string{"default_standard"}
}

// ResolveBuiltInPolicy resolves the governance level and execution projection for a
// change using the built-in policy. It does NOT consult the database;
// Service.ResolveProfile handles explicitly requested profile keys against the
// built-in and imported (database-backed) profile sets.
//
// The recommended level is normalized upward against any explicitly requested level
// (a requested level can never lower the recommendation).
func ResolveBuiltInPolicy(in ResolveInput) (*ResolvedProfile, error) {
	recommended, reasons := RecommendGovernanceLevel(in)
	effective := MaxGovernanceLevel(recommended, in.RequestedGovernanceLevel)

	def, ok := builtInLevels[effective]
	if !ok {
		// Fallback to standard if the effective level is unrecognized (defensive).
		def = builtInLevels[GovernanceStandard]
		effective = GovernanceStandard
	}

	def = NormalizeDefinition(def)
	digest, _, err := DefinitionDigest(def)
	if err != nil {
		return nil, err
	}

	return &ResolvedProfile{
		Namespace:                "builtin",
		Key:                      "policy_v1",
		FullKey:                  "builtin/policy_v1",
		Version:                  "1",
		DisplayName:              def.DisplayName,
		WorkType:                 in.RequestType,
		RiskLevel:                in.ImpactLevel,
		RequestedGovernanceLevel: in.RequestedGovernanceLevel,
		GovernanceLevel:          effective,
		ReasonCodes:              reasons,
		RequiredRoles:            def.RequiredRoles,
		RequiredTopics:           def.RequiredTopics,
		RequiredEvidence:         def.RequiredEvidence,
		EnabledGates:             def.EnabledGates,
		RendererKey:              def.RendererKey,
		Digest:                   digest,
		Source:                   SourceBuiltin,
		ApprovalPolicy:           def.ApprovalPolicy,
		EvidencePolicy:           def.EvidencePolicy,
	}, nil
}
