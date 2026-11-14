package command

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/specgate/specgate/app/cli/internal/output"
)

// terminalText keeps manifest-provided text on one harmless terminal line.
func terminalText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
}

func styled(deps *Deps, style output.Style, text string) string {
	if deps == nil || deps.Printer == nil {
		return text
	}
	return deps.Printer.Style(text, style)
}

func styledStatus(deps *Deps, status string) string {
	return styled(deps, statusStyle(status), status)
}

func styledStatusPadded(deps *Deps, status string, width int) string {
	return styled(deps, statusStyle(status), fmt.Sprintf("%-*s", width, status))
}

func title(deps *Deps, text string) string {
	return styled(deps, output.StyleAction, text)
}

func label(deps *Deps, text string) string {
	return styled(deps, output.StyleMuted, text)
}

func notice(deps *Deps, style output.Style, kind, text string) string {
	return styled(deps, style, kind) + ": " + text
}

func nextStep(deps *Deps, lead, command string) string {
	prefix := label(deps, "Next:")
	if lead == "" {
		return prefix + " `" + styled(deps, output.StyleAction, command) + "`"
	}
	return prefix + " " + lead + " `" + styled(deps, output.StyleAction, command) + "`"
}

func styleHelpHeadings(deps *Deps, text string) string {
	for _, heading := range []string{
		"Usage:", "Examples:", "Available Commands:", "Additional Commands:",
		"Flags:", "Global Flags:", "Start here", "Optional browser review",
		"Advanced and maintenance commands",
	} {
		text = strings.ReplaceAll(text, heading, title(deps, heading))
	}
	return text
}

func humanVisuals(deps *Deps) bool {
	return deps != nil && deps.Printer != nil && deps.Printer.ColorEnabled()
}

func criterionBox(deps *Deps, status string) string {
	if !humanVisuals(deps) {
		return ""
	}
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "pass", "passed", "met", "ready", "ok":
		return styled(deps, output.StyleSuccess, "☑")
	case "fail", "failed", "not_done", "not done":
		return styled(deps, output.StyleDanger, "☒")
	default:
		return styled(deps, statusStyle(status), "☐")
	}
}

func criterionMarker(deps *Deps, done bool) string {
	if !humanVisuals(deps) {
		if done {
			return "[x]"
		}
		return "[ ]"
	}
	if done {
		return criterionBox(deps, "passed")
	}
	return criterionBox(deps, "pending")
}

func statusStyle(status string) output.Style {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pass", "passed", "met", "ready", "ok":
		return output.StyleSuccess
	case "fail", "failed", "not_done", "not done":
		return output.StyleDanger
	case "warn", "warning", "waiting", "needs_review", "needs review", "needs_human_review", "unclear", "skipped", "missing", "unknown", "not_member", "degraded":
		return output.StyleWarning
	default:
		return output.StyleInfo
	}
}
