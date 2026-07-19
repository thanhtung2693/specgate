package governanceprofile

import (
	"strings"
	"testing"
)

func TestExplainSnapshot_LightGovernance(t *testing.T) {
	t.Parallel()
	profile := ResolvedProfile{
		GovernanceLevel: GovernanceLight,
		ApprovalPolicy:  "human_required",
		EvidencePolicy:  "attested_ok",
		ReasonCodes:     []string{"low_risk_bugfix"},
	}
	exp := ExplainSnapshot(profile)
	if exp.GovernanceLevel != GovernanceLight {
		t.Fatalf("GovernanceLevel = %q, want %q", exp.GovernanceLevel, GovernanceLight)
	}
	if exp.Title != "Light governance" {
		t.Fatalf("Title = %q, want %q", exp.Title, "Light governance")
	}
	if len(exp.Reasons) == 0 {
		t.Fatal("expected non-empty Reasons")
	}
}

func TestExplainSnapshot_EnhancedGovernance(t *testing.T) {
	t.Parallel()
	profile := ResolvedProfile{
		GovernanceLevel: GovernanceEnhanced,
		ApprovalPolicy:  "human_required",
		EvidencePolicy:  "corroborated_required",
		ReasonCodes:     []string{"high_impact", "protected_domain"},
	}
	exp := ExplainSnapshot(profile)
	if exp.GovernanceLevel != GovernanceEnhanced {
		t.Fatalf("GovernanceLevel = %q, want %q", exp.GovernanceLevel, GovernanceEnhanced)
	}
	if exp.Title != "Enhanced governance" {
		t.Fatalf("Title = %q, want %q", exp.Title, "Enhanced governance")
	}
	if len(exp.Reasons) != 2 {
		t.Fatalf("Reasons = %v, want 2 items", exp.Reasons)
	}
	if len(exp.Obligations) == 0 {
		t.Fatal("expected non-empty Obligations for human_required + corroborated_required")
	}
}

func TestExplainSnapshot_StandardGovernance(t *testing.T) {
	t.Parallel()
	profile := ResolvedProfile{
		GovernanceLevel: GovernanceStandard,
		ApprovalPolicy:  "human_required",
		EvidencePolicy:  "attested_ok",
		ReasonCodes:     []string{"default_standard"},
	}
	exp := ExplainSnapshot(profile)
	if exp.GovernanceLevel != GovernanceStandard {
		t.Fatalf("GovernanceLevel = %q, want %q", exp.GovernanceLevel, GovernanceStandard)
	}
	if exp.Title != "Standard governance" {
		t.Fatalf("Title = %q, want %q", exp.Title, "Standard governance")
	}
}

func TestExplainSnapshot_PolicyLineagePropagated(t *testing.T) {
	t.Parallel()
	profile := ResolvedProfile{
		GovernanceLevel: GovernanceStandard,
		ApprovalPolicy:  "human_required",
		EvidencePolicy:  "attested_ok",
		ReasonCodes:     []string{"default_standard"},
		PolicyLineage: []PolicyLineageEntry{
			{Key: "builtin/standard", Version: "1", Digest: "sha256:abc"},
		},
	}
	exp := ExplainSnapshot(profile)
	if len(exp.PolicyLineage) != 1 {
		t.Fatalf("PolicyLineage = %v, want 1 entry", exp.PolicyLineage)
	}
	if exp.PolicyLineage[0].Key != "builtin/standard" {
		t.Fatalf("PolicyLineage[0].Key = %q", exp.PolicyLineage[0].Key)
	}
}

func TestExplainSnapshot_UnknownReasonCodeHandled(t *testing.T) {
	t.Parallel()
	// Unknown reason codes must not panic; they should produce a non-empty label.
	profile := ResolvedProfile{
		GovernanceLevel: GovernanceEnhanced,
		ApprovalPolicy:  "human_required",
		EvidencePolicy:  "attested_ok",
		ReasonCodes:     []string{"unknown_future_code"},
	}
	exp := ExplainSnapshot(profile)
	if len(exp.Reasons) == 0 {
		t.Fatal("expected non-empty Reasons even for unknown code")
	}
}

func TestExplainSnapshot_InvalidEvidencePolicyDoesNotClaimAttestation(t *testing.T) {
	t.Parallel()
	explanation := ExplainSnapshot(ResolvedProfile{
		GovernanceLevel: GovernanceStandard,
		ApprovalPolicy:  "human_required",
	})
	if strings.Contains(explanation.Summary, "attestation accepted") || !strings.Contains(explanation.Summary, "evidence policy unavailable") {
		t.Fatalf("Summary = %q, want fail-closed policy wording", explanation.Summary)
	}
}
