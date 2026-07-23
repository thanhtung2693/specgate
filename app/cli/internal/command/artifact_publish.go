package command

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func newArtifactPublishCmd(deps *Deps) *cobra.Command {
	var filePath string
	var previewOnly bool
	var compareArtifactID string
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish an artifact version from a JSON file",
		Long: `Publish one immutable, path-preserving artifact version from a JSON file.

Use --preview for a zero-write local preview. Add --compare only with --preview
to compare explicit paths, roles, and hashes against one stored artifact.`,
		Example: `  specgate artifact publish --file artifact.json --preview --json
  specgate artifact publish --file artifact.json --preview --compare <artifact-id> --json
  specgate artifact publish --file artifact.json --json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if compareArtifactID != "" && !previewOnly {
				err := errors.New("--compare requires --preview")
				payload := output.ErrorPayload{Code: "usage", Message: err.Error()}
				code := deps.Printer.Error("artifact.publish", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			if filePath == "" {
				payload := output.ErrorPayload{Code: "usage", Message: "--file is required"}
				code := deps.Printer.Error("artifact.publish", payload)
				return &output.ExitError{Code: code, Err: ErrInputRequired}
			}
			body, err := readJSONBodyFile(deps, "artifact.publish", filePath)
			if err != nil {
				return err
			}
			if err := normalizeArtifactPublishBody(body); err != nil {
				payload := output.ErrorPayload{Code: "validation", Message: err.Error()}
				code := deps.Printer.Error("artifact.publish", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			if err := validateArtifactPublishFields(body); err != nil {
				payload := output.ErrorPayload{Code: "validation", Message: err.Error()}
				code := deps.Printer.Error("artifact.publish", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			documentSources, err := expandArtifactDocumentSources(body, filePath)
			if err != nil {
				payload := output.ErrorPayload{Code: "usage", Message: err.Error()}
				code := deps.Printer.Error("artifact.publish", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			if previewOnly {
				preview := artifactPublishPreview(body, documentSources)
				var comparison *artifactComparison
				if compareArtifactID != "" {
					previewCtx, err := artifactPublishPreviewContext(cmd.Context(), deps, body)
					if err != nil {
						return apiExitError(deps, "artifact.publish.preview", err)
					}
					base, err := deps.Client.GetArtifact(previewCtx, compareArtifactID)
					if err != nil {
						return apiExitError(deps, "artifact.publish.preview", err)
					}
					if requestedBase, _ := body["base_version"].(string); requestedBase != "" && requestedBase != base.Version {
						err := fmt.Errorf("base_version %q does not match compared artifact version %q", requestedBase, base.Version)
						payload := output.ErrorPayload{Code: "validation", Message: err.Error()}
						code := deps.Printer.Error("artifact.publish.preview", payload)
						return &output.ExitError{Code: code, Err: err}
					}
					baseFiles, err := deps.Client.ListArtifactFiles(previewCtx, compareArtifactID)
					if err != nil {
						return apiExitError(deps, "artifact.publish.preview", err)
					}
					built, err := buildArtifactComparison(body, base, baseFiles)
					if err != nil {
						payload := output.ErrorPayload{Code: "validation", Message: err.Error()}
						code := deps.Printer.Error("artifact.publish.preview", payload)
						return &output.ExitError{Code: code, Err: err}
					}
					comparison = &built
					preview["comparison"] = built
				}
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("artifact.publish.preview", preview)
					return nil
				}
				fmt.Fprintln(deps.Stdout, title(deps, "Artifact publish preview:"))
				for _, doc := range preview["documents"].([]map[string]any) {
					fmt.Fprintf(deps.Stdout, "%s\t%s\t%d bytes\n", styled(deps, output.StyleBold, fmt.Sprint(doc["path"])), doc["role"], doc["size_bytes"])
				}
				if comparison != nil {
					writeArtifactComparison(deps.Stdout, *comparison)
				}
				fmt.Fprintln(deps.Stdout, notice(deps, output.StyleWarning, "No publication performed", "Human confirmation required before publishing."))
				return nil
			}
			if deps.Topology == config.ModeLocal {
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "artifact.publish", err)
				}
				defer store.Close()
				selection, err := localSelection(cmd.Context(), deps, store)
				if err != nil {
					return localExitError(deps, "artifact.publish", err)
				}
				input, err := localArtifactInput(body)
				if err != nil {
					return localExitError(deps, "artifact.publish", err)
				}
				artifact, err := store.PublishArtifact(cmd.Context(), selection.Workspace.ID, input)
				if err != nil {
					return localExitError(deps, "artifact.publish", err)
				}
				result := map[string]any{"artifact_id": artifact.ID, "version": artifact.Version, "status": artifact.Status, "snapshot_digest": artifact.SnapshotDigest}
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("artifact.publish", result)
					return nil
				}
				fmt.Fprintf(deps.Stdout, "%s %s (v%d)\n", styled(deps, output.StyleSuccess, "Published"), styled(deps, output.StyleBold, artifact.ID), artifact.Version)
				return nil
			}
			if err := annotateBodyWithCurrentSelection(cmd.Context(), deps, body); err != nil {
				return apiExitError(deps, "artifact.publish", err)
			}
			// Collect impact_declaration interactively when the session is a
			// real TTY and the field is absent from the JSON file. Non-TTY
			// sessions proceed without a declaration (same as --no-input)
			// instead of blocking on a prompt nobody can answer.
			if sessionInteractive(deps) {
				if _, ok := body["impact_declaration"]; !ok {
					answers, err := interactive.CollectImpactDeclaration(deps.Stdin, deps.Stdout, interactive.ImpactAnswers{})
					if err != nil {
						payload := output.ErrorPayload{Code: "usage", Message: fmt.Sprintf("impact declaration: %v", err)}
						code := deps.Printer.Error("artifact.publish", payload)
						return &output.ExitError{Code: code, Err: err}
					}
					body["impact_declaration"] = interactive.NormalizeImpactAnswers(answers)
				}
			}
			result, err := deps.Client.PublishArtifact(requestContextForBody(cmd.Context(), body), body)
			if err != nil {
				return apiExitError(deps, "artifact.publish", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("artifact.publish", result)
				return nil
			}
			if id, ok := result["artifact_id"].(string); ok {
				fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleSuccess, "Published"), styled(deps, output.StyleBold, id))
			} else {
				fmt.Fprintln(deps.Stdout, styled(deps, output.StyleSuccess, "Published artifact"))
			}
			// Publish is deliberately non-blocking on required roles (spec-first
			// drafts are legitimate), but a human in plain mode should see the
			// gap now instead of discovering it at gate time.
			if missing, ok := result["missing_roles"].([]any); ok && len(missing) > 0 {
				hint, _ := result["readiness_hint"].(string)
				if hint == "" {
					hint = fmt.Sprintf("missing required roles: %v", missing)
				}
				fmt.Fprintf(deps.Stdout, "%s %s — add the missing documents and republish before readiness gates.\n", styled(deps, output.StyleWarning, "!"), hint)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&filePath, "file", "", "JSON file to publish (required)")
	cmd.Flags().BoolVar(&previewOnly, "preview", false, "Show exact package mapping without publishing")
	cmd.Flags().StringVar(&compareArtifactID, "compare", "", "Compare preview with one published artifact using stored hashes")
	return cmd
}

func localArtifactInput(body map[string]any) (local.ArtifactInput, error) {
	input := local.ArtifactInput{}
	input.FeatureKey, _ = body["feature_key"].(string)
	input.RequestType, _ = body["request_type"].(string)
	rawDocuments, ok := body["documents"].([]any)
	if !ok {
		return input, fmt.Errorf("documents must be an array")
	}
	for _, raw := range rawDocuments {
		document, ok := raw.(map[string]any)
		if !ok {
			return input, fmt.Errorf("documents must contain objects")
		}
		path, _ := document["path"].(string)
		role, _ := document["role"].(string)
		content, _ := document["content"].(string)
		input.Documents = append(input.Documents, local.ArtifactDocumentInput{Path: path, Role: role, Content: []byte(content)})
	}
	return input, nil
}

func localArtifactView(artifact local.Artifact, includeContent bool) map[string]any {
	view := map[string]any{"id": artifact.ID, "workspace_id": artifact.WorkspaceID, "feature_key": artifact.FeatureKey, "request_type": artifact.RequestType, "version": artifact.Version, "status": artifact.Status, "snapshot_digest": artifact.SnapshotDigest, "created_at": artifact.CreatedAt}
	if includeContent {
		documents := make([]map[string]any, 0, len(artifact.Documents))
		for _, document := range artifact.Documents {
			documents = append(documents, map[string]any{"path": document.Path, "role": document.Role, "content": string(document.Content), "digest": document.Digest, "size_bytes": document.SizeBytes})
		}
		view["documents"] = documents
	}
	return view
}

func artifactPublishPreview(body map[string]any, sources []string) map[string]any {
	documents := []map[string]any{}
	if raw, ok := body["documents"].([]any); ok {
		for index, item := range raw {
			doc, ok := item.(map[string]any)
			if !ok {
				continue
			}
			content, _ := doc["content"].(string)
			row := map[string]any{
				"path": doc["path"], "role": doc["role"], "size_bytes": len(content),
			}
			if index < len(sources) && sources[index] != "" {
				row["source_path"] = sources[index]
			}
			documents = append(documents, row)
		}
	}
	base, _ := body["base_version"].(string)
	target := body["feature_key"]
	if target == nil {
		target = body["feature_id"]
	}
	omitted := []string{}
	if _, declared := body["impact_declaration"]; !declared {
		omitted = append(omitted, "impact_declaration")
	}
	preview := map[string]any{
		"source_kind": body["source_kind"], "source_id": body["source_id"], "source_revision": body["source_revision"],
		"documents": documents, "target": target, "base_version": base, "new_artifact": base == "",
		"omitted": omitted, "ambiguous": []string{}, "human_confirmation_required": true,
		"non_goals": []string{"No filesystem watcher", "No implicit repository-wide upload"},
	}
	if len(omitted) > 0 {
		preview["governance_hint"] = "Impact declaration missing; Full mode may select stricter governance."
	}
	return preview
}

func normalizeArtifactPublishBody(body map[string]any) error {
	if _, ok := body["version"]; ok {
		return fmt.Errorf("version is server-assigned; remove version from the publish file and use base_version only when publishing an update")
	}
	if _, hasRequestType := body["request_type"]; !hasRequestType {
		if workType, ok := body["work_type"]; ok {
			body["request_type"] = workType
			delete(body, "work_type")
		} else {
			body["request_type"] = "unknown"
		}
	}
	if requestType, ok := body["request_type"].(string); ok {
		body["request_type"] = strings.TrimSpace(requestType)
	}
	return nil
}

func validateArtifactPublishFields(body map[string]any) error {
	allowed := map[string]bool{
		"feature_key": true, "feature_name": true, "workspace_id": true,
		"base_version": true, "documents": true, "source_kind": true,
		"source_revision": true, "source_id": true, "created_by": true,
		"impact_level": true, "request_type": true, "authority": true,
		"requested_governance_level": true, "impact_declaration": true,
	}
	var unknown []string
	for field := range body {
		if !allowed[field] {
			unknown = append(unknown, field)
		}
	}
	slices.Sort(unknown)
	if len(unknown) > 0 {
		return fmt.Errorf("unknown artifact package field %q", unknown[0])
	}
	requestType, ok := body["request_type"].(string)
	if !ok || !slices.Contains([]string{"new_feature", "change_request", "bugfix", "unknown"}, strings.TrimSpace(requestType)) {
		return fmt.Errorf("request_type must be new_feature, change_request, bugfix, or unknown")
	}
	documents, ok := body["documents"].([]any)
	if !ok {
		return nil
	}
	allowedDocument := map[string]bool{
		"path": true, "role": true, "content": true, "source_file": true, "file_url": true,
	}
	for index, raw := range documents {
		document, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		unknown = unknown[:0]
		for field := range document {
			if !allowedDocument[field] {
				unknown = append(unknown, field)
			}
		}
		slices.Sort(unknown)
		if len(unknown) > 0 {
			return fmt.Errorf("unknown artifact package field %q", fmt.Sprintf("documents[%d].%s", index, unknown[0]))
		}
		sourceFields := make([]string, 0, 3)
		for _, field := range []string{"content", "source_file", "file_url"} {
			if _, exists := document[field]; exists {
				sourceFields = append(sourceFields, field)
			}
		}
		if len(sourceFields) == 0 {
			return fmt.Errorf("documents[%d] must set exactly one of content, source_file, or file_url", index)
		}
		if len(sourceFields) > 1 {
			return fmt.Errorf("documents[%d] must set exactly one of content, source_file, or file_url; found %s", index, strings.Join(sourceFields, ", "))
		}
		if _, ok := document[sourceFields[0]].(string); !ok {
			return fmt.Errorf("documents[%d].%s must be a string", index, sourceFields[0])
		}
	}
	return nil
}

func expandArtifactDocumentSources(body map[string]any, packagePath string) ([]string, error) {
	rawDocuments, ok := body["documents"]
	if !ok || rawDocuments == nil {
		return nil, nil
	}

	documents, ok := rawDocuments.([]any)
	if !ok {
		return nil, fmt.Errorf("documents must be an array")
	}

	packageDir := filepath.Dir(packagePath)
	realPackageDir, err := filepath.EvalSymlinks(packageDir)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact package directory %s: %w", packageDir, err)
	}
	sources := make([]string, len(documents))
	for index, raw := range documents {
		document, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("documents[%d] must be an object", index)
		}

		sourceFile, hasSourceFile := stringField(document, "source_file")
		fileURL, hasFileURL := stringField(document, "file_url")
		if hasSourceFile && hasFileURL {
			return nil, fmt.Errorf("documents[%d] must use source_file or file_url, not both", index)
		}
		if !hasSourceFile && !hasFileURL {
			continue
		}
		if content, hasContent := stringField(document, "content"); hasContent && content != "" {
			return nil, fmt.Errorf("documents[%d] must use content or a file reference, not both", index)
		}

		var (
			sourcePath string
			sourceInfo os.FileInfo
		)
		if hasFileURL {
			parsed, err := url.Parse(fileURL)
			if err != nil || parsed.Scheme != "file" || parsed.Host != "" || parsed.Path == "" {
				return nil, fmt.Errorf("documents[%d].file_url must be an absolute local file:// URL", index)
			}
			sourcePath = filepath.FromSlash(parsed.Path)
			if !filepath.IsAbs(sourcePath) {
				return nil, fmt.Errorf("documents[%d].file_url must be an absolute local file:// URL", index)
			}
		} else {
			if filepath.IsAbs(sourceFile) {
				return nil, fmt.Errorf("documents[%d].source_file must be relative; use file_url for an explicit external file", index)
			}
			clean := filepath.Clean(sourceFile)
			if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
				return nil, fmt.Errorf("documents[%d].source_file must stay within the artifact package directory", index)
			}
			sourcePath = filepath.Join(packageDir, clean)
			sourceInfo, err = os.Lstat(sourcePath)
			if err != nil {
				return nil, fmt.Errorf("read documents[%d] source %s: %w", index, sourcePath, err)
			}
			if sourceInfo.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("documents[%d] source %s is a symlink; publish the regular file explicitly", index, sourcePath)
			}
			realSource, err := filepath.EvalSymlinks(sourcePath)
			if err != nil {
				return nil, fmt.Errorf("read documents[%d] source %s: %w", index, sourcePath, err)
			}
			relative, err := filepath.Rel(realPackageDir, realSource)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return nil, fmt.Errorf("documents[%d].source_file must stay within the artifact package directory", index)
			}
		}

		if sourceInfo == nil {
			sourceInfo, err = os.Lstat(sourcePath)
		}
		if err != nil {
			return nil, fmt.Errorf("read documents[%d] source %s: %w", index, sourcePath, err)
		}
		if sourceInfo.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("documents[%d] source %s is a symlink; publish the regular file explicitly", index, sourcePath)
		}
		content, err := readArtifactSource(sourcePath, sourceInfo)
		if err != nil {
			return nil, fmt.Errorf("read documents[%d] source %s: %w", index, sourcePath, err)
		}
		if !utf8.Valid(content) {
			return nil, fmt.Errorf("documents[%d] source %s is not valid UTF-8 text", index, sourcePath)
		}

		absoluteSource, err := filepath.Abs(sourcePath)
		if err != nil {
			return nil, fmt.Errorf("resolve documents[%d] source %s: %w", index, sourcePath, err)
		}
		sources[index] = absoluteSource
		document["content"] = string(content)
		delete(document, "source_file")
		delete(document, "file_url")
	}
	return sources, nil
}

func readArtifactSource(path string, info os.FileInfo) ([]byte, error) {
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("source is not a regular file")
	}
	if info.Size() > artifactSourceMaxBytes {
		return nil, fmt.Errorf("source exceeds the 1 MiB limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, openedInfo) {
		return nil, fmt.Errorf("source changed while it was being opened")
	}
	if !openedInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("source is not a regular file")
	}
	if openedInfo.Size() > artifactSourceMaxBytes {
		return nil, fmt.Errorf("source exceeds the 1 MiB limit")
	}
	content, err := io.ReadAll(io.LimitReader(file, artifactSourceMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > artifactSourceMaxBytes {
		return nil, fmt.Errorf("source exceeds the 1 MiB limit")
	}
	return content, nil
}

func stringField(values map[string]any, key string) (string, bool) {
	value, ok := values[key]
	if !ok || value == nil {
		return "", false
	}
	text, ok := value.(string)
	return text, ok
}

// specgate artifact approve <artifact-id> [--note <text>]
