"""Tests for the pure helpers the board delivery-review runner parses with.

The runner itself is covered in ``test_board_delivery_review.py`` and the judge
in ``test_delivery_review.py``; here we lock the tolerant parsing that turns Doc
Registry rows into the judge's inputs.
"""

from __future__ import annotations

import json

import pytest

from specgate_agents.governance.board.delivery_review import (
    _event_payload,
    apply_peer_review_tiers,
    corroboration_evidence,
    latest_event,
    latest_valid_peer_review_payload,
    peer_review_covers_attested_met_criteria,
    resolve_evidence_policy_from_snapshot,
    valid_bound_peer_review_payload,
)
from specgate_agents.governance.quality_gates.delivery_review import CriterionReview
from specgate_agents.governance.quality_gates.profile_snapshot import UnsupportedSnapshotVersion


def latest_completed_payload(events: list[dict], change_request_id: str) -> dict | None:
    """Compose the production idiom: newest completion event, then its payload."""
    event = latest_event(events, change_request_id, "coding_agent.completed")
    return _event_payload(event) if event else None


def test_latest_completed_payload_picks_newest_for_this_cr() -> None:
    events = [
        {
            "event_type": "coding_agent.completed",
            "change_request_id": "cr-1",
            "created_at": "2026-06-10T00:00:00Z",
            "payload_json": json.dumps({"summary": "old"}),
        },
        {
            "event_type": "coding_agent.completed",
            "change_request_id": "cr-1",
            "created_at": "2026-06-12T00:00:00Z",
            "payload_json": json.dumps({"summary": "new"}),
        },
        {
            "event_type": "coding_agent.completed",
            "change_request_id": "cr-OTHER",
            "created_at": "2026-06-13T00:00:00Z",
            "payload_json": json.dumps({"summary": "other-cr"}),
        },
        {
            "event_type": "coding_agent.progress",
            "change_request_id": "cr-1",
            "created_at": "2026-06-14T00:00:00Z",
            "payload_json": json.dumps({"summary": "wrong-type"}),
        },
    ]
    assert latest_completed_payload(events, "cr-1") == {"summary": "new"}


def test_latest_completed_payload_breaks_timestamp_ties_by_id() -> None:
    created_at = "2026-07-19T00:00:00Z"
    events = [
        {
            "id": "completion-a",
            "event_type": "coding_agent.completed",
            "change_request_id": "cr-1",
            "created_at": created_at,
            "payload_json": json.dumps({"summary": "lower id"}),
        },
        {
            "id": "completion-z",
            "event_type": "coding_agent.completed",
            "change_request_id": "cr-1",
            "created_at": created_at,
            "payload_json": json.dumps({"summary": "higher id"}),
        },
    ]

    assert latest_completed_payload(events, "cr-1") == {"summary": "higher id"}


def test_latest_completed_payload_none_when_absent() -> None:
    assert latest_completed_payload([], "cr-1") is None
    assert (
        latest_completed_payload(
            [{"event_type": "coding_agent.completed", "change_request_id": "cr-2"}], "cr-1"
        )
        is None
    )


def test_latest_completed_payload_tolerates_malformed_newest_payload() -> None:
    events = [
        {
            "event_type": "coding_agent.completed",
            "change_request_id": "cr-1",
            "created_at": "2026-06-10T00:00:00Z",
            "payload_json": json.dumps({"summary": "old"}),
        },
        {
            "event_type": "coding_agent.completed",
            "change_request_id": "cr-1",
            "created_at": "2026-06-12T00:00:00Z",
            "payload_json": "{not-json",
        },
    ]

    assert latest_completed_payload(events, "cr-1") == {}


def test_apply_peer_review_tiers_marks_matching_met_criteria() -> None:
    reviews = [
        CriterionReview(criterion_id="ac-1", text="A", verdict="met"),
        CriterionReview(criterion_id="ac-2", text="B", verdict="met", trust_tier="grounded"),
    ]
    got = apply_peer_review_tiers(
        reviews,
        {"criteria": [{"criterion_id": "ac-1", "claim": "satisfied"}]},
    )
    assert got[0].trust_tier == "peer_reviewed"
    assert got[1].trust_tier == "grounded"


def test_grounded_agent_claim_still_requires_peer_review() -> None:
    criteria = [
        CriterionReview(criterion_id="ac-1", text="A", verdict="met", trust_tier="grounded")
    ]
    assert peer_review_covers_attested_met_criteria(criteria, None) is False


def test_valid_bound_peer_review_rejects_stale_completion() -> None:
    completed = {
        "id": "feedback-new",
        "payload_json": json.dumps({"git_receipt": {"head_revision": "new"}}),
    }
    peer = {
        "payload_json": json.dumps(
            {
                "peer_review_of": {
                    "completion_feedback_event_id": "feedback-old",
                    "git_receipt": {"head_revision": "old"},
                },
                "criteria": [{"criterion_id": "ac-1", "claim": "satisfied"}],
            }
        )
    }
    assert valid_bound_peer_review_payload(peer, completed) is None


def test_latest_valid_peer_review_skips_newer_review_of_stale_completion() -> None:
    completed = {
        "id": "feedback-current",
        "payload_json": json.dumps({"git_receipt": {"head_revision": "current-head"}}),
    }
    events = [
        {
            "id": "peer-valid",
            "event_type": "coding_agent.peer_reviewed",
            "change_request_id": "cr-1",
            "created_at": "2026-07-20T00:00:00Z",
            "payload_json": json.dumps(
                {
                    "peer_review_of": {
                        "completion_feedback_event_id": "feedback-current",
                        "git_receipt": {"head_revision": "current-head"},
                    },
                    "criteria": [{"criterion_id": "ac-1", "claim": "satisfied"}],
                }
            ),
        },
        {
            "id": "peer-stale",
            "event_type": "coding_agent.peer_reviewed",
            "change_request_id": "cr-1",
            "created_at": "2026-07-21T00:00:00Z",
            "payload_json": json.dumps(
                {
                    "peer_review_of": {
                        "completion_feedback_event_id": "feedback-old",
                        "git_receipt": {"head_revision": "old-head"},
                    },
                    "criteria": [{"criterion_id": "ac-1", "claim": "satisfied"}],
                }
            ),
        },
    ]

    assert latest_valid_peer_review_payload(events, "cr-1", completed) == {
        "peer_review_of": {
            "completion_feedback_event_id": "feedback-current",
            "git_receipt": {"head_revision": "current-head"},
        },
        "criteria": [{"criterion_id": "ac-1", "claim": "satisfied"}],
    }


# ── Slice B: repository corroboration ─────────────────────────────────────


def test_corroboration_requires_merged_head_to_match_latest_completion() -> None:
    events = [
        {
            "event_type": "delivery.pr_merged",
            "change_request_id": "cr-1",
            "payload_json": json.dumps({"head_sha": "abc123"}),
        }
    ]

    assert corroboration_evidence(
        events,
        "cr-1",
        {"git_receipt": {"head_revision": "abc123"}},
    ) == [{"kind": "pr_merged"}]


@pytest.mark.parametrize("head_sha", [None, "different-head"])
def test_corroboration_rejects_missing_or_mismatched_merged_head(head_sha: str | None) -> None:
    events = [
        {
            "event_type": "delivery.pr_merged",
            "change_request_id": "cr-1",
            "payload_json": json.dumps({"head_sha": head_sha}),
        }
    ]

    assert (
        corroboration_evidence(
            events,
            "cr-1",
            {"git_receipt": {"head_revision": "abc123"}},
        )
        == []
    )


def test_corroboration_normalizes_completion_and_merged_heads() -> None:
    events = [
        {
            "event_type": "delivery.pr_merged",
            "change_request_id": "cr-1",
            "payload_json": json.dumps({"head_sha": "  ABC123  "}),
        }
    ]

    assert corroboration_evidence(
        events,
        "cr-1",
        {"git_receipt": {"head_revision": " abc123 "}},
    ) == [{"kind": "pr_merged"}]


def test_corroboration_skips_newer_merge_for_different_head() -> None:
    events = [
        {
            "id": "merge-current",
            "event_type": "delivery.pr_merged",
            "change_request_id": "cr-1",
            "created_at": "2026-07-20T00:00:00Z",
            "payload_json": json.dumps({"head_sha": "current-head"}),
        },
        {
            "id": "merge-other",
            "event_type": "delivery.pr_merged",
            "change_request_id": "cr-1",
            "created_at": "2026-07-21T00:00:00Z",
            "payload_json": json.dumps({"head_sha": "different-head"}),
        },
    ]

    assert corroboration_evidence(
        events,
        "cr-1",
        {"git_receipt": {"head_revision": "current-head"}},
    ) == [{"kind": "pr_merged"}]


def test_corroboration_treats_older_merged_event_as_stale_after_new_completion() -> None:
    events = [
        {
            "event_type": "delivery.pr_merged",
            "change_request_id": "cr-1",
            "payload_json": json.dumps({"head_sha": "old-head"}),
        }
    ]

    assert (
        corroboration_evidence(
            events,
            "cr-1",
            {"git_receipt": {"head_revision": "new-head"}},
        )
        == []
    )


def test_ci_event_never_produces_repository_corroboration() -> None:
    events = [
        {
            "event_type": "delivery.ci_passed",
            "change_request_id": "cr-1",
            "payload_json": json.dumps({"head_sha": "abc123"}),
        }
    ]

    assert (
        corroboration_evidence(
            events,
            "cr-1",
            {"git_receipt": {"head_revision": "abc123"}},
        )
        == []
    )


# ── Slice B: resolve_evidence_policy_from_snapshot ────────────────────────


def test_resolve_evidence_policy_reads_field_from_snapshot() -> None:
    snapshot = json.dumps(
        {
            "snapshot_schema_version": "specgate.policy/v1",
            "approval_policy": "human_required",
            "evidence_policy": "corroborated_required",
        }
    )
    assert resolve_evidence_policy_from_snapshot(snapshot) == "corroborated_required"


def test_resolve_evidence_policy_defaults_attested_ok_when_snapshot_is_absent() -> None:
    assert resolve_evidence_policy_from_snapshot("") == "attested_ok"
    assert resolve_evidence_policy_from_snapshot(None) == "attested_ok"


def test_resolve_evidence_policy_rejects_bad_json() -> None:
    with pytest.raises(json.JSONDecodeError):
        resolve_evidence_policy_from_snapshot("not-json")


def test_resolve_evidence_policy_rejects_missing_snapshot_version() -> None:
    with pytest.raises(UnsupportedSnapshotVersion):
        resolve_evidence_policy_from_snapshot("{}")


def test_resolve_evidence_policy_rejects_v1_without_field() -> None:
    snapshot = json.dumps(
        {
            "snapshot_schema_version": "specgate.policy/v1",
            "approval_policy": "human_required",
        }
    )
    with pytest.raises(ValueError, match="evidence_policy is required"):
        resolve_evidence_policy_from_snapshot(snapshot)


def test_resolve_evidence_policy_rejects_unsupported_value() -> None:
    snapshot = json.dumps(
        {
            "snapshot_schema_version": "specgate.policy/v1",
            "approval_policy": "human_required",
            "evidence_policy": "unsupported_policy",
        }
    )
    with pytest.raises(ValueError, match="unsupported evidence policy"):
        resolve_evidence_policy_from_snapshot(snapshot)
