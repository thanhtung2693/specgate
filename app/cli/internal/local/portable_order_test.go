package local

import (
	"strings"
	"testing"
)

func TestPortableDeliveryMatchesTheLatestReviewAndItsBoundReport(t *testing.T) {
	store, workspaceID, workID, workKey := newDeliveryOrderTestStore(t)
	const createdAt = "2026-10-04T00:00:00Z"
	for _, row := range []struct{ id, summary string }{
		{"report-z", "higher report ID"}, {"report-a", "bound report body"},
	} {
		if _, err := store.db.ExecContext(t.Context(),
			`INSERT INTO delivery_reports(id,workspace_id,work_id,context_digest,body,created_at)
 VALUES(?,?,?,'digest',json_object('summary',?),?)`,
			row.id, workspaceID, workID, row.summary, createdAt); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct{ id, reportID string }{
		{"review-z", "report-a"}, {"review-a", "report-z"},
	} {
		if _, err := store.db.ExecContext(t.Context(),
			`INSERT INTO delivery_reviews(id,workspace_id,work_id,report_id,verdict,summary,created_at)
 VALUES(?,?,?,?,'passed','reviewed',?)`,
			row.id, workspaceID, workID, row.reportID, createdAt); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"peer-z", "peer-a"} {
		body := `{}`
		if id == "peer-a" {
			body = `{"old_field":"must not survive"}`
		}
		if _, err := store.db.ExecContext(t.Context(),
			`INSERT INTO delivery_peer_reviews(id,workspace_id,work_id,agent_name,body,created_at)
 VALUES(?,?,?,'reviewer',?,?)`, id, workspaceID, workID, body, createdAt); err != nil {
			t.Fatal(err)
		}
	}
	current, err := store.DeliveryStatus(t.Context(), workspaceID, workKey)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != "review-z" || current.ReportID != "report-a" {
		t.Fatalf("unexpected current review: %#v", current)
	}
	bundle, err := store.ExportWorkspace(t.Context(), workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Delivery) != 1 {
		t.Fatalf("delivery rows = %d, want 1", len(bundle.Delivery))
	}
	got := bundle.Delivery[0]
	if got.ReviewID != "review-z" || got.ReportID != "report-a" || got.Report["summary"] != "bound report body" || got.PeerReviewID != "peer-z" {
		t.Fatalf("export differs from current review or mismatches its report body: %#v", got)
	}
	if len(got.PeerReview) != 0 {
		t.Fatalf("latest peer body retained fields from an older review: %#v", got.PeerReview)
	}
}

func TestPortableLatestReportDoesNotMergeHistoricalFields(t *testing.T) {
	store, workspaceID, workID, _ := newDeliveryOrderTestStore(t)
	for _, row := range []struct{ id, body string }{
		{"report-z", `{}`}, {"report-a", `{"old_field":"must not survive"}`},
	} {
		if _, err := store.db.ExecContext(t.Context(), `INSERT INTO delivery_reports
			(id,workspace_id,work_id,context_digest,body,created_at)
			VALUES (?,?,?,'digest',?,'2026-10-04T00:00:00Z')`, row.id, workspaceID, workID, row.body); err != nil {
			t.Fatal(err)
		}
	}
	bundle, err := store.ExportWorkspace(t.Context(), workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Delivery) != 1 || bundle.Delivery[0].ReportID != "report-z" || len(bundle.Delivery[0].Report) != 0 {
		t.Fatalf("latest report retained historical fields: %#v", bundle.Delivery)
	}
}

func TestPortableDeliveryDoesNotGuessAnUnresolvedReviewReport(t *testing.T) {
	for _, reportID := range []string{"", "missing-report"} {
		t.Run("binding="+reportID, func(t *testing.T) {
			store, workspaceID, workID, _ := newDeliveryOrderTestStore(t)
			if _, err := store.db.ExecContext(t.Context(), `INSERT INTO delivery_reports
				(id,workspace_id,work_id,context_digest,body,created_at)
				VALUES ('unrelated-report',?,?,'digest','{"summary":"not bound"}','2026-10-04T00:00:00Z')`, workspaceID, workID); err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.ExecContext(t.Context(), `INSERT INTO delivery_reviews
				(id,workspace_id,work_id,report_id,verdict,summary,created_at)
				VALUES ('review',?,?,?,'passed','reviewed','2026-10-04T00:00:01Z')`, workspaceID, workID, reportID); err != nil {
				t.Fatal(err)
			}
			bundle, err := store.ExportWorkspace(t.Context(), workspaceID)
			if reportID != "" {
				if err == nil || !strings.Contains(err.Error(), "no matching report") {
					t.Fatalf("unresolved binding export error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(bundle.Delivery) != 1 || bundle.Delivery[0].ReviewID != "review" || bundle.Delivery[0].ReportID != "" || bundle.Delivery[0].Report != nil {
				t.Fatalf("legacy unbound review gained a guessed report: %#v", bundle.Delivery)
			}
		})
	}
}
