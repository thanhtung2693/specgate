package command

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/specgate/specgate/app/cli/internal/deploy"
)

// gitReceipt is local repository identity and status metadata. It deliberately
// contains no patch text or file contents; the digest is computed locally.
type gitReceipt struct {
	Repository   string   `json:"repository"`
	Availability string   `json:"availability"`
	Branch       string   `json:"branch"`
	BaseRevision string   `json:"base_revision"`
	HeadRevision string   `json:"head_revision"`
	ChangedFiles []string `json:"changed_files"`
	DiffDigest   string   `json:"diff_digest"`
	Warnings     []string `json:"warnings"`
}

type gitStatusEntry struct {
	status string
	path   string
}

// collectGitReceipt gathers enough local Git identity to bind a delivery
// report to a checkout. A missing repository or origin is intentionally an
// unavailable receipt rather than a fabricated identity.
func collectGitReceipt(ctx context.Context, runner deploy.CommandRunner, dir string, reported []string) gitReceipt {
	return collectGitReceiptWithPriorBase(ctx, runner, dir, reported, "")
}

// collectGitReceiptWithPriorBase retains a prior receipt's base only when the
// current tracking base has caught up to HEAD and that prior base is still an
// ancestor. This preserves a delivered commit range after its branch is pushed.
func collectGitReceiptWithPriorBase(ctx context.Context, runner deploy.CommandRunner, dir string, reported []string, priorBase string) gitReceipt {
	receipt := gitReceipt{
		Availability: "unavailable",
		ChangedFiles: []string{},
		Warnings:     []string{},
	}
	if runner == nil {
		runner = deploy.ExecRunner{}
	}
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}

	root, err := gitOutput(ctx, runner, dir, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(string(root)) == "" {
		receipt.Warnings = append(receipt.Warnings, "Git receipt unavailable: working directory is not a Git repository")
		return receipt
	}
	repoRoot := filepath.Clean(strings.TrimSpace(string(root)))

	remote, err := gitOutput(ctx, runner, dir, "remote", "get-url", "origin")
	if err != nil || strings.TrimSpace(string(remote)) == "" {
		receipt.Warnings = append(receipt.Warnings, "Git receipt unavailable: origin remote is not configured")
		return receipt
	}
	receipt.Repository = strings.TrimSpace(string(remote))
	receipt.Availability = "available"

	if branch, err := gitOutput(ctx, runner, dir, "branch", "--show-current"); err == nil {
		receipt.Branch = strings.TrimSpace(string(branch))
	} else {
		receipt.Warnings = append(receipt.Warnings, "Git branch could not be determined")
	}
	if head, err := gitOutput(ctx, runner, dir, "rev-parse", "HEAD"); err == nil {
		receipt.HeadRevision = strings.TrimSpace(string(head))
	} else {
		receipt.Warnings = append(receipt.Warnings, "Git HEAD revision could not be determined")
	}

	status, err := gitOutput(ctx, runner, dir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	statusOK := err == nil
	statusEntries := []gitStatusEntry{}
	if err != nil {
		receipt.Warnings = append(receipt.Warnings, "Git status could not be determined")
	} else {
		statusEntries = parseGitStatus(status)
	}

	baseRef := "origin/HEAD"
	if receipt.Branch != "" {
		baseRef = "origin/" + receipt.Branch
	}
	base, baseErr := gitOutput(ctx, runner, dir, "merge-base", "HEAD", baseRef)
	if baseErr != nil && baseRef != "origin/HEAD" {
		// Feature branches commonly have no pushed origin/<branch>; the
		// origin/HEAD default branch is still a useful local base.
		base, baseErr = gitOutput(ctx, runner, dir, "merge-base", "HEAD", "origin/HEAD")
	}
	if baseErr == nil {
		receipt.BaseRevision = strings.TrimSpace(string(base))
		if receipt.BaseRevision == receipt.HeadRevision && receipt.HeadRevision != "" {
			if prior := strings.TrimSpace(priorBase); prior != "" && prior != receipt.HeadRevision {
				if _, err := gitOutput(ctx, runner, dir, "merge-base", "--is-ancestor", prior, receipt.HeadRevision); err == nil {
					receipt.BaseRevision = prior
				}
			}
		}
	} else {
		receipt.Warnings = append(receipt.Warnings, "Git base revision could not be determined")
	}
	committedEntries := []gitStatusEntry{}
	if receipt.BaseRevision != "" && receipt.HeadRevision != "" {
		committed, diffErr := gitOutput(ctx, runner, dir, "diff", "--name-only", receipt.BaseRevision, receipt.HeadRevision)
		if diffErr != nil {
			receipt.Warnings = append(receipt.Warnings, "Git committed file history could not be determined")
		} else {
			committedEntries = parseGitDiffNames(committed)
		}
	}
	allEntries := append(append([]gitStatusEntry{}, statusEntries...), committedEntries...)
	receipt.ChangedFiles = uniqueSortedStatusPaths(allEntries)
	// A nil scope means the caller is only collecting repository identity, as
	// the completion scaffold does before the user fills affected_files. An
	// explicit empty scope still participates in mismatch detection.
	if reported != nil {
		if mismatch := reportedFilesWarning(normalizeReportedPaths(reported, dir, repoRoot), receipt.ChangedFiles); mismatch != "" {
			receipt.Warnings = append(receipt.Warnings, mismatch)
		}
	}
	// Include identity in the digest even when status is clean. Recompute after
	// base revision collection so a checkout identity cannot collide silently.
	if statusOK {
		receipt.DiffDigest = gitStatusDigest(repoRoot, receipt.Repository, receipt.Branch, receipt.BaseRevision, receipt.HeadRevision, allEntries)
	}
	return receipt
}

// normalizeReportedPaths makes absolute paths and paths that resolve from the
// invocation directory comparable with Git's repository-root-relative status.
func normalizeReportedPaths(reported []string, dir, repoRoot string) []string {
	normalized := make([]string, 0, len(reported))
	for _, path := range reported {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if filepath.IsAbs(path) {
			if rel, err := filepath.Rel(repoRoot, path); err == nil {
				path = rel
			}
		} else if _, err := os.Stat(filepath.Join(dir, path)); err == nil {
			if rel, relErr := filepath.Rel(repoRoot, filepath.Join(dir, path)); relErr == nil {
				path = rel
			}
		}
		normalized = append(normalized, filepath.Clean(path))
	}
	return normalized
}

func gitOutput(ctx context.Context, runner deploy.CommandRunner, dir string, args ...string) ([]byte, error) {
	return runner.Output(ctx, "git", append([]string{"-C", dir}, args...)...)
}

func parseGitStatus(data []byte) []gitStatusEntry {
	parts := bytes.Split(data, []byte{0})
	entries := make([]gitStatusEntry, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		raw := string(parts[i])
		if raw == "" {
			continue
		}
		if len(raw) < 4 {
			continue
		}
		entry := gitStatusEntry{status: raw[:2], path: raw[3:]}
		entries = append(entries, entry)
		if (entry.status[0] == 'R' || entry.status[0] == 'C' || entry.status[1] == 'R' || entry.status[1] == 'C') && i+1 < len(parts) && len(parts[i+1]) > 0 {
			i++
			entries = append(entries, gitStatusEntry{status: entry.status, path: string(parts[i])})
		}
	}
	return entries
}

func parseGitDiffNames(data []byte) []gitStatusEntry {
	if strings.TrimSpace(string(data)) == "[]" {
		// The command-layer fake runner's default output; git emits an empty
		// stream when no names are selected.
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	entries := make([]gitStatusEntry, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if line != "" {
			entries = append(entries, gitStatusEntry{status: "committed", path: line})
		}
	}
	return entries
}

func uniqueSortedStatusPaths(entries []gitStatusEntry) []string {
	set := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.path != "" {
			set[entry.path] = struct{}{}
		}
	}
	paths := make([]string, 0, len(set))
	for path := range set {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func reportedFilesWarning(reported, actual []string) string {
	reported = uniqueSortedStatusPaths(func() []gitStatusEntry {
		entries := make([]gitStatusEntry, 0, len(reported))
		for _, path := range reported {
			entries = append(entries, gitStatusEntry{path: path})
		}
		return entries
	}())
	reportedOnly := pathDifference(reported, actual)
	checkoutOnly := pathDifference(actual, reported)
	if len(reportedOnly) == 0 && len(checkoutOnly) == 0 {
		return ""
	}
	if len(reportedOnly) == 0 {
		return fmt.Sprintf("Git checkout has unrelated changes outside reported delivery scope: %v", checkoutOnly)
	}
	if len(checkoutOnly) == 0 {
		return fmt.Sprintf("Git reported files are absent from checkout changes: %v", reportedOnly)
	}
	return fmt.Sprintf("Git delivery scope differs: reported-only=%v checkout-only=%v", reportedOnly, checkoutOnly)
}

func pathDifference(left, right []string) []string {
	set := make(map[string]struct{}, len(right))
	for _, path := range right {
		set[path] = struct{}{}
	}
	result := make([]string, 0)
	for _, path := range left {
		if _, ok := set[path]; !ok {
			result = append(result, path)
		}
	}
	return result
}

func gitStatusDigest(dir, repository, branch, base, head string, entries []gitStatusEntry) string {
	entries = append([]gitStatusEntry(nil), entries...)
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].path == entries[j].path {
			return entries[i].status < entries[j].status
		}
		return entries[i].path < entries[j].path
	})
	h := sha256.New()
	fmt.Fprintf(h, "repository\x00%s\x00branch\x00%s\x00base\x00%s\x00head\x00%s\n", repository, branch, base, head)
	for _, entry := range entries {
		path := filepath.Clean(entry.path)
		info, err := os.Stat(filepath.Join(dir, path))
		if err != nil {
			fmt.Fprintf(h, "%s\x00%s\x00missing\n", entry.status, path)
			continue
		}
		contentDigest := ""
		if data, readErr := os.ReadFile(filepath.Join(dir, path)); readErr == nil {
			contentDigest = fmt.Sprintf("sha256:%x", sha256.Sum256(data))
		}
		fmt.Fprintf(h, "%s\x00%s\x00%d\x00%d\x00%d\x00%s\n", entry.status, path, info.Size(), info.ModTime().UnixNano(), info.Mode(), contentDigest)
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil))
}
