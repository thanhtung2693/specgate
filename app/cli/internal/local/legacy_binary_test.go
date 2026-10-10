package local

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Release gate: provide a CLI built from the last pre-enhancement revision.
// No user config, home, repository or database is accessed by the subprocess.
func TestLegacyBinaryCannotMutateEnhancedStore(t *testing.T) {
	binary := os.Getenv("SPECGATE_LEGACY_CLI")
	if binary == "" {
		t.Skip("set SPECGATE_LEGACY_CLI to a pre-enhancement CLI binary")
	}
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sel, err := s.Initialize(t.Context(), InitInput{WorkspaceName: "Legacy guard", Username: "human", DisplayName: "Human"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := s.CreateQuickWork(t.Context(), sel.Workspace.ID, QuickWorkInput{Title: "Guarded work", AcceptanceCriteria: []string{"Works"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCheckpointWithFiles(t.Context(), sel.Workspace.ID, work.Key, "checkout", nil, "baseline"); err != nil {
		t.Fatal(err)
	}
	cfg, err := json.Marshal(map[string]any{"mode": "local", "local": map[string]string{"path": dir}})
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, cfg, 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(t.Context(), binary, args...)
		cmd.Dir = dir
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "SPECGATE_CONFIG_PATH=" + config, "SPECGATE_NO_UPDATE_CHECK=1", "CI=1"}
		return cmd.CombinedOutput()
	}
	if b, err := run("version"); err != nil {
		t.Fatalf("legacy binary does not run: %v %s", err, b)
	}
	snapshot := func() string {
		var tables []string
		rows, err := s.db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatal(err)
			}
			tables = append(tables, name)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		material := map[string][]string{}
		for _, table := range tables {
			rows, err := s.db.Query(`SELECT * FROM "` + strings.ReplaceAll(table, `"`, `""`) + `" ORDER BY rowid`)
			if err != nil {
				t.Fatal(err)
			}
			cols, err := rows.Columns()
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				vals := make([]any, len(cols))
				ptrs := make([]any, len(cols))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					t.Fatal(err)
				}
				material[table] = append(material[table], fmt.Sprintf("%#v", vals))
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
		}
		b, err := json.Marshal(material)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	before := snapshot()
	writeInput := func(name string, body any) string {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	artifactFile := writeInput("artifact.json", map[string]any{"feature_key": "LEGACY", "request_type": "new_feature", "documents": []any{map[string]any{"path": "spec.md", "role": "spec", "content": "# Spec"}}})
	pinFile := writeInput("pin.json", map[string]any{"context_digest": work.ContextDigest, "shell": "sh", "checks": []any{}})
	reportFile := writeInput("completion.json", map[string]any{"event_type": "coding_agent.completed", "agent": map[string]string{"name": "legacy"}, "context_digest": work.ContextDigest, "criteria": []any{}, "checks": []any{}})
	for _, args := range [][]string{
		{"--json", "work", "list"},
		{"--json", "--yes", "workspace", "create", "Forbidden legacy write"},
		{"--json", "--yes", "work", "create-quick", "Forbidden legacy work", "--ac", "Works"},
		{"--json", "--yes", "work", "verification", work.Key},
		{"--json", "--yes", "work", "verification", work.Key, "--file", pinFile},
		{"--json", "--yes", "artifact", "publish", "--file", artifactFile},
		{"--json", "--yes", "delivery", "submit", work.Key, "--file", reportFile},
		{"--json", "--yes", "delivery", "approve", work.Key, "--review-id", "reviewed"},
		{"--json", "--yes", "delivery", "review", work.Key},
	} {
		b, err := run(args...)
		if strings.Contains(string(b), "unknown command") || strings.Contains(string(b), "unknown flag") {
			if got := snapshot(); got != before {
				t.Fatalf("unsupported legacy command %v mutated store", args)
			}
			t.Logf("legacy CLI does not expose %v; mutation path unavailable", args)
			continue
		}
		if err == nil || !strings.Contains(string(b), "specgate_enhanced_writer") {
			t.Fatalf("expected capability refusal for %v: %v %s", args, err, b)
		}
		if got := snapshot(); got != before {
			t.Fatalf("legacy command %v mutated store", args)
		}
	}
}
