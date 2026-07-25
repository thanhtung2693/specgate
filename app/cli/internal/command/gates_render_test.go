package command_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// `Spec quality: warn` is not a judgeable readout. A human approving a contract
// has to see which gates ran, which did not, and why — the same facts the JSON
// already carries.
func TestGatesCheckNamesEachGateStateAndPendingCount(t *testing.T) {
	deps, _, _, out := newFakeDeps(t)
	stateDir := t.TempDir()
	store, err := local.Open(filepath.Join(stateDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	selection, err := store.Initialize(t.Context(), local.InitInput{
		WorkspaceName: "Local workspace", DisplayName: "Local developer", Username: "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.PublishArtifact(t.Context(), selection.Workspace.ID, local.ArtifactInput{
		FeatureKey: "GATES", RequestType: "new_feature",
		Documents: []local.ArtifactDocumentInput{{
			Path: "spec.md", Role: "spec", Content: []byte("# Spec\n\n## Acceptance criteria\n\n1. Works."),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: stateDir}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "gates", "check", artifact.ID)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	rendered := out.String()

	// The deterministic gates that did run must be named with their state.
	for _, want := range []string{"has_documents", "has_spec"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("readout omits gate %q:\n%s", want, rendered)
		}
	}
	// A semantic gate awaiting the IDE agent must be visible as not_run, not
	// hidden inside an aggregate word.
	if !strings.Contains(rendered, "not_run") {
		t.Fatalf("readout hides gates awaiting a result:\n%s", rendered)
	}
	if !strings.Contains(rendered, "spec_completeness") {
		t.Fatalf("readout does not name the pending semantic gates:\n%s", rendered)
	}
	// A dispatched task carries the trust tier its future result will have. Showing
	// it while the gate is still not_run would claim an attestation nobody made.
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "not_run") && strings.Contains(line, "attested") {
			t.Fatalf("unanswered gate claims a trust tier: %q", line)
		}
	}
}
