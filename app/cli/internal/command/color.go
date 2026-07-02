package command

import (
	"fmt"
	"math"
	"strings"

	"github.com/specgate/specgate/app/cli/internal/output"
)

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

func humanVisuals(deps *Deps) bool {
	return deps != nil && deps.Printer != nil && deps.Printer.Mode() == output.ModeHuman
}

func visualRule(deps *Deps) string {
	if !humanVisuals(deps) {
		return ""
	}
	return styled(deps, output.StyleMuted, strings.Repeat("─", 64))
}

func progressBar(deps *Deps, done, total, width int) string {
	if total <= 0 || width <= 0 {
		return ""
	}
	done = max(0, min(done, total))
	filled := int(math.Round(float64(done) / float64(total) * float64(width)))
	filled = max(0, min(filled, width))
	empty := width - filled
	if humanVisuals(deps) {
		return "[" +
			styled(deps, output.StyleSuccess, strings.Repeat("█", filled)) +
			styled(deps, output.StyleMuted, strings.Repeat("░", empty)) +
			"]"
	}
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", empty) + "]"
}

func percent(done, total int) int {
	if total <= 0 {
		return 0
	}
	return int(math.Round(float64(done) / float64(total) * 100))
}

func statusIcon(deps *Deps, status string) string {
	if !humanVisuals(deps) {
		return ""
	}
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "pass", "passed", "met", "ready", "ok":
		return styled(deps, output.StyleSuccess, "✓")
	case "fail", "failed", "not_done", "not done":
		return styled(deps, output.StyleDanger, "✕")
	case "warn", "warning", "waiting", "needs_review", "needs review", "needs_human_review", "unclear", "skipped":
		return styled(deps, output.StyleWarning, "!")
	case "not_applicable", "not applicable", "not_run", "not run":
		return styled(deps, output.StyleMuted, "·")
	default:
		return styled(deps, output.StyleInfo, "•")
	}
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

func coloredBullet(deps *Deps, style output.Style) string {
	if !humanVisuals(deps) {
		return ""
	}
	return styled(deps, style, "●")
}

func passingStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pass", "passed", "met", "ready", "ok":
		return true
	default:
		return false
	}
}

func statusStyle(status string) output.Style {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pass", "passed", "met", "ready", "ok":
		return output.StyleSuccess
	case "fail", "failed", "not_done", "not done":
		return output.StyleDanger
	case "warn", "warning", "waiting", "needs_review", "needs review", "needs_human_review", "unclear", "skipped":
		return output.StyleWarning
	default:
		return output.StyleInfo
	}
}
