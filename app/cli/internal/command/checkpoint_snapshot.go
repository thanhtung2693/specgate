package command

import (
	"context"
	"crypto/sha1" // Git's default object identity, not an authentication primitive.
	"crypto/sha256"
	"fmt"
	"github.com/specgate/specgate/app/cli/internal/deploy"
	"github.com/specgate/specgate/app/cli/internal/local"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func captureCheckout(ctx context.Context, deps *Deps) local.CheckoutSnapshot {
	s := local.CheckoutSnapshot{Version: 1, State: "unavailable", Files: map[string]string{}}
	runner := deps.DeployRunner
	if runner == nil {
		runner = deploy.ExecRunner{}
	}
	dir := deliveryWorkingDir(deps)
	out, err := gitOutput(ctx, runner, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		s.Reason = "Git checkout unavailable"
		return s
	}
	root, err := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	if err != nil {
		s.Reason = "checkout path unavailable"
		return s
	}
	identity := sha256.Sum256([]byte(root))
	s.CheckoutID = fmt.Sprintf("%x", identity)
	before := collectGitReceipt(ctx, runner, root, nil)
	s.Head = before.HeadRevision
	s.Fingerprint = before.DiffDigest
	if s.Head == "" || s.Fingerprint == "" {
		s.Reason = "HEAD or checkout fingerprint unavailable"
		return s
	}
	out, err = gitOutput(ctx, runner, root, "rev-parse", "HEAD^{tree}")
	if err != nil {
		s.Reason = "Git tree unavailable"
		return s
	}
	s.Tree = strings.TrimSpace(string(out))
	out, err = gitOutput(ctx, runner, root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		s.Reason = "Git status unavailable"
		return s
	}
	paths := uniqueSortedStatusPaths(parseGitStatus(out))
	if len(paths) > 10000 {
		s.Reason = "checkpoint exceeds 10000 dirty paths"
		return s
	}
	var total int64
	for _, path := range paths {
		value, size, err := checkoutBlob(root, path, len(s.Head) == 64)
		if err != nil {
			s.Reason = "cannot capture path: " + path
			s.Files = nil
			return s
		}
		total += size
		if total > 64<<20 {
			s.Reason = "checkpoint exceeds 64 MiB dirty content"
			s.Files = nil
			return s
		}
		s.Files[path] = value
	}
	after := collectGitReceipt(ctx, runner, root, nil)
	if before.DiffDigest != after.DiffDigest || before.HeadRevision != after.HeadRevision {
		s.Reason = "checkout changed during capture"
		s.Files = nil
		return s
	}
	s.State = "available"
	return s
}

func checkpointSnapshot(checkpoint local.Checkpoint) local.CheckoutSnapshot {
	if checkpoint.Snapshot == nil {
		return local.CheckoutSnapshot{State: "unavailable", Reason: "checkpoint has no captured checkout snapshot"}
	}
	return *checkpoint.Snapshot
}

// completionReceiptBaseline creates the deliberately weaker fallback used when
// no Local checkpoint exists. A receipt has a committed HEAD and endpoint
// digest, but no checkout-local dirty manifest, so only the committed tree can
// contribute path detail. Local-checkout receipts lack a durable historical
// checkout identity and are therefore not comparable after the fact.
func completionReceiptBaseline(stored, current gitReceipt, snapshot local.CheckoutSnapshot) (local.CheckoutSnapshot, bool) {
	if stored.Availability != "available" || current.Availability != "available" ||
		stored.FreshnessScope != "shared_repository" || current.FreshnessScope != "shared_repository" ||
		strings.TrimSpace(stored.Repository) == "" || stored.Repository != current.Repository ||
		strings.TrimSpace(stored.Branch) == "" || stored.Branch != current.Branch ||
		strings.TrimSpace(stored.HeadRevision) == "" || strings.TrimSpace(stored.DiffDigest) == "" ||
		snapshot.State != "available" || strings.TrimSpace(snapshot.CheckoutID) == "" {
		return local.CheckoutSnapshot{}, false
	}
	return local.CheckoutSnapshot{
		Version:     1,
		State:       "available",
		CheckoutID:  snapshot.CheckoutID,
		Head:        stored.HeadRevision,
		Fingerprint: stored.DiffDigest,
	}, true
}

func checkoutBlob(root, path string, sha256Objects bool) (string, int64, error) {
	if !filepath.IsLocal(path) {
		return "", 0, fmt.Errorf("nonlocal path")
	}
	info, err := os.Lstat(filepath.Join(root, path))
	if os.IsNotExist(err) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	if info.IsDir() {
		// Git reports a tracked file replaced by a directory as a deleted
		// path plus its untracked children. A nested Git checkout is different:
		// its contents cannot be represented by this dirty-file manifest.
		if _, err := os.Lstat(filepath.Join(root, path, ".git")); err == nil {
			return "", 0, fmt.Errorf("nested Git checkout")
		} else if !os.IsNotExist(err) {
			return "", 0, err
		}
		return "", 0, nil
	}
	mode := "100644"
	var body []byte
	if info.Mode()&os.ModeSymlink != 0 {
		mode = "120000"
		value, e := os.Readlink(filepath.Join(root, path))
		err = e
		body = []byte(value)
	} else {
		if !info.Mode().IsRegular() || info.Size() > 16<<20 {
			return "", 0, fmt.Errorf("unsupported or oversized dirty file")
		}
		if info.Mode()&0111 != 0 {
			mode = "100755"
		}
		file, e := os.OpenInRoot(root, path)
		if e != nil {
			return "", 0, e
		}
		body, err = io.ReadAll(io.LimitReader(file, (16<<20)+1))
		file.Close()
		if len(body) > 16<<20 {
			return "", 0, fmt.Errorf("oversized dirty file")
		}
	}
	if err != nil {
		return "", 0, err
	}
	var h hash.Hash = sha1.New()
	if sha256Objects {
		h = sha256.New()
	}
	fmt.Fprintf(h, "blob %d\x00", len(body))
	h.Write(body)
	return mode + ":" + fmt.Sprintf("%x", h.Sum(nil)), int64(len(body)), nil
}

func compareCheckout(ctx context.Context, deps *Deps, before, after local.CheckoutSnapshot) checkpointDelta {
	if before.State != "available" || after.State != "available" {
		return checkpointDelta{State: "unavailable", Limit: "baseline/current checkout incomplete: " + before.Reason + " " + after.Reason}
	}
	if before.CheckoutID == "" || before.CheckoutID != after.CheckoutID {
		return checkpointDelta{State: "noncomparable", Limit: "different checkout identities"}
	}
	runner := deps.DeployRunner
	if runner == nil {
		runner = deploy.ExecRunner{}
	}
	dir := deliveryWorkingDir(deps)
	for _, head := range []string{before.Head, after.Head} {
		if _, err := gitOutput(ctx, runner, dir, "cat-file", "-e", head+"^{tree}"); err != nil {
			return checkpointDelta{State: "unavailable", Limit: "historical Git tree unavailable"}
		}
	}
	paths := map[string]bool{}
	for p := range before.Files {
		paths[p] = true
	}
	for p := range after.Files {
		paths[p] = true
	}
	changed, err := gitOutput(ctx, runner, dir, "diff", "--no-relative", "--no-renames", "--name-only", "-z", before.Head, after.Head)
	if err != nil {
		return checkpointDelta{State: "unavailable", Limit: "Git tree comparison unavailable"}
	}
	for _, p := range strings.Split(string(changed), "\x00") {
		if p != "" {
			paths[p] = true
		}
	}
	delta := checkpointDelta{State: "unchanged"}
	value := func(s local.CheckoutSnapshot, path string) (string, error) {
		if v, ok := s.Files[path]; ok {
			return v, nil
		}
		raw, err := gitOutput(ctx, runner, dir, "ls-tree", "--full-tree", "-z", s.Head, "--", path)
		if err != nil {
			return "", err
		}
		if len(raw) == 0 {
			return "", nil
		}
		prefix, _, ok := strings.Cut(string(raw), "\t")
		fields := strings.Fields(prefix)
		if !ok || len(fields) != 3 {
			return "", fmt.Errorf("invalid tree entry")
		}
		if fields[1] == "tree" {
			return "", nil
		}
		return fields[0] + ":" + fields[2], nil
	}
	for p := range paths {
		a, e1 := value(before, p)
		b, e2 := value(after, p)
		if e1 != nil || e2 != nil {
			return checkpointDelta{State: "unavailable", Limit: "Git path comparison unavailable"}
		}
		if a == b {
			continue
		}
		delta.State = "changed"
		switch {
		case a == "":
			delta.Added = append(delta.Added, p)
		case b == "":
			delta.Removed = append(delta.Removed, p)
		default:
			delta.Modified = append(delta.Modified, p)
		}
	}
	sort.Strings(delta.Added)
	sort.Strings(delta.Removed)
	sort.Strings(delta.Modified)
	return delta
}

// compareCommittedTrees intentionally ignores dirty manifests. Completion
// receipts retain only their committed Git identity, so their fallback must not
// reinterpret a later checkout's dirty state as a change since completion.
func compareCommittedTrees(ctx context.Context, deps *Deps, before, current local.CheckoutSnapshot) checkpointDelta {
	committed := current
	committed.Files = nil
	committed.Fingerprint = before.Fingerprint
	return compareCheckout(ctx, deps, before, committed)
}
