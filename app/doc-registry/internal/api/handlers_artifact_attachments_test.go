package api

import (
	"net/http"
	"strings"
	"testing"

	storagedb "github.com/specgate/doc-registry/internal/storage/db"
)

// newAttachmentsServer wires an in-process handler with a real Postgres DB and the
// ArtifactAttachments store (no S3 needed — attachments are metadata rows).
func newAttachmentsServer(t *testing.T) http.Handler {
	t.Helper()
	db := newTestGormDB(t)
	rt := &Router{
		Handlers: &Handlers{
			ArtifactAttachments: storagedb.NewArtifactAttachmentRepository(db),
		},
		Config: testConfig(),
	}
	return rt.Build()
}

func TestCreateFeatureAttachment_LinkHappyPath(t *testing.T) {
	t.Parallel()
	srv := newAttachmentsServer(t)
	rr := postJSON(t, srv, "/features/feat-1/attachments",
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
	rr := postJSON(t, srv, "/features/feat-1/attachments",
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
	rr := postJSON(t, srv, "/features/feat-1/attachments", `{"kind":"link"}`)
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
		rr := postJSON(t, srv, "/features/feat-1/attachments", bad)
		if rr.Code != 400 {
			t.Fatalf("status=%d, want 400 for %s; body=%s", rr.Code, bad, rr.Body.String())
		}
	}
}

func TestCreateFeatureAttachment_FileRequiresGovernanceFileID(t *testing.T) {
	t.Parallel()
	srv := newAttachmentsServer(t)
	rr := postJSON(t, srv, "/features/feat-1/attachments", `{"kind":"image"}`)
	if rr.Code != 400 {
		t.Fatalf("status=%d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestListAndDeleteFeatureAttachments(t *testing.T) {
	t.Parallel()
	srv := newAttachmentsServer(t)
	mk := postJSON(t, srv, "/features/feat-9/attachments",
		`{"kind":"image","governance_file_id":"pf-1","audience":"gate"}`)
	if mk.Code != 200 {
		t.Fatalf("create status=%d body=%s", mk.Code, mk.Body.String())
	}
	id := jsonField(t, mk.Body.String(), "id")

	list := getReq(t, srv, "/features/feat-9/attachments")
	if list.Code != 200 || !strings.Contains(list.Body.String(), `"governance_file_id":"pf-1"`) {
		t.Fatalf("list wrong: %d %s", list.Code, list.Body.String())
	}

	del := deleteReq(t, srv, "/attachments/"+id)
	if del.Code != 200 {
		t.Fatalf("delete status=%d body=%s", del.Code, del.Body.String())
	}
	// Deleting an unknown id is 404.
	if again := deleteReq(t, srv, "/attachments/"+id); again.Code != 404 {
		t.Fatalf("re-delete status=%d, want 404", again.Code)
	}

	empty := getReq(t, srv, "/features/feat-9/attachments")
	if !strings.Contains(empty.Body.String(), `"items":[]`) {
		t.Fatalf("expected empty items: %s", empty.Body.String())
	}
}
