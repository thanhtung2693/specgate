package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	storagedb "github.com/specgate/doc-registry/internal/storage/db"
	"github.com/specgate/doc-registry/internal/workboard"
)

// newAttachmentsServer wires an in-process handler with a real Postgres DB and the
// ArtifactAttachments store (no S3 needed — attachments are metadata rows).
func newAttachmentsServer(t *testing.T) http.Handler {
	t.Helper()
	db := newTestGormDB(t)
	rt := &Router{
		Handlers: &Handlers{
			ArtifactAttachments: storagedb.NewArtifactAttachmentRepository(db),
			WorkBoard:           storagedb.NewWorkBoardRepository(db),
		},
		Config: testConfig(),
	}
	board := rt.Handlers.WorkBoard
	for _, id := range []string{"feat-1", "feat-9"} {
		if _, err := board.CreateFeature(context.Background(), workboard.Feature{ID: id, WorkspaceID: "ws-test", Key: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	return rt.Build()
}

func TestCreateFeatureAttachment_LinkHappyPath(t *testing.T) {
	t.Parallel()
	srv := newAttachmentsServer(t)
	rr := postJSON(t, srv, "/features/feat-1/attachments?workspace_id=ws-test",
		`{"kind":"link","url":"https://figma.com/x","title":"Mock","audience":"both"}`)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"feature_id":"feat-1"`) ||
		!strings.Contains(body, `"audience":"both"`) ||
		!strings.Contains(body, `"id"`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestCreateFeatureAttachment_DefaultsToGateAudience(t *testing.T) {
	t.Parallel()
	srv := newAttachmentsServer(t)
	rr := postJSON(t, srv, "/features/feat-1/attachments?workspace_id=ws-test",
		`{"kind":"link","url":"https://example.com"}`)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"audience":"gate"`) {
		t.Fatalf("expected default gate audience: %s", rr.Body.String())
	}
}

func TestCreateFeatureAttachment_LinkRequiresURL(t *testing.T) {
	t.Parallel()
	srv := newAttachmentsServer(t)
	rr := postJSON(t, srv, "/features/feat-1/attachments?workspace_id=ws-test", `{"kind":"link"}`)
	if rr.Code != 400 {
		t.Fatalf("status=%d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestCreateFeatureAttachment_RejectsNonHttpURL(t *testing.T) {
	t.Parallel()
	srv := newAttachmentsServer(t)
	// A javascript: URL would become a stored-XSS vector if rendered as a link.
	for _, bad := range []string{
		`{"kind":"link","url":"javascript:alert(1)"}`,
		`{"kind":"link","url":"data:text/html,<script>alert(1)</script>"}`,
		`{"kind":"link","url":"not-a-url"}`,
	} {
		rr := postJSON(t, srv, "/features/feat-1/attachments?workspace_id=ws-test", bad)
		if rr.Code != 400 {
			t.Fatalf("status=%d, want 400 for %s; body=%s", rr.Code, bad, rr.Body.String())
		}
	}
}

func TestCreateFeatureAttachment_FileRequiresGovernanceFileID(t *testing.T) {
	t.Parallel()
	srv := newAttachmentsServer(t)
	rr := postJSON(t, srv, "/features/feat-1/attachments?workspace_id=ws-test", `{"kind":"image"}`)
	if rr.Code != 400 {
		t.Fatalf("status=%d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestFeatureAttachments_RejectWrongOrMissingWorkspace(t *testing.T) {
	t.Parallel()
	srv := newAttachmentsServer(t)
	wrong := postJSON(t, srv, "/features/feat-1/attachments?workspace_id=ws-other", `{"kind":"link","url":"https://example.com"}`)
	if wrong.Code != http.StatusNotFound {
		t.Fatalf("wrong workspace status=%d, want 404; body=%s", wrong.Code, wrong.Body.String())
	}
	missing := postJSON(t, srv, "/features/feat-1/attachments", `{"kind":"link","url":"https://example.com"}`)
	if missing.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing workspace status=%d, want 422; body=%s", missing.Code, missing.Body.String())
	}
}

func TestListAndDeleteFeatureAttachments(t *testing.T) {
	t.Parallel()
	srv := newAttachmentsServer(t)
	mk := postJSON(t, srv, "/features/feat-9/attachments?workspace_id=ws-test",
		`{"kind":"image","governance_file_id":"pf-1","audience":"gate"}`)
	if mk.Code != 200 {
		t.Fatalf("create status=%d body=%s", mk.Code, mk.Body.String())
	}
	id := jsonField(t, mk.Body.String(), "id")

	list := getReq(t, srv, "/features/feat-9/attachments?workspace_id=ws-test")
	if list.Code != 200 || !strings.Contains(list.Body.String(), `"governance_file_id":"pf-1"`) {
		t.Fatalf("list wrong: %d %s", list.Code, list.Body.String())
	}

	del := deleteReq(t, srv, "/attachments/"+id+"?workspace_id=ws-test")
	if del.Code != 200 {
		t.Fatalf("delete status=%d body=%s", del.Code, del.Body.String())
	}
	// Deleting an unknown id is 404.
	if again := deleteReq(t, srv, "/attachments/"+id+"?workspace_id=ws-test"); again.Code != 404 {
		t.Fatalf("re-delete status=%d, want 404", again.Code)
	}

	empty := getReq(t, srv, "/features/feat-9/attachments?workspace_id=ws-test")
	if !strings.Contains(empty.Body.String(), `"items":[]`) {
		t.Fatalf("expected empty items: %s", empty.Body.String())
	}
}
