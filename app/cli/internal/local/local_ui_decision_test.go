package local

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestExpectedCompletionRejectsNewUnreviewedReportInDecisionTransaction(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	selection, err := store.Initialize(t.Context(), InitInput{WorkspaceName: "Local", Username: "reviewer", DisplayName: "Reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateQuickWork(t.Context(), selection.Workspace.ID, QuickWorkInput{Title: "Exact completion", AcceptanceCriteria: []string{"Keep binding"}})
	if err != nil {
		t.Fatal(err)
	}
	report := map[string]any{"context_digest": work.ContextDigest, "agent": map[string]any{"name": "builder"}}
	review, err := store.SubmitDelivery(t.Context(), selection.Workspace.ID, work.Key, report)
	if err != nil {
		t.Fatal(err)
	}
	// An unreviewed completion must invalidate the viewed legacy pair atomically.
	_, err = store.db.ExecContext(t.Context(), `INSERT INTO delivery_reports(id, workspace_id, work_id, context_digest, body, created_at) VALUES (?, ?, ?, ?, ?, ?)`, "new-report", selection.Workspace.ID, work.ID, work.ContextDigest, `{}`, time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	err = store.DecideDeliveryWithBasis(t.Context(), selection.Workspace.ID, work.Key, "reject", "reviewer", "outdated", review.ID, "", AcceptanceOptions{ExpectedCompletionID: review.ReportID})
	if !errors.Is(err, ErrReviewChanged) {
		t.Fatalf("stale completion accepted: %v", err)
	}
	current, err := store.DeliveryStatus(t.Context(), selection.Workspace.ID, work.Key)
	if err != nil || current.HumanDecision != "" {
		t.Fatalf("failed guard wrote decision: %+v %v", current, err)
	}
}
