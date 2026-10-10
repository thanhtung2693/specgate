package local

import (
	"bytes"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	maxJUnitReportBytes = 16 << 20
	maxJUnitCases       = 100000
)

type JUnitCaseObservation struct {
	ClassName string `json:"classname"`
	Name      string `json:"name"`
	Outcome   string `json:"outcome"`
}

// JUnitObservation contains normalized outcomes only. Deliberately do not
// retain failure bodies, properties, or process output, which can carry
// secrets and add no evidence for the selected-case decision.
type JUnitObservation struct {
	ReportDigest string                 `json:"report_digest"`
	TotalCases   int                    `json:"total_cases"`
	PassedCases  int                    `json:"passed_cases"`
	FailedCases  int                    `json:"failed_cases"`
	SkippedCases int                    `json:"skipped_cases"`
	Selected     []JUnitCaseObservation `json:"selected"`
	Passing      bool                   `json:"passing"`
}

// ObserveJUnitReportFromRoot reads only from a directory handle opened before
// the runner. Renaming or replacing its pathname cannot redirect the read.
// Testcase identities must be exact and unambiguous.
func ObserveJUnitReportFromRoot(root *os.Root, name string, selectors []SelectedTestCase) (JUnitObservation, error) {
	if name != filepath.Base(name) || name == "." {
		return JUnitObservation{}, fmt.Errorf("%w: invalid report name", ErrVerificationInvalid)
	}
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return JUnitObservation{}, fmt.Errorf("%w: test report must be a regular non-symlink file", ErrVerificationInvalid)
	}
	if info.Size() > maxJUnitReportBytes {
		return JUnitObservation{}, fmt.Errorf("%w: JUnit report exceeds 16 MiB", ErrVerificationInvalid)
	}
	file, err := root.Open(name)
	if err != nil {
		return JUnitObservation{}, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return JUnitObservation{}, fmt.Errorf("%w: report changed while opening", ErrVerificationInvalid)
	}
	body, err := io.ReadAll(io.LimitReader(file, maxJUnitReportBytes+1))
	if err != nil {
		return JUnitObservation{}, err
	}
	if len(body) == 0 || len(body) > maxJUnitReportBytes || bytes.Contains(bytes.ToUpper(body), []byte("<!DOCTYPE")) || bytes.Contains(bytes.ToUpper(body), []byte("<!ENTITY")) {
		return JUnitObservation{}, fmt.Errorf("%w: unsupported JUnit report", ErrVerificationInvalid)
	}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	cases := map[string]JUnitCaseObservation{}
	total := 0
	passed, failed, skipped := 0, 0, 0
	allPassing := true
	var stack []string
	rootSeen := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return JUnitObservation{}, fmt.Errorf("%w: malformed JUnit XML", ErrVerificationInvalid)
		}
		if _, ok := token.(xml.EndElement); ok {
			stack = stack[:len(stack)-1]
			continue
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		name := start.Name.Local
		if len(stack) == 0 {
			if rootSeen || (name != "testsuite" && name != "testsuites") {
				return JUnitObservation{}, fmt.Errorf("%w: unsupported JUnit root", ErrVerificationInvalid)
			}
			rootSeen = true
		} else if name != "testsuite" && name != "testcase" {
			if name != "properties" && name != "system-out" && name != "system-err" {
				return JUnitObservation{}, fmt.Errorf("%w: unsupported JUnit element", ErrVerificationInvalid)
			}
			if err := decoder.Skip(); err != nil {
				return JUnitObservation{}, fmt.Errorf("%w: malformed JUnit metadata", ErrVerificationInvalid)
			}
			continue
		}
		if name != "testcase" {
			stack = append(stack, name)
			continue
		}
		if len(stack) == 0 || stack[len(stack)-1] != "testsuite" {
			return JUnitObservation{}, fmt.Errorf("%w: testcase outside suite", ErrVerificationInvalid)
		}
		var testcase struct {
			ClassName string                       `xml:"classname,attr"`
			Name      string                       `xml:"name,attr"`
			Children  []struct{ XMLName xml.Name } `xml:",any"`
		}
		if err := decoder.DecodeElement(&testcase, &start); err != nil {
			return JUnitObservation{}, fmt.Errorf("%w: malformed testcase", ErrVerificationInvalid)
		}
		total++
		if total > maxJUnitCases {
			return JUnitObservation{}, fmt.Errorf("%w: JUnit report exceeds 100000 cases", ErrVerificationInvalid)
		}
		key := testcase.ClassName + "\x00" + testcase.Name
		if testcase.ClassName == "" || testcase.Name == "" || cases[key].Name != "" {
			return JUnitObservation{}, fmt.Errorf("%w: ambiguous JUnit testcase identity", ErrVerificationInvalid)
		}
		outcome := "passed"
		outcomes := 0
		for _, child := range testcase.Children {
			switch child.XMLName.Local {
			case "failure", "error":
				outcome = "failed"
				outcomes++
			case "skipped":
				outcome = "skipped"
				outcomes++
			case "properties", "system-out", "system-err":
			default:
				return JUnitObservation{}, fmt.Errorf("%w: unsupported testcase result", ErrVerificationInvalid)
			}
		}
		if outcomes > 1 {
			return JUnitObservation{}, fmt.Errorf("%w: ambiguous testcase outcome", ErrVerificationInvalid)
		}
		switch outcome {
		case "passed":
			passed++
		case "failed":
			failed++
			allPassing = false
		case "skipped":
			skipped++
		}
		cases[key] = JUnitCaseObservation{ClassName: testcase.ClassName, Name: testcase.Name, Outcome: outcome}
	}
	if total == 0 {
		return JUnitObservation{}, fmt.Errorf("%w: JUnit report contains zero cases", ErrVerificationInvalid)
	}
	observation := JUnitObservation{TotalCases: total, PassedCases: passed, FailedCases: failed, SkippedCases: skipped, Passing: allPassing, Selected: make([]JUnitCaseObservation, 0, len(selectors))}
	digest := sha256.Sum256(body)
	observation.ReportDigest = fmt.Sprintf("%x", digest)
	seen := map[string]bool{}
	for _, selector := range selectors {
		key := selector.ClassName + "\x00" + selector.Name
		if seen[key] {
			return JUnitObservation{}, fmt.Errorf("%w: duplicate selected testcase", ErrVerificationInvalid)
		}
		seen[key] = true
		caseResult, ok := cases[key]
		if !ok {
			return JUnitObservation{}, fmt.Errorf("%w: selected testcase is missing", ErrVerificationInvalid)
		}
		observation.Selected = append(observation.Selected, caseResult)
		if caseResult.Outcome != "passed" {
			observation.Passing = false
		}
	}
	if len(selectors) == 0 {
		return JUnitObservation{}, fmt.Errorf("%w: selected JUnit report needs selectors", ErrVerificationInvalid)
	}
	return observation, nil
}
