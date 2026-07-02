package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/specgate/doc-registry/internal/artifact"
)

// ArtifactPublisher is List + Publish for MCP artifact tools.
type ArtifactPublisher interface {
	ArtifactLister
	Publish(ctx context.Context, in artifact.PublishInput) (*artifact.Artifact, error)
}

// ArtifactLister is the subset of artifact.Service needed by the MCP tool.
type ArtifactLister interface {
	List(ctx context.Context, f artifact.ListFilter) ([]artifact.Artifact, error)
}

// ArtifactReader is the subset of artifact.Service needed by read-only artifact MCP tools.
type ArtifactReader interface {
	Get(ctx context.Context, id string) (*artifact.Artifact, error)
	FileContent(ctx context.Context, id string, path string) ([]byte, error)
}

// ArtifactSearchInput matches the search_artifacts tool schema.
type ArtifactSearchInput struct {
	FeatureID string `json:"feature_id,omitempty"`
	Status    string `json:"status,omitempty"`
	Service   string `json:"service,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

type artifactResultItem struct {
	ID          string   `json:"id"`
	FeatureID   string   `json:"feature_id"`
	Status      string   `json:"status"`
	ImpactLevel string   `json:"impact_level"`
	Services    []string `json:"services"`
	CreatedAt   string   `json:"created_at"`
}

// ArtifactBundleReadInput matches the artifact_read_bundle tool schema.
type ArtifactBundleReadInput struct {
	ArtifactID string   `json:"artifact_id"`
	Files      []string `json:"files,omitempty"`
	MaxChars   int      `json:"max_chars,omitempty"`
}

type artifactBundleMeta struct {
	ID          string `json:"id"`
	FeatureID   string `json:"feature_id"`
	Version     string `json:"version"`
	Status      string `json:"status"`
	ImpactLevel string `json:"impact_level"`
}

// NewArtifactSearchHandler returns a handler function for the search_artifacts tool.
func NewArtifactSearchHandler(svc ArtifactLister) func(ctx context.Context, in ArtifactSearchInput) (string, error) {
	return func(ctx context.Context, in ArtifactSearchInput) (string, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 10
		}
		if limit > 50 {
			limit = 50
		}

		queryLimit := limit + 1

		results, err := svc.List(ctx, artifact.ListFilter{
			FeatureID: in.FeatureID,
			Service:   in.Service,
			Status:    artifact.Status(in.Status),
			Limit:     queryLimit,
		})
		if err != nil {
			return "", err
		}

		truncated := len(results) > limit
		if truncated {
			results = results[:limit]
		}

		items := make([]artifactResultItem, 0, len(results))
		for _, a := range results {
			services := make([]string, 0, len(a.Services))
			for _, svc := range a.Services {
				services = append(services, svc.Name)
			}
			created := a.CreatedAt
			if created.IsZero() {
				created = time.Unix(0, 0).UTC()
			}
			items = append(items, artifactResultItem{
				ID:          a.ID,
				FeatureID:   a.FeatureID,
				Status:      string(a.Status),
				ImpactLevel: string(a.ImpactLevel),
				Services:    services,
				CreatedAt:   created.UTC().Format(time.RFC3339),
			})
		}

		out, _ := json.Marshal(map[string]any{
			"items":     items,
			"truncated": truncated,
		})
		return string(out), nil
	}
}

// NewArtifactBundleReadHandler reads selected artifact package files.
// The optional files list filters by document path; if empty, all files in
// the artifact are returned (up to maxChars each).
func NewArtifactBundleReadHandler(svc ArtifactReader) func(ctx context.Context, in ArtifactBundleReadInput) (string, error) {
	return func(ctx context.Context, in ArtifactBundleReadInput) (string, error) {
		if svc == nil {
			return "", errors.New("artifact service not configured")
		}
		artifactID := strings.TrimSpace(in.ArtifactID)
		if artifactID == "" {
			return "", errors.New("artifact_id is required")
		}
		maxChars := in.MaxChars
		if maxChars <= 0 {
			maxChars = 12000
		}
		if maxChars > 40000 {
			maxChars = 40000
		}
		a, err := svc.Get(ctx, artifactID)
		if err != nil {
			return "", err
		}
		meta := artifactBundleMeta{
			ID:          a.ID,
			FeatureID:   a.FeatureID,
			Version:     a.Version,
			Status:      string(a.Status),
			ImpactLevel: string(a.ImpactLevel),
		}
		// When no files are requested, return a listing (paths + sizes) only.
		// Pass files: ["*"] to fetch content for all files.
		if len(in.Files) == 0 {
			listing := make(map[string]map[string]any, len(a.Files))
			for _, f := range a.Files {
				listing[f.Path] = map[string]any{"size_bytes": f.SizeBytes}
			}
			out, err := json.Marshal(map[string]any{
				"artifact":     meta,
				"listing_only": true,
				"files":        listing,
			})
			if err != nil {
				return "", err
			}
			return string(out), nil
		}
		// ["*"] expands to all files.
		var filesFilter []string
		if len(in.Files) == 1 && in.Files[0] == "*" {
			filesFilter = nil
		} else {
			filesFilter = in.Files
		}
		// Determine which paths to read: caller-supplied filter or all artifact paths.
		paths := resolveArtifactPaths(a, filesFilter)
		files := make(map[string]map[string]any, len(paths))
		for _, path := range paths {
			body, err := svc.FileContent(ctx, artifactID, path)
			if err != nil {
				files[path] = map[string]any{"error": err.Error()}
				continue
			}
			text := string(body)
			truncated := false
			if len(text) > maxChars {
				text = truncateArtifactText(text, maxChars)
				truncated = true
			}
			files[path] = map[string]any{
				"content":   text,
				"size":      len(body),
				"truncated": truncated,
			}
		}
		out, err := json.Marshal(map[string]any{
			"artifact": meta,
			"files":    files,
		})
		if err != nil {
			return "", err
		}
		return string(out), nil
	}
}

func truncateArtifactText(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	out := text[:maxChars]
	for !utf8.ValidString(out) && len(out) > 0 {
		out = out[:len(out)-1]
	}
	return out
}

// resolveArtifactPaths returns the list of document paths to read from an artifact.
// When filter is non-empty, it is used as-is (caller-supplied paths); otherwise all
// paths present in the artifact are returned.
func resolveArtifactPaths(a *artifact.Artifact, filter []string) []string {
	if len(filter) > 0 {
		seen := map[string]bool{}
		out := make([]string, 0, len(filter))
		for _, raw := range filter {
			p := strings.TrimSpace(raw)
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
		return out
	}
	out := make([]string, 0, len(a.Files))
	for _, f := range a.Files {
		out = append(out, f.Path)
	}
	return out
}

// ArtifactCreateInput matches POST /artifacts body (Governance publish bundle).
type ArtifactCreateInput struct {
	FeatureID            string            `json:"feature_id"`
	RequestType          string            `json:"request_type"`
	ImpactLevel          string            `json:"impact_level"`
	ArtifactPhase        string            `json:"artifact_phase,omitempty"`
	ArtifactCompleteness string            `json:"artifact_completeness,omitempty"`
	Version              string            `json:"version"`
	Status               string            `json:"status,omitempty"`
	ConfidenceScore      *float64          `json:"confidence_score,omitempty"`
	AmbiguityScore       *float64          `json:"ambiguity_score,omitempty"`
	GovernanceVersion    string            `json:"governance_version,omitempty"`
	ImpactedServices     []string          `json:"impacted_services"`
	ImpactedApps         []string          `json:"impacted_apps,omitempty"`
	Files                map[string]string `json:"files"`
}

type artifactCreateOutput struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Status  string `json:"status"`
}

// NewArtifactCreateHandler returns a handler for artifact_create (thin wrapper over POST /artifacts).
func NewArtifactCreateHandler(svc ArtifactPublisher) func(ctx context.Context, in ArtifactCreateInput) (string, error) {
	return func(ctx context.Context, in ArtifactCreateInput) (string, error) {
		if svc == nil {
			return "", errors.New("artifact service not configured")
		}
		if in.FeatureID == "" || in.Version == "" {
			return "", errors.New("feature_id and version are required")
		}
		if len(in.Files) == 0 {
			return "", errors.New("files map is required")
		}
		docs := make([]artifact.DocumentInput, 0, len(in.Files))
		for key, val := range in.Files {
			decoded, err := base64.StdEncoding.DecodeString(val)
			if err != nil {
				return "", err
			}
			// The MCP tool contract is the fixed-key files map; map each key to {path, role}.
			docs = append(docs, artifact.DocumentInput{
				Path:    artifact.FixedKeyToPath(key),
				Role:    string(artifact.FixedKeyToRole(key)),
				Content: decoded,
			})
		}
		refs := make([]artifact.ServiceRef, 0, len(in.ImpactedServices)+len(in.ImpactedApps))
		for _, svcName := range in.ImpactedServices {
			refs = append(refs, artifact.ServiceRef{Name: svcName, Kind: "service"})
		}
		for _, app := range in.ImpactedApps {
			refs = append(refs, artifact.ServiceRef{Name: app, Kind: "app"})
		}
		status := in.Status
		if status == "" {
			status = string(artifact.StatusDraft)
		}
		a, err := svc.Publish(ctx, artifact.PublishInput{
			FeatureID:            in.FeatureID,
			Version:              in.Version,
			Status:               artifact.Status(status),
			RequestType:          artifact.RequestType(in.RequestType),
			ImpactLevel:          artifact.ImpactLevel(in.ImpactLevel),
			ArtifactPhase:        artifact.ArtifactPhase(in.ArtifactPhase),
			ArtifactCompleteness: artifact.ArtifactCompleteness(in.ArtifactCompleteness),
			ConfidenceScore:      in.ConfidenceScore,
			AmbiguityScore:       in.AmbiguityScore,
			GovernanceVersion:    in.GovernanceVersion,
			CreatedBy:            "governance-ops",
			ImpactedServices:     refs,
			Documents:            docs,
		})
		if err != nil {
			return "", err
		}
		out, err := json.Marshal(artifactCreateOutput{
			ID:      a.ID,
			Version: a.Version,
			Status:  string(a.Status),
		})
		if err != nil {
			return "", err
		}
		return string(out), nil
	}
}
