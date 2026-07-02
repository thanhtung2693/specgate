package artifact

import (
	"context"
	"errors"
	"testing"
)

// statusTestRepo embeds *memRepo with a working UpdateStatus (instead of panic).
type statusTestRepo struct {
	*memRepo
}

func (r *statusTestRepo) UpdateStatus(_ context.Context, id string, status Status, actor string, ev Event) error {
	a, ok := r.artifacts[id]
	if !ok {
		return ErrNotFound
	}
	a.Status = status
	a.ApprovedBy = actor
	r.events = append(r.events, ev)
	return nil
}

func newStatusTestService(t *testing.T) (*RegistryService, *statusTestRepo, *memStore) {
	t.Helper()
	repo := &statusTestRepo{memRepo: newMemRepo()}
	store := newMemStore()
	svc := NewService(repo, store, testObjectKey, 0)
	return svc, repo, store
}

// seedWithProfile seeds an artifact that carries the given profile snapshot JSON.
func seedWithProfile(r *statusTestRepo, id, featureID, version, snapshotJSON string) {
	r.artifacts[id] = &Artifact{
		ID:                       id,
		FeatureID:                featureID,
		Version:                  version,
		Status:                   StatusDraft,
		GatesProfileSnapshotJSON: snapshotJSON,
	}
}

// Minimal snapshot JSON strings used across the UpdateStatus guard tests.
const (
	snapshotHumanRequired = `{"approval_policy":"human_required"}`
	snapshotSelfApprove   = `{"approval_policy":"self_approve"}`
	snapshotAutoApprove   = `{"approval_policy":"auto"}`
	snapshotNoPolicy      = `{}`
	snapshotCorruptJSON   = `{"snapshot_schema_version":"specgate.policy/v1","approval_policy":"self_approve","bad-json`
	snapshotV1HumanReq    = `{"snapshot_schema_version":"specgate.policy/v1","approval_policy":"human_required"}`
	snapshotV1SelfApprove = `{"snapshot_schema_version":"specgate.policy/v1","approval_policy":"self_approve"}`
)

// TestUpdateStatus_AgentApprove_HumanRequired_Blocked asserts that an agent
// actor cannot approve an artifact whose profile requires a human.
// This is a cooperative surface check (actor_kind is client-asserted); the
// human surface is expected to perform the approval, not a server identity gate.
func TestUpdateStatus_AgentApprove_HumanRequired_Blocked(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newStatusTestService(t)
	seedWithProfile(repo, "art-1", "feat-1", "v0.1", snapshotHumanRequired)

	_, err := svc.UpdateStatus(context.Background(), "art-1", StatusUpdate{
		Status:    StatusApproved,
		Actor:     "agent-x",
		ActorKind: "agent",
	})
	if !errors.Is(err, ErrApprovalRequiresHuman) {
		t.Errorf("agent approve under human_required: err = %v, want ErrApprovalRequiresHuman", err)
	}
}

// TestUpdateStatus_AgentApprove_SelfApprove_Allowed asserts that an agent
// actor can approve an artifact whose profile is self_approve AND that the
// canonical artifact.published event is emitted (per spec §14 + design note
// "CRITICAL invariant": an approved artifact must always have its event).
func TestUpdateStatus_AgentApprove_SelfApprove_Allowed(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newStatusTestService(t)
	seedWithProfile(repo, "art-2", "feat-1", "v0.1", snapshotSelfApprove)

	_, err := svc.UpdateStatus(context.Background(), "art-2", StatusUpdate{
		Status:    StatusApproved,
		Actor:     "agent-x",
		ActorKind: "agent",
	})
	if err != nil {
		t.Errorf("agent approve under self_approve: unexpected error %v", err)
	}
	assertPublishedEventEmitted(t, repo, "art-2")
}

// TestUpdateStatus_AgentApprove_AutoPolicy_Allowed asserts that auto policy
// permits any actor_kind AND that artifact.published is emitted. The auto path
// in specgate_publish delegates here so the event fires exactly once via the
// canonical transition — not a separate code path.
func TestUpdateStatus_AgentApprove_AutoPolicy_Allowed(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newStatusTestService(t)
	seedWithProfile(repo, "art-3", "feat-1", "v0.1", snapshotAutoApprove)

	_, err := svc.UpdateStatus(context.Background(), "art-3", StatusUpdate{
		Status:    StatusApproved,
		Actor:     "specgate-ide",
		ActorKind: "agent",
	})
	if err != nil {
		t.Errorf("agent approve under auto: unexpected error %v", err)
	}
	assertPublishedEventEmitted(t, repo, "art-3")
}

// assertPublishedEventEmitted verifies the repo has an artifact.published event
// for the given artifact ID — the CRITICAL invariant from the design notes.
func assertPublishedEventEmitted(t *testing.T, repo *statusTestRepo, artifactID string) {
	t.Helper()
	for _, ev := range repo.events {
		if ev.ArtifactID == artifactID && ev.EventType == EventPublished {
			return
		}
	}
	t.Errorf("no artifact.published event for artifact %s; events: %v", artifactID, repo.events)
}

func TestEventTypeForStatus(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status Status
		want   string
	}{
		{StatusApproved, EventPublished},
		{StatusNeedsChanges, EventNeedsChanges},
		{StatusSuperseded, EventSuperseded},
		// Draft is publish-time only; the mapping falls back to the publish event.
		{StatusDraft, EventPublished},
	} {
		if got := EventTypeForStatus(tc.status); got != tc.want {
			t.Fatalf("EventTypeForStatus(%q) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

// TestUpdateStatus_HumanApprove_HumanRequired_Allowed asserts that a human
// actor can always approve, regardless of approval_policy.
func TestUpdateStatus_HumanApprove_HumanRequired_Allowed(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newStatusTestService(t)
	seedWithProfile(repo, "art-4", "feat-1", "v0.1", snapshotHumanRequired)

	_, err := svc.UpdateStatus(context.Background(), "art-4", StatusUpdate{
		Status:    StatusApproved,
		Actor:     "reviewer@example.com",
		ActorKind: "human",
	})
	if err != nil {
		t.Errorf("human approve under human_required: unexpected error %v", err)
	}
}

// TestUpdateStatus_EmptyActorKind_TreatedAsHuman asserts that an absent
// actor_kind (empty string) defaults to human and is allowed under human_required.
func TestUpdateStatus_EmptyActorKind_TreatedAsHuman(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newStatusTestService(t)
	seedWithProfile(repo, "art-5", "feat-1", "v0.1", snapshotHumanRequired)

	_, err := svc.UpdateStatus(context.Background(), "art-5", StatusUpdate{
		Status: StatusApproved,
		Actor:  "reviewer@example.com",
		// ActorKind deliberately absent — should default to human
	})
	if err != nil {
		t.Errorf("empty actor_kind under human_required: unexpected error %v", err)
	}
}

// TestUpdateStatus_EmptySnapshot_DefaultsToHumanRequired_BlocksAgent asserts
// that an artifact with no profile snapshot defaults to human_required, blocking
// agent approval.
func TestUpdateStatus_EmptySnapshot_DefaultsToHumanRequired_BlocksAgent(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newStatusTestService(t)
	seedWithProfile(repo, "art-6", "feat-1", "v0.1", snapshotNoPolicy)

	_, err := svc.UpdateStatus(context.Background(), "art-6", StatusUpdate{
		Status:    StatusApproved,
		Actor:     "agent-x",
		ActorKind: "agent",
	})
	if !errors.Is(err, ErrApprovalRequiresHuman) {
		t.Errorf("agent approve with no snapshot policy: err = %v, want ErrApprovalRequiresHuman", err)
	}
}

// TestUpdateStatus_CorruptSnapshot_FailClosed asserts that corrupt or unparseable
// snapshot JSON causes agent approval to fail with ErrApprovalRequiresHuman
// (fail-closed). The old inline unmarshal with `if err == nil` was fail-open;
// ParseSnapshot returns the error so the guard fires correctly.
func TestUpdateStatus_CorruptSnapshot_FailClosed(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newStatusTestService(t)
	seedWithProfile(repo, "art-8", "feat-1", "v0.1", snapshotCorruptJSON)

	_, err := svc.UpdateStatus(context.Background(), "art-8", StatusUpdate{
		Status:    StatusApproved,
		Actor:     "agent-x",
		ActorKind: "agent",
	})
	if !errors.Is(err, ErrApprovalRequiresHuman) {
		t.Errorf("corrupt snapshot + agent approve: err = %v, want ErrApprovalRequiresHuman", err)
	}
}

// TestUpdateStatus_PolicyV1Snapshot_HumanRequired_BlocksAgent verifies the
// approval guard works through ParseSnapshot for the v1 schema path.
func TestUpdateStatus_PolicyV1Snapshot_HumanRequired_BlocksAgent(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newStatusTestService(t)
	seedWithProfile(repo, "art-9", "feat-1", "v0.1", snapshotV1HumanReq)

	_, err := svc.UpdateStatus(context.Background(), "art-9", StatusUpdate{
		Status:    StatusApproved,
		Actor:     "agent-x",
		ActorKind: "agent",
	})
	if !errors.Is(err, ErrApprovalRequiresHuman) {
		t.Errorf("v1 snapshot human_required + agent: err = %v, want ErrApprovalRequiresHuman", err)
	}
}

// TestUpdateStatus_PolicyV1Snapshot_SelfApprove_Allowed verifies an agent can
// approve through the ParseSnapshot path when the v1 snapshot has self_approve.
func TestUpdateStatus_PolicyV1Snapshot_SelfApprove_Allowed(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newStatusTestService(t)
	seedWithProfile(repo, "art-10", "feat-1", "v0.1", snapshotV1SelfApprove)

	_, err := svc.UpdateStatus(context.Background(), "art-10", StatusUpdate{
		Status:    StatusApproved,
		Actor:     "agent-x",
		ActorKind: "agent",
	})
	if err != nil {
		t.Errorf("v1 snapshot self_approve + agent: unexpected error %v", err)
	}
}

// TestUpdateStatus_NonApproveTransition_ActorKindIgnored asserts that the
// actor_kind guard only applies to the draft→approved transition.
func TestUpdateStatus_NonApproveTransition_ActorKindIgnored(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newStatusTestService(t)
	seedWithProfile(repo, "art-7", "feat-1", "v0.1", snapshotHumanRequired)

	_, err := svc.UpdateStatus(context.Background(), "art-7", StatusUpdate{
		Status:    StatusNeedsChanges,
		Actor:     "agent-x",
		ActorKind: "agent",
	})
	if err != nil {
		t.Errorf("agent needs_changes under human_required: unexpected error %v", err)
	}
}
