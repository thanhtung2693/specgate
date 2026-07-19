package command_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// --- policy ---

func TestPolicyListPlain(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.governanceLevels = []client.GovernanceLevel{
		{GovernanceLevel: "light", DisplayName: "Light governance", ApprovalPolicy: "human_required", EvidencePolicy: "attested_ok"},
		{GovernanceLevel: "standard", DisplayName: "Standard governance", ApprovalPolicy: "human_required", EvidencePolicy: "attested_ok"},
		{GovernanceLevel: "enhanced", DisplayName: "Enhanced governance", ApprovalPolicy: "human_required", EvidencePolicy: "corroborated_required"},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "policy")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "light") {
		t.Errorf("output missing light tier:\n%s", got)
	}
	if !strings.Contains(got, "standard") {
		t.Errorf("output missing standard tier:\n%s", got)
	}
	if !strings.Contains(got, "enhanced") {
		t.Errorf("output missing enhanced tier:\n%s", got)
	}
}

func TestPolicyListJSON(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "policy")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Levels []map[string]any `json:"levels"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v, output: %s", err, out.String())
	}
	if !env.OK {
		t.Fatalf("ok = false: %s", out.String())
	}
}

func TestPolicyListEmpty(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.governanceLevels = []client.GovernanceLevel{}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "policy")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "No governance levels found.") {
		t.Errorf("output missing empty message:\n%s", out.String())
	}
}

func TestPolicyListSubcommandIsRemoved(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "policy", "list")
	if code == output.ExitOK {
		t.Fatalf("removed policy list path still succeeds: %s", out.String())
	}
}

// --- work policy ---

func TestWorkPolicyPlain(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.policyExplanation = &client.PolicyExplanation{
		GovernanceLevel: "standard",
		Title:           "Standard governance",
		Summary:         "Human approval required; agent attestation accepted.",
		Reasons:         []string{"Default standard governance applies"},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "work", "policy", "CR-101")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Standard governance") {
		t.Errorf("output missing title:\n%s", got)
	}
	if fc.lastPolicyRef != "cr-1" {
		t.Fatalf("lastPolicyRef = %q, want cr-1", fc.lastPolicyRef)
	}
}
