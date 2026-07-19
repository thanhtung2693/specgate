package governanceprofile

import (
	"errors"
	"testing"
)

func TestParseSnapshot_MissingVersionFailsClosed(t *testing.T) {
	t.Parallel()
	if _, err := ParseSnapshot(`{"key":"bug_fix","approval_policy":"self_approve","enabled_gates":["spec_completeness"]}`); !errors.Is(err, ErrUnsupportedSnapshot) {
		t.Fatalf("err = %v, want ErrUnsupportedSnapshot", err)
	}
}

func TestParseSnapshot_MissingPolicyFieldsFailsClosed(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"snapshot_schema_version":"specgate.policy/v1","approval_policy":"human_required"}`,
		`{"snapshot_schema_version":"specgate.policy/v1","evidence_policy":"attested_ok"}`,
	} {
		if _, err := ParseSnapshot(raw); !errors.Is(err, ErrInvalidSnapshot) {
			t.Fatalf("ParseSnapshot(%s) err = %v, want ErrInvalidSnapshot", raw, err)
		}
	}
}

func TestParseSnapshot_PolicyV1(t *testing.T) {
	t.Parallel()
	got, err := ParseSnapshot(`{
	  "snapshot_schema_version":"specgate.policy/v1",
	  "work_type":"bugfix",
	  "governance_level":"enhanced",
	  "approval_policy":"human_required",
	  "evidence_policy":"attested_ok"
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

// TestParseSnapshot_PolicyV1_SchemaPropagated verifies SchemaVersion is set from the envelope.
func TestParseSnapshot_PolicyV1_SchemaPropagated(t *testing.T) {
	t.Parallel()
	got, err := ParseSnapshot(`{"snapshot_schema_version":"specgate.policy/v1","approval_policy":"human_required","evidence_policy":"attested_ok"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != SnapshotSchemaPolicyV1 {
		t.Fatalf("schema = %q, want specgate.policy/v1", got.SchemaVersion)
	}
	if got.ApprovalPolicy != "human_required" {
		t.Fatalf("approval = %q, want human_required", got.ApprovalPolicy)
	}
}

// TestParseSnapshot_PolicyV1_RequiredTopicsAndEvidencePolicy verifies that
// RequiredTopics and EvidencePolicy round-trip through the v1 path.
func TestParseSnapshot_PolicyV1_RequiredTopicsAndEvidencePolicy(t *testing.T) {
	t.Parallel()
	got, err := ParseSnapshot(`{
	  "snapshot_schema_version":"specgate.policy/v1",
	  "required_topics":["outcomes","verification"],
	  "approval_policy":"human_required",
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
