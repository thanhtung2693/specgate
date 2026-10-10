package local

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var ErrVerificationConflict = errors.New("verification contract already pinned or delivery already reported")
var ErrVerificationInvalid = errors.New("invalid verification contract or report")

type VerificationCheck struct {
	Name       string           `json:"name"`
	Command    string           `json:"command"`
	Cwd        string           `json:"cwd"`
	TestReport *JUnitTestReport `json:"test_report,omitempty"`
}

// JUnitTestReport is an opt-in v2 assertion over a report created by a
// reviewed command. It deliberately contains only exact testcase identities;
// callers must not infer selectors from a framework, heading, or test name.
type JUnitTestReport struct {
	Format    string                        `json:"format"`
	Selectors map[string][]SelectedTestCase `json:"selectors"`
}

type SelectedTestCase struct {
	ClassName string `json:"classname"`
	Name      string `json:"name"`
}

type WatchedPath struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

type WatchedPathDrift struct {
	Path          string `json:"path"`
	PinnedDigest  string `json:"pinned_digest"`
	CurrentDigest string `json:"current_digest,omitempty"`
	State         string `json:"state"`
}
type VerificationContractInput struct {
	ContextDigest string              `json:"context_digest"`
	Shell         string              `json:"shell"`
	Checks        []VerificationCheck `json:"checks"`
	WatchedPaths  []string            `json:"watched_paths,omitempty"`
}
type VerificationContract struct {
	Version       int                 `json:"version,omitempty"`
	Status        string              `json:"status"`
	WorkID        string              `json:"work_id"`
	ContextDigest string              `json:"context_digest"`
	Digest        string              `json:"digest,omitempty"`
	Shell         string              `json:"shell,omitempty"`
	Checks        []VerificationCheck `json:"checks,omitempty"`
	WatchedPaths  []WatchedPath       `json:"watched_paths,omitempty"`
	Bindings      map[string]string   `json:"bindings,omitempty"`
	Actor         string              `json:"actor,omitempty"`
	CreatedAt     string              `json:"created_at,omitempty"`
}
type verificationQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getVerificationContract(ctx context.Context, q verificationQuerier, work WorkItem) (VerificationContract, error) {
	var body string
	err := q.QueryRowContext(ctx, `SELECT body FROM verification_contracts WHERE workspace_id = ? AND work_id = ?`, work.WorkspaceID, work.ID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return VerificationContract{Status: "unconfigured", WorkID: work.ID, ContextDigest: work.ContextDigest}, nil
	}
	if err != nil {
		return VerificationContract{}, err
	}
	var c VerificationContract
	err = json.Unmarshal([]byte(body), &c)
	if err == nil && (c.Version < 0 || c.Version > 2) {
		return VerificationContract{}, fmt.Errorf("%w: verification contract version %d", ErrStoreIncompatible, c.Version)
	}
	return c, err
}
func (s *Store) GetVerificationContract(ctx context.Context, workspaceID, ref string) (VerificationContract, error) {
	work, err := s.GetWork(ctx, workspaceID, ref)
	if err != nil {
		return VerificationContract{}, err
	}
	return getVerificationContract(ctx, s.db, work)
}

// ResolveVerificationCwd normalizes a repository-relative cwd, rejecting
// escapes including symlink targets. The directory must already exist.
func ResolveVerificationCwd(repoRoot, cwd string) (string, string, error) {
	invalid := func() (string, string, error) {
		return "", "", fmt.Errorf("%w: cwd must remain inside an existing repository directory", ErrVerificationInvalid)
	}
	if repoRoot == "" || filepath.IsAbs(cwd) {
		return invalid()
	}
	if cwd == "" {
		cwd = "."
	}
	clean := filepath.Clean(cwd)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return invalid()
	}
	root, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return invalid()
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return invalid()
	}
	abs, err := filepath.EvalSymlinks(filepath.Join(root, clean))
	if err != nil {
		return invalid()
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return invalid()
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return invalid()
	}
	return filepath.ToSlash(clean), abs, nil
}

// PreviewVerificationContract validates pin inputs without writing a row.
// Bindings always come from the stored criteria, never the caller.
func (s *Store) PreviewVerificationContract(ctx context.Context, workspaceID, ref, repoRoot, actor string, input VerificationContractInput) (VerificationContract, error) {
	work, err := s.GetWork(ctx, workspaceID, ref)
	if err != nil {
		return VerificationContract{}, err
	}
	existing, err := getVerificationContract(ctx, s.db, work)
	if err != nil {
		return VerificationContract{}, err
	}
	var reports int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM delivery_reports WHERE workspace_id=? AND work_id=?`, workspaceID, work.ID).Scan(&reports); err != nil {
		return VerificationContract{}, err
	}
	if existing.Status == "pinned" || reports > 0 || work.Phase == "delivered" {
		return VerificationContract{}, ErrVerificationConflict
	}
	return buildVerificationContract(work, repoRoot, actor, input)
}
func buildVerificationContract(work WorkItem, root, actor string, input VerificationContractInput) (VerificationContract, error) {
	bad := func(message string) (VerificationContract, error) {
		return VerificationContract{}, fmt.Errorf("%w: %s", ErrVerificationInvalid, message)
	}
	if input.ContextDigest != work.ContextDigest {
		return bad("context_digest does not match work")
	}
	if input.Shell != "sh" {
		return bad("shell must be sh")
	}
	bindings := map[string]string{}
	names := map[string]bool{}
	for i, raw := range work.AcceptanceCriteria {
		if problem := AcceptanceCriterionBindingProblem(raw); problem != "" {
			return bad(problem)
		}
		_, name := ParseAcceptanceCriterionBinding(raw)
		if name != "" {
			bindings[fmt.Sprintf("local-%d", i+1)] = name
			names[name] = true
		}
	}
	if len(names) == 0 || len(input.Checks) == 0 {
		return bad("at least one stored @check binding and check is required")
	}
	checks := append([]VerificationCheck(nil), input.Checks...)
	seen := map[string]bool{}
	for i, check := range checks {
		if !names[check.Name] || seen[check.Name] || strings.TrimSpace(check.Command) == "" {
			return bad("checks must have unique bound names and nonempty commands")
		}
		cwd, _, err := ResolveVerificationCwd(root, check.Cwd)
		if err != nil {
			return VerificationContract{}, err
		}
		checks[i].Cwd = cwd
		seen[check.Name] = true
	}
	if len(seen) != len(names) {
		return bad("every stored bound check must be configured")
	}
	watched, err := resolveWatchedPaths(root, input.WatchedPaths)
	if err != nil {
		return VerificationContract{}, err
	}
	version := 0
	for _, check := range checks {
		if check.TestReport == nil {
			continue
		}
		version = 2
		if err := validateJUnitSelectors(check.Name, check.TestReport, bindings); err != nil {
			return VerificationContract{}, err
		}
	}
	if len(watched) > 0 {
		version = 2
	}
	c := VerificationContract{Version: version, Status: "pinned", WorkID: work.ID, ContextDigest: work.ContextDigest, Shell: "sh", Checks: checks, WatchedPaths: watched, Bindings: bindings, Actor: strings.TrimSpace(actor), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	encoded, err := json.Marshal(struct {
		Version       int
		WorkspaceID   string
		WorkID        string
		ContextDigest string
		Shell         string
		Checks        []VerificationCheck
		WatchedPaths  []WatchedPath
		Bindings      map[string]string
	}{c.Version, work.WorkspaceID, work.ID, c.ContextDigest, c.Shell, c.Checks, c.WatchedPaths, c.Bindings})
	if err != nil {
		return VerificationContract{}, err
	}
	// Keep the v1 serialization byte-for-byte stable: a legacy pin must retain
	// its old digest as promised by the Local compatibility contract.
	if c.Version == 0 {
		encoded, err = json.Marshal(struct {
			WorkspaceID   string
			WorkID        string
			ContextDigest string
			Shell         string
			Checks        []VerificationCheck
			Bindings      map[string]string
		}{work.WorkspaceID, work.ID, c.ContextDigest, c.Shell, c.Checks, c.Bindings})
		if err != nil {
			return VerificationContract{}, err
		}
	}
	c.Digest = digestText(string(encoded))
	return c, nil
}

func validateJUnitSelectors(checkName string, report *JUnitTestReport, bindings map[string]string) error {
	if report.Format != "junit" {
		return fmt.Errorf("%w: test_report format must be junit", ErrVerificationInvalid)
	}
	bound := map[string]bool{}
	for criterionID, boundCheck := range bindings {
		if boundCheck == checkName {
			bound[criterionID] = true
		}
	}
	for criterionID := range bound {
		selectors := report.Selectors[criterionID]
		if len(selectors) == 0 {
			return fmt.Errorf("%w: report-enabled check %q needs selectors for %s", ErrVerificationInvalid, checkName, criterionID)
		}
		seen := map[string]bool{}
		for _, selector := range selectors {
			key := selector.ClassName + "\x00" + selector.Name
			if strings.TrimSpace(selector.ClassName) == "" || strings.TrimSpace(selector.Name) == "" || seen[key] {
				return fmt.Errorf("%w: selectors must be unique exact classname/name pairs", ErrVerificationInvalid)
			}
			seen[key] = true
		}
	}
	for criterionID := range report.Selectors {
		if !bound[criterionID] {
			return fmt.Errorf("%w: selector references unbound criterion %s", ErrVerificationInvalid, criterionID)
		}
	}
	return nil
}

func resolveWatchedPaths(root string, paths []string) ([]WatchedPath, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	result := make([]WatchedPath, 0, len(paths))
	for _, raw := range paths {
		clean, _, err := ResolveVerificationFile(root, raw)
		if err != nil {
			return nil, err
		}
		if seen[clean] {
			return nil, fmt.Errorf("%w: watched_paths must be unique", ErrVerificationInvalid)
		}
		seen[clean] = true
		bytes, err := readWatchedFile(root, clean)
		if err != nil {
			return nil, fmt.Errorf("%w: watched path is unreadable", ErrVerificationInvalid)
		}
		digest := sha256.Sum256(bytes)
		result = append(result, WatchedPath{Path: clean, Digest: fmt.Sprintf("%x", digest)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

// ResolveVerificationFile is the regular-file analogue of
// ResolveVerificationCwd. It rejects symlinks and anything outside the
// checkout, so a pin cannot hash arbitrary machine files.
func ResolveVerificationFile(repoRoot, path string) (string, string, error) {
	if repoRoot == "" || filepath.IsAbs(path) || strings.TrimSpace(path) == "" {
		return "", "", fmt.Errorf("%w: watched path must be a repository-relative regular file", ErrVerificationInvalid)
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("%w: watched path must remain inside repository", ErrVerificationInvalid)
	}
	root, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return "", "", ErrVerificationInvalid
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", "", ErrVerificationInvalid
	}
	joined := filepath.Join(root, clean)
	info, err := os.Lstat(joined)
	if err != nil {
		return "", "", fmt.Errorf("%w: watched path: %w", ErrVerificationInvalid, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", "", fmt.Errorf("%w: watched path must be a regular non-symlink file", ErrVerificationInvalid)
	}
	abs, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", "", ErrVerificationInvalid
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", ErrVerificationInvalid
	}
	return filepath.ToSlash(clean), abs, nil
}

// Open relative to a confined root and bound the read, including growth after
// stat. Comparing the opened inode prevents silently hashing a replacement.
func readWatchedFile(root, path string) ([]byte, error) {
	_, abs, err := ResolveVerificationFile(root, path)
	if err != nil {
		return nil, err
	}
	before, err := os.Lstat(abs)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenInRoot(root, path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	const limit = 16 * 1024 * 1024
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) || opened.Size() > limit {
		return nil, fmt.Errorf("%w: watched file changed or exceeds 16 MiB", ErrVerificationInvalid)
	}
	body, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if len(body) > limit || opened.Size() != after.Size() || !opened.ModTime().Equal(after.ModTime()) {
		return nil, fmt.Errorf("%w: watched file changed during read or exceeds 16 MiB", ErrVerificationInvalid)
	}
	return body, nil
}

// WatchedPathDrift compares only the explicitly pinned regular files. It is a
// review warning, never an automatic claim that a test was weakened; endpoint
// hashes cannot observe a change that was reverted before this read.
func WatchedPathDriftFor(root string, watched []WatchedPath) []WatchedPathDrift {
	result := make([]WatchedPathDrift, 0, len(watched))
	for _, pinned := range watched {
		item := WatchedPathDrift{Path: pinned.Path, PinnedDigest: pinned.Digest, State: "unavailable"}
		body, err := readWatchedFile(root, pinned.Path)
		if err == nil {
			digest := sha256.Sum256(body)
			item.CurrentDigest = fmt.Sprintf("%x", digest)
			item.State = "unchanged"
			if item.CurrentDigest != item.PinnedDigest {
				item.State = "changed"
			}
		} else if errors.Is(err, os.ErrNotExist) {
			item.State = "missing"
		}
		result = append(result, item)
	}
	return result
}
func (s *Store) PinVerificationContract(ctx context.Context, workspaceID, ref, repoRoot, actor string, input VerificationContractInput, reviewedDigests ...string) (VerificationContract, error) {
	work, err := s.GetWork(ctx, workspaceID, ref)
	if err != nil {
		return VerificationContract{}, err
	}
	c, err := buildVerificationContract(work, repoRoot, actor, input)
	if err != nil {
		return VerificationContract{}, err
	}
	if len(reviewedDigests) > 0 && c.Digest != reviewedDigests[0] {
		return VerificationContract{}, fmt.Errorf("%w: verification inputs changed after preview; review again", ErrVerificationConflict)
	}
	if c.Version >= 2 {
		var pinned VerificationContract
		err := s.WithEnhancedWrite(ctx, func(tx *sql.Tx) error {
			current, err := buildVerificationContract(work, repoRoot, actor, input)
			if err != nil {
				return err
			}
			if current.Digest != c.Digest {
				return fmt.Errorf("%w: verification inputs changed before pinning; review again", ErrVerificationConflict)
			}
			var pinErr error
			pinned, pinErr = pinVerificationContractTx(ctx, tx, workspaceID, work, c)
			return pinErr
		})
		return pinned, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return VerificationContract{}, err
	}
	defer tx.Rollback()
	pinned, err := pinVerificationContractTx(ctx, tx, workspaceID, work, c)
	if err != nil {
		return VerificationContract{}, err
	}
	if err := tx.Commit(); err != nil {
		return VerificationContract{}, err
	}
	return pinned, nil
}

func pinVerificationContractTx(ctx context.Context, tx *sql.Tx, workspaceID string, work WorkItem, c VerificationContract) (VerificationContract, error) {
	var phase, digest string
	if err := tx.QueryRowContext(ctx, `SELECT phase, context_digest FROM work_items WHERE workspace_id = ? AND id = ?`, workspaceID, work.ID).Scan(&phase, &digest); err != nil {
		return VerificationContract{}, err
	}
	var reports int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM delivery_reports WHERE workspace_id = ? AND work_id = ?`, workspaceID, work.ID).Scan(&reports); err != nil {
		return VerificationContract{}, err
	}
	existing, err := getVerificationContract(ctx, tx, work)
	if err != nil {
		return VerificationContract{}, err
	}
	if phase == "delivered" || reports > 0 || existing.Status == "pinned" {
		return VerificationContract{}, ErrVerificationConflict
	}
	if digest != c.ContextDigest {
		return VerificationContract{}, fmt.Errorf("%w: work context changed", ErrVerificationInvalid)
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		return VerificationContract{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO verification_contracts(workspace_id,work_id,body) VALUES (?,?,?)`, workspaceID, work.ID, encoded); err != nil {
		return VerificationContract{}, err
	}
	return c, nil
}
func (s *Store) ValidateVerificationReport(ctx context.Context, workspaceID, ref, repoRoot string, body map[string]any) error {
	work, err := s.GetWork(ctx, workspaceID, ref)
	if err != nil {
		return err
	}
	if work.Phase == "delivered" {
		return ErrDeliveryApproved
	}
	if digest, _ := body["context_digest"].(string); digest != work.ContextDigest {
		return fmt.Errorf("%w: context_digest does not match work", ErrVerificationInvalid)
	}
	c, err := getVerificationContract(ctx, s.db, work)
	if err != nil {
		return err
	}
	return validateVerificationReport(c, repoRoot, body)
}
func validateVerificationReport(c VerificationContract, root string, body map[string]any) error {
	bad := func(message string) error { return fmt.Errorf("%w: %s", ErrVerificationInvalid, message) }
	digest, _ := body["verification_contract_digest"].(string)
	if c.Status == "unconfigured" {
		if digest != "" {
			return bad("work has no pinned contract")
		}
		return nil
	}
	if digest != c.Digest {
		return bad("verification_contract_digest does not match pinned contract")
	}
	encoded, err := json.Marshal(body["checks"])
	if err != nil {
		return err
	}
	var checks []VerificationCheck
	if err := json.Unmarshal(encoded, &checks); err != nil {
		return bad("checks must be an array")
	}
	if len(checks) != len(c.Checks) {
		return bad("report must contain exactly the pinned checks")
	}
	expected := map[string]VerificationCheck{}
	for _, check := range c.Checks {
		expected[check.Name] = check
	}
	seen := map[string]bool{}
	for _, check := range checks {
		want, ok := expected[check.Name]
		if !ok || seen[check.Name] || check.Command != want.Command {
			return bad("check command or name does not match pinned contract")
		}
		cwd, _, err := ResolveVerificationCwd(root, check.Cwd)
		if err != nil {
			return err
		}
		if cwd != want.Cwd {
			return bad("check cwd does not match pinned contract")
		}
		seen[check.Name] = true
	}
	return nil
}
