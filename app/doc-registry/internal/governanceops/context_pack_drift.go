package governanceops

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/specgate/doc-registry/internal/artifact"
)

const unresolvedGatesHeader = "_These quality gates did not pass at handoff. Account for them as you implement._"

// mergeDriftReadiness appends the latest non-pass spec_repo_drift readiness run
// (and its per-finding bullets) to the Unresolved Quality Gates section, adding
// the section header if CR gate runs produced none. Mirrors the Python renderer
// so the drifted-doc guidance reaches the coding agent on the full route.
func mergeDriftReadiness(existing string, runs []artifact.ReadinessRun) string {
	var latest *artifact.ReadinessRun
	for i := range runs {
		if runs[i].Gate != "spec_repo_drift" {
			continue
		}
		if latest == nil || runs[i].CreatedAt.After(latest.CreatedAt) {
			latest = &runs[i]
		}
	}
	if latest == nil {
		return existing
	}
	switch string(latest.State) {
	case "warn", "fail", "needs_human_review":
	default:
		return existing // pass / not_applicable: nothing to carry
	}

	line := fmt.Sprintf("- **%s** (%s)", latest.Gate, latest.State)
	if hint := strings.TrimSpace(latest.Hint); hint != "" {
		line += ": " + hint
	}
	bullets := []string{line}
	for _, f := range driftFindings(latest.EvidenceJSON) {
		doc := strings.TrimSpace(f.DocPath)
		detail := strings.TrimSpace(f.ConflictingClaim)
		if s := strings.TrimSpace(f.SpecSection); s != "" {
			if detail != "" {
				detail += " — "
			}
			detail += "contradicts " + s
		}
		b := "  - `" + doc + "`"
		if detail != "" {
			b += ": " + detail
		}
		bullets = append(bullets, b)
	}
	drift := strings.Join(bullets, "\n")
	if strings.TrimSpace(existing) == "" {
		return unresolvedGatesHeader + "\n" + drift
	}
	return existing + "\n" + drift
}

type driftFinding struct {
	DocPath          string `json:"doc_path"`
	ConflictingClaim string `json:"conflicting_claim"`
	SpecSection      string `json:"spec_section"`
}

// driftFindings parses the readiness run's evidence_json findings envelope. A
// stored run wraps the submit envelope in gate-run-v1, so findings sit under
// `.evidence` (a JSON string) → `.findings`; a bare `{executor, findings}`
// envelope carries them at the top level. Mirrors the Python _gate_run_findings.
func driftFindings(evidenceJSON string) []driftFinding {
	evidenceJSON = strings.TrimSpace(evidenceJSON)
	if evidenceJSON == "" {
		return nil
	}
	var env struct {
		Findings []driftFinding  `json:"findings"`
		Evidence json.RawMessage `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(evidenceJSON), &env); err != nil {
		return nil
	}
	if len(env.Findings) > 0 {
		return env.Findings
	}
	if len(env.Evidence) == 0 {
		return nil
	}
	// evidence may be a JSON string containing an object, or an object directly.
	raw := env.Evidence
	var asString string
	if json.Unmarshal(raw, &asString) == nil {
		raw = json.RawMessage(asString)
	}
	var inner struct {
		Findings []driftFinding `json:"findings"`
	}
	if json.Unmarshal(raw, &inner) == nil {
		return inner.Findings
	}
	return nil
}
