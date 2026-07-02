package integrations

import (
	"context"
	"testing"

	"github.com/specgate/doc-registry/internal/workboard"
)

type fakeWorkBoard struct {
	items   []workboard.ChangeRequest
	cr      *workboard.ChangeRequest
	feature *workboard.Feature
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

func mrPayload(title, description, sourceBranch string) normalizedDelivery {
	return normalizedDelivery{
		Title:        title,
		Description:  description,
		SourceBranch: sourceBranch,
	}
}

// An explicit `fixes SPECGATE-{key}` footer is a deliberate link and must win over a
// fuzzy title/branch coincidence.
func TestMatchChangeRequest_FixesFooterTakesPrecedenceOverFuzzy(t *testing.T) {
	t.Parallel()
	svc := NewServiceWithWorkBoard(nil, &fakeWorkBoard{items: []workboard.ChangeRequest{
		{ID: "cr-a-id", Key: "CR-ALPHA", FeatureID: "feat-a"},
		{ID: "cr-b-id", Key: "CR-BETA", FeatureID: "feat-b"},
	}})
	// Title fuzzily matches ALPHA, but the author explicitly footnoted BETA.
	payload := mrPayload("CR-ALPHA groundwork", "Implements the thing.\n\nfixes SPECGATE-CR-BETA", "")
	cr, err := svc.matchChangeRequest(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if cr.Key != "CR-BETA" {
		t.Fatalf("matched %q, want CR-BETA (footer precedence)", cr.Key)
	}
}

// The footer links even when nothing in the title/branch/url would fuzzy-match,
// and the SPECGATE- prefix is case-insensitive.
func TestMatchChangeRequest_FixesFooterMatchesWhenFuzzyWouldNot(t *testing.T) {
	t.Parallel()
	svc := NewServiceWithWorkBoard(nil, &fakeWorkBoard{items: []workboard.ChangeRequest{
		{ID: "cr-pay-id", Key: "CR-PAYMENTS", FeatureID: "feat-pay"},
	}})
	payload := mrPayload("Refactor checkout", "Body text.\n\nfixes specgate-CR-PAYMENTS", "feature/checkout")
	cr, err := svc.matchChangeRequest(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if cr.ID != "cr-pay-id" {
		t.Fatalf("matched %q, want cr-pay-id", cr.ID)
	}
}

// A footer can also reference the change-request id directly.
func TestMatchChangeRequest_FixesFooterMatchesByID(t *testing.T) {
	t.Parallel()
	svc := NewServiceWithWorkBoard(nil, &fakeWorkBoard{items: []workboard.ChangeRequest{
		{ID: "cr-uuid-123", Key: "CR-GAMMA", FeatureID: "feat-g"},
	}})
	payload := mrPayload("unrelated title", "fixes SPECGATE-cr-uuid-123", "")
	cr, err := svc.matchChangeRequest(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if cr.ID != "cr-uuid-123" {
		t.Fatalf("matched %q, want cr-uuid-123", cr.ID)
	}
}

// Without a footer, matching falls back to the existing fuzzy text scan.
func TestMatchChangeRequest_FallsBackToFuzzyWithoutFooter(t *testing.T) {
	t.Parallel()
	svc := NewServiceWithWorkBoard(nil, &fakeWorkBoard{items: []workboard.ChangeRequest{
		{ID: "cr-x-id", Key: "CR-XRAY", FeatureID: "feat-x"},
	}})
	payload := mrPayload("CR-XRAY implement", "no footer here", "")
	cr, err := svc.matchChangeRequest(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if cr.Key != "CR-XRAY" {
		t.Fatalf("matched %q, want CR-XRAY (fuzzy fallback)", cr.Key)
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
