"""Tests for the proportional-ceremony route classifier.

Per the eval contract, CI uses a deterministic stub for the structured-output
decision; live-model judgment is exercised separately. The classifier keeps the
confidence threshold -> route downgrade and the error fallback in deterministic
code, not in the model.
"""

from __future__ import annotations

import pytest

from specgate_agents.governance.board.route_classifier import (
    CONSERVATIVE_ROUTE,
    RouteJudgment,
    classify_route,
    resolve_route,
)
from specgate_agents.governance.provider_keys import governance_gate_confidence_threshold


def test_resolve_route_downgrades_low_confidence_quick() -> None:
    floor = governance_gate_confidence_threshold()
    assert resolve_route("quick", floor - 0.01) == "full"
    assert resolve_route("quick", floor) == "quick"
    assert resolve_route("quick", 0.99) == "quick"


def test_resolve_route_passes_full_through_regardless_of_confidence() -> None:
    assert resolve_route("full", 0.01) == "full"
    assert resolve_route("full", 0.99) == "full"


class _FakeStructured:
    def __init__(self, decision: RouteJudgment) -> None:
        self._decision = decision

    async def ainvoke(self, _messages, config=None) -> RouteJudgment:  # noqa: ANN001
        return self._decision


class _FakeRouteModel:
    """Minimal stand-in: with_structured_output(schema).ainvoke() -> canned decision."""

    def __init__(self, decision: RouteJudgment) -> None:
        self._decision = decision

    def with_structured_output(self, _schema, **_kwargs):  # noqa: ANN001, ANN201
        return _FakeStructured(self._decision)


class _BoomModel:
    def with_structured_output(self, _schema, **_kwargs):  # noqa: ANN001, ANN201
        raise RuntimeError("model unavailable")


@pytest.mark.asyncio
async def test_classify_route_suggests_quick_for_low_impact_bug_fix() -> None:
    model = _FakeRouteModel(
        RouteJudgment(route="quick", confidence=0.92, rationale="Small contained bug fix.")
    )
    decision = await classify_route(
        title="Fix import error copy",
        intent_md="Surface the auth error message on a failed product import.",
        work_type="bug_fix",
        impact_level="low",
        model=model,
    )
    assert decision.route == "quick"
    assert decision.confidence == 0.92
    assert "bug fix" in decision.rationale.lower()


@pytest.mark.asyncio
async def test_classify_route_suggests_full_for_high_impact_net_new() -> None:
    model = _FakeRouteModel(
        RouteJudgment(
            route="full", confidence=0.9, rationale="Net-new multi-service capability."
        )
    )
    decision = await classify_route(
        title="Launch loyalty points at checkout",
        intent_md="Build a new loyalty earning + redemption flow across checkout and payments.",
        work_type="new_feature",
        impact_level="high",
        model=model,
    )
    assert decision.route == "full"


@pytest.mark.asyncio
async def test_classify_route_downgrades_low_confidence_quick_to_full() -> None:
    floor = governance_gate_confidence_threshold()
    model = _FakeRouteModel(
        RouteJudgment(route="quick", confidence=floor - 0.05, rationale="Unsure.")
    )
    decision = await classify_route(
        title="Tweak something",
        intent_md="Maybe small.",
        work_type="bug_fix",
        impact_level="unknown",
        model=model,
    )
    assert decision.route == "full"


@pytest.mark.asyncio
async def test_classify_route_falls_back_to_full_on_error() -> None:
    decision = await classify_route(
        title="anything",
        intent_md="anything",
        work_type="bug_fix",
        impact_level="low",
        model=_BoomModel(),
    )
    assert decision.route == CONSERVATIVE_ROUTE
    assert decision.confidence == 0.0
