package integrations

import (
	"context"
	"errors"
	"testing"

	"github.com/specgate/doc-registry/internal/workboard"
)

func TestCreate_RequiresWorkspace(t *testing.T) {
	t.Parallel()
	store := &oauthResolveFakeStore{}
	svc := NewService(store)

	_, err := svc.Create(context.Background(), CreateInput{
		Provider: ProviderGitLab,
		Name:     "Acme GitLab",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Create error = %v, want ErrValidation", err)
	}
	if store.createdIntegration != nil {
		t.Fatal("unscoped integration create reached the store")
	}
}

type fakeWorkBoard struct {
	items    []workboard.ChangeRequest
	cr       *workboard.ChangeRequest
	feature  *workboard.Feature
	criteria []workboard.AcceptanceCriterion
}

type deliveryLinksStore struct {
	Store
	rows      []DeliveryLink
	requested string
}

func (s *deliveryLinksStore) ListDeliveryLinksByChangeRequest(_ context.Context, changeRequestID string) ([]DeliveryLink, error) {
	s.requested = changeRequestID
	return s.rows, nil
}

func TestListDeliveryLinks_ReadsPersistedHeadsWithoutComputingAVerdict(t *testing.T) {
	store := &deliveryLinksStore{rows: []DeliveryLink{{
		ExternalKey: "owner/repo#42", HeadSHA: "submitted-head", MergeCommitSHA: "provider-merge",
	}}}
	links, err := NewService(store).ListDeliveryLinks(context.Background(), "cr-123")
	if err != nil {
		t.Fatal(err)
	}
	if store.requested != "cr-123" || len(links) != 1 || links[0].HeadSHA != "submitted-head" || links[0].MergeCommitSHA != "provider-merge" {
		t.Fatalf("links = %#v requested=%q", links, store.requested)
	}
}

func (f *fakeWorkBoard) ListChangeRequests(context.Context, bool) ([]workboard.ChangeRequest, error) {
	return f.items, nil
}

func (f *fakeWorkBoard) GetChangeRequest(_ context.Context, id string) (*workboard.ChangeRequest, error) {
	if f.cr != nil && (id == f.cr.ID || id == f.cr.Key) {
		return f.cr, nil
	}
	for i := range f.items {
		if f.items[i].ID == id || f.items[i].Key == id {
			return &f.items[i], nil
		}
	}
	return nil, workboard.ErrNotFound
}

func (f *fakeWorkBoard) GetFeature(_ context.Context, id string) (*workboard.Feature, error) {
	if f.feature != nil && id == f.feature.ID {
		return f.feature, nil
	}
	return nil, workboard.ErrNotFound
}

func (f *fakeWorkBoard) ListAcceptanceCriteria(context.Context, string) ([]workboard.AcceptanceCriterion, error) {
	return f.criteria, nil
}

func mrPayload(title, description, sourceBranch string) normalizedDelivery {
	return normalizedDelivery{
		Title:        title,
		Description:  description,
		SourceBranch: sourceBranch,
	}
}

// An exact machine marker is a deliberate link.
func TestMatchChangeRequest_WorkMarkerLinksEvenWhenTitleMentionsAnotherCR(t *testing.T) {
	t.Parallel()
	svc := NewServiceWithWorkBoard(nil, &fakeWorkBoard{items: []workboard.ChangeRequest{
		{ID: "cr-a-id", Key: "CR-ALPHA", FeatureID: "feat-a"},
		{ID: "cr-b-id", Key: "CR-BETA", FeatureID: "feat-b"},
	}})
	// Title mentions ALPHA, but the author explicitly linked BETA.
	payload := mrPayload("CR-ALPHA groundwork", "Implements the thing.\n\n<!-- specgate-work-ref: CR-BETA -->", "")
	cr, err := svc.matchChangeRequest(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if cr.Key != "CR-BETA" {
		t.Fatalf("matched %q, want CR-BETA", cr.Key)
	}
}

// The marker links even when nothing else mentions the work item.
func TestMatchChangeRequest_WorkMarkerMatchesWithoutOtherSignals(t *testing.T) {
	t.Parallel()
	svc := NewServiceWithWorkBoard(nil, &fakeWorkBoard{items: []workboard.ChangeRequest{
		{ID: "cr-pay-id", Key: "CR-PAYMENTS", FeatureID: "feat-pay"},
	}})
	payload := mrPayload("Refactor checkout", "Body text.\n\n<!-- specgate-work-ref: CR-PAYMENTS -->", "feature/checkout")
	cr, err := svc.matchChangeRequest(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if cr.ID != "cr-pay-id" {
		t.Fatalf("matched %q, want cr-pay-id", cr.ID)
	}
}

// A marker can also reference the change-request id directly.
func TestMatchChangeRequest_WorkMarkerMatchesByID(t *testing.T) {
	t.Parallel()
	svc := NewServiceWithWorkBoard(nil, &fakeWorkBoard{items: []workboard.ChangeRequest{
		{ID: "cr-uuid-123", Key: "CR-GAMMA", FeatureID: "feat-g"},
	}})
	payload := mrPayload("unrelated title", "<!-- specgate-work-ref: cr-uuid-123 -->", "")
	cr, err := svc.matchChangeRequest(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if cr.ID != "cr-uuid-123" {
		t.Fatalf("matched %q, want cr-uuid-123", cr.ID)
	}
}

func TestMatchChangeRequest_DoesNotNormalizePunctuationInReference(t *testing.T) {
	t.Parallel()
	svc := NewServiceWithWorkBoard(nil, &fakeWorkBoard{items: []workboard.ChangeRequest{
		{ID: "cr-a-id", Key: "CR-A", FeatureID: "feat-a"},
	}})
	payload := mrPayload("unrelated", "<!-- specgate-work-ref: CR_A -->", "")
	if _, err := svc.matchChangeRequest(context.Background(), payload); err == nil {
		t.Fatal("CR_A must not match canonical key CR-A")
	}
}

// Without a marker, matching must not infer a link from title, branch, or URL
// text. That avoids accidental delivery attribution from prose coincidence.
func TestMatchChangeRequest_RequiresExplicitWorkMarker(t *testing.T) {
	t.Parallel()
	svc := NewServiceWithWorkBoard(nil, &fakeWorkBoard{items: []workboard.ChangeRequest{
		{ID: "cr-x-id", Key: "CR-XRAY", FeatureID: "feat-x"},
	}})
	payload := mrPayload("CR-XRAY implement", "no marker here", "feature/CR-XRAY")
	if _, err := svc.matchChangeRequest(context.Background(), payload); err == nil {
		t.Fatal("expected unmatched delivery without exact work marker")
	}
}

// Delivery feedback events use a provider-neutral vocabulary so a GitHub PR and
// a GitLab MR surface identically to downstream consumers.
func TestDeliveryFeedbackEventVocabularyIsProviderNeutral(t *testing.T) {
	t.Parallel()
	cases := []struct {
		got  string
		want string
	}{
		{FeedbackEventPROpened, "delivery.pr_opened"},
		{FeedbackEventPRMerged, "delivery.pr_merged"},
		{FeedbackEventPRClosed, "delivery.pr_closed"},
		{FeedbackEventPRUnmatched, "delivery.pr_unmatched"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Fatalf("delivery feedback event = %q, want %q", c.got, c.want)
		}
	}
}
