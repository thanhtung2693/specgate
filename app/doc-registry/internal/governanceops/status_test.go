package governanceops

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/workboard"
)

// fakeWorkBoardReader is a minimal in-memory WorkBoardReader for tests.
type fakeWorkBoardReader struct {
	crs []workboard.ChangeRequest
}

func (f *fakeWorkBoardReader) ListChangeRequests(_ context.Context, _ bool) ([]workboard.ChangeRequest, error) {
	return f.crs, nil
}

func (f *fakeWorkBoardReader) GetChangeRequest(_ context.Context, id string) (*workboard.ChangeRequest, error) {
	for i := range f.crs {
		if f.crs[i].ID == id {
			return &f.crs[i], nil
		}
	}
	return nil, errors.New("not found")
}

func (f *fakeWorkBoardReader) GetFeature(_ context.Context, _ string) (*workboard.Feature, error) {
	return nil, workboard.ErrNotFound
}

func (f *fakeWorkBoardReader) ListAcceptanceCriteria(_ context.Context, _ string) ([]workboard.AcceptanceCriterion, error) {
	return nil, nil
}

func (f *fakeWorkBoardReader) ListGateRuns(_ context.Context, _ string, _ int) ([]workboard.GateRun, error) {
	return nil, nil
}

func (f *fakeWorkBoardReader) ListStaleWarnings(_ context.Context, _ workboard.StaleWarningFilter) ([]workboard.StaleWarning, error) {
	return nil, nil
}

// fakeTrackerReader returns one GitHub integration and one tracker link for cr-1.
type fakeTrackerReader struct {
	integrations []integrations.Integration
	links        map[string][]integrations.TrackerLink // keyed by changeRequestID
}

func (f *fakeTrackerReader) List(_ context.Context) ([]integrations.Integration, error) {
	return f.integrations, nil
}

func (f *fakeTrackerReader) ListTrackerLinks(_ context.Context, changeRequestID string) ([]integrations.TrackerLink, error) {
	return f.links[changeRequestID], nil
}

func newStatusTestService() *Service {
	cr := workboard.ChangeRequest{
		ID:    "cr-1",
		Key:   "CR-101",
		Title: "Test CR",
		Phase: workboard.BoardPhaseReady,
	}
	link := integrations.TrackerLink{
		ID:              "link-1",
		IntegrationID:   "intg-1",
		ChangeRequestID: "cr-1",
		ExternalKey:     "42",
		URL:             "https://github.com/acme/shop/issues/42",
		UpdatedAt:       time.Now(),
	}
	return &Service{
		WorkBoard: &fakeWorkBoardReader{crs: []workboard.ChangeRequest{cr}},
		Trackers: &fakeTrackerReader{
			integrations: []integrations.Integration{
				{ID: "intg-1", Provider: "github"},
			},
			links: map[string][]integrations.TrackerLink{
				"cr-1": {link},
			},
		},
	}
}

func TestResolveWorkRefAcceptsIDKeyAndIssueURL(t *testing.T) {
	t.Parallel()
	svc := newStatusTestService()

	for _, ref := range []string{
		"cr-1",
		"CR-101",
		"https://github.com/acme/shop/issues/42",
	} {
		got, err := svc.ResolveWorkRef(context.Background(), ResolveWorkRefInput{Ref: ref})
		if err != nil {
			t.Fatalf("ResolveWorkRef(%q): %v", ref, err)
		}
		if got.ChangeRequestID != "cr-1" {
			t.Fatalf("ResolveWorkRef(%q) id = %q, want cr-1", ref, got.ChangeRequestID)
		}
	}
}

func TestResolveWorkRefAcceptsContextPackURI(t *testing.T) {
	t.Parallel()
	svc := newStatusTestService()

	// The specgate://context-pack/<cr-id> URI the system emits (with optional
	// fe/be lane suffix) must resolve back to the change request, so `work show`
	// output is accepted as `work context` input.
	for _, ref := range []string{
		"specgate://context-pack/cr-1",
		"specgate://context-pack/cr-1/fe",
		"specgate://context-pack/cr-1/be",
	} {
		got, err := svc.ResolveWorkRef(context.Background(), ResolveWorkRefInput{Ref: ref})
		if err != nil {
			t.Fatalf("ResolveWorkRef(%q): %v", ref, err)
		}
		if got.ChangeRequestID != "cr-1" {
			t.Fatalf("ResolveWorkRef(%q) id = %q, want cr-1", ref, got.ChangeRequestID)
		}
	}

	// The artifact-scoped variant is not change-request-scoped and must not be
	// mis-resolved to a change request.
	if _, err := svc.ResolveWorkRef(context.Background(), ResolveWorkRefInput{Ref: "specgate://context-pack/artifact/some-artifact-id"}); err == nil {
		t.Fatalf("artifact-scoped context-pack URI should not resolve to a change request")
	}
}

func TestResolveWorkRefBareIssueKeyRequiresProvider(t *testing.T) {
	t.Parallel()
	svc := newStatusTestService()
	_, err := svc.ResolveWorkRef(context.Background(), ResolveWorkRefInput{Ref: "SHOP-42"})
	if !errors.Is(err, ErrProviderRequired) {
		t.Fatalf("error = %v, want ErrProviderRequired", err)
	}
}
