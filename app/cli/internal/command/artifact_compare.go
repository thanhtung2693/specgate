package command

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/specgate/specgate/app/cli/internal/client"
)

type artifactComparisonCounts struct {
	Added     int `json:"added"`
	Removed   int `json:"removed"`
	Changed   int `json:"changed"`
	Unchanged int `json:"unchanged"`
}

type artifactComparisonFile struct {
	Path          string   `json:"path"`
	Role          string   `json:"role"`
	ContentSHA256 string   `json:"content_sha256,omitempty"`
	State         string   `json:"state"`
	Changes       []string `json:"changes,omitempty"`
}

type artifactComparison struct {
	BaseArtifactID     string                   `json:"base_artifact_id"`
	BaseVersion        string                   `json:"base_version"`
	BaseSnapshotDigest string                   `json:"base_snapshot_digest,omitempty"`
	Counts             artifactComparisonCounts `json:"counts"`
	Files              []artifactComparisonFile `json:"files"`
	Removed            []artifactComparisonFile `json:"removed"`
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

func digestArtifactContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
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

func buildArtifactComparison(body map[string]any, base *client.Artifact, baseFiles []client.ArtifactFile) (artifactComparison, error) {
	if base == nil {
		return artifactComparison{}, fmt.Errorf("base artifact is required")
	}
	comparison := artifactComparison{
		BaseArtifactID:     base.ID,
		BaseVersion:        base.Version,
		BaseSnapshotDigest: base.SnapshotDigest,
		Files:              []artifactComparisonFile{},
		Removed:            []artifactComparisonFile{},
	}

	baseByKey := make(map[string]client.ArtifactFile, len(baseFiles))
	for _, file := range baseFiles {
		normalized, ok := normalizeArtifactDocumentPath(file.Path)
		if !ok {
			return artifactComparison{}, fmt.Errorf("unsafe base document path %q", file.Path)
		}
		file.Role = normalizeArtifactDocumentRole(file.Role)
		key := normalized + "\x00" + file.Role
		if _, exists := baseByKey[key]; exists {
			return artifactComparison{}, fmt.Errorf("base document %q is mapped twice under role %q", normalized, file.Role)
		}
		if file.ContentSHA256 == "" {
			return artifactComparison{}, fmt.Errorf("base document %q is missing content_sha256", normalized)
		}
		file.Path = normalized
		baseByKey[key] = file
	}

	seen := map[string]struct{}{}
	rawDocuments, _ := body["documents"].([]any)
	for index, raw := range rawDocuments {
		document, ok := raw.(map[string]any)
		if !ok {
			return artifactComparison{}, fmt.Errorf("documents[%d] must be an object", index)
		}
		rawPath, _ := document["path"].(string)
		normalized, ok := normalizeArtifactDocumentPath(rawPath)
		if !ok {
			return artifactComparison{}, fmt.Errorf("unsafe document path %q", rawPath)
		}
		role, _ := document["role"].(string)
		role = normalizeArtifactDocumentRole(role)
		key := normalized + "\x00" + role
		if _, exists := seen[key]; exists {
			return artifactComparison{}, fmt.Errorf("document %q is mapped twice under role %q", normalized, role)
		}
		seen[key] = struct{}{}

		content, _ := document["content"].(string)
		current := artifactComparisonFile{
			Path:          normalized,
			Role:          role,
			ContentSHA256: digestArtifactContent(content),
		}
		previous, exists := baseByKey[key]
		switch {
		case !exists:
			current.State = "added"
			comparison.Counts.Added++
		default:
			if previous.ContentSHA256 != current.ContentSHA256 {
				current.Changes = append(current.Changes, "content")
			}
			if len(current.Changes) == 0 {
				current.State = "unchanged"
				comparison.Counts.Unchanged++
			} else {
				current.State = "changed"
				comparison.Counts.Changed++
			}
		}
		comparison.Files = append(comparison.Files, current)
	}

	for key, file := range baseByKey {
		if _, exists := seen[key]; exists {
			continue
		}
		comparison.Removed = append(comparison.Removed, artifactComparisonFile{
			Path:          file.Path,
			Role:          file.Role,
			ContentSHA256: file.ContentSHA256,
			State:         "removed",
		})
		comparison.Counts.Removed++
	}

	sort.Slice(comparison.Files, func(i, j int) bool {
		if comparison.Files[i].Path != comparison.Files[j].Path {
			return comparison.Files[i].Path < comparison.Files[j].Path
		}
		return comparison.Files[i].Role < comparison.Files[j].Role
	})
	sort.Slice(comparison.Removed, func(i, j int) bool {
		if comparison.Removed[i].Path != comparison.Removed[j].Path {
			return comparison.Removed[i].Path < comparison.Removed[j].Path
		}
		return comparison.Removed[i].Role < comparison.Removed[j].Role
	})
	return comparison, nil
}

func writeArtifactComparison(w io.Writer, comparison artifactComparison) {
	fmt.Fprintf(w, "Comparison with %s (%s):\n", comparison.BaseArtifactID, comparison.BaseVersion)
	for _, file := range comparison.Files {
		detail := ""
		if len(file.Changes) > 0 {
			detail = " (" + strings.Join(file.Changes, ", ") + ")"
		}
		fmt.Fprintf(w, "%s\t%s\t%s%s\n", file.State, file.Path, file.Role, detail)
	}
	for _, file := range comparison.Removed {
		fmt.Fprintf(w, "removed\t%s\t%s\n", file.Path, file.Role)
	}
}
