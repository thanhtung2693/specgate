"""Tests for the quality-gate eval gold data.

The CI path is deterministic: a fake judge returns each fixture's intended
verdict/confidence, the real ``evaluate_*`` function resolves it to a gate
state, and the scorer compares that to the fixture's expected state. No LLM or
network calls run here. A single opt-in ``live_smoke`` test runs the real judge
for calibration.
"""

from __future__ import annotations

import pytest

from evals.quality_gates.gold_data import (
    GATE_EVALUATORS,
    GOLD_FIXTURES,
    GateEvalCase,
    run_gate_for_case,
    score_gate_evaluations,
)
from specgate_agents.governance.quality_gates.judge import (
    ALL_LLM_GATES,
    SPEC_COMPLETENESS_GATE,
    GateEvaluation,
    GateJudgment,
    resolve_gate_state,
)

# spec_completeness resolves its state from per-topic coverage (advisory,
# warn-capped), not a single verdict, so it doesn't fit this fixture harness —
# it has its own suite in tests/governance/test_completeness.py. Every other LLM
# gate must be exercisable here.
_HARNESS_EXCLUDED_GATES = {SPEC_COMPLETENESS_GATE}


class _FakeStructured:
    def __init__(self, decision: GateJudgment) -> None:
        self._decision = decision

    async def ainvoke(self, _messages, config=None) -> GateJudgment:  # noqa: ANN001
        return self._decision


class _FakeGateModel:
    """with_structured_output(schema).ainvoke() -> a canned per-fixture decision."""

    def __init__(self, decision: GateJudgment) -> None:
        self._decision = decision

    def with_structured_output(self, _schema, **_kwargs):  # noqa: ANN001, ANN201
        return _FakeStructured(self._decision)


def _fake_model_for(case: GateEvalCase) -> _FakeGateModel:
    return _FakeGateModel(
        GateJudgment(
            verdict=case.judge_verdict,
            confidence=case.judge_confidence,
            hint="",
        )
    )


def test_gold_fixtures_load_and_are_valid() -> None:
    assert len(GOLD_FIXTURES) >= 10  # at least one pass + one fail/warn per gate
    case_ids = [c.case_id for c in GOLD_FIXTURES]
    assert len(case_ids) == len(set(case_ids)), "fixture case_ids must be unique"
    for case in GOLD_FIXTURES:
        assert case.gate in GATE_EVALUATORS, f"unknown gate {case.gate!r}"
        assert case.artifact_md.strip(), f"{case.case_id} has empty artifact"
        # expected_state must be the deterministic resolution of the raw verdict.
        assert case.expected_state == resolve_gate_state(case.judge_verdict, case.judge_confidence)


def test_harness_covers_every_verdict_gate() -> None:
    # The harness must evaluate every LLM gate except the documented exclusion;
    # a new verdict gate added to ALL_LLM_GATES without wiring fails here.
    assert set(GATE_EVALUATORS) == set(ALL_LLM_GATES) - _HARNESS_EXCLUDED_GATES


def test_every_gate_has_a_pass_and_a_non_pass_fixture() -> None:
    for gate in GATE_EVALUATORS:
        states = {c.expected_state for c in GOLD_FIXTURES if c.gate == gate}
        assert "pass" in states, f"{gate} missing a clear-pass fixture"
        assert states - {"pass"}, f"{gate} missing a fail/warn/escalate fixture"


@pytest.mark.asyncio
async def test_low_confidence_pass_fixture_downgrades_to_needs_human_review() -> None:
    case = next(c for c in GOLD_FIXTURES if c.case_id == "rollback-low-confidence-pass-escalates")
    assert case.judge_verdict == "pass"
    assert case.expected_state == "needs_human_review"
    ev = await run_gate_for_case(case, model=_fake_model_for(case))
    assert ev.state == "needs_human_review"


@pytest.mark.asyncio
async def test_each_fixture_scores_against_fake_judge() -> None:
    # Each fixture, judged by a fake returning its intended verdict, must score 1.0.
    for case in GOLD_FIXTURES:
        ev = await run_gate_for_case(case, model=_fake_model_for(case))
        score, diags = score_gate_evaluations([(ev, case)])
        assert score == 1.0, f"{case.case_id} did not score perfectly: {diags}"
        assert ev.gate == case.gate


@pytest.mark.asyncio
async def test_full_suite_aggregates_to_one_against_faithful_fake_judge() -> None:
    pairs = []
    for case in GOLD_FIXTURES:
        ev = await run_gate_for_case(case, model=_fake_model_for(case))
        pairs.append((ev, case))
    score, diags = score_gate_evaluations(pairs)
    assert score == 1.0, f"aggregate below 1.0 against faithful judge: {diags}"


@pytest.mark.asyncio
async def test_scorer_penalizes_wrong_state() -> None:
    # A judge that always returns a confident pass should fail the fail/warn fixtures.
    pairs = []
    for case in GOLD_FIXTURES:
        wrong_model = _FakeGateModel(GateJudgment(verdict="pass", confidence=0.99, hint=""))
        ev = await run_gate_for_case(case, model=wrong_model)
        pairs.append((ev, case))
    score, diags = score_gate_evaluations(pairs)
    assert score < 1.0
    assert diags
    # Every non-pass fixture should appear as a mismatch.
    non_pass = [c.case_id for c in GOLD_FIXTURES if c.expected_state != "pass"]
    for case_id in non_pass:
        assert any(case_id in d for d in diags), f"expected mismatch diag for {case_id}"


def test_score_gate_evaluations_handles_empty() -> None:
    score, diags = score_gate_evaluations([])
    assert score == 0.0
    assert diags


def test_score_gate_evaluations_flags_gate_mismatch() -> None:
    case = GOLD_FIXTURES[0]
    wrong_gate_eval = GateEvaluation(
        gate="some_other_gate",
        state=case.expected_state,
        confidence=case.judge_confidence,
    )
    score, diags = score_gate_evaluations([(wrong_gate_eval, case)])
    assert score == 0.0
    assert any("gate=" in d for d in diags)


# ---------------------------------------------------------------------------
# Live calibration — real LLM judge (opt-in via GOVERNANCE_LIVE_SMOKE=1)
# ---------------------------------------------------------------------------


@pytest.mark.live_smoke
@pytest.mark.asyncio
async def test_quality_gates_live_smoke() -> None:
    """Live calibration: run the real gate judge over the gold fixtures.

    Gate-judge API keys live in Doc Registry settings, not env vars, so we
    hydrate via the gate judge's own ``_hydrate_model_settings`` before building
    the mini model. Run with:

        GOVERNANCE_LIVE_SMOKE=1 uv run pytest -m live_smoke --override-ini="addopts="
    """
    from specgate_agents.governance.agents.factories import build_model, ensure_llm_env
    from specgate_agents.governance.board.quality_gates import _hydrate_model_settings

    # Hydrate provider keys from Doc Registry (keys stored there, not in env).
    await _hydrate_model_settings()

    if not ensure_llm_env("mini"):
        pytest.skip("LLM env not configured")

    model = build_model()
    pairs = []
    for case in GOLD_FIXTURES:
        # The confidence-downgrade to needs_human_review is offline-only — a real
        # judge never emits that state, so those fixtures only exercise
        # resolve_gate_state in the deterministic path, not live calibration.
        if case.expected_state == "needs_human_review":
            continue
        ev = await run_gate_for_case(case, model=model)
        pairs.append((ev, case))

    score, diags = score_gate_evaluations(pairs)
    assert score >= 0.75, (
        f"quality-gate live calibration below floor: score={score:.2f}, diags={diags}"
    )
