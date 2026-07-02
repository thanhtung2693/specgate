package governanceprofile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("governance profile not found")
var ErrConflict = errors.New("governance profile conflict")

type Repository interface {
	Insert(context.Context, *Profile) error
	ListActive(context.Context) ([]Profile, error)
}

type Service struct {
	repo Repository
	now  func() time.Time
}

func NewService(repo Repository) *Service {
	return &Service{
		repo: repo,
		now:  func() time.Time { return time.Now().UTC() },
	}
}

// ResolveBuiltInPolicy delegates to the package-level ResolveBuiltInPolicy function.
// It is a method on Service so callers that already hold a *Service do not need to
// import the function separately.
func (s *Service) ResolveBuiltInPolicy(in ResolveInput) (*ResolvedProfile, error) {
	return ResolveBuiltInPolicy(in)
}

func (s *Service) ListProfiles(ctx context.Context) ([]ResolvedProfile, error) {
	profiles := builtinProfiles()
	imported, err := s.repo.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	latest := latestImported(imported)
	for _, p := range latest {
		rp, err := profileToResolved(p)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, rp)
	}
	slices.SortFunc(profiles, func(a, b ResolvedProfile) int {
		return strings.Compare(a.FullKey, b.FullKey)
	})
	return profiles, nil
}

func (s *Service) ImportProfiles(ctx context.Context, inputs []ImportInput) ([]ResolvedProfile, error) {
	out := make([]ResolvedProfile, 0, len(inputs))
	for _, in := range inputs {
		if strings.TrimSpace(in.Namespace) == "" || strings.TrimSpace(in.Key) == "" || strings.TrimSpace(in.Version) == "" {
			return nil, fmt.Errorf("namespace, key, and version are required")
		}
		def := NormalizeDefinition(in.Definition)
		if in.DisplayName != "" {
			def.DisplayName = strings.TrimSpace(in.DisplayName)
		}
		if in.ChangeType != "" {
			def.ChangeType = strings.TrimSpace(in.ChangeType)
		}
		if def.DisplayName == "" || def.ChangeType == "" {
			return nil, fmt.Errorf("display_name and change_type are required")
		}
		if err := ValidateDefinition(def); err != nil {
			return nil, err
		}
		digest, normalizedJSON, err := DefinitionDigest(def)
		if err != nil {
			return nil, err
		}
		now := s.now()
		p := &Profile{
			ID:             uuid.NewString(),
			Namespace:      strings.TrimSpace(in.Namespace),
			Key:            strings.TrimSpace(in.Key),
			Version:        strings.TrimSpace(in.Version),
			DisplayName:    def.DisplayName,
			ChangeType:     def.ChangeType,
			DefinitionJSON: normalizedJSON,
			Digest:         digest,
			Source:         SourceImport,
			SourceRepo:     strings.TrimSpace(in.SourceRepo),
			SourcePath:     strings.TrimSpace(in.SourcePath),
			Status:         StatusActive,
			ImportedAt:     now,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := s.repo.Insert(ctx, p); err != nil {
			return nil, err
		}
		rp, err := profileToResolved(*p)
		if err != nil {
			return nil, err
		}
		out = append(out, rp)
	}
	return out, nil
}

func (s *Service) ResolveProfile(ctx context.Context, in ResolveInput) (*ResolvedProfile, error) {
	// When no explicit profile key is requested, delegate to the built-in
	// policy resolver.
	if strings.TrimSpace(in.RequestedKey) == "" {
		return ResolveBuiltInPolicy(in)
	}
	requested := strings.TrimSpace(in.RequestedKey)
	if builtin, ok := builtinByKey(requested); ok {
		return &builtin, nil
	}
	ns, key := ParseFullKey(requested)
	if ns == "" || key == "" {
		return nil, ErrNotFound
	}
	imported, err := s.repo.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	var match *Profile
	for _, p := range imported {
		if p.Namespace != ns || p.Key != key || p.Status != StatusActive {
			continue
		}
		if match == nil || compareVersion(p.Version, match.Version) > 0 {
			cp := p
			match = &cp
		}
	}
	if match == nil {
		return nil, ErrNotFound
	}
	rp, err := profileToResolved(*match)
	if err != nil {
		return nil, err
	}
	return &rp, nil
}

func builtinByKey(key string) (ResolvedProfile, bool) {
	for _, p := range builtinProfiles() {
		if p.FullKey == key || p.Key == key {
			return p, true
		}
	}
	return ResolvedProfile{}, false
}

func builtinProfiles() []ResolvedProfile {
	out := make([]ResolvedProfile, 0, 5)
	add := func(key, displayName, changeType string, roles, topics, evidence, gates []string, renderer, approvalPolicy, evidencePolicy string) {
		gateSkills := builtinGateSkillsFor(gates)
		def := Definition{
			DisplayName:      displayName,
			ChangeType:       changeType,
			RequiredRoles:    roles,
			RequiredTopics:   topics,
			RequiredEvidence: evidence,
			EnabledGates:     gates,
			RendererKey:      renderer,
			ApprovalPolicy:   approvalPolicy,
			EvidencePolicy:   evidencePolicy,
			GateSkills:       gateSkills,
		}
		digest, _, _ := DefinitionDigest(def)
		out = append(out, ResolvedProfile{
			Namespace:        "builtin",
			Key:              key,
			FullKey:          key,
			Version:          "1",
			DisplayName:      displayName,
			ChangeType:       changeType,
			RequiredRoles:    def.RequiredRoles,
			RequiredTopics:   def.RequiredTopics,
			RequiredEvidence: def.RequiredEvidence,
			EnabledGates:     def.EnabledGates,
			RendererKey:      renderer,
			Digest:           digest,
			Source:           SourceBuiltin,
			ApprovalPolicy:   approvalPolicy,
			EvidencePolicy:   evidencePolicy,
			GateSkills:       gateSkills,
		})
	}
	// Topic keys mirror the agents completeness gate (outcomes=goal, rollout_rollback=risks);
	// gate keys mirror ALL_LLM_GATES. enabled_gates is differentiated per the governance bar
	// (generic=light, high_impact=full, bug_fix/adr/research=minimal). Both are validated
	// against KnownTopics/KnownGates on import.
	//
	// generic_change MUST stay human_required — it is the built-in policy
	// resolver's catch-all; self_approve there = "unclassified change → no human
	// gate" (a governance hole).
	add("generic_change", "Generic change", "generic_change",
		[]string{"spec", "plan"},
		[]string{"outcomes", "scope", "acceptance_criteria", "verification"},
		[]string{"tests"},
		[]string{"spec_completeness", "scope_clear", "acceptance_criteria_verifiable"},
		"default_context_pack",
		"human_required", "attested_ok",
	)
	add("high_impact_feature", "High impact feature", "high_impact_feature",
		[]string{"spec", "design", "plan", "verification"},
		[]string{"outcomes", "scope", "non_goals", "acceptance_criteria", "constraints", "rollout_rollback", "verification"},
		[]string{"tests", "rollout_defined"},
		[]string{
			"spec_completeness", "scope_clear", "acceptance_criteria_verifiable",
			"acceptance_criteria_edge_cases", "success_metric_measurable",
			"rollback_plan_present", "implementation_plan_traceable",
		},
		"default_context_pack",
		// per spec §B: high-impact changes require independently-corroborated evidence
		// (git/CI webhook), not just agent self-attestation.
		"human_required", "corroborated_required",
	)
	add("bug_fix", "Bug fix", "bug_fix",
		[]string{"spec", "verification"},
		[]string{"outcomes", "scope", "acceptance_criteria", "verification"},
		[]string{"tests"},
		[]string{"spec_completeness", "acceptance_criteria_verifiable"},
		"default_context_pack",
		"self_approve", "attested_ok",
	)
	add("adr", "Architecture decision", "adr",
		[]string{"spec", "research"},
		[]string{"outcomes", "constraints", "rollout_rollback"},
		nil,
		[]string{"spec_completeness", "scope_clear"},
		"default_context_pack",
		"human_required", "attested_ok",
	)
	add("research_spike", "Research spike", "research_spike",
		[]string{"spec", "research"},
		[]string{"outcomes", "scope"},
		nil,
		[]string{"spec_completeness"},
		"default_context_pack",
		"self_approve", "attested_ok",
	)
	return out
}

func profileToResolved(p Profile) (ResolvedProfile, error) {
	var def Definition
	if err := jsonUnmarshal([]byte(p.DefinitionJSON), &def); err != nil {
		return ResolvedProfile{}, err
	}
	def = NormalizeDefinition(def)
	return ResolvedProfile{
		Namespace:        p.Namespace,
		Key:              p.Key,
		FullKey:          FullKey(p.Namespace, p.Key),
		Version:          p.Version,
		DisplayName:      def.DisplayName,
		ChangeType:       def.ChangeType,
		RequiredRoles:    def.RequiredRoles,
		RequiredTopics:   def.RequiredTopics,
		RequiredEvidence: def.RequiredEvidence,
		EnabledGates:     def.EnabledGates,
		RendererKey:      def.RendererKey,
		Digest:           p.Digest,
		Source:           p.Source,
		ApprovalPolicy:   def.ApprovalPolicy,
		EvidencePolicy:   def.EvidencePolicy,
		GateSkills:       def.GateSkills,
	}, nil
}

// builtinGateRubricSkills binds each readiness gate to the kept rubric Skill the
// gate judge injects as team policy. delivery_review (post-build) is added to
// every built-in separately since it runs regardless of enabled_gates.
var builtinGateRubricSkills = map[string]string{
	"spec_completeness":              "spec-review",
	"scope_clear":                    "prd-review",
	"success_metric_measurable":      "prd-review",
	"acceptance_criteria_verifiable": "acceptance-criteria",
	"acceptance_criteria_edge_cases": "acceptance-criteria",
	"implementation_plan_traceable":  "task-breakdown",
	"rollback_plan_present":          "rollout-risk",
}

// builtinGateSkillsFor derives a profile's gate_skills from its enabled gates
// (only gates with a rubric are bound) plus the always-on delivery_review rubric.
func builtinGateSkillsFor(enabledGates []string) map[string]string {
	m := make(map[string]string, len(enabledGates)+1)
	for _, g := range enabledGates {
		if skill, ok := builtinGateRubricSkills[g]; ok {
			m[g] = skill
		}
	}
	m[DeliveryReviewGateKey] = "review-impl"
	return m
}

func latestImported(rows []Profile) []Profile {
	byKey := map[string]Profile{}
	for _, row := range rows {
		full := FullKey(row.Namespace, row.Key)
		current, ok := byKey[full]
		if !ok || compareVersion(row.Version, current.Version) > 0 {
			byKey[full] = row
		}
	}
	out := make([]Profile, 0, len(byKey))
	for _, row := range byKey {
		out = append(out, row)
	}
	return out
}

func compareVersion(a, b string) int {
	ai, aerr := strconv.Atoi(strings.TrimSpace(a))
	bi, berr := strconv.Atoi(strings.TrimSpace(b))
	switch {
	case aerr == nil && berr == nil:
		return ai - bi
	case a == b:
		return 0
	case a < b:
		return -1
	default:
		return 1
	}
}

func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
