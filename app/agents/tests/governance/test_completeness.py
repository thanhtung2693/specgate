"""Tests for the spec-completeness judge.

Per-topic coverage comes from the model; the overall gate state is enforced
deterministically (pass only when every required topic is covered; any
missing/partial required topic → warn) with the shared low-confidence downgrade.
"""

from __future__ import annotations

import json

import pytest

from specgate_agents.governance.quality_gates.completeness import (
    SPEC_COMPLETENESS_GATE,
    CompletenessJudgment,
    TopicCoverage,
    evaluate_spec_completeness,
)


class _FakeStructured:
    def __init__(self, decision: CompletenessJudgment) -> None:
        self._decision = decision

    async def ainvoke(self, messages, config=None):  # noqa: ANN001, ANN201
        return self._decision


class _FakeModel:
    def __init__(self, decision: CompletenessJudgment) -> None:
        self._decision = decision

    def with_structured_output(self, _schema, **_kwargs):  # noqa: ANN001, ANN201
        return _FakeStructured(self._decision)


def _topics(**status_by_topic: str) -> list[TopicCoverage]:
    return [TopicCoverage(topic=t, status=s) for t, s in status_by_topic.items()]


@pytest.mark.asyncio
async def test_all_required_covered_passes() -> None:
    model = _FakeModel(
        CompletenessJudgment(
            topics=_topics(
                outcomes="covered", data_model="covered", rollout_rollback="not_applicable"
            ),
            summary="Looks complete.",
            confidence=0.9,
        )
    )
    ev = await evaluate_spec_completeness("## PRD\nx", model=model, work_type="new_feature")
    assert ev.gate == SPEC_COMPLETENESS_GATE
    assert ev.state == "pass"


@pytest.mark.asyncio
async def test_a_missing_required_topic_warns_and_rides_evidence() -> None:
    model = _FakeModel(
        CompletenessJudgment(
            topics=_topics(outcomes="covered", data_model="missing"),
            summary="Data model missing.",
            confidence=0.9,
        )
    )
    ev = await evaluate_spec_completeness("## PRD\nx", model=model, work_type="new_feature")
    assert ev.state == "warn"
    inner = json.loads(ev.evidence)  # evidence is a JSON string
    assert inner["summary"] == "Data model missing."
    assert any(t["topic"] == "data_model" and t["status"] == "missing" for t in inner["topics"])


@pytest.mark.asyncio
async def test_not_applicable_topics_do_not_block_a_pass() -> None:
    model = _FakeModel(
        CompletenessJudgment(
            topics=_topics(
                outcomes="covered",
                rollout_rollback="not_applicable",
                observability="not_applicable",
            ),
            summary="Docs change.",
            confidence=0.9,
        )
    )
    ev = await evaluate_spec_completeness("## PRD\nx", model=model, work_type="docs")
    assert ev.state == "pass"


@pytest.mark.asyncio
async def test_low_confidence_pass_escalates_to_needs_human_review() -> None:
    model = _FakeModel(
        CompletenessJudgment(
            topics=_topics(outcomes="covered", data_model="covered"),
            summary="Maybe complete.",
            confidence=0.2,
        )
    )
    ev = await evaluate_spec_completeness("## PRD\nx", model=model, work_type="new_feature")
    assert ev.gate == SPEC_COMPLETENESS_GATE
    assert ev.state == "needs_human_review"


@pytest.mark.asyncio
async def test_all_not_applicable_warns() -> None:
    model = _FakeModel(
        CompletenessJudgment(
            topics=_topics(outcomes="not_applicable", data_model="not_applicable"),
            summary="Everything marked N/A.",
            confidence=0.9,
        )
    )
    ev = await evaluate_spec_completeness("## PRD\nx", model=model, work_type="new_feature")
    assert ev.state == "warn"


def test_format_topics_lists_known_keys() -> None:
    from specgate_agents.governance.quality_gates.completeness import _format_topics

    rendered = _format_topics()
    assert "outcomes" in rendered
    assert "phased_tasks" in rendered
    assert "[REQUIRED]" not in rendered  # no profile required set → no tags


def test_format_topics_tags_profile_required_topics() -> None:
    from specgate_agents.governance.quality_gates.completeness import _format_topics

    rendered = _format_topics({"outcomes", "verification"})
    lines = {line.split(" — ")[0].lstrip("- "): line for line in rendered.splitlines()}
    assert "[REQUIRED]" in lines["outcomes"]
    assert "[REQUIRED]" in lines["verification"]
    assert "[REQUIRED]" not in lines["data_model"]  # not in the profile required set


def test_topics_include_minimum_executable_contract() -> None:
    from specgate_agents.governance.quality_gates.completeness import COMPLETENESS_TOPICS

    keys = {k for k, _ in COMPLETENESS_TOPICS}
    for required in (
        "outcomes",
        "scope",
        "non_goals",
        "acceptance_criteria",
        "constraints",
        "rollout_rollback",
        "verification",
    ):
        assert required in keys, f"min-exec-contract topic {required!r} missing"


@pytest.mark.asyncio
async def test_missing_min_exec_contract_topic_warns_and_rides_evidence() -> None:
    model = _FakeModel(
        CompletenessJudgment(
            topics=_topics(
                outcomes="covered",
                scope="covered",
                acceptance_criteria="covered",
                constraints="covered",
                verification="missing",
            ),
            summary="No verification plan.",
            confidence=0.9,
        )
    )
    ev = await evaluate_spec_completeness("## Spec\nx", model=model, work_type="new_feature")
    assert ev.state == "warn"
    inner = json.loads(ev.evidence)
    assert any(t["topic"] == "verification" and t["status"] == "missing" for t in inner["topics"])


@pytest.mark.asyncio
async def test_required_topics_override_default_gap_rollup() -> None:
    model = _FakeModel(
        CompletenessJudgment(
            topics=_topics(
                outcomes="covered",
                verification="covered",
                data_model="missing",
            ),
            summary="Enough for bug fix.",
            confidence=0.9,
        )
    )
    ev = await evaluate_spec_completeness(
        "## Spec\nx",
        model=model,
        work_type="bugfix",
        required_topics=["outcomes", "verification"],
    )
    assert ev.state == "pass"
