package command_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

const legacyCredentialOrigin = "https://user:synthetic-secret@example.invalid/repo.git?token=synthetic-query"

func TestPeerReviewRefusesCredentialBearingLegacyBinding(t *testing.T) {
	for _, mode := range []config.Mode{config.ModeLocal, config.ModeFull} {
		t.Run(string(mode), func(t *testing.T) {
			deps, fc, _, out := newFakeDeps(t)
			if mode == config.ModeLocal {
				if err := (config.Config{Mode: mode, Local: config.LocalStore{Path: t.TempDir()}}).SaveTo(deps.ConfigPath); err != nil {
					t.Fatal(err)
				}
			}
			path := writeDeliveryJSON(t, map[string]any{
				"event_type": "coding_agent.peer_reviewed", "summary": "Reviewed", "agent": map[string]any{"name": "reviewer"},
				"peer_review_of": map[string]any{"completion_feedback_event_id": "old", "git_receipt": map[string]any{"repository": legacyCredentialOrigin}},
			})
			code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "peer-review", "CR-101", "--file", path)
			if code != output.ExitUsage || fc.calls != 0 || !strings.Contains(out.String(), "fresh completion") || strings.Contains(out.String(), "synthetic") {
				t.Fatalf("unsafe binding was not refused before effects: code=%d calls=%d output=%s", code, fc.calls, out.String())
			}
		})
	}
}

func TestCompletionRoutesRefuseCredentialBearingPeerBinding(t *testing.T) {
	for _, route := range [][]string{{"delivery", "report"}, {"delivery", "submit"}, {"change", "submit"}} {
		for _, event := range []string{"coding_agent.completed", "coding_agent.peer_reviewed"} {
			t.Run(strings.Join(route, ".")+"/"+event, func(t *testing.T) {
				deps, fc, _, out := newFakeDeps(t)
				path := writeDeliveryJSON(t, map[string]any{
					"event_type": event, "summary": "Reviewed", "agent": map[string]any{"name": "reviewer"},
					"peer_review_of": map[string]any{"completion_feedback_event_id": "old", "git_receipt": map[string]any{"repository": legacyCredentialOrigin}},
				})
				args := append([]string{"--json"}, route...)
				args = append(args, "CR-101", "--file", path)
				code := command.ExecuteForCode(command.NewRootCommand(deps), args...)
				if code != output.ExitUsage || fc.calls != 0 || strings.Contains(out.String(), "synthetic") {
					t.Fatalf("unsafe binding reached completion route: code=%d calls=%d output=%s", code, fc.calls, out.String())
				}
			})
		}
	}
}

func TestPeerReviewInitRefusesCredentialBearingLegacyCompletion(t *testing.T) {
	for _, mode := range []config.Mode{config.ModeLocal, config.ModeFull} {
		t.Run(string(mode), func(t *testing.T) {
			deps, fc, _, out := newFakeDeps(t)
			ref := "CR-101"
			body := map[string]any{"agent": map[string]any{"name": "builder"}, "git_receipt": map[string]any{"repository": legacyCredentialOrigin}}
			if mode == config.ModeLocal {
				state, store, sel, work := newLocalChangeWork(t, deps)
				defer store.Close()
				body["context_digest"] = work.ContextDigest
				if _, err := store.SubmitDelivery(t.Context(), sel.Workspace.ID, work.Key, body); err != nil {
					t.Fatal(err)
				}
				if err := (config.Config{Mode: mode, Local: config.LocalStore{Path: state}}).SaveTo(deps.ConfigPath); err != nil {
					t.Fatal(err)
				}
				ref = work.Key
			} else {
				raw, _ := json.Marshal(body)
				fc.feedbackEvents = []client.GovernanceFeedbackEvent{{ID: "old", EventType: "coding_agent.completed", PayloadJSON: string(raw)}}
			}
			path := filepath.Join(t.TempDir(), "peer.json")
			code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "peer-review", ref, "--init="+path)
			if code != output.ExitUsage || !strings.Contains(out.String(), "fresh completion") || strings.Contains(out.String(), "synthetic") {
				t.Fatalf("unsafe scaffold: code=%d output=%s", code, out.String())
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("unsafe scaffold written: %v", err)
			}
		})
	}
}

type credentialFeedbackClient struct {
	*fakeClient
	bodies []map[string]any
}

func (c *credentialFeedbackClient) ReportFeedback(ctx context.Context, id string, body map[string]any) (map[string]any, error) {
	c.bodies = append(c.bodies, body)
	return c.fakeClient.ReportFeedback(ctx, id, body)
}

func TestLegacyPortableImportProjectsBothReceiptsAfterChecksumValidation(t *testing.T) {
	deps, fc, _, out := newFakeDeps(t)
	setPortableDestination(t, deps)
	capture := &credentialFeedbackClient{fakeClient: fc}
	deps.Client = capture
	fc.acceptanceCriteria = []client.AcceptanceCriterion{{ID: "full-ac", Text: "Works"}}
	receipt := map[string]any{"repository": legacyCredentialOrigin, "diff_digest": "sha256:legacy"}
	payload := local.PortableWorkspace{
		Workspace: local.Workspace{ID: "local-ws", Slug: "local", Name: "Local"},
		Work:      []local.WorkItem{{ID: "local-work", Key: "LOCAL-1", WorkspaceID: "local-ws", Title: "Fix", Phase: "ready", AcceptanceCriteria: []string{"Works"}}},
		Delivery: []local.PortableDeliveryEvidence{{WorkID: "local-work",
			Report:     map[string]any{"event_type": "coding_agent.completed", "git_receipt": receipt},
			PeerReview: map[string]any{"event_type": "coding_agent.peer_reviewed", "peer_review_of": map[string]any{"completion_feedback_event_id": "old", "git_receipt": receipt}},
		}},
	}
	path := writePortableBundle(t, payload)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "portable", "import", "--file", path)
	if code != output.ExitOK || len(capture.bodies) != 2 {
		t.Fatalf("import code=%d feedback=%d output=%s", code, len(capture.bodies), out.String())
	}
	raw, _ := json.Marshal(capture.bodies)
	if strings.Contains(string(raw), "synthetic") {
		t.Fatalf("legacy import sent credentials: %s", raw)
	}
	completion := capture.bodies[0]["git_receipt"].(map[string]any)
	binding := capture.bodies[1]["peer_review_of"].(map[string]any)
	if !reflect.DeepEqual(completion, binding["git_receipt"]) || completion["repository"] != "https://example.invalid/repo.git" || completion["diff_digest"] != "sha256:legacy" || binding["completion_feedback_event_id"] != "evt-1" {
		t.Fatalf("import changed binding: %v %v", completion, binding)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("import rewrote source bundle")
	}

	// A credential-free rewrite without updating the checksum remains invalid.
	tampered := strings.ReplaceAll(string(before), legacyCredentialOrigin, "https://example.invalid/repo.git")
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	calls := fc.calls
	code = command.ExecuteForCode(command.NewRootCommand(deps), "--json", "--yes", "portable", "import", "--file", path)
	if code == output.ExitOK || fc.calls != calls || !strings.Contains(out.String(), "checksum") {
		t.Fatalf("invalid checksum accepted: code=%d output=%s", code, out.String())
	}
}
