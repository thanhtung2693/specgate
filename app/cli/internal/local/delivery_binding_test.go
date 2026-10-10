package local_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/local"
)

// Local delivery review takes a bound criterion's verdict from its named check,
// matching the Full-mode deterministic binding path. A bound check that is
// skipped, missing, or failing must never pass on the strength of a prose claim.

func boundWork(t *testing.T, criteria ...string) (*local.Store, string, local.WorkItem) {
	t.Helper()
	store, err := local.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	selection, err := store.Initialize(context.Background(), local.InitInput{
		WorkspaceName: "Alpha", DisplayName: "Human", Username: "human",
	})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateQuickWork(context.Background(), selection.Workspace.ID, local.QuickWorkInput{
		Title:              "Fix timeout",
		Description:        "Stop retrying after the configured limit.",
		AcceptanceCriteria: criteria,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store, selection.Workspace.ID, work
}

func boundCompletion(work local.WorkItem, checks ...map[string]any) map[string]any {
	raw := make([]any, 0, len(checks))
	for _, check := range checks {
		raw = append(raw, check)
	}
	return map[string]any{
		"context_digest": work.ContextDigest,
		"agent":          map[string]any{"name": "builder"},
		"git_receipt": map[string]any{
			"availability":  "available",
			"branch":        "feature/timeout",
			"head_revision": "0b1d2e3f4a5b6c7d8e9f",
		},
		"criteria": []any{map[string]any{
			"criterion_id": "local-1",
			"claim":        "satisfied",
			"evidence":     map[string]any{"heading": "implemented and verified by hand"},
		}},
		"checks": raw,
	}
}

func TestSubmitDeliveryFailsWhenBoundCheckIsSkipped(t *testing.T) {
	store, workspaceID, work := boundWork(t, "Retries stop after three attempts @check:unit")

	review, err := store.SubmitDelivery(context.Background(), workspaceID, work.Key, boundCompletion(work,
		map[string]any{"name": "unit", "status": "skipped", "detail": "not run in this environment"},
	))
	if err != nil {
		t.Fatal(err)
	}
	if review.Verdict != "failed" {
		t.Fatalf("verdict = %q, want failed for a skipped bound check", review.Verdict)
	}
	if !strings.Contains(review.Summary, "unit") {
		t.Fatalf("summary = %q, want the bound check named", review.Summary)
	}
}

func TestSubmitDeliveryFailsWhenBoundCheckIsMissing(t *testing.T) {
	store, workspaceID, work := boundWork(t, "Retries stop after three attempts @check:unit")

	review, err := store.SubmitDelivery(context.Background(), workspaceID, work.Key, boundCompletion(work,
		map[string]any{"name": "lint", "status": "pass", "command": "golangci-lint run"},
	))
	if err != nil {
		t.Fatal(err)
	}
	if review.Verdict != "failed" {
		t.Fatalf("verdict = %q, want failed when the bound check is absent", review.Verdict)
	}
	if !strings.Contains(review.Summary, "unit") {
		t.Fatalf("summary = %q, want the bound check named", review.Summary)
	}
}

// The acceptance criterion is the authority for a binding. A completion that
// omits verification_binding must not escape enforcement.
func TestSubmitDeliveryEnforcesBindingFromAcceptanceCriterionNotReportBody(t *testing.T) {
	store, workspaceID, work := boundWork(t, "Retries stop after three attempts @check:unit")

	body := boundCompletion(work, map[string]any{"name": "unit", "status": "skipped", "detail": "skipped"})
	criteria, _ := body["criteria"].([]any)
	row, _ := criteria[0].(map[string]any)
	delete(row, "verification_binding")

	review, err := store.SubmitDelivery(context.Background(), workspaceID, work.Key, body)
	if err != nil {
		t.Fatal(err)
	}
	if review.Verdict != "failed" {
		t.Fatalf("verdict = %q, want failed regardless of the report body binding", review.Verdict)
	}
}

func TestSubmitDeliveryPassesWhenBoundCheckPasses(t *testing.T) {
	store, workspaceID, work := boundWork(t, "Retries stop after three attempts @check:unit")

	review, err := store.SubmitDelivery(context.Background(), workspaceID, work.Key, boundCompletion(work,
		map[string]any{"name": "unit", "status": "pass", "command": "go test ./..."},
	))
	if err != nil {
		t.Fatal(err)
	}
	if review.Verdict != "passed" {
		t.Fatalf("verdict = %q, want passed for a passing bound check: %s", review.Verdict, review.Summary)
	}
}

// Unbound criteria keep the claim-plus-evidence path, including a skipped check.
func TestSubmitDeliveryLeavesUnboundCriteriaOnTheClaimPath(t *testing.T) {
	store, workspaceID, work := boundWork(t, "Retries stop after three attempts")

	review, err := store.SubmitDelivery(context.Background(), workspaceID, work.Key, boundCompletion(work,
		map[string]any{"name": "unit", "status": "skipped", "detail": "no runner available"},
	))
	if err != nil {
		t.Fatal(err)
	}
	if review.Verdict != "passed" {
		t.Fatalf("verdict = %q, want passed for an unbound criterion: %s", review.Verdict, review.Summary)
	}
}

func TestPeerReviewUsesBoundCheckFromLatestCompletion(t *testing.T) {
	store, workspaceID, work := boundWork(t, "Retries stop after three attempts @check:unit")

	if _, err := store.SubmitDelivery(context.Background(), workspaceID, work.Key, boundCompletion(work,
		map[string]any{"name": "unit", "status": "pass", "command": "go test ./..."},
	)); err != nil {
		t.Fatal(err)
	}
	completion, err := store.LatestDeliveryReport(context.Background(), workspaceID, work.Key)
	if err != nil {
		t.Fatal(err)
	}
	receipt, _ := completion.Body["git_receipt"].(map[string]any)

	peer := map[string]any{
		"agent": map[string]any{"name": "reviewer"},
		"criteria": []any{map[string]any{
			"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "reviewed"},
		}},
		"peer_review_of": map[string]any{
			"completion_feedback_event_id": completion.ID,
			"git_receipt":                  receipt,
		},
	}

	first, err := store.PeerReviewDelivery(context.Background(), workspaceID, work.Key, peer)
	if err != nil {
		t.Fatalf("peer review rejected the completion's passing bound check: %v", err)
	}
	second, err := store.PeerReviewDelivery(context.Background(), workspaceID, work.Key, peer)
	if err != nil {
		t.Fatalf("exact peer-review retry failed: %v", err)
	}
	if second.ID != first.ID || second.CreatedAt != first.CreatedAt {
		t.Fatalf("exact retry created another review: first=%#v second=%#v", first, second)
	}
	for _, field := range []string{"completion_feedback_event_id", "git_receipt"} {
		t.Run(field, func(t *testing.T) {
			binding := peer["peer_review_of"].(map[string]any)
			original := binding[field]
			binding[field] = "stale"
			defer func() { binding[field] = original }()
			if _, err := store.PeerReviewDelivery(context.Background(), workspaceID, work.Key, peer); !errors.Is(err, local.ErrReviewChanged) {
				t.Fatalf("stale binding must be conflict, got %v", err)
			}
		})
	}
}

func TestPeerReviewCanRecoverAfterAnInterveningFailedReview(t *testing.T) {
	for _, interveningAgent := range []string{"reviewer", "other-reviewer"} {
		t.Run(interveningAgent, func(t *testing.T) {
			store, workspaceID, work := boundWork(t, "Retries stop after three attempts")
			if _, err := store.SubmitDelivery(t.Context(), workspaceID, work.Key, boundCompletion(work)); err != nil {
				t.Fatal(err)
			}
			completion, err := store.LatestDeliveryReport(t.Context(), workspaceID, work.Key)
			if err != nil {
				t.Fatal(err)
			}
			criterion := map[string]any{
				"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "reviewed"},
			}
			peer := map[string]any{
				"agent":    map[string]any{"name": "reviewer"},
				"criteria": []any{criterion},
				"peer_review_of": map[string]any{
					"completion_feedback_event_id": completion.ID,
					"git_receipt":                  completion.Body["git_receipt"],
				},
			}
			first, err := store.PeerReviewDelivery(t.Context(), workspaceID, work.Key, peer)
			if err != nil {
				t.Fatal(err)
			}
			criterion["claim"] = "partial"
			peer["agent"] = map[string]any{"name": interveningAgent}
			failed, err := store.PeerReviewDelivery(t.Context(), workspaceID, work.Key, peer)
			if err != nil {
				t.Fatal(err)
			}
			status, err := store.PeerReviewStatus(t.Context(), workspaceID, work.Key)
			if err != nil || status.State != "failed" {
				t.Fatalf("intervening review status = %#v, err = %v", status, err)
			}
			criterion["claim"] = "satisfied"
			peer["agent"] = map[string]any{"name": "reviewer"}
			recovered, err := store.PeerReviewDelivery(t.Context(), workspaceID, work.Key, peer)
			if err != nil {
				t.Fatal(err)
			}
			if recovered.ID == first.ID || recovered.ID == failed.ID {
				t.Fatalf("recovery reused historical evidence instead of recording the new review: %#v", recovered)
			}
			status, err = store.PeerReviewStatus(t.Context(), workspaceID, work.Key)
			if err != nil || status.State != "passed" || status.ReviewedAt != recovered.CreatedAt {
				t.Fatalf("recovered review must be the current readback: %#v, err = %v", status, err)
			}
			retry, err := store.PeerReviewDelivery(t.Context(), workspaceID, work.Key, peer)
			if err != nil || retry.ID != recovered.ID || retry.CreatedAt != recovered.CreatedAt {
				t.Fatalf("immediate retry must reuse the current review: %#v, err = %v", retry, err)
			}
		})
	}
}
