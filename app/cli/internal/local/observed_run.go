package local

import (
	"encoding/json"
	"fmt"
	"strings"
)

func validateObservedRunVersions(body map[string]any) error {
	checks, _ := body["checks"].([]any)
	for _, raw := range checks {
		check, _ := raw.(map[string]any)
		if run, found := check["test_run"]; found {
			encoded, err := json.Marshal(run)
			if err != nil {
				return err
			}
			var receipt ObservedRun
			if err := json.Unmarshal(encoded, &receipt); err != nil {
				return err
			}
			if receipt.Version != 1 {
				return fmt.Errorf("%w: observed run version %d", ErrStoreIncompatible, receipt.Version)
			}
		}
	}
	return nil
}

// ObservedRun describes local endpoint observations, not continuous execution
// attestation. Only the confirmed CLI executor supplies it at ingress.
type ObservedRun struct {
	Version            int                `json:"version"`
	ID                 string             `json:"id"`
	WorkID             string             `json:"work_id"`
	ContextDigest      string             `json:"context_digest"`
	VerificationDigest string             `json:"verification_digest"`
	CompletionID       string             `json:"completion_id"`
	CheckName          string             `json:"check_name"`
	ExitCode           int                `json:"exit_code"`
	StartedAt          string             `json:"started_at"`
	FinishedAt         string             `json:"finished_at"`
	Before             CheckoutSnapshot   `json:"before"`
	After              CheckoutSnapshot   `json:"after"`
	WatchedBefore      []WatchedPathDrift `json:"watched_before"`
	WatchedAfter       []WatchedPathDrift `json:"watched_after"`
	Freshness          string             `json:"freshness"`
}

func bindObservedChecks(body map[string]any, contract VerificationContract, completionID string) {
	checks, _ := body["checks"].([]any)
	for _, pinned := range contract.Checks {
		if pinned.TestReport == nil {
			continue
		}
		for _, raw := range checks {
			check, _ := raw.(map[string]any)
			if check["name"] != pinned.Name {
				continue
			}
			var run ObservedRun
			var observation JUnitObservation
			rawRun, _ := json.Marshal(check["test_run"])
			rawObservation, _ := json.Marshal(check["test_observation"])
			valid := json.Unmarshal(rawRun, &run) == nil && json.Unmarshal(rawObservation, &observation) == nil &&
				run.Version == 1 && run.ID != "" && run.WorkID == contract.WorkID && run.ContextDigest == contract.ContextDigest && run.VerificationDigest == contract.Digest && run.CheckName == pinned.Name &&
				check["source"] == "specgate_cli" && observation.ReportDigest != "" && observation.TotalCases > 0
			seen := map[SelectedTestCase]string{}
			for _, c := range observation.Selected {
				key := SelectedTestCase{ClassName: c.ClassName, Name: c.Name}
				if _, ok := seen[key]; ok {
					valid = false
				}
				seen[key] = c.Outcome
			}
			for _, selectors := range pinned.TestReport.Selectors {
				for _, selector := range selectors {
					if seen[selector] != "passed" {
						valid = false
					}
				}
			}
			if !valid || !observation.Passing || run.ExitCode != 0 || run.Freshness == "changed" {
				check["status"] = "fail"
				if !valid {
					detail, _ := check["detail"].(string)
					if check["source"] != "specgate_cli" || !strings.Contains(detail, " — selected-test report: ") {
						check["detail"] = "selected-test observation missing, insufficient, or bound to another run"
					}
				}
			}
			if run.ID != "" {
				run.CompletionID = completionID
				check["test_run"] = run
			}
		}
	}
}
