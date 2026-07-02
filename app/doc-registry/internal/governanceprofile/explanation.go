package governanceprofile

// Explanation is the human-readable governance decision surface returned with a
// published artifact. It carries the effective governance level, a short title,
// a one-line summary of the approval/evidence obligations, the structural reason
// codes that drove the level recommendation (as labelled strings), any additional
// obligations that apply (e.g. corroboration requirement), and the policy lineage
// chain for auditability.
type Explanation struct {
	// GovernanceLevel is the effective level: light, standard, or enhanced.
	GovernanceLevel GovernanceLevel `json:"governance_level"`
	// Title is a short human-readable label for the level.
	Title string `json:"title"`
	// Summary describes the core approval and evidence obligations in one sentence.
	Summary string `json:"summary"`
	// Reasons are labelled strings derived from the structural reason codes that
	// drove the governance-level recommendation. Labels are mapped from a closed
	// code vocabulary, not from natural-language keywords.
	Reasons []string `json:"reasons,omitempty"`
	// Obligations are additional actionable constraints beyond the level label:
	// e.g., corroboration requirements, rollback plan mandatory.
	Obligations []string `json:"obligations,omitempty"`
	// PolicyLineage records the profile version chain that produced this snapshot.
	PolicyLineage []PolicyLineageEntry `json:"policy_lineage,omitempty"`
}

// reasonCodeLabels maps structural reason codes to human-readable labels. Keys
// are the canonical code vocabulary defined by RecommendGovernanceLevel; values
// are English labels. Do not use natural-language keyword matching here — the
// mapping is code-key to label, not string pattern match on user input.
var reasonCodeLabels = map[string]string{
	"high_impact":              "High-impact change",
	"protected_domain":         "Touches a protected domain",
	"data_or_schema_change":    "Data or schema change",
	"external_contract_change": "External contract change",
	"complex_rollback":         "Irreversible or complex rollback",
	"broad_blast_radius":       "Broad blast radius",
	"unknown_protected_domain": "Protected-domain status unknown (escalated to enhanced)",
	"low_risk_bugfix":          "Low-risk bug fix",
	"default_standard":         "Default standard governance applies",
}

// ExplainSnapshot builds an Explanation from a ResolvedProfile. The function is
// pure and does not consult external state. It takes a ResolvedProfile (not the
// narrower ParsedSnapshot) because the explanation requires ReasonCodes and
// PolicyLineage, which are absent from ParsedSnapshot.
func ExplainSnapshot(p ResolvedProfile) Explanation {
	return Explanation{
		GovernanceLevel: p.GovernanceLevel,
		Title:           governanceLevelLabel(p.GovernanceLevel),
		Summary:         approvalSummary(p.ApprovalPolicy, p.EvidencePolicy),
		Reasons:         reasonLabels(p.ReasonCodes),
		Obligations:     obligationLabels(p),
		PolicyLineage:   p.PolicyLineage,
	}
}

// governanceLevelLabel returns the short display label for a governance level.
// It returns "Standard governance" for any unrecognized level so the caller is
// never handed an empty string.
func governanceLevelLabel(level GovernanceLevel) string {
	switch level {
	case GovernanceLight:
		return "Light governance"
	case GovernanceEnhanced:
		return "Enhanced governance"
	default:
		return "Standard governance"
	}
}

// approvalSummary builds a one-line sentence describing the approval and
// evidence obligations. It uses the effective (defaulted) policy values.
func approvalSummary(approvalPolicy, evidencePolicy string) string {
	approval := EffectiveApprovalPolicy(approvalPolicy)
	evidence := EffectiveEvidencePolicy(evidencePolicy)

	approvalDesc := approvalDesc(approval)
	evidenceDesc := evidenceDesc(evidence)
	return approvalDesc + "; " + evidenceDesc + "."
}

func approvalDesc(policy string) string {
	switch policy {
	case "auto":
		return "Auto-approved on publish"
	case "self_approve":
		return "Author self-approval allowed"
	default:
		// human_required or any unrecognized value
		return "Human approval required"
	}
}

func evidenceDesc(policy string) string {
	switch policy {
	case "corroborated_required":
		return "independently corroborated evidence required"
	default:
		// attested_ok or any unrecognized value
		return "agent attestation accepted"
	}
}

// reasonLabels maps each reason code to a human-readable label. Unknown codes
// are returned as-is rather than silently dropped, so the caller always sees a
// non-empty reasons list when reason codes were present.
func reasonLabels(codes []string) []string {
	if len(codes) == 0 {
		return nil
	}
	out := make([]string, 0, len(codes))
	for _, code := range codes {
		if label, ok := reasonCodeLabels[code]; ok {
			out = append(out, label)
		} else {
			// Pass through unknown codes so callers are not silently missing info.
			out = append(out, code)
		}
	}
	return out
}

// obligationLabels derives the list of additional obligations from the resolved
// profile fields (evidence policy, required evidence). They are structural
// projections, not keyword-matched strings.
func obligationLabels(p ResolvedProfile) []string {
	out := make([]string, 0, 3)
	effective := EffectiveApprovalPolicy(p.ApprovalPolicy)
	if effective == "human_required" {
		out = append(out, "A human reviewer must approve this artifact before it can be shipped.")
	}
	if EffectiveEvidencePolicy(p.EvidencePolicy) == "corroborated_required" {
		out = append(out, "Evidence must be independently corroborated (e.g., CI/CD webhook, external reviewer).")
	}
	for _, ev := range p.RequiredEvidence {
		switch ev {
		case "rollout_defined":
			out = append(out, "A rollout and rollback plan must be defined and attached.")
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
