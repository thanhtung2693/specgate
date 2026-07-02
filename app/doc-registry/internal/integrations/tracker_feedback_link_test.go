package integrations

import (
	"context"
	"testing"

	"github.com/specgate/doc-registry/internal/workboard"
)

// captureFeedbackStore records the GovernanceFeedbackEvent that createTrackerFeedback
// writes. Embedding Store means every other method is nil — only the one method
// the function under test calls is implemented (the rest panic if touched).
type captureFeedbackStore struct {
	Store
	created  *GovernanceFeedbackEvent
	link     *TrackerLink // returned by TrackerLinkByExternal (nil ⇒ footer fallback)
	upserted *TrackerLink // captured from UpsertTrackerLink
}

func (c *captureFeedbackStore) UpsertTrackerLink(_ context.Context, in TrackerLink) (*TrackerLink, error) {
	c.upserted = &in
	return &in, nil
}

func (c *captureFeedbackStore) CreateGovernanceFeedbackEvent(_ context.Context, in GovernanceFeedbackEvent) (*GovernanceFeedbackEvent, error) {
	if in.ID == "" {
		in.ID = "fe-" + in.WebhookEventID
	}
	c.created = &in
	return &in, nil
}

// No persisted link → correlation falls back to the footer ref.
func (c *captureFeedbackStore) TrackerLinkByExternal(_ context.Context, _, _, _ string) (*TrackerLink, error) {
	return c.link, nil
}

// An inbound tracker status change carries the work item only as a `fixes
// SPECGATE-{key}` footer (the outbound formatter uses cr.Key). The feedback event
// must resolve that ref to the work item's UUID + feature so it surfaces on the
// work item — the UI queries feedback by change_request_id (UUID).
func TestCreateTrackerFeedback_LinksRefToWorkItem(t *testing.T) {
	cr := workboard.ChangeRequest{ID: "cr-uuid-1", Key: "CR-LOYALTY-REDEEM-1", FeatureID: "feat-1"}
	svc := NewServiceWithWorkBoard(nil, &fakeWorkBoard{items: []workboard.ChangeRequest{cr}})
	store := &captureFeedbackStore{}
	nd := normalizedDelivery{Provider: ProviderLinear, ExternalKey: "ZOP-7", RawState: "completed"}

	// correlationID is the parsed footer ref — the work item KEY, not its UUID.
	_, err := svc.createTrackerFeedback(context.Background(), store, "int-1", "evt-1", "CR-LOYALTY-REDEEM-1", nd)
	if err != nil {
		t.Fatalf("createTrackerFeedback: %v", err)
	}
	if store.created == nil {
		t.Fatal("no feedback event created")
	}
	if got := store.created.ChangeRequestID; got != "cr-uuid-1" {
		t.Errorf("ChangeRequestID = %q, want resolved UUID %q", got, "cr-uuid-1")
	}
	if got := store.created.FeatureID; got != "feat-1" {
		t.Errorf("FeatureID = %q, want %q", got, "feat-1")
	}
}

// A title/description edit re-delivers the same workflow state; recordTracker-
// StatusChange must suppress it (no feedback) so the work item's feedback isn't
// spammed. A real transition emits and advances the link's tracked state.
// For Linear the full state name (nd.Action) is stored, not the coarse type;
// lifecycle (State) still derives from nd.RawState.
func TestRecordTrackerStatusChange_OnlyOnRealTransition(t *testing.T) {
	cr := workboard.ChangeRequest{ID: "cr-uuid-1", Key: "CR-ONE", FeatureID: "feat-1"}
	svc := NewServiceWithWorkBoard(nil, &fakeWorkBoard{items: []workboard.ChangeRequest{cr}})
	nd := normalizedDelivery{Provider: ProviderLinear, ExternalKey: "ZOP-1", RawState: "started", Action: "In Progress"}

	// Same state name as the link → unchanged → no feedback, no upsert.
	same := &captureFeedbackStore{link: &TrackerLink{ChangeRequestID: "cr-uuid-1", ExternalKey: "ZOP-1", TrackerState: "In Progress"}}
	id, changed, err := svc.recordTrackerStatusChange(context.Background(), same, "int-1", "evt-1", "", nd)
	if err != nil || changed || id != "" || same.created != nil {
		t.Fatalf("unchanged state should emit nothing: changed=%v id=%q created=%#v", changed, id, same.created)
	}

	// Transition "In Progress" → "Done" (completed) → emit + advance link.
	// TrackerState must store the full name "Done"; State (lifecycle) must be closed (from RawState "completed").
	moved := &captureFeedbackStore{link: &TrackerLink{ChangeRequestID: "cr-uuid-1", ExternalKey: "ZOP-1", TrackerState: "In Progress"}}
	nd.RawState = "completed"
	nd.Action = "Done"
	id, changed, err = svc.recordTrackerStatusChange(context.Background(), moved, "int-1", "evt-2", "", nd)
	if err != nil || !changed || id == "" || moved.created == nil {
		t.Fatalf("transition should emit: changed=%v id=%q created=%#v err=%v", changed, id, moved.created, err)
	}
	if moved.created.ChangeRequestID != "cr-uuid-1" {
		t.Fatalf("feedback not linked: %#v", moved.created)
	}
	if moved.upserted == nil || moved.upserted.TrackerState != "Done" || moved.upserted.State != TrackerStateClosed {
		t.Fatalf("link not advanced (want TrackerState=Done, State=closed): %#v", moved.upserted)
	}
}

// A deleted issue arrives as a terminal "removed" state: it emits (the chip is
// derived from the newest tracker_status_changed) and drives the link to the
// removed lifecycle state so it drops out of the open set. The remove-action
// handler sets nd.RawState="removed" while nd.Action still holds the stale last
// state name — the removal path must bypass Action and store "removed".
func TestRecordTrackerStatusChange_RemovedIsTerminal(t *testing.T) {
	cr := workboard.ChangeRequest{ID: "cr-uuid-1", Key: "CR-ONE", FeatureID: "feat-1"}
	svc := NewServiceWithWorkBoard(nil, &fakeWorkBoard{items: []workboard.ChangeRequest{cr}})
	store := &captureFeedbackStore{link: &TrackerLink{ChangeRequestID: "cr-uuid-1", ExternalKey: "ZOP-1", TrackerState: "In Progress"}}
	// Action is the stale last-seen name; RawState is "removed" (set by remove-action handler).
	nd := normalizedDelivery{Provider: ProviderLinear, ExternalKey: "ZOP-1", RawState: TrackerStateRemoved, Action: "In Review"}

	id, changed, err := svc.recordTrackerStatusChange(context.Background(), store, "int-1", "evt-1", "", nd)
	if err != nil || !changed || id == "" {
		t.Fatalf("removal should emit: changed=%v id=%q err=%v", changed, id, err)
	}
	// TrackerState must be "removed", not the stale Action "In Review".
	if store.upserted == nil || store.upserted.State != TrackerStateRemoved || store.upserted.TrackerState != TrackerStateRemoved {
		t.Fatalf("link not marked removed (want TrackerState=removed, State=removed): %#v", store.upserted)
	}
}

// A persisted handoff link correlates the webhook even when the footer ref is
// absent (e.g. the issue description was edited and the `fixes` line removed):
// the link is matched by the immutable issue id/key, no footer needed.
func TestCreateTrackerFeedback_PrefersPersistedLink(t *testing.T) {
	cr := workboard.ChangeRequest{ID: "cr-uuid-9", Key: "CR-NINE", FeatureID: "feat-9"}
	svc := NewServiceWithWorkBoard(nil, &fakeWorkBoard{items: []workboard.ChangeRequest{cr}})
	store := &captureFeedbackStore{link: &TrackerLink{ChangeRequestID: "cr-uuid-9"}}
	nd := normalizedDelivery{Provider: ProviderLinear, ExternalKey: "ZOP-9", ExternalID: "linear-uuid-9", RawState: "completed"}

	// No correlationID (footer dropped) — only the persisted link can resolve it.
	if _, err := svc.createTrackerFeedback(context.Background(), store, "int-1", "evt-1", "", nd); err != nil {
		t.Fatalf("createTrackerFeedback: %v", err)
	}
	if store.created == nil || store.created.ChangeRequestID != "cr-uuid-9" || store.created.FeatureID != "feat-9" {
		t.Fatalf("link correlation failed: %#v", store.created)
	}
}

// No matching work item: the event is still emitted (the tracker match is an
// optional upgrade), just unlinked — never an error.
func TestCreateTrackerFeedback_UnmatchedRefStaysUnlinked(t *testing.T) {
	svc := NewServiceWithWorkBoard(nil, &fakeWorkBoard{items: []workboard.ChangeRequest{
		{ID: "cr-uuid-1", Key: "CR-OTHER", FeatureID: "feat-1"},
	}})
	store := &captureFeedbackStore{}
	nd := normalizedDelivery{Provider: ProviderLinear, ExternalKey: "ZOP-9", RawState: "started"}

	if _, err := svc.createTrackerFeedback(context.Background(), store, "int-1", "evt-1", "CR-NOPE", nd); err != nil {
		t.Fatalf("createTrackerFeedback: %v", err)
	}
	if store.created == nil {
		t.Fatal("no feedback event created")
	}
	if store.created.ChangeRequestID != "" {
		t.Errorf("ChangeRequestID = %q, want empty (unlinked)", store.created.ChangeRequestID)
	}
}

// GitLab issue nd.Action is a verb ("open"/"close"/"reopen"), not a state name.
// recordTrackerStatusChange must store nd.RawState for GitLab, not nd.Action, so
// the badge and dedup are not regressed by the Linear state-name change.
func TestRecordTrackerStatusChange_GitLabUsesRawState(t *testing.T) {
	t.Parallel()
	svc := NewServiceWithWorkBoard(nil, &fakeWorkBoard{})
	// GitLab issue: Action="close" (verb), RawState="completed" (derived state).
	// Existing link has TrackerState "started" — transition should fire.
	store := &captureFeedbackStore{link: &TrackerLink{ChangeRequestID: "cr-uuid-gl", ExternalKey: "#42", TrackerState: "started"}}
	nd := normalizedDelivery{
		Provider:    ProviderGitLab,
		ExternalKey: "#42",
		RawState:    "completed",
		Action:      "close", // GitLab verb — must NOT be stored
	}

	id, changed, err := svc.recordTrackerStatusChange(context.Background(), store, "int-gl", "evt-gl-1", "", nd)
	if err != nil || !changed || id == "" {
		t.Fatalf("GitLab transition should emit: changed=%v id=%q err=%v", changed, id, err)
	}
	// Must store "completed" (RawState), not "close" (Action verb).
	if store.upserted == nil || store.upserted.TrackerState != "completed" {
		t.Fatalf("GitLab TrackerState = %q, want \"completed\" (not the verb %q): %#v",
			store.upserted.TrackerState, nd.Action, store.upserted)
	}
	if store.upserted.State != TrackerStateClosed {
		t.Fatalf("GitLab lifecycle State = %q, want closed", store.upserted.State)
	}
}

// persistedTrackerState returns the full state name for Linear, RawState for
// GitLab, and "removed" for any removal regardless of provider.
func TestPersistedTrackerState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		nd   normalizedDelivery
		want string
	}{
		{
			name: "linear state name",
			nd:   normalizedDelivery{Provider: ProviderLinear, RawState: "started", Action: "In Progress"},
			want: "In Progress",
		},
		{
			name: "linear removal bypasses action",
			nd:   normalizedDelivery{Provider: ProviderLinear, RawState: TrackerStateRemoved, Action: "In Review"},
			want: TrackerStateRemoved,
		},
		{
			name: "linear empty action falls back to rawstate",
			nd:   normalizedDelivery{Provider: ProviderLinear, RawState: "started", Action: ""},
			want: "started",
		},
		{
			name: "gitlab uses rawstate not verb",
			nd:   normalizedDelivery{Provider: ProviderGitLab, RawState: "completed", Action: "close"},
			want: "completed",
		},
		{
			name: "gitlab removal",
			nd:   normalizedDelivery{Provider: ProviderGitLab, RawState: TrackerStateRemoved, Action: "close"},
			want: TrackerStateRemoved,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := persistedTrackerState(tc.nd)
			if got != tc.want {
				t.Errorf("persistedTrackerState(%+v) = %q, want %q", tc.nd, got, tc.want)
			}
		})
	}
}
