package interactive_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/interactive"
)

func TestNormalizeImpactAnswers_MissingBecomesUnknown(t *testing.T) {
	t.Parallel()
	got := interactive.NormalizeImpactAnswers(interactive.ImpactAnswers{})
	if got.DataOrSchemaChange != "unknown" {
		t.Fatalf("data_or_schema_change = %q, want unknown", got.DataOrSchemaChange)
	}
	if got.ProtectedDomainsStatus != "unknown" {
		t.Fatalf("protected_domains_status = %q, want unknown", got.ProtectedDomainsStatus)
	}
	if got.ExternalContractChange != "unknown" {
		t.Fatalf("external_contract_change = %q, want unknown", got.ExternalContractChange)
	}
	if got.IrreversibleOrComplexRollback != "unknown" {
		t.Fatalf("irreversible_or_complex_rollback = %q, want unknown", got.IrreversibleOrComplexRollback)
	}
	if got.BroadBlastRadius != "unknown" {
		t.Fatalf("broad_blast_radius = %q, want unknown", got.BroadBlastRadius)
	}
}

func TestNormalizeImpactAnswers_ExistingValuesPreserved(t *testing.T) {
	t.Parallel()
	in := interactive.ImpactAnswers{
		ProtectedDomainsStatus:        "yes",
		DataOrSchemaChange:            "no",
		ExternalContractChange:        "unknown",
		IrreversibleOrComplexRollback: "yes",
		BroadBlastRadius:              "no",
	}
	got := interactive.NormalizeImpactAnswers(in)
	if got.ProtectedDomainsStatus != "yes" {
		t.Fatalf("protected_domains_status = %q, want yes", got.ProtectedDomainsStatus)
	}
	if got.DataOrSchemaChange != "no" {
		t.Fatalf("data_or_schema_change = %q, want no", got.DataOrSchemaChange)
	}
}

// TestNormalizeImpactAnswers_JSONSerializesSnakeCase proves that
// json.Marshal emits the snake_case keys the server expects. If any field
// is mis-tagged, "protected_domains_status":"unknown" would be absent and the
// server's fail-safe escalation would silently not fire.
func TestNormalizeImpactAnswers_JSONSerializesSnakeCase(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(interactive.NormalizeImpactAnswers(interactive.ImpactAnswers{}))
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	s := string(b)
	wantKeys := []string{
		`"protected_domains_status":"unknown"`,
		`"data_or_schema_change":"unknown"`,
		`"external_contract_change":"unknown"`,
		`"irreversible_or_complex_rollback":"unknown"`,
		`"broad_blast_radius":"unknown"`,
	}
	for _, k := range wantKeys {
		if !strings.Contains(s, k) {
			t.Errorf("JSON missing %s: %s", k, s)
		}
	}
}
