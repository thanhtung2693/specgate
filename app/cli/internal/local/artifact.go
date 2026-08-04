package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
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

type ArtifactInput struct {
	FeatureKey  string
	RequestType string
	BaseVersion string
	Documents   []ArtifactDocumentInput
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
}

type ArtifactDocument struct {
	Path      string
	Role      string
	Content   []byte
	Digest    string
	SizeBytes int
}

func (s *Store) PublishArtifact(ctx context.Context, workspaceID string, input ArtifactInput) (Artifact, error) {
	input.FeatureKey = strings.TrimSpace(input.FeatureKey)
	input.RequestType = strings.TrimSpace(input.RequestType)
	if workspaceID == "" || input.FeatureKey == "" || input.RequestType == "" || len(input.Documents) == 0 {
		return Artifact{}, fmt.Errorf("workspace, feature key, request type, and at least one document are required")
	}
	if _, ok := artifactRequestTypes[input.RequestType]; !ok {
		return Artifact{}, fmt.Errorf("request type must be new_feature, change_request, bugfix, or unknown")
	}
	documents, digest, err := validateArtifactDocuments(input.Documents)
	if err != nil {
		return Artifact{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Artifact{}, err
	}
	defer tx.Rollback()
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
			return Artifact{}, fmt.Errorf("base_version must be empty when publishing the first version")
		}
		return Artifact{}, fmt.Errorf("base_version %q does not match latest version %q", input.BaseVersion, wantBase)
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
	if _, err := tx.ExecContext(ctx, `INSERT INTO artifacts(id, workspace_id, feature_key, request_type, version, status, snapshot_digest, policy_digest, policy_snapshot_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, artifact.ID, artifact.WorkspaceID, artifact.FeatureKey, artifact.RequestType, artifact.Version, artifact.Status, artifact.SnapshotDigest, artifact.PolicyDigest, artifact.PolicySnapshot, artifact.CreatedAt); err != nil {
		return Artifact{}, err
	}
	for _, document := range artifact.Documents {
		if _, err := tx.ExecContext(ctx, `INSERT INTO artifact_documents(artifact_id, path, role, content, digest) VALUES (?, ?, ?, ?, ?)`, artifact.ID, document.Path, document.Role, document.Content, document.Digest); err != nil {
			return Artifact{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Artifact{}, err
	}
	return artifact, nil
}

func (s *Store) ListArtifacts(ctx context.Context, workspaceID string) ([]Artifact, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, workspace_id, feature_key, request_type, version, status, snapshot_digest, policy_digest, policy_snapshot_json, created_at FROM artifacts WHERE workspace_id = ? ORDER BY created_at DESC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var artifacts []Artifact
	for rows.Next() {
		var artifact Artifact
		if err := rows.Scan(&artifact.ID, &artifact.WorkspaceID, &artifact.FeatureKey, &artifact.RequestType, &artifact.Version, &artifact.Status, &artifact.SnapshotDigest, &artifact.PolicyDigest, &artifact.PolicySnapshot, &artifact.CreatedAt); err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

func (s *Store) GetArtifact(ctx context.Context, workspaceID, id string) (Artifact, error) {
	var artifact Artifact
	err := s.db.QueryRowContext(ctx, `SELECT id, workspace_id, feature_key, request_type, version, status, snapshot_digest, policy_digest, policy_snapshot_json, created_at FROM artifacts WHERE workspace_id = ? AND id = ?`, workspaceID, id).Scan(&artifact.ID, &artifact.WorkspaceID, &artifact.FeatureKey, &artifact.RequestType, &artifact.Version, &artifact.Status, &artifact.SnapshotDigest, &artifact.PolicyDigest, &artifact.PolicySnapshot, &artifact.CreatedAt)
	if err != nil {
		return Artifact{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT path, role, content, digest FROM artifact_documents WHERE artifact_id = ? ORDER BY path`, id)
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
