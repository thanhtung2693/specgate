package local

import (
	"fmt"
	"strings"
)

// ProjectAcceptanceEvidence keeps reviewer-facing facts separate from raw
// completion prose and command output. Missing rows remain explicit gaps.
func ProjectAcceptanceEvidence(criteria []string, body map[string]any, contracts ...VerificationContract) ([]AcceptanceCriterionEvidence, []string) {
	rows, _ := body["criteria"].([]any)
	checks, _ := body["checks"].([]any)
	byID := map[string]map[string]any{}
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if id, _ := row["criterion_id"].(string); id != "" {
			byID[id] = row
		}
	}
	result := make([]AcceptanceCriterionEvidence, 0, len(criteria))
	var gaps []string
	for index, raw := range criteria {
		id := fmt.Sprintf("local-%d", index+1)
		text, binding := ParseAcceptanceCriterionBinding(raw)
		item := AcceptanceCriterionEvidence{ID: id, Text: text, Claim: "missing", CheckName: binding}
		if row, ok := byID[id]; ok {
			if claim, _ := row["claim"].(string); claim != "" {
				item.Claim = claim
			}
			if evidence, _ := row["evidence"].(map[string]any); evidence != nil {
				item.EvidenceKind, _ = evidence["kind"].(string)
				item.EvidencePath, _ = evidence["path"].(string)
				if heading, _ := evidence["heading"].(string); heading != "" {
					item.Citation = "heading: " + heading
				} else if line, ok := evidence["line"].(float64); ok {
					item.Citation = fmt.Sprintf("line: %.0f", line)
				}
				if grounding, _ := evidence["grounding"].(map[string]any); grounding != nil {
					item.GroundingStatus, _ = grounding["status"].(string)
					if status := item.GroundingStatus; status != "" && status != "grounded" {
						item.Gaps = append(item.Gaps, "citation: "+status)
					}
				} else if item.Citation != "" || item.EvidencePath != "" {
					item.GroundingStatus = "unverified"
					item.Gaps = append(item.Gaps, "citation: unverified")
				}
			}
		}
		if item.Claim != "satisfied" {
			item.Gaps = append(item.Gaps, "claim: "+item.Claim)
		}
		if binding != "" {
			for _, raw := range checks {
				check, _ := raw.(map[string]any)
				if name, _ := check["name"].(string); name != binding {
					continue
				}
				item.CheckStatus, _ = check["status"].(string)
				item.CheckSource, _ = check["source"].(string)
				if item.CheckSource == "" {
					item.CheckSource = "agent_attested"
				}
				item.CheckProvenance = item.CheckSource
				if run, _ := check["test_run"].(map[string]any); run != nil {
					item.RunFreshness, _ = run["freshness"].(string)
					if item.RunFreshness != "matching_endpoints" {
						item.Gaps = append(item.Gaps, "verification run freshness: "+item.RunFreshness)
					}
				}
				if observation, _ := check["test_observation"].(map[string]any); observation != nil {
					item.ReportDigest, _ = observation["report_digest"].(string)
					var selectors []SelectedTestCase
					if len(contracts) > 0 {
						for _, pinned := range contracts[0].Checks {
							if pinned.Name == binding && pinned.TestReport != nil {
								selectors = pinned.TestReport.Selectors[id]
								break
							}
						}
						if item.CheckSource == "specgate_cli" && len(selectors) > 0 {
							item.CheckProvenance = "selected_test_observed"
						}
					}
					if selected, _ := observation["selected"].([]any); selected != nil {
						for _, raw := range selected {
							caseRow, _ := raw.(map[string]any)
							className, _ := caseRow["classname"].(string)
							name, _ := caseRow["name"].(string)
							outcome, _ := caseRow["outcome"].(string)
							matched := false
							for _, selector := range selectors {
								if selector.ClassName == className && selector.Name == name {
									matched = true
								}
							}
							if !matched {
								continue
							}
							item.Selected = append(item.Selected, JUnitCaseObservation{ClassName: className, Name: name, Outcome: outcome})
							if outcome != "passed" {
								item.Gaps = append(item.Gaps, "selected test "+className+"/"+name+": "+outcome)
							}
						}
					}
					for _, selector := range selectors {
						found := false
						for _, selected := range item.Selected {
							if selected.ClassName == selector.ClassName && selected.Name == selector.Name {
								found = true
							}
						}
						if !found {
							item.Gaps = append(item.Gaps, "selected test "+selector.ClassName+"/"+selector.Name+": missing")
						}
					}
				}
				if item.CheckSource == "specgate_cli" && item.CheckProvenance != "selected_test_observed" {
					item.CheckProvenance = "command_only"
				}
				break
			}
			if item.CheckStatus != "pass" {
				item.Gaps = append(item.Gaps, "bound check "+binding+": "+strings.TrimSpace(item.CheckStatus))
			}
		}
		if item.Claim == "satisfied" && item.EvidenceKind == "" && item.EvidencePath == "" && item.Citation == "" && (item.CheckStatus == "" || item.CheckStatus == "fail") {
			item.Gaps = append(item.Gaps, "evidence: missing")
		}
		for _, gap := range item.Gaps {
			gaps = append(gaps, id+" "+gap)
		}
		result = append(result, item)
	}
	return result, gaps
}
