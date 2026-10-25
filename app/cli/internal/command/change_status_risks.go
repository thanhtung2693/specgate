package command

import "strings"

type acceptanceRiskCategory struct {
	Category string   `json:"category"`
	Count    int      `json:"count"`
	Criteria []string `json:"criteria,omitempty"`
	Details  []string `json:"details,omitempty"`
}

// These are issue counts, not a confidence score. Source coverage is shown
// separately because feature inventory outside this work is not a failed AC.
func acceptanceRisks(status changeStatusResult) []acceptanceRiskCategory {
	risks := []acceptanceRiskCategory{{Category: "failed"}, {Category: "missing"}, {Category: "stale"}, {Category: "unknown"}}
	add := func(category, criterion, detail string) {
		for i := range risks {
			if risks[i].Category != category {
				continue
			}
			risks[i].Count++
			risks[i].Details = append(risks[i].Details, detail)
			if criterion != "" {
				found := false
				for _, id := range risks[i].Criteria {
					if id == criterion {
						found = true
					}
				}
				if !found {
					risks[i].Criteria = append(risks[i].Criteria, criterion)
				}
			}
			return
		}
	}
	for _, row := range status.CriterionEvidence {
		if row.Claim == "missing" {
			add("missing", row.ID, row.ID+": no recorded claim")
		}
		failedSelected := map[string]bool{}
		for _, selected := range row.Selected {
			if selected.Outcome == "failed" {
				failedSelected["selected test "+selected.ClassName+"/"+selected.Name+": failed"] = true
			}
		}
		for _, gap := range row.Gaps {
			category := "missing"
			// Names and paths are data, not evidence of a failed or stale outcome.
			switch {
			case failedSelected[gap]:
				category = "failed"
			case row.CheckStatus == "fail" && gap == "bound check "+row.CheckName+": fail":
				category = "failed"
			case row.RunFreshness == "changed" && gap == "verification run freshness: changed":
				category = "stale"
			case row.RunFreshness != "" && row.RunFreshness != "matching_endpoints" && gap == "verification run freshness: "+row.RunFreshness:
				category = "unknown"
			case row.GroundingStatus == "unverified" && gap == "citation: unverified":
				category = "unknown"
			}
			add(category, row.ID, row.ID+": "+gap)
		}
		if row.CheckProvenance == "agent_attested" || row.CheckProvenance == "command_only" {
			add("unknown", row.ID, row.ID+": check provenance is "+row.CheckProvenance)
		}
	}
	if status.CompletionID == "" {
		add("missing", "", "delivery evidence has not been submitted")
	}
	if status.Stale {
		add("stale", "", "current checkout or peer review differs from stored evidence")
	}
	if status.PeerState == "failed" {
		add("failed", "", "peer review found gaps")
	}
	if status.AcceptanceBasis != nil {
		for _, watched := range status.AcceptanceBasis.Watched {
			if watched.State == "changed" {
				add("stale", "", "watched input "+watched.Path+" changed")
			} else if watched.State != "unchanged" {
				add("unknown", "", "watched input "+watched.Path+": "+watched.State)
			}
		}
	}
	if status.SelectedCheckpointDelta != nil && (status.SelectedCheckpointDelta.State == "unavailable" || status.SelectedCheckpointDelta.State == "noncomparable") {
		add("unknown", "", "selected checkpoint comparison: "+status.SelectedCheckpointDelta.State)
	}
	if status.SelectedImpact != nil {
		if status.SelectedImpact.RequirementImpact == "unavailable" {
			add("unknown", "", "selected artifact requirement impact: "+status.SelectedImpact.RequirementReason)
		}
		if status.SelectedImpact.SourceOverlapState == "unknown" || status.SelectedImpact.PathOverlapState == "unknown" || status.SelectedImpact.PathOverlapState == "noncomparable" {
			add("unknown", "", "selected artifact overlap evidence is "+status.SelectedImpact.SourceOverlapState+"/"+status.SelectedImpact.PathOverlapState)
		}
	}
	unverifiedFreshness := status.FreshnessUnchecked || strings.HasPrefix(status.Receipt, "No Git receipt") || strings.HasPrefix(status.Receipt, "Git receipt unavailable") || strings.Contains(status.Freshness, "not checked")
	if status.AcceptanceBasis != nil && status.AcceptanceBasis.Freshness != "" {
		unverifiedFreshness = status.AcceptanceBasis.Freshness == "unavailable" || status.AcceptanceBasis.Freshness == "noncomparable"
	}
	if unverifiedFreshness {
		add("unknown", "", "current-checkout provenance unavailable or not verified")
	}
	return risks
}
