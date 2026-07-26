package governanceops

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/artifactattachment"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/skills"
	"github.com/specgate/doc-registry/internal/workboard"
)

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
