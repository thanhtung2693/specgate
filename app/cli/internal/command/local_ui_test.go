package command

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/specgate/specgate/app/cli/internal/config"
	"github.com/specgate/specgate/app/cli/internal/local"
)

func localUITestServer(t *testing.T) (*localUIServer, local.WorkItem) {
	t.Helper()
	return localUITestServerAt(t, filepath.Join(t.TempDir(), "state.db"))
}

func localUITestServerAt(t *testing.T, dbPath string) (*localUIServer, local.WorkItem) {
	t.Helper()
	root := t.TempDir()
	store, err := local.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	selection, err := store.Initialize(t.Context(), local.InitInput{WorkspaceName: "UI workspace", Username: "reviewer", DisplayName: "Reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateQuickWork(t.Context(), selection.Workspace.ID, local.QuickWorkInput{Title: "Review safely", Description: "Scoped work", AcceptanceCriteria: []string{"Keep exact evidence"}})
	if err != nil {
		t.Fatal(err)
	}
	deps := &Deps{WorkingDir: root, Stdout: io.Discard, Stderr: io.Discard}
	s, err := newLocalUIServer(deps, store, selection, root, "127.0.0.1:8910")
	if err != nil {
		t.Fatal(err)
	}
	return s, work
}
func localUIRequest(s *localUIServer, method, path, body string, session, csrf bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://"+s.host+path, strings.NewReader(body))
	if session {
		req.AddCookie(&http.Cookie{Name: s.cookieName(), Value: s.sessionToken})
	}
	if csrf {
		req.Header.Set("Origin", "http://"+s.host)
		req.Header.Set("X-SpecGate-CSRF", s.csrfToken)
	}
	req.Header.Set("Content-Type", "application/json")
	out := httptest.NewRecorder()
	s.ServeHTTP(out, req)
	return out
}
func TestLocalUISecurityBoundary(t *testing.T) {
	s, _ := localUITestServer(t)
	for _, tc := range []struct {
		name, method, path, origin, host, fetch string
		session, csrf                           bool
		want                                    int
	}{
		{name: "bootstrap", method: "GET", path: "/api/session", want: 200},
		{name: "missing session", method: "GET", path: "/api/works", want: 403},
		{name: "valid session", method: "GET", path: "/api/works", session: true, want: 200},
		{name: "bad host", method: "GET", path: "/api/session", host: "attacker.example", want: 403},
		{name: "bad origin", method: "GET", path: "/api/session", origin: "https://attacker.example", want: 403},
		{name: "same-site different port", method: "POST", path: "/api/decision", origin: "http://127.0.0.1:12345", fetch: "same-site", session: true, csrf: true, want: 403},
		{name: "cross site", method: "GET", path: "/api/session", fetch: "cross-site", want: 403},
		{name: "missing csrf", method: "POST", path: "/api/decision", session: true, want: 403},
		{name: "missing session post", method: "POST", path: "/api/decision", csrf: true, want: 403},
		{name: "method", method: "DELETE", path: "/api/works", session: true, want: 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "http://"+s.host+tc.path, strings.NewReader(`{}`))
			if tc.host != "" {
				r.Host = tc.host
			}
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.fetch != "" {
				r.Header.Set("Sec-Fetch-Site", tc.fetch)
			}
			if tc.session {
				r.AddCookie(&http.Cookie{Name: s.cookieName(), Value: s.sessionToken})
			}
			if tc.csrf {
				if tc.origin == "" {
					r.Header.Set("Origin", "http://"+s.host)
				}
				r.Header.Set("X-SpecGate-CSRF", s.csrfToken)
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("permissive CORS")
			}
		})
	}
	bootstrap := localUIRequest(s, "GET", "/api/session", "", false, false)
	cookies := bootstrap.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("unsafe cookie: %+v", cookies)
	}
}
func TestLocalUIReadAndExactDecision(t *testing.T) {
	s, work := localUITestServer(t)
	body := map[string]any{"context_digest": work.ContextDigest, "agent": map[string]any{"name": "builder"}, "summary": "Scoped evidence", "criteria": []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"path": "proof.txt", "line": 1}}}}
	if _, err := s.store.SubmitDelivery(t.Context(), s.selection.Workspace.ID, work.Key, body); err != nil {
		t.Fatal(err)
	}
	read := localUIRequest(s, "GET", "/api/work?ref="+work.Key, "", true, false)
	if read.Code != 200 {
		t.Fatalf("read %d: %s", read.Code, read.Body.String())
	}
	var result struct {
		Data struct {
			Status changeStatusResult `json:"status"`
		}
	}
	if err := json.Unmarshal(read.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	status := result.Data.Status
	review, err := s.store.DeliveryStatus(t.Context(), s.selection.Workspace.ID, work.Key)
	if err != nil || review.HumanDecision != "" {
		t.Fatalf("GET changed decision: %+v %v", review, err)
	}
	payload := map[string]any{"ref": work.Key, "decision": "reject", "review_id": status.ReviewID, "completion_id": "wrong", "basis_digest": status.BasisDigest, "note": "Needs correction"}
	encoded, _ := json.Marshal(payload)
	rejected := localUIRequest(s, "POST", "/api/decision", string(encoded), true, true)
	if rejected.Code != 409 {
		t.Fatalf("stale completion %d %s", rejected.Code, rejected.Body.String())
	}
	payload["completion_id"] = status.CompletionID
	for _, note := range []any{nil, "", " \t\n\u00a0"} {
		if note == nil {
			delete(payload, "note")
		} else {
			payload["note"] = note
		}
		encoded, _ = json.Marshal(payload)
		w := localUIRequest(s, "POST", "/api/decision", string(encoded), true, true)
		if w.Code != 400 || !strings.Contains(w.Body.String(), "validation_failed") {
			t.Fatalf("blank request-changes note %#v: %d %s", note, w.Code, w.Body.String())
		}
	}
	payload["note"] = "Needs correction"
	encoded, _ = json.Marshal(payload)
	accepted := localUIRequest(s, "POST", "/api/decision", string(encoded), true, true)
	if accepted.Code != 200 {
		t.Fatalf("decision %d %s", accepted.Code, accepted.Body.String())
	}
	var decisionResponse struct {
		Data struct {
			Review struct {
				ID            string `json:"id"`
				ReportID      string `json:"report_id"`
				HumanDecision string `json:"human_decision"`
				Note          string `json:"note"`
				CreatedAt     string `json:"created_at"`
			} `json:"review"`
		} `json:"data"`
	}
	if err := json.Unmarshal(accepted.Body.Bytes(), &decisionResponse); err != nil {
		t.Fatal(err)
	}
	recorded := decisionResponse.Data.Review
	if recorded.ID != status.ReviewID || recorded.ReportID != status.CompletionID || recorded.HumanDecision != "reject" || recorded.Note != "Needs correction" || recorded.CreatedAt == "" {
		t.Fatalf("missing exact decision record: %+v", recorded)
	}
	repeat := localUIRequest(s, "POST", "/api/decision", string(encoded), true, true)
	if repeat.Code != 409 {
		t.Fatalf("duplicate decision %d %s", repeat.Code, repeat.Body.String())
	}
	traversal := localUIRequest(s, "GET", "/api/context?ref="+work.Key+"&path=../../state.db", "", true, false)
	if traversal.Code != 404 {
		t.Fatalf("path escape %d %s", traversal.Code, traversal.Body.String())
	}
}
func TestLocalUIDecodeBoundedStrictJSON(t *testing.T) {
	for _, body := range []string{`{"extra":1}`, `{} {}`, `null`, strings.Repeat(" ", 65537) + `{}`} {
		req := httptest.NewRequest("POST", "http://127.0.0.1", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		var dst struct {
			Ref string `json:"ref"`
		}
		if decodeLocalUIRequest(w, req, &dst) {
			t.Fatalf("accepted %q", body[:min(len(body), 30)])
		}
	}
}

func TestLocalUICrossSiteLinkNavigationAndCookieIsolation(t *testing.T) {
	s, _ := localUITestServer(t)
	other, _ := localUITestServer(t)
	if s.cookieName() == other.cookieName() {
		t.Fatal("sessions share a cookie across loopback ports")
	}
	for _, path := range []string{"/", "/api/session", "/api/works"} {
		r := httptest.NewRequest("GET", "http://"+s.host+path, nil)
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Sec-Fetch-Mode", "navigate")
		r.Header.Set("Sec-Fetch-Dest", "document")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if path == "/" {
			if w.Code != 200 {
				t.Fatalf("review link cannot open: %d %s", w.Code, w.Body.String())
			}
		} else if w.Code != 403 {
			t.Fatalf("cross-site API navigation accepted: %s", path)
		}
	}
	r := httptest.NewRequest("GET", "http://"+other.host+"/api/meta", nil)
	r.AddCookie(&http.Cookie{Name: s.cookieName(), Value: s.sessionToken})
	w := httptest.NewRecorder()
	other.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("another UI session cookie was accepted")
	}
}

func TestLocalUIBoundWorkspaceAndReadOnlyInspection(t *testing.T) {
	s, work := localUITestServer(t)
	beta, err := s.store.CreateWorkspace(t.Context(), "Other workspace")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := s.store.CreateQuickWork(t.Context(), beta.ID, local.QuickWorkInput{Title: "Private other work", AcceptanceCriteria: []string{"Other"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.SelectWorkspace(t.Context(), beta.ID); err != nil {
		t.Fatal(err)
	}
	before, err := s.store.Audit(t.Context(), s.selection.Workspace.ID, work.Key)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/meta", "/api/works", "/api/work?ref=" + work.Key, "/api/context?ref=" + work.Key} {
		w := localUIRequest(s, "GET", path, "", true, false)
		if w.Code != 200 {
			t.Fatalf("%s %d %s", path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), foreign.Title) {
			t.Fatal("foreign workspace exposed")
		}
	}
	if w := localUIRequest(s, "GET", "/api/work?ref="+foreign.Key, "", true, false); w.Code != 404 {
		t.Fatalf("foreign work status %d", w.Code)
	}
	after, err := s.store.Audit(t.Context(), s.selection.Workspace.ID, work.Key)
	if err != nil || len(after) != len(before) {
		t.Fatalf("GET wrote audit: %d -> %d %v", len(before), len(after), err)
	}
	current, err := s.store.Current(t.Context())
	if err != nil || current.Workspace.ID != beta.ID {
		t.Fatal("GET changed selected workspace")
	}
	upgrade, err := s.store.PendingStoreUpgrade(t.Context())
	if err != nil || upgrade == nil {
		t.Fatal("GET upgraded legacy store")
	}
}

func TestLocalUIRejectsChangedReviewAndBasis(t *testing.T) {
	s, work := localUITestServer(t)
	body := map[string]any{"context_digest": work.ContextDigest, "agent": map[string]any{"name": "builder"}, "criteria": []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"path": "proof.txt", "line": 1}}}}
	old, err := s.store.SubmitDelivery(t.Context(), s.selection.Workspace.ID, work.Key, body)
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range []map[string]any{
		{"ref": work.Key, "decision": "reject", "review_id": old.ID, "completion_id": old.ReportID, "basis_digest": "wrong", "note": "Requires changes"},
		{"ref": work.Key, "decision": "reject", "review_id": "wrong", "completion_id": old.ReportID, "note": "Requires changes"},
	} {
		encoded, _ := json.Marshal(payload)
		w := localUIRequest(s, "POST", "/api/decision", string(encoded), true, true)
		if w.Code != 409 {
			t.Fatalf("stale pin %d %s", w.Code, w.Body.String())
		}
	}
	if _, err := s.store.SubmitDelivery(t.Context(), s.selection.Workspace.ID, work.Key, body); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"ref": work.Key, "decision": "reject", "review_id": old.ID, "completion_id": old.ReportID, "note": "Requires changes"}
	encoded, _ := json.Marshal(payload)
	w := localUIRequest(s, "POST", "/api/decision", string(encoded), true, true)
	if w.Code != 409 {
		t.Fatalf("outdated review %d %s", w.Code, w.Body.String())
	}
}

type localUIStartedWriter struct{ messages chan []byte }

func (w localUIStartedWriter) Write(data []byte) (int, error) {
	copy := append([]byte(nil), data...)
	w.messages <- copy
	return len(data), nil
}

func TestLocalUILifecycleAndLocalOpenDiscovery(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	root, _ = filepath.EvalSymlinks(root)
	state := t.TempDir()
	store, err := local.Open(filepath.Join(state, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	selection, err := store.Initialize(t.Context(), local.InitInput{WorkspaceName: "Bound workspace", Username: "reviewer", DisplayName: "Reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateQuickWork(t.Context(), selection.Workspace.ID, local.QuickWorkInput{Title: "UI lifecycle", AcceptanceCriteria: []string{"Exact bound URL"}})
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	cfg := config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: state}}
	cfg.SetProjectWorkspace(root, config.CurrentWorkspace{ID: selection.Workspace.ID, Slug: selection.Workspace.Slug, Name: selection.Workspace.Name})
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := cfg.SaveTo(cfgPath); err != nil {
		t.Fatal(err)
	}
	messages := make(chan []byte, 4)
	deps := &Deps{ConfigPath: cfgPath, WorkingDir: root, Stdout: localUIStartedWriter{messages}, Stderr: io.Discard, Stdin: strings.NewReader(""), UserHomeDir: func() (string, error) { return t.TempDir(), nil }}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cmd := NewRootCommand(deps)
	cmd.SetContext(ctx)
	done := make(chan int, 1)
	go func() { done <- ExecuteForCode(cmd, "ui", work.Key, "--no-open", "--port", "0", "--json") }()
	var started []byte
	select {
	case started = <-messages:
	case <-time.After(10 * time.Second):
		t.Fatal("UI did not start")
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			URL      string `json:"url"`
			RepoRoot string `json:"repo_root"`
		} `json:"data"`
	}
	if err := json.Unmarshal(started, &envelope); err != nil || !envelope.OK {
		t.Fatalf("startup: %s %v", started, err)
	}
	if !strings.HasPrefix(envelope.Data.URL, "http://127.0.0.1:") || strings.Contains(envelope.Data.URL, "token") || envelope.Data.RepoRoot != root {
		t.Fatalf("bad URL: %s", started)
	}
	var out bytes.Buffer
	openDeps := &Deps{ConfigPath: cfgPath, WorkingDir: root, Stdout: &out, Stderr: io.Discard, Stdin: strings.NewReader(""), Opener: func(string) error { t.Error("--print launched browser"); return nil }}
	if code := ExecuteForCode(NewRootCommand(openDeps), "open", work.Key, "--print", "--json"); code != 0 {
		t.Fatalf("open %d %s", code, out.String())
	}
	if !strings.Contains(out.String(), envelope.Data.URL) {
		t.Fatalf("open selected different UI: %s", out.String())
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("shutdown %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("UI did not stop")
	}
	files, err := os.ReadDir(filepath.Join(state, "ui"))
	if err != nil || len(files) != 0 {
		t.Fatalf("discovery not cleaned: %v %v", files, err)
	}
	out.Reset()
	if code := ExecuteForCode(NewRootCommand(openDeps), "open", work.Key, "--print", "--json"); code != 5 || !strings.Contains(out.String(), "specgate ui --no-open") {
		t.Fatalf("missing live UI: %d %s", code, out.String())
	}
}

func TestLocalUIDiscoveryRejectsUnboundAndUnsafeDescriptors(t *testing.T) {
	s, _ := localUITestServer(t)
	state := t.TempDir()
	s.deps.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if err := (config.Config{Mode: config.ModeLocal, Local: config.LocalStore{Path: state}}).SaveTo(s.deps.ConfigPath); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(s)
	defer server.Close()
	u, _ := url.Parse(server.URL)
	s.host = u.Host
	descriptor := localUIDiscovery{URL: server.URL, SessionID: s.id, WorkspaceID: s.selection.Workspace.ID, RepoRoot: s.repoRoot}
	path, err := writeLocalUIDiscovery(s.deps, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if target, err := discoverLocalUI(t.Context(), s.deps, s.selection.Workspace.ID, s.repoRoot); err != nil || target != server.URL {
		t.Fatalf("live discovery %s %v", target, err)
	}
	if _, err := discoverLocalUI(t.Context(), s.deps, "foreign", s.repoRoot); err == nil {
		t.Fatal("foreign workspace discovery accepted")
	}
	if _, err := discoverLocalUI(t.Context(), s.deps, s.selection.Workspace.ID, s.repoRoot+"/foreign"); err == nil {
		t.Fatal("foreign repository discovery accepted")
	}
	for _, mutate := range []func(*localUIDiscovery){func(d *localUIDiscovery) { d.URL = "https://attacker.invalid" }, func(d *localUIDiscovery) { d.URL = server.URL + "/arbitrary" }, func(d *localUIDiscovery) { d.SessionID = strings.Repeat("0", 64) }, func(d *localUIDiscovery) { d.URL = strings.Replace(server.URL, "127.0.0.1", "localhost", 1) }} {
		bad := descriptor
		mutate(&bad)
		data, _ := json.Marshal(bad)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := discoverLocalUI(t.Context(), s.deps, s.selection.Workspace.ID, s.repoRoot); err == nil {
			t.Fatalf("unsafe discovery accepted %+v", bad)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(state, "state.db"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverLocalUI(t.Context(), s.deps, s.selection.Workspace.ID, s.repoRoot); err == nil {
		t.Fatal("symlink discovery accepted")
	}
}

func TestLocalUIDiscoveryPrivatePermissionsByPlatform(t *testing.T) {
	for _, platform := range []struct {
		goos          string
		sharedAllowed bool
	}{
		{"darwin", false}, {"linux", false}, {"freebsd", false}, {"windows", true},
	} {
		for _, mode := range []os.FileMode{0600, 0700} {
			if !localUIPrivatePermissions(mode, platform.goos) {
				t.Errorf("%s rejected private mode %o", platform.goos, mode)
			}
		}
		for _, mode := range []os.FileMode{0644, 0755, 0666, 0777} {
			if got := localUIPrivatePermissions(mode, platform.goos); got != platform.sharedAllowed {
				t.Errorf("%s mode %o: got %v want %v", platform.goos, mode, got, platform.sharedAllowed)
			}
		}
	}
}

func TestLocalUIRejectsSourceBasisAndLabelsHistoricalVerification(t *testing.T) {
	s, work := localUITestServer(t)
	ctx := t.Context()
	report := map[string]any{
		"context_digest": work.ContextDigest, "agent": map[string]any{"name": "builder"},
		"git_receipt": map[string]any{"availability": "available", "freshness_scope": "local_checkout", "head_revision": "old-head"},
		"checks":      []any{map[string]any{"name": "unit", "command": "exit 99", "source": "specgate_cli", "status": "pass"}},
		"criteria":    []any{map[string]any{"criterion_id": "local-1", "claim": "satisfied", "evidence": map[string]any{"heading": "Evidence"}}},
	}
	if _, err := s.store.SubmitDelivery(ctx, work.WorkspaceID, work.Key, report); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.CreateCheckpointWithFiles(ctx, work.WorkspaceID, work.Key, "source", nil, "Historical note"); err != nil {
		t.Fatal(err)
	}
	read := func() changeStatusResult {
		t.Helper()
		response := localUIRequest(s, "GET", "/api/work?ref="+work.Key, "", true, false)
		var envelope struct {
			Data struct {
				Status changeStatusResult `json:"status"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || response.Code != 200 {
			t.Fatalf("inspect review: %d %s (%v)", response.Code, response.Body.String(), err)
		}
		if !strings.Contains(response.Body.String(), `"status":"pass"`) {
			t.Fatalf("historical passing test disappeared: %s", response.Body.String())
		}
		return envelope.Data.Status
	}
	source := read()
	if source.BasisDigest == "" || source.HistoricalVerification || source.NextActor != "human_reviewer" {
		t.Fatalf("bad source basis: %+v", source)
	}
	s.deps.RunCheckCommand = func(context.Context, string) (int, string) {
		t.Fatal("review inspection or token refusal re-executed source evidence")
		return 0, ""
	}
	if _, err := s.store.InitializeTransferGeneration(ctx); err != nil {
		t.Fatal(err)
	}
	destination := read()
	if !destination.HistoricalVerification || destination.BasisDigest == "" || destination.BasisDigest == source.BasisDigest ||
		destination.ReviewID != source.ReviewID || destination.CompletionID != source.CompletionID || destination.NextActor != source.NextActor ||
		!strings.Contains(destination.Assurance, "reproduced by the source SpecGate CLI") {
		t.Fatalf("receiving UI review: %+v", destination)
	}
	payload, err := json.Marshal(map[string]string{
		"ref": work.Key, "decision": "approve", "review_id": source.ReviewID,
		"completion_id": source.CompletionID, "basis_digest": source.BasisDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	rejected := localUIRequest(s, "POST", "/api/decision", string(payload), true, true)
	if rejected.Code != 409 {
		t.Fatalf("UI accepted a source basis: %d %s", rejected.Code, rejected.Body.String())
	}
	if current := read(); current.BasisDigest != destination.BasisDigest || current.State != source.State || current.RecordedBasis != nil {
		t.Fatalf("pending review changed: %+v", current)
	}
}
