package knowledge

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/specgate/doc-registry/internal/workspace"
)

func validateMetadata(m Metadata) error {
	if _, valid := workspace.NormalizeID(m.WorkspaceID); !valid {
		return validation("workspace_id is required and must be a safe path segment")
	}
	if documentID := strings.TrimSpace(m.DocumentID); documentID != "" {
		if _, valid := workspace.NormalizeID(documentID); !valid {
			return validation("document_id must be a safe opaque path segment")
		}
	}
	if strings.TrimSpace(m.Title) == "" {
		return validation("title is required")
	}
	if !validDocumentType(m.DocumentType) {
		return validation("document_type is required or invalid")
	}
	if !validAuthority(m.AuthorityLevel) {
		return validation("authority_level is required or invalid")
	}
	role := strings.TrimSpace(m.ActorRole)
	if m.AuthorityLevel == AuthoritySourceOfTruth && role != "" && role != "reviewer" && role != "admin" {
		return validation("source_of_truth requires reviewer or admin actor_role")
	}
	if m.NewVersion != "" && !validVersion(m.NewVersion) {
		return validation("new_version must look like v1 or v1.1")
	}
	return nil
}

func validDocumentType(v DocumentType) bool {
	switch v {
	case DocumentTypeProductBrief, DocumentTypeSRS, DocumentTypeDesignReference, DocumentTypeSupportingDoc, DocumentTypeExistingArtifact, DocumentTypeQAFinding, DocumentTypePolicyDoc:
		return true
	default:
		return false
	}
}

func validAuthority(v AuthorityLevel) bool {
	switch v {
	case AuthoritySourceOfTruth, AuthorityHigh, AuthorityReference, AuthorityLow:
		return true
	default:
		return false
	}
}

var versionRE = regexp.MustCompile(`^v\d+(\.\d+)?$`)

func validVersion(v string) bool { return versionRE.MatchString(v) }

func validation(msg string) error { return fmt.Errorf("%w: %s", ErrValidation, msg) }

func allowedFilename(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".txt":
		return true
	default:
		return false
	}
}

func safeFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == "/" || name == "" {
		return "original.file"
	}
	return name
}

func (s *Service) rawObjectKey(workspaceID, documentID, version, filename string) string {
	prefix := workspaceObjectPrefix(workspaceID)
	return fmt.Sprintf("%sdocuments/%s/%s/raw/%s/%s", prefix+s.keyPrefix, documentID, version, uuid.NewString(), filename)
}

func (s *Service) processedObjectKey(workspaceID, documentID, version, filename string) string {
	return fmt.Sprintf("%sdocuments/%s/%s/processed/%s", workspaceObjectPrefix(workspaceID)+s.keyPrefix, documentID, version, filename)
}

func workspaceObjectPrefix(workspaceID string) string {
	return "workspaces/" + strings.TrimSpace(workspaceID) + "/"
}

func extractText(doc *Document, raw []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(doc.OriginalFilename))
	if doc.SourceKind == SourceKindText || ext == ".md" || ext == ".txt" {
		return strings.TrimSpace(string(raw)), nil
	}
	return "", fmt.Errorf("unsupported file type for parsing; raw file was stored")
}

func linksFor(doc *Document) []Link {
	var out []Link
	if doc.LinkedFeatureID != "" {
		out = append(out, Link{ID: uuid.NewString(), DocumentID: doc.DocumentID, Version: doc.Version, EntityType: "feature", EntityID: doc.LinkedFeatureID, RelationType: "primary_context"})
	}
	if doc.LinkedRequestID != "" {
		out = append(out, Link{ID: uuid.NewString(), DocumentID: doc.DocumentID, Version: doc.Version, EntityType: "request", EntityID: doc.LinkedRequestID, RelationType: "primary_context"})
	}
	return out
}

func summaryFor(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	return truncate(text, 240)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func cleanTags(tags []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

func tagsFromJSON(src string) []string {
	var out []string
	_ = json.Unmarshal([]byte(src), &out)
	return out
}

func strPayload(p map[string]any, key string) string {
	if v, ok := p[key].(string); ok {
		return v
	}
	return ""
}

func intPayload(p map[string]any, key string) int {
	switch v := p[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

// strSlicePayload reads a []string payload value. The in-memory store keeps it
// as []string; JSONB round-trips (real pgvector) yield []any of strings.
func strSlicePayload(p map[string]any, key string) []string {
	switch v := p[key].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
