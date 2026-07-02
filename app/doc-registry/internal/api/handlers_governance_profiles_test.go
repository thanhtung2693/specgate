package api

import (
	"context"
	"testing"

	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
	"github.com/specgate/doc-registry/internal/workboard"
)

func testHandlersGovernanceProfiles(t *testing.T) (*Handlers, func()) {
	t.Helper()
	db := newTestGormDB(t)
	svc := governanceprofile.NewService(storagedb.NewGovernanceProfileRepository(db))
	h := &Handlers{GovernanceProfiles: svc}
	cleanup := func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}
	return h, cleanup
}

func TestListGovernanceProfiles_ReturnsBuiltinsWithEffectivePolicies(t *testing.T) {
	t.Parallel()
	h, cleanup := testHandlersGovernanceProfiles(t)
	defer cleanup()
	ctx := context.Background()

	out, err := h.ListGovernanceProfiles(ctx, &struct{}{})
	if err != nil {
		t.Fatalf("ListGovernanceProfiles: %v", err)
	}

	byKey := map[string]GovernanceProfileDTO{}
	for _, p := range out.Body.Items {
		byKey[p.Key] = p
	}

	// All five built-ins are present even against an empty repo.
	for _, key := range []string{"generic_change", "high_impact_feature", "bug_fix", "adr", "research_spike"} {
		if _, ok := byKey[key]; !ok {
			t.Errorf("builtin %q missing from response", key)
		}
	}

	// The catalog surfaces the EFFECTIVE approval/evidence policy (never empty),
	// so the UI does not have to re-derive the safe default.
	hi := byKey["high_impact_feature"]
	if hi.ApprovalPolicy != "human_required" {
		t.Errorf("high_impact_feature approval_policy = %q, want human_required", hi.ApprovalPolicy)
	}
	if hi.EvidencePolicy != "corroborated_required" {
		t.Errorf("high_impact_feature evidence_policy = %q, want corroborated_required", hi.EvidencePolicy)
	}
	bug := byKey["bug_fix"]
	if bug.ApprovalPolicy != "self_approve" {
		t.Errorf("bug_fix approval_policy = %q, want self_approve", bug.ApprovalPolicy)
	}
	if bug.EvidencePolicy != "attested_ok" {
		t.Errorf("bug_fix evidence_policy = %q, want attested_ok", bug.EvidencePolicy)
	}

	// Source + structural fields ride through for the catalog view.
	if hi.Source != "builtin" {
		t.Errorf("high_impact_feature source = %q, want builtin", hi.Source)
	}
	if len(hi.RequiredRoles) == 0 || len(hi.EnabledGates) == 0 {
		t.Errorf("high_impact_feature should carry required_roles + enabled_gates, got roles=%v gates=%v", hi.RequiredRoles, hi.EnabledGates)
	}
	if hi.Digest == "" {
		t.Error("high_impact_feature digest should be populated")
	}
}

func TestListGovernanceProfiles_ImportedProfileSurfacesImportSource(t *testing.T) {
	t.Parallel()
	h, cleanup := testHandlersGovernanceProfiles(t)
	defer cleanup()
	ctx := context.Background()

	// Import a team profile, then confirm it serialises with source="import" —
	// the catalog's built-in-vs-imported distinction depends on this exact value.
	if _, err := h.GovernanceProfiles.ImportProfiles(ctx, []governanceprofile.ImportInput{{
		Namespace:   "acme",
		Key:         "security_change",
		Version:     "1",
		DisplayName: "Security change",
		ChangeType:  "security_change",
		Definition: governanceprofile.Definition{
			DisplayName: "Security change",
			ChangeType:  "security_change",
		},
	}}); err != nil {
		t.Fatalf("ImportProfiles: %v", err)
	}

	out, err := h.ListGovernanceProfiles(ctx, &struct{}{})
	if err != nil {
		t.Fatalf("ListGovernanceProfiles: %v", err)
	}

	var found *GovernanceProfileDTO
	for i := range out.Body.Items {
		if out.Body.Items[i].Key == "security_change" {
			found = &out.Body.Items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("imported profile security_change missing from response")
	}
	if found.Source != "import" {
		t.Errorf("imported profile source = %q, want import", found.Source)
	}
	// Effective defaults apply to imports that omit policy fields.
	if found.ApprovalPolicy != "human_required" {
		t.Errorf("imported profile approval_policy = %q, want human_required (default)", found.ApprovalPolicy)
	}
	if found.EvidencePolicy != "attested_ok" {
		t.Errorf("imported profile evidence_policy = %q, want attested_ok (default)", found.EvidencePolicy)
	}
}

func TestListGovernanceProfiles_NotConfigured(t *testing.T) {
	t.Parallel()
	h := &Handlers{} // no GovernanceProfiles service wired
	_, err := h.ListGovernanceProfiles(context.Background(), &struct{}{})
	if err == nil {
		t.Fatal("expected error when governance profiles service is not configured")
	}
}

// --- Policy endpoint tests ---

func TestCLIListPolicyLevels_ReturnsThreeLevels(t *testing.T) {
	t.Parallel()
	h, cleanup := testHandlersGovernanceProfiles(t)
	defer cleanup()

	out, err := h.CLIListPolicyLevels(context.Background(), &struct{}{})
	if err != nil {
		t.Fatalf("CLIListPolicyLevels: %v", err)
	}
	if len(out.Body.Levels) != 3 {
		t.Fatalf("levels count = %d, want 3", len(out.Body.Levels))
	}
	byLevel := map[string]CLIPolicyLevelDTO{}
	for _, l := range out.Body.Levels {
		byLevel[string(l.GovernanceLevel)] = l
	}
	for _, lv := range []string{"light", "standard", "enhanced"} {
		if _, ok := byLevel[lv]; !ok {
			t.Errorf("level %q missing from response", lv)
		}
	}
	// Spot-check enhanced governance fields are non-empty.
	enh := byLevel["enhanced"]
	if enh.DisplayName == "" {
		t.Error("enhanced DisplayName is empty")
	}
	if enh.ApprovalPolicy == "" {
		t.Error("enhanced ApprovalPolicy is empty")
	}
	if len(enh.EnabledGates) == 0 {
		t.Error("enhanced EnabledGates is empty")
	}
}

func TestCLIWorkItemPolicy_QuickRouteWithoutLeadArtifact(t *testing.T) {
	t.Parallel()
	governance := &cliTestWorkBoard{
		crs: []workboard.ChangeRequest{{
			ID:                    "cr-quick",
			Key:                   "CR-QUICK",
			FeatureID:             "feat-quick",
			Title:                 "Quick bug fix",
			ContextPackArtifactID: "pack-1",
			AcceptanceCriteria:    `["Done"]`,
		}},
		features: map[string]*workboard.Feature{
			"feat-quick": {ID: "feat-quick", Key: "FEAT-QUICK"},
		},
	}
	h := &Handlers{
		Governance: &governanceops.Service{WorkBoard: governance},
	}

	out, err := h.CLIWorkItemPolicy(context.Background(), &CLIWorkItemPolicyInput{ID: "CR-QUICK"})
	if err != nil {
		t.Fatalf("CLIWorkItemPolicy: %v", err)
	}
	if out.Body.GovernanceLevel != governanceprofile.GovernanceStandard {
		t.Fatalf("governance level = %q, want standard", out.Body.GovernanceLevel)
	}
	if out.Body.Title != "Quick-route governance" {
		t.Fatalf("title = %q", out.Body.Title)
	}
	if len(out.Body.Obligations) == 0 {
		t.Fatal("expected quick-route obligations")
	}
}

func TestCLIResolvePolicy_LowRiskBugfix(t *testing.T) {
	t.Parallel()
	h, cleanup := testHandlersGovernanceProfiles(t)
	defer cleanup()

	out, err := h.CLIResolvePolicy(context.Background(), &CLIResolvePolicyInput{
		Body: CLIResolvePolicyBody{
			RequestType: "bugfix",
			ImpactLevel: "low",
		},
	})
	if err != nil {
		t.Fatalf("CLIResolvePolicy: %v", err)
	}
	if out.Body.GovernanceLevel != governanceprofile.GovernanceLight {
		t.Fatalf("governance_level = %q, want light", out.Body.GovernanceLevel)
	}
	if out.Body.Title == "" {
		t.Error("Title is empty")
	}
}

func TestCLIResolvePolicy_HighImpact(t *testing.T) {
	t.Parallel()
	h, cleanup := testHandlersGovernanceProfiles(t)
	defer cleanup()

	out, err := h.CLIResolvePolicy(context.Background(), &CLIResolvePolicyInput{
		Body: CLIResolvePolicyBody{
			RequestType: "new_feature",
			ImpactLevel: "high",
		},
	})
	if err != nil {
		t.Fatalf("CLIResolvePolicy: %v", err)
	}
	if out.Body.GovernanceLevel != governanceprofile.GovernanceEnhanced {
		t.Fatalf("governance_level = %q, want enhanced", out.Body.GovernanceLevel)
	}
	// Enhanced should have at least one reason.
	if len(out.Body.Reasons) == 0 {
		t.Error("expected non-empty Reasons for high-impact change")
	}
}

func TestCLIArtifactPolicy_NoSnapshot(t *testing.T) {
	t.Parallel()
	h, cleanup := testHandlersGovernanceProfiles(t)
	defer cleanup()

	// Handlers.Artifacts not wired → 503.
	_, err := h.CLIArtifactPolicy(context.Background(), &CLIArtifactPolicyInput{ID: "art-1"})
	if err == nil {
		t.Fatal("expected error when Artifacts service is not configured")
	}
}
