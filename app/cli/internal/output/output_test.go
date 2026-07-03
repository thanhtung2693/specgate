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
	want := `{"schema_version":"specgate.cli/v1","command":"work.show","ok":false,"error":{"code":"not_found","message":"work item not found","details":{}}}` + "\n"
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
