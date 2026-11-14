package command

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/specgate/specgate/app/cli/internal/executil"
	"github.com/specgate/specgate/app/cli/internal/provenance"
)

type transferGit struct {
	Repository string `json:"repository,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Head       string `json:"head,omitempty"`
	Tree       string `json:"tree,omitempty"`
	Dirty      *bool  `json:"dirty,omitempty"`
}

func (g transferGit) valid() bool {
	return transferRepository(g.Repository) == g.Repository &&
		strings.IndexFunc(g.Branch, unicode.IsControl) < 0 &&
		((g.Head == "" && g.Tree == "") ||
			((validLowerHex(g.Head, 20) || validLowerHex(g.Head, 32)) &&
				len(g.Head) == len(g.Tree) && validLowerHex(g.Tree, len(g.Head)/2)))
}

// Only normalized network coordinates travel in a transfer. In particular,
// discard SSH usernames and local filesystem origins and never print raw URLs.
func transferRepository(origin string) string {
	safe := provenance.Repository(origin)
	if safe == "" || filepath.IsAbs(safe) {
		return ""
	}
	if !strings.Contains(safe, "://") {
		host, path, ok := strings.Cut(safe, ":")
		if !ok || path == "" || strings.ContainsAny(path, "?#") {
			return ""
		}
		if index := strings.LastIndexByte(host, '@'); index >= 0 {
			host = host[index+1:]
		}
		if !strings.Contains(host, ".") || strings.HasPrefix(host, ".") || strings.IndexFunc(host, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-')
		}) >= 0 {
			return ""
		}
		// Preserve SCP-style remote-home and absolute paths as distinct Git
		// identities. Mapping them to URLs would lose this distinction.
		return strings.ToLower(host) + ":" + path
	}
	u, err := url.Parse(safe)
	if err != nil || u.Hostname() == "" || u.Path == "" || u.Path == "/" || u.Scheme == "file" {
		return ""
	}
	u.User = nil
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	return u.String()
}

func transferGitCommand(ctx context.Context, deps *Deps, dir string, args ...string) (string, error) {
	runner := deps.CommandRunner
	if runner == nil {
		runner = executil.ExecRunner{}
	}
	result, err := gitOutput(ctx, runner, dir, args...)
	return strings.TrimSpace(string(result)), err
}

func captureTransferGit(ctx context.Context, deps *Deps) (transferGit, error) {
	dir := deps.WorkingDir
	if dir == "" {
		dir = "."
	}
	var g transferGit
	if root, err := transferGitCommand(ctx, deps, dir, "rev-parse", "--show-toplevel"); err == nil && root != "" {
		origin, _ := transferGitCommand(ctx, deps, root, "remote", "get-url", "origin")
		g.Repository = transferRepository(origin)
		g.Branch, _ = transferGitCommand(ctx, deps, root, "branch", "--show-current")
		head, _ := transferGitCommand(ctx, deps, root, "rev-parse", "--verify", "HEAD^{commit}")
		tree, _ := transferGitCommand(ctx, deps, root, "rev-parse", "--verify", "HEAD^{tree}")
		if head != "" && tree != "" {
			g.Head, g.Tree = head, tree
		}
		if status, err := transferGitCommand(ctx, deps, root, "-c", "core.fsmonitor=false", "--no-optional-locks", "status", "--porcelain=v1", "--untracked-files=normal"); err == nil {
			dirty := status != ""
			g.Dirty = &dirty
		}
	}
	return g, ctx.Err()
}

func verifyTransferRepository(ctx context.Context, deps *Deps, repo string, source transferGit) error {
	root, err := transferGitCommand(ctx, deps, repo, "rev-parse", "--show-toplevel")
	canonical, canonicalErr := filepath.EvalSymlinks(root)
	if err != nil || canonicalErr != nil || filepath.Clean(canonical) != repo {
		return fmt.Errorf("--repo must select the root of an existing Local Git checkout")
	}
	if source.Repository != "" {
		rawOrigin, _ := transferGitCommand(ctx, deps, repo, "remote", "get-url", "origin")
		if target := transferRepository(rawOrigin); target != source.Repository {
			return fmt.Errorf("receiving checkout has a missing or different normalized Git origin")
		}
		return nil
	}
	if source.Head == "" {
		return fmt.Errorf("snapshot has no usable origin or Git commit/tree to identify the receiving checkout")
	}
	tree, err := transferGitCommand(ctx, deps, repo, "rev-parse", "--verify", source.Head+"^{tree}")
	if err != nil || tree != source.Tree {
		return fmt.Errorf("receiving checkout lacks the recorded source Git commit/tree")
	}
	return nil
}
