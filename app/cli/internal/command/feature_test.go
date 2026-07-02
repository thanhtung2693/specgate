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

func TestFeatureListSearchForwarded(t *testing.T) {
	t.Parallel()
	deps, fc, _, _ := newFakeDeps(t)
	command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "feature", "list", "--search", "checkout") //nolint:errcheck
	if fc.lastFeatureSearch != "checkout" {
		t.Fatalf("lastFeatureSearch = %q, want checkout", fc.lastFeatureSearch)
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
