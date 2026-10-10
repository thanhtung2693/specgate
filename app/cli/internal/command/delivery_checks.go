package command

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

// executeCompletionChecks re-runs each checks[].command locally and replaces the
// claimed status with the observed result, converting narrated checks into
// executed checks. Skipped checks and checks without an explicit command are
// untouched. The corrected body is submitted either way — an observed failure
// is honest data for the delivery review, which already fails the verdict on any
// failed check.
// Observation metadata belongs to this execution, never to the input file.
// Clear it once at ingress, not during validation after --run-checks.
func clearCheckObservations(body map[string]any) {
	checks, _ := body["checks"].([]any)
	for _, raw := range checks {
		entry, _ := raw.(map[string]any)
		delete(entry, "source")
		delete(entry, "claimed_status")
		delete(entry, "test_observation")
		delete(entry, "test_run")
	}
}

func rejectFullCheckExtensions(deps *Deps, op string, body map[string]any) error {
	if deps.Topology == config.ModeLocal {
		return nil
	}
	checks, _ := body["checks"].([]any)
	for _, raw := range checks {
		check, _ := raw.(map[string]any)
		for _, field := range []string{"test_report", "test_observation", "test_run"} {
			if _, found := check[field]; found {
				return incompatibleCommand(deps, op, field+" is Local-only")
			}
		}
	}
	return nil
}

func executeCompletionChecks(ctx context.Context, deps *Deps, body map[string]any, contract *local.VerificationContract, repoRoots ...string) {
	runner := deps.RunCheckCommand
	if runner == nil {
		runner = defaultRunCheckCommand
	}
	checks, _ := body["checks"].([]any)
	for _, raw := range checks {
		entry, _ := raw.(map[string]any)
		if entry == nil {
			continue
		}
		command, _ := entry["command"].(string)
		claimed, _ := entry["status"].(string)
		command = strings.TrimSpace(command)
		if command == "" || claimed == "skipped" {
			continue
		}
		executable := command
		var report *local.JUnitTestReport
		if contract != nil {
			for _, pinned := range contract.Checks {
				if pinned.Name == entry["name"] {
					report = pinned.TestReport
					break
				}
			}
		}
		var reportPath, runDir string
		var runRoot *os.Root
		if len(repoRoots) > 0 {
			cwd, _ := entry["cwd"].(string)
			_, dir, err := local.ResolveVerificationCwd(repoRoots[0], cwd)
			if err != nil {
				entry["status"] = "fail"
				entry["detail"] = err.Error()
				continue
			}
			executable = "cd " + shellQuote(dir) + " && " + command
			if report != nil {
				var err error
				runDir, reportPath, err = prepareJUnitOutput(ctx, deps, repoRoots[0])
				if err != nil {
					entry["status"] = "fail"
					entry["detail"] = err.Error()
					continue
				}
				executable = "export SPECGATE_TEST_REPORT=" + shellQuote(reportPath) + "; " + executable
				runRoot, err = os.OpenRoot(runDir)
				if err != nil {
					_ = os.Remove(runDir)
					entry["status"] = "fail"
					entry["detail"] = err.Error()
					continue
				}
			}
		}
		var run local.ObservedRun
		if report != nil {
			run = local.ObservedRun{Version: 1, ID: filepath.Base(runDir), WorkID: contract.WorkID, ContextDigest: contract.ContextDigest, VerificationDigest: contract.Digest, CheckName: fmt.Sprint(entry["name"]), StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Before: captureCheckout(ctx, deps)}
			if len(repoRoots) > 0 {
				run.WatchedBefore = local.WatchedPathDriftFor(repoRoots[0], contract.WatchedPaths)
			}
		}
		exitCode, combined := runner(ctx, executable)
		observed := "pass"
		if exitCode != 0 {
			observed = "fail"
		}
		detail := fmt.Sprintf("executed by specgate: exit %d", exitCode)
		if tail := lastOutputLine(combined); report == nil && tail != "" {
			detail += " — " + tail
		}
		entry["status"] = observed
		entry["detail"] = detail
		entry["source"] = "specgate_cli"
		if report != nil {
			run.ExitCode = exitCode
			run.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
			run.After = captureCheckout(ctx, deps)
			run.Freshness = "unavailable"
			if len(repoRoots) > 0 {
				run.WatchedAfter = local.WatchedPathDriftFor(repoRoots[0], contract.WatchedPaths)
			}
			if run.Before.State == "available" && run.After.State == "available" {
				run.Freshness = "matching_endpoints"
				if run.Before.Fingerprint != run.After.Fingerprint {
					run.Freshness = "changed"
				}
			}
			if run.Before.Reason == "checkout changed during capture" || run.After.Reason == "checkout changed during capture" {
				run.Freshness = "changed"
			}
			entry["test_run"] = run
			if run.Freshness == "changed" {
				observed = "fail"
				entry["status"] = "fail"
				entry["detail"] = detail + " — checkout changed during verification"
			}
			observation, err := local.ObserveJUnitReportFromRoot(runRoot, "report.xml", junitSelectorsForCheck(report))
			_ = runRoot.Remove("report.xml")
			_ = runRoot.Close()
			_ = os.Remove(runDir) // Remove only when empty; preserve unrelated runner output.
			if err != nil {
				observed = "fail"
				entry["status"] = observed
				entry["detail"] = detail + " — selected-test report: " + err.Error()
			} else {
				entry["test_observation"] = observation
				if !observation.Passing {
					observed = "fail"
					entry["status"] = observed
					entry["detail"] = detail + " — selected JUnit testcase failed or skipped"
				}
			}
		}
		// Keep the superseded claim so the stored report — and the delivery
		// receipt read back from it — shows what re-execution corrected.
		if claimed = strings.TrimSpace(claimed); claimed != "" && observed != claimed {
			entry["claimed_status"] = claimed
		}
		if deps.Printer.Mode() != output.ModeJSON {
			note := ""
			if observed != claimed {
				note = fmt.Sprintf(" (reported %q)", claimed)
			}
			fmt.Fprintf(deps.Stderr, "Executed check %q → %s%s\n", command, observed, note)
		}
	}
}

func prepareJUnitOutput(ctx context.Context, deps *Deps, root string) (runDir, reportPath string, err error) {
	base := filepath.Join(root, ".specgate")
	if err = config.EnsureSpecgateDirGitignore(base); err != nil {
		return "", "", fmt.Errorf("cannot create ignored SpecGate run directory: %w", err)
	}
	runDir, err = os.MkdirTemp(base, "junit-run-")
	if err != nil {
		return "", "", err
	}
	if !gitIgnoresPath(ctx, deps, runDir) {
		_ = os.Remove(runDir)
		return "", "", fmt.Errorf("JUnit output directory must be Git-ignored in a repository")
	}
	return runDir, filepath.Join(runDir, "report.xml"), nil
}

func junitSelectorsForCheck(report *local.JUnitTestReport) []local.SelectedTestCase {
	var selectors []local.SelectedTestCase
	if report == nil {
		return nil
	}
	seen := map[local.SelectedTestCase]bool{}
	for _, values := range report.Selectors {
		for _, value := range values {
			if !seen[value] {
				selectors = append(selectors, value)
				seen[value] = true
			}
		}
	}
	sort.Slice(selectors, func(i, j int) bool {
		if selectors[i].ClassName != selectors[j].ClassName {
			return selectors[i].ClassName < selectors[j].ClassName
		}
		return selectors[i].Name < selectors[j].Name
	})
	return selectors
}
