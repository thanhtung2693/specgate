package interactive

import (
	"io"

	"github.com/charmbracelet/huh"
)

// ImpactAnswers captures the author's self-declared impact signals from the
// interactive governance classification prompt. JSON tags use snake_case to
// match doc-registry's ImpactDeclaration API field names exactly.
type ImpactAnswers struct {
	// ProtectedDomainsStatus is "yes" / "no" / "unknown". Default must be
	// "unknown" so that a skipped question fails safe (→ enhanced).
	ProtectedDomainsStatus        string   `json:"protected_domains_status,omitempty"`
	ProtectedDomains              []string `json:"protected_domains,omitempty"`
	DataOrSchemaChange            string   `json:"data_or_schema_change,omitempty"`
	ExternalContractChange        string   `json:"external_contract_change,omitempty"`
	IrreversibleOrComplexRollback string   `json:"irreversible_or_complex_rollback,omitempty"`
	BroadBlastRadius              string   `json:"broad_blast_radius,omitempty"`
}

// triStateOptions returns the standard Yes / No / Unknown option set used for
// boolean impact fields.
func triStateOptions() []huh.Option[string] {
	return []huh.Option[string]{
		huh.NewOption("Yes", "yes"),
		huh.NewOption("No", "no"),
		huh.NewOption("Unknown", "unknown"),
	}
}

// NormalizeImpactAnswers maps empty-string tri-state fields to "unknown" so
// the server's fail-safe escalation logic fires correctly.
func NormalizeImpactAnswers(a ImpactAnswers) ImpactAnswers {
	norm := func(s string) string {
		if s == "" {
			return "unknown"
		}
		return s
	}
	a.ProtectedDomainsStatus = norm(a.ProtectedDomainsStatus)
	a.DataOrSchemaChange = norm(a.DataOrSchemaChange)
	a.ExternalContractChange = norm(a.ExternalContractChange)
	a.IrreversibleOrComplexRollback = norm(a.IrreversibleOrComplexRollback)
	a.BroadBlastRadius = norm(a.BroadBlastRadius)
	return a
}

// CollectImpactDeclaration presents a Huh form to collect the author's impact
// signals. The initial value is used as the starting point; any field set to ""
// is pre-defaulted to "unknown" before the form runs (fail-safe).
//
// r and w are the form's input and output streams, enabling non-interactive use
// in tests.
func CollectImpactDeclaration(r io.Reader, w io.Writer, initial ImpactAnswers) (ImpactAnswers, error) {
	if initial.ProtectedDomainsStatus == "" {
		initial.ProtectedDomainsStatus = "unknown"
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Does this change touch a protected domain (payments, auth, PII)?").
			Description("If unsure, choose 'Unknown' — this escalates governance to keep things safe.").
			Options(
				huh.NewOption("Yes", "yes"),
				huh.NewOption("No", "no"),
				huh.NewOption("Unknown", "unknown"),
			).
			Value(&initial.ProtectedDomainsStatus),
		huh.NewMultiSelect[string]().
			Title("Which protected domains? (skip if No / Unknown above)").
			Options(
				huh.NewOption("Authentication", "auth"),
				huh.NewOption("Payments", "payment"),
				huh.NewOption("Data migration", "migration"),
			).
			Value(&initial.ProtectedDomains),
		huh.NewSelect[string]().
			Title("Does it change stored data or schema?").
			Options(triStateOptions()...).
			Value(&initial.DataOrSchemaChange),
		huh.NewSelect[string]().
			Title("Does it change an external API or contract?").
			Options(triStateOptions()...).
			Value(&initial.ExternalContractChange),
		huh.NewSelect[string]().
			Title("Is rollback irreversible or operationally complex?").
			Options(triStateOptions()...).
			Value(&initial.IrreversibleOrComplexRollback),
		huh.NewSelect[string]().
			Title("Could this affect multiple services or many users?").
			Options(triStateOptions()...).
			Value(&initial.BroadBlastRadius),
	)).WithInput(r).WithOutput(w)
	return initial, form.Run()
}
