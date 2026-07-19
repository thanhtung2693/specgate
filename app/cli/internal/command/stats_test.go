package command_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func sampleStats() *client.StatsResult {
	return &client.StatsResult{
		WindowDays:             30,
		ReviewedItems:          4,
		FirstPass:              3,
		GateCatchesPreBuild:    2,
		ReviewCatchesPostBuild: 3,
		ReviewCatchesFixed:     3,
		Rework:                 4,
		ItemsWithRework:        2,
		AmbiguityBlocks:        1,
		CycleTimeAvgHours:      6.2,
		CycleTimeItems:         3,
		Ledger: []client.StatsLedgerEntry{
			{
				OccurredAt:       "2026-07-02T08:39:00Z",
				ChangeRequestKey: "CR-EB7FC5BB",
				Kind:             "review_catch",
				Gate:             "delivery_review",
				Detail:           "needs human review — evidence missing",
			},
		},
	}
}

func TestStatsRendersGovernanceValueReadout(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	err := (config.Config{
		Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "specgate"},
	}).SaveTo(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	fc.statsResult = sampleStats()

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "stats")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastStatsWorkspaceID != "ws-1" {
		t.Fatalf("stats workspace = %q, want ws-1", fc.lastStatsWorkspaceID)
	}
	if fc.lastStatsDays != 30 {
		t.Fatalf("stats days = %d, want 30", fc.lastStatsDays)
	}
	got := out.String()
	for _, want := range []string{
		`SpecGate Stats (last 30 days · workspace "specgate")`,
		"4 work items",
		"75% (3/4 passed first review)",
		"2 pre-build gate signals before handoff",
		"3 post-build review signals (3 later passed review)",
		"4 resubmits across 2 items",
		"1 blocked-ambiguity report",
		"avg 6.2h create → pass (3 items)",
		"Governance signals (recent)",
		"CR-EB7FC5BB",
		"review_signal",
		"delivery_review: needs human review — evidence missing",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	for _, banned := range []string{
		"Caught by SpecGate",
		"Caught pre-build",
		"Caught post-build",
		"Ambiguity saves",
		"review_catch",
	} {
		if strings.Contains(got, banned) {
			t.Errorf("output must not claim %q:\n%s", banned, got)
		}
	}
}

func TestStatsAllWorkspacesClearsFilterAndForwardsDays(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	err := (config.Config{
		Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "specgate"},
	}).SaveTo(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	fc.statsResult = sampleStats()

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "stats", "--all-workspaces", "--days", "7")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastStatsWorkspaceID != "" {
		t.Fatalf("stats workspace = %q, want empty for --all-workspaces", fc.lastStatsWorkspaceID)
	}
	if fc.lastStatsDays != 7 {
		t.Fatalf("stats days = %d, want 7", fc.lastStatsDays)
	}
	if !strings.Contains(out.String(), "all workspaces") {
		t.Fatalf("output missing all-workspaces scope:\n%s", out.String())
	}
}

func TestStatsWorkspaceOverrideScopeMatchesQuery(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	err := (config.Config{
		Workspace: config.CurrentWorkspace{ID: "ws-global", Slug: "global"},
	}).SaveTo(deps.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	fc.workspaces = []client.IdentityWorkspace{{ID: "ws-platform", Slug: "platform", Name: "Platform"}}
	fc.statsResult = sampleStats()

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--workspace", "platform", "stats")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastStatsWorkspaceID != "ws-platform" {
		t.Fatalf("stats workspace = %q, want ws-platform", fc.lastStatsWorkspaceID)
	}
	if !strings.Contains(out.String(), `workspace "platform"`) {
		t.Fatalf("output missing override scope:\n%s", out.String())
	}
}

func TestStatsWithoutWorkspaceRequiresExplicitAllWorkspaces(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	fc.statsResult = sampleStats()

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "stats")
	if code == output.ExitOK {
		t.Fatalf("stats unexpectedly succeeded without workspace: %s", out.String())
	}
	if !strings.Contains(out.String(), "select a workspace first") || !strings.Contains(out.String(), "--all-workspaces") {
		t.Fatalf("output missing scope guidance:\n%s", out.String())
	}
}

func TestStatsHonestEmptyState(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.statsResult = &client.StatsResult{WindowDays: 30}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "stats", "--all-workspaces")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Not enough data yet — run a few governed work items first.") {
		t.Fatalf("output missing honesty line:\n%s", got)
	}
	for _, banned := range []string{"0%", "100%", "First-pass", "Rework", "Cycle time"} {
		if strings.Contains(got, banned) {
			t.Errorf("empty state must not render %q:\n%s", banned, got)
		}
	}
}

func TestStatsJSONEmitsRawPayloadEnvelope(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.statsResult = sampleStats()

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "stats", "--all-workspaces")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK   bool               `json:"ok"`
		Data client.StatsResult `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v, output: %s", err, out.String())
	}
	if !env.OK {
		t.Fatalf("ok = false, output: %s", out.String())
	}
	if env.Data.ReviewedItems != 4 || env.Data.Rework != 4 || len(env.Data.Ledger) != 1 {
		t.Fatalf("data = %#v, want raw server payload", env.Data)
	}
}
