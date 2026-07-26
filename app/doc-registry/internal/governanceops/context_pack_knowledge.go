package governanceops

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/specgate/doc-registry/internal/knowledge"
	"github.com/specgate/doc-registry/internal/workboard"
)

func buildKnowledgeProvenance(ctx context.Context, kr ContextPackKnowledgeReader, workspaceID string, featureRefs []string, requestID string) ([]ProvenanceRow, []Warning) {
	if kr == nil {
		return []ProvenanceRow{}, nil
	}
	docs, err := kr.ListByFeatureOrRequest(ctx, workspaceID, featureRefs, requestID)
	if err != nil {
		slog.WarnContext(ctx, "knowledge provenance lookup failed", "feature_refs", featureRefs, "request_id", requestID, "err", err)
		return []ProvenanceRow{}, []Warning{{
			Code:    "knowledge_provenance_unavailable",
			Message: "Knowledge provenance lookup failed; context pack has no knowledge_provenance.",
		}}
	}
	if len(docs) == 0 {
		return []ProvenanceRow{}, nil
	}
	best := make(map[string]knowledge.Document, len(docs))
	for _, d := range docs {
		existing, found := best[d.DocumentID]
		if !found {
			best[d.DocumentID] = d
			continue
		}
		if d.IsLatest && !existing.IsLatest {
			best[d.DocumentID] = d
		} else if !d.IsLatest && !existing.IsLatest && d.CreatedAt.After(existing.CreatedAt) {
			best[d.DocumentID] = d
		}
	}
	selected := make([]knowledge.Document, 0, len(best))
	for _, d := range best {
		selected = append(selected, d)
	}
	sort.Slice(selected, func(i, j int) bool {
		pi, pj := authorityPriority(selected[i].AuthorityLevel), authorityPriority(selected[j].AuthorityLevel)
		if pi != pj {
			return pi < pj
		}
		return selected[i].Title < selected[j].Title
	})
	rows := make([]ProvenanceRow, 0, len(selected))
	for _, d := range selected {
		freshness := "stale"
		if d.IsLatest {
			freshness = "current"
		}
		rows = append(rows, ProvenanceRow{
			DocumentID:        d.DocumentID,
			Title:             d.Title,
			Version:           d.Version,
			DocumentType:      string(d.DocumentType),
			AuthorityLevel:    string(d.AuthorityLevel),
			IsLatest:          d.IsLatest,
			Freshness:         freshness,
			KnowledgeStoreURI: "specgate://knowledge/" + d.DocumentID,
		})
	}
	return rows, nil
}

func authorityPriority(level knowledge.AuthorityLevel) int {
	switch level {
	case knowledge.AuthoritySourceOfTruth:
		return 1
	case knowledge.AuthorityHigh:
		return 2
	case knowledge.AuthorityReference:
		return 3
	case knowledge.AuthorityLow:
		return 4
	default:
		return 5
	}
}

func renderKnowledgeReferences(rows []ProvenanceRow) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("| Document | Type | Authority | Freshness |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, r := range rows {
		freshness := r.Freshness
		if freshness == "stale" {
			freshness = "stale — newer version available"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", r.Title, r.DocumentType, r.AuthorityLevel, freshness)
	}
	return strings.TrimSpace(b.String())
}

func nonEmpty(a, fallback string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return fallback
}

func formatAcceptanceCriteria(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return ""
	}
	var items []string
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return raw
	}
	lines := make([]string, 0, len(items))
	for _, it := range items {
		if s := strings.TrimSpace(it); s != "" {
			lines = append(lines, "- "+s)
		}
	}
	return strings.Join(lines, "\n")
}

// outstandingReviewFeedback turns the authoritative failed delivery_review
// GateRun into a markdown body listing the unmet/unclear criteria + failing
// checks. Human decisions outrank later platform runs, matching delivery status.
func outstandingReviewFeedback(runs []workboard.GateRun) string {
	latest := latestDeliveryRun(runs)
	if latest == nil || (latest.State != workboard.NextActionStateFail && latest.State != workboard.NextActionStateNeedsHumanReview) {
		return ""
	}

	var wrapper struct {
		Evidence string `json:"evidence"`
	}
	_ = json.Unmarshal([]byte(latest.EvidenceJSON), &wrapper)
	var detail struct {
		Criteria []struct {
			Text    string `json:"text"`
			Verdict string `json:"verdict"`
			Why     string `json:"why"`
		} `json:"criteria"`
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Detail string `json:"detail"`
		} `json:"checks"`
	}
	if strings.TrimSpace(wrapper.Evidence) != "" {
		_ = json.Unmarshal([]byte(wrapper.Evidence), &detail)
	}

	var b strings.Builder
	b.WriteString("_The previous delivery review did not pass. Address these before reporting done again._\n")
	for _, c := range detail.Criteria {
		v := strings.ToLower(strings.TrimSpace(c.Verdict))
		if v != "unmet" && v != "unclear" {
			continue
		}
		label := strings.TrimSpace(c.Text)
		if label == "" {
			label = "(criterion)"
		}
		line := fmt.Sprintf("\n- **%s** (%s)", label, v)
		if why := strings.TrimSpace(c.Why); why != "" {
			line += ": " + why
		}
		b.WriteString(line)
	}
	for _, c := range detail.Checks {
		if isFailedCheckStatus(c.Status) {
			line := fmt.Sprintf("\n- **Check failed: %s**", strings.TrimSpace(c.Name))
			if d := strings.TrimSpace(c.Detail); d != "" {
				line += " — " + d
			}
			b.WriteString(line)
		}
	}
	if hint := strings.TrimSpace(latest.Hint); hint != "" {
		fmt.Fprintf(&b, "\n\n_Reviewer summary: %s_", hint)
	}
	return strings.TrimSpace(b.String())
}

// unresolvedQualityGates lists the latest-per-gate quality verdicts that did not
// pass (warn / fail / needs_human_review) as markdown bullets.
func unresolvedQualityGates(runs []workboard.GateRun) string {
	latest := map[string]workboard.GateRun{}
	for _, r := range runs {
		if r.Gate == "delivery_review" {
			continue
		}
		if cur, ok := latest[r.Gate]; !ok || r.CreatedAt.After(cur.CreatedAt) {
			latest[r.Gate] = r
		}
	}
	keys := make([]string, 0, len(latest))
	for k := range latest {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var lines []string
	for _, k := range keys {
		r := latest[k]
		switch string(r.State) {
		case "warn", "fail", "needs_human_review":
		default:
			continue
		}
		line := fmt.Sprintf("- **%s** (%s)", r.Gate, r.State)
		if hint := strings.TrimSpace(r.Hint); hint != "" {
			line += ": " + hint
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	return unresolvedGatesHeader + "\n" + strings.Join(lines, "\n")
}
