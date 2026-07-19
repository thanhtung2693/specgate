package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Mode controls the output format for a command run.
type Mode int

const (
	ModeHuman Mode = iota
	ModePlain
	ModeJSON
)

// Style is an ANSI style used only for human terminal output.
type Style int

const (
	StyleBold Style = iota
	StyleMuted
	StyleInfo
	StyleAction
	StyleSuccess
	StyleWarning
	StyleDanger
)

var ansiStyles = map[Style]string{
	StyleBold:    "\x1b[1m",
	StyleMuted:   "\x1b[2m",
	StyleInfo:    "\x1b[36m",
	StyleAction:  "\x1b[1;36m",
	StyleSuccess: "\x1b[32m",
	StyleWarning: "\x1b[33m",
	StyleDanger:  "\x1b[31m",
}

// Exit codes per spec §4.
const (
	ExitOK               = 0
	ExitGovernanceFailed = 1
	ExitUsage            = 2
	ExitNotFound         = 3
	ExitConflict         = 4
	ExitUnavailable      = 5
	ExitIncompatible     = 6
)

var errorCodeToExit = map[string]int{
	"usage":                     ExitUsage,
	"governance_failed":         ExitGovernanceFailed,
	"validation":                ExitUsage,
	"validation_failed":         ExitUsage,
	"not_found":                 ExitNotFound,
	"conflict":                  ExitConflict,
	"unavailable":               ExitUnavailable,
	"incompatible":              ExitIncompatible,
	"initialization_required":   ExitUnavailable,
	"missing_workspace_binding": ExitUnavailable,
	"stale_context":             ExitConflict,
	"stale_task_result":         ExitConflict,
	"confirmation_required":     ExitUsage,
	"package_too_large":         ExitUsage,
	"store_busy":                ExitUnavailable,
}

// ExitError wraps an exit code so cobra command errors propagate to main.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("exit %d", e.Code)
}

func (e *ExitError) Unwrap() error { return e.Err }

// ErrorPayload is the error object inside a JSON envelope.
type ErrorPayload struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Transient bool           `json:"transient"`
	Details   map[string]any `json:"details"`
}

type envelope struct {
	SchemaVersion string        `json:"schema_version"`
	Command       string        `json:"command"`
	OK            bool          `json:"ok"`
	Data          any           `json:"data,omitempty"`
	Error         *ErrorPayload `json:"error,omitempty"`
}

// Printer emits structured output.
type Printer struct {
	stdout      io.Writer
	stderr      io.Writer
	mode        Mode
	color       bool
	stderrColor bool
}

// New creates a Printer with the given streams and output mode.
func New(stdout, stderr io.Writer, mode Mode) *Printer {
	return NewWithColor(stdout, stderr, mode, mode == ModeHuman)
}

// NewWithColor creates a Printer with explicit ANSI capability. Root commands
// derive this from the output stream and terminal environment; direct callers
// can retain the existing New behavior when that distinction is irrelevant.
func NewWithColor(stdout, stderr io.Writer, mode Mode, color bool) *Printer {
	return NewWithCapabilities(stdout, stderr, mode, color, color)
}

// NewWithCapabilities configures ANSI separately for stdout and stderr.
// Commands render data to stdout, while errors and prompts use stderr; each
// stream can be redirected independently.
func NewWithCapabilities(stdout, stderr io.Writer, mode Mode, stdoutColor, stderrColor bool) *Printer {
	return &Printer{stdout: stdout, stderr: stderr, mode: mode, color: stdoutColor, stderrColor: stderrColor}
}

// Error emits a JSON error envelope and returns the corresponding exit code.
// Details is always present as {} when not supplied.
func (p *Printer) Error(command string, payload ErrorPayload) int {
	if payload.Details == nil {
		payload.Details = map[string]any{}
	}
	if p.mode != ModeJSON {
		// Human and plain sessions get a readable line on stderr, not a JSON
		// envelope. The envelope is the --json contract only.
		fmt.Fprintf(p.stderr, "%s (%s): %s\n", p.StyleStderr("Error", StyleDanger), payload.Code, payload.Message)
		printHumanErrorDetails(p.stderr, payload.Details)
		if code, ok := errorCodeToExit[payload.Code]; ok {
			return code
		}
		return ExitUnavailable
	}
	env := envelope{
		SchemaVersion: "specgate.cli/v1",
		Command:       command,
		OK:            false,
		Error:         &payload,
	}
	p.writeJSON(env)
	if code, ok := errorCodeToExit[payload.Code]; ok {
		return code
	}
	return ExitUnavailable
}

// Success emits a JSON success envelope and returns ExitOK.
func (p *Printer) Success(command string, data any) int {
	env := envelope{
		SchemaVersion: "specgate.cli/v1",
		Command:       command,
		OK:            true,
		Data:          data,
	}
	p.writeJSON(env)
	return ExitOK
}

// Mode returns the current output mode.
func (p *Printer) Mode() Mode { return p.mode }

// ColorEnabled reports whether ANSI styling is active for this printer.
func (p *Printer) ColorEnabled() bool {
	return p != nil && p.mode == ModeHuman && p.color
}

// StderrColorEnabled reports whether ANSI styling is active for stderr.
func (p *Printer) StderrColorEnabled() bool {
	return p != nil && p.mode == ModeHuman && p.stderrColor
}

// Style decorates text for human terminal output. Plain and JSON modes are
// intentionally stable and never include ANSI escapes.
func (p *Printer) Style(text string, style Style) string {
	if p == nil || p.mode != ModeHuman || !p.color {
		return text
	}
	code, ok := ansiStyles[style]
	if !ok {
		return text
	}
	return code + text + "\x1b[0m"
}

// StyleStderr decorates diagnostic text only when stderr is a capable human
// terminal. It is intentionally separate from Style because stdout and stderr
// may be redirected independently.
func (p *Printer) StyleStderr(text string, style Style) string {
	if p == nil || p.mode != ModeHuman || !p.stderrColor {
		return text
	}
	code, ok := ansiStyles[style]
	if !ok {
		return text
	}
	return code + text + "\x1b[0m"
}

func (p *Printer) writeJSON(env envelope) {
	data, _ := json.Marshal(env)
	fmt.Fprintf(p.stdout, "%s\n", data)
}

// ExitCodeFromError extracts the exit code from an error returned by a cobra command.
func ExitCodeFromError(err error) int {
	if err == nil {
		return ExitOK
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	return ExitUnavailable
}

// printHumanErrorDetails surfaces server validation details (the huma
// errors[] list) as indented lines so human sessions see the same field-level
// hints --json carries. Caps at a handful of lines to stay readable.
func printHumanErrorDetails(w io.Writer, details map[string]any) {
	// The errors list arrives as []map[string]any straight from the client and
	// as []any after a JSON round-trip; accept both.
	var items []any
	switch v := details["errors"].(type) {
	case []any:
		items = v
	case []map[string]any:
		items = make([]any, len(v))
		for i, entry := range v {
			items[i] = entry
		}
	default:
		return
	}
	const maxLines = 6
	shown := 0
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		location, _ := entry["location"].(string)
		message, _ := entry["message"].(string)
		if location == "" && message == "" {
			continue
		}
		if shown == maxLines {
			fmt.Fprintf(w, "  … and %d more\n", len(items)-shown)
			return
		}
		switch {
		case location == "":
			fmt.Fprintf(w, "  - %s\n", message)
		default:
			fmt.Fprintf(w, "  - %s: %s\n", location, message)
		}
		shown++
	}
}
