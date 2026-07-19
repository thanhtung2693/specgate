package command_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/client"
	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/output"
)

func TestKnowledgeSearchUsesSelectedWorkspace(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	if err := (config.Config{
		Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	fc.knowledgeSearchResult = []client.KnowledgeSearchResult{{
		DocumentID:  "doc-1",
		Version:     "v1",
		Title:       "Trust model",
		Score:       0.87,
		ContextText: "Deterministic evidence requires a human-authored check binding.",
	}}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "knowledge", "search", "deterministic evidence", "--limit", "3", "--context", "section")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastKnowledgeSearchInput.WorkspaceID != "ws-1" {
		t.Fatalf("workspace = %q, want ws-1", fc.lastKnowledgeSearchInput.WorkspaceID)
	}
	if fc.lastKnowledgeSearchInput.Query != "deterministic evidence" || fc.lastKnowledgeSearchInput.MaxChunks != 3 || fc.lastKnowledgeSearchInput.ContextMode != "section" {
		t.Fatalf("search input = %#v", fc.lastKnowledgeSearchInput)
	}
	if !strings.Contains(out.String(), "Trust model") {
		t.Fatalf("human output missing result title:\n%s", out.String())
	}
}

func TestKnowledgeListExplainsPagination(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	if err := (config.Config{Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	fc.knowledgeListResult = &client.KnowledgeDocumentList{
		Items: []client.KnowledgeDocument{{DocumentID: "doc-1", Version: "v1", Status: "indexed", Title: "Trust model"}},
		Total: 3,
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "knowledge", "list", "--limit", "1")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "Showing 1 of 3") || !strings.Contains(out.String(), "--offset 1") {
		t.Fatalf("pagination guidance missing:\n%s", out.String())
	}
}

func TestKnowledgeListStylesRichTerminalButNotRedirectedOutput(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	deps, fc, _, out := newFakeDeps(t)
	deps.StdoutIsTTY = func() bool { return true }
	if err := (config.Config{Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	fc.knowledgeListResult = &client.KnowledgeDocumentList{Items: []client.KnowledgeDocument{{DocumentID: "doc-1", Version: "v1", Status: "indexed", Title: "Trust model"}}}

	if code := command.ExecuteForCode(command.NewRootCommand(deps), "knowledge", "list"); code != output.ExitOK {
		t.Fatalf("rich exit = %d, output = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("rich knowledge list has no ANSI styling: %q", out.String())
	}

	out.Reset()
	deps.StdoutIsTTY = func() bool { return false }
	if code := command.ExecuteForCode(command.NewRootCommand(deps), "knowledge", "list"); code != output.ExitOK {
		t.Fatalf("portable exit = %d, output = %s", code, out.String())
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("portable knowledge list contains ANSI styling: %q", out.String())
	}
}

func TestKnowledgeAddTextReadsFileAndUsesActor(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	if err := (config.Config{
		CurrentUser: config.CurrentUser{Username: "tung"},
		Workspace:   config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"},
	}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("# Trust notes\nKeep evidence grounded."), 0o644); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "knowledge", "add-text",
		"--title", "Trust notes",
		"--file", path,
		"--type", "policy_doc",
		"--authority", "high",
		"--tag", "trust",
	)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	got := fc.lastKnowledgeCreateInput
	if got.WorkspaceID != "ws-1" || got.UploadedBy != "tung" {
		t.Fatalf("workspace/uploaded_by = %q/%q", got.WorkspaceID, got.UploadedBy)
	}
	if got.Title != "Trust notes" || got.DocumentType != "policy_doc" || got.AuthorityLevel != "high" {
		t.Fatalf("metadata = %#v", got)
	}
	if got.Content != "# Trust notes\nKeep evidence grounded." {
		t.Fatalf("content = %q", got.Content)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "trust" {
		t.Fatalf("tags = %#v", got.Tags)
	}
}

func TestKnowledgeAddTextRejectsOversizedInputBeforeRequest(t *testing.T) {
	const maxKnowledgeTextBytes = 10 << 20

	for _, source := range []string{"file", "stdin"} {
		t.Run(source, func(t *testing.T) {
			deps, fc, _, out := newFakeDeps(t)
			if err := (config.Config{
				Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"},
			}).SaveTo(deps.ConfigPath); err != nil {
				t.Fatal(err)
			}

			filePath := "-"
			oversized := bytes.Repeat([]byte("x"), maxKnowledgeTextBytes+1)
			if source == "file" {
				filePath = filepath.Join(t.TempDir(), "oversized.md")
				if err := os.WriteFile(filePath, oversized, 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				deps.Stdin = bytes.NewReader(oversized)
			}

			code := command.ExecuteForCode(command.NewRootCommand(deps), "--json", "knowledge", "add-text",
				"--title", "Oversized",
				"--file", filePath,
			)
			if code != output.ExitUsage {
				t.Fatalf("exit = %d, want usage; output = %s", code, out.String())
			}
			if !strings.Contains(out.String(), "10 MiB") {
				t.Fatalf("output missing input limit: %s", out.String())
			}
			if fc.lastKnowledgeCreateInput.Content != "" {
				t.Fatal("oversized input reached the Knowledge client")
			}
		})
	}
}

func TestKnowledgeLinkRequestResolvesWorkRef(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	fc.resolvedWork = &client.ResolvedWork{ChangeRequestID: "cr-uuid", ChangeRequestKey: "CR-123"}
	if err := (config.Config{CurrentUser: config.CurrentUser{Username: "curator"}, Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "knowledge", "link", "doc-1", "--request", "CR-123")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastWorkRef != "CR-123" {
		t.Fatalf("resolved ref = %q", fc.lastWorkRef)
	}
	if fc.lastKnowledgeCurateID != "doc-1" {
		t.Fatalf("document id = %q", fc.lastKnowledgeCurateID)
	}
	got := fc.lastKnowledgeCurateInput
	if got.WorkspaceID != "ws-1" || got.LinkedRequestID != "cr-uuid" || got.UploadedBy != "curator" {
		t.Fatalf("curate input = %#v", got)
	}
	if got.ClearFeatureLink || got.ClearRequestLink {
		t.Fatalf("link should not clear links: %#v", got)
	}
}

func TestKnowledgeUnlinkClearsSelectedLink(t *testing.T) {
	t.Parallel()
	deps, fc, _, out := newFakeDeps(t)
	if err := (config.Config{Workspace: config.CurrentWorkspace{ID: "ws-1", Slug: "platform", Name: "Platform"}}).SaveTo(deps.ConfigPath); err != nil {
		t.Fatal(err)
	}

	code := command.ExecuteForCode(command.NewRootCommand(deps), "--plain", "knowledge", "unlink", "doc-1", "--feature")
	if code != output.ExitOK {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
	if fc.lastKnowledgeCurateID != "doc-1" {
		t.Fatalf("document id = %q", fc.lastKnowledgeCurateID)
	}
	got := fc.lastKnowledgeCurateInput
	if got.WorkspaceID != "ws-1" || !got.ClearFeatureLink || got.ClearRequestLink {
		t.Fatalf("curate input = %#v", got)
	}
}
