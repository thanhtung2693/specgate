package command_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestLocalDraftPromotionIsGovernanceRefusalWithoutWrites(t *testing.T) {
	deps, out := newTestDeps(t)
	deps.Stderr = out
	state := filepath.Join(t.TempDir(), "local")
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "init", "--mode", "local", "--local-dir", state, "--workspace-name", "Alpha", "--display-name", "Tester", "--username", "tester"); code != output.ExitOK {
		t.Fatalf("init exit=%d output=%s", code, out.String())
	}
	file := filepath.Join(t.TempDir(), "artifact.json")
	if err := os.WriteFile(file, []byte(`{"feature_key":"DRAFT","request_type":"new_feature","documents":[{"path":"spec.md","role":"spec","content":"Draft fixture, not approved"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "artifact", "publish", "--file", file); code != output.ExitOK {
		t.Fatalf("publish exit=%d output=%s", code, out.String())
	}
	var published struct {
		Data struct {
			ID string `json:"artifact_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &published); err != nil || published.Data.ID == "" {
		t.Fatalf("publish envelope=%s err=%v", out.String(), err)
	}
	db := filepath.Join(state, "state.db")
	before, err := os.ReadFile(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"--json", "--plain"} {
		out.Reset()
		if code := command.ExecuteForCode(command.NewRootCommand(deps), format, "--yes", "artifact", "promote", published.Data.ID); code != output.ExitGovernanceFailed {
			t.Fatalf("%s promotion exit=%d output=%s", format, code, out.String())
		}
		if !strings.Contains(out.String(), "must be approved before promotion") {
			t.Fatalf("missing actionable refusal: %s", out.String())
		}
		if format == "--json" {
			var envelope struct {
				OK    bool                `json:"ok"`
				Error output.ErrorPayload `json:"error"`
			}
			if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || envelope.OK || envelope.Error.Code != "governance_failed" || envelope.Error.Transient {
				t.Fatalf("refusal envelope=%s err=%v", out.String(), err)
			}
		}
		after, err := os.ReadFile(db)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("rejected promotion changed store: err=%v", err)
		}
	}
	if deps.Client != nil {
		t.Fatal("Local refusal created HTTP client")
	}
}
