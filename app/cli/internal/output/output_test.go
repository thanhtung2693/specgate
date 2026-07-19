package output_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestJSONErrorEnvelope(t *testing.T) {
	var buf bytes.Buffer
	p := output.New(&buf, io.Discard, output.ModeJSON)
	code := p.Error("work.show", output.ErrorPayload{
		Code:    "not_found",
		Message: "work item not found",
	})
	if code != 3 {
		t.Fatalf("exit = %d, want 3", code)
	}
	want := `{"schema_version":"specgate.cli/v1","command":"work.show","ok":false,"error":{"code":"not_found","message":"work item not found","transient":false,"details":{}}}` + "\n"
	if buf.String() != want {
		t.Fatalf("output = %q\n  want = %q", buf.String(), want)
	}
}

func TestJSONErrorDetails(t *testing.T) {
	var buf bytes.Buffer
	p := output.New(&buf, io.Discard, output.ModeJSON)
	p.Error("gates.run", output.ErrorPayload{
		Code:    "governance_failed",
		Message: "gate failed",
		Details: map[string]any{"gate": "completeness"},
	})
	if buf.Len() == 0 {
		t.Fatal("no output")
	}
}

func TestLocalErrorCodesHaveStableExitAndTransientFields(t *testing.T) {
	for _, tc := range []struct {
		code      string
		exit      int
		transient bool
	}{
		{code: "initialization_required", exit: output.ExitUnavailable},
		{code: "stale_context", exit: output.ExitConflict},
		{code: "confirmation_required", exit: output.ExitUsage},
		{code: "store_busy", exit: output.ExitUnavailable, transient: true},
	} {
		var buf bytes.Buffer
		got := output.New(&buf, io.Discard, output.ModeJSON).Error("local.test", output.ErrorPayload{Code: tc.code, Message: "test", Transient: tc.transient})
		if got != tc.exit {
			t.Fatalf("%s exit = %d, want %d", tc.code, got, tc.exit)
		}
		want := `"transient":` + map[bool]string{false: "false", true: "true"}[tc.transient]
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("%s missing %s: %s", tc.code, want, buf.String())
		}
	}
}

func TestJSONSuccessEnvelope(t *testing.T) {
	var buf bytes.Buffer
	p := output.New(&buf, io.Discard, output.ModeJSON)
	code := p.Success("meta", map[string]string{"version": "dev"})
	if code != output.ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	if buf.Len() == 0 {
		t.Fatal("no output")
	}
}

func TestStyleOnlyAppliesInHumanMode(t *testing.T) {
	human := output.New(io.Discard, io.Discard, output.ModeHuman)
	if got := human.Style("pass", output.StyleSuccess); got != "\x1b[32mpass\x1b[0m" {
		t.Fatalf("human style = %q", got)
	}

	plain := output.New(io.Discard, io.Discard, output.ModePlain)
	if got := plain.Style("pass", output.StyleSuccess); got != "pass" {
		t.Fatalf("plain style = %q", got)
	}

	json := output.New(io.Discard, io.Discard, output.ModeJSON)
	if got := json.Style("pass", output.StyleSuccess); got != "pass" {
		t.Fatalf("json style = %q", got)
	}
}

func TestStyleRequiresHumanModeAndColorCapability(t *testing.T) {
	rich := output.NewWithColor(io.Discard, io.Discard, output.ModeHuman, true)
	if got := rich.Style("Next", output.StyleAction); got != "\x1b[1;36mNext\x1b[0m" {
		t.Fatalf("rich style = %q", got)
	}

	for _, p := range []*output.Printer{
		output.NewWithColor(io.Discard, io.Discard, output.ModeHuman, false),
		output.NewWithColor(io.Discard, io.Discard, output.ModePlain, true),
		output.NewWithColor(io.Discard, io.Discard, output.ModeJSON, true),
	} {
		if got := p.Style("Next", output.StyleAction); got != "Next" {
			t.Fatalf("safe style = %q", got)
		}
	}
}

func TestHumanErrorUsesRedLabelOnlyWhenColorEnabled(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := output.NewWithColor(&stdout, &stderr, output.ModeHuman, true)
	p.Error("status", output.ErrorPayload{Code: "validation", Message: "choose a workspace"})
	if !strings.Contains(stderr.String(), "\x1b[31mError\x1b[0m") {
		t.Fatalf("missing red Error label: %q", stderr.String())
	}
}

func TestErrorUsesStderrCapabilityIndependently(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := output.NewWithCapabilities(&stdout, &stderr, output.ModeHuman, true, false)
	p.Error("status", output.ErrorPayload{Code: "unavailable", Message: "offline"})
	if strings.Contains(stderr.String(), "\x1b[") {
		t.Fatalf("stderr contains ANSI despite disabled stderr capability: %q", stderr.String())
	}
}

func TestExitCodeFromError_nil(t *testing.T) {
	if output.ExitCodeFromError(nil) != output.ExitOK {
		t.Fatal("nil error should give ExitOK")
	}
}

func TestExitCodeFromError_exitError(t *testing.T) {
	err := &output.ExitError{Code: output.ExitNotFound}
	if got := output.ExitCodeFromError(err); got != output.ExitNotFound {
		t.Fatalf("got %d, want %d", got, output.ExitNotFound)
	}
}

func TestExitCodeFromError_unknownError(t *testing.T) {
	err := &output.ExitError{Code: output.ExitConflict}
	if got := output.ExitCodeFromError(err); got != output.ExitConflict {
		t.Fatalf("got %d, want %d", got, output.ExitConflict)
	}
}

func TestErrorPrintsReadableLineInHumanMode(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	p := output.New(&stdout, &stderr, output.ModeHuman)

	code := p.Error("work.list", output.ErrorPayload{Code: "unavailable", Message: "Internal Server Error: governance-status"})

	if code != output.ExitUnavailable {
		t.Fatalf("code = %d", code)
	}
	if strings.Contains(stderr.String()+stdout.String(), "schema_version") {
		t.Fatalf("human mode leaked JSON envelope: %s%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "Internal Server Error: governance-status") {
		t.Fatalf("stderr missing readable message: %q", stderr.String())
	}
}

func TestErrorKeepsEnvelopeInJSONMode(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	p := output.New(&stdout, &stderr, output.ModeJSON)

	p.Error("work.list", output.ErrorPayload{Code: "unavailable", Message: "boom"})

	if !strings.Contains(stdout.String(), `"schema_version"`) {
		t.Fatalf("json mode missing envelope: %q", stdout.String())
	}
}

func TestErrorPrintsValidationDetailsInHumanMode(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	p := output.New(&stdout, &stderr, output.ModeHuman)

	p.Error("artifact.publish", output.ErrorPayload{
		Code:    "incompatible",
		Message: "Unprocessable Entity: validation failed",
		Details: map[string]any{
			"errors": []any{
				map[string]any{"location": "body.created_by", "message": "unexpected property"},
				map[string]any{"location": "body.impact_declaration.migration", "message": "unexpected property"},
			},
		},
	})

	got := stderr.String()
	if !strings.Contains(got, "body.created_by: unexpected property") {
		t.Fatalf("stderr missing detail line: %q", got)
	}
	if !strings.Contains(got, "body.impact_declaration.migration: unexpected property") {
		t.Fatalf("stderr missing second detail line: %q", got)
	}
}

func TestErrorPrintsTypedDetailSliceInHumanMode(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	p := output.New(&stdout, &stderr, output.ModeHuman)

	p.Error("artifact.publish", output.ErrorPayload{
		Code:    "incompatible",
		Message: "validation failed",
		Details: map[string]any{
			"errors": []map[string]any{{"location": "body.created_by", "message": "unexpected property"}},
		},
	})

	if !strings.Contains(stderr.String(), "body.created_by: unexpected property") {
		t.Fatalf("typed slice details not rendered: %q", stderr.String())
	}
}
