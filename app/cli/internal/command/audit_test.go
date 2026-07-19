package command_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func sampleAuditTrail() *client.AuditTrail {
	return &client.AuditTrail{
		Ref:              "CR-1",
		ChangeRequestID:  "cr-1",
		ChangeRequestKey: "CR-1",
		FeatureKey:       "FEAT-1",
		FeatureName:      "Audit",
		Title:            "Add audit trail",
		Phase:            "Handoff",
		Events: []client.AuditEvent{
			{Timestamp: "2026-07-09T08:00:00Z", Actor: "dave", ActorKind: "human", Action: "published", Subject: "art-lead", Detail: "v1"},
			{Timestamp: "2026-07-09T09:00:00Z", Actor: "ide_agent", ActorKind: "agent", Action: "gate:spec_repo_drift", Verdict: "pass", Trust: "agent_attested", Detail: "no drift"},
			{Timestamp: "2026-07-09T13:00:00Z", Actor: "carol", ActorKind: "human", Action: "delivery_review", Verdict: "pass", Trust: "human", Detail: "looks good"},
		},
	}
}

func TestAuditRendersTimeline(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.auditResult = sampleAuditTrail()

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "audit", "CR-1")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastAuditRef != "CR-1" {
		t.Fatalf("audit ref = %q, want CR-1", fc.lastAuditRef)
	}
	got := out.String()
	for _, want := range []string{"published", "gate:spec_repo_drift", "Agent-reported", "Ready for human review", "delivery_review", "looks good", "Audit"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "agent_attested") || strings.Contains(got, "needs_human_review") {
		t.Fatalf("plain audit leaked machine enums:\n%s", got)
	}
}

func TestAuditJSONOutput(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.auditResult = sampleAuditTrail()

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "audit", "CR-1")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		Data client.AuditTrail `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal %v — output: %s", err, out.String())
	}
	if len(env.Data.Events) != 3 || env.Data.ChangeRequestKey != "CR-1" {
		t.Fatalf("json trail wrong: %+v", env.Data)
	}
}

func TestAuditVerifyRendersChainState(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	trail := sampleAuditTrail()
	trail.Chain = &client.ChainReport{State: "tampered", ArtifactID: "art-1", FirstBadEventID: "ev-9"}
	fc.auditResult = trail
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "audit", "CR-1", "--verify")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, want 0; out: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "TAMPERED at event ev-9") {
		t.Fatalf("chain verdict missing: %s", out.String())
	}
}

func TestAuditWithoutVerifyOmitsChainLine(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.auditResult = sampleAuditTrail() // no Chain set (server omits without ?verify)
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "audit", "CR-1")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	if strings.Contains(out.String(), "Chain:") {
		t.Fatalf("chain line must not render without --verify: %s", out.String())
	}
}
