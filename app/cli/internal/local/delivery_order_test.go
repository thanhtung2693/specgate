package local

import (
	"context"
	"path/filepath"
	"testing"
)

func TestLatestDeliveryReportBreaksTimestampTiesByID(t *testing.T) {
	store, workspaceID, workID, workKey := newDeliveryOrderTestStore(t)
	createdAt := "2026-07-19T00:00:00Z"
	for _, row := range []struct {
		id   string
		body string
	}{
		{id: "report-a", body: `{"summary":"lower id"}`},
		{id: "report-z", body: `{"summary":"higher id"}`},
	} {
		if _, err := store.db.Exec(
			`INSERT INTO delivery_reports(id, workspace_id, work_id, context_digest, body, created_at)
			 VALUES (?, ?, ?, 'digest', ?, ?)`,
			row.id, workspaceID, workID, row.body, createdAt,
		); err != nil {
			t.Fatal(err)
		}
	}

	got, err := store.LatestDeliveryReport(context.Background(), workspaceID, workKey)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "report-z" {
		t.Fatalf("latest report = %q, want report-z", got.ID)
	}
}

func TestPeerReviewStatusBreaksTimestampTiesByID(t *testing.T) {
	store, workspaceID, workID, workKey := newDeliveryOrderTestStore(t)
	createdAt := "2026-07-19T00:00:00Z"
	if _, err := store.db.Exec(
		`INSERT INTO delivery_reports(id, workspace_id, work_id, context_digest, body, created_at)
		 VALUES ('report-z', ?, ?, 'digest', '{}', ?)`,
		workspaceID, workID, createdAt,
	); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		id    string
		agent string
	}{
		{id: "peer-a", agent: "reviewer-a"},
		{id: "peer-z", agent: "reviewer-z"},
	} {
		if _, err := store.db.Exec(
			`INSERT INTO delivery_peer_reviews(id, workspace_id, work_id, agent_name, body, created_at)
			 VALUES (?, ?, ?, ?, '{"peer_review_of":{"completion_feedback_event_id":"report-z"}}', ?)`,
			row.id, workspaceID, workID, row.agent, createdAt,
		); err != nil {
			t.Fatal(err)
		}
	}

	got, err := store.PeerReviewStatus(context.Background(), workspaceID, workKey)
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentName != "reviewer-z" {
		t.Fatalf("latest peer reviewer = %q, want reviewer-z", got.AgentName)
	}
}

func newDeliveryOrderTestStore(t *testing.T) (*Store, string, string, string) {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	selection, err := store.Initialize(
		context.Background(),
		InitInput{WorkspaceName: "Alpha", DisplayName: "Human", Username: "human"},
	)
	if err != nil {
		t.Fatal(err)
	}
	const workID = "work-1"
	const workKey = "LOCAL-ORDER"
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{
			query: `INSERT INTO artifacts(
				id, workspace_id, feature_key, request_type, version, status,
				snapshot_digest, policy_digest, policy_snapshot_json, created_at
			) VALUES ('artifact-1', ?, 'ORDER', 'bug_fix', 1, 'approved', 'snapshot', '', '', '2026-07-19T00:00:00Z')`,
			args: []any{selection.Workspace.ID},
		},
		{
			query: `INSERT INTO features(id, workspace_id, key, canonical_artifact_id, version, created_at)
				VALUES ('feature-1', ?, 'ORDER', 'artifact-1', 1, '2026-07-19T00:00:00Z')`,
			args: []any{selection.Workspace.ID},
		},
		{
			query: `INSERT INTO work_items(
				id, workspace_id, key, feature_id, artifact_id, title, description,
				phase, context_digest, acceptance_criteria, created_at
			) VALUES (?, ?, ?, 'feature-1', 'artifact-1', 'Ordering', '', 'ready', 'digest', '[]', '2026-07-19T00:00:00Z')`,
			args: []any{workID, selection.Workspace.ID, workKey},
		},
	} {
		if _, err := store.db.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	return store, selection.Workspace.ID, workID, workKey
}
