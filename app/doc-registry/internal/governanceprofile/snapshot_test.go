package governanceprofile

import (
	"errors"
	"testing"
)

func TestParseSnapshot_Legacy(t *testing.T) {
	t.Parallel()
	got, err := ParseSnapshot(`{"key":"bug_fix","approval_policy":"self_approve","enabled_gates":["spec_completeness"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != SnapshotSchemaLegacyV1 {
		t.Fatalf("schema = %q", got.SchemaVersion)
	}
	if got.ApprovalPolicy != "self_approve" {
		t.Fatalf("approval = %q", got.ApprovalPolicy)
	}
}

func TestParseSnapshot_PolicyV1(t *testing.T) {
	t.Parallel()
	got, err := ParseSnapshot(`{
	  "snapshot_schema_version":"specgate.policy/v1",
	  "work_type":"bugfix",
	  "governance_level":"enhanced",
	  "approval_policy":"human_required"
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.GovernanceLevel != GovernanceEnhanced {
		t.Fatalf("level = %q", got.GovernanceLevel)
	}
}

func TestParseSnapshot_CorruptFailsClosed(t *testing.T) {
	t.Parallel()
	if _, err := ParseSnapshot(`{"snapshot_schema_version":"unknown/v9"}`); !errors.Is(err, ErrUnsupportedSnapshot) {
		t.Fatalf("err = %v", err)
	}
}

// TestParseSnapshot_Empty ensures empty input returns a zero ParsedSnapshot without error.
func TestParseSnapshot_Empty(t *testing.T) {
	t.Parallel()
	got, err := ParseSnapshot("")
	if err != nil {
		t.Fatalf("unexpected error for empty input: %v", err)
	}
	if got.SchemaVersion != "" || got.ApprovalPolicy != "" || got.GovernanceLevel != "" {
		t.Fatalf("expected zero ParsedSnapshot for empty input, got %+v", got)
	}
}

// TestParseSnapshot_LegacyMissingApprovalPolicy checks that absent approval_policy is
// preserved raw (EffectiveApprovalPolicy handles defaulting at call-site).
func TestParseSnapshot_LegacyMissingApprovalPolicy(t *testing.T) {
	t.Parallel()
	got, err := ParseSnapshot(`{"key":"bug_fix","enabled_gates":["spec_completeness"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != SnapshotSchemaLegacyV1 {
		t.Fatalf("schema = %q, want legacy/v1", got.SchemaVersion)
	}
	// raw value should be empty; callers use EffectiveApprovalPolicy to default
	if got.ApprovalPolicy != "" {
		t.Fatalf("approval = %q, want empty (let EffectiveApprovalPolicy default)", got.ApprovalPolicy)
	}
}

// TestParseSnapshot_PolicyV1_SchemaPropagated verifies SchemaVersion is set from the envelope.
func TestParseSnapshot_PolicyV1_SchemaPropagated(t *testing.T) {
	t.Parallel()
	got, err := ParseSnapshot(`{"snapshot_schema_version":"specgate.policy/v1","approval_policy":"self_approve"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != SnapshotSchemaPolicyV1 {
		t.Fatalf("schema = %q, want specgate.policy/v1", got.SchemaVersion)
	}
	if got.ApprovalPolicy != "self_approve" {
		t.Fatalf("approval = %q, want self_approve", got.ApprovalPolicy)
	}
}

// TestParseSnapshot_Legacy_RequiredTopicsAndEvidencePolicy verifies that
// RequiredTopics and EvidencePolicy round-trip through the legacy path.
func TestParseSnapshot_Legacy_RequiredTopicsAndEvidencePolicy(t *testing.T) {
	t.Parallel()
	got, err := ParseSnapshot(`{
	  "key":"bug_fix",
	  "required_topics":["outcomes","verification"],
	  "evidence_policy":"corroborated_required"
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != SnapshotSchemaLegacyV1 {
		t.Fatalf("schema = %q, want legacy/v1", got.SchemaVersion)
	}
	wantTopics := []string{"outcomes", "verification"}
	if len(got.RequiredTopics) != len(wantTopics) {
		t.Fatalf("RequiredTopics = %v, want %v", got.RequiredTopics, wantTopics)
	}
	for i, v := range wantTopics {
		if got.RequiredTopics[i] != v {
			t.Fatalf("RequiredTopics[%d] = %q, want %q", i, got.RequiredTopics[i], v)
		}
	}
	if got.EvidencePolicy != "corroborated_required" {
		t.Fatalf("EvidencePolicy = %q, want corroborated_required", got.EvidencePolicy)
	}
}

// TestParseSnapshot_PolicyV1_RequiredTopicsAndEvidencePolicy verifies that
// RequiredTopics and EvidencePolicy round-trip through the v1 path.
func TestParseSnapshot_PolicyV1_RequiredTopicsAndEvidencePolicy(t *testing.T) {
	t.Parallel()
	got, err := ParseSnapshot(`{
	  "snapshot_schema_version":"specgate.policy/v1",
	  "required_topics":["outcomes","verification"],
	  "evidence_policy":"corroborated_required"
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != SnapshotSchemaPolicyV1 {
		t.Fatalf("schema = %q, want specgate.policy/v1", got.SchemaVersion)
	}
	wantTopics := []string{"outcomes", "verification"}
	if len(got.RequiredTopics) != len(wantTopics) {
		t.Fatalf("RequiredTopics = %v, want %v", got.RequiredTopics, wantTopics)
	}
	for i, v := range wantTopics {
		if got.RequiredTopics[i] != v {
			t.Fatalf("RequiredTopics[%d] = %q, want %q", i, got.RequiredTopics[i], v)
		}
	}
	if got.EvidencePolicy != "corroborated_required" {
		t.Fatalf("EvidencePolicy = %q, want corroborated_required", got.EvidencePolicy)
	}
}
