package local

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
)

var ErrArtifactPairInvalid = errors.New("invalid artifact pair")

type ArtifactImpact struct {
	BaseArtifactID         string                  `json:"base_artifact_id"`
	TargetArtifactID       string                  `json:"target_artifact_id"`
	BaseSnapshotDigest     string                  `json:"base_snapshot_digest"`
	TargetSnapshotDigest   string                  `json:"target_snapshot_digest"`
	BaseStatus             string                  `json:"base_status"`
	TargetStatus           string                  `json:"target_status"`
	RequirementImpact      string                  `json:"requirement_impact"`
	RequirementReason      string                  `json:"requirement_reason,omitempty"`
	Transitions            []RequirementTransition `json:"transitions,omitempty"`
	AddedRequirements      []SourceCriterion       `json:"added_requirements,omitempty"`
	RemovedRequirements    []SourceCriterion       `json:"removed_requirements,omitempty"`
	DeferredRequirements   []SourceCriterion       `json:"deferred_requirements,omitempty"`
	UnassignedRequirements []SourceCriterion       `json:"unassigned_requirements,omitempty"`
	DocumentDelta          ArtifactDocumentDelta   `json:"document_delta"`
	BaseWorkItems          []ImpactWorkLink        `json:"base_work_items,omitempty"`
	TargetWorkItems        []ImpactWorkLink        `json:"target_work_items,omitempty"`
	UnlinkedWorkItems      []ImpactWorkLink        `json:"unlinked_work_items,omitempty"`
	SourceOverlapState     string                  `json:"source_overlap_state"`
	SourceOverlaps         []ArtifactImpactOverlap `json:"source_overlaps,omitempty"`
	SourceOverlapTruncated bool                    `json:"source_overlap_truncated,omitempty"`
	PathOverlapState       string                  `json:"path_overlap_state"`
	PathOverlaps           []ArtifactPathOverlap   `json:"path_overlaps,omitempty"`
	PathOverlapTruncated   bool                    `json:"path_overlap_truncated,omitempty"`
}

// RequirementTransition compares recorded fields only; it does not claim
// semantic equivalence between the source documents.
type RequirementTransition struct {
	BaseID        string            `json:"base_id"`
	TargetIDs     []string          `json:"target_ids"`
	Reason        string            `json:"reason,omitempty"`
	Before        SourceCriterion   `json:"before"`
	After         []SourceCriterion `json:"after"`
	ChangedFields []string          `json:"changed_fields"`
	Summary       string            `json:"summary"`
}

// ImpactWorkLink is a recorded work-to-source association for one immutable
// artifact version. It is traceability, not a claim that source text or files
// outside the explicit mapping are unaffected.
type ImpactWorkLink struct {
	ID               string   `json:"id"`
	Key              string   `json:"key"`
	Title            string   `json:"title"`
	ArtifactID       string   `json:"artifact_id"`
	Phase            string   `json:"phase"`
	SourceCriteria   []string `json:"source_criteria"`
	ReviewID         string   `json:"review_id,omitempty"`
	ReportID         string   `json:"report_id,omitempty"`
	AffectedReportID string   `json:"affected_report_id,omitempty"`
	Evidence         string   `json:"evidence"`
	HumanDecision    string   `json:"human_decision,omitempty"`
	AffectedFiles    []string `json:"affected_files,omitempty"`
	RepositoryID     string   `json:"repository_id,omitempty"`
	Inspection       string   `json:"inspection,omitempty"`
}

type ArtifactPathOverlap struct {
	Path      string   `json:"path"`
	WorkKeys  []string `json:"work_keys"`
	ReportIDs []string `json:"report_ids"`
}

// ArtifactImpactOverlap is a bounded coordination hint from exact recorded
// source links. It does not infer file ownership, dependency, or merge safety.
type ArtifactImpactOverlap struct {
	SourceCriterion string   `json:"source_criterion"`
	WorkKeys        []string `json:"work_keys"`
	ReportIDs       []string `json:"report_ids,omitempty"`
}

// ArtifactDocumentDelta compares immutable package document identities only.
// It does not interpret document prose or infer requirement relationships.
type ArtifactDocumentDelta struct {
	Added    []string `json:"added,omitempty"`
	Removed  []string `json:"removed,omitempty"`
	Modified []string `json:"modified,omitempty"`
}

// ArtifactImpact compares only the caller's exact same-feature pair. It does
// not walk version history: a target's lineage applies only when it names this
// exact base ID and digest.
func (s *Store) ArtifactImpact(ctx context.Context, workspaceID, targetID, baseID string) (ArtifactImpact, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ArtifactImpact{}, err
	}
	defer tx.Rollback()
	target, err := getArtifact(ctx, tx, workspaceID, targetID)
	if err != nil {
		return ArtifactImpact{}, err
	}
	base, err := getArtifact(ctx, tx, workspaceID, baseID)
	if err != nil {
		return ArtifactImpact{}, err
	}
	return artifactImpactPairQuery(ctx, tx, workspaceID, target, base)
}

// PreviewArtifactImpact applies publication validation to an unpublished
// candidate, then uses the same exact-pair projection as stored impact.
func (s *Store) PreviewArtifactImpact(ctx context.Context, workspaceID, baseID string, input ArtifactInput) (ArtifactImpact, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ArtifactImpact{}, err
	}
	defer tx.Rollback()
	input, documents, documentDigest, criteria, err := normalizedArtifactInput(input)
	if err != nil {
		return ArtifactImpact{}, err
	}
	base, err := getArtifact(ctx, tx, workspaceID, baseID)
	if err != nil {
		return ArtifactImpact{}, err
	}
	var latest int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM artifacts WHERE workspace_id=? AND feature_key=?`, workspaceID, input.FeatureKey).Scan(&latest); err != nil {
		return ArtifactImpact{}, err
	}
	if base.FeatureKey != input.FeatureKey || base.Version != latest || strings.TrimSpace(input.BaseVersion) != fmt.Sprintf("v%d", base.Version) {
		return ArtifactImpact{}, fmt.Errorf("%w: preview base is not the current exact feature version", ErrArtifactVersionConflict)
	}
	if input.SourceLineage != nil {
		if !sourceInventoryKnown(base) {
			return ArtifactImpact{}, fmt.Errorf("source_lineage cannot declare impact because the base requirement inventory is unknown")
		}
		lineage, err := validateSourceLineage(*input.SourceLineage, base.ID, base.SnapshotDigest, base.SourceCriteria, criteria)
		if err != nil {
			return ArtifactImpact{}, err
		}
		input.SourceLineage = &lineage
	}
	digest, _, err := artifactSnapshotDigest(documentDigest, criteria, input.SourceLineage)
	if err != nil {
		return ArtifactImpact{}, err
	}
	target := Artifact{ID: "unpublished", WorkspaceID: workspaceID, FeatureKey: input.FeatureKey, Status: "draft", SnapshotDigest: digest, Documents: documents, SourceCriteria: criteria, SourceLineage: input.SourceLineage}
	return artifactImpactPairQuery(ctx, tx, workspaceID, target, base)
}

func artifactImpactPairQuery(ctx context.Context, q artifactQueryer, workspaceID string, target, base Artifact) (ArtifactImpact, error) {
	if target.FeatureKey != base.FeatureKey {
		return ArtifactImpact{}, fmt.Errorf("%w: artifact impact requires artifacts from the same feature", ErrArtifactPairInvalid)
	}
	impact := ArtifactImpact{BaseArtifactID: base.ID, TargetArtifactID: target.ID, BaseSnapshotDigest: base.SnapshotDigest, TargetSnapshotDigest: target.SnapshotDigest, BaseStatus: base.Status, TargetStatus: target.Status, RequirementImpact: "unavailable", RequirementReason: "target has no lineage for this exact base pair", DocumentDelta: artifactDocumentDelta(base.Documents, target.Documents), SourceOverlapState: "unavailable"}
	baseLinks, baseUnlinked, err := artifactImpactWorkLinksQuery(ctx, q, workspaceID, base)
	if err != nil {
		return ArtifactImpact{}, err
	}
	targetLinks, targetUnlinked, err := artifactImpactWorkLinksQuery(ctx, q, workspaceID, target)
	if err != nil {
		return ArtifactImpact{}, err
	}
	impact.BaseWorkItems, impact.TargetWorkItems = baseLinks, targetLinks
	impact.UnlinkedWorkItems = append(baseUnlinked, targetUnlinked...)
	sort.Slice(impact.UnlinkedWorkItems, func(i, j int) bool { return impact.UnlinkedWorkItems[i].Key < impact.UnlinkedWorkItems[j].Key })
	impact.PathOverlaps, impact.PathOverlapState, impact.PathOverlapTruncated = artifactPathOverlaps(append(append([]ImpactWorkLink{}, baseLinks...), targetLinks...))
	if len(impact.DocumentDelta.Added)+len(impact.DocumentDelta.Removed)+len(impact.DocumentDelta.Modified) > 0 {
		for i := range impact.BaseWorkItems {
			impact.BaseWorkItems[i].Inspection = "unassessed_changed_documents"
		}
	}
	if target.SourceLineage == nil || target.SourceLineage.BaseArtifactID != base.ID || target.SourceLineage.BaseDigest != base.SnapshotDigest {
		return impact, nil
	}
	if !sourceInventoryKnown(base) {
		impact.RequirementReason = "base requirement inventory is unknown"
		return impact, nil
	}
	impact.RequirementImpact = "declared"
	impact.RequirementReason = ""
	impact.SourceOverlaps, impact.SourceOverlapTruncated = artifactSourceOverlaps(impact.BaseWorkItems, impact.TargetWorkItems, *target.SourceLineage)
	impact.SourceOverlapState = "no_recorded_overlap"
	if len(impact.SourceOverlaps) > 0 {
		impact.SourceOverlapState = "recorded"
	} else if len(impact.UnlinkedWorkItems) > 0 {
		impact.SourceOverlapState = "unknown"
	}
	baseCriteria := make(map[string]SourceCriterion, len(base.SourceCriteria))
	for _, criterion := range base.SourceCriteria {
		baseCriteria[criterion.ID] = criterion
	}
	targetCriteria := make(map[string]SourceCriterion, len(target.SourceCriteria))
	for _, criterion := range target.SourceCriteria {
		targetCriteria[criterion.ID] = criterion
	}
	for _, row := range target.SourceLineage.Rows {
		before := baseCriteria[row.BaseID]
		transition := RequirementTransition{BaseID: row.BaseID, TargetIDs: append([]string{}, row.TargetIDs...), Reason: row.Reason, Before: before, After: []SourceCriterion{}, ChangedFields: []string{}}
		for _, id := range row.TargetIDs {
			transition.After = append(transition.After, targetCriteria[id])
		}
		if len(transition.After) == 0 {
			transition.ChangedFields = append(transition.ChangedFields, "removed")
		} else {
			for _, field := range []struct {
				name    string
				changed func(SourceCriterion) bool
			}{{"id", func(after SourceCriterion) bool { return before.ID != after.ID }}, {"text", func(after SourceCriterion) bool { return before.Text != after.Text }}, {"source_path", func(after SourceCriterion) bool { return before.SourcePath != after.SourcePath }}, {"deferred_reason", func(after SourceCriterion) bool { return before.DeferredReason != after.DeferredReason }}} {
				for _, after := range transition.After {
					if field.changed(after) {
						transition.ChangedFields = append(transition.ChangedFields, field.name)
						break
					}
				}
			}
		}
		transition.Summary = "changed recorded fields"
		if len(transition.ChangedFields) == 0 {
			transition.Summary = "unchanged recorded fields"
		}
		impact.Transitions = append(impact.Transitions, transition)
		if len(transition.ChangedFields) > 0 {
			for i := range impact.BaseWorkItems {
				for _, source := range impact.BaseWorkItems[i].SourceCriteria {
					if source == row.BaseID {
						impact.BaseWorkItems[i].Inspection = "inspect_changed_source"
					}
				}
			}
		}
		if len(row.TargetIDs) == 0 {
			impact.RemovedRequirements = append(impact.RemovedRequirements, baseCriteria[row.BaseID])
		}
	}
	for _, id := range target.SourceLineage.Added {
		impact.AddedRequirements = append(impact.AddedRequirements, targetCriteria[id])
		assigned := false
		for _, work := range impact.TargetWorkItems {
			for _, source := range work.SourceCriteria {
				assigned = assigned || source == id
			}
		}
		if !assigned {
			impact.UnassignedRequirements = append(impact.UnassignedRequirements, targetCriteria[id])
		}
	}
	for _, criterion := range target.SourceCriteria {
		if criterion.DeferredReason != "" {
			impact.DeferredRequirements = append(impact.DeferredRequirements, criterion)
		}
	}
	sort.Slice(impact.AddedRequirements, func(i, j int) bool { return impact.AddedRequirements[i].ID < impact.AddedRequirements[j].ID })
	sort.Slice(impact.RemovedRequirements, func(i, j int) bool { return impact.RemovedRequirements[i].ID < impact.RemovedRequirements[j].ID })
	sort.Slice(impact.DeferredRequirements, func(i, j int) bool { return impact.DeferredRequirements[i].ID < impact.DeferredRequirements[j].ID })
	return impact, nil
}

func artifactImpactWorkLinksQuery(ctx context.Context, q artifactQueryer, workspaceID string, artifact Artifact) ([]ImpactWorkLink, []ImpactWorkLink, error) {
	items, err := listWork(ctx, q, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	known := make(map[string]bool, len(artifact.SourceCriteria))
	for _, criterion := range artifact.SourceCriteria {
		known[criterion.ID] = true
	}
	links, unlinked := []ImpactWorkLink{}, []ImpactWorkLink{}
	for _, item := range items {
		if item.ArtifactID != artifact.ID {
			continue
		}
		link := ImpactWorkLink{ID: item.ID, Key: item.Key, Title: item.Title, ArtifactID: item.ArtifactID, Phase: item.Phase, SourceCriteria: impactSourceCriteria(item.AcceptanceCriteria, known), Evidence: "not_reviewed"}
		if review, err := deliveryStatus(ctx, q, workspaceID, item); err == nil {
			link.ReviewID, link.ReportID, link.Evidence, link.HumanDecision = review.ID, review.ReportID, review.Verdict, review.HumanDecision
		} else if err != sql.ErrNoRows {
			return nil, nil, err
		}
		if report, err := latestDeliveryReportQuery(ctx, q, workspaceID, item.ID); err == nil {
			link.AffectedReportID = report.ID
			link.AffectedFiles, link.RepositoryID = impactReportPaths(report.Body)
		} else if err != sql.ErrNoRows {
			return nil, nil, err
		}
		if len(link.SourceCriteria) == 0 {
			unlinked = append(unlinked, link)
		} else {
			links = append(links, link)
		}
	}
	sort.Slice(links, func(i, j int) bool { return links[i].Key < links[j].Key })
	sort.Slice(unlinked, func(i, j int) bool { return unlinked[i].Key < unlinked[j].Key })
	return links, unlinked, nil
}

func impactReportPaths(body map[string]any) ([]string, string) {
	receipt, _ := body["git_receipt"].(map[string]any)
	if receipt["availability"] != "available" {
		return nil, ""
	}
	var identity string
	switch receipt["freshness_scope"] {
	case "shared_repository":
		if repo, _ := receipt["repository"].(string); repo != "" {
			identity = "remote:" + repo
		}
	case "local_checkout":
		if checkout, _ := receipt["checkout_id"].(string); checkout != "" {
			identity = "checkout:" + checkout
		}
	}
	raw, _ := receipt["reported_files"].([]any)
	if raw == nil {
		raw, _ = body["affected_files"].([]any)
	}
	seen := map[string]bool{}
	paths := []string{}
	for _, value := range raw {
		name, ok := value.(string)
		if !ok || name == "" || strings.Contains(name, "\\") || path.IsAbs(name) {
			continue
		}
		clean := path.Clean(name)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || seen[clean] {
			continue
		}
		seen[clean] = true
		paths = append(paths, clean)
	}
	sort.Strings(paths)
	return paths, identity
}

func artifactPathOverlaps(links []ImpactWorkLink) ([]ArtifactPathOverlap, string, bool) {
	byPath := map[string][]ImpactWorkLink{}
	unknown, noncomparable := false, false
	for i, work := range links {
		if work.Phase == "delivered" {
			continue
		}
		if work.RepositoryID == "" || len(work.AffectedFiles) == 0 {
			unknown = true
			continue
		}
		for _, other := range links[i+1:] {
			if other.Phase != "delivered" && other.RepositoryID != "" && work.RepositoryID != other.RepositoryID {
				noncomparable = true
			}
		}
		for _, name := range work.AffectedFiles {
			byPath[work.RepositoryID+"\x00"+name] = append(byPath[work.RepositoryID+"\x00"+name], work)
		}
	}
	keys := make([]string, 0, len(byPath))
	for key, work := range byPath {
		if len(work) > 1 {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	overlaps := make([]ArtifactPathOverlap, 0, len(keys))
	for _, key := range keys {
		if len(overlaps) == 100 {
			return overlaps, "recorded", true
		}
		_, name, _ := strings.Cut(key, "\x00")
		overlap := ArtifactPathOverlap{Path: name, WorkKeys: []string{}, ReportIDs: []string{}}
		for _, work := range byPath[key] {
			overlap.WorkKeys = append(overlap.WorkKeys, work.Key)
			overlap.ReportIDs = append(overlap.ReportIDs, work.AffectedReportID)
		}
		sort.Strings(overlap.WorkKeys)
		sort.Strings(overlap.ReportIDs)
		overlaps = append(overlaps, overlap)
	}
	if len(overlaps) > 0 {
		return overlaps, "recorded", false
	}
	if unknown {
		return overlaps, "unknown", false
	}
	if noncomparable {
		return overlaps, "noncomparable", false
	}
	return overlaps, "no_recorded_overlap", false
}

func impactSourceCriteria(criteria []string, known map[string]bool) []string {
	seen := map[string]bool{}
	for _, criterion := range criteria {
		for _, token := range strings.Fields(criterion) {
			token = strings.Trim(strings.TrimSpace(token), ".,;:!?)]}\"'")
			if id := strings.TrimPrefix(token, "@source:"); id != token && known[id] {
				seen[id] = true
			}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func artifactSourceOverlaps(base, target []ImpactWorkLink, lineage SourceLineage) ([]ArtifactImpactOverlap, bool) {
	mapped := map[string][]string{}
	for _, row := range lineage.Rows {
		mapped[row.BaseID] = append([]string(nil), row.TargetIDs...)
	}
	bySource := map[string][]ImpactWorkLink{}
	appendLink := func(source string, work ImpactWorkLink) {
		if work.Phase != "delivered" {
			bySource[source] = append(bySource[source], work)
		}
	}
	for _, work := range base {
		for _, source := range work.SourceCriteria {
			for _, targetID := range mapped[source] {
				appendLink(targetID, work)
			}
		}
	}
	for _, work := range target {
		for _, source := range work.SourceCriteria {
			appendLink(source, work)
		}
	}
	sources := make([]string, 0, len(bySource))
	for source, work := range bySource {
		if len(work) > 1 {
			sources = append(sources, source)
		}
	}
	sort.Strings(sources)
	overlaps := make([]ArtifactImpactOverlap, 0, len(sources))
	for _, source := range sources {
		if len(overlaps) == 100 {
			return overlaps, true
		}
		seenWork, seenReport := map[string]bool{}, map[string]bool{}
		overlap := ArtifactImpactOverlap{SourceCriterion: source, WorkKeys: []string{}}
		for _, work := range bySource[source] {
			if !seenWork[work.Key] {
				seenWork[work.Key] = true
				overlap.WorkKeys = append(overlap.WorkKeys, work.Key)
			}
			if work.ReportID != "" && !seenReport[work.ReportID] {
				seenReport[work.ReportID] = true
				overlap.ReportIDs = append(overlap.ReportIDs, work.ReportID)
			}
		}
		sort.Strings(overlap.WorkKeys)
		sort.Strings(overlap.ReportIDs)
		if len(overlap.WorkKeys) > 1 {
			overlaps = append(overlaps, overlap)
		}
	}
	return overlaps, false
}

func artifactDocumentDelta(base, target []ArtifactDocument) ArtifactDocumentDelta {
	key := func(doc ArtifactDocument) string { return doc.Role + ":" + doc.Path }
	before, after := make(map[string]ArtifactDocument, len(base)), make(map[string]ArtifactDocument, len(target))
	for _, doc := range base {
		before[key(doc)] = doc
	}
	for _, doc := range target {
		after[key(doc)] = doc
	}
	delta := ArtifactDocumentDelta{}
	for id, old := range before {
		current, ok := after[id]
		switch {
		case !ok:
			delta.Removed = append(delta.Removed, id)
		case old.Digest != current.Digest:
			delta.Modified = append(delta.Modified, id)
		}
	}
	for id := range after {
		if _, ok := before[id]; !ok {
			delta.Added = append(delta.Added, id)
		}
	}
	sort.Strings(delta.Added)
	sort.Strings(delta.Removed)
	sort.Strings(delta.Modified)
	return delta
}
