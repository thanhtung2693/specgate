package command_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestFeatureListPlain(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.featuresResult = []client.Feature{
		{ID: "f-1", Key: "checkout-redesign", Name: "Checkout redesign", Status: "active", Version: 3},
		{ID: "f-2", Key: "login-2fa", Name: "Login 2FA", Status: "candidate", Version: 1},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "feature", "list")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "checkout-redesign") || !strings.Contains(got, "login-2fa") {
		t.Errorf("output missing feature keys:\n%s", got)
	}
}

func TestFeatureListUsesSemanticColorOnlyOnCapableTerminal(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	deps, fc, _, out := newFakeDeps(t)
	deps.StdoutIsTTY = func() bool { return true }
	fc.featuresResult = []client.Feature{{ID: "f-1", Key: "checkout-redesign", Name: "Checkout redesign", Status: "active"}}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "feature", "list")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("rich feature list has no ANSI styling: %q", out.String())
	}

	out.Reset()
	deps.StdoutIsTTY = func() bool { return false }
	code = command.ExecuteForCode(command.NewRootCommand(deps), "feature", "list")
	if code != output.ExitOK {
		t.Fatalf("portable exit = %d, output = %s", code, out.String())
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("portable feature list contains ANSI styling: %q", out.String())
	}
}

func TestFeatureListSearchForwarded(t *testing.T) {
	t.Parallel()
	deps, fc, _, _ := newFakeDeps(t)
	command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "feature", "list", "--search", "checkout") //nolint:errcheck
	if fc.lastFeatureSearch != "checkout" {
		t.Fatalf("lastFeatureSearch = %q, want checkout", fc.lastFeatureSearch)
	}
}

func TestFeatureListHidesArchivedByDefault(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.featuresResult = []client.Feature{
		{ID: "f-1", Key: "checkout-redesign", Name: "Checkout redesign", Status: "active"},
		{ID: "f-2", Key: "legacy-flow", Name: "Legacy flow", Status: "archived"},
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "feature", "list")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if strings.Contains(out.String(), "legacy-flow") {
		t.Errorf("default list should hide archived features:\n%s", out.String())
	}

	deps2, fc2, _, out2 := newFakeDeps(t)
	fc2.featuresResult = fc.featuresResult
	code = command.ExecuteForCode(command.NewRootCommand(deps2), "--plain", "feature", "list", "--all")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out2.String())
	}
	if !strings.Contains(out2.String(), "legacy-flow") {
		t.Errorf("--all should include archived features:\n%s", out2.String())
	}
}

func TestFeatureArchive(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.featureResult = &client.Feature{ID: "f-2", Key: "legacy-flow", Name: "Legacy flow", Status: "active"}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "--yes", "feature", "archive", "legacy-flow")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastFeatureUpdateID != "f-2" {
		t.Fatalf("update id = %q, want f-2", fc.lastFeatureUpdateID)
	}
	if fc.lastFeatureUpdateStatus != "archived" {
		t.Fatalf("update status = %q, want archived", fc.lastFeatureUpdateStatus)
	}
	if !strings.Contains(out.String(), "Archived legacy-flow") {
		t.Fatalf("output = %q, want archive confirmation", out.String())
	}
}

func TestFeatureArchiveCancelledPrompt(t *testing.T) {
	t.Parallel()
	deps, fc, fp, out := newFakeDeps(t)
	fc.featureResult = &client.Feature{ID: "f-2", Key: "legacy-flow", Name: "Legacy flow", Status: "active"}
	deps.StdinIsTTY = func() bool { return true }
	fp.confirmValue = false
	code := command.ExecuteForCode(command.NewRootCommand(deps), "feature", "archive", "legacy-flow")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastFeatureUpdateID != "" {
		t.Fatal("archive must not run after cancelled prompt")
	}
}

func TestFeatureListJSON(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.featuresResult = []client.Feature{{ID: "f-1", Key: "checkout-redesign", Name: "Checkout redesign", Status: "active"}}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "feature", "list")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Items []client.Feature `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.OK || len(env.Data.Items) != 1 || env.Data.Items[0].Key != "checkout-redesign" {
		t.Fatalf("unexpected envelope: %s", out.String())
	}
}

func TestFeatureShowByKey(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.featureResult = &client.Feature{ID: "f-1", Key: "checkout-redesign", Name: "Checkout redesign", Status: "active", Version: 3}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "feature", "show", "checkout-redesign")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastFeatureRef != "checkout-redesign" {
		t.Errorf("lastFeatureRef = %q, want checkout-redesign", fc.lastFeatureRef)
	}
	if !strings.Contains(out.String(), "checkout-redesign") {
		t.Errorf("output missing feature key:\n%s", out.String())
	}
}
