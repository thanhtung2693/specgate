package command_test

import (
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestOpenDeepLinkRequiresAdvertisedWebURL(t *testing.T) {
	t.Parallel()
	// A server that advertises no web_url: a deep link must error clearly
	// instead of pointing the browser at the API server.
	deps, fc, _, out := newFakeDeps(t)
	fc.metaResult = &client.Meta{
		APIVersion: "specgate.api/v1",
		CapabilityDetails: map[string]client.CapabilityDetail{
			"agents": {State: "available"},
		},
	}
	opened := ""
	deps.Opener = func(u string) error { opened = u; return nil }

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "open", "CR-1")

	if code == output.ExitOK {
		t.Fatalf("deep link without web_url should fail, opened %q: %s", opened, out.String())
	}
	if opened != "" {
		t.Fatalf("browser should not open, got %q", opened)
	}
	if !strings.Contains(out.String(), "web UI URL") {
		t.Fatalf("error should explain the missing web UI URL: %s", out.String())
	}
}

func TestOpenWithoutTargetUsesConfiguredServerURL(t *testing.T) {
	t.Parallel()
	deps, _, _, out := newFakeDeps(t)
	opened := ""
	deps.Opener = func(u string) error { opened = u; return nil }

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "open")

	if code != output.ExitOK {
		t.Fatalf("bare open should keep working: %s", out.String())
	}
	if opened == "" {
		t.Fatalf("bare open should still open the configured URL")
	}
}
