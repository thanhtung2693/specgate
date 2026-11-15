package command

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/specgate/specgate/app/cli/internal/buildinfo"
	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
	"github.com/specgate/specgate/app/cli/internal/output"
	"github.com/spf13/cobra"
)

//go:embed local_ui_assets
var localUIAssets embed.FS

const localUICookie = "specgate_local_ui"

type localUIServer struct {
	deps                                        *Deps
	store                                       *local.Store
	selection                                   local.Selection
	repoRoot, host, sessionToken, csrfToken, id string
	mu                                          sync.Mutex
	maintenance                                 *localUIMaintenance
}

func newLocalUIServer(deps *Deps, store *local.Store, selection local.Selection, repoRoot, host string) (*localUIServer, error) {
	token := func() (string, error) {
		var data [32]byte
		_, err := rand.Read(data[:])
		return hex.EncodeToString(data[:]), err
	}
	session, err := token()
	if err != nil {
		return nil, err
	}
	csrf, err := token()
	if err != nil {
		return nil, err
	}
	id, err := token()
	if err != nil {
		return nil, err
	}
	bound := *deps
	bound.WorkingDir = repoRoot
	bound.WorkspaceOverride = selection.Workspace.ID
	return &localUIServer{deps: &bound, store: store, selection: selection, repoRoot: repoRoot, host: host, sessionToken: session, csrfToken: csrf, id: id, maintenance: newLocalUIMaintenance(&bound)}, nil
}

func newLocalUICmd(deps *Deps) *cobra.Command {
	var port int
	var noOpen bool
	cmd := &cobra.Command{Use: "ui [work-ref]", Short: "Serve the Local inspection and human review UI on loopback", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if port < 0 || port > 65535 {
			return &output.ExitError{Code: output.ExitUsage, Err: errors.New("--port must be between 0 and 65535")}
		}
		root, ok := config.FindProjectRoot(deps.WorkingDir)
		if !ok {
			return localExitError(deps, "ui", errors.New("local UI requires a repository; run from the intended repository"))
		}
		var err error
		root, err = filepath.EvalSymlinks(root)
		if err != nil {
			return localExitError(deps, "ui", err)
		}
		store, err := openLocalStore(deps)
		if err != nil {
			return localExitError(deps, "ui", err)
		}
		defer store.Close()
		selection, err := localSelection(cmd.Context(), deps, store)
		if err != nil {
			return localExitError(deps, "ui", err)
		}
		ref := ""
		if len(args) > 0 {
			work, err := store.GetWork(cmd.Context(), selection.Workspace.ID, args[0])
			if err != nil {
				return localExitError(deps, "ui", err)
			}
			ref = work.Key
		}
		listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			return localExitError(deps, "ui", fmt.Errorf("start Local UI: %w", err))
		}
		defer listener.Close()
		ui, err := newLocalUIServer(deps, store, selection, root, listener.Addr().String())
		if err != nil {
			return localExitError(deps, "ui", err)
		}
		descriptor := localUIDiscovery{URL: "http://" + ui.host, SessionID: ui.id, WorkspaceID: selection.Workspace.ID, RepoRoot: root}
		path, err := writeLocalUIDiscovery(deps, descriptor)
		if err != nil {
			return localExitError(deps, "ui", err)
		}
		defer os.Remove(path)
		target := descriptor.URL
		if ref != "" {
			target += "?ref=" + url.QueryEscape(ref)
		}
		server := &http.Server{Handler: ui, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 8192}
		done := make(chan error, 1)
		go func() { done <- server.Serve(listener) }()
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		defer func() {
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdown)
		}()
		if !noOpen && deps.Opener != nil {
			if err := deps.Opener(target); err != nil {
				fmt.Fprintf(deps.Stderr, "Browser unavailable: %v. Open %s\n", err, target)
			}
		}
		if deps.Printer.Mode() == output.ModeJSON {
			deps.Printer.Success("ui", map[string]any{"url": target, "workspace": selection.Workspace, "repo_root": root, "mode": "local"})
		} else {
			fmt.Fprintf(deps.Stdout, "Local UI: %s\nWorkspace: %s\nRepository: %s\nPress Ctrl+C to stop.\n", target, selection.Workspace.Name, root)
		}
		select {
		case <-ctx.Done():
			return nil
		case err := <-done:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return &output.ExitError{Code: output.ExitUnavailable, Err: err}
		}
	}}
	cmd.Flags().IntVar(&port, "port", 0, "Loopback port (0 chooses an available port)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Print the URL without opening a browser")
	return cmd
}

func (s *localUIServer) cookieName() string { return localUICookie + "_" + s.id[:16] }

func (s *localUIServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	origin := r.Header.Get("Origin")
	if r.Host != s.host || (origin != "" && origin != "http://"+s.host) || (r.Header.Get("Sec-Fetch-Site") == "cross-site" && !(r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("Sec-Fetch-Mode") == "navigate" && r.Header.Get("Sec-Fetch-Dest") == "document")) {
		writeLocalUIError(w, 403, "forbidden", "Local UI requires its exact loopback host and origin")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeLocalUIError(w, 405, "method_not_allowed", "Only GET and explicit POST actions are supported")
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		if r.Method != http.MethodGet {
			writeLocalUIError(w, 405, "method_not_allowed", "Assets are read-only")
			return
		}
		assets, _ := fs.Sub(localUIAssets, "local_ui_assets")
		http.FileServer(http.FS(assets)).ServeHTTP(w, r)
		return
	}
	if r.URL.Path == "/api/ping" && r.Method == http.MethodGet {
		writeLocalUIJSON(w, 200, map[string]string{"session_id": s.id})
		return
	}
	if r.URL.Path == "/api/session" && r.Method == http.MethodGet {
		http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: s.sessionToken, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		writeLocalUIJSON(w, 200, map[string]string{"csrf_token": s.csrfToken})
		return
	}
	cookie, err := r.Cookie(s.cookieName())
	if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(s.sessionToken)) != 1 {
		writeLocalUIError(w, 403, "forbidden", "Open the Local UI to establish a session")
		return
	}
	if r.Method == http.MethodPost {
		if origin != "http://"+s.host || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-SpecGate-CSRF")), []byte(s.csrfToken)) != 1 {
			writeLocalUIError(w, 403, "forbidden", "Refresh the Local UI before this action")
			return
		}
		if ct := strings.Split(r.Header.Get("Content-Type"), ";")[0]; ct != "application/json" {
			writeLocalUIError(w, 415, "validation_failed", "POST actions require application/json")
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
	}
	if s.handleMaintenance(w, r) {
		return
	}
	switch r.URL.Path {
	case "/api/meta":
		if !localUIRequireMethod(w, r, "GET") {
			return
		}
		writeLocalUIJSON(w, 200, map[string]any{"version": buildinfo.Version, "mode": "local", "workspace": s.selection.Workspace, "repo_root": s.repoRoot})
	case "/api/works":
		if !localUIRequireMethod(w, r, "GET") {
			return
		}
		s.works(w, r)
	case "/api/work":
		if !localUIRequireMethod(w, r, "GET") {
			return
		}
		view, err := s.workView(r.Context(), r.URL.Query().Get("ref"))
		if err != nil {
			s.fail(w, err)
			return
		}
		writeLocalUIJSON(w, 200, view)
	case "/api/context":
		if !localUIRequireMethod(w, r, "GET") {
			return
		}
		s.context(w, r)
	case "/api/source":
		if !localUIRequireMethod(w, r, "GET") {
			return
		}
		s.source(w, r)
	case "/api/decision":
		if !localUIRequireMethod(w, r, "POST") {
			return
		}
		s.decision(w, r)
	default:
		writeLocalUIError(w, 404, "not_found", "Unknown Local UI endpoint")
	}
}
func localUIRequireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		w.Header().Set("Allow", method)
		writeLocalUIError(w, 405, "method_not_allowed", "This endpoint requires "+method)
		return false
	}
	return true
}
func writeLocalUIJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data})
}
func writeLocalUIError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": map[string]string{"code": code, "message": message}})
}
func decodeLocalUIRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 65536))
	if err != nil {
		writeLocalUIError(w, 400, "validation_failed", "Request body exceeds 64 KiB or is unreadable")
		return false
	}
	if strings.TrimSpace(string(body)) == "null" {
		writeLocalUIError(w, 400, "validation_failed", "JSON object is required")
		return false
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeLocalUIError(w, 400, "validation_failed", "Invalid JSON action: "+err.Error())
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeLocalUIError(w, 400, "validation_failed", "Exactly one JSON object is required")
		return false
	}
	return true
}
func (s *localUIServer) requestCommand(ctx context.Context) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	acceptanceSelectionFlags(cmd)
	return cmd
}
func (s *localUIServer) status(ctx context.Context, ref string) (changeStatusResult, error) {
	return changeStatusLocalFromStore(s.requestCommand(ctx), s.deps, s.store, s.selection.Workspace.ID, ref)
}
func (s *localUIServer) works(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListWork(r.Context(), s.selection.Workspace.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	views := make([]map[string]any, 0, len(items))
	for _, item := range items {
		status, _, err := deriveLocalChangeStatusFromStore(r.Context(), s.store, s.selection.Workspace.ID, item)
		if err != nil {
			s.fail(w, err)
			return
		}
		views = append(views, map[string]any{
			"ref": item.Key, "title": item.Title, "state": status.State, "next_actor": status.NextActor,
			"decision": status.Decision, "stale": status.Stale, "created_at": item.CreatedAt,
			"review_id": status.ReviewID, "completion_id": status.CompletionID,
		})
	}
	writeLocalUIJSON(w, 200, map[string]any{"items": views})
}
func (s *localUIServer) workView(ctx context.Context, ref string) (map[string]any, error) {
	status, err := s.status(ctx, ref)
	if err != nil {
		return nil, err
	}
	context, err := readLocalContextView(ctx, s.store, s.selection.Workspace.ID, ref)
	if err != nil {
		return nil, err
	}
	completion := map[string]any{}
	var review map[string]any
	var comparison *localUIReviewComparison
	if status.Inspection != nil {
		record := status.Inspection.Review
		review = map[string]any{"id": record.ID, "report_id": record.ReportID, "human_decision": record.HumanDecision, "note": record.Note, "created_at": record.CreatedAt}
		for _, key := range []string{"agent", "summary", "affected_files", "criteria"} {
			if v, ok := status.Inspection.Report.Body[key]; ok {
				completion[key] = v
			}
		}
		var requiredCases []string
		for _, check := range status.Inspection.Contract.Checks {
			if check.TestReport != nil {
				for id, cases := range check.TestReport.Selectors {
					if len(cases) != 0 {
						requiredCases = append(requiredCases, id)
					}
				}
			}
		}
		slices.Sort(requiredCases)
		if len(requiredCases) > 0 {
			completion["selected_tests_required"] = requiredCases
		}
		checks := localUIReviewChecks(status.Inspection.Report.Body, status.Inspection.Contract)
		completion["checks"] = checks
		comparison = s.compareReviews(ctx, status.Inspection, checks)
	}
	return map[string]any{"status": status, "work": map[string]any{"id": context.Work.ID, "key": context.Work.Key, "title": context.Work.Title, "description": context.Work.Description, "acceptance_criteria": context.Work.AcceptanceCriteria, "context_digest": context.ContextDigest, "artifact_id": context.Work.ArtifactID, "phase": context.Work.Phase}, "documents": context.Documents, "completion": completion, "review": review, "review_comparison": comparison, "can_decide": status.Inspection != nil && status.Inspection.Review.HumanDecision == ""}, nil
}
func (s *localUIServer) context(w http.ResponseWriter, r *http.Request) {
	ref, path, role := r.URL.Query().Get("ref"), r.URL.Query().Get("path"), r.URL.Query().Get("role")
	work, err := s.store.GetWork(r.Context(), s.selection.Workspace.ID, ref)
	if err != nil {
		s.fail(w, err)
		return
	}
	if path == "" {
		pack, err := s.store.ContextPack(r.Context(), s.selection.Workspace.ID, ref)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeLocalUIJSON(w, 200, map[string]any{"path": "context.md", "role": "context", "digest": pack.Digest, "content": pack.Markdown})
		return
	}
	if work.ArtifactID != "" {
		artifact, err := s.store.GetArtifact(r.Context(), s.selection.Workspace.ID, work.ArtifactID)
		if err != nil {
			s.fail(w, err)
			return
		}
		var selected *local.ArtifactDocument
		for _, doc := range artifact.Documents {
			if doc.Path == path && (role == "" || doc.Role == role) {
				if selected != nil {
					writeLocalUIError(w, 400, "validation_failed", "Document path has multiple roles; select the exact role")
					return
				}
				copy := doc
				selected = &copy
			}
		}
		if selected != nil {
			writeLocalUIJSON(w, 200, map[string]any{"path": selected.Path, "role": selected.Role, "digest": selected.Digest, "content": string(selected.Content)})
			return
		}
	}
	writeLocalUIError(w, 404, "not_found", "Document is not in this work's pinned context")
}
func (s *localUIServer) decision(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ref          string `json:"ref"`
		Decision     string `json:"decision"`
		ReviewID     string `json:"review_id"`
		CompletionID string `json:"completion_id"`
		BasisDigest  string `json:"basis_digest"`
		Note         string `json:"note"`
	}
	if !decodeLocalUIRequest(w, r, &body) {
		return
	}
	if body.Ref == "" || (body.Decision != "approve" && body.Decision != "reject") || body.ReviewID == "" || body.CompletionID == "" {
		writeLocalUIError(w, 400, "validation_failed", "ref, decision, review_id and completion_id are required")
		return
	}
	if body.Decision == "reject" && strings.TrimSpace(body.Note) == "" {
		writeLocalUIError(w, 400, "validation_failed", "A note is required when requesting changes.")
		return
	}
	status, err := s.status(r.Context(), body.Ref)
	if err != nil {
		s.fail(w, err)
		return
	}
	if status.Inspection == nil || status.Inspection.Review.HumanDecision != "" || status.ReviewID != body.ReviewID || status.CompletionID != body.CompletionID || status.BasisDigest != body.BasisDigest {
		writeLocalUIError(w, 409, "conflict", "Review changed; refresh and review the current evidence before deciding")
		return
	}
	opts, _, err := acceptanceOptions(s.requestCommand(r.Context()), s.deps, s.store, s.selection.Workspace.ID, body.Ref)
	if err != nil {
		s.fail(w, err)
		return
	}
	opts.ExpectedCompletionID = body.CompletionID
	if err := s.store.DecideDeliveryWithBasis(r.Context(), s.selection.Workspace.ID, body.Ref, body.Decision, s.selection.User.Username, body.Note, body.ReviewID, body.BasisDigest, opts); err != nil {
		s.fail(w, err)
		return
	}
	view, err := s.workView(r.Context(), body.Ref)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeLocalUIJSON(w, 200, view)
}
func (s *localUIServer) fail(w http.ResponseWriter, err error) {
	status, code := 500, "unavailable"
	if errors.Is(err, sql.ErrNoRows) {
		status, code = 404, "not_found"
	}
	if errors.Is(err, local.ErrReviewChanged) || errors.Is(err, local.ErrDecisionRecorded) || errors.Is(err, local.ErrPreconditionNotMet) || errors.Is(err, local.ErrSeparationOfDuties) {
		status, code = 409, "conflict"
	}
	writeLocalUIError(w, status, code, err.Error())
}
