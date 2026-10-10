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

func TestLocalCrossFeatureImpactIsValidationWithoutWrites(t *testing.T) {
	deps, out := newTestDeps(t)
	state := filepath.Join(t.TempDir(), "state")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "init", "--mode", "local", "--local-dir", state, "--workspace-name", "Impact", "--display-name", "Tester", "--username", "tester"); code != 0 {
		t.Fatalf("init exit=%d: %s", code, out.String())
	}
	var ids []string
	for _, feature := range []string{"FIRST", "SECOND"} {
		file := filepath.Join(t.TempDir(), "artifact.json")
		if err := os.WriteFile(file, []byte(`{"feature_key":"`+feature+`","request_type":"new_feature","documents":[{"path":"spec.md","role":"spec","content":"Draft fixture"}]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		out.Reset()
		if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", file); code != 0 {
			t.Fatalf("publish exit=%d: %s", code, out.String())
		}
		var envelope struct {
			Data struct {
				ID string `json:"artifact_id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || envelope.Data.ID == "" {
			t.Fatalf("publish envelope=%s err=%v", out.String(), err)
		}
		ids = append(ids, envelope.Data.ID)
	}
	db := filepath.Join(state, "state.db")
	before, err := os.ReadFile(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{ids[0], ids[1]}, {ids[1], ids[0]}} {
		out.Reset()
		if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "impact", pair[0], "--compare", pair[1]); code != output.ExitUsage {
			t.Fatalf("impact exit=%d: %s", code, out.String())
		}
		var envelope struct {
			OK    bool                `json:"ok"`
			Error output.ErrorPayload `json:"error"`
		}
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || envelope.OK || envelope.Error.Code != "validation" || envelope.Error.Transient {
			t.Fatalf("impact envelope=%s err=%v", out.String(), err)
		}
		after, err := os.ReadFile(db)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("impact changed store: %v", err)
		}
	}
	if deps.Client != nil {
		t.Fatal("Local impact created HTTP client")
	}
}
