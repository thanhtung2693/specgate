package local

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPortableExportRedactsLegacyCompletionAndPeerBinding(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	sel, err := s.Initialize(t.Context(), InitInput{WorkspaceName: "Credential export", Username: "human", DisplayName: "Human"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := s.CreateQuickWork(t.Context(), sel.Workspace.ID, QuickWorkInput{Title: "Export", AcceptanceCriteria: []string{"Keep evidence"}})
	if err != nil {
		t.Fatal(err)
	}
	const receipt = `{"repository":"https://user:synthetic-secret@example.invalid/org/repo.git","head_revision":"head","diff_digest":"sha256:legacy"}`
	const report = `{"summary":"keep","git_receipt":` + receipt + `}`
	const peer = `{"agent":{"name":"reviewer"},"peer_review_of":{"completion_feedback_event_id":"report","git_receipt":` + receipt + `}}`
	if _, err := s.db.ExecContext(t.Context(), `INSERT INTO delivery_reports(id,workspace_id,work_id,context_digest,body,created_at) VALUES('report',?,?,?,?,'2026-10-04T00:00:00Z')`, sel.Workspace.ID, work.ID, work.ContextDigest, report); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(t.Context(), `INSERT INTO delivery_reviews(id,workspace_id,work_id,report_id,verdict,summary,created_at) VALUES('review',?,?,'report','passed','keep','2026-10-04T00:00:01Z')`, sel.Workspace.ID, work.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(t.Context(), `INSERT INTO delivery_peer_reviews(id,workspace_id,work_id,agent_name,body,created_at) VALUES('peer',?,?,'reviewer',?,'2026-10-04T00:00:00Z')`, sel.Workspace.ID, work.ID, peer); err != nil {
		t.Fatal(err)
	}
	bundle, err := s.ExportWorkspace(t.Context(), sel.Workspace.ID)
	if err != nil || len(bundle.Delivery) != 1 {
		t.Fatalf("export: %+v %v", bundle, err)
	}
	raw, err := json.Marshal(bundle)
	if err != nil || strings.Contains(string(raw), "synthetic-secret") {
		t.Errorf("portable export retained credential: %s err=%v", raw, err)
	}
	completion, _ := bundle.Delivery[0].Report["git_receipt"].(map[string]any)
	bound, _ := bundle.Delivery[0].PeerReview["peer_review_of"].(map[string]any)
	if completion["repository"] != "https://example.invalid/org/repo.git" || completion["diff_digest"] != "sha256:legacy" || !reflect.DeepEqual(completion, bound["git_receipt"]) {
		t.Errorf("legacy receipt/binding changed inconsistently: %v %v", completion, bound)
	}
	for _, tc := range []struct{ table, want string }{{"delivery_reports", report}, {"delivery_peer_reviews", peer}} {
		var stored string
		if err := s.db.QueryRowContext(t.Context(), `SELECT body FROM `+tc.table).Scan(&stored); err != nil || stored != tc.want {
			t.Errorf("immutable %s changed: %q err=%v", tc.table, stored, err)
		}
	}
}
