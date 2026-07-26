package command

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// `change status --json` is the payload an IDE agent parses and a human reads
// before accepting delivery. Listing its fields and states as literals in a
// docs test only restates the doc; deriving them from the struct and from the
// code that assigns them is what fails when a field or state is renamed.

func cliReference(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "docs", "using-specgate", "reference", "cli.md"))
	if err != nil {
		t.Fatalf("read CLI reference: %v", err)
	}
	return string(raw)
}

func TestChangeStatusFieldsAreDocumented(t *testing.T) {
	t.Parallel()
	reference := cliReference(t)

	var undocumented []string
	fields := reflect.TypeOf(changeStatusResult{})
	for i := range fields.NumField() {
		name, _, _ := strings.Cut(fields.Field(i).Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		if !strings.Contains(reference, "`"+name+"`") {
			undocumented = append(undocumented, name)
		}
	}

	sort.Strings(undocumented)
	if len(undocumented) > 0 {
		t.Fatalf("change status fields missing from the CLI reference: %s", strings.Join(undocumented, ", "))
	}
}

func TestChangeStatusStatesAreDocumented(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("change.go")
	if err != nil {
		t.Fatal(err)
	}
	assigned := regexp.MustCompile(`result\.State = "([a-z_]+)"`).FindAllStringSubmatch(string(source), -1)
	if len(assigned) == 0 {
		t.Fatal("no change states found; the assignment pattern changed")
	}

	reference := cliReference(t)
	var undocumented []string
	for _, match := range assigned {
		if !strings.Contains(reference, "`"+match[1]+"`") {
			undocumented = append(undocumented, match[1])
		}
	}

	sort.Strings(undocumented)
	if len(undocumented) > 0 {
		t.Fatalf("change states missing from the CLI reference: %s", strings.Join(undocumented, ", "))
	}
}
