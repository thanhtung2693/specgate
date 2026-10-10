package local

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	maxDocumentBytes = 1 << 20
	maxPackageBytes  = 10 << 20
)

var artifactRequestTypes = map[string]struct{}{
	"new_feature":    {},
	"change_request": {},
	"bugfix":         {},
	"unknown":        {},
}

var sourceCriterionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

var ErrArtifactVersionConflict = errors.New("artifact version conflict")

type ArtifactInput struct {
	FeatureKey     string
	RequestType    string
	BaseVersion    string
	Documents      []ArtifactDocumentInput
	SourceCriteria []SourceCriterion
	SourceLineage  *SourceLineage
}

// SourceLineage is an optional, reviewed declaration relating only an exact
// base snapshot to the new snapshot. It is intentionally not a graph: callers
// must never traverse it to infer a relationship for another pair.
type SourceLineage struct {
	Version        int          `json:"version"`
	BaseArtifactID string       `json:"base_artifact_id"`
	BaseDigest     string       `json:"base_digest"`
	Rows           []LineageRow `json:"rows"`
	Added          []string     `json:"added,omitempty"`
}

type LineageRow struct {
	BaseID    string   `json:"base_id"`
	TargetIDs []string `json:"target_ids"`
	Reason    string   `json:"reason,omitempty"`
}

type SourceCriterion struct {
	ID             string `json:"id"`
	Text           string `json:"text"`
	SourcePath     string `json:"source_path"`
	DeferredReason string `json:"deferred_reason,omitempty"`
}

type ArtifactDocumentInput struct {
	Path    string
	Role    string
	Content []byte
}

type Artifact struct {
	ID             string
	WorkspaceID    string
	FeatureKey     string
	RequestType    string
	Version        int
	Status         string
	SnapshotDigest string
	PolicyDigest   string
	PolicySnapshot string
	CreatedAt      string
	Documents      []ArtifactDocument
	SourceCriteria []SourceCriterion
	SourceLineage  *SourceLineage
}

type ArtifactDocument struct {
	Path      string
	Role      string
	Content   []byte
	Digest    string
	SizeBytes int
}

func (s *Store) PublishArtifact(ctx context.Context, workspaceID string, input ArtifactInput) (Artifact, error) {
	if input.SourceLineage != nil {
		var result Artifact
		err := s.WithEnhancedWrite(ctx, func(tx *sql.Tx) error {
			var err error
			result, err = publishArtifactTx(ctx, tx, workspaceID, input)
			return err
		})
		return result, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Artifact{}, err
	}
	defer tx.Rollback()
	result, err := publishArtifactTx(ctx, tx, workspaceID, input)
	if err != nil {
		return Artifact{}, err
	}
	return result, tx.Commit()
}

func publishArtifactTx(ctx context.Context, tx *sql.Tx, workspaceID string, input ArtifactInput) (Artifact, error) {
	input, documents, digest, criteria, err := normalizedArtifactInput(input)
	if err != nil {
		return Artifact{}, err
	}
	if workspaceID == "" {
		return Artifact{}, fmt.Errorf("workspace, feature key, request type, and at least one document are required")
	}
	criteriaJSON, err := json.Marshal(criteria)
	if err != nil {
		return Artifact{}, err
	}
	var latestVersion int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM artifacts WHERE workspace_id = ? AND feature_key = ?`, workspaceID, input.FeatureKey).Scan(&latestVersion); err != nil {
		return Artifact{}, err
	}
	wantBase := ""
	if latestVersion > 0 {
		wantBase = fmt.Sprintf("v%d", latestVersion)
	}
	if strings.TrimSpace(input.BaseVersion) != wantBase {
		if wantBase == "" {
			return Artifact{}, fmt.Errorf("%w: base_version must be empty when publishing the first version", ErrArtifactVersionConflict)
		}
		return Artifact{}, fmt.Errorf("%w: base_version %q does not match latest version %q", ErrArtifactVersionConflict, input.BaseVersion, wantBase)
	}
	if input.SourceLineage != nil {
		if latestVersion == 0 {
			return Artifact{}, fmt.Errorf("source_lineage requires an exact base artifact")
		}
		var baseID, baseDigest, baseCriteriaJSON, baseLineageJSON string
		if err := tx.QueryRowContext(ctx, `SELECT id, snapshot_digest, source_criteria_json, source_lineage_json FROM artifacts WHERE workspace_id = ? AND feature_key = ? AND version = ?`, workspaceID, input.FeatureKey, latestVersion).Scan(&baseID, &baseDigest, &baseCriteriaJSON, &baseLineageJSON); err != nil {
			return Artifact{}, err
		}
		var base Artifact
		base.ID, base.SnapshotDigest = baseID, baseDigest
		if err := json.Unmarshal([]byte(baseCriteriaJSON), &base.SourceCriteria); err != nil {
			return Artifact{}, fmt.Errorf("decode base source criteria: %w", err)
		}
		if baseLineageJSON != "" {
			if err := json.Unmarshal([]byte(baseLineageJSON), &base.SourceLineage); err != nil {
				return Artifact{}, fmt.Errorf("decode base source lineage: %w", err)
			}
			if err := validateLineageVersion(base.SourceLineage); err != nil {
				return Artifact{}, err
			}
		}
		if !sourceInventoryKnown(base) {
			return Artifact{}, fmt.Errorf("source_lineage cannot declare impact because the base requirement inventory is unknown")
		}
		lineage, err := validateSourceLineage(*input.SourceLineage, baseID, baseDigest, base.SourceCriteria, criteria)
		if err != nil {
			return Artifact{}, err
		}
		input.SourceLineage = &lineage
	}
	digest, lineageJSON, err := artifactSnapshotDigest(digest, criteria, input.SourceLineage)
	if err != nil {
		return Artifact{}, err
	}
	version := latestVersion + 1
	policySnapshot, policyDigest, err := localPolicySnapshot()
	if err != nil {
		return Artifact{}, err
	}
	id, err := newID()
	if err != nil {
		return Artifact{}, err
	}
	artifact := Artifact{
		ID:             id,
		WorkspaceID:    workspaceID,
		FeatureKey:     input.FeatureKey,
		RequestType:    input.RequestType,
		Version:        version,
		Status:         "draft",
		SnapshotDigest: digest,
		PolicyDigest:   policyDigest,
		PolicySnapshot: policySnapshot,
		CreatedAt:      time.Now().UTC().Format(time.RFC3339Nano),
		Documents:      documents,
	}
	artifact.SourceCriteria = criteria
	artifact.SourceLineage = input.SourceLineage
	if _, err := tx.ExecContext(ctx, `INSERT INTO artifacts(id, workspace_id, feature_key, request_type, version, status, snapshot_digest, policy_digest, policy_snapshot_json, source_criteria_json, source_lineage_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, artifact.ID, artifact.WorkspaceID, artifact.FeatureKey, artifact.RequestType, artifact.Version, artifact.Status, artifact.SnapshotDigest, artifact.PolicyDigest, artifact.PolicySnapshot, string(criteriaJSON), lineageJSON, artifact.CreatedAt); err != nil {
		return Artifact{}, err
	}
	for _, document := range artifact.Documents {
		if _, err := tx.ExecContext(ctx, `INSERT INTO artifact_documents(artifact_id, path, role, content, digest) VALUES (?, ?, ?, ?, ?)`, artifact.ID, document.Path, document.Role, document.Content, document.Digest); err != nil {
			return Artifact{}, err
		}
	}
	return artifact, nil
}

func artifactSnapshotDigest(digest string, criteria []SourceCriterion, lineage *SourceLineage) (string, string, error) {
	if len(criteria) > 0 {
		hash := sha256.New()
		_, _ = hash.Write([]byte(digest + "\n"))
		for _, criterion := range criteria {
			_, _ = hash.Write([]byte(criterion.ID + "\x00" + criterion.Text + "\x00" + criterion.SourcePath + "\x00" + criterion.DeferredReason + "\n"))
		}
		digest = "sha256:" + hex.EncodeToString(hash.Sum(nil))
	}
	if lineage == nil {
		return digest, "", nil
	}
	encoded, err := json.Marshal(lineage)
	if err != nil {
		return "", "", err
	}
	lineageJSON := string(encoded)
	hash := sha256.New()
	_, _ = hash.Write([]byte(digest + "\n" + lineageJSON))
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), lineageJSON, nil
}

func validateSourceLineage(input SourceLineage, baseID, baseDigest string, base, target []SourceCriterion) (SourceLineage, error) {
	if input.Version != 1 || input.BaseArtifactID != baseID || input.BaseDigest != baseDigest {
		return SourceLineage{}, fmt.Errorf("source_lineage must name this exact base artifact and digest")
	}
	baseIDs, targetIDs := map[string]bool{}, map[string]bool{}
	for _, criterion := range base {
		baseIDs[criterion.ID] = true
	}
	for _, criterion := range target {
		targetIDs[criterion.ID] = true
	}
	seenBase, incoming, added := map[string]bool{}, map[string]bool{}, map[string]bool{}
	incomingCount := map[string]int{}
	for i := range input.Rows {
		row := &input.Rows[i]
		row.BaseID = strings.TrimSpace(row.BaseID)
		row.Reason = strings.TrimSpace(row.Reason)
		if !baseIDs[row.BaseID] || seenBase[row.BaseID] {
			return SourceLineage{}, fmt.Errorf("source_lineage rows must name each base requirement once")
		}
		seenBase[row.BaseID] = true
		if len(row.TargetIDs) == 0 && row.Reason == "" {
			return SourceLineage{}, fmt.Errorf("source_lineage removal requires a reason")
		}
		if len(row.TargetIDs) > 1 && row.Reason == "" {
			return SourceLineage{}, fmt.Errorf("source_lineage split requires a reason")
		}
		seenTarget := map[string]bool{}
		for index, id := range row.TargetIDs {
			id = strings.TrimSpace(id)
			if !targetIDs[id] || seenTarget[id] {
				return SourceLineage{}, fmt.Errorf("source_lineage target ids must be known and unique per row")
			}
			row.TargetIDs[index] = id
			seenTarget[id], incoming[id] = true, true
			incomingCount[id]++
		}
	}
	for _, row := range input.Rows {
		for _, id := range row.TargetIDs {
			if incomingCount[id] > 1 && row.Reason == "" {
				return SourceLineage{}, fmt.Errorf("source_lineage merge requires a reason on each incoming row")
			}
		}
	}
	for id := range baseIDs {
		if !seenBase[id] {
			return SourceLineage{}, fmt.Errorf("source_lineage must cover every base requirement")
		}
	}
	for i, id := range input.Added {
		id = strings.TrimSpace(id)
		if !targetIDs[id] || incoming[id] || added[id] {
			return SourceLineage{}, fmt.Errorf("source_lineage additions must be target-only and unique")
		}
		input.Added[i], added[id] = id, true
	}
	for id := range targetIDs {
		if !incoming[id] && !added[id] {
			return SourceLineage{}, fmt.Errorf("source_lineage must declare every target requirement as mapped or added")
		}
	}
	return input, nil
}

// ValidateArtifactInput applies the same immutable-package checks as PublishArtifact
// without opening a transaction. It supports no-write CLI previews.
func ValidateArtifactInput(input ArtifactInput) error {
	_, _, _, _, err := normalizedArtifactInput(input)
	return err
}

func normalizedArtifactInput(input ArtifactInput) (ArtifactInput, []ArtifactDocument, string, []SourceCriterion, error) {
	if input.SourceLineage != nil && input.SourceCriteria == nil {
		return input, nil, "", nil, fmt.Errorf("source_lineage requires an explicit source_criteria array (use [] for intentional removal of all requirements)")
	}
	input.FeatureKey = strings.TrimSpace(input.FeatureKey)
	input.RequestType = strings.TrimSpace(input.RequestType)
	if input.FeatureKey == "" || input.RequestType == "" || len(input.Documents) == 0 {
		return input, nil, "", nil, fmt.Errorf("feature key, request type, and at least one document are required")
	}
	if _, ok := artifactRequestTypes[input.RequestType]; !ok {
		return input, nil, "", nil, fmt.Errorf("request type must be new_feature, change_request, bugfix, or unknown")
	}
	documents, digest, err := validateArtifactDocuments(input.Documents)
	if err != nil {
		return input, nil, "", nil, err
	}
	criteria, err := validateSourceCriteria(input.SourceCriteria, documents)
	if err != nil {
		return input, nil, "", nil, err
	}
	return input, documents, digest, criteria, nil
}

func (s *Store) ListArtifacts(ctx context.Context, workspaceID string) ([]Artifact, error) {
	return listArtifacts(ctx, s.db, workspaceID)
}

func listArtifacts(ctx context.Context, q artifactQueryer, workspaceID string) ([]Artifact, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, workspace_id, feature_key, request_type, version, status, snapshot_digest, policy_digest, policy_snapshot_json, source_criteria_json, source_lineage_json, created_at FROM artifacts WHERE workspace_id = ? ORDER BY created_at DESC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var artifacts []Artifact
	for rows.Next() {
		var artifact Artifact
		var criteria, lineage string
		if err := rows.Scan(&artifact.ID, &artifact.WorkspaceID, &artifact.FeatureKey, &artifact.RequestType, &artifact.Version, &artifact.Status, &artifact.SnapshotDigest, &artifact.PolicyDigest, &artifact.PolicySnapshot, &criteria, &lineage, &artifact.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(criteria), &artifact.SourceCriteria); err != nil {
			return nil, fmt.Errorf("decode source criteria for artifact %q: %w", artifact.ID, err)
		}
		if lineage != "" {
			if err := json.Unmarshal([]byte(lineage), &artifact.SourceLineage); err != nil {
				return nil, fmt.Errorf("decode source lineage for artifact %q: %w", artifact.ID, err)
			}
			if err := validateLineageVersion(artifact.SourceLineage); err != nil {
				return nil, err
			}
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

func (s *Store) GetArtifact(ctx context.Context, workspaceID, id string) (Artifact, error) {
	return getArtifact(ctx, s.db, workspaceID, id)
}

type artifactQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getArtifact(ctx context.Context, q artifactQueryer, workspaceID, id string) (Artifact, error) {
	var artifact Artifact
	var criteria, lineage string
	err := q.QueryRowContext(ctx, `SELECT id, workspace_id, feature_key, request_type, version, status, snapshot_digest, policy_digest, policy_snapshot_json, source_criteria_json, source_lineage_json, created_at FROM artifacts WHERE workspace_id = ? AND id = ?`, workspaceID, id).Scan(&artifact.ID, &artifact.WorkspaceID, &artifact.FeatureKey, &artifact.RequestType, &artifact.Version, &artifact.Status, &artifact.SnapshotDigest, &artifact.PolicyDigest, &artifact.PolicySnapshot, &criteria, &lineage, &artifact.CreatedAt)
	if err != nil {
		return Artifact{}, err
	}
	if err := json.Unmarshal([]byte(criteria), &artifact.SourceCriteria); err != nil {
		return Artifact{}, err
	}
	if lineage != "" {
		if err := json.Unmarshal([]byte(lineage), &artifact.SourceLineage); err != nil {
			return Artifact{}, err
		}
		if err := validateLineageVersion(artifact.SourceLineage); err != nil {
			return Artifact{}, err
		}
	}
	rows, err := q.QueryContext(ctx, `SELECT path, role, content, digest FROM artifact_documents WHERE artifact_id = ? ORDER BY path`, id)
	if err != nil {
		return Artifact{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var document ArtifactDocument
		if err := rows.Scan(&document.Path, &document.Role, &document.Content, &document.Digest); err != nil {
			return Artifact{}, err
		}
		document.SizeBytes = len(document.Content)
		artifact.Documents = append(artifact.Documents, document)
	}
	return artifact, rows.Err()
}

func validateLineageVersion(lineage *SourceLineage) error {
	if lineage == nil || lineage.Version != 1 {
		return fmt.Errorf("%w: unsupported source lineage version", ErrStoreIncompatible)
	}
	return nil
}

func sourceInventoryKnown(artifact Artifact) bool {
	return len(artifact.SourceCriteria) > 0 || artifact.SourceLineage != nil
}

func validateSourceCriteria(input []SourceCriterion, documents []ArtifactDocument) ([]SourceCriterion, error) {
	documentPaths := make(map[string]struct{}, len(documents))
	for _, document := range documents {
		documentPaths[document.Path] = struct{}{}
	}
	seen := map[string]bool{}
	out := make([]SourceCriterion, 0, len(input))
	for _, criterion := range input {
		criterion.ID = strings.TrimSpace(criterion.ID)
		criterion.Text = strings.TrimSpace(criterion.Text)
		criterion.SourcePath = strings.TrimSpace(criterion.SourcePath)
		criterion.DeferredReason = strings.TrimSpace(criterion.DeferredReason)
		if criterion.ID == "" || criterion.Text == "" || criterion.SourcePath == "" {
			return nil, fmt.Errorf("each source criterion needs id, text, and source_path")
		}
		if !sourceCriterionIDPattern.MatchString(criterion.ID) {
			return nil, fmt.Errorf("source criterion id %q must start with an ASCII letter or digit and contain only ASCII letters, digits, hyphens, or underscores", criterion.ID)
		}
		if _, found := documentPaths[criterion.SourcePath]; !found {
			return nil, fmt.Errorf("source criterion %q source_path %q is not an artifact document", criterion.ID, criterion.SourcePath)
		}
		if seen[criterion.ID] {
			return nil, fmt.Errorf("duplicate source criterion id %q", criterion.ID)
		}
		seen[criterion.ID] = true
		out = append(out, criterion)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func validateArtifactDocuments(input []ArtifactDocumentInput) ([]ArtifactDocument, string, error) {
	seen := map[string]bool{}
	documents := make([]ArtifactDocument, 0, len(input))
	total := 0
	for _, inputDocument := range input {
		documentPath, ok := normalizeArtifactDocumentPath(inputDocument.Path)
		if !ok {
			return nil, "", fmt.Errorf("each document needs a safe repository-relative path")
		}
		role := normalizeArtifactDocumentRole(inputDocument.Role)
		// A role is a routing label, so one source may carry several roles: a
		// single spec often is both the spec and the plan. The identity is the
		// path and role together, which is what the snapshot digest below hashes
		// and what Full mode stores. Keying uniqueness on path alone rejected
		// manifests the preview had already accepted and left a single-document
		// author with no way to satisfy a multi-role policy.
		if seen[documentPath+"\x00"+role] {
			return nil, "", fmt.Errorf("document %q is mapped twice under the same role %q", documentPath, role)
		}
		if len(inputDocument.Content) > maxDocumentBytes {
			return nil, "", fmt.Errorf("document %q exceeds the 1 MiB Local limit", documentPath)
		}
		total += len(inputDocument.Content)
		if total > maxPackageBytes {
			return nil, "", fmt.Errorf("artifact package exceeds the 10 MiB Local limit")
		}
		content := append([]byte(nil), inputDocument.Content...)
		hash := sha256.Sum256(content)
		seen[documentPath+"\x00"+role] = true
		documents = append(documents, ArtifactDocument{Path: documentPath, Role: role, Content: content, Digest: "sha256:" + hex.EncodeToString(hash[:]), SizeBytes: len(content)})
	}
	sort.Slice(documents, func(i, j int) bool {
		if documents[i].Path != documents[j].Path {
			return documents[i].Path < documents[j].Path
		}
		return documents[i].Role < documents[j].Role
	})
	hash := sha256.New()
	for _, document := range documents {
		_, _ = hash.Write([]byte(document.Path + "\x00" + document.Role + "\x00" + document.Digest + "\n"))
	}
	return documents, "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func normalizeArtifactDocumentPath(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "/") || strings.ContainsRune(value, 0) || strings.ContainsRune(value, '\\') {
		return "", false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return "", false
		}
	}
	clean := path.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

func normalizeArtifactDocumentRole(value string) string {
	role := strings.ToLower(strings.TrimSpace(value))
	switch role {
	case "spec", "design", "plan", "verification", "research", "reference", "unspecified":
		return role
	default:
		if strings.HasPrefix(role, "custom:") && len(role) > len("custom:") {
			return role
		}
		return "unspecified"
	}
}
