package command_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestLocalQuickMalformedCriterionIsNotSilentlyDropped(t *testing.T) {
	deps, out := newTestDeps(t)
	state := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "init", "--mode", "local", "--local-dir", state, "--workspace-name", "Alpha", "--display-name", "Tester", "--username", "tester"); code != output.ExitOK {
		t.Fatalf("init exit=%d: %s", code, out.String())
	}
	for _, row := range []any{42, nil, []string{"nested"}, map[string]any{"text": 42}, map[string]any{"text": "Must be checked", "verification_binding": 42}} {
		t.Run(string(mustJSON(t, row)), func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "input.json")
			body := map[string]any{"title": "Invalid quick input", "acceptance_criteria": []any{"Valid criterion must not hide malformed scope", row}}
			if err := os.WriteFile(file, mustJSON(t, body), 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(filepath.Join(state, "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			out.Reset()
			if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "work", "create-quick", "--file", file); code != output.ExitUsage {
				t.Fatalf("invalid row exit=%d output=%s", code, out.String())
			}
			var envelope struct {
				OK    bool                `json:"ok"`
				Error output.ErrorPayload `json:"error"`
			}
			if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || envelope.OK || envelope.Error.Code != "validation" || envelope.Error.Transient {
				t.Fatalf("invalid envelope=%s err=%v", out.String(), err)
			}
			after, err := os.ReadFile(filepath.Join(state, "state.db"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("invalid row changed store: %v", err)
			}
		})
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
