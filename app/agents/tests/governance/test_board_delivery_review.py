"""Tests for the board-level delivery-review runner.

Payload parsing helpers are covered in ``test_board_delivery_review_helpers.py``
and the judge itself in ``test_delivery_review.py``.
"""

from __future__ import annotations

import json

import pytest

from specgate_agents.governance.board.delivery_review import (
    review_change_request_delivery,
)


@pytest.mark.asyncio
async def test_review_scopes_feedback_queries_by_change_and_event_type(monkeypatch) -> None:
    import specgate_agents.governance.board.delivery_review as board_review

    calls: set[tuple[str | None, str | None, int]] = set()

    class FakeClient:
        def __init__(self, _base_url: str, **_kwargs):
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
        def __init__(self, _base_url: str, **_kwargs):
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
        def __init__(self, _base_url: str, **_kwargs):
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
        def __init__(self, _base_url: str, **_kwargs):
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
async def test_review_treats_work_without_an_artifact_or_feature_as_quick_route(
    monkeypatch,
) -> None:
    """Work with neither a lead artifact nor a feature is quick route whatever its
    work type, so it reviews against the built-in evidence policy.

    This used to require work_type == bug_fix to count as quick route, so a
    documentation or feature item created through the quick route went down the
    feature-policy path and was blocked as policy_unavailable for lacking a
    snapshot it could never have had."""
    import specgate_agents.governance.board.delivery_review as board_review

    posted: list[dict] = []

    class FakeClient:
        def __init__(self, _base_url: str, **_kwargs):
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

    reached_model_setup: list[bool] = []

    async def record_model_setup():
        reached_model_setup.append(True)

    monkeypatch.setattr(board_review, "DocRegistryClient", FakeClient)
    monkeypatch.setattr(board_review, "doc_registry_base_url", lambda: "http://registry")
    monkeypatch.setattr(board_review, "_hydrate_model_settings", record_model_setup)

    result = await review_change_request_delivery("cr-1", workspace_id="ws-a")

    assert reached_model_setup, (
        "quick-route work must proceed into review rather than stopping at policy resolution"
    )
    assert result.get("reason_code") != "policy_unavailable", (
        "quick-route work has no snapshot to read; blocking it for a missing policy "
        "leaves it with no way forward"
    )
    assert posted, "the review must still record a verdict"
    assert json.loads(posted[0]["evidence"]).get("reason_code") != "policy_unavailable"


@pytest.mark.asyncio
async def test_review_requires_canonical_acceptance_rows(monkeypatch) -> None:
    import specgate_agents.governance.board.delivery_review as board_review

    class FakeClient:
        def __init__(self, _base_url: str, **_kwargs):
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
        def __init__(self, _base_url: str, **_kwargs):
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
        def __init__(self, _base_url: str, **_kwargs):
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
        def __init__(self, _base_url: str, **_kwargs):
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
