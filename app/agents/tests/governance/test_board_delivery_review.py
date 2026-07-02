"""Tests for the board-level delivery-review runner's payload parsing.

The judge itself is covered in ``test_delivery_review.py``; here we lock the
tolerant parsing that turns Doc Registry rows into the judge's inputs.
"""

from __future__ import annotations

import json

import pytest

from specgate_agents.governance.board.delivery_review import (
    has_pr_merged_event,
    latest_completed_payload,
    parse_acceptance_criteria,
    resolve_evidence_policy_from_snapshot,
    review_change_request_delivery,
)


def test_parse_acceptance_criteria_from_json_string_of_objects() -> None:
    raw = json.dumps(
        [
            {"id": "ac-1", "text": "Import disabled", "done": True},
            {"id": "ac-2", "text": "Show CTA"},
        ]
    )
    assert parse_acceptance_criteria(raw) == [
        {"id": "ac-1", "text": "Import disabled"},
        {"id": "ac-2", "text": "Show CTA"},
    ]


def test_parse_acceptance_criteria_tolerates_strings_and_blanks() -> None:
    assert parse_acceptance_criteria('["plain one", "  ", "plain two"]') == [
        {"id": "ac-0", "text": "plain one"},
        {"id": "ac-2", "text": "plain two"},
    ]
    assert parse_acceptance_criteria(None) == []
    assert parse_acceptance_criteria("not json") == []


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


def test_latest_completed_payload_none_when_absent() -> None:
    assert latest_completed_payload([], "cr-1") is None
    assert (
        latest_completed_payload(
            [{"event_type": "coding_agent.completed", "change_request_id": "cr-2"}], "cr-1"
        )
        is None
    )


# ── Slice B: has_pr_merged_event ───────────────────────────────────────────

def test_has_pr_merged_event_true_when_present() -> None:
    events = [
        {"event_type": "delivery.pr_merged", "change_request_id": "cr-1"},
        {"event_type": "coding_agent.completed", "change_request_id": "cr-1"},
    ]
    assert has_pr_merged_event(events, "cr-1") is True


def test_has_pr_merged_event_false_when_absent() -> None:
    events = [
        {"event_type": "coding_agent.completed", "change_request_id": "cr-1"},
        {"event_type": "delivery.pr_merged", "change_request_id": "cr-OTHER"},
    ]
    assert has_pr_merged_event(events, "cr-1") is False


def test_has_pr_merged_event_false_on_empty() -> None:
    assert has_pr_merged_event([], "cr-1") is False


# ── Slice B: resolve_evidence_policy_from_snapshot ────────────────────────

def test_resolve_evidence_policy_reads_field_from_snapshot() -> None:
    snapshot = json.dumps(
        {"approval_policy": "human_required", "evidence_policy": "corroborated_required"}
    )
    assert resolve_evidence_policy_from_snapshot(snapshot) == "corroborated_required"


def test_resolve_evidence_policy_defaults_attested_ok_when_absent() -> None:
    assert resolve_evidence_policy_from_snapshot("{}") == "attested_ok"
    assert resolve_evidence_policy_from_snapshot("") == "attested_ok"
    assert resolve_evidence_policy_from_snapshot(None) == "attested_ok"


def test_resolve_evidence_policy_defaults_on_bad_json() -> None:
    assert resolve_evidence_policy_from_snapshot("not-json") == "attested_ok"


@pytest.mark.asyncio
async def test_review_change_request_delivery_falls_back_when_model_unavailable(
    monkeypatch,
) -> None:
    import specgate_agents.governance.board.delivery_review as board_review

    class FakeClient:
        def __init__(self, _base_url: str):
            pass

        def get_change_request(self, _change_request_id: str):
            return {
                "id": "cr-1",
                "feature_id": "",
                "lead_artifact_id": "",
                "acceptance_criteria_json": json.dumps([{"id": "ac-1", "text": "Works"}]),
            }

        async def alist_governance_feedback_events(self, limit: int = 200):
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

        def refresh_change_request_gate_runs(self, change_request_id: str, evaluations: list[dict]):
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

    result = await review_change_request_delivery("cr-1")

    assert result["verdict"] == "pass"
    assert result["criteria"][0]["verdict"] == "met"
    assert result["gate_runs"][0]["state"] == "pass"
