package command

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func registerArtifactCommands(root *cobra.Command, deps *Deps) {
	art := &cobra.Command{
		Use:   "artifact",
		Short: "Manage and inspect artifacts",
	}
	art.AddCommand(newArtifactListCmd(deps))
	art.AddCommand(newArtifactShowCmd(deps))
	art.AddCommand(newArtifactFilesCmd(deps))
	art.AddCommand(newArtifactPublishCmd(deps))
	art.AddCommand(newArtifactProposeCmd(deps))
	root.AddCommand(art)
}

// specgate artifact list [--status <s>] [--feature <f>]
func newArtifactListCmd(deps *Deps) *cobra.Command {
	var (
		status    string
		featureID string
		limit     int
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List artifacts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			list, err := deps.Client.ListArtifacts(cmd.Context(), client.ArtifactFilter{
				FeatureID: featureID,
				Status:    status,
				Limit:     limit,
			})
			if err != nil {
				code := deps.Printer.Error("artifact.list", mapAPIError("artifact.list", err))
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("artifact.list", list)
				return nil
			}
			if len(list.Items) == 0 {
				fmt.Fprintln(deps.Stdout, "No artifacts found.")
				return nil
			}
			for _, a := range list.Items {
				line := fmt.Sprintf("%s  %s  %s", a.ID[:8], a.Version, a.Status)
				if a.FeatureName != "" {
					line += "  " + a.FeatureName
				} else if a.FeatureID != "" {
					line += "  " + a.FeatureID
				}
				fmt.Fprintln(deps.Stdout, line)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "Filter by status (draft, approved, …)")
	cmd.Flags().StringVar(&featureID, "feature", "", "Filter by feature ID")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum number of results")
	return cmd
}

// specgate artifact show <id>
func newArtifactShowCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show artifact details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := deps.Client.GetArtifact(cmd.Context(), args[0])
			if err != nil {
				code := deps.Printer.Error("artifact.show", mapAPIError("artifact.show", err))
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("artifact.show", a)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "ID:      %s\n", a.ID)
			fmt.Fprintf(deps.Stdout, "Version: %s\n", a.Version)
			fmt.Fprintf(deps.Stdout, "Status:  %s\n", a.Status)
			fmt.Fprintf(deps.Stdout, "Type:    %s\n", a.RequestType)
			if a.FeatureName != "" {
				fmt.Fprintf(deps.Stdout, "Feature: %s\n", a.FeatureName)
			} else if a.FeatureID != "" {
				fmt.Fprintf(deps.Stdout, "Feature: %s\n", a.FeatureID)
			}
			return nil
		},
	}
}

// specgate artifact files <id> [path1 path2 ...]
// No paths: lists file metadata. With paths: prints file references unless
// --content is set.
func newArtifactFilesCmd(deps *Deps) *cobra.Command {
	var includeContent bool
	cmd := &cobra.Command{
		Use:   "files <id> [path...]",
		Short: "List or fetch artifact file content",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			paths := args[1:]

			if len(paths) == 0 {
				files, err := deps.Client.ListArtifactFiles(cmd.Context(), id)
				if err != nil {
					code := deps.Printer.Error("artifact.files", mapAPIError("artifact.files", err))
					return &output.ExitError{Code: code, Err: err}
				}
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("artifact.files", map[string]any{"items": files})
					return nil
				}
				for _, f := range files {
					fmt.Fprintf(deps.Stdout, "%s\t%s\t%d\n", f.Path, f.Role, f.SizeBytes)
				}
				return nil
			}

			// Fetch content for requested paths.
			type fileRow struct {
				Path      string `json:"path"`
				SizeBytes int64  `json:"size_bytes,omitempty"`
				SignedURL string `json:"signed_url,omitempty"`
				Content   string `json:"content,omitempty"`
			}
			rows := make([]fileRow, 0, len(paths))
			for _, p := range paths {
				fc, err := deps.Client.GetArtifactFile(cmd.Context(), id, p)
				if err != nil {
					code := deps.Printer.Error("artifact.files", mapAPIError("artifact.files", err))
					return &output.ExitError{Code: code, Err: err}
				}
				row := fileRow{Path: p, SizeBytes: fc.SizeBytes, SignedURL: fc.SignedURL}
				if includeContent {
					row.Content = fc.Content
				}
				rows = append(rows, row)
			}

			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("artifact.files", map[string]any{"files": rows})
				return nil
			}

			for _, r := range rows {
				if includeContent {
					fmt.Fprintf(deps.Stdout, "--- %s ---\n", r.Path)
					fmt.Fprint(deps.Stdout, r.Content)
					if !strings.HasSuffix(r.Content, "\n") {
						fmt.Fprintln(deps.Stdout)
					}
					continue
				}
				if r.SignedURL != "" {
					fmt.Fprintf(deps.Stdout, "%s\t%d\t%s\n", r.Path, r.SizeBytes, r.SignedURL)
				} else {
					fmt.Fprintf(deps.Stdout, "%s\t%d\n", r.Path, r.SizeBytes)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&includeContent, "content", false, "Print full file content instead of file references")
	return cmd
}

// specgate artifact publish --file <path>
func newArtifactPublishCmd(deps *Deps) *cobra.Command {
	var filePath string
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish an artifact version from a JSON file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if filePath == "" {
				payload := output.ErrorPayload{Code: "usage", Message: "--file is required"}
				code := deps.Printer.Error("artifact.publish", payload)
				return &output.ExitError{Code: code, Err: ErrInputRequired}
			}
			data, err := os.ReadFile(filePath)
			if err != nil {
				payload := output.ErrorPayload{Code: "usage", Message: fmt.Sprintf("read file %s: %v", filePath, err)}
				code := deps.Printer.Error("artifact.publish", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			var body map[string]any
			if err := json.Unmarshal(data, &body); err != nil {
				payload := output.ErrorPayload{Code: "usage", Message: fmt.Sprintf("parse JSON from %s: %v", filePath, err)}
				code := deps.Printer.Error("artifact.publish", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			if err := expandArtifactDocumentSources(body, filePath); err != nil {
				payload := output.ErrorPayload{Code: "usage", Message: err.Error()}
				code := deps.Printer.Error("artifact.publish", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			// Collect impact_declaration interactively when interactive mode is
			// enabled and the field is absent from the JSON file.
			if !deps.NoInput {
				if _, ok := body["impact_declaration"]; !ok {
					answers, err := interactive.CollectImpactDeclaration(deps.Stdin, deps.Stdout, interactive.ImpactAnswers{})
					if err != nil {
						payload := output.ErrorPayload{Code: "usage", Message: fmt.Sprintf("impact declaration: %v", err)}
						code := deps.Printer.Error("artifact.publish", payload)
						return &output.ExitError{Code: code, Err: err}
					}
					answers = interactive.NormalizeImpactAnswers(answers)
					// Marshal to map[string]any so it round-trips through the
					// server's ImpactDeclaration JSON tags correctly.
					raw, err := json.Marshal(answers)
					if err != nil {
						return &output.ExitError{Code: output.ExitUsage, Err: err}
					}
					var decl map[string]any
					if err := json.Unmarshal(raw, &decl); err != nil {
						return &output.ExitError{Code: output.ExitUsage, Err: err}
					}
					body["impact_declaration"] = decl
				}
			}
			result, err := deps.Client.PublishArtifact(cmd.Context(), body)
			if err != nil {
				code := deps.Printer.Error("artifact.publish", mapAPIError("artifact.publish", err))
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("artifact.publish", result)
				return nil
			}
			if id, ok := result["artifact_id"].(string); ok {
				fmt.Fprintf(deps.Stdout, "Published %s\n", id)
			} else {
				fmt.Fprintln(deps.Stdout, "Published artifact")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&filePath, "file", "", "JSON file to publish (required)")
	return cmd
}

func expandArtifactDocumentSources(body map[string]any, packagePath string) error {
	rawDocuments, ok := body["documents"]
	if !ok || rawDocuments == nil {
		return nil
	}

	documents, ok := rawDocuments.([]any)
	if !ok {
		return fmt.Errorf("documents must be an array")
	}

	packageDir := filepath.Dir(packagePath)
	for index, raw := range documents {
		document, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("documents[%d] must be an object", index)
		}

		sourceFile, hasSourceFile := stringField(document, "source_file")
		fileURL, hasFileURL := stringField(document, "file_url")
		if hasSourceFile && hasFileURL {
			return fmt.Errorf("documents[%d] must use source_file or file_url, not both", index)
		}
		if !hasSourceFile && !hasFileURL {
			continue
		}
		if content, hasContent := stringField(document, "content"); hasContent && content != "" {
			return fmt.Errorf("documents[%d] must use content or a file reference, not both", index)
		}

		sourcePath := sourceFile
		if hasFileURL {
			parsed, err := url.Parse(fileURL)
			if err != nil || parsed.Scheme != "file" || parsed.Host != "" || parsed.Path == "" {
				return fmt.Errorf("documents[%d].file_url must be a local file:// URL", index)
			}
			sourcePath = parsed.Path
		}
		if !filepath.IsAbs(sourcePath) {
			sourcePath = filepath.Join(packageDir, sourcePath)
		}

		content, err := os.ReadFile(sourcePath)
		if err != nil {
			return fmt.Errorf("read documents[%d] source %s: %w", index, sourcePath, err)
		}
		if !utf8.Valid(content) {
			return fmt.Errorf("documents[%d] source %s is not valid UTF-8 text", index, sourcePath)
		}

		document["content"] = string(content)
		delete(document, "source_file")
		delete(document, "file_url")
	}
	return nil
}

func stringField(values map[string]any, key string) (string, bool) {
	value, ok := values[key]
	if !ok || value == nil {
		return "", false
	}
	text, ok := value.(string)
	return text, ok
}

// specgate artifact propose <id> --file <path>
func newArtifactProposeCmd(deps *Deps) *cobra.Command {
	var filePath string
	cmd := &cobra.Command{
		Use:   "propose <id>",
		Short: "Open a draft artifact-edit proposal from a JSON file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			artifactID := args[0]
			if filePath == "" {
				payload := output.ErrorPayload{Code: "usage", Message: "--file is required"}
				code := deps.Printer.Error("artifact.propose", payload)
				return &output.ExitError{Code: code, Err: ErrInputRequired}
			}
			data, err := os.ReadFile(filePath)
			if err != nil {
				payload := output.ErrorPayload{Code: "usage", Message: fmt.Sprintf("read file %s: %v", filePath, err)}
				code := deps.Printer.Error("artifact.propose", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			var body map[string]any
			if err := json.Unmarshal(data, &body); err != nil {
				payload := output.ErrorPayload{Code: "usage", Message: fmt.Sprintf("parse JSON from %s: %v", filePath, err)}
				code := deps.Printer.Error("artifact.propose", payload)
				return &output.ExitError{Code: code, Err: err}
			}
			result, err := deps.Client.DraftProposal(cmd.Context(), artifactID, body)
			if err != nil {
				code := deps.Printer.Error("artifact.propose", mapAPIError("artifact.propose", err))
				return &output.ExitError{Code: code, Err: err}
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("artifact.propose", result)
				return nil
			}
			if result.Drafted {
				fmt.Fprintf(deps.Stdout, "Proposal opened: %s\n", result.SessionID)
			} else {
				fmt.Fprintf(deps.Stdout, "No changes: %s\n", result.Reason)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&filePath, "file", "", "JSON file with proposal body (required)")
	return cmd
}
