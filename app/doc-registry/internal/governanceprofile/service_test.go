package governanceprofile

import (
	"context"
	"errors"
	"testing"
)

type memRepo struct {
	rows []Profile
}

func (m *memRepo) Insert(_ context.Context, p *Profile) error {
	for _, row := range m.rows {
		if row.Namespace == p.Namespace && row.Key == p.Key && row.Version == p.Version {
			return ErrConflict
		}
	}
	m.rows = append(m.rows, *p)
	return nil
}

func (m *memRepo) ListActive(_ context.Context) ([]Profile, error) {
	out := make([]Profile, 0, len(m.rows))
	for _, row := range m.rows {
		if row.Status == StatusActive {
			out = append(out, row)
		}
	}
	return out, nil
}

func TestService_ListProfiles_IncludesBuiltins(t *testing.T) {
	t.Parallel()

	svc := NewService(&memRepo{})
	profiles, err := svc.ListProfiles(context.Background())
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	if len(profiles) < 5 {
		t.Fatalf("len(profiles) = %d, want at least 5 builtins", len(profiles))
	}
}

func TestService_ImportProfiles_AppendsNewVersion(t *testing.T) {
	t.Parallel()

	repo := &memRepo{}
	svc := NewService(repo)
	ctx := context.Background()

	_, err := svc.ImportProfiles(ctx, []ImportInput{{
		Namespace: "checkout-team",
		Key:       "bug_fix",
		Version:   "1",
		Definition: Definition{
			DisplayName:    "Bug fix",
			ChangeType:     "bug_fix",
			RequiredRoles:  []string{"spec", "verification"},
			RequiredTopics: []string{"outcomes", "verification"},
			EnabledGates:   []string{"spec_completeness"},
		},
	}})
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	_, err = svc.ImportProfiles(ctx, []ImportInput{{
		Namespace: "checkout-team",
		Key:       "bug_fix",
		Version:   "2",
		Definition: Definition{
			DisplayName:    "Bug fix v2",
			ChangeType:     "bug_fix",
			RequiredRoles:  []string{"spec"},
			RequiredTopics: []string{"outcomes"},
			EnabledGates:   []string{"spec_completeness"},
		},
	}})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if len(repo.rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(repo.rows))
	}
}

func TestService_Builtins_UseKnownVocabularyAndDifferentiateGates(t *testing.T) {
	t.Parallel()

	svc := NewService(&memRepo{})
	profiles, err := svc.ListProfiles(context.Background())
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	byKey := map[string]ResolvedProfile{}
	for _, p := range profiles {
		if p.Source == SourceBuiltin {
			byKey[p.Key] = p
		}
	}

	// Every builtin must reference only the closed readiness vocabulary, so a
	// snapshot can never freeze a topic/gate the readiness engine cannot act on.
	for key, p := range byKey {
		def := Definition{
			RequiredRoles:    p.RequiredRoles,
			RequiredTopics:   p.RequiredTopics,
			RequiredEvidence: p.RequiredEvidence,
			EnabledGates:     p.EnabledGates,
			RendererKey:      p.RendererKey,
		}
		if err := ValidateDefinition(def); err != nil {
			t.Errorf("builtin %q fails vocabulary validation: %v", key, err)
		}
	}

	// Gate bars are differentiated: high_impact (full) > generic > research_spike (minimal).
	if got := len(byKey["high_impact_feature"].EnabledGates); got != len(KnownGates) {
		t.Errorf("high_impact_feature enables %d gates, want all %d", got, len(KnownGates))
	}
	if got := byKey["research_spike"].EnabledGates; len(got) != 1 || got[0] != "spec_completeness" {
		t.Errorf("research_spike gates = %v, want [spec_completeness]", got)
	}
	if len(byKey["high_impact_feature"].EnabledGates) <= len(byKey["generic_change"].EnabledGates) {
		t.Error("high_impact_feature should enable more gates than generic_change")
	}
}

func TestService_ImportProfiles_RejectsUnknownVocabulary(t *testing.T) {
	t.Parallel()

	svc := NewService(&memRepo{})
	_, err := svc.ImportProfiles(context.Background(), []ImportInput{{
		Namespace:   "checkout-team",
		Key:         "weird",
		Version:     "1",
		DisplayName: "Weird",
		ChangeType:  "generic_change",
		Definition: Definition{
			RequiredTopics: []string{"frobnicate"}, // not a known topic
			EnabledGates:   []string{"spec_completeness"},
		},
	}})
	if !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("import with unknown topic err = %v, want ErrInvalidDefinition", err)
	}
}

func TestService_ResolveProfile_EmptyKeyUsesBuiltInPolicy(t *testing.T) {
	t.Parallel()

	svc := NewService(&memRepo{})
	got, err := svc.ResolveProfile(context.Background(), ResolveInput{RequestType: "bugfix"})
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if got.FullKey != "builtin/policy_v1" {
		t.Errorf("FullKey = %q, want builtin/policy_v1", got.FullKey)
	}
	if got.WorkType != "bugfix" {
		t.Errorf("WorkType = %q, want bugfix", got.WorkType)
	}
}

func TestService_ResolveProfile_ExplicitKeyResolvesBuiltin(t *testing.T) {
	t.Parallel()

	svc := NewService(&memRepo{})
	got, err := svc.ResolveProfile(context.Background(), ResolveInput{RequestedKey: "bug_fix"})
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if got.FullKey != "bug_fix" {
		t.Errorf("FullKey = %q, want bug_fix", got.FullKey)
	}
}

// TestBuiltinProfiles_ApprovalPolicies_MatchTable verifies the per-change-type
// governance bar. generic_change MUST stay human_required — it is the catch-all
// fallback; self_approve there would mean "unclassified change → no human gate".
func TestBuiltinProfiles_ApprovalPolicies_MatchTable(t *testing.T) {
	t.Parallel()

	svc := NewService(&memRepo{})
	profiles, err := svc.ListProfiles(context.Background())
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	byKey := map[string]ResolvedProfile{}
	for _, p := range profiles {
		if p.Source == SourceBuiltin {
			byKey[p.Key] = p
		}
	}

	want := map[string]string{
		"generic_change":      "human_required",
		"high_impact_feature": "human_required",
		"bug_fix":             "self_approve",
		"adr":                 "human_required",
		"research_spike":      "self_approve",
	}
	for key, wantPolicy := range want {
		p, ok := byKey[key]
		if !ok {
			t.Errorf("builtin %q not found", key)
			continue
		}
		if p.ApprovalPolicy != wantPolicy {
			t.Errorf("builtin %q approval_policy = %q, want %q", key, p.ApprovalPolicy, wantPolicy)
		}
	}
}

// TestBuiltinProfiles_GateSkills_BindRubrics verifies each built-in profile binds
// a rubric Skill to the gates it enables (gate-consumes-Skills). Every gate_skills
// key must be one of the profile's enabled_gates (or delivery_review), and the
// bound skill must be one of the kept rubric skills.
func TestBuiltinProfiles_GateSkills_BindRubrics(t *testing.T) {
	t.Parallel()

	svc := NewService(&memRepo{})
	profiles, err := svc.ListProfiles(context.Background())
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}

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

	for _, p := range profiles {
		if p.Source != SourceBuiltin {
			continue
		}
		if len(p.GateSkills) == 0 {
			t.Errorf("builtin %q has no gate_skills", p.Key)
			continue
		}
		enabled := map[string]bool{}
		for _, g := range p.EnabledGates {
			enabled[g] = true
		}
		for gate, skill := range p.GateSkills {
			if want, ok := wantRubric[gate]; !ok || want != skill {
				t.Errorf("builtin %q gate_skills[%q] = %q, want %q", p.Key, gate, skill, wantRubric[gate])
			}
			// Every bound gate (except delivery_review, which runs post-build
			// regardless) must be in the profile's enabled_gates.
			if gate != "delivery_review" && !enabled[gate] {
				t.Errorf("builtin %q binds gate_skills for %q but does not enable that gate", p.Key, gate)
			}
		}
	}
}

// TestBuiltinProfiles_EvidencePolicies_MatchTable verifies built-in profiles
// carry the Slice B evidence_policy values (per specgate-design-notes.md §Slice B).
func TestBuiltinProfiles_EvidencePolicies_MatchTable(t *testing.T) {
	t.Parallel()

	svc := NewService(&memRepo{})
	profiles, err := svc.ListProfiles(context.Background())
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	byKey := map[string]ResolvedProfile{}
	for _, p := range profiles {
		if p.Source == SourceBuiltin {
			byKey[p.Key] = p
		}
	}

	want := map[string]string{
		"generic_change":      "attested_ok",
		"high_impact_feature": "corroborated_required",
		"bug_fix":             "attested_ok",
		"adr":                 "attested_ok",
		"research_spike":      "attested_ok",
	}
	for key, wantPolicy := range want {
		p, ok := byKey[key]
		if !ok {
			t.Errorf("builtin %q not found", key)
			continue
		}
		if p.EvidencePolicy != wantPolicy {
			t.Errorf("builtin %q evidence_policy = %q, want %q", key, p.EvidencePolicy, wantPolicy)
		}
	}
}
