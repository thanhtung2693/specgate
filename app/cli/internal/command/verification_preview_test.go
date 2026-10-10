package command_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/local"
)

func TestVerificationHumanPreviewIncludesSelectorsAndWatchedHashes(t *testing.T) {
	for _, dryRun := range []bool{true, false} {
		t.Run(fmt.Sprint(dryRun), func(t *testing.T) {
			deps, _, prompt, out := newFakeDeps(t)
			dir, store, sel, _ := newLocalChangeWork(t, deps)
			work, err := store.CreateQuickWork(t.Context(), sel.Workspace.ID, local.QuickWorkInput{Title: "preview", AcceptanceCriteria: []string{"Works @check:unit"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(deps.WorkingDir, ".git"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(deps.WorkingDir, "test.txt"), []byte("reviewed"), 0600); err != nil {
				t.Fatal(err)
			}
			file := writeDeliveryJSON(t, map[string]any{"context_digest": work.ContextDigest, "shell": "sh", "watched_paths": []string{"test.txt"}, "checks": []any{map[string]any{"name": "unit", "command": "true", "cwd": ".", "test_report": map[string]any{"format": "junit", "selectors": map[string]any{"local-1": []any{map[string]any{"classname": "pkg", "name": "works"}}}}}}})
			closeLocalChangeStore(t, deps, dir, store)
			assertPreview := func() {
				t.Helper()
				for _, want := range []string{"local-1", "pkg", "works", "test.txt", fmt.Sprintf("%x", sha256.Sum256([]byte("reviewed")))} {
					if !strings.Contains(out.String(), want) {
						t.Fatalf("preview omitted %s: %s", want, out.String())
					}
				}
			}
			args := []string{"work", "verification", work.Key, "--file", file}
			if dryRun {
				args = append(args, "--dry-run")
			} else {
				deps.StdinIsTTY = func() bool { return true }
				prompt.confirmObserver = assertPreview
				prompt.confirmValue = true
			}
			if code := command.ExecuteForCode(command.NewRootCommand(deps), args...); code != 0 {
				t.Fatalf("preview exit %d: %s", code, out.String())
			}
			assertPreview()
		})
	}
}
