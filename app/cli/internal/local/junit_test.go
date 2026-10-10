package local_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/local"
)

// Parser fixtures open their own root; production keeps the pre-run handle.
func observeJUnitFixture(path string, selectors []local.SelectedTestCase) (local.JUnitObservation, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return local.JUnitObservation{}, err
	}
	defer root.Close()
	return local.ObserveJUnitReportFromRoot(root, filepath.Base(path), selectors)
}

func TestJUnitReportLimitsAndIncompleteInputsNeverPass(t *testing.T) {
	var many strings.Builder
	many.WriteString("<testsuite>")
	for i := 0; i < 100001; i++ {
		fmt.Fprintf(&many, `<testcase classname="pkg" name="case-%d"/>`, i)
	}
	many.WriteString("</testsuite>")
	for _, tc := range []struct {
		name, body string
		absent     bool
	}{
		{"missing", "", true}, {"empty", "", false},
		{"zero-cases", "<testsuite/>", false},
		{"truncated", `<testsuite><testcase classname="pkg" name="case-0">`, false},
		{"selected-skip", `<testsuite><testcase classname="pkg" name="case-0"><skipped/></testcase></testsuite>`, false},
		{"byte-limit", strings.Repeat(" ", (16<<20)+1), false},
		{"case-limit", many.String(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "report.xml")
			if !tc.absent {
				if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			observation, err := observeJUnitFixture(path, []local.SelectedTestCase{{ClassName: "pkg", Name: "case-0"}})
			if err == nil && observation.Passing {
				t.Fatal("incomplete or over-limit report passed")
			}
			if tc.name == "selected-skip" && (err != nil || len(observation.Selected) != 1 || observation.Selected[0].Outcome != "skipped") {
				t.Fatalf("skip not retained: %+v %v", observation, err)
			}
		})
	}
}

func TestObserveJUnitReportRequiresEverySelectedCaseExactlyOnceAndPassing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.xml")
	if err := os.WriteFile(path, []byte(`<testsuite><testcase classname="pkg" name="passes"/><testcase classname="pkg" name="fails"><failure/></testcase></testsuite>`), 0o600); err != nil {
		t.Fatal(err)
	}
	selectors := []local.SelectedTestCase{{ClassName: "pkg", Name: "passes"}, {ClassName: "pkg", Name: "fails"}}
	observation, err := observeJUnitFixture(path, selectors)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Passing || observation.TotalCases != 2 || observation.Selected[1].Outcome != "failed" {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestObserveJUnitReportRejectsMissingDuplicateAndUnsafeXML(t *testing.T) {
	for _, report := range []string{
		`<testsuite><testcase classname="pkg" name="other"/></testsuite>`,
		`<testsuite><testcase classname="pkg" name="wanted"/><testcase classname="pkg" name="wanted"/></testsuite>`,
		`<!DOCTYPE suite [<!ENTITY xxe SYSTEM "file:///etc/passwd">]><testsuite/>`,
		`<arbitrary><testcase classname="pkg" name="wanted"/></arbitrary>`,
		`<testsuite><testcase classname="pkg" name="wanted"><flakyFailure/></testcase></testsuite>`,
		`<testsuite><testcase classname="pkg" name="wanted"><failure/><skipped/></testcase></testsuite>`,
	} {
		path := filepath.Join(t.TempDir(), "report.xml")
		if err := os.WriteFile(path, []byte(report), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := observeJUnitFixture(path, []local.SelectedTestCase{{ClassName: "pkg", Name: "wanted"}}); err == nil {
			t.Fatalf("accepted unsafe report %q", report)
		}
	}
}

func TestObserveJUnitReportFailsOnUnselectedFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.xml")
	if err := os.WriteFile(path, []byte(`<testsuite><testcase classname="pkg" name="selected"/><testcase classname="pkg" name="broken"><failure/></testcase></testsuite>`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := observeJUnitFixture(path, []local.SelectedTestCase{{ClassName: "pkg", Name: "selected"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Passing {
		t.Fatal("unselected failure reported passing")
	}
}

func TestObserveJUnitReportCountsUnselectedSkippedCases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.xml")
	if err := os.WriteFile(path, []byte(`<testsuite><testcase classname="pkg" name="selected"/><testcase classname="pkg" name="optional"><skipped/></testcase></testsuite>`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := observeJUnitFixture(path, []local.SelectedTestCase{{ClassName: "pkg", Name: "selected"}})
	if err != nil || !got.Passing || got.TotalCases != 2 || got.PassedCases != 1 || got.SkippedCases != 1 || got.FailedCases != 0 {
		t.Fatalf("unselected skip lost or failed: %#v %v", got, err)
	}
}
