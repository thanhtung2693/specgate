package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/interactive"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
)

const artifactSourceMaxBytes = 1 << 20

func registerArtifactCommands(root *cobra.Command, deps *Deps) {
	art := &cobra.Command{
		Use:   "artifact",
		Short: "Manage and inspect artifacts",
	}
	art.AddCommand(newArtifactListCmd(deps))
	art.AddCommand(newArtifactShowCmd(deps))
	art.AddCommand(newArtifactCoverageCmd(deps))
	art.AddCommand(newArtifactFilesCmd(deps))
	art.AddCommand(newArtifactPublishCmd(deps))
	art.AddCommand(newArtifactApproveCmd(deps))
	art.AddCommand(newArtifactPromoteCmd(deps))
	art.AddCommand(newArtifactRequestChangesCmd(deps))
	root.AddCommand(art)
}

// specgate artifact coverage <artifact-id>
func newArtifactCoverageCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "coverage <artifact-id>",
		Short: "Show delivery coverage for this exact artifact version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if deps.Topology == config.ModeLocal {
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "artifact.coverage", err)
				}
				defer store.Close()
				selection, err := localSelection(cmd.Context(), deps, store)
				if err != nil {
					return localExitError(deps, "artifact.coverage", err)
				}
				artifact, err := store.GetArtifact(cmd.Context(), selection.Workspace.ID, id)
				if err != nil {
					return localExitError(deps, "artifact.coverage", err)
				}
				items, err := store.ListWork(cmd.Context(), selection.Workspace.ID)
				if err != nil {
					return localExitError(deps, "artifact.coverage", err)
				}
				return printArtifactCoverage(deps, artifactCoverageView(id, artifact.Status, localWorkCoverage(items, id)))
			}
			workspaceID, err := currentWorkspaceID(cmd.Context(), deps)
			if err != nil {
				return apiExitError(deps, "artifact.coverage", err)
			}
			artifact, err := getArtifactForWorkspaceCLI(cmd.Context(), deps, workspaceID, id)
			if err != nil {
				return apiExitError(deps, "artifact.coverage", err)
			}
			items, err := deps.Client.ListWorkItemsIncludingArchived(cmd.Context(), workspaceID)
			if err != nil {
				return apiExitError(deps, "artifact.coverage", err)
			}
			return printArtifactCoverage(deps, artifactCoverageView(artifact.ID, artifact.Status, fullWorkCoverage(items, artifact.ID)))
		},
	}
}

func localWorkCoverage(items []local.WorkItem, artifactID string) []map[string]string {
	out := []map[string]string{}
	for _, item := range items {
		if item.ArtifactID == artifactID {
			out = append(out, map[string]string{"key": item.Key, "phase": item.Phase, "title": item.Title})
		}
	}
	return out
}
func fullWorkCoverage(items []client.WorkItemSummary, artifactID string) []map[string]string {
	out := []map[string]string{}
	for _, item := range items {
		if item.LeadArtifactID == artifactID {
			out = append(out, map[string]string{"key": item.Key, "phase": item.Phase, "title": item.Title})
		}
	}
	return out
}
func artifactCoverageView(id, artifactStatus string, items []map[string]string) map[string]any {
	state := "uncovered"
	if len(items) > 0 {
		state = "delivered"
		for _, item := range items {
			if !isDeliveredPhase(item["phase"]) {
				state = "in_progress"
				break
			}
		}
	} else if artifactStatus == "superseded" {
		state = "superseded"
	}
	return map[string]any{"artifact_id": id, "state": state, "work_items": items}
}
func printArtifactCoverage(deps *Deps, data map[string]any) error {
	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success("artifact.coverage", data)
		return nil
	}
	fmt.Fprintf(deps.Stdout, "%s %s\n", styled(deps, output.StyleBold, fmt.Sprint(data["artifact_id"]))+":", styledStatus(deps, fmt.Sprint(data["state"])))
	for _, item := range data["work_items"].([]map[string]string) {
		fmt.Fprintf(deps.Stdout, "%s  [%s]  %s\n", styled(deps, output.StyleBold, item["key"]), styledStatus(deps, item["phase"]), item["title"])
	}
	return nil
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
			if deps.Topology == config.ModeLocal {
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "artifact.list", err)
				}
				defer store.Close()
				selection, err := localSelection(cmd.Context(), deps, store)
				if err != nil {
					return localExitError(deps, "artifact.list", err)
				}
				artifacts, err := store.ListArtifacts(cmd.Context(), selection.Workspace.ID)
				if err != nil {
					return localExitError(deps, "artifact.list", err)
				}
				items := make([]map[string]any, 0, len(artifacts))
				for _, artifact := range artifacts {
					if status != "" && status != "all" && artifact.Status != status {
						continue
					}
					if featureID != "" && artifact.FeatureKey != featureID {
						continue
					}
					items = append(items, localArtifactView(artifact, false))
					if limit > 0 && len(items) == limit {
						break
					}
				}
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("artifact.list", map[string]any{"items": items})
					return nil
				}
				if len(items) == 0 {
					fmt.Fprintln(deps.Stdout, notice(deps, output.StyleInfo, "Notice", "No artifacts found."))
					return nil
				}
				for _, item := range items {
					fmt.Fprintf(deps.Stdout, "%s  v%d  %s  %s\n", styled(deps, output.StyleBold, fmt.Sprint(item["id"])), item["version"], styledStatus(deps, fmt.Sprint(item["status"])), item["feature_key"])
				}
				return nil
			}
			workspaceID, err := currentWorkspaceID(cmd.Context(), deps)
			if err != nil {
				return apiExitError(deps, "artifact.list", err)
			}
			// Default view shows current artifacts; superseded versions stay
			// reachable via --status all or --status superseded. Filtering is
			// server-side so limit and total keep their real semantics.
			// --feature accepts a feature key or id; resolve to the id the
			// server filters on (the key alone matched nothing before).
			featureRef := featureID
			if featureRef != "" {
				feat, err := getFeatureForWorkspaceCLI(cmd.Context(), deps, workspaceID, featureRef)
				if err != nil {
					return apiExitError(deps, "artifact.list", err)
				}
				featureRef = feat.ID
			}
			filter := client.ArtifactFilter{WorkspaceID: workspaceID, FeatureID: featureRef, Limit: limit}
			switch {
			case strings.EqualFold(status, "all"):
				// no status filters
			case status != "":
				filter.Status = status
			default:
				filter.ExcludeStatus = "superseded"
			}
			list, err := deps.Client.ListArtifacts(cmd.Context(), filter)
			if err != nil {
				return apiExitError(deps, "artifact.list", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("artifact.list", list)
				return nil
			}
			if len(list.Items) == 0 {
				fmt.Fprintln(deps.Stdout, notice(deps, output.StyleInfo, "Notice", "No artifacts found."))
				return nil
			}
			fmt.Fprintf(deps.Stdout, "%s  %s  %s  %s  %s\n", label(deps, fmt.Sprintf("%-10s", "ID")), label(deps, fmt.Sprintf("%-8s", "VERSION")), label(deps, fmt.Sprintf("%-14s", "STATUS")), label(deps, fmt.Sprintf("%-30s", "FEATURE")), label(deps, "UPDATED"))
			for _, a := range list.Items {
				feature := a.FeatureName
				if feature == "" {
					feature = a.FeatureID
				}
				fmt.Fprintf(deps.Stdout, "%s  %-8s  %s  %-30s  %s\n",
					styled(deps, output.StyleBold, fmt.Sprintf("%-10s", shortID(a.ID, 10))), a.Version, styledStatusPadded(deps, a.Status, 14), truncateText(feature, 30), formatTimestamp(a.UpdatedAt))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "Filter by status (draft, approved, …); default hides superseded, use 'all' to show every status")
	cmd.Flags().StringVar(&featureID, "feature", "", "Filter by feature key or ID")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum number of results")
	return cmd
}

// specgate artifact show <id>
//
// The ref may be a full artifact id or a unique id prefix (as printed by
// `artifact list`). On a lookup miss, the CLI fetches the
// artifact list and resolves the prefix.
func newArtifactShowCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show artifact details (full id or unique id prefix)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if deps.Topology == config.ModeLocal {
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "artifact.show", err)
				}
				defer store.Close()
				selection, err := localSelection(cmd.Context(), deps, store)
				if err != nil {
					return localExitError(deps, "artifact.show", err)
				}
				artifact, err := store.GetArtifact(cmd.Context(), selection.Workspace.ID, args[0])
				if err != nil {
					return localExitError(deps, "artifact.show", err)
				}
				view := localArtifactView(artifact, true)
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("artifact.show", view)
					return nil
				}
				fmt.Fprintf(deps.Stdout, "%s %s\n%s %d\n%s %s\n%s %s\n", label(deps, "ID:"), styled(deps, output.StyleBold, artifact.ID), label(deps, "Version:"), artifact.Version, label(deps, "Status:"), styledStatus(deps, artifact.Status), label(deps, "Feature:"), artifact.FeatureKey)
				for _, document := range artifact.Documents {
					fmt.Fprintf(deps.Stdout, "\n--- %s ---\n%s", document.Path, document.Content)
				}
				return nil
			}
			workspaceID, werr := currentWorkspaceID(cmd.Context(), deps)
			if werr != nil {
				return apiExitError(deps, "artifact.show", werr)
			}
			ref := args[0]
			a, err := getArtifactForWorkspaceCLI(cmd.Context(), deps, workspaceID, ref)
			if err != nil && isArtifactLookupMiss(err) {
				fullID, payload := resolveArtifactIDPrefix(cmd, deps, ref)
				if payload != nil {
					code := deps.Printer.Error("artifact.show", *payload)
					return &output.ExitError{Code: code, Err: err}
				}
				a, err = getArtifactForWorkspaceCLI(cmd.Context(), deps, workspaceID, fullID)
			}
			if err != nil {
				return apiExitError(deps, "artifact.show", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("artifact.show", a)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "ID:"), styled(deps, output.StyleBold, a.ID))
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Version:"), a.Version)
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Status:"), styledStatus(deps, a.Status))
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Type:"), a.RequestType)
			if a.FeatureName != "" {
				fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Feature:"), a.FeatureName)
			} else if a.FeatureID != "" {
				fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Feature:"), a.FeatureID)
			}
			if a.SourceRevision != "" {
				fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Source:"), a.SourceRevision)
			}
			if a.SnapshotDigest != "" {
				fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Digest:"), a.SnapshotDigest)
			}
			return nil
		},
	}
}

// isArtifactLookupMiss reports whether err is a server response that means the
// ref did not resolve to an artifact (404, or 400 for a non-UUID ref).
func isArtifactLookupMiss(err error) bool {
	var apiErr *client.APIError
	return errors.As(err, &apiErr) && (apiErr.Kind == client.ErrorNotFound || apiErr.Kind == client.ErrorUsage)
}

// resolveArtifactIDPrefix resolves an id prefix against the artifact list.
// It returns the full id, or an error payload when the prefix matches zero or
// multiple artifacts.
func resolveArtifactIDPrefix(cmd *cobra.Command, deps *Deps, prefix string) (string, *output.ErrorPayload) {
	workspaceID, _ := currentWorkspaceID(cmd.Context(), deps)
	list, err := deps.Client.ListArtifacts(cmd.Context(), client.ArtifactFilter{WorkspaceID: workspaceID, Limit: 200})
	if err != nil {
		payload := mapAPIError(err)
		return "", &payload
	}
	var matches []string
	for _, a := range list.Items {
		if strings.HasPrefix(a.ID, prefix) {
			matches = append(matches, a.ID)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", &output.ErrorPayload{
			Code:    "not_found",
			Message: fmt.Sprintf("artifact %q not found — try `specgate artifact list`", prefix),
		}
	default:
		return "", &output.ErrorPayload{
			Code:    "validation_failed",
			Message: fmt.Sprintf("artifact id prefix %q is ambiguous — matches: %s", prefix, strings.Join(matches, ", ")),
		}
	}
}

// shortID returns the first n characters of id (or id when shorter).
func shortID(id string, n int) string {
	if len(id) <= n {
		return id
	}
	return id[:n]
}

// truncateText shortens s to at most max runes, marking truncation with "...".
func truncateText(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-3]) + "..."
}

// formatTimestamp renders an RFC 3339 timestamp as local "2006-01-02 15:04",
// falling back to the raw value when it does not parse.
func formatTimestamp(ts string) string {
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t.Local().Format("2006-01-02 15:04")
	}
	return ts
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
			workspaceID, err := currentWorkspaceID(cmd.Context(), deps)
			if err != nil {
				return apiExitError(deps, "artifact.files", err)
			}
			if workspaceID == "" {
				payload := output.ErrorPayload{Code: "validation", Message: "select a workspace first with `specgate workspace select` or bind this repo with `specgate workspace bind`"}
				code := deps.Printer.Error("artifact.files", payload)
				return &output.ExitError{Code: code}
			}
			ctx := client.WithWorkspace(cmd.Context(), workspaceID)
			id := args[0]
			paths := args[1:]

			if len(paths) == 0 {
				files, err := deps.Client.ListArtifactFiles(ctx, id)
				if err != nil {
					return apiExitError(deps, "artifact.files", err)
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
				Content   string `json:"content,omitempty"`
			}
			rows := make([]fileRow, 0, len(paths))
			for _, p := range paths {
				fc, err := deps.Client.GetArtifactFile(ctx, id, p)
				if err != nil {
					return apiExitError(deps, "artifact.files", err)
				}
				row := fileRow{Path: p, SizeBytes: fc.SizeBytes}
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
				fmt.Fprintf(deps.Stdout, "%s\t%d\n", r.Path, r.SizeBytes)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&includeContent, "content", false, "Print full file content instead of file references")
	return cmd
}

func getArtifactForWorkspaceCLI(ctx context.Context, deps *Deps, workspaceID, id string) (*client.Artifact, error) {
	type scoped interface {
		GetArtifactInWorkspace(context.Context, string, string) (*client.Artifact, error)
	}
	if workspaceID != "" {
		if c, ok := deps.Client.(scoped); ok {
			return c.GetArtifactInWorkspace(ctx, workspaceID, id)
		}
	}
	return deps.Client.GetArtifact(ctx, id)
}

// artifactPublishPreviewContext scopes the stored-artifact reads made by
// --compare. A bare local preview remains offline; only a comparison resolves
// the selected workspace because it reads a workspace-owned artifact.
func artifactPublishPreviewContext(ctx context.Context, deps *Deps, body map[string]any) (context.Context, error) {
	if workspaceID, _ := body["workspace_id"].(string); strings.TrimSpace(workspaceID) != "" {
		return client.WithWorkspace(ctx, workspaceID), nil
	}
	workspaceID, err := currentWorkspaceID(ctx, deps)
	if err != nil {
		return nil, err
	}
	if workspaceID == "" {
		return nil, fmt.Errorf("select a workspace first with `specgate workspace select` or bind this repo with `specgate workspace bind`")
	}
	return client.WithWorkspace(ctx, workspaceID), nil
}

// specgate artifact publish --file <path>
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
func newArtifactApproveCmd(deps *Deps) *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:   "approve <artifact-id>",
		Short: "Approve an artifact version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runArtifactStatusChange(cmd, deps, "artifact.approve", args[0], "approved", note,
				fmt.Sprintf("Approve artifact %s?", args[0]),
				func(a *client.Artifact) string {
					return fmt.Sprintf("Approved %s (%s)", a.ID, a.Version)
				})
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "Optional reviewer note recorded with the decision")
	return cmd
}

// specgate artifact promote <artifact-id>
//
// Promotes an approved artifact to its feature's canonical spec — the
// deliberate approve->promote->handoff step. Without it, an approved
// feature-backed artifact never becomes the feature canonical, so the Context
// Pack handoff renders no spec content. Promotion is never automatic on approval.
func newArtifactPromoteCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "promote <artifact-id>",
		Short: "Promote an approved artifact to its feature's canonical spec",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if deps.Topology == config.ModeLocal {
				if !deps.Yes {
					payload := output.ErrorPayload{Code: "confirmation_required", Message: fmt.Sprintf("Promote artifact %s to its feature's canonical? Re-run with --yes to record this human decision.", id)}
					code := deps.Printer.Error("artifact.promote", payload)
					return &output.ExitError{Code: code}
				}
				store, err := openLocalStore(deps)
				if err != nil {
					return localExitError(deps, "artifact.promote", err)
				}
				defer store.Close()
				selection, err := localSelection(cmd.Context(), deps, store)
				if err != nil {
					return localExitError(deps, "artifact.promote", err)
				}
				feature, err := store.PromoteArtifact(cmd.Context(), selection.Workspace.ID, id)
				if err != nil {
					return localExitError(deps, "artifact.promote", err)
				}
				result := map[string]any{"id": feature.ID, "key": feature.Key, "canonical_artifact_id": feature.CanonicalArtifactID, "version": feature.Version}
				if deps.Printer.Mode() == output.ModeJSON {
					deps.Printer.Success("artifact.promote", result)
					return nil
				}
				fmt.Fprintf(deps.Stdout, "%s %s to canonical for feature %s (v%d)\n", styled(deps, output.StyleSuccess, "Promoted"), styled(deps, output.StyleBold, id), feature.Key, feature.Version)
				return nil
			}
			proceed, err := requireConfirm(deps, fmt.Sprintf("Promote artifact %s to its feature's canonical?", id))
			if err != nil || !proceed {
				return err
			}
			feature, err := deps.Client.PromoteArtifactCanonical(cmd.Context(), id, currentActor(deps))
			if err != nil {
				return apiExitError(deps, "artifact.promote", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("artifact.promote", feature)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "%s %s to canonical for feature %s (v%d)\n", styled(deps, output.StyleSuccess, "Promoted"), styled(deps, output.StyleBold, id), feature.Key, feature.Version)
			return nil
		},
	}
}

// specgate artifact request-changes <artifact-id> [--note <text>]
func newArtifactRequestChangesCmd(deps *Deps) *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:   "request-changes <artifact-id>",
		Short: "Send an artifact version back for changes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runArtifactStatusChange(cmd, deps, "artifact.request-changes", args[0], "needs_changes", note,
				fmt.Sprintf("Request changes on artifact %s?", args[0]),
				func(a *client.Artifact) string {
					return fmt.Sprintf("Requested changes on %s (%s)", a.ID, a.Version)
				})
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "Optional reviewer note recorded with the decision")
	return cmd
}

// runArtifactStatusChange performs the human status decision shared by
// `artifact approve` and `artifact request-changes`. Local mode exposes only
// approval through the root command guard; Full mode confirms interactively,
// then patches the selected artifact status with the selected user as actor.
func runArtifactStatusChange(cmd *cobra.Command, deps *Deps, op, id, status, note, confirmPrompt string, render func(*client.Artifact) string) error {
	if deps.Topology == config.ModeLocal {
		if !deps.Yes {
			payload := output.ErrorPayload{Code: "confirmation_required", Message: fmt.Sprintf("%s Re-run with --yes to record this human decision.", confirmPrompt)}
			code := deps.Printer.Error(op, payload)
			return &output.ExitError{Code: code}
		}
		store, err := openLocalStore(deps)
		if err != nil {
			return localExitError(deps, op, err)
		}
		defer store.Close()
		selection, err := localSelection(cmd.Context(), deps, store)
		if err != nil {
			return localExitError(deps, op, err)
		}
		if err := store.ApproveArtifact(cmd.Context(), selection.Workspace.ID, id, selection.User.Username, note); err != nil {
			return localExitError(deps, op, err)
		}
		artifact, err := store.GetArtifact(cmd.Context(), selection.Workspace.ID, id)
		if err != nil {
			return localExitError(deps, op, err)
		}
		if deps.Printer.Mode() == output.ModeJSON {
			deps.Printer.Success(op, localArtifactView(artifact, false))
			return nil
		}
		fmt.Fprintf(deps.Stdout, "%s %s (v%d)\n", styled(deps, output.StyleSuccess, "Approved"), styled(deps, output.StyleBold, artifact.ID), artifact.Version)
		return nil
	}
	proceed, err := requireConfirm(deps, confirmPrompt)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}
	a, err := deps.Client.UpdateArtifactStatus(cmd.Context(), id, client.UpdateArtifactStatusInput{
		Status:     status,
		ApprovedBy: currentActor(deps),
		Note:       note,
		ActorKind:  "human",
	})
	if err != nil {
		return apiExitError(deps, op, err)
	}
	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success(op, a)
		return nil
	}
	fmt.Fprintln(deps.Stdout, styled(deps, statusStyle(status), render(a)))
	return nil
}
