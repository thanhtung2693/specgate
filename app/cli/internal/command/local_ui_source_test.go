package command

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func sourceFixture(t *testing.T, path string, line, end any) (*localUIServer, string) {
	t.Helper()
	return sourceFixtureKind(t, "file", path, line, end)
}
func sourceFixtureKind(t *testing.T, kind, path string, line, end any) (*localUIServer, string) {
	t.Helper()
	s, w := localUITestServer(t)
	evidence := map[string]any{"kind": kind, "path": path}
	if line != nil {
		evidence["line"] = line
	}
	if end != nil {
		evidence["end_line"] = end
	}
	body := map[string]any{"context_digest": w.ContextDigest, "agent": map[string]any{"name": "builder"}, "criteria": []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": evidence}}}
	if _, err := s.store.SubmitDelivery(t.Context(), s.selection.Workspace.ID, w.Key, body); err != nil {
		t.Fatal(err)
	}
	return s, w.Key
}
func getSource(t *testing.T, s *localUIServer, ref string) (int, localUISourceView) {
	t.Helper()
	response := localUIRequest(s, "GET", "/api/source?ref="+url.QueryEscape(ref)+"&criterion=local-1", "", true, false)
	var envelope struct {
		Data localUISourceView `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return response.Code, envelope.Data
}
func TestLocalUISourceReadsActualBoundedLinesAndRange(t *testing.T) {
	s, ref := sourceFixture(t, "proof.go", 8, 10)
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, fmt.Sprintf("original line %d", i))
	}
	if err := os.WriteFile(filepath.Join(s.repoRoot, "proof.go"), []byte(strings.Join(lines, "\n")), 0600); err != nil {
		t.Fatal(err)
	}
	code, view := getSource(t, s, ref)
	if code != 200 || !view.Source.Available || view.Origin != "current_checkout" || view.Source.StartLine != 3 || view.Source.HighlightStart != 8 || view.Source.HighlightEnd != 10 || len(view.Source.Lines) != 13 {
		t.Fatalf("source: %d %+v", code, view)
	}
	if view.Source.Lines[5].Text != "original line 8" || view.Source.Lines[5].Number != 8 {
		t.Fatalf("fabricated source: %+v", view.Source.Lines)
	}
	if view.Diff.Available || view.Diff.Text != "" {
		t.Fatalf("invented diff: %+v", view.Diff)
	}
}
func TestLocalUISourceRejectsUnsafeOrUnavailableEvidence(t *testing.T) {
	cases := []struct {
		name, path string
		line, end  any
		content    []byte
		reason     string
	}{
		{"traversal", "../outside.go", 1, nil, []byte("outside"), "unsafe_path"},
		{"absolute", "/tmp/outside.go", 1, nil, nil, "unsafe_path"},
		{"backslash", "..\\outside.go", 1, nil, nil, "unsafe_path"},
		{"git", ".git/config", 1, nil, nil, "sensitive_path"},
		{"env", ".env.local", 1, nil, nil, "sensitive_path"},
		{"private key", "id_rsa", 1, nil, nil, "sensitive_path"},
		{"local store", ".specgate/local/state.db", 1, nil, nil, "sensitive_path"},
		{"binary", "proof.bin", 1, nil, []byte{'a', 0, 'b'}, "binary_or_invalid_utf8"},
		{"invalid utf8", "proof.txt", 1, nil, []byte{255, 254}, "binary_or_invalid_utf8"},
		{"oversize", "proof.txt", 1, nil, []byte(strings.Repeat("a", (1<<20)+1)), "file_too_large"},
		{"key content", "proof.txt", 1, nil, []byte("-----BEGIN PRIVATE KEY-----\nsecret"), "sensitive_content"},
		{"missing line", "proof.txt", nil, nil, []byte("actual\nsource"), "citation_unavailable"},
		{"string line", "proof.txt", "one", nil, []byte("actual"), "citation_unavailable"},
		{"fractional line", "proof.txt", 1.5, nil, []byte("actual"), "citation_unavailable"},
		{"zero line", "proof.txt", 0, nil, []byte("actual"), "citation_unavailable"},
		{"past end", "proof.txt", 20, nil, []byte("actual"), "citation_out_of_range"},
		{"reverse range", "proof.txt", 2, 1, []byte("a\nb"), "citation_unavailable"},
		{"past end range", "proof.txt", 1, 5, []byte("a\nb"), "citation_out_of_range"},
		{"missing file", "missing.txt", 1, nil, nil, "file_unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, ref := sourceFixtureKind(t, "test", tc.path, tc.line, tc.end)
			if tc.content != nil && !strings.Contains(tc.path, "..") && !filepath.IsAbs(tc.path) {
				if err := os.WriteFile(filepath.Join(s.repoRoot, tc.path), tc.content, 0600); err != nil {
					t.Fatal(err)
				}
			}
			code, view := getSource(t, s, ref)
			if code != 200 || view.Source.Available || view.Source.Reason != tc.reason || len(view.Source.Lines) != 0 || view.Diff.Available || view.Diff.Text != "" {
				t.Fatalf("%d %+v", code, view)
			}
		})
	}
}
func TestLocalUISourceEndpointScopeAndSecurity(t *testing.T) {
	s, ref := sourceFixture(t, "proof.txt", 1, nil)
	for _, tc := range []struct {
		path    string
		session bool
		code    int
	}{
		{"/api/source?ref=" + ref + "&criterion=local-1", false, 403},
		{"/api/source?ref=unknown&criterion=local-1", true, 404},
		{"/api/source?ref=" + ref + "&criterion=local-2", true, 404},
		{"/api/source?ref=" + ref + "&criterion=local-1&path=.env", true, 400},
		{"/api/source?ref=" + ref + "&criterion=local-1&rev=HEAD~1", true, 400},
		{"/api/source?ref=" + ref + "&criterion=local-1&criterion=local-2", true, 400},
	} {
		w := localUIRequest(s, "GET", tc.path, "", tc.session, false)
		if w.Code != tc.code {
			t.Fatalf("%s => %d %s", tc.path, w.Code, w.Body.String())
		}
	}
	req := httptest.NewRequest("GET", "http://"+s.host+"/api/source?ref="+ref+"&criterion=local-1", nil)
	req.Host = "attacker.example"
	out := httptest.NewRecorder()
	s.ServeHTTP(out, req)
	if out.Code != 403 {
		t.Fatal("Host bypass")
	}
}
func sourceGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, data)
	}
	return strings.TrimSpace(string(data))
}
func TestLocalUISourceActualTrackedDiffAndUntrackedSource(t *testing.T) {
	for _, kind := range []string{"file", "test", "code"} {
		t.Run(kind, func(t *testing.T) {
			testLocalUISourceTrackedDiffAndUntrackedSource(t, kind)
		})
	}
}

func testLocalUISourceTrackedDiffAndUntrackedSource(t *testing.T, kind string) {
	t.Helper()
	s, ref := sourceFixtureKind(t, kind, "proof.txt", 1, nil)
	sourceGit(t, s.repoRoot, "init", "-q")
	sourceGit(t, s.repoRoot, "config", "user.name", "Fixture")
	sourceGit(t, s.repoRoot, "config", "user.email", "fixture@example.invalid")
	file := filepath.Join(s.repoRoot, "proof.txt")
	os.WriteFile(file, []byte("before\n"), 0600)
	sourceGit(t, s.repoRoot, "add", "--", "proof.txt")
	sourceGit(t, s.repoRoot, "commit", "-qm", "fixture")
	head := sourceGit(t, s.repoRoot, "rev-parse", "HEAD")
	os.WriteFile(file, []byte("after\n"), 0600)
	_, view := getSource(t, s, ref)
	if !view.Source.Available || len(view.Source.Lines) != 1 || view.Source.Lines[0].Text != "after" || view.Source.HighlightStart != 1 ||
		!view.Diff.Available || view.HeadRevision != head || !strings.Contains(view.Diff.Text, "-before") || !strings.Contains(view.Diff.Text, "+after") {
		t.Fatalf("actual diff missing: %+v", view)
	}
	os.Remove(file)
	os.WriteFile(file, []byte("new untracked\n"), 0600)
	sourceGit(t, s.repoRoot, "rm", "--cached", "--", "proof.txt")
	_, view = getSource(t, s, ref)
	if !view.Source.Available || view.Diff.Available || view.Diff.Reason != "untracked" || view.Diff.Text != "" {
		t.Fatalf("untracked %+v", view)
	}
}

func TestLocalUISourceDiffRejectsDirectoryCitations(t *testing.T) {
	for _, replaceWithFile := range []bool{false, true} {
		t.Run(fmt.Sprintf("index_has_exact_file_%t", replaceWithFile), func(t *testing.T) {
			s, ref := sourceFixture(t, "proof", 1, nil)
			sourceGit(t, s.repoRoot, "init", "-q")
			sourceGit(t, s.repoRoot, "config", "user.name", "Fixture")
			sourceGit(t, s.repoRoot, "config", "user.email", "fixture@example.invalid")
			dir := filepath.Join(s.repoRoot, "proof")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			child := filepath.Join(dir, ".env.fixture")
			if err := os.WriteFile(child, []byte("synthetic fixture content\n"), 0600); err != nil {
				t.Fatal(err)
			}
			sourceGit(t, s.repoRoot, "add", "--all")
			sourceGit(t, s.repoRoot, "commit", "-qm", "fixture")
			if err := os.Remove(child); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(dir); err != nil {
				t.Fatal(err)
			}
			if replaceWithFile {
				if err := os.WriteFile(dir, []byte("regular replacement\n"), 0600); err != nil {
					t.Fatal(err)
				}
				sourceGit(t, s.repoRoot, "add", "--all")
			}
			_, view := getSource(t, s, ref)
			if view.Diff.Available || view.Diff.Text != "" {
				t.Error("directory citation exposed a subtree diff")
			}
		})
	}
}

func TestLocalUISourceDiffKeepsExactDeletedFiles(t *testing.T) {
	s, ref := sourceFixture(t, "proof.txt", 1, nil)
	sourceGit(t, s.repoRoot, "init", "-q")
	sourceGit(t, s.repoRoot, "config", "user.name", "Fixture")
	sourceGit(t, s.repoRoot, "config", "user.email", "fixture@example.invalid")
	file := filepath.Join(s.repoRoot, "proof.txt")
	if err := os.WriteFile(file, []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sourceGit(t, s.repoRoot, "add", "--", "proof.txt")
	sourceGit(t, s.repoRoot, "commit", "-qm", "fixture")
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	for _, staged := range []bool{false, true} {
		if staged {
			sourceGit(t, s.repoRoot, "add", "-u", "--", "proof.txt")
		}
		_, view := getSource(t, s, ref)
		if view.Source.Available || !view.Diff.Available || !strings.Contains(view.Diff.Text, "-before") {
			t.Errorf("staged=%t: deleted file diff missing: %+v", staged, view.Diff)
		}
	}
}

func TestLocalUISourceRejectsLinksAndNonregularFiles(t *testing.T) {
	for _, kind := range []string{"outside_link", "secret_link", "directory"} {
		t.Run(kind, func(t *testing.T) {
			s, ref := sourceFixtureKind(t, "code", "proof.txt", 1, nil)
			target := filepath.Join(s.repoRoot, "proof.txt")
			wantReason := "symlink_not_allowed"
			switch kind {
			case "outside_link":
				outside := filepath.Join(t.TempDir(), "secret.txt")
				if err := os.WriteFile(outside, []byte("outside secret"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, target); err != nil {
					t.Fatal(err)
				}
			case "secret_link":
				if err := os.WriteFile(filepath.Join(s.repoRoot, ".env"), []byte("credential"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(".env", target); err != nil {
					t.Fatal(err)
				}
			case "directory":
				wantReason = "not_regular_file"
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
			}
			_, view := getSource(t, s, ref)
			if view.Source.Available || len(view.Source.Lines) != 0 || view.Source.Reason != wantReason || view.Diff.Available || view.HeadRevision != "" {
				t.Fatalf("unsafe source exposed: %+v", view)
			}
		})
	}
}

func TestLocalUISourceBoundsLargeRangesAndIndividualLines(t *testing.T) {
	for _, content := range []string{strings.Repeat("bounded row\n", 300), strings.Repeat("large text", 20000)} {
		end := any(nil)
		if strings.Contains(content, "\n") {
			end = 250
		}
		s, ref := sourceFixture(t, "proof.txt", 1, end)
		if err := os.WriteFile(filepath.Join(s.repoRoot, "proof.txt"), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		_, view := getSource(t, s, ref)
		size := 0
		for _, line := range view.Source.Lines {
			size += len(line.Text)
		}
		if !view.Source.Available || !view.Source.Truncated || len(view.Source.Lines) > 120 || size > 64<<10 {
			t.Fatalf("unbounded source: rows=%d bytes=%d %+v", len(view.Source.Lines), size, view.Source)
		}
	}
}

func TestLocalUISourceLiteralDiffDoesNotRunGitHelpers(t *testing.T) {
	s, ref := sourceFixture(t, "proof[1].txt", 1, nil)
	sourceGit(t, s.repoRoot, "init", "-q")
	sourceGit(t, s.repoRoot, "config", "user.name", "Fixture")
	sourceGit(t, s.repoRoot, "config", "user.email", "fixture@example.invalid")
	for _, file := range []string{"proof[1].txt", "proof1.txt"} {
		if err := os.WriteFile(filepath.Join(s.repoRoot, file), []byte("before\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sourceGit(t, s.repoRoot, "add", "--all")
	sourceGit(t, s.repoRoot, "commit", "-qm", "fixture")
	marker := filepath.Join(t.TempDir(), "helper-ran")
	helper := filepath.Join(t.TempDir(), "helper.sh")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nprintf executed > '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	sourceGit(t, s.repoRoot, "config", "diff.external", helper)
	sourceGit(t, s.repoRoot, "config", "diff.custom.textconv", helper)
	sourceGit(t, s.repoRoot, "config", "core.fsmonitor", helper)
	t.Setenv("GIT_EXTERNAL_DIFF", helper)
	t.Setenv("GIT_DIR", t.TempDir())
	if err := os.WriteFile(filepath.Join(s.repoRoot, ".gitattributes"), []byte("*.txt diff=custom\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.repoRoot, "proof[1].txt"), []byte("selected actual change\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.repoRoot, "proof1.txt"), []byte("unrelated secret diff\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, view := getSource(t, s, ref)
	if !view.Diff.Available || !strings.Contains(view.Diff.Text, "+selected actual change") || strings.Contains(view.Diff.Text, "unrelated secret") {
		t.Fatalf("literal diff: %+v", view.Diff)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("Git helper ran: %v", err)
	}
}

func TestLocalUISourceDiffUsesRawCheckoutWithoutGitFilters(t *testing.T) {
	s, ref := sourceFixture(t, "proof.txt", 1, nil)
	sourceGit(t, s.repoRoot, "init", "-q")
	sourceGit(t, s.repoRoot, "config", "user.name", "Fixture")
	sourceGit(t, s.repoRoot, "config", "user.email", "fixture@example.invalid")
	file := filepath.Join(s.repoRoot, "proof.txt")
	if err := os.WriteFile(file, []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sourceGit(t, s.repoRoot, "add", "--", "proof.txt")
	sourceGit(t, s.repoRoot, "commit", "-qm", "fixture")
	sourceGit(t, s.repoRoot, "config", "filter.fixture.clean", "printf 'filtered checkout text\\n'")
	if err := os.WriteFile(filepath.Join(s.repoRoot, ".gitattributes"), []byte("*.txt filter=fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("actual checkout text\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, view := getSource(t, s, ref)
	if !view.Diff.Available || !strings.Contains(view.Diff.Text, "+actual checkout text") || strings.Contains(view.Diff.Text, "filtered checkout text") {
		t.Fatal("diff did not use the raw checkout bytes")
	}
}

func TestLocalUISourceDiffLimitsDenseOrOversizedInputs(t *testing.T) {
	for _, tc := range []struct{ name, base, checkout, reason string }{
		{"dense changes", strings.Repeat("a\n", 16385), strings.Repeat("b\n", 16384), "diff_too_large"},
		{"dense unchanged", strings.Repeat("a\n", 32769), strings.Repeat("a\n", 32769), ""},
		{"oversized HEAD blob", strings.Repeat("x", (1<<20)+1), "current\n", "file_too_large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, ref := sourceFixture(t, "proof.txt", 1, nil)
			sourceGit(t, s.repoRoot, "init", "-q")
			sourceGit(t, s.repoRoot, "config", "user.name", "Fixture")
			sourceGit(t, s.repoRoot, "config", "user.email", "fixture@example.invalid")
			file := filepath.Join(s.repoRoot, "proof.txt")
			if err := os.WriteFile(file, []byte(tc.base), 0600); err != nil {
				t.Fatal(err)
			}
			sourceGit(t, s.repoRoot, "add", "--", "proof.txt")
			sourceGit(t, s.repoRoot, "commit", "-qm", "fixture")
			if err := os.WriteFile(file, []byte(tc.checkout), 0600); err != nil {
				t.Fatal(err)
			}
			_, view := getSource(t, s, ref)
			if !view.Source.Available || view.Source.Lines[0].Text != strings.SplitN(tc.checkout, "\n", 2)[0] {
				t.Fatal("bounded source excerpt unavailable")
			}
			if view.Diff.Available != (tc.reason == "") || view.Diff.Reason != tc.reason || view.Diff.Text != "" {
				t.Errorf("diff available=%t reason=%q bytes=%d", view.Diff.Available, view.Diff.Reason, len(view.Diff.Text))
			}
		})
	}
}

func TestLocalUISourceRejectsUnsupportedEvidenceKind(t *testing.T) {
	for _, kind := range []string{"command", "url", "Test", ""} {
		t.Run(kind, func(t *testing.T) {
			s, ref := sourceFixtureKind(t, kind, "proof.txt", 1, nil)
			if err := os.WriteFile(filepath.Join(s.repoRoot, "proof.txt"), []byte("fixture content\n"), 0600); err != nil {
				t.Fatal(err)
			}
			_, view := getSource(t, s, ref)
			if view.Source.Available || view.Source.Reason != "unsupported_evidence_kind" || view.Diff.Reason != "unsupported_evidence_kind" || view.HeadRevision != "" {
				t.Fatalf("unsupported evidence inspected: %+v", view)
			}
		})
	}
}

func TestLocalUISourceDetectsFileReplacementAndMutation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "proof.txt")
	if err := os.WriteFile(file, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if !localUISourceStableFile(original, original) {
		t.Fatal("stable file rejected")
	}
	if err := os.WriteFile(file, []byte("changed size"), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if localUISourceStableFile(original, changed) {
		t.Fatal("modified file accepted")
	}
	replacement := filepath.Join(t.TempDir(), "replacement.txt")
	if err := os.WriteFile(replacement, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	replaced, err := os.Stat(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if localUISourceStableFile(original, replaced) {
		t.Fatal("replaced inode accepted")
	}
}

func TestLocalUISourceKeepsRealDiffWhenCitedLinesDisappear(t *testing.T) {
	for _, deleted := range []bool{true, false} {
		t.Run(fmt.Sprintf("deleted=%v", deleted), func(t *testing.T) {
			s, ref := sourceFixture(t, "proof.txt", 2, nil)
			sourceGit(t, s.repoRoot, "init", "-q")
			sourceGit(t, s.repoRoot, "config", "user.name", "Fixture")
			sourceGit(t, s.repoRoot, "config", "user.email", "fixture@example.invalid")
			file := filepath.Join(s.repoRoot, "proof.txt")
			if err := os.WriteFile(file, []byte("before\ncited line\n"), 0600); err != nil {
				t.Fatal(err)
			}
			sourceGit(t, s.repoRoot, "add", "--", "proof.txt")
			sourceGit(t, s.repoRoot, "commit", "-qm", "fixture")
			if deleted {
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(file, []byte("replacement\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, view := getSource(t, s, ref)
			if view.Source.Available || !view.Diff.Available || !strings.Contains(view.Diff.Text, "-cited line") {
				t.Fatalf("deleted citation lost actual diff: %+v", view)
			}
		})
	}
}

func TestLocalUISourceNamesExactLatestCompletionForCitation(t *testing.T) {
	s, ref := sourceFixture(t, "old.txt", 1, nil)
	work, err := s.store.GetWork(t.Context(), s.selection.Workspace.ID, ref)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"old.txt": "old citation", "new.txt": "new citation"} {
		if err := os.WriteFile(filepath.Join(s.repoRoot, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	report := map[string]any{"context_digest": work.ContextDigest, "agent": map[string]any{"name": "builder"}, "criteria": []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"kind": "file", "path": "new.txt", "line": 1}}}}
	latest, err := s.store.SubmitDelivery(t.Context(), s.selection.Workspace.ID, ref, report)
	if err != nil {
		t.Fatal(err)
	}
	_, view := getSource(t, s, ref)
	if view.CompletionID != latest.ReportID || view.Path != "new.txt" || !view.Source.Available || view.Source.Lines[0].Text != "new citation" {
		t.Fatalf("mixed report attribution: %+v latest=%+v", view, latest)
	}
}

func TestLocalUISourceBoundsActualDiffOutput(t *testing.T) {
	s, ref := sourceFixture(t, "proof.txt", 1, nil)
	sourceGit(t, s.repoRoot, "init", "-q")
	sourceGit(t, s.repoRoot, "config", "user.name", "Fixture")
	sourceGit(t, s.repoRoot, "config", "user.email", "fixture@example.invalid")
	file := filepath.Join(s.repoRoot, "proof.txt")
	if err := os.WriteFile(file, []byte(strings.Repeat("before "+strings.Repeat("a", 100)+"\n", 1500)), 0600); err != nil {
		t.Fatal(err)
	}
	sourceGit(t, s.repoRoot, "add", "--", "proof.txt")
	sourceGit(t, s.repoRoot, "commit", "-qm", "fixture")
	if err := os.WriteFile(file, []byte(strings.Repeat("after "+strings.Repeat("b", 100)+"\n", 1500)), 0600); err != nil {
		t.Fatal(err)
	}
	_, view := getSource(t, s, ref)
	if !view.Diff.Available || !view.Diff.Truncated || len(view.Diff.Text) > 64<<10 {
		t.Fatalf("unbounded diff: available=%v truncated=%v size=%d reason=%s", view.Diff.Available, view.Diff.Truncated, len(view.Diff.Text), view.Diff.Reason)
	}
}

func TestLocalUISourceAllowsCitedWorkEvidenceWithoutWritingRecords(t *testing.T) {
	s, ref := sourceFixture(t, ".specgate/work/verification.json", 1, nil)
	file := filepath.Join(s.repoRoot, ".specgate", "work", "verification.json")
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("{\"checks\": []}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := s.store.Audit(t.Context(), s.selection.Workspace.ID, ref)
	if err != nil {
		t.Fatal(err)
	}
	_, view := getSource(t, s, ref)
	after, err := s.store.Audit(t.Context(), s.selection.Workspace.ID, ref)
	if err != nil {
		t.Fatal(err)
	}
	if !view.Source.Available || view.Source.Lines[0].Text != "{\"checks\": []}" || len(before) != len(after) {
		t.Fatalf("work evidence unavailable or read wrote records: %+v", view)
	}
}
