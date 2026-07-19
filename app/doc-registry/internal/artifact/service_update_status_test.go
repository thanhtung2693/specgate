package artifact

import (
	"context"
	"encoding/json"
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
	svc := NewService(repo, store, testObjectKey)
	return svc, repo, store
}

// seedWithProfile seeds an artifact that carries the given policy snapshot JSON.
func seedWithProfile(r *statusTestRepo, id, featureID, version, snapshotJSON string) {
	r.artifacts[id] = &Artifact{
		ID:                 id,
		FeatureID:          featureID,
		Version:            version,
		Status:             StatusDraft,
		PolicySnapshotJSON: snapshotJSON,
	}
}

// Minimal snapshot JSON strings used across the UpdateStatus guard tests.
const (
	snapshotHumanRequired = `{"snapshot_schema_version":"specgate.policy/v1","approval_policy":"human_required","evidence_policy":"attested_ok"}`
	snapshotSelfApprove   = `{"snapshot_schema_version":"specgate.policy/v1","approval_policy":"self_approve","evidence_policy":"attested_ok"}`
	snapshotAutoApprove   = `{"snapshot_schema_version":"specgate.policy/v1","approval_policy":"auto","evidence_policy":"attested_ok"}`
	snapshotUnsupported   = `{"snapshot_schema_version":"specgate.policy/v1","approval_policy":"unsupported_policy","evidence_policy":"attested_ok"}`
	snapshotNoPolicy      = `{"snapshot_schema_version":"specgate.policy/v1"}`
	snapshotCorruptJSON   = `{"snapshot_schema_version":"specgate.policy/v1","approval_policy":"self_approve","bad-json`
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

// Unsupported snapshots must not let an agent bypass human approval.
func TestUpdateStatus_AgentApprove_SelfApproveSnapshot_Rejected(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newStatusTestService(t)
	seedWithProfile(repo, "art-2", "feat-1", "v0.1", snapshotSelfApprove)

	_, err := svc.UpdateStatus(context.Background(), "art-2", StatusUpdate{
		Status:    StatusApproved,
		Actor:     "agent-x",
		ActorKind: "agent",
	})
	if !errors.Is(err, ErrApprovalRequiresHuman) {
		t.Errorf("agent approve under self_approve: err = %v, want ErrApprovalRequiresHuman", err)
	}
}

// Unsupported snapshots must not let an agent bypass human approval.
func TestUpdateStatus_AgentApprove_AutoSnapshot_Rejected(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newStatusTestService(t)
	seedWithProfile(repo, "art-3", "feat-1", "v0.1", snapshotAutoApprove)

	_, err := svc.UpdateStatus(context.Background(), "art-3", StatusUpdate{
		Status:    StatusApproved,
		Actor:     "specgate-ide",
		ActorKind: "agent",
	})
	if !errors.Is(err, ErrApprovalRequiresHuman) {
		t.Errorf("agent approve under auto: err = %v, want ErrApprovalRequiresHuman", err)
	}
}

func TestUpdateStatus_HumanApprove_RetiredPolicySnapshot_Rejected(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newStatusTestService(t)
	seedWithProfile(repo, "art-retired-policy", "feat-1", "v0.1", snapshotAutoApprove)

	_, err := svc.UpdateStatus(context.Background(), "art-retired-policy", StatusUpdate{
		Status:    StatusApproved,
		Actor:     "reviewer@example.com",
		ActorKind: "human",
	})
	if !errors.Is(err, ErrUnsupportedApprovalPolicy) {
		t.Errorf("human approve with retired policy: err = %v, want ErrUnsupportedApprovalPolicy", err)
	}
}

func TestEventTypeForStatus(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status Status
		want   string
	}{
		{StatusApproved, EventApproved},
		{StatusNeedsChanges, EventNeedsChanges},
		{StatusSuperseded, EventSuperseded},
		{StatusDraft, ""},
	} {
		if got := EventTypeForStatus(tc.status); got != tc.want {
			t.Fatalf("EventTypeForStatus(%q) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestUpdateStatus_RejectsUnsupportedTransition(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newStatusTestService(t)
	seedWithProfile(repo, "art-invalid-transition", "feat-1", "v0.1", snapshotHumanRequired)

	if _, err := svc.UpdateStatus(context.Background(), "art-invalid-transition", StatusUpdate{
		Status: StatusDraft,
		Actor:  "reviewer@example.com",
	}); err == nil {
		t.Fatal("UpdateStatus(draft) error = nil, want unsupported transition")
	}
	if got := repo.artifacts["art-invalid-transition"].Status; got != StatusDraft {
		t.Fatalf("artifact status = %q, want unchanged draft", got)
	}
	if len(repo.events) != 0 {
		t.Fatalf("events = %d, want none", len(repo.events))
	}
}

// TestUpdateStatus_HumanApprove_HumanRequired_Allowed asserts that the default
// human_required policy permits human approval.
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

func TestUpdateStatus_UnsupportedApprovalPolicyRejected(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newStatusTestService(t)
	seedWithProfile(repo, "art-unsupported-policy", "feat-1", "v0.1", snapshotUnsupported)

	_, err := svc.UpdateStatus(context.Background(), "art-unsupported-policy", StatusUpdate{
		Status:    StatusApproved,
		Actor:     "reviewer",
		ActorKind: "human",
	})
	if !errors.Is(err, ErrUnsupportedApprovalPolicy) {
		t.Errorf("unsupported approval policy: err = %v, want ErrUnsupportedApprovalPolicy", err)
	}
	if repo.artifacts["art-unsupported-policy"].Status != StatusDraft {
		t.Fatalf("unsupported policy approval mutated status to %q", repo.artifacts["art-unsupported-policy"].Status)
	}
}

func TestUpdateStatus_HumanApprove_MissingApprovalPolicyRejected(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newStatusTestService(t)
	seedWithProfile(repo, "art-missing-policy", "feat-1", "v0.1", snapshotNoPolicy)

	_, err := svc.UpdateStatus(context.Background(), "art-missing-policy", StatusUpdate{
		Status: StatusApproved, Actor: "reviewer", ActorKind: "human",
	})
	if !errors.Is(err, ErrUnsupportedApprovalPolicy) {
		t.Fatalf("human approval with missing policy: err = %v, want ErrUnsupportedApprovalPolicy", err)
	}
}

func TestUpdateStatus_HumanApprove_EmptySnapshotRejected(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newStatusTestService(t)
	seedWithProfile(repo, "art-empty-snapshot", "feat-1", "v0.1", "")

	_, err := svc.UpdateStatus(context.Background(), "art-empty-snapshot", StatusUpdate{
		Status: StatusApproved, Actor: "reviewer", ActorKind: "human",
	})
	if !errors.Is(err, ErrUnsupportedApprovalPolicy) {
		t.Fatalf("human approval with empty snapshot: err = %v, want ErrUnsupportedApprovalPolicy", err)
	}
}

func TestUpdateStatus_HumanApprove_CorruptSnapshotRejected(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newStatusTestService(t)
	seedWithProfile(repo, "art-corrupt-snapshot", "feat-1", "v0.1", snapshotCorruptJSON)

	_, err := svc.UpdateStatus(context.Background(), "art-corrupt-snapshot", StatusUpdate{
		Status: StatusApproved, Actor: "reviewer", ActorKind: "human",
	})
	if !errors.Is(err, ErrUnsupportedApprovalPolicy) {
		t.Fatalf("human approval with corrupt snapshot: err = %v, want ErrUnsupportedApprovalPolicy", err)
	}
}

func TestUpdateStatus_DefaultSoloSelfApprovalStillAllowed(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newStatusTestService(t)
	seedWithProfile(repo, "art-solo-default", "feat-1", "v0.1", snapshotHumanRequired)
	repo.artifacts["art-solo-default"].CreatedBy = "alice"

	_, err := svc.UpdateStatus(context.Background(), "art-solo-default", StatusUpdate{
		Status:    StatusApproved,
		Actor:     "alice",
		ActorKind: "human",
	})
	if err != nil {
		t.Errorf("solo self-approval under default human_required: unexpected error %v", err)
	}
}

func TestUpdateStatus_ApprovedEventPayloadIncludesActorKind(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newStatusTestService(t)
	seedWithProfile(repo, "art-actor-kind", "feat-1", "v0.1", snapshotHumanRequired)

	_, err := svc.UpdateStatus(context.Background(), "art-actor-kind", StatusUpdate{
		Status:    StatusApproved,
		Actor:     "reviewer@example.com",
		ActorKind: "human",
	})
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if got := repo.events[0].EventType; got != EventApproved {
		t.Fatalf("event type = %q, want artifact.approved", got)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(repo.events[0].Payload), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got := payload["actor_kind"]; got != "human" {
		t.Fatalf("actor_kind = %v, want human", got)
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
