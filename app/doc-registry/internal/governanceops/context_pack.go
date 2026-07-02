package governanceops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/artifactattachment"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/knowledge"
	"github.com/specgate/doc-registry/internal/skills"
	"github.com/specgate/doc-registry/internal/workboard"
)

// ContextPack assembles the on-read handoff pack for a change request or
// artifact. Kind must be "change_request" or "artifact".
func (s *Service) ContextPack(ctx context.Context, in ContextPackInput) (ContextPackResult, error) {
	if in.Kind == "artifact" {
		return s.contextPackForArtifact(ctx, in.ID)
	}
	return s.contextPackForCR(ctx, in.ID, in.Lane)
}

func (s *Service) contextPackForArtifact(ctx context.Context, artifactID string) (ContextPackResult, error) {
	empty := ContextPackResult{
		Kind:                "artifact",
		ArtifactID:          artifactID,
		State:               "not_generated",
		KnowledgeProvenance: []ProvenanceRow{},
		Warnings:            []Warning{},
	}
	if s.Artifacts == nil {
		return empty, nil
	}
	art, err := s.Artifacts.Get(ctx, artifactID)
	if err != nil {
		if errors.Is(err, artifact.ErrNotFound) {
			return ContextPackResult{}, workboard.ErrNotFound
		}
		return ContextPackResult{}, err
	}
	markdown := renderContextPackMarkdown(ctx, s.Artifacts, nil, s.Skills, art, nil, nil, nil, "", "", "")

	// per spec §8: read governance level from the artifact snapshot. Snapshots
	// without a governance level produce an empty GovernanceLevel without error.
	governanceLevel := ""
	if snap, snapErr := governanceprofile.ParseSnapshot(art.GatesProfileSnapshotJSON); snapErr == nil {
		governanceLevel = string(snap.GovernanceLevel)
	}

	return ContextPackResult{
		Kind:                "artifact",
		ArtifactID:          art.ID,
		State:               "assembled",
		Markdown:            markdown,
		KnowledgeProvenance: []ProvenanceRow{},
		Warnings:            []Warning{},
		GovernanceLevel:     governanceLevel,
	}, nil
}

func (s *Service) contextPackForCR(ctx context.Context, changeRequestID, lane string) (ContextPackResult, error) {
	if s.WorkBoard == nil {
		return ContextPackResult{}, ErrUnavailable
	}
	cr, err := s.WorkBoard.GetChangeRequest(ctx, changeRequestID)
	if err != nil {
		return ContextPackResult{}, err
	}
	var feature *workboard.Feature
	featureID := strings.TrimSpace(cr.FeatureID)
	if featureID != "" {
		loaded, err := s.WorkBoard.GetFeature(ctx, featureID)
		if err != nil {
			return ContextPackResult{}, err
		}
		feature = loaded
	}
	rawWarnings, err := s.WorkBoard.ListStaleWarnings(ctx, workboard.StaleWarningFilter{
		ChangeRequestID: changeRequestID,
	})
	if err != nil {
		return ContextPackResult{}, err
	}
	warnings := make([]Warning, 0, len(rawWarnings))
	for _, w := range rawWarnings {
		warnings = append(warnings, Warning{
			Code:       string(w.Code),
			Message:    w.Message,
			ArtifactID: w.ArtifactID,
		})
	}
	provenance, provWarnings := buildKnowledgeProvenance(ctx, s.Knowledge, featureID, cr.ID)
	warnings = append(warnings, provWarnings...)

	outstanding := ""
	unresolvedGates := ""
	if runs, runErr := s.WorkBoard.ListGateRuns(ctx, changeRequestID, 50); runErr == nil {
		outstanding = outstandingReviewFeedback(runs)
		unresolvedGates = unresolvedQualityGates(runs)
	}

	renderCR := *cr
	if rows, acErr := s.WorkBoard.ListAcceptanceCriteria(ctx, changeRequestID); acErr == nil && len(rows) > 0 {
		items := make([]string, 0, len(rows))
		for _, row := range rows {
			if text := strings.TrimSpace(row.Text); text != "" {
				items = append(items, text)
			}
		}
		if encoded, marshalErr := json.Marshal(items); marshalErr == nil {
			renderCR.AcceptanceCriteria = string(encoded)
		}
	}

	sourceArtifactID := cr.ContextPackArtifactID
	if strings.TrimSpace(sourceArtifactID) == "" {
		sourceArtifactID = cr.LeadArtifactID
	}
	if strings.TrimSpace(sourceArtifactID) == "" && feature != nil {
		sourceArtifactID = feature.CanonicalArtifactID
	}
	state := "not_generated"
	markdown := ""
	// per spec §8: read governance level from the source artifact snapshot.
	// Snapshots without a governance level produce an empty GovernanceLevel
	// without error.
	governanceLevel := ""
	if id := strings.TrimSpace(sourceArtifactID); id != "" && s.Artifacts != nil {
		if art, artErr := s.Artifacts.Get(ctx, id); artErr == nil {
			state = "assembled"
			if snap, snapErr := governanceprofile.ParseSnapshot(art.GatesProfileSnapshotJSON); snapErr == nil {
				governanceLevel = string(snap.GovernanceLevel)
			}
			if id == strings.TrimSpace(cr.ContextPackArtifactID) {
				if body, fileErr := s.Artifacts.FileContent(ctx, id, artifact.FixedKeyToPath("implementation_plan")); fileErr == nil {
					markdown = replaceMarkdownSection(
						strings.TrimSpace(string(body)),
						"Acceptance Criteria",
						formatAcceptanceCriteria(renderCR.AcceptanceCriteria),
					)
				}
			}
			if markdown == "" {
				markdown = renderContextPackMarkdown(ctx, s.Artifacts, s.Attachments, s.Skills, art, &renderCR, feature, provenance, lane, outstanding, unresolvedGates)
			}
		}
	}

	return ContextPackResult{
		Lane:                  lane,
		ChangeRequestID:       cr.ID,
		FeatureID:             featureID,
		SourceArtifactID:      sourceArtifactID,
		ContextPackArtifactID: cr.ContextPackArtifactID,
		State:                 state,
		Markdown:              markdown,
		KnowledgeProvenance:   provenance,
		Warnings:              warnings,
		GovernanceLevel:       governanceLevel,
	}, nil
}

// roleDisplayOrder is the canonical display order and labels for document roles.
var roleDisplayOrder = []struct {
	role  artifact.Role
	label string
}{
	{artifact.RoleSpec, "Spec"},
	{artifact.RoleDesign, "Design"},
	{artifact.RolePlan, "Implementation Plan"},
	{artifact.RoleVerification, "Verification"},
	{artifact.RoleResearch, "Research"},
	{artifact.RoleReference, "Reference"},
}

func renderContextPackMarkdown(
	ctx context.Context,
	artifacts ContextPackArtifactReader,
	attachments ContextPackAttachmentReader,
	skillReader ContextPackSkillReader,
	art *artifact.Artifact,
	cr *workboard.ChangeRequest,
	feature *workboard.Feature,
	provenance []ProvenanceRow,
	lane string,
	outstanding string,
	unresolvedGates string,
) string {
	if art == nil {
		return ""
	}

	var profile governanceprofile.ResolvedProfile
	if snap := strings.TrimSpace(art.GatesProfileSnapshotJSON); snap != "" {
		_ = json.Unmarshal([]byte(snap), &profile)
	}

	switch profile.RendererKey {
	default:
		return renderRoleBasedPack(ctx, artifacts, attachments, skillReader, art, profile, cr, feature, provenance, lane, outstanding, unresolvedGates)
	}
}

func renderRoleBasedPack(
	ctx context.Context,
	artifacts ContextPackArtifactReader,
	attachments ContextPackAttachmentReader,
	skillReader ContextPackSkillReader,
	art *artifact.Artifact,
	profile governanceprofile.ResolvedProfile,
	cr *workboard.ChangeRequest,
	feature *workboard.Feature,
	provenance []ProvenanceRow,
	lane string,
	outstanding string,
	unresolvedGates string,
) string {
	read := func(path string) string {
		if artifacts == nil {
			return ""
		}
		b, err := artifacts.FileContent(ctx, art.ID, path)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}

	byRole := map[artifact.Role][]string{}
	for _, f := range art.Files {
		byRole[f.Role] = append(byRole[f.Role], f.Path)
	}
	for role := range byRole {
		sort.Strings(byRole[role])
	}

	if len(art.Files) == 0 && cr != nil {
		if impl := read(artifact.FixedKeyToPath("implementation_plan")); impl != "" {
			return impl
		}
	}

	var b strings.Builder
	section := func(title, body string) {
		if strings.TrimSpace(body) == "" {
			return
		}
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", title, body)
	}

	b.WriteString("# Implementation Context Pack\n\n")
	b.WriteString("## Coding Agent Instructions\n\n")
	b.WriteString("- Read this Context Pack before editing.\n")
	b.WriteString("- Treat the approved spec as the implementation contract — stronger than chat, tracker, or stale repo docs.\n")
	b.WriteString("- Stay inside the approved scope and acceptance criteria.\n")
	b.WriteString("- Use the SpecGate CLI for handoff lifecycle steps (`specgate work ...`, `specgate delivery report ...`, `specgate delivery review ...`).\n")
	b.WriteString("- Report blocking ambiguity / completion / docs-updated via `specgate delivery report`.\n\n")

	if cr != nil {
		featureKey := ""
		if feature != nil {
			featureKey = nonEmpty(feature.Key, feature.ID)
		}
		fmt.Fprintf(&b, "## Execution Brief\n\n- Work item: %s\n- Title: %s\n- Feature: %s\n- Work type: %s\n\n",
			nonEmpty(cr.Key, cr.ID), cr.Title, featureKey, string(cr.WorkType))
		if lane != "" {
			fmt.Fprintf(&b, "- Lane: %s\n\n", strings.ToUpper(lane))
		}
		section("Intent", cr.IntentMD)
		section("Acceptance Criteria", formatAcceptanceCriteria(cr.AcceptanceCriteria))
		if refs := renderKnowledgeReferences(provenance); refs != "" {
			fmt.Fprintf(&b, "### Knowledge References\n\n%s\n\n", refs)
		}
	}

	section("Outstanding Review Feedback", outstanding)
	section("Unresolved Quality Gates", unresolvedGates)

	if len(profile.RequiredRoles) > 0 {
		section("Required Roles", "Required roles for this change type: "+strings.Join(profile.RequiredRoles, ", "))
	}

	section("Applicable Skills", applicableSkillsSection(ctx, skillReader, profile.GateSkills))
	section("Domain Vocabulary", domainVocabularySection(read, art.Files))

	readRole := func(role artifact.Role) string {
		paths, ok := byRole[role]
		if !ok || len(paths) == 0 {
			return ""
		}
		var parts []string
		for _, p := range paths {
			if isDomainVocabularyPath(p, role) {
				continue
			}
			if c := read(p); c != "" {
				parts = append(parts, c)
			}
		}
		return strings.Join(parts, "\n\n")
	}

	for _, entry := range roleDisplayOrder {
		role := entry.role
		label := entry.label

		if cr != nil && lane != "" && role == artifact.RolePlan {
			paths := byRole[role]
			var lanePaths []string
			for _, p := range paths {
				base := strings.ToLower(p)
				switch lane {
				case "fe":
					if strings.Contains(base, "-be") || strings.Contains(base, "_be") {
						continue
					}
				case "be":
					if strings.Contains(base, "-fe") || strings.Contains(base, "_fe") {
						continue
					}
				}
				lanePaths = append(lanePaths, p)
			}
			if len(byRole[role]) == 0 {
				switch lane {
				case "fe":
					lanePaths = []string{artifact.FixedKeyToPath("tasks_fe")}
				case "be":
					lanePaths = []string{artifact.FixedKeyToPath("tasks_be")}
				}
			}
			var parts []string
			for _, p := range lanePaths {
				if c := read(p); c != "" {
					parts = append(parts, c)
				}
			}
			section(label, strings.Join(parts, "\n\n"))
			continue
		}

		if len(art.Files) == 0 && cr != nil {
			section(label, readFixedKeyRole(read, role, lane))
			continue
		}

		section(label, readRole(role))
	}

	if cr != nil && feature != nil {
		section("Reference Attachments", renderCodingAgentAttachments(ctx, attachments, feature))
	}

	var additionalParts []string
	for role, paths := range byRole {
		if role == artifact.RoleUnspecified || strings.HasPrefix(string(role), "custom:") {
			sort.Strings(paths)
			for _, p := range paths {
				if isDomainVocabularyPath(p, role) {
					continue
				}
				if c := read(p); c != "" {
					additionalParts = append(additionalParts, c)
				}
			}
		}
	}
	sort.Strings(additionalParts)
	section("Additional Documents", strings.Join(additionalParts, "\n\n"))

	if cr != nil && len(art.Files) == 0 {
		for _, line := range manifestScopeSections(read(artifact.FixedKeyToPath("manifest"))) {
			b.WriteString(line)
		}
	}

	return strings.TrimSpace(b.String())
}

func domainVocabularySection(read func(string) string, files []artifact.File) string {
	var paths []string
	seen := map[string]struct{}{}
	for _, f := range files {
		path := strings.TrimSpace(f.Path)
		if path == "" || !isDomainVocabularyPath(path, f.Role) {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var parts []string
	for _, path := range paths {
		if body := read(path); body != "" {
			parts = append(parts, fmt.Sprintf("### %s\n\n%s", path, body))
		}
	}
	return strings.Join(parts, "\n\n")
}

func isDomainVocabularyPath(path string, role artifact.Role) bool {
	roleName := strings.ToLower(strings.TrimSpace(string(role)))
	if strings.HasPrefix(roleName, "custom:") {
		for _, term := range []string{"glossary", "vocabulary", "domain", "ubiquitous-language", "context"} {
			if strings.Contains(roleName, term) {
				return true
			}
		}
	}

	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"))
	base := normalized
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	base = strings.TrimSuffix(base, ".md")
	base = strings.TrimSuffix(base, ".markdown")
	base = strings.ReplaceAll(base, "_", "-")
	base = strings.ReplaceAll(base, " ", "-")
	switch base {
	case "context", "glossary", "vocabulary", "domain-vocabulary", "domain-language", "ubiquitous-language":
		return true
	}
	return strings.Contains(normalized, "/glossary/") ||
		strings.Contains(normalized, "/vocabulary/") ||
		strings.Contains(normalized, "/domain-language/") ||
		strings.Contains(normalized, "/ubiquitous-language/")
}

func readFixedKeyRole(read func(string) string, role artifact.Role, lane string) string {
	switch role {
	case artifact.RoleSpec:
		prd := read(artifact.FixedKeyToPath("prd"))
		spec := read(artifact.FixedKeyToPath("spec"))
		return strings.TrimSpace(prd + "\n\n" + spec)
	case artifact.RoleDesign:
		return read(artifact.FixedKeyToPath("design"))
	case artifact.RolePlan:
		switch lane {
		case "fe":
			return read(artifact.FixedKeyToPath("tasks_fe"))
		case "be":
			return read(artifact.FixedKeyToPath("tasks_be"))
		default:
			fe := read(artifact.FixedKeyToPath("tasks_fe"))
			be := read(artifact.FixedKeyToPath("tasks_be"))
			return strings.TrimSpace(fe + "\n\n" + be)
		}
	case artifact.RoleVerification:
		return read(artifact.FixedKeyToPath("tasks_qa"))
	case artifact.RoleReference:
		rollout := read(artifact.FixedKeyToPath("rollout"))
		risks := read(artifact.FixedKeyToPath("risks"))
		return strings.TrimSpace(rollout + "\n\n" + risks)
	}
	return ""
}

func renderCodingAgentAttachments(ctx context.Context, attachments ContextPackAttachmentReader, feature *workboard.Feature) string {
	if attachments == nil || feature == nil {
		return ""
	}
	key := strings.TrimSpace(feature.Key)
	if key == "" {
		key = strings.TrimSpace(feature.ID)
	}
	rows, err := attachments.ListByFeature(ctx, key)
	if err != nil {
		return ""
	}
	var lines []string
	for _, a := range rows {
		if a.Audience != artifactattachment.AudienceCodingAgent && a.Audience != artifactattachment.AudienceBoth {
			continue
		}
		label := strings.TrimSpace(a.Title)
		if label == "" {
			label = string(a.Kind)
		}
		target := strings.TrimSpace(a.URL)
		if target == "" && strings.TrimSpace(a.GovernanceFileID) != "" {
			target = "/governance/files/" + strings.TrimSpace(a.GovernanceFileID) + "/content"
		}
		line := fmt.Sprintf("- [%s] %s: %s", a.Kind, label, target)
		if note := strings.TrimSpace(a.Note); note != "" {
			line += " — " + note
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func applicableSkillsSection(ctx context.Context, skillReader ContextPackSkillReader, gateSkills map[string]string) string {
	if skillReader == nil || len(gateSkills) == 0 {
		return ""
	}
	want := map[string]struct{}{}
	for _, name := range gateSkills {
		if n := strings.TrimSpace(name); n != "" {
			want[n] = struct{}{}
		}
	}
	if len(want) == 0 {
		return ""
	}
	all, err := skillReader.List(ctx)
	if err != nil {
		return ""
	}
	byName := make(map[string]skills.Skill, len(all))
	for _, sk := range all {
		byName[strings.TrimSpace(sk.Name)] = sk
	}
	names := make([]string, 0, len(want))
	for n := range want {
		names = append(names, n)
	}
	sort.Strings(names)

	var lines []string
	for _, n := range names {
		sk, ok := byName[n]
		if !ok {
			continue
		}
		line := fmt.Sprintf("- %s", sk.Name)
		if desc := strings.TrimSpace(sk.Description); desc != "" {
			line += " — " + desc
		}
		line += fmt.Sprintf(": specgate://skills/%s", sk.ID)
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	return "_Apply these team Skills as you implement. Pull each Skill's current version via its specgate://skills/{id} resource._\n" +
		strings.Join(lines, "\n")
}

// buildKnowledgeProvenance queries linked knowledge documents and maps them to
// ProvenanceRow slices for inclusion in the CR-scoped context pack (per spec §3).
// It is non-fatal: repo errors produce an empty slice + a Warning entry.
func buildKnowledgeProvenance(ctx context.Context, kr ContextPackKnowledgeReader, featureID, requestID string) ([]ProvenanceRow, []Warning) {
	if kr == nil {
		return []ProvenanceRow{}, nil
	}
	docs, err := kr.ListByFeatureOrRequest(ctx, featureID, requestID)
	if err != nil {
		slog.WarnContext(ctx, "knowledge provenance lookup failed", "feature_id", featureID, "request_id", requestID, "err", err)
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

func replaceMarkdownSection(markdown, title, body string) string {
	markdown = strings.TrimSpace(markdown)
	body = strings.TrimSpace(body)
	if markdown == "" || body == "" {
		return markdown
	}
	marker := "## " + title
	start := strings.Index(markdown, marker)
	if start < 0 {
		return strings.TrimSpace(markdown + "\n\n" + marker + "\n\n" + body)
	}
	contentStart := start + len(marker)
	end := len(markdown)
	if next := strings.Index(markdown[contentStart:], "\n## "); next >= 0 {
		end = contentStart + next
	}
	prefix := strings.TrimRight(markdown[:contentStart], "\n")
	suffix := strings.TrimLeft(markdown[end:], "\n")
	if suffix == "" {
		return prefix + "\n\n" + body
	}
	return prefix + "\n\n" + body + "\n\n" + suffix
}

// outstandingReviewFeedback turns the newest failed delivery_review GateRun into
// a markdown body listing the unmet/unclear criteria + failing checks.
func outstandingReviewFeedback(runs []workboard.GateRun) string {
	var latest *workboard.GateRun
	for i := range runs {
		if runs[i].Gate != "delivery_review" {
			continue
		}
		if latest == nil || runs[i].CreatedAt.After(latest.CreatedAt) {
			latest = &runs[i]
		}
	}
	if latest == nil || latest.State != workboard.NextActionState("fail") {
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
		if strings.EqualFold(strings.TrimSpace(c.Status), "fail") {
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
	return "_These quality gates did not pass at handoff. Account for them as you implement._\n" +
		strings.Join(lines, "\n")
}

// manifestScopeSections renders impacted services/apps/files + design refs from
// the manifest JSON as handoff sections.
func manifestScopeSections(manifestJSON string) []string {
	manifestJSON = strings.TrimSpace(manifestJSON)
	if manifestJSON == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(manifestJSON), &m); err != nil {
		return nil
	}
	strList := func(v any) []string {
		arr, ok := v.([]any)
		if !ok {
			return nil
		}
		out := make([]string, 0, len(arr))
		for _, x := range arr {
			if s := strings.TrimSpace(fmt.Sprint(x)); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	var out []string
	services, apps, files := strList(m["impacted_services"]), strList(m["impacted_apps"]), strList(m["files"])
	if len(services)+len(apps)+len(files) > 0 {
		var s strings.Builder
		s.WriteString("## Scope & Blast Radius\n\n")
		if len(services) > 0 {
			fmt.Fprintf(&s, "**Impacted services:** %s\n", strings.Join(services, ", "))
		}
		if len(apps) > 0 {
			fmt.Fprintf(&s, "**Impacted apps:** %s\n", strings.Join(apps, ", "))
		}
		if len(files) > 0 {
			s.WriteString("**Files likely touched:**\n")
			for _, f := range files {
				fmt.Fprintf(&s, "- %s\n", f)
			}
		}
		s.WriteString("\n")
		out = append(out, s.String())
	}
	if refs, ok := m["design_refs"].([]any); ok && len(refs) > 0 {
		var s strings.Builder
		s.WriteString("## Design References\n\n")
		for _, r := range refs {
			if rm, ok := r.(map[string]any); ok {
				url := strings.TrimSpace(fmt.Sprint(rm["url"]))
				if url == "" || url == "<nil>" {
					continue
				}
				label := strings.TrimSpace(fmt.Sprint(rm["type"]))
				if label == "" || label == "<nil>" {
					label = "design"
				}
				fmt.Fprintf(&s, "- [%s] %s\n", label, url)
			}
		}
		s.WriteString("\n")
		out = append(out, s.String())
	}
	return out
}
