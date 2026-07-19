package command

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/output"
)

const maxKnowledgeTextBytes = 10 << 20

func registerKnowledgeCommands(root *cobra.Command, deps *Deps) {
	knowledge := &cobra.Command{
		Use:   "knowledge",
		Short: "Manage workspace-scoped Governance Knowledge",
	}
	knowledge.AddCommand(newKnowledgeListCmd(deps))
	knowledge.AddCommand(newKnowledgeShowCmd(deps))
	knowledge.AddCommand(newKnowledgeSearchCmd(deps))
	knowledge.AddCommand(newKnowledgeAddTextCmd(deps))
	knowledge.AddCommand(newKnowledgeLinkCmd(deps))
	knowledge.AddCommand(newKnowledgeUnlinkCmd(deps))
	root.AddCommand(knowledge)
}

func newKnowledgeListCmd(deps *Deps) *cobra.Command {
	var filter client.KnowledgeListFilter
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Knowledge documents in the selected workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			workspaceID, err := selectedWorkspaceIDOrExit(cmd, deps, "knowledge.list")
			if err != nil {
				return err
			}
			filter.WorkspaceID = workspaceID
			list, err := deps.Client.ListKnowledgeDocuments(cmd.Context(), filter)
			if err != nil {
				return apiExitError(deps, "knowledge.list", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("knowledge.list", list)
				return nil
			}
			if len(list.Items) == 0 {
				fmt.Fprintln(deps.Stdout, notice(deps, output.StyleInfo, "Notice", "No Knowledge documents found in this workspace."))
				fmt.Fprintln(deps.Stdout, nextStep(deps, "add one with", "specgate knowledge add-text --title <title> --file <path>"))
				return nil
			}
			for _, doc := range list.Items {
				fmt.Fprintf(deps.Stdout, "%s  %-8s  %s  %s\n", styled(deps, output.StyleBold, fmt.Sprintf("%-38s", doc.DocumentID)), doc.Version, styledStatusPadded(deps, doc.Status, 14), doc.Title)
			}
			if list.Total > len(list.Items) {
				fmt.Fprintf(deps.Stdout, "%s %d of %d. %s\n", label(deps, "Showing"), len(list.Items), list.Total, nextStep(deps, "continue with", fmt.Sprintf("specgate knowledge list --offset %d", filter.Offset+len(list.Items))))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&filter.LinkedFeatureID, "feature", "", "Filter by linked feature ID")
	cmd.Flags().StringVar(&filter.LinkedRequestID, "request", "", "Filter by linked change request ID")
	cmd.Flags().StringVar(&filter.DocumentType, "type", "", "Filter by document type")
	cmd.Flags().StringVar(&filter.Status, "status", "", "Filter by ingest status")
	cmd.Flags().BoolVar(&filter.IncludeHistory, "history", false, "Include historical versions")
	cmd.Flags().IntVar(&filter.Limit, "limit", 100, "Maximum documents to return")
	cmd.Flags().IntVar(&filter.Offset, "offset", 0, "Pagination offset")
	return cmd
}

func newKnowledgeShowCmd(deps *Deps) *cobra.Command {
	var version string
	cmd := &cobra.Command{
		Use:   "show <document-id>",
		Short: "Show a Knowledge document",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workspaceID, err := selectedWorkspaceIDOrExit(cmd, deps, "knowledge.show")
			if err != nil {
				return err
			}
			detail, err := deps.Client.GetKnowledgeDocument(client.WithWorkspace(cmd.Context(), workspaceID), args[0], version)
			if err != nil {
				return apiExitError(deps, "knowledge.show", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("knowledge.show", detail)
				return nil
			}
			doc := detail.Document
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "ID:"), styled(deps, output.StyleBold, doc.DocumentID))
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Version:"), doc.Version)
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Status:"), styledStatus(deps, doc.Status))
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Title:"), doc.Title)
			fmt.Fprintf(deps.Stdout, "%s %s\n", label(deps, "Type:"), doc.DocumentType)
			if detail.ExtractedPreview != "" {
				fmt.Fprintf(deps.Stdout, "\n%s\n", detail.ExtractedPreview)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "Document version to show")
	return cmd
}

func newKnowledgeSearchCmd(deps *Deps) *cobra.Command {
	var in client.KnowledgeSearchInput
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search Knowledge in the selected workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workspaceID, err := selectedWorkspaceIDOrExit(cmd, deps, "knowledge.search")
			if err != nil {
				return err
			}
			in.WorkspaceID = workspaceID
			in.Query = strings.TrimSpace(args[0])
			results, err := deps.Client.SearchKnowledge(cmd.Context(), in)
			if err != nil {
				return apiExitError(deps, "knowledge.search", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("knowledge.search", map[string]any{"results": results})
				return nil
			}
			if len(results) == 0 {
				fmt.Fprintln(deps.Stdout, notice(deps, output.StyleInfo, "Notice", "No Knowledge results found in this workspace."))
				return nil
			}
			for _, result := range results {
				text := result.ContextText
				if text == "" {
					text = result.ChunkText
				}
				fmt.Fprintf(deps.Stdout, "%s  %s  %s\n", styled(deps, output.StyleBold, result.DocumentID), styled(deps, output.StyleMuted, fmt.Sprintf("%.3f", result.Score)), result.Title)
				if snippet := oneLineSnippet(text, 180); snippet != "" {
					fmt.Fprintf(deps.Stdout, "  %s\n", snippet)
				}
				if result.URL != "" {
					fmt.Fprintf(deps.Stdout, "  %s\n", styled(deps, output.StyleMuted, result.URL))
				}
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&in.MaxChunks, "limit", 8, "Maximum chunks to return")
	cmd.Flags().StringVar(&in.ContextMode, "context", "section", "Context mode: chunk, section, or document")
	cmd.Flags().IntVar(&in.ContextMaxChars, "context-max-chars", 4000, "Maximum context characters per result")
	cmd.Flags().StringVar(&in.LinkedFeatureID, "feature", "", "Filter by linked feature ID")
	cmd.Flags().StringVar(&in.LinkedRequestID, "request", "", "Filter by linked change request ID")
	cmd.Flags().StringArrayVar(&in.DocumentTypes, "type", nil, "Filter by document type (repeatable)")
	cmd.Flags().StringArrayVar(&in.AuthorityLevels, "authority", nil, "Filter by authority level (repeatable)")
	cmd.Flags().BoolVar(&in.IncludeHistory, "history", false, "Search historical versions too")
	return cmd
}

func newKnowledgeAddTextCmd(deps *Deps) *cobra.Command {
	var (
		filePath string
		in       client.KnowledgeCreateTextInput
	)
	cmd := &cobra.Command{
		Use:   "add-text",
		Short: "Add a text Knowledge document to the selected workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			workspaceID, err := selectedWorkspaceIDOrExit(cmd, deps, "knowledge.add-text")
			if err != nil {
				return err
			}
			content, err := readKnowledgeTextInput(deps, filePath)
			if err != nil {
				code := deps.Printer.Error("knowledge.add-text", output.ErrorPayload{Code: "usage", Message: err.Error()})
				return &output.ExitError{Code: code, Err: err}
			}
			in.WorkspaceID = workspaceID
			in.Content = content
			if in.UploadedBy == "" {
				in.UploadedBy = currentActor(deps)
			}
			doc, err := deps.Client.CreateTextKnowledgeDocument(cmd.Context(), in)
			if err != nil {
				return apiExitError(deps, "knowledge.add-text", err)
			}
			if deps.Printer.Mode() == output.ModeJSON {
				deps.Printer.Success("knowledge.add-text", doc)
				return nil
			}
			fmt.Fprintf(deps.Stdout, "%s %s %s (%s)\n", styled(deps, output.StyleSuccess, "Added"), styled(deps, output.StyleBold, doc.DocumentID), doc.Version, styledStatus(deps, doc.Status))
			return nil
		},
	}
	cmd.Flags().StringVar(&in.Title, "title", "", "Document title")
	cmd.Flags().StringVar(&filePath, "file", "", "Text file to ingest (`-` reads stdin)")
	cmd.Flags().StringVar(&in.DocumentType, "type", "supporting_doc", "Document type")
	cmd.Flags().StringVar(&in.AuthorityLevel, "authority", "reference", "Authority level")
	cmd.Flags().StringVar(&in.LinkedFeatureID, "feature", "", "Linked feature ID")
	cmd.Flags().StringVar(&in.LinkedRequestID, "request", "", "Linked change request ID")
	cmd.Flags().StringVar(&in.Notes, "notes", "", "Optional curator notes")
	cmd.Flags().StringArrayVar(&in.Tags, "tag", nil, "Tag to attach (repeatable)")
	cmd.Flags().StringVar(&in.ActorRole, "actor-role", "", "Role of the uploader")
	_ = cmd.MarkFlagRequired("title")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newKnowledgeLinkCmd(deps *Deps) *cobra.Command {
	var (
		featureID  string
		requestRef string
		in         client.KnowledgeCurateLinksInput
	)
	cmd := &cobra.Command{
		Use:   "link <document-id>",
		Short: "Create a new Knowledge version linked to a feature or work item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workspaceID, err := selectedWorkspaceIDOrExit(cmd, deps, "knowledge.link")
			if err != nil {
				return err
			}
			ctx := client.WithWorkspace(cmd.Context(), workspaceID)
			if strings.TrimSpace(featureID) == "" && strings.TrimSpace(requestRef) == "" {
				payload := output.ErrorPayload{Code: "validation", Message: "pass --feature or --request"}
				code := deps.Printer.Error("knowledge.link", payload)
				return &output.ExitError{Code: code}
			}
			if strings.TrimSpace(featureID) != "" && strings.TrimSpace(requestRef) != "" {
				payload := output.ErrorPayload{Code: "validation", Message: "pass only one of --feature or --request"}
				code := deps.Printer.Error("knowledge.link", payload)
				return &output.ExitError{Code: code}
			}
			in.LinkedFeatureID = strings.TrimSpace(featureID)
			in.WorkspaceID = workspaceID
			if strings.TrimSpace(requestRef) != "" {
				work, err := deps.Client.ResolveWorkRef(ctx, requestRef)
				if err != nil {
					code := deps.Printer.Error("knowledge.link", mapWorkRefError(requestRef, err))
					return &output.ExitError{Code: code, Err: err}
				}
				in.LinkedRequestID = work.ChangeRequestID
			}
			if in.UploadedBy == "" {
				in.UploadedBy = currentActor(deps)
			}
			in.WorkspaceID = workspaceID
			doc, err := deps.Client.CurateKnowledgeLinks(ctx, args[0], in)
			if err != nil {
				return apiExitError(deps, "knowledge.link", err)
			}
			printKnowledgeCurationResult(deps, "knowledge.link", doc, "Linked")
			return nil
		},
	}
	cmd.Flags().StringVar(&featureID, "feature", "", "Feature ID or key to link")
	cmd.Flags().StringVar(&requestRef, "request", "", "Work item ref to link")
	cmd.Flags().StringVar(&in.Version, "version", "", "Source document version (default latest)")
	cmd.Flags().StringVar(&in.Notes, "notes", "", "Optional curator notes for the new version")
	cmd.Flags().StringVar(&in.ActorRole, "actor-role", "", "Role of the curator")
	return cmd
}

func newKnowledgeUnlinkCmd(deps *Deps) *cobra.Command {
	var in client.KnowledgeCurateLinksInput
	cmd := &cobra.Command{
		Use:   "unlink <document-id>",
		Short: "Create a new Knowledge version with selected links cleared",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workspaceID, err := selectedWorkspaceIDOrExit(cmd, deps, "knowledge.unlink")
			if err != nil {
				return err
			}
			ctx := client.WithWorkspace(cmd.Context(), workspaceID)
			if !in.ClearFeatureLink && !in.ClearRequestLink {
				payload := output.ErrorPayload{Code: "validation", Message: "pass --feature, --request, or both"}
				code := deps.Printer.Error("knowledge.unlink", payload)
				return &output.ExitError{Code: code}
			}
			if in.UploadedBy == "" {
				in.UploadedBy = currentActor(deps)
			}
			in.WorkspaceID = workspaceID
			doc, err := deps.Client.CurateKnowledgeLinks(ctx, args[0], in)
			if err != nil {
				return apiExitError(deps, "knowledge.unlink", err)
			}
			printKnowledgeCurationResult(deps, "knowledge.unlink", doc, "Unlinked")
			return nil
		},
	}
	cmd.Flags().BoolVar(&in.ClearFeatureLink, "feature", false, "Clear the feature link")
	cmd.Flags().BoolVar(&in.ClearRequestLink, "request", false, "Clear the work item link")
	cmd.Flags().StringVar(&in.Version, "version", "", "Source document version (default latest)")
	cmd.Flags().StringVar(&in.Notes, "notes", "", "Optional curator notes for the new version")
	cmd.Flags().StringVar(&in.ActorRole, "actor-role", "", "Role of the curator")
	return cmd
}

func printKnowledgeCurationResult(deps *Deps, commandName string, doc *client.KnowledgeDocument, verb string) {
	if deps.Printer.Mode() == output.ModeJSON {
		deps.Printer.Success(commandName, doc)
		return
	}
	fmt.Fprintf(deps.Stdout, "%s %s %s", styled(deps, output.StyleSuccess, verb), styled(deps, output.StyleBold, doc.DocumentID), doc.Version)
	if doc.ParentVersion != "" {
		fmt.Fprintf(deps.Stdout, " (from %s)", doc.ParentVersion)
	}
	fmt.Fprintln(deps.Stdout)
}

func selectedWorkspaceIDOrExit(cmd *cobra.Command, deps *Deps, op string) (string, error) {
	workspaceID, err := currentWorkspaceID(cmd.Context(), deps)
	if err != nil {
		return "", apiExitError(deps, op, err)
	}
	if workspaceID == "" {
		payload := output.ErrorPayload{Code: "validation", Message: "select a workspace first with `specgate workspace select` or bind this repo with `specgate workspace bind`"}
		code := deps.Printer.Error(op, payload)
		return "", &output.ExitError{Code: code}
	}
	return workspaceID, nil
}

func readKnowledgeTextInput(deps *Deps, filePath string) (string, error) {
	var reader io.Reader
	if filePath == "-" {
		reader = deps.Stdin
	} else {
		file, err := os.Open(filePath)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", filePath, err)
		}
		defer file.Close()
		reader = file
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxKnowledgeTextBytes+1))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", filePath, err)
	}
	if len(data) > maxKnowledgeTextBytes {
		return "", fmt.Errorf("%s exceeds the 10 MiB Knowledge text limit; use a smaller file", filePath)
	}
	content := string(data)
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("%s is empty", filePath)
	}
	return content, nil
}

func oneLineSnippet(text string, limit int) string {
	snippet := strings.Join(strings.Fields(text), " ")
	if limit <= 0 || len(snippet) <= limit {
		return snippet
	}
	if limit <= 3 {
		return snippet[:limit]
	}
	return strings.TrimSpace(snippet[:limit-3]) + "..."
}
