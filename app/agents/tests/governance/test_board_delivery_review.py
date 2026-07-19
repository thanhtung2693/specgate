"""Tests for the board-level delivery-review runner's payload parsing.

The judge itself is covered in ``test_delivery_review.py``; here we lock the
tolerant parsing that turns Doc Registry rows into the judge's inputs.
"""

from __future__ import annotations

import json

import pytest

from specgate_agents.governance.board.delivery_review import (
    apply_peer_review_tiers,
    corroboration_evidence,
    latest_completed_payload,
    latest_valid_peer_review_payload,
    peer_review_covers_attested_met_criteria,
    resolve_evidence_policy_from_snapshot,
    review_change_request_delivery,
    valid_bound_peer_review_payload,
)
from specgate_agents.governance.quality_gates.delivery_review import CriterionReview
from specgate_agents.governance.quality_gates.profile_snapshot import UnsupportedSnapshotVersion


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


@pytest.mark.asyncio
async def test_review_scopes_feedback_queries_by_change_and_event_type(monkeypatch) -> None:
    import specgate_agents.governance.board.delivery_review as board_review

    calls: set[tuple[str | None, str | None, int]] = set()

    class FakeClient:
        def __init__(self, _base_url: str):
            pass

        def get_change_request(self, _change_request_id: str, *, workspace_id: str):
            return {"id": "cr-1", "work_type": "bug_fix"}

        async def alist_acceptance_criteria(self, _change_request_id: str, *, workspace_id: str):
            return []

        async def alist_governance_feedback_events(
            self,
            *,
            workspace_id: str,
            change_request_id: str | None = None,
            event_type: str | None = None,
            limit: int = 200,
        ):
            calls.add((change_request_id, event_type, limit))
            return []

    monkeypatch.setattr(board_review, "DocRegistryClient", FakeClient)
    monkeypatch.setattr(board_review, "doc_registry_base_url", lambda: "http://registry")

    result = await review_change_request_delivery("cr-1", workspace_id="ws-a")

    assert result["reason"] == "no_completion_report"
    assert calls == {
        ("cr-1", "coding_agent.completed", 1),
        ("cr-1", "coding_agent.peer_reviewed", 200),
        ("cr-1", "delivery.pr_merged", 200),
    }


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "artifact_result",
    [None, {}],
    ids=["artifact-unreachable", "policy-snapshot-missing"],
)
async def test_review_blocks_when_lead_artifact_policy_is_unavailable(
    monkeypatch, artifact_result
) -> None:
    import specgate_agents.governance.board.delivery_review as board_review

    posted: list[dict] = []

    class FakeClient:
        def __init__(self, _base_url: str):
            pass

        def get_change_request(self, _change_request_id: str, *, workspace_id: str):
            return {"id": "cr-1", "feature_id": "", "lead_artifact_id": "art-1"}

        async def alist_acceptance_criteria(self, _change_request_id: str, *, workspace_id: str):
            return [{"id": "ac-1", "text": "Delivery remains governed"}]

        async def alist_governance_feedback_events(
            self,
            *,
            workspace_id: str,
            change_request_id: str | None = None,
            event_type: str | None = None,
            limit: int = 200,
        ):
            return [
                {
                    "id": "completion-1",
                    "event_type": "coding_agent.completed",
                    "change_request_id": "cr-1",
                    "created_at": "2026-07-19T00:00:00Z",
                    "payload_json": json.dumps(
                        {
                            "criteria": [
                                {
                                    "criterion_id": "ac-1",
                                    "claim": "satisfied",
                                    "evidence": {"kind": "test", "path": "test_delivery.py"},
                                }
                            ]
                        }
                    ),
                }
            ]

        async def aget_artifact(self, _artifact_id: str, *, workspace_id: str):
            if artifact_result is None:
                raise RuntimeError("artifact unavailable")
            return artifact_result

        def refresh_change_request_gate_runs(
            self,
            change_request_id: str,
            evaluations: list[dict],
            *,
            evaluations_only: bool = False,
            workspace_id: str,
        ):
            assert evaluations_only is True
            posted.extend(evaluations)
            return [{"change_request_id": change_request_id, **evaluations[0]}]

    async def unexpected_model_setup():
        pytest.fail("policy-unavailable review must stop before model setup")

    monkeypatch.setattr(board_review, "DocRegistryClient", FakeClient)
    monkeypatch.setattr(board_review, "doc_registry_base_url", lambda: "http://registry")
    monkeypatch.setattr(board_review, "_hydrate_model_settings", unexpected_model_setup)

    result = await review_change_request_delivery("cr-1", workspace_id="ws-a")

    assert result["verdict"] == "needs_human_review"
    assert result["reason_code"] == "policy_unavailable"
    assert posted[0]["gate"] == "delivery_review"
    assert posted[0]["state"] == "needs_human_review"
    assert posted[0]["judge_model"] == "deterministic_policy_guard"
    persisted = json.loads(posted[0]["evidence"])
    assert persisted["reason_code"] == "policy_unavailable"
    assert persisted["completion_feedback_event_id"] == "completion-1"


@pytest.mark.asyncio
async def test_review_blocks_when_feature_policy_lookup_is_unavailable(monkeypatch) -> None:
    import specgate_agents.governance.board.delivery_review as board_review

    posted: list[dict] = []

    class FakeClient:
        def __init__(self, _base_url: str):
            pass

        def get_change_request(self, _change_request_id: str, *, workspace_id: str):
            return {
                "id": "cr-1",
                "feature_id": "feature-1",
                "lead_artifact_id": "",
                "work_type": "new_feature",
            }

        def get_workboard_feature(self, _feature_id: str, *, workspace_id: str):
            raise RuntimeError("feature unavailable")

        async def alist_acceptance_criteria(self, _change_request_id: str, *, workspace_id: str):
            return [{"id": "ac-1", "text": "Delivery remains governed"}]

        async def alist_governance_feedback_events(
            self,
            *,
            workspace_id: str,
            change_request_id: str | None = None,
            event_type: str | None = None,
            limit: int = 200,
        ):
            return [
                {
                    "id": "completion-1",
                    "event_type": "coding_agent.completed",
                    "change_request_id": "cr-1",
                    "created_at": "2026-07-19T00:00:00Z",
                    "payload_json": json.dumps(
                        {"criteria": [{"criterion_id": "ac-1", "claim": "satisfied"}]}
                    ),
                }
            ]

        def refresh_change_request_gate_runs(
            self,
            change_request_id: str,
            evaluations: list[dict],
            *,
            evaluations_only: bool = False,
            workspace_id: str,
        ):
            assert evaluations_only is True
            posted.extend(evaluations)
            return [{"change_request_id": change_request_id, **evaluations[0]}]

    async def unexpected_model_setup():
        pytest.fail("policy-unavailable review must stop before model setup")

    monkeypatch.setattr(board_review, "DocRegistryClient", FakeClient)
    monkeypatch.setattr(board_review, "doc_registry_base_url", lambda: "http://registry")
    monkeypatch.setattr(board_review, "_hydrate_model_settings", unexpected_model_setup)

    result = await review_change_request_delivery("cr-1", workspace_id="ws-a")

    assert result["verdict"] == "needs_human_review"
    assert result["reason_code"] == "policy_unavailable"
    assert json.loads(posted[0]["evidence"])["reason_code"] == "policy_unavailable"


@pytest.mark.asyncio
async def test_review_blocks_when_feature_has_no_canonical_policy_artifact(monkeypatch) -> None:
    import specgate_agents.governance.board.delivery_review as board_review

    posted: list[dict] = []

    class FakeClient:
        def __init__(self, _base_url: str):
            pass

        def get_change_request(self, _change_request_id: str, *, workspace_id: str):
            return {
                "id": "cr-1",
                "feature_id": "feature-1",
                "lead_artifact_id": "",
                "work_type": "new_feature",
            }

        def get_workboard_feature(self, _feature_id: str, *, workspace_id: str):
            return {"id": "feature-1", "canonical_artifact_id": ""}

        async def alist_acceptance_criteria(self, _change_request_id: str, *, workspace_id: str):
            return [{"id": "ac-1", "text": "Delivery remains governed"}]

        async def alist_governance_feedback_events(
            self,
            *,
            workspace_id: str,
            change_request_id: str | None = None,
            event_type: str | None = None,
            limit: int = 200,
        ):
            return [
                {
                    "id": "completion-1",
                    "event_type": "coding_agent.completed",
                    "change_request_id": "cr-1",
                    "created_at": "2026-07-19T00:00:00Z",
                    "payload_json": json.dumps(
                        {"criteria": [{"criterion_id": "ac-1", "claim": "satisfied"}]}
                    ),
                }
            ]

        def refresh_change_request_gate_runs(
            self,
            change_request_id: str,
            evaluations: list[dict],
            *,
            evaluations_only: bool = False,
            workspace_id: str,
        ):
            assert evaluations_only is True
            posted.extend(evaluations)
            return [{"change_request_id": change_request_id, **evaluations[0]}]

    async def unexpected_model_setup():
        pytest.fail("missing canonical policy must stop before model setup")

    monkeypatch.setattr(board_review, "DocRegistryClient", FakeClient)
    monkeypatch.setattr(board_review, "doc_registry_base_url", lambda: "http://registry")
    monkeypatch.setattr(board_review, "_hydrate_model_settings", unexpected_model_setup)

    result = await review_change_request_delivery("cr-1", workspace_id="ws-a")

    assert result["verdict"] == "needs_human_review"
    assert result["reason_code"] == "policy_unavailable"
    assert json.loads(posted[0]["evidence"])["reason_code"] == "policy_unavailable"


@pytest.mark.asyncio
async def test_review_blocks_non_quick_work_without_a_policy_artifact(monkeypatch) -> None:
    import specgate_agents.governance.board.delivery_review as board_review

    posted: list[dict] = []

    class FakeClient:
        def __init__(self, _base_url: str):
            pass

        def get_change_request(self, _change_request_id: str, *, workspace_id: str):
            return {
                "id": "cr-1",
                "feature_id": "",
                "lead_artifact_id": "",
                "work_type": "documentation",
            }

        async def alist_acceptance_criteria(self, _change_request_id: str, *, workspace_id: str):
            return [{"id": "ac-1", "text": "Delivery remains governed"}]

        async def alist_governance_feedback_events(
            self,
            *,
            workspace_id: str,
            change_request_id: str | None = None,
            event_type: str | None = None,
            limit: int = 200,
        ):
            return [
                {
                    "id": "completion-1",
                    "event_type": "coding_agent.completed",
                    "change_request_id": "cr-1",
                    "created_at": "2026-07-19T00:00:00Z",
                    "payload_json": json.dumps(
                        {"criteria": [{"criterion_id": "ac-1", "claim": "satisfied"}]}
                    ),
                }
            ]

        def refresh_change_request_gate_runs(
            self,
            change_request_id: str,
            evaluations: list[dict],
            *,
            evaluations_only: bool = False,
            workspace_id: str,
        ):
            assert evaluations_only is True
            posted.extend(evaluations)
            return [{"change_request_id": change_request_id, **evaluations[0]}]

    async def unexpected_model_setup():
        pytest.fail("non-quick work without policy must stop before model setup")

    monkeypatch.setattr(board_review, "DocRegistryClient", FakeClient)
    monkeypatch.setattr(board_review, "doc_registry_base_url", lambda: "http://registry")
    monkeypatch.setattr(board_review, "_hydrate_model_settings", unexpected_model_setup)

    result = await review_change_request_delivery("cr-1", workspace_id="ws-a")

    assert result["verdict"] == "needs_human_review"
    assert result["reason_code"] == "policy_unavailable"
    assert json.loads(posted[0]["evidence"])["reason_code"] == "policy_unavailable"


@pytest.mark.asyncio
async def test_review_requires_canonical_acceptance_rows(monkeypatch) -> None:
    import specgate_agents.governance.board.delivery_review as board_review

    class FakeClient:
        def __init__(self, _base_url: str):
            pass

        def get_change_request(self, _change_request_id: str, *, workspace_id: str):
            return {"id": "cr-1"}

        async def alist_acceptance_criteria(self, _change_request_id: str, *, workspace_id: str):
            raise RuntimeError("canonical rows unavailable")

        async def alist_governance_feedback_events(
            self,
            *,
            workspace_id: str,
            change_request_id: str | None = None,
            event_type: str | None = None,
            limit: int = 200,
        ):
            return []

    monkeypatch.setattr(board_review, "DocRegistryClient", FakeClient)
    monkeypatch.setattr(board_review, "doc_registry_base_url", lambda: "http://registry")

    with pytest.raises(RuntimeError, match="canonical rows unavailable"):
        await review_change_request_delivery("cr-1", workspace_id="ws-a")


@pytest.mark.asyncio
async def test_review_uses_canonical_acceptance_criterion_ids(monkeypatch) -> None:
    import specgate_agents.governance.board.delivery_review as board_review

    class FakeClient:
        def __init__(self, _base_url: str):
            pass

        def get_change_request(self, _change_request_id: str, *, workspace_id: str):
            return {
                "id": "cr-1",
                "feature_id": "",
                "lead_artifact_id": "art-1",
                # Legacy mirror omits row ids; it must not be the review source.
                "acceptance_criteria_json": json.dumps(["Receipt persists"]),
            }

        async def aget_artifact(self, _artifact_id: str, *, workspace_id: str):
            assert workspace_id == "ws-a"
            return {
                "policy_snapshot_json": json.dumps(
                    {
                        "snapshot_schema_version": "specgate.policy/v1",
                        "approval_policy": "human_required",
                        "evidence_policy": "attested_ok",
                        "gate_skills": {"delivery_review": "delivery-rubric"},
                        "gate_definitions": [
                            {
                                "key": "delivery_review",
                                "version": "v1",
                                "skill_name": "delivery-rubric",
                                "skill_content": "review carefully",
                                "skill_digest": "sha256:frozen",
                            }
                        ],
                    }
                )
            }

        async def aget_skills(self, *, workspace_id: str):
            pytest.fail("delivery review must not read mutable workspace Skills")

        async def alist_acceptance_criteria(self, _change_request_id: str, *, workspace_id: str):
            return [{"id": "criterion-uuid", "text": "Receipt persists"}]

        async def alist_governance_feedback_events(
            self,
            *,
            workspace_id: str,
            change_request_id: str | None = None,
            event_type: str | None = None,
            limit: int = 200,
        ):
            return [
                {
                    "event_type": "coding_agent.completed",
                    "change_request_id": "cr-1",
                    "created_at": "2026-07-10T00:00:00Z",
                    "payload_json": json.dumps(
                        {
                            "criteria": [
                                {
                                    "criterion_id": "criterion-uuid",
                                    "text": "Receipt persists",
                                    "claim": "satisfied",
                                }
                            ]
                        }
                    ),
                }
            ]

        def refresh_change_request_gate_runs(
            self,
            change_request_id: str,
            evaluations: list[dict],
            *,
            evaluations_only: bool = False,
            workspace_id: str,
        ):
            assert evaluations_only is True
            return [
                {
                    "change_request_id": change_request_id,
                    "gate": evaluations[0]["gate"],
                    "state": evaluations[0]["state"],
                }
            ]

    async def noop_hydrate_model_settings():
        return None

    monkeypatch.setattr(board_review, "DocRegistryClient", FakeClient)
    monkeypatch.setattr(board_review, "doc_registry_base_url", lambda: "http://registry")
    monkeypatch.setattr(board_review, "_hydrate_model_settings", noop_hydrate_model_settings)
    monkeypatch.setattr(board_review, "ensure_llm_env", lambda: False)

    result = await review_change_request_delivery("cr-1", workspace_id="ws-a")

    assert result["verdict"] == "needs_human_review"
    assert [criterion["criterion_id"] for criterion in result["criteria"]] == ["criterion-uuid"]


@pytest.mark.asyncio
async def test_review_change_request_delivery_falls_back_when_model_unavailable(
    monkeypatch,
) -> None:
    import specgate_agents.governance.board.delivery_review as board_review

    class FakeClient:
        def __init__(self, _base_url: str):
            pass

        def get_change_request(self, _change_request_id: str, *, workspace_id: str):
            return {
                "id": "cr-1",
                "feature_id": "",
                "lead_artifact_id": "",
                "work_type": "bug_fix",
            }

        async def alist_acceptance_criteria(self, _change_request_id: str, *, workspace_id: str):
            return [{"id": "ac-1", "text": "Works"}]

        async def alist_governance_feedback_events(
            self,
            *,
            workspace_id: str,
            change_request_id: str | None = None,
            event_type: str | None = None,
            limit: int = 200,
        ):
            return [
                {
                    "event_type": "coding_agent.completed",
                    "change_request_id": "cr-1",
                    "created_at": "2026-06-24T00:00:00Z",
                    "payload_json": json.dumps(
                        {"criteria": [{"criterion_id": "ac-1", "claim": "satisfied"}]}
                    ),
                }
            ]

        def refresh_change_request_gate_runs(
            self,
            change_request_id: str,
            evaluations: list[dict],
            *,
            evaluations_only: bool = False,
            workspace_id: str,
        ):
            assert evaluations_only is True
            return [
                {
                    "change_request_id": change_request_id,
                    "gate": evaluations[0]["gate"],
                    "state": evaluations[0]["state"],
                }
            ]

    async def raise_provider_error(*_args, **_kwargs):
        raise RuntimeError("invalid api key")

    async def noop_hydrate_model_settings():
        return None

    monkeypatch.setattr(board_review, "DocRegistryClient", FakeClient)
    monkeypatch.setattr(board_review, "doc_registry_base_url", lambda: "http://registry")
    monkeypatch.setattr(board_review, "_hydrate_model_settings", noop_hydrate_model_settings)
    monkeypatch.setattr(board_review, "ensure_llm_env", lambda: True)
    monkeypatch.setattr(board_review, "build_model", lambda: object())
    monkeypatch.setattr(board_review, "review_delivery", raise_provider_error)

    result = await review_change_request_delivery("cr-1", workspace_id="ws-a")

    assert result["verdict"] == "needs_human_review"
    assert result["criteria"][0]["verdict"] == "met"
    assert result["gate_runs"][0]["state"] == "needs_human_review"


@pytest.mark.asyncio
async def test_review_change_request_delivery_fallback_hint_keeps_provider_detail(
    monkeypatch,
) -> None:
    import specgate_agents.governance.board.delivery_review as board_review

    class FakeClient:
        def __init__(self, _base_url: str):
            pass

        def get_change_request(self, _change_request_id: str, *, workspace_id: str):
            return {
                "id": "cr-1",
                "feature_id": "",
                "lead_artifact_id": "",
                "work_type": "bug_fix",
            }

        async def alist_acceptance_criteria(self, _change_request_id: str, *, workspace_id: str):
            return [{"id": "ac-1", "text": "Works"}]

        async def alist_governance_feedback_events(
            self,
            *,
            workspace_id: str,
            change_request_id: str | None = None,
            event_type: str | None = None,
            limit: int = 200,
        ):
            return [
                {
                    "event_type": "coding_agent.completed",
                    "change_request_id": "cr-1",
                    "created_at": "2026-06-24T00:00:00Z",
                    "payload_json": json.dumps(
                        {"criteria": [{"criterion_id": "ac-1", "claim": "satisfied"}]}
                    ),
                }
            ]

        def refresh_change_request_gate_runs(
            self,
            change_request_id: str,
            evaluations: list[dict],
            *,
            evaluations_only: bool = False,
            workspace_id: str,
        ):
            assert evaluations_only is True
            return evaluations

    class ProviderError(Exception):
        def __init__(self):
            super().__init__("Provider returned error")
            self.response = {
                "error": {
                    "metadata": {
                        "raw": "openai/gpt-oss-120b:free is temporarily rate-limited upstream"
                    }
                }
            }

    async def raise_provider_error(*_args, **_kwargs):
        raise ProviderError()

    async def noop_hydrate_model_settings():
        return None

    monkeypatch.setattr(board_review, "DocRegistryClient", FakeClient)
    monkeypatch.setattr(board_review, "doc_registry_base_url", lambda: "http://registry")
    monkeypatch.setattr(board_review, "_hydrate_model_settings", noop_hydrate_model_settings)
    monkeypatch.setattr(board_review, "ensure_llm_env", lambda: True)
    monkeypatch.setattr(board_review, "build_model", lambda: object())
    monkeypatch.setattr(board_review, "review_delivery", raise_provider_error)

    result = await review_change_request_delivery("cr-1", workspace_id="ws-a")

    assert "rate-limited upstream" in result["gate_runs"][0]["hint"]
