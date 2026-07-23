package command

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/config"
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
