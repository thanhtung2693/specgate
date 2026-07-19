package local

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Feature struct {
	ID                  string
	WorkspaceID         string
	Key                 string
	CanonicalArtifactID string
	Version             int
}

func (s *Store) ListFeatures(ctx context.Context, workspaceID string) ([]Feature, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, workspace_id, key, canonical_artifact_id, version FROM features WHERE workspace_id = ? ORDER BY key`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var features []Feature
	for rows.Next() {
		var feature Feature
		if err := rows.Scan(&feature.ID, &feature.WorkspaceID, &feature.Key, &feature.CanonicalArtifactID, &feature.Version); err != nil {
			return nil, err
		}
		features = append(features, feature)
	}
	return features, rows.Err()
}

type WorkInput struct {
	FeatureRef         string
	Title              string
	Description        string
	AcceptanceCriteria []string
}

type QuickWorkInput struct {
	Title              string
	Description        string
	AcceptanceCriteria []string
}

type WorkItem struct {
	ID                 string   `json:"id"`
	Key                string   `json:"key"`
	WorkspaceID        string   `json:"workspace_id"`
	FeatureID          string   `json:"feature_id"`
	FeatureKey         string   `json:"feature_key,omitempty"`
	ArtifactID         string   `json:"artifact_id"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	Phase              string   `json:"phase"`
	ContextDigest      string   `json:"context_digest"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	CreatedAt          string   `json:"created_at"`
}

type ContextPack struct {
	WorkID   string
	WorkKey  string
	Digest   string
	Markdown string
}

func (s *Store) PromoteArtifact(ctx context.Context, workspaceID, artifactID string) (Feature, error) {
	artifact, err := s.GetArtifact(ctx, workspaceID, artifactID)
	if err != nil {
		return Feature{}, err
	}
	if artifact.Status != "approved" {
		return Feature{}, fmt.Errorf("artifact %s must be approved before promotion", artifactID)
	}
	feature := Feature{WorkspaceID: workspaceID, Key: artifact.FeatureKey, CanonicalArtifactID: artifact.ID, Version: artifact.Version}
	err = s.db.QueryRowContext(ctx, `SELECT id FROM features WHERE workspace_id = ? AND key = ?`, workspaceID, feature.Key).Scan(&feature.ID)
	if err == sql.ErrNoRows {
		feature.ID, err = newID()
		if err != nil {
			return Feature{}, err
		}
		_, err = s.db.ExecContext(ctx, `INSERT INTO features(id, workspace_id, key, canonical_artifact_id, version, created_at) VALUES (?, ?, ?, ?, ?, ?)`, feature.ID, feature.WorkspaceID, feature.Key, feature.CanonicalArtifactID, feature.Version, time.Now().UTC().Format(time.RFC3339Nano))
	} else if err == nil {
		_, err = s.db.ExecContext(ctx, `UPDATE features SET canonical_artifact_id = ?, version = ? WHERE id = ?`, feature.CanonicalArtifactID, feature.Version, feature.ID)
	}
	if err != nil {
		return Feature{}, err
	}
	return feature, nil
}

func (s *Store) CreateWork(ctx context.Context, workspaceID string, input WorkInput) (WorkItem, error) {
	input.FeatureRef = strings.TrimSpace(input.FeatureRef)
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	if input.FeatureRef == "" || input.Title == "" {
		return WorkItem{}, fmt.Errorf("feature and title are required")
	}
	var feature Feature
	err := s.db.QueryRowContext(ctx, `SELECT id, workspace_id, key, canonical_artifact_id, version FROM features WHERE workspace_id = ? AND (id = ? OR key = ?)`, workspaceID, input.FeatureRef, input.FeatureRef).Scan(&feature.ID, &feature.WorkspaceID, &feature.Key, &feature.CanonicalArtifactID, &feature.Version)
	if err != nil {
		return WorkItem{}, err
	}
	artifact, err := s.GetArtifact(ctx, workspaceID, feature.CanonicalArtifactID)
	if err != nil {
		return WorkItem{}, err
	}
	if artifact.Status != "approved" {
		return WorkItem{}, fmt.Errorf("feature %s has no approved canonical artifact", feature.Key)
	}
	criteria := cleanCriteria(input.AcceptanceCriteria)
	if len(criteria) == 0 {
		return WorkItem{}, fmt.Errorf("at least one acceptance criterion is required")
	}
	id, err := newID()
	if err != nil {
		return WorkItem{}, err
	}
	key := "LOCAL-" + id[:8]
	markdown := contextMarkdown(key, input.Title, artifact, criteria)
	digest := digestText(markdown)
	criteriaJSON, err := json.Marshal(criteria)
	if err != nil {
		return WorkItem{}, err
	}
	work := WorkItem{ID: id, Key: key, WorkspaceID: workspaceID, FeatureID: feature.ID, FeatureKey: feature.Key, ArtifactID: artifact.ID, Title: input.Title, Description: input.Description, Phase: "ready", ContextDigest: digest, AcceptanceCriteria: criteria, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO work_items(id, workspace_id, key, feature_id, artifact_id, title, description, phase, context_digest, acceptance_criteria, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, work.ID, work.WorkspaceID, work.Key, work.FeatureID, work.ArtifactID, work.Title, work.Description, work.Phase, work.ContextDigest, criteriaJSON, work.CreatedAt); err != nil {
		return WorkItem{}, err
	}
	return work, nil
}

func (s *Store) CreateQuickWork(ctx context.Context, workspaceID string, input QuickWorkInput) (WorkItem, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	if input.Title == "" {
		return WorkItem{}, fmt.Errorf("title is required")
	}
	criteria := cleanCriteria(input.AcceptanceCriteria)
	if len(criteria) == 0 {
		return WorkItem{}, fmt.Errorf("at least one acceptance criterion is required")
	}
	id, err := newID()
	if err != nil {
		return WorkItem{}, err
	}
	key := "LOCAL-" + id[:8]
	markdown := quickContextMarkdown(key, input.Title, input.Description, criteria)
	digest := digestText(markdown)
	criteriaJSON, err := json.Marshal(criteria)
	if err != nil {
		return WorkItem{}, err
	}
	work := WorkItem{
		ID: id, Key: key, WorkspaceID: workspaceID, Title: input.Title,
		Description: input.Description, Phase: "ready", ContextDigest: digest,
		AcceptanceCriteria: criteria, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO work_items(id, workspace_id, key, feature_id, artifact_id, title, description, phase, context_digest, acceptance_criteria, created_at) VALUES (?, ?, ?, NULL, NULL, ?, ?, ?, ?, ?, ?)`, work.ID, work.WorkspaceID, work.Key, work.Title, work.Description, work.Phase, work.ContextDigest, criteriaJSON, work.CreatedAt); err != nil {
		return WorkItem{}, err
	}
	return work, nil
}

func (s *Store) GetWork(ctx context.Context, workspaceID, ref string) (WorkItem, error) {
	var work WorkItem
	var criteria string
	err := scanWork(s.db.QueryRowContext(ctx, `SELECT id, key, workspace_id, feature_id, artifact_id, title, description, phase, context_digest, acceptance_criteria, created_at FROM work_items WHERE workspace_id = ? AND (id = ? OR key = ?)`, workspaceID, ref, ref), &work, &criteria)
	if err != nil {
		return WorkItem{}, err
	}
	if err := json.Unmarshal([]byte(criteria), &work.AcceptanceCriteria); err != nil {
		return WorkItem{}, err
	}
	return work, nil
}

func (s *Store) ListWork(ctx context.Context, workspaceID string) ([]WorkItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, key, workspace_id, feature_id, artifact_id, title, description, phase, context_digest, acceptance_criteria, created_at FROM work_items WHERE workspace_id = ? ORDER BY created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []WorkItem
	for rows.Next() {
		var work WorkItem
		var criteria string
		if err := scanWork(rows, &work, &criteria); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(criteria), &work.AcceptanceCriteria); err != nil {
			return nil, err
		}
		items = append(items, work)
	}
	return items, rows.Err()
}

func (s *Store) ContextPack(ctx context.Context, workspaceID, ref string) (ContextPack, error) {
	work, err := s.GetWork(ctx, workspaceID, ref)
	if err != nil {
		return ContextPack{}, err
	}
	markdown := ""
	if work.ArtifactID == "" {
		markdown = quickContextMarkdown(work.Key, work.Title, work.Description, work.AcceptanceCriteria)
	} else {
		artifact, err := s.GetArtifact(ctx, workspaceID, work.ArtifactID)
		if err != nil {
			return ContextPack{}, err
		}
		markdown = contextMarkdown(work.Key, work.Title, artifact, work.AcceptanceCriteria)
	}
	digest := digestText(markdown)
	if digest != work.ContextDigest {
		return ContextPack{}, fmt.Errorf("stored Context Pack digest does not match its approved snapshot")
	}
	return ContextPack{WorkID: work.ID, WorkKey: work.Key, Digest: digest, Markdown: markdown}, nil
}

type workScanner interface {
	Scan(dest ...any) error
}

func scanWork(scanner workScanner, work *WorkItem, criteria *string) error {
	var featureID, artifactID sql.NullString
	if err := scanner.Scan(
		&work.ID, &work.Key, &work.WorkspaceID, &featureID, &artifactID,
		&work.Title, &work.Description, &work.Phase, &work.ContextDigest, criteria, &work.CreatedAt,
	); err != nil {
		return err
	}
	work.FeatureID = featureID.String
	work.ArtifactID = artifactID.String
	return nil
}

func cleanCriteria(input []string) []string {
	seen := make(map[string]struct{}, len(input))
	criteria := make([]string, 0, len(input))
	for _, criterion := range input {
		if value := strings.TrimSpace(criterion); value != "" {
			if _, ok := seen[value]; !ok {
				seen[value] = struct{}{}
				criteria = append(criteria, value)
			}
		}
	}
	return criteria
}

func contextMarkdown(key, title string, artifact Artifact, criteria []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Context Pack: %s\n\n%s\n\n", key, title)
	fmt.Fprintf(&b, "Approved artifact: %s (v%d, %s)\n\n", artifact.ID, artifact.Version, artifact.SnapshotDigest)
	for _, document := range artifact.Documents {
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", document.Path, document.Content)
	}
	b.WriteString("## Acceptance criteria\n\n")
	for _, criterion := range criteria {
		fmt.Fprintf(&b, "- %s\n", criterion)
	}
	return b.String()
}

func quickContextMarkdown(key, title, description string, criteria []string) string {
	const policy = "Local quick-route policy: evidence is reviewable but human acceptance remains authoritative."
	var b strings.Builder
	fmt.Fprintf(&b, "# Implementation Context Pack: %s\n\n## Quick Handoff\n\n", key)
	fmt.Fprintf(&b, "### Intent\n\n%s\n", title)
	if description != "" && description != title {
		fmt.Fprintf(&b, "\n%s\n", description)
	}
	b.WriteString("\n### Acceptance criteria\n\n")
	for _, criterion := range criteria {
		fmt.Fprintf(&b, "- %s\n", criterion)
	}
	fmt.Fprintf(&b, "\n### Impact declaration\n\nQuick route selected; no protected-domain impact was declared.\n\n### Policy snapshot\n\n%s\n\nPolicy digest: %s\n", policy, digestText(policy))
	return b.String()
}

func digestText(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}
