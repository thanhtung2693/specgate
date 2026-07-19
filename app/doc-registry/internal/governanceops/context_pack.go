package governanceops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/artifactattachment"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/knowledge"
	"github.com/specgate/doc-registry/internal/skills"
	"github.com/specgate/doc-registry/internal/workboard"
)

const (
	maxContextPackChars     = 96 * 1024
	contextTruncationMarker = "\n\n<!-- specgate: context pack truncated to stay within the model-input budget -->\n\n"
)

// ContextPack assembles the on-read handoff pack for a change request or
// artifact. Kind must be "change_request" or "artifact".
func (s *Service) ContextPack(ctx context.Context, in ContextPackInput) (ContextPackResult, error) {
	if in.Kind == "artifact" {
		return s.contextPackForArtifact(ctx, in.ID)
	}
	return s.contextPackForCR(ctx, in.ID)
}

func (s *Service) contextPackForArtifact(ctx context.Context, artifactID string) (ContextPackResult, error) {
	if s.Artifacts == nil {
		return ContextPackResult{}, fmt.Errorf("%w: artifact reader is not configured", ErrUnavailable)
	}
	art, err := s.Artifacts.Get(ctx, artifactID)
	if err != nil {
		if errors.Is(err, artifact.ErrNotFound) {
			return ContextPackResult{}, workboard.ErrNotFound
		}
		return ContextPackResult{}, err
	}
	if selected := trustedWorkspace(ctx); selected != "" && strings.TrimSpace(art.WorkspaceID) != selected {
		return ContextPackResult{}, workboard.ErrNotFound
	}
	ctx = skills.WithWorkspace(ctx, art.WorkspaceID)
	profile, err := parseContextPackProfile(art)
	if err != nil {
		return ContextPackResult{}, err
	}
	markdown, err := renderContextPackMarkdown(ctx, s.Artifacts, nil, s.Skills, art, profile, nil, nil, nil, "", "")
	if err != nil {
		return ContextPackResult{}, err
	}
	markdown = capContextPack(markdown)

	return ContextPackResult{
		Kind:                "artifact",
		ArtifactID:          art.ID,
		State:               "assembled",
		Markdown:            markdown,
		KnowledgeProvenance: []ProvenanceRow{},
		Warnings:            []Warning{},
		GovernanceLevel:     string(profile.GovernanceLevel),
	}, nil
}

func (s *Service) contextPackForCR(ctx context.Context, changeRequestID string) (ContextPackResult, error) {
	if s.WorkBoard == nil {
		return ContextPackResult{}, ErrUnavailable
	}
	cr, err := s.WorkBoard.GetChangeRequest(ctx, changeRequestID)
	if err != nil {
		return ContextPackResult{}, err
	}
	if err := requireChangeRequestWorkspace(ctx, cr); err != nil {
		return ContextPackResult{}, err
	}
	ctx = skills.WithWorkspace(ctx, cr.WorkspaceID)
	var feature *workboard.Feature
	featureID := strings.TrimSpace(cr.FeatureID)
	if featureID != "" {
		loaded, err := s.WorkBoard.GetFeature(ctx, featureID)
		if err != nil {
			return ContextPackResult{}, err
		}
		if err := requireFeatureWorkspace(ctx, loaded); err != nil {
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
	featureRefs := []string{}
	if featureID != "" {
		featureRefs = append(featureRefs, featureID)
	}
	if feature != nil && strings.TrimSpace(feature.Key) != "" && feature.Key != featureID {
		featureRefs = append(featureRefs, strings.TrimSpace(feature.Key))
	}
	provenance, provWarnings := buildKnowledgeProvenance(ctx, s.Knowledge, cr.WorkspaceID, featureRefs, cr.ID)
	warnings = append(warnings, provWarnings...)

	outstanding := ""
	unresolvedGates := ""
	runs, runErr := s.WorkBoard.ListGateRuns(ctx, changeRequestID, 50)
	if current, ok := s.WorkBoard.(interface {
		CurrentGateRuns(context.Context, string) ([]workboard.GateRun, error)
	}); ok {
		runs, runErr = current.CurrentGateRuns(ctx, changeRequestID)
	}
	if runErr != nil {
		return ContextPackResult{}, fmt.Errorf("%w: gate state: %v", ErrUnavailable, runErr)
	}
	var authoritative *workboard.GateRun
	authoritative, authoritativeErr := authoritativeDeliveryReviewRun(
		ctx,
		s.WorkBoard,
		changeRequestID,
	)
	if authoritativeErr != nil {
		return ContextPackResult{}, fmt.Errorf("%w: delivery review state: %v", ErrUnavailable, authoritativeErr)
	}
	reviewOutdated := false
	completion, completionErr := s.latestCompletionRecord(ctx, changeRequestID)
	if completionErr != nil {
		return ContextPackResult{}, fmt.Errorf("%w: completion state: %v", ErrUnavailable, completionErr)
	}
	if completion != nil {
		evidenceJSON := ""
		if authoritative != nil {
			evidenceJSON = authoritative.EvidenceJSON
		}
		wrapper, _ := decodeDeliveryReview(evidenceJSON)
		reviewOutdated = authoritative == nil ||
			strings.TrimSpace(wrapper.CompletionFeedbackEventID) != completion.Event.ID
	}
	if reviewOutdated {
		filtered := make([]workboard.GateRun, 0, len(runs))
		for i := range runs {
			if runs[i].Gate != "delivery_review" {
				filtered = append(filtered, runs[i])
			}
		}
		runs = filtered
		authoritative = nil
	}
	if authoritative != nil {
		outstanding = outstandingReviewFeedback([]workboard.GateRun{*authoritative})
		filtered := make([]workboard.GateRun, 0, len(runs)+1)
		for i := range runs {
			if runs[i].Gate != "delivery_review" {
				filtered = append(filtered, runs[i])
			}
		}
		runs = append(filtered, *authoritative)
	}
	if authoritative == nil && !reviewOutdated {
		outstanding = outstandingReviewFeedback(runs)
	}
	unresolvedGates = unresolvedQualityGates(runs)

	renderCR := *cr
	sourceArtifactID := cr.LeadArtifactID
	if strings.TrimSpace(sourceArtifactID) == "" && feature != nil {
		sourceArtifactID = feature.CanonicalArtifactID
	}
	var sourceArtifact *artifact.Artifact
	if id := strings.TrimSpace(sourceArtifactID); id != "" {
		if s.Artifacts == nil {
			return ContextPackResult{}, fmt.Errorf("%w: source artifact reader is not configured", ErrUnavailable)
		}
		art, artErr := s.Artifacts.Get(ctx, id)
		if artErr != nil {
			return ContextPackResult{}, fmt.Errorf("%w: source artifact %q: %v", ErrUnavailable, id, artErr)
		}
		if strings.TrimSpace(art.WorkspaceID) != strings.TrimSpace(cr.WorkspaceID) {
			return ContextPackResult{}, workboard.ErrNotFound
		}
		sourceArtifact = art
	}
	assemble := sourceArtifact != nil ||
		(strings.TrimSpace(sourceArtifactID) == "" && cr.WorkType == workboard.WorkTypeBugFix)
	if assemble {
		rows, err := s.WorkBoard.ListAcceptanceCriteria(ctx, changeRequestID)
		if err != nil {
			return ContextPackResult{}, err
		}
		items := make([]string, 0, len(rows))
		for _, row := range rows {
			if text := strings.TrimSpace(row.Text); text != "" {
				items = append(items, text)
			}
		}
		if len(items) == 0 {
			return ContextPackResult{}, fmt.Errorf("%w: canonical acceptance criteria are unavailable", ErrValidation)
		}
		encoded, err := json.Marshal(items)
		if err != nil {
			return ContextPackResult{}, err
		}
		renderCR.AcceptanceCriteria = string(encoded)
	}
	// spec_repo_drift is an artifact-scoped readiness run, never a CR gate_run,
	// so ListGateRuns above cannot see it. Pull it from the source artifact's
	// readiness runs and merge its findings into Unresolved Quality Gates, or the
	// drift warn is silently dropped from the full-route handoff (per agents spec §6).
	if s.ReadinessRuns != nil {
		if id := strings.TrimSpace(sourceArtifactID); id != "" {
			rruns, rErr := s.ReadinessRuns.ListReadinessRuns(ctx, id, 50)
			if rErr != nil {
				return ContextPackResult{}, fmt.Errorf("%w: readiness state: %v", ErrUnavailable, rErr)
			}
			unresolvedGates = mergeDriftReadiness(unresolvedGates, rruns)
		}
	}
	state := "not_generated"
	markdown := ""
	governanceLevel := ""
	if sourceArtifact != nil {
		profile, profileErr := parseContextPackProfile(sourceArtifact)
		if profileErr != nil {
			return ContextPackResult{}, profileErr
		}
		state = "assembled"
		governanceLevel = string(profile.GovernanceLevel)
		if markdown == "" {
			markdown, err = renderContextPackMarkdown(ctx, s.Artifacts, s.Attachments, s.Skills, sourceArtifact, profile, &renderCR, feature, provenance, outstanding, unresolvedGates)
			if err != nil {
				return ContextPackResult{}, err
			}
			markdown = capContextPack(markdown)
		}
	} else if assemble {
		state = "assembled"
		markdown = capContextPack(renderQuickContextPack(&renderCR, feature, provenance, outstanding, unresolvedGates))
	}

	return ContextPackResult{
		ChangeRequestID:     cr.ID,
		FeatureID:           featureID,
		SourceArtifactID:    sourceArtifactID,
		State:               state,
		Markdown:            markdown,
		KnowledgeProvenance: provenance,
		Warnings:            warnings,
		GovernanceLevel:     governanceLevel,
	}, nil
}

func capContextPack(markdown string) string {
	if len(markdown) <= maxContextPackChars {
		return markdown
	}
	available := maxContextPackChars - len(contextTruncationMarker)
	if available <= 0 {
		return contextTruncationMarker
	}
	headBytes := available * 3 / 4
	tailBytes := available - headBytes
	headBytes = utf8PrefixBoundary(markdown, headBytes)
	tailStart := utf8SuffixBoundary(markdown, len(markdown)-tailBytes)
	return markdown[:headBytes] + contextTruncationMarker + markdown[tailStart:]
}

func utf8PrefixBoundary(value string, end int) int {
	if end >= len(value) {
		return len(value)
	}
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return end
}

func utf8SuffixBoundary(value string, start int) int {
	if start <= 0 {
		return 0
	}
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return start
}

func renderQuickContextPack(
	cr *workboard.ChangeRequest,
	feature *workboard.Feature,
	provenance []ProvenanceRow,
	outstanding string,
	unresolvedGates string,
) string {
	var b strings.Builder
	b.WriteString("# Implementation Context Pack\n\n")
	b.WriteString("## Quick Handoff\n\n")
	b.WriteString("This is quick-route work. The persisted ChangeRequest and acceptance criteria are the implementation contract.\n\n")
	featureKey := "none"
	if feature != nil {
		featureKey = nonEmpty(feature.Key, feature.ID)
	}
	fmt.Fprintf(&b, "## Execution Brief\n\n- Work item: %s\n- Title: %s\n- Feature: %s\n- Work type: %s\n\n",
		nonEmpty(cr.Key, cr.ID), cr.Title, featureKey, cr.WorkType)
	if intent := strings.TrimSpace(cr.IntentMD); intent != "" {
		fmt.Fprintf(&b, "## Intent\n\n%s\n\n", intent)
	}
	fmt.Fprintf(&b, "## Acceptance Criteria\n\n%s\n\n", formatAcceptanceCriteria(cr.AcceptanceCriteria))
	if refs := renderKnowledgeReferences(provenance); refs != "" {
		fmt.Fprintf(&b, "## Knowledge References\n\n%s\n\n", refs)
	}
	if strings.TrimSpace(outstanding) != "" {
		fmt.Fprintf(&b, "## Outstanding Review Feedback\n\n%s\n\n", outstanding)
	}
	if strings.TrimSpace(unresolvedGates) != "" {
		fmt.Fprintf(&b, "## Unresolved Quality Gates\n\n%s\n\n", unresolvedGates)
	}
	b.WriteString("## Coding Agent Instructions\n\n- Stay inside the persisted acceptance criteria.\n- Update repo-owned docs when shipped behavior changes.\n- Report completion or blockers with `specgate delivery report`.\n")
	return strings.TrimSpace(b.String())
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
	profile governanceprofile.ParsedSnapshot,
	cr *workboard.ChangeRequest,
	feature *workboard.Feature,
	provenance []ProvenanceRow,
	outstanding string,
	unresolvedGates string,
) (string, error) {
	if art == nil {
		return "", nil
	}

	return renderRoleBasedPack(ctx, artifacts, attachments, skillReader, art, profile, cr, feature, provenance, outstanding, unresolvedGates)
}

func parseContextPackProfile(art *artifact.Artifact) (governanceprofile.ParsedSnapshot, error) {
	if art == nil {
		return governanceprofile.ParsedSnapshot{}, nil
	}
	profile, err := governanceprofile.ParseSnapshot(strings.TrimSpace(art.PolicySnapshotJSON))
	if err != nil {
		return governanceprofile.ParsedSnapshot{}, fmt.Errorf("source artifact %q policy snapshot: %w", art.ID, err)
	}
	return profile, nil
}

func renderRoleBasedPack(
	ctx context.Context,
	artifacts ContextPackArtifactReader,
	attachments ContextPackAttachmentReader,
	skillReader ContextPackSkillReader,
	art *artifact.Artifact,
	profile governanceprofile.ParsedSnapshot,
	cr *workboard.ChangeRequest,
	feature *workboard.Feature,
	provenance []ProvenanceRow,
	outstanding string,
	unresolvedGates string,
) (string, error) {
	read := func(path string) (string, error) {
		if artifacts == nil {
			return "", fmt.Errorf("%w: source artifact reader is not configured", ErrUnavailable)
		}
		b, err := artifacts.FileContent(ctx, art.ID, path)
		if err != nil {
			return "", fmt.Errorf("%w: source artifact %q file %q: %v", ErrUnavailable, art.ID, path, err)
		}
		return strings.TrimSpace(string(b)), nil
	}

	byRole := map[artifact.Role][]string{}
	for _, f := range art.Files {
		byRole[f.Role] = append(byRole[f.Role], f.Path)
	}
	for role := range byRole {
		sort.Strings(byRole[role])
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

	readRole := func(role artifact.Role) (string, error) {
		paths, ok := byRole[role]
		if !ok || len(paths) == 0 {
			return "", nil
		}
		var parts []string
		for _, p := range paths {
			c, err := read(p)
			if err != nil {
				return "", err
			}
			if c != "" {
				parts = append(parts, c)
			}
		}
		return strings.Join(parts, "\n\n"), nil
	}

	for _, entry := range roleDisplayOrder {
		role := entry.role
		label := entry.label

		content, err := readRole(role)
		if err != nil {
			return "", err
		}
		section(label, content)
	}

	if cr != nil && feature != nil {
		references, err := renderCodingAgentAttachments(ctx, attachments, cr.WorkspaceID, feature)
		if err != nil {
			return "", err
		}
		section("Reference Attachments", references)
	}

	var additionalPaths []string
	for role, paths := range byRole {
		if role == artifact.RoleUnspecified || strings.HasPrefix(string(role), "custom:") {
			additionalPaths = append(additionalPaths, paths...)
		}
	}
	sort.Strings(additionalPaths)
	var additionalParts []string
	for _, path := range additionalPaths {
		content, err := read(path)
		if err != nil {
			return "", err
		}
		if content != "" {
			additionalParts = append(additionalParts, content)
		}
	}
	section("Additional Documents", strings.Join(additionalParts, "\n\n"))

	return strings.TrimSpace(b.String()), nil
}

func renderCodingAgentAttachments(ctx context.Context, attachments ContextPackAttachmentReader, workspaceID string, feature *workboard.Feature) (string, error) {
	if attachments == nil || feature == nil {
		return "", nil
	}
	key := strings.TrimSpace(feature.Key)
	if key == "" {
		key = strings.TrimSpace(feature.ID)
	}
	rows, err := attachments.ListByFeature(ctx, workspaceID, key)
	if err != nil {
		return "", fmt.Errorf("%w: reference attachments: %v", ErrUnavailable, err)
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
	return strings.Join(lines, "\n"), nil
}

func applicableSkillsSection(ctx context.Context, skillReader ContextPackSkillReader, gateSkills map[string]string) string {
	if len(gateSkills) == 0 {
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
	var all []skills.Skill
	if skillReader != nil {
		all, _ = skillReader.List(ctx)
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
		line := "- " + n
		if ok {
			if desc := strings.TrimSpace(sk.Description); desc != "" {
				line += " — " + desc
			}
			line += fmt.Sprintf(" (Skill ID: %s)", sk.ID)
		}
		lines = append(lines, line)
	}
	return "_Skill names come from the frozen artifact policy. Current catalog metadata is shown when available; gate evaluation must use the frozen rubric in the artifact policy snapshot or gate task._\n" +
		strings.Join(lines, "\n")
}

// buildKnowledgeProvenance queries linked knowledge documents and maps them to
// ProvenanceRow slices for inclusion in the CR-scoped context pack (per spec §3).
// It is non-fatal: repo errors produce an empty slice + a Warning entry.
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
