package command

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/specgate/specgate/app/cli/internal/local"
)

type localUIReviewCheck struct {
	Name                  string                       `json:"name"`
	Command               string                       `json:"command,omitempty"`
	Cwd                   string                       `json:"cwd,omitempty"`
	Status                string                       `json:"status,omitempty"`
	ClaimedStatus         string                       `json:"claimed_status,omitempty"`
	Source                string                       `json:"source,omitempty"`
	Detail                string                       `json:"detail,omitempty"`
	RunFreshness          string                       `json:"run_freshness,omitempty"`
	Selected              []local.JUnitCaseObservation `json:"selected_cases,omitempty"`
	SelectedTestsRequired bool                         `json:"selected_tests_required,omitempty"`
}

func localUIReviewChecks(body map[string]any, contract local.VerificationContract) []localUIReviewCheck {
	required := make(map[string]bool)
	for _, check := range contract.Checks {
		if check.TestReport != nil {
			required[check.Name] = true
		}
	}
	rows, _ := body["checks"].([]any)
	checks := make([]localUIReviewCheck, 0, len(rows))
	for _, row := range rows {
		raw, ok := row.(map[string]any)
		if !ok {
			continue
		}
		check := localUIReviewCheck{
			Name: strings.TrimSpace(reviewString(raw, "name")), Command: reviewString(raw, "command"), Cwd: reviewString(raw, "cwd"),
			Status: strings.TrimSpace(reviewString(raw, "status")), ClaimedStatus: reviewString(raw, "claimed_status"),
			Source: checkSource(raw["source"]), Detail: reviewString(raw, "detail"),
		}
		check.SelectedTestsRequired = required[check.Name]
		if check.Source == "specgate_cli" {
			run, _ := raw["test_run"].(map[string]any)
			check.RunFreshness = reviewString(run, "freshness")
			observation, _ := raw["test_observation"].(map[string]any)
			cases, _ := observation["selected"].([]any)
			for _, selected := range cases {
				if item, ok := selected.(map[string]any); ok {
					check.Selected = append(check.Selected, local.JUnitCaseObservation{
						ClassName: reviewString(item, "classname"), Name: reviewString(item, "name"), Outcome: reviewString(item, "outcome"),
					})
				}
			}
		}
		checks = append(checks, check)
	}
	return checks
}

func reviewString(body map[string]any, key string) string {
	value, _ := body[key].(string)
	return value
}

type localUIReviewBaseline struct {
	ReviewID     string `json:"review_id"`
	CompletionID string `json:"completion_id"`
	Decision     string `json:"decision"`
	Verdict      string `json:"verdict"`
	Note         string `json:"note"`
	Summary      string `json:"summary,omitempty"`
}

type localUIReviewComparison struct {
	State           string                          `json:"state"`
	ReviewID        string                          `json:"review_id"`
	CompletionID    string                          `json:"completion_id"`
	Prior           *localUIReviewBaseline          `json:"prior,omitempty"`
	SummaryChanged  bool                            `json:"summary_changed"`
	ChangedCriteria []string                        `json:"changed_criteria,omitempty"`
	ChangedChecks   []string                        `json:"changed_checks,omitempty"`
	PriorCriteria   map[string]map[string]any       `json:"prior_criteria,omitempty"`
	PriorChecks     map[string][]localUIReviewCheck `json:"prior_checks,omitempty"`
}

func (s *localUIServer) compareReviews(ctx context.Context, current *local.AcceptanceInspection, checks []localUIReviewCheck) *localUIReviewComparison {
	view := &localUIReviewComparison{State: "unavailable", ReviewID: current.Review.ID, CompletionID: current.Report.ID}
	prior, err := s.store.PreviousDecidedReview(ctx, s.selection.Workspace.ID, current.Review)
	if errors.Is(err, sql.ErrNoRows) {
		view.State = "no_baseline"
		return view
	}
	if err != nil {
		return view
	}
	view.Prior = &localUIReviewBaseline{
		ReviewID: prior.ID, CompletionID: prior.ReportID, Decision: prior.HumanDecision, Verdict: prior.Verdict, Note: prior.Note,
	}
	report, err := s.store.DeliveryReportForReview(ctx, s.selection.Workspace.ID, prior)
	if err != nil || !localUIComparableReport(report.Body, len(current.Work.AcceptanceCriteria)) {
		return view
	}
	view.State = "available"
	view.Prior.Summary = reviewString(report.Body, "summary")
	view.SummaryChanged = view.Prior.Summary != reviewString(current.Report.Body, "summary")
	before := localUIReviewCriteria(current.Work.AcceptanceCriteria, report, current.Contract)
	after := localUIReviewCriteria(current.Work.AcceptanceCriteria, current.Report, current.Contract)
	view.PriorCriteria = make(map[string]map[string]any)
	for i := range current.Work.AcceptanceCriteria {
		id := fmt.Sprintf("local-%d", i+1)
		if !reflect.DeepEqual(before[id], after[id]) {
			view.ChangedCriteria = append(view.ChangedCriteria, id)
			delete(before[id], "url_digest")
			view.PriorCriteria[id] = before[id]
		}
	}
	previousChecks := localUIReviewChecks(report.Body, current.Contract)
	beforeChecks, afterChecks := localUICheckSignatures(previousChecks), localUICheckSignatures(checks)
	for name, signature := range beforeChecks {
		if !slices.Equal(signature, afterChecks[name]) {
			view.ChangedChecks = append(view.ChangedChecks, name)
		}
	}
	for name := range afterChecks {
		if _, existed := beforeChecks[name]; !existed {
			view.ChangedChecks = append(view.ChangedChecks, name)
		}
	}
	slices.Sort(view.ChangedChecks)
	view.PriorChecks = make(map[string][]localUIReviewCheck)
	for _, check := range previousChecks {
		if slices.Contains(view.ChangedChecks, check.Name) {
			view.PriorChecks[check.Name] = append(view.PriorChecks[check.Name], check)
		}
	}
	return view
}

func localUIComparableReport(body map[string]any, criterionCount int) bool {
	if body == nil || !reviewStringFields(body, "summary") {
		return false
	}
	ids := make(map[string]bool, criterionCount)
	for i := 1; i <= criterionCount; i++ {
		ids[fmt.Sprintf("local-%d", i)] = true
	}
	if !reviewObjectRows(body, "criteria", func(row map[string]any) bool {
		id := reviewString(row, "criterion_id")
		if !ids[id] || !reviewStringFields(row, "summary", "detail", "explanation", "note") {
			return false
		}
		delete(ids, id)
		switch reviewString(row, "claim") {
		case "satisfied", "partial", "not_done":
		default:
			return false
		}
		if raw, present := row["evidence"]; present {
			evidence, ok := raw.(map[string]any)
			if !ok || !reviewStringFields(evidence, "kind", "path", "heading", "file_key", "url") {
				return false
			}
			for _, key := range []string{"line", "end_line"} {
				if raw, present := evidence[key]; present {
					if _, ok := raw.(float64); !ok {
						return false
					}
				}
			}
			if raw, present := evidence["grounding"]; present {
				grounding, ok := raw.(map[string]any)
				return ok && reviewStringFields(grounding, "status", "digest")
			}
		}
		return true
	}) {
		return false
	}
	return reviewObjectRows(body, "checks", func(row map[string]any) bool {
		switch strings.TrimSpace(reviewString(row, "status")) {
		case "pass", "fail", "skipped":
		default:
			return false
		}
		if !reviewStringFields(row, "name", "command", "cwd", "claimed_status", "source", "detail") {
			return false
		}
		if raw, present := row["test_run"]; present {
			run, ok := raw.(map[string]any)
			if !ok || !reviewStringFields(run, "freshness") {
				return false
			}
		}
		if raw, present := row["test_observation"]; present {
			observation, ok := raw.(map[string]any)
			return ok && reviewObjectRows(observation, "selected", func(selected map[string]any) bool {
				return reviewString(selected, "classname") != "" && reviewString(selected, "name") != "" &&
					slices.Contains([]string{"passed", "failed", "skipped"}, reviewString(selected, "outcome"))
			})
		}
		return true
	})
}

func reviewObjectRows(body map[string]any, key string, valid func(map[string]any) bool) bool {
	raw, present := body[key]
	if !present {
		return true
	}
	rows, ok := raw.([]any)
	if !ok {
		return false
	}
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok || !valid(row) {
			return false
		}
	}
	return true
}

func reviewStringFields(body map[string]any, keys ...string) bool {
	for _, key := range keys {
		if raw, present := body[key]; present {
			if _, ok := raw.(string); !ok {
				return false
			}
		}
	}
	return true
}

func localUIReviewCriteria(criteria []string, report local.DeliveryReport, contract local.VerificationContract) map[string]map[string]any {
	rows, _ := report.Body["criteria"].([]any)
	byID := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		if raw, ok := row.(map[string]any); ok {
			byID[reviewString(raw, "criterion_id")] = raw
		}
	}
	verdicts, _ := localReportEvidence(criteria, report)
	evidence, _ := local.ProjectAcceptanceEvidence(criteria, report.Body, contract)
	result := make(map[string]map[string]any, len(evidence))
	for i, row := range evidence {
		raw := byID[row.ID]
		citation, _ := raw["evidence"].(map[string]any)
		grounding, _ := citation["grounding"].(map[string]any)
		selected := row.Selected
		sortReviewCases(selected)
		material := map[string]any{
			"claim": row.Claim, "verdict": verdicts[i].Verdict, "check_name": row.CheckName, "check_status": row.CheckStatus,
			"check_provenance": row.CheckProvenance, "selected_cases": selected,
			"evidence_kind": row.EvidenceKind, "evidence_path": row.EvidencePath, "citation": row.Citation,
			"grounding_status": row.GroundingStatus, "grounding_digest": reviewString(grounding, "digest"),
		}
		for _, key := range []string{"summary", "detail", "explanation", "note"} {
			if value := reviewString(raw, key); value != "" {
				material[key] = value
			}
		}
		for _, key := range []string{"line", "end_line", "file_key"} {
			if value, ok := citation[key]; ok {
				material[key] = value
			}
		}
		if target := reviewString(citation, "url"); target != "" {
			material["url_digest"] = sha256.Sum256([]byte(target))
		}
		result[row.ID] = material
	}
	return result
}

func localUICheckSignatures(checks []localUIReviewCheck) map[string][]string {
	groups := make(map[string][]string)
	for _, check := range checks {
		check.Selected = slices.Clone(check.Selected)
		sortReviewCases(check.Selected)
		if check.Source == "specgate_cli" && check.Status == "pass" {
			// A successful command can include an arbitrary stdout tail such as shell timing.
			if prefix, _, ok := strings.Cut(check.Detail, " — "); ok {
				if code, found := strings.CutPrefix(prefix, "executed by specgate: exit "); found {
					if _, err := strconv.Atoi(code); err == nil {
						check.Detail = prefix
					}
				}
			}
		}
		encoded, _ := json.Marshal(check)
		groups[check.Name] = append(groups[check.Name], string(encoded))
	}
	for signature := range maps.Values(groups) {
		slices.Sort(signature)
	}
	return groups
}

func sortReviewCases(cases []local.JUnitCaseObservation) {
	slices.SortFunc(cases, func(a, b local.JUnitCaseObservation) int {
		if group := strings.Compare(a.ClassName, b.ClassName); group != 0 {
			return group
		}
		if name := strings.Compare(a.Name, b.Name); name != 0 {
			return name
		}
		return strings.Compare(a.Outcome, b.Outcome)
	})
}
