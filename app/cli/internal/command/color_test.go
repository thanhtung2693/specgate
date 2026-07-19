package command

import (
	"io"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestNextStepKeepsCommandCopyable(t *testing.T) {
	deps := &Deps{
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Printer: output.NewWithColor(io.Discard, io.Discard, output.ModeHuman, true),
	}

	got := nextStep(deps, "Inspect the item with", "specgate work show CR-123")
	if !strings.Contains(got, "Next:") || !strings.Contains(got, "specgate work show CR-123") {
		t.Fatalf("next step = %q", got)
	}
	if !strings.Contains(got, "\x1b[1;36mspecgate work show CR-123\x1b[0m") {
		t.Fatalf("missing action style: %q", got)
	}
}

func TestCriterionMarkerUsesASCIIOutsideRichTerminal(t *testing.T) {
	deps := &Deps{Printer: output.NewWithColor(io.Discard, io.Discard, output.ModeHuman, false)}
	if got := criterionMarker(deps, true); got != "[x]" {
		t.Fatalf("done marker = %q", got)
	}
	if got := criterionMarker(deps, false); got != "[ ]" {
		t.Fatalf("pending marker = %q", got)
	}
}
