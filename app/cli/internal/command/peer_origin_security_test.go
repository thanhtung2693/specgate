package command_test

import (
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/output"
)

const legacyCredentialOrigin = "https://user:synthetic-secret@example.invalid/repo.git?token=synthetic-query"

func TestPeerReviewRefusesCredentialBearingLegacyBinding(t *testing.T) {
	deps, out := newTestDeps(t)
	path := writeDeliveryJSON(t, map[string]any{
		"event_type": "coding_agent.peer_reviewed", "summary": "Reviewed", "agent": map[string]any{"name": "reviewer"},
		"peer_review_of": map[string]any{"completion_feedback_event_id": "old", "git_receipt": map[string]any{"repository": legacyCredentialOrigin}},
	})
	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "delivery", "peer-review", "LOCAL-101", "--file", path)
	if code != output.ExitUsage || !strings.Contains(out.String(), "fresh completion") || strings.Contains(out.String(), "synthetic") {
		t.Fatalf("unsafe binding was not refused: code=%d output=%s", code, out.String())
	}
}

func TestCompletionRoutesRefuseCredentialBearingPeerBinding(t *testing.T) {
	for _, route := range [][]string{{"delivery", "submit"}, {"change", "submit"}} {
		for _, event := range []string{"coding_agent.completed", "coding_agent.peer_reviewed"} {
			t.Run(strings.Join(route, ".")+"/"+event, func(t *testing.T) {
				deps, out := newTestDeps(t)
				path := writeDeliveryJSON(t, map[string]any{
					"event_type": event, "summary": "Reviewed", "agent": map[string]any{"name": "reviewer"},
					"peer_review_of": map[string]any{"completion_feedback_event_id": "old", "git_receipt": map[string]any{"repository": legacyCredentialOrigin}},
				})
				args := append([]string{"--json"}, route...)
				args = append(args, "CR-101", "--file", path)
				code := command.ExecuteForCode(command.NewRootCommand(deps), args...)
				if code != output.ExitUsage || strings.Contains(out.String(), "synthetic") {
					t.Fatalf("unsafe binding reached completion route: code=%d output=%s", code, out.String())
				}
			})
		}
	}
}
