package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aymanbagabas/go-udiff"

	"github.com/specgate/specgate/app/cli/internal/local"
)

const localUISourceFileLimit = 1 << 20
const localUISourceOutputLimit = 64 << 10
const localUISourceDiffLineLimit = 32 << 10

type localUISourceLine struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
}
type localUISourceExcerpt struct {
	Available      bool                `json:"available"`
	StartLine      int                 `json:"start_line"`
	HighlightStart int                 `json:"highlight_start"`
	HighlightEnd   int                 `json:"highlight_end"`
	Lines          []localUISourceLine `json:"lines"`
	Truncated      bool                `json:"truncated"`
	Reason         string              `json:"reason,omitempty"`
}
type localUISourceDiff struct {
	Available bool   `json:"available"`
	Base      string `json:"base"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
	Reason    string `json:"reason,omitempty"`
}
type localUISourceView struct {
	CompletionID string               `json:"completion_id,omitempty"`
	Path         string               `json:"path"`
	Citation     string               `json:"citation"`
	Origin       string               `json:"origin"`
	HeadRevision string               `json:"head_revision"`
	Source       localUISourceExcerpt `json:"source"`
	Diff         localUISourceDiff    `json:"diff"`
}

func (s *localUIServer) source(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	for key, values := range query {
		if (key != "ref" && key != "criterion") || len(values) != 1 {
			writeLocalUIError(w, 400, "validation_failed", "Select one work ref and canonical criterion; paths and revisions are not accepted")
			return
		}
	}
	ref, id := query.Get("ref"), query.Get("criterion")
	if ref == "" || id == "" {
		writeLocalUIError(w, 400, "validation_failed", "ref and criterion are required")
		return
	}
	work, err := s.store.GetWork(r.Context(), s.selection.Workspace.ID, ref)
	if err != nil {
		s.fail(w, err)
		return
	}
	known := false
	for index := range work.AcceptanceCriteria {
		if id == fmt.Sprintf("local-%d", index+1) {
			known = true
			break
		}
	}
	if !known {
		writeLocalUIError(w, 404, "not_found", "Criterion is not in this work's acceptance contract")
		return
	}
	view := localUISourceView{Origin: "current_checkout", Source: localUISourceExcerpt{Lines: []localUISourceLine{}}, Diff: localUISourceDiff{Base: "HEAD"}}
	report, err := s.store.LatestDeliveryReport(r.Context(), s.selection.Workspace.ID, ref)
	if errors.Is(err, local.ErrPreconditionNotMet) {
		view.unavailable("completion_unavailable")
		writeLocalUIJSON(w, 200, view)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	view.CompletionID = report.ID
	var evidence map[string]any
	rows, _ := report.Body["criteria"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if criterion, _ := row["criterion_id"].(string); criterion == id {
			evidence, _ = row["evidence"].(map[string]any)
		}
	}
	view.Path, _ = evidence["path"].(string)
	if kind, _ := evidence["kind"].(string); kind != "file" {
		view.unavailable("unsupported_evidence_kind")
		writeLocalUIJSON(w, 200, view)
		return
	}
	if reason := localUISourcePathReason(view.Path); reason != "" {
		view.unavailable(reason)
		writeLocalUIJSON(w, 200, view)
		return
	}
	first, last, ok := localUISourceCitation(evidence)
	if !ok {
		view.unavailable("citation_unavailable")
		writeLocalUIJSON(w, 200, view)
		return
	}
	view.Citation = "line: " + strconv.Itoa(first)
	if last != first {
		view.Citation += "-" + strconv.Itoa(last)
	}
	data, reason := s.readSourceFile(view.Path)
	if reason != "" {
		if reason == "file_missing" {
			view.unavailable("file_unavailable")
			view.HeadRevision, view.Diff = localUISourceGitDiff(r.Context(), s.repoRoot, view.Path, nil, true)
		} else {
			view.unavailable(reason)
		}
		writeLocalUIJSON(w, 200, view)
		return
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(data) == 0 || first > len(lines) || last > len(lines) {
		view.unavailable("citation_out_of_range")
		view.HeadRevision, view.Diff = localUISourceGitDiff(r.Context(), s.repoRoot, view.Path, data, false)
		writeLocalUIJSON(w, 200, view)
		return
	}
	start, end := max(1, first-5), min(len(lines), last+5)
	if end-start+1 > 120 {
		end = start + 119
		view.Source.Truncated = true
	}
	view.Source.Available = true
	view.Source.StartLine = start
	view.Source.HighlightStart = first
	view.Source.HighlightEnd = last
	remaining := localUISourceOutputLimit
	for number := start; number <= end; number++ {
		text := lines[number-1]
		if len(text) > remaining {
			text = localUISourceUTF8Prefix(text, remaining)
			view.Source.Truncated = true
		}
		view.Source.Lines = append(view.Source.Lines, localUISourceLine{Number: number, Text: text})
		remaining -= len(text)
		if remaining == 0 && number < end {
			view.Source.Truncated = true
			break
		}
	}
	view.HeadRevision, view.Diff = localUISourceGitDiff(r.Context(), s.repoRoot, view.Path, data, false)
	writeLocalUIJSON(w, 200, view)
}
func (v *localUISourceView) unavailable(reason string) {
	v.Source.Reason = reason
	v.Diff.Reason = reason
}
func localUISourceCitation(evidence map[string]any) (int, int, bool) {
	integer := func(value any) (int, bool) {
		number, ok := value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number < 1 || number > 1e9 || math.Trunc(number) != number {
			return 0, false
		}
		return int(number), true
	}
	first, ok := integer(evidence["line"])
	if !ok {
		return 0, 0, false
	}
	last := first
	if raw, exists := evidence["end_line"]; exists {
		var valid bool
		last, valid = integer(raw)
		if !valid || last < first {
			return 0, 0, false
		}
	}
	return first, last, true
}
func localUISourcePathReason(name string) string {
	if name == "" {
		return "citation_unavailable"
	}
	if len(name) > 4096 || !utf8.ValidString(name) || strings.ContainsAny(name, "\\:\x00") || path.IsAbs(name) || filepath.IsAbs(name) || path.Clean(name) != name || name == "." {
		return "unsafe_path"
	}
	parts := strings.Split(strings.ToLower(name), "/")
	for _, part := range parts {
		if part == ".." || part == "." {
			return "unsafe_path"
		}
		if part == ".git" || part == ".ssh" || part == ".aws" || part == ".kube" || strings.HasPrefix(part, ".env") || part == ".netrc" || part == ".npmrc" || part == ".pypirc" || part == "credentials" || part == "credentials.json" || strings.HasPrefix(part, "id_rsa") || strings.HasPrefix(part, "id_dsa") || strings.HasPrefix(part, "id_ecdsa") || strings.HasPrefix(part, "id_ed25519") {
			return "sensitive_path"
		}
	}
	for index, part := range parts {
		if part == ".specgate" && (index+1 >= len(parts) || parts[index+1] != "work") {
			return "sensitive_path"
		}
	}
	lower := strings.ToLower(name)
	for _, suffix := range []string{".pem", ".key", ".p12", ".pfx", ".sqlite", ".sqlite3", ".db", ".db-wal", ".db-shm"} {
		if strings.HasSuffix(lower, suffix) {
			return "sensitive_path"
		}
	}
	return ""
}
func (s *localUIServer) readSourceFile(name string) ([]byte, string) {
	root, err := os.OpenRoot(s.repoRoot)
	if err != nil {
		return nil, "file_unavailable"
	}
	defer root.Close()
	// Reject links before opening; os.Root also confines resolution during concurrent changes.
	var before os.FileInfo
	segments := strings.Split(name, "/")
	for index := range segments {
		info, err := root.Lstat(filepath.Join(segments[:index+1]...))
		if err != nil {
			if os.IsNotExist(err) {
				return nil, "file_missing"
			}
			return nil, "file_unavailable"
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, "symlink_not_allowed"
		}
		if index == len(segments)-1 && !info.Mode().IsRegular() {
			return nil, "not_regular_file"
		}
		if index == len(segments)-1 {
			before = info
		}
		if info.Size() > localUISourceFileLimit && index == len(segments)-1 {
			return nil, "file_too_large"
		}
	}
	file, err := root.Open(filepath.FromSlash(name))
	if err != nil {
		return nil, "file_unavailable"
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, "not_regular_file"
	}
	if !localUISourceStableFile(before, info) {
		return nil, "file_changed"
	}
	data, err := io.ReadAll(io.LimitReader(file, localUISourceFileLimit+1))
	if err != nil {
		return nil, "file_unavailable"
	}
	if len(data) > localUISourceFileLimit {
		return nil, "file_too_large"
	}
	after, statErr := file.Stat()
	if statErr != nil || !localUISourceStableFile(info, after) {
		return nil, "file_changed"
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return nil, "binary_or_invalid_utf8"
	}
	if bytes.Contains(data, []byte("PRIVATE KEY-----")) {
		return nil, "sensitive_content"
	}
	return data, ""
}
func localUISourceStableFile(before, after os.FileInfo) bool {
	return before != nil && after != nil && os.SameFile(before, after) && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime()) && before.Mode() == after.Mode()
}

func localUISourceUTF8Prefix(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}

type localUISourceOutput struct {
	data      []byte
	limit     int
	truncated bool
}

func (b *localUISourceOutput) Write(data []byte) (int, error) {
	remaining := max(0, b.limit-len(b.data))
	b.data = append(b.data, data[:min(len(data), remaining)]...)
	if len(data) > remaining {
		b.truncated = true
	}
	return len(data), nil
}
func runLocalUISourceGit(ctx context.Context, root string, limit int, args ...string) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	argv := []string{"--no-pager", "--no-replace-objects", "--literal-pathspecs", "-c", "core.fsmonitor=false", "--work-tree=" + root, "-C", root}
	argv = append(argv, args...)
	cmd := exec.CommandContext(ctx, "git", argv...)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_OPTIONAL_LOCKS=0", "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "LC_ALL=C")
	output := &localUISourceOutput{limit: limit}
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	err := cmd.Run()
	return output.data, output.truncated, err
}
func localUISourceGitDiff(ctx context.Context, root, name string, checkout []byte, missing bool) (string, localUISourceDiff) {
	view := localUISourceDiff{Base: "HEAD"}
	head, _, err := runLocalUISourceGit(ctx, root, 256, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		view.Reason = "head_unavailable"
		return "", view
	}
	revision := strings.TrimSpace(string(head))
	if len(revision) != 40 && len(revision) != 64 {
		view.Reason = "head_unavailable"
		return "", view
	}
	for _, r := range revision {
		if !strings.ContainsRune("0123456789abcdef", r) {
			view.Reason = "head_unavailable"
			return "", view
		}
	}
	// A literal pathspec can still select a subtree. Only an exact regular file qualifies.
	index, truncated, err := runLocalUISourceGit(ctx, root, len(name)+256, "ls-files", "--stage", "-z", "--", name)
	if err != nil || truncated {
		view.Reason = "diff_unavailable"
		return revision, view
	}
	if len(index) != 0 {
		fields := localUISourceGitEntry(index, name)
		if len(fields) != 3 || !localUISourceGitRegularMode(fields[0]) || fields[2] != "0" {
			view.Reason = "not_regular_file"
			return revision, view
		}
	} else if !missing {
		view.Reason = "untracked"
		return revision, view
	}

	tree, truncated, err := runLocalUISourceGit(ctx, root, len(name)+256, "ls-tree", "-z", revision, "--", name)
	if err != nil || truncated {
		view.Reason = "diff_unavailable"
		return revision, view
	}
	var base []byte
	if len(tree) != 0 {
		fields := localUISourceGitEntry(tree, name)
		if len(fields) != 3 || !localUISourceGitRegularMode(fields[0]) || fields[1] != "blob" {
			view.Reason = "not_regular_file"
			return revision, view
		}
		sizeOutput, sizeTruncated, sizeErr := runLocalUISourceGit(ctx, root, 32, "cat-file", "-s", fields[2])
		size, parseErr := strconv.ParseUint(strings.TrimSpace(string(sizeOutput)), 10, 64)
		if sizeErr != nil || sizeTruncated || parseErr != nil {
			view.Reason = "diff_unavailable"
			return revision, view
		}
		if size > localUISourceFileLimit {
			view.Reason = "file_too_large"
			return revision, view
		}
		base, truncated, err = runLocalUISourceGit(ctx, root, localUISourceFileLimit, "cat-file", "blob", fields[2])
		if err != nil || (!truncated && uint64(len(base)) != size) {
			view.Reason = "diff_unavailable"
			return revision, view
		}
		if truncated {
			view.Reason = "file_too_large"
			return revision, view
		}
	} else if len(index) == 0 {
		view.Reason = "untracked"
		return revision, view
	}
	if !utf8.Valid(base) || bytes.IndexByte(base, 0) >= 0 {
		view.Reason = "binary_or_invalid_utf8"
		return revision, view
	}
	if bytes.Contains(base, []byte("PRIVATE KEY-----")) {
		view.Reason = "sensitive_content"
		return revision, view
	}
	if bytes.Equal(base, checkout) {
		view.Available = true
		return revision, view
	}
	if localUISourceLineCount(base)+localUISourceLineCount(checkout) > localUISourceDiffLineLimit {
		view.Reason = "diff_too_large"
		return revision, view
	}
	// Comparing raw bytes avoids repository-configured clean filters.
	oldLabel, newLabel := "a/"+name, "b/"+name
	if strings.ContainsAny(name, "\r\n\t") {
		oldLabel, newLabel = strconv.Quote(oldLabel), strconv.Quote(newLabel)
	}
	if len(tree) == 0 {
		oldLabel = "/dev/null"
	}
	if missing {
		newLabel = "/dev/null"
	}
	before, after := string(base), string(checkout)
	diff, err := udiff.ToUnified(oldLabel, newLabel, before, udiff.Lines(before, after), udiff.DefaultContextLines)
	if err != nil {
		view.Reason = "diff_unavailable"
		return revision, view
	}
	view.Available = true
	view.Text = localUISourceUTF8Prefix(diff, localUISourceOutputLimit)
	view.Truncated = len(view.Text) != len(diff)
	return revision, view
}

func localUISourceLineCount(data []byte) int {
	lines := bytes.Count(data, []byte{'\n'})
	if len(data) != 0 && data[len(data)-1] != '\n' {
		lines++
	}
	return lines
}

func localUISourceGitEntry(output []byte, name string) []string {
	header, file, ok := strings.Cut(string(output), "\t")
	if !ok || file != name+"\x00" {
		return nil
	}
	return strings.Fields(header)
}

func localUISourceGitRegularMode(mode string) bool {
	return mode == "100644" || mode == "100755"
}
