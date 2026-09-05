package local

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrVerificationConflict = errors.New("verification contract already pinned or delivery already reported")
var ErrVerificationInvalid = errors.New("invalid verification contract or report")

type VerificationCheck struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Cwd     string `json:"cwd"`
}
type VerificationContractInput struct {
	ContextDigest string              `json:"context_digest"`
	Shell         string              `json:"shell"`
	Checks        []VerificationCheck `json:"checks"`
}
type VerificationContract struct {
	Status        string              `json:"status"`
	WorkID        string              `json:"work_id"`
	ContextDigest string              `json:"context_digest"`
	Digest        string              `json:"digest,omitempty"`
	Shell         string              `json:"shell,omitempty"`
	Checks        []VerificationCheck `json:"checks,omitempty"`
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
	c := VerificationContract{Status: "pinned", WorkID: work.ID, ContextDigest: work.ContextDigest, Shell: "sh", Checks: checks, Bindings: bindings, Actor: strings.TrimSpace(actor), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	encoded, err := json.Marshal(struct {
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
	c.Digest = digestText(string(encoded))
	return c, nil
}
func (s *Store) PinVerificationContract(ctx context.Context, workspaceID, ref, repoRoot, actor string, input VerificationContractInput) (VerificationContract, error) {
	work, err := s.GetWork(ctx, workspaceID, ref)
	if err != nil {
		return VerificationContract{}, err
	}
	c, err := buildVerificationContract(work, repoRoot, actor, input)
	if err != nil {
		return VerificationContract{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return VerificationContract{}, err
	}
	defer tx.Rollback()
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
	if err := tx.Commit(); err != nil {
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
