package command_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// writeTempJSON writes data as JSON to a temp file and returns the path.
func writeTempJSON(t *testing.T, data any) string {
	t.Helper()
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(f, b, 0644); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestArtifactShowLocalJSONOmitsDocumentContent(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	stateDir, store, _, work := newLocalChangeWork(t, deps)
	closeLocalChangeStore(t, deps, stateDir, store)

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "show", work.ArtifactID)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if _, found := envelope.Data["documents"]; found || strings.Contains(out.String(), "# Change") {
		t.Fatalf("artifact show leaked document content; use work context to inspect a pinned document: %s", out.String())
	}
}

func TestArtifactPublishPreviewUsesEmbeddedLocalPolicyWithoutAPI(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	if err := (config.Config{Mode: config.ModeLocal}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	packagePath := writeTempJSON(t, map[string]any{
		"feature_key": "feat-local",
		"documents": []map[string]any{
			{"path": "spec.md", "role": "spec", "content": "# Spec"},
		},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--preview", "--file", packagePath)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	for _, want := range []string{
		`"governance_level":"standard"`,
		`"reason_codes":["local_fixed_standard"]`,
		`"source":"embedded_default"`,
		`"missing_roles":["plan"]`,
		`"omitted":[]`,
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("local preview missing %s: %s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "Impact declaration missing") {
		t.Fatalf("Local fixed policy requested unused impact answers: %s", out.String())
	}
}

func TestArtifactPublishPreviewLocalValidatesAndShowsSourceCriteria(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	if err := (config.Config{Mode: config.ModeLocal}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	valid := writeTempJSON(t, map[string]any{
		"feature_key":     "feat-local-source-criteria",
		"documents":       []map[string]any{{"path": "spec.md", "role": "spec", "content": "# Spec"}},
		"source_criteria": []map[string]any{{"id": "req-later", "text": "Export CSV", "source_path": "spec.md", "deferred_reason": "Out of scope"}},
	})
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--preview", "--file", valid); code != output.ExitOK {
		t.Fatalf("valid local preview exit = %d; output = %s", code, out.String())
	}
	for _, want := range []string{`"source_criteria":[{`, `"id":"req-later"`, `"deferred_reason":"Out of scope"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("local preview missing %s: %s", want, out.String())
		}
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "artifact", "publish", "--preview", "--file", valid); code != output.ExitOK {
		t.Fatalf("plain local preview exit = %d; output = %s", code, out.String())
	}
	for _, want := range []string{"Source criterion", "req-later", "Export CSV", "deferred: Out of scope"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("plain local preview missing %s: %s", want, out.String())
		}
	}

	out.Reset()
	invalid := writeTempJSON(t, map[string]any{
		"feature_key":     "feat-local-source-criteria",
		"documents":       []map[string]any{{"path": "spec.md", "role": "spec", "content": "# Spec"}},
		"source_criteria": []map[string]any{{"id": "req 1", "text": "Unmappable", "source_path": "spec.md"}},
	})
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--preview", "--file", invalid); code != output.ExitUsage {
		t.Fatalf("invalid local preview exit = %d; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "source criterion id") || strings.Contains(out.String(), "human_confirmation_required") {
		t.Fatalf("invalid local preview did not fail before output: %s", out.String())
	}
}

func TestArtifactPublishPreviewRejectsDocumentWithoutContentSource(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	packagePath := writeTempJSON(t, map[string]any{
		"feature_key": "feat-x",
		"documents": []map[string]any{{
			"path": "docs/spec.md", "role": "spec",
		}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--preview", "--file", packagePath)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	for _, want := range []string{"documents[0]", "content", "source_file", "file_url"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing actionable source error %q: %s", want, out.String())
		}
	}
}

func TestArtifactPublishPreviewRejectsUnknownPackageField(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	packagePath := writeTempJSON(t, map[string]any{
		"feature_key": "feat-x",
		"non_goals":   []string{"silently ignored before validation"},
		"documents": []map[string]any{{
			"path": "spec.md", "role": "spec", "content": "# Spec",
		}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--preview", "--file", packagePath)

	if code == output.ExitOK {
		t.Fatalf("unknown package field was silently ignored: %s", out.String())
	}
	if !strings.Contains(out.String(), "unknown artifact package field") || !strings.Contains(out.String(), "non_goals") {
		t.Fatalf("missing actionable unknown-field error: %s", out.String())
	}
}

func TestArtifactPublishPreviewRejectsUnknownRequestType(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	packagePath := writeTempJSON(t, map[string]any{
		"feature_key":  "feat-x",
		"request_type": "feature",
		"documents": []map[string]any{{
			"path": "spec.md", "role": "spec", "content": "# Spec",
		}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--preview", "--file", packagePath)

	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
	}
	for _, want := range []string{"request_type", "new_feature", "change_request", "bugfix", "unknown"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing allowed request type %q: %s", want, out.String())
		}
	}
}

func TestArtifactPublishCompareRequiresPreview(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	packagePath := writeTempJSON(t, map[string]any{
		"documents": []map[string]any{{"path": "spec.md", "role": "spec", "content": "# Spec"}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", packagePath, "--compare", "art-base")
	if code == output.ExitOK {
		t.Fatalf("exit = 0, want usage error: %s", out.String())
	}
	if !strings.Contains(out.String(), "--compare requires --preview") {
		t.Fatalf("output missing compare/preview error: %s", out.String())
	}
}

func TestArtifactPublishRejectsWorkTypeAlias(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	f := writeTempJSON(t, map[string]any{
		"feature_key": "feat-x",
		"work_type":   "new_feature",
		"documents":   []map[string]any{{"path": "spec.md", "content": "# Spec"}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", f)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want validation; output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "unknown artifact package field") ||
		!strings.Contains(out.String(), "work_type") {
		t.Fatalf("output = %s", out.String())
	}
}

func TestArtifactPublishRejectsVersionFieldBeforeHTTP(t *testing.T) {
	t.Parallel()
	deps, out := newTestDeps(t)
	f := writeTempJSON(t, map[string]any{
		"feature_key": "feat-x",
		"version":     "v0.1",
		"documents":   []map[string]any{{"path": "spec.md", "content": "# Spec"}},
	})

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", f)
	if code != output.ExitUsage {
		t.Fatalf("exit = %d, want %d; output = %s", code, output.ExitUsage, out.String())
	}
	if !strings.Contains(out.String(), "version is assigned by SpecGate") {
		t.Fatalf("output missing version guidance: %s", out.String())
	}
}
