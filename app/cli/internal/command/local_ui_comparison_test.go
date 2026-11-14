package command

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/local"
)

func TestLocalUIComparisonUsesEarlierHumanDecision(t *testing.T) {
	s, work := localUITestServer(t)
	ctx, workspace := t.Context(), s.selection.Workspace.ID
	reading := func() map[string]any {
		t.Helper()
		response := localUIRequest(s, "GET", "/api/work?ref="+work.Key, "", true, false)
		var envelope struct{ Data map[string]any }
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &envelope) != nil {
			t.Fatalf("work response %d: %s", response.Code, response.Body.String())
		}
		comparison, _ := envelope.Data["review_comparison"].(map[string]any)
		return comparison
	}
	submit := func(item local.WorkItem, summary, claim string) local.DeliveryReview {
		t.Helper()
		review, err := s.store.SubmitDelivery(ctx, item.WorkspaceID, item.Key, map[string]any{
			"context_digest": item.ContextDigest, "agent": map[string]any{"name": "builder"}, "summary": summary,
			"criteria": []any{map[string]any{"criterion_id": "local-1", "claim": claim, "evidence": map[string]any{"kind": "file", "path": "proof.txt", "line": 1}}},
			"checks":   []any{map[string]any{"name": "unit", "command": "test -f proof.txt", "status": "pass"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return review
	}
	reject := func(item local.WorkItem, review local.DeliveryReview, note string) {
		t.Helper()
		if err := s.store.DecideDeliveryWithBasis(ctx, item.WorkspaceID, item.Key, "reject", "reviewer", note, review.ID, ""); err != nil {
			t.Fatal(err)
		}
	}
	old := submit(work, "old", "partial")
	if got := reading()["state"]; got != "no_baseline" {
		t.Fatalf("first report comparison = %q", got)
	}
	reject(work, old, "first explicit review")
	prior := submit(work, "prior summary", "partial")
	reject(work, prior, "use a production path")
	submit(work, "an undecided report", "partial")
	current := submit(work, "current summary", "satisfied")
	other, err := s.store.CreateQuickWork(ctx, workspace, local.QuickWorkInput{Title: "Other work", Description: "Other scope", AcceptanceCriteria: []string{"Other criterion"}})
	if err != nil {
		t.Fatal(err)
	}
	reject(other, submit(other, "unrelated", "partial"), "unrelated")
	otherWorkspace, err := s.store.CreateWorkspace(ctx, "Outside workspace")
	if err != nil {
		t.Fatal(err)
	}
	other, err = s.store.CreateQuickWork(ctx, otherWorkspace.ID, local.QuickWorkInput{Title: "Outside", Description: "Outside scope", AcceptanceCriteria: []string{"Outside criterion"}})
	if err != nil {
		t.Fatal(err)
	}
	reject(other, submit(other, "unrelated workspace", "partial"), "unrelated workspace")
	comparison := reading()
	baseline, _ := comparison["prior"].(map[string]any)
	if comparison["state"] != "available" || baseline["review_id"] != prior.ID || baseline["completion_id"] != prior.ReportID || baseline["decision"] != "reject" || baseline["note"] != "use a production path" || comparison["review_id"] != current.ID || comparison["completion_id"] != current.ReportID {
		t.Fatalf("comparison identities or note: %#v", comparison)
	}
	if comparison["summary_changed"] != true || fmt.Sprint(comparison["changed_criteria"]) != "[local-1]" || comparison["changed_checks"] != nil {
		t.Fatalf("material differences: %#v", comparison)
	}
	if latest, err := s.store.DeliveryStatus(ctx, workspace, work.Key); err != nil || latest.ID != current.ID || latest.HumanDecision != "" {
		t.Fatalf("comparison changed the current review: %+v %v", latest, err)
	}
}

func TestLocalUIComparisonProjectsMaterialCheckChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, work := localUITestServerAt(t, path)
	ctx, workspace := t.Context(), s.selection.Workspace.ID
	submit := func(runVersion int, runID, digest, secondStatus string) local.DeliveryReview {
		t.Helper()
		review, err := s.store.SubmitDelivery(ctx, workspace, work.Key, map[string]any{
			"context_digest": work.ContextDigest, "agent": map[string]any{"name": "builder"}, "summary": "Stable scope",
			"criteria": []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"kind": "file", "path": "proof.txt", "line": 1}}},
			"checks": []any{
				map[string]any{"name": "unit", "command": "run first", "status": "pass", "source": "specgate_cli", "detail": "executed by specgate: exit 0 — ok example.test/suite " + runID,
					"test_run":         map[string]any{"version": runVersion, "id": runID, "freshness": "matching_endpoints", "before": map[string]any{"files": map[string]any{"private.txt": "content-hash"}}},
					"test_observation": map[string]any{"report_digest": digest, "selected": []any{map[string]any{"classname": "suite", "name": "case", "outcome": "passed"}}}},
				map[string]any{"name": "unit", "command": "run second", "status": secondStatus},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return review
	}
	reject := func(review local.DeliveryReview) {
		t.Helper()
		if err := s.store.DecideDeliveryWithBasis(ctx, workspace, work.Key, "reject", "reviewer", "rework", review.ID, ""); err != nil {
			t.Fatal(err)
		}
	}
	read := func() (*localUIReviewComparison, []localUIReviewCheck) {
		t.Helper()
		response := localUIRequest(s, "GET", "/api/work?ref="+work.Key, "", true, false)
		var envelope struct {
			Data struct {
				Comparison *localUIReviewComparison `json:"review_comparison"`
				Completion struct{ Checks []localUIReviewCheck }
			}
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &envelope) != nil || envelope.Data.Comparison == nil {
			t.Fatalf("work response %d: %s", response.Code, response.Body.String())
		}
		if contains := response.Body.String(); strings.Contains(contains, "private.txt") || strings.Contains(contains, "content-hash") {
			t.Fatal("run checkout manifest leaked into the review projection")
		}
		return envelope.Data.Comparison, envelope.Data.Completion.Checks
	}
	reject(submit(1, "original-run", "digest-one", "pass"))
	submit(1, "repeated-run", "digest-two", "pass")
	comparison, checks := read()
	if comparison.State != "available" || len(comparison.ChangedChecks) != 0 || len(comparison.ChangedCriteria) != 0 || comparison.SummaryChanged {
		t.Fatalf("rerun metadata changed the comparison: %+v", comparison)
	}
	if len(checks) != 2 || checks[0].Name != checks[1].Name || checks[1].Command != "run second" || len(checks[0].Selected) != 1 {
		t.Fatalf("submitted checks lost original evidence: %+v", checks)
	}
	submit(1, "rerun-changed", "new-digest", "skipped")
	comparison, _ = read()
	if fmt.Sprint(comparison.ChangedChecks) != "[unit]" || len(comparison.PriorChecks["unit"]) != 2 {
		t.Fatalf("change to same-name check was lost: %+v", comparison)
	}
	bad := submit(2, "unsupported-run", "unreadable", "pass")
	reject(bad)
	current := submit(1, "new-run", "digest-three", "pass")
	comparison, _ = read()
	if comparison.State != "unavailable" || comparison.Prior == nil || comparison.Prior.ReviewID != bad.ID || comparison.ReviewID != current.ID {
		t.Fatalf("unreadable history fell back or hid current review: %+v", comparison)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, malformed := range []string{
		`null`, `{"criteria":"broken"}`, `{"checks":{}}`, `{"summary":false}`,
		`{"criteria":[{}]}`, `{"criteria":[{"criterion_id":"local-1","claim":"satisfied","evidence":false}]}`,
		`{"checks":[null]}`, `{"checks":[{"name":"unit"}]}`,
		`{"checks":[{"name":"unit","status":true}]}`, `{"checks":[{"name":false,"status":" pass "}]}`,
		`{"checks":[{"status":"pass","source":"specgate_cli","test_observation":{"selected":[{}]}}]}`,
	} {
		if _, err := db.ExecContext(ctx, "UPDATE delivery_reports SET body = ? WHERE id = ?", malformed, bad.ReportID); err != nil {
			t.Fatal(err)
		}
		comparison, _ = read()
		if comparison.State != "unavailable" || comparison.Prior == nil || comparison.Prior.ReviewID != bad.ID || comparison.ReviewID != current.ID || len(comparison.ChangedCriteria)+len(comparison.ChangedChecks) != 0 {
			t.Errorf("malformed %s presented as comparable: %+v", malformed, comparison)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE delivery_reports SET body = '{"summary":"legacy"}' WHERE id = ?`, bad.ReportID); err != nil {
		t.Fatal(err)
	}
	comparison, _ = read()
	if comparison.State != "available" || comparison.Prior == nil || comparison.Prior.Summary != "legacy" {
		t.Fatalf("absent legacy optional fields made the comparison unavailable: %+v", comparison)
	}
}

func TestLocalUIComparisonNormalizesHistoricalCheckNamesAndStatuses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, _ := localUITestServerAt(t, path)
	ctx, workspace := t.Context(), s.selection.Workspace.ID
	work, err := s.store.CreateQuickWork(ctx, workspace, local.QuickWorkInput{
		Title: "Compare check evidence", AcceptanceCriteria: []string{"Work meets the criterion @check:unit"},
	})
	if err != nil {
		t.Fatal(err)
	}
	submit := func(name, status string) local.DeliveryReview {
		t.Helper()
		review, err := s.store.SubmitDelivery(ctx, workspace, work.Key, map[string]any{
			"context_digest": work.ContextDigest, "agent": map[string]any{"name": "builder"},
			"criteria": []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"kind": "file"}}},
			"checks":   []any{map[string]any{"name": name, "command": "run unit", "status": status}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return review
	}
	read := func() (*localUIReviewComparison, []localUIReviewCheck) {
		t.Helper()
		response := localUIRequest(s, "GET", "/api/work?ref="+work.Key, "", true, false)
		var envelope struct {
			Data struct {
				Comparison *localUIReviewComparison `json:"review_comparison"`
				Completion struct{ Checks []localUIReviewCheck }
			}
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &envelope) != nil || envelope.Data.Comparison == nil {
			t.Fatalf("work response %d: %s", response.Code, response.Body.String())
		}
		return envelope.Data.Comparison, envelope.Data.Completion.Checks
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stored := func(id string) string {
		t.Helper()
		var body string
		if err := db.QueryRowContext(ctx, "SELECT body FROM delivery_reports WHERE id = ?", id).Scan(&body); err != nil {
			t.Fatal(err)
		}
		return body
	}

	prior := submit(" unit ", " pass ")
	before := stored(prior.ReportID)
	if !strings.Contains(before, `"name":" unit "`) || !strings.Contains(before, `"status":" pass "`) {
		t.Fatalf("fixture lost historical padding: %s", before)
	}
	_, checks := read()
	if len(checks) != 1 || checks[0].Name != "unit" || checks[0].Status != "pass" {
		t.Errorf("historical check projection = %+v", checks)
	}
	if err := s.store.DecideDeliveryWithBasis(ctx, workspace, work.Key, "reject", "reviewer", "request another run", prior.ID, ""); err != nil {
		t.Fatal(err)
	}
	submit("unit", "pass")
	comparison, _ := read()
	if comparison.State != "available" || comparison.Prior == nil || comparison.Prior.ReviewID != prior.ID || len(comparison.ChangedChecks)+len(comparison.ChangedCriteria) != 0 {
		t.Errorf("equivalent check changed the comparison: %+v", comparison)
	}
	if after := stored(prior.ReportID); after != before {
		t.Fatalf("comparison rewrote historical report: before=%s after=%s", before, after)
	}
}

func TestLocalUIComparisonDetectsChangedURLWithoutPublishingHistoricalURL(t *testing.T) {
	s, work := localUITestServer(t)
	ctx, workspace := t.Context(), s.selection.Workspace.ID
	for _, target := range []string{"https://example.test/old?secret=private-history", "https://example.test/new"} {
		review, err := s.store.SubmitDelivery(ctx, workspace, work.Key, map[string]any{
			"context_digest": work.ContextDigest, "agent": map[string]any{"name": "builder"},
			"criteria": []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"kind": "url", "url": target}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(target, "private-history") {
			if err := s.store.DecideDeliveryWithBasis(ctx, workspace, work.Key, "reject", "reviewer", "update link", review.ID, ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	response := localUIRequest(s, "GET", "/api/work?ref="+work.Key, "", true, false)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"changed_criteria":["local-1"]`) {
		t.Fatalf("URL change absent from comparison: %d", response.Code)
	}
	if strings.Contains(response.Body.String(), "private-history") {
		t.Fatal("historical credential reached the work response")
	}
}

func TestLocalUIReportsMissingRequiredSelectedTestsFromPinnedContract(t *testing.T) {
	s, _ := localUITestServer(t)
	ctx, workspace := t.Context(), s.selection.Workspace.ID
	work, err := s.store.CreateQuickWork(ctx, workspace, local.QuickWorkInput{Title: "Required test", AcceptanceCriteria: []string{"Preserved behavior @check:unit"}})
	if err != nil {
		t.Fatal(err)
	}
	check := local.VerificationCheck{Name: "unit", Command: "run unit", Cwd: ".", TestReport: &local.JUnitTestReport{Format: "junit", Selectors: map[string][]local.SelectedTestCase{
		"local-1": {{ClassName: "suite", Name: "required case"}},
	}}}
	contract, err := s.store.PinVerificationContract(ctx, workspace, work.Key, s.repoRoot, "reviewer", local.VerificationContractInput{ContextDigest: work.ContextDigest, Shell: "sh", Checks: []local.VerificationCheck{check}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.store.SubmitDelivery(ctx, workspace, work.Key, map[string]any{
		"context_digest": work.ContextDigest, "verification_contract_digest": contract.Digest, "agent": map[string]any{"name": "builder"},
		"checks":   []any{map[string]any{"name": check.Name, "command": check.Command, "cwd": check.Cwd, "status": "fail", "source": "specgate_cli", "detail": "executed by specgate: exit 0 — selected-test report: missing required report"}},
		"criteria": []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "preservation behavior"}}},
	}, s.repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	response := localUIRequest(s, "GET", "/api/work?ref="+work.Key, "", true, false)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"selected_tests_required":["local-1"]`) || !strings.Contains(response.Body.String(), `"selected_tests_required":true`) || !strings.Contains(response.Body.String(), "missing required report") {
		t.Fatalf("required selected test or diagnostic absent from %d: %s", response.Code, response.Body.String())
	}
}
