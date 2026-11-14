package command_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestLocalWorkListByPhaseIncludesActionableStatus(t *testing.T) {
	deps, out := newTestDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "list", "--phase", "ready")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		Data struct {
			Items []struct {
				Key         string `json:"key"`
				ChangeState string `json:"change_state"`
				NextActor   string `json:"next_actor"`
				NextCommand string `json:"next_command"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v; out=%s", err, out.String())
	}
	if len(env.Data.Items) != 1 {
		t.Fatalf("items = %+v", env.Data.Items)
	}
	if got := env.Data.Items[0]; got.Key != work.Key || got.ChangeState != "implementation" || got.NextActor != "implementing_agent" || got.NextCommand == "" {
		t.Fatalf("item = %+v", got)
	}
}

func TestWorkShowNoInputRequiresRef(t *testing.T) {
	t.Parallel()
	deps, _ := newTestDeps(t)

	cmd := command.NewRootCommand(deps)
	cmd.SetArgs([]string{"--no-input", "work", "show"})
	err := cmd.Execute()
	if !errors.Is(err, command.ErrWorkRefRequired) {
		t.Fatalf("error = %v, want ErrWorkRefRequired", err)
	}
}

func TestWorkCreateQuickLocalInvalidInputIsUsage(t *testing.T) {
	for _, args := range [][]string{
		{"Fix crash"},
		{"Fix crash", "--ac", " "},
		{"Fix crash", "--ac", "Login works @check:unit @check:integration"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			deps, out := newTestDeps(t)
			stateDir := filepath.Join(t.TempDir(), "uninitialized")
			if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(deps.ConfigPath); err != nil {
				t.Fatal(err)
			}
			code := command.ExecuteForCode(command.NewRootCommand(deps), append([]string{"--json", "work", "create-quick"}, args...)...)
			if code != output.ExitUsage || !strings.Contains(out.String(), `"code":"validation"`) {
				t.Fatalf("exit = %d, output = %s", code, out.String())
			}
			if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
				t.Fatalf("invalid input created state: %v", err)
			}
		})
	}
}

func TestWorkCreateQuickRunsEntirelyInLocalMode(t *testing.T) {
	deps, out := newTestDeps(t)
	stateDir := t.TempDir()
	store, err := local.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Initialize(t.Context(), local.InitInput{
		WorkspaceName: "Local workspace", DisplayName: "Local developer", Username: "local",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "work", "create-quick", "Fix crash",
		"--description", "Prevent login crash",
		"--ac", "Login succeeds @check:unit",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var envelope struct {
		Data struct {
			Key             string `json:"change_request_key"`
			LeadArtifactID  string `json:"lead_artifact_id"`
			AcceptanceCount int    `json:"acceptance_count"`
			BoundCount      int    `json:"acceptance_criteria_bound"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Key == "" || envelope.Data.LeadArtifactID != "" || envelope.Data.AcceptanceCount != 1 {
		t.Fatalf("result = %#v, output = %s", envelope.Data, out.String())
	}
	// The IDE agent runs this command in machine mode, so the enforcement signal
	// has to reach the envelope; a human-only line would leave the default path
	// silent about which criteria review can actually enforce.
	if envelope.Data.BoundCount != 1 {
		t.Fatalf("bound criteria count = %d, want 1; output = %s", envelope.Data.BoundCount, out.String())
	}
}

// TestWorkCreateQuickLocalEnvelopeCountsUnboundCriteria pins the case the count
// exists for: a criterion with no resolvable check is reported as unenforced
// rather than folded into the total.
func TestWorkCreateQuickLocalEnvelopeCountsUnboundCriteria(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir := t.TempDir()
	store, err := local.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Initialize(t.Context(), local.InitInput{
		WorkspaceName: "Local workspace", DisplayName: "Local developer", Username: "local",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(
		command.NewRootCommand(deps),
		"--json", "work", "create-quick", "Fix crash",
		"--ac", "Login succeeds @check:unit",
		"--ac", "Error message is friendly",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var envelope struct {
		Data struct {
			AcceptanceCount int `json:"acceptance_count"`
			BoundCount      int `json:"acceptance_criteria_bound"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.AcceptanceCount != 2 || envelope.Data.BoundCount != 1 {
		t.Fatalf("counts = %#v, want 2 criteria with 1 bound; output = %s", envelope.Data, out.String())
	}
}

func TestWorkCreateQuickNoInputRequiresFile(t *testing.T) {
	t.Parallel()
	deps, _ := newTestDeps(t)

	cmd := command.NewRootCommand(deps)
	cmd.SetArgs([]string{"--no-input", "work", "create-quick"})
	err := cmd.Execute()
	if !errors.Is(err, command.ErrInputRequired) {
		t.Fatalf("error = %v, want ErrInputRequired", err)
	}
}

func TestWorkCreateQuickTitleAndFileConflict(t *testing.T) {
	t.Parallel()
	deps, _ := newTestDeps(t)
	f := filepath.Join(t.TempDir(), "issue.json")
	if err := os.WriteFile(f, []byte(`{"title":"From file"}`), 0644); err != nil {
		t.Fatal(err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain",
		"work", "create-quick", "Arg title", "--file", f)
	if code == output.ExitOK {
		t.Fatal("expected non-zero exit when both a title argument and --file are given")
	}
}
