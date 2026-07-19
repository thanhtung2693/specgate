"""Tests for model-judged quality gates.

Per the eval contract, CI uses a deterministic stub for structured-output
plumbing; live-model judgment is exercised separately. The judge keeps the
threshold->state downgrade in deterministic code, not in the model.
"""

from __future__ import annotations

import pytest

from specgate_agents.governance.provider_keys import governance_gate_confidence_threshold
from specgate_agents.governance.quality_gates.judge import (
    AC_EDGE_CASES_GATE,
    AC_VERIFIABLE_GATE,
    IMPLEMENTATION_TRACEABLE_GATE,
    ROLLBACK_PLAN_GATE,
    SCOPE_CLEAR_GATE,
    SUCCESS_METRIC_GATE,
    GateJudgment,
    evaluate_acceptance_criteria_edge_cases,
    evaluate_acceptance_criteria_verifiable,
    evaluate_all_gates,
    evaluate_implementation_plan_traceable,
    evaluate_rollback_plan,
    evaluate_scope_clear,
    evaluate_success_metric_measurable,
    resolve_gate_state,
)


def test_resolve_gate_state_downgrades_low_confidence_pass() -> None:
    # A "pass" the judge is not confident about must never persist as pass.
    floor = governance_gate_confidence_threshold()
    assert resolve_gate_state("pass", floor - 0.01) == "needs_human_review"
    assert resolve_gate_state("pass", floor) == "pass"
    assert resolve_gate_state("pass", 0.99) == "pass"


def test_resolve_gate_state_downgrades_low_confidence_fail() -> None:
    # A low-confidence "fail" is as unsafe to auto-trust as a low-confidence pass.
    floor = governance_gate_confidence_threshold()
    assert resolve_gate_state("fail", floor - 0.01) == "needs_human_review"
    assert resolve_gate_state("fail", floor) == "fail"
    assert resolve_gate_state("fail", 0.99) == "fail"


def test_resolve_gate_state_passes_warn_and_na_through() -> None:
    # warn/not_applicable are kept verbatim regardless of confidence.
    assert resolve_gate_state("warn", 0.99) == "warn"
    assert resolve_gate_state("warn", 0.01) == "warn"
    assert resolve_gate_state("not_applicable", 0.1) == "not_applicable"


class _FakeStructured:
    def __init__(self, decision: GateJudgment, sink: list[str]) -> None:
        self._decision = decision
        self._sink = sink

    async def ainvoke(self, messages, config=None) -> GateJudgment:  # noqa: ANN001
        # Record the rendered prompt text so tests can assert routed sections.
        for m in messages:
            self._sink.append(str(getattr(m, "content", m)))
        return self._decision


class _FakeGateModel:
    """Minimal stand-in: with_structured_output(schema).ainvoke() -> canned decision.

    ``prompts`` accumulates every rendered prompt string passed to the judge so
    tests can assert which artifact sections each gate received.

    When called with ``CompletenessJudgment`` schema (from the spec_completeness
    gate), a compatible default is returned so existing tests don't fail on the
    new gate.
    """

    def __init__(self, decision: GateJudgment) -> None:
        self._decision = decision
        self.prompts: list[str] = []

    def with_structured_output(self, schema, **_kwargs):  # noqa: ANN001, ANN201
        from specgate_agents.governance.quality_gates.completeness import (  # noqa: PLC0415
            CompletenessJudgment,
            TopicCoverage,
        )

        if schema is CompletenessJudgment:
            completeness_decision = CompletenessJudgment(
                topics=[TopicCoverage(topic="outcomes", status="covered")], confidence=0.9
            )
            return _FakeStructured(completeness_decision, self.prompts)  # type: ignore[arg-type]
        return _FakeStructured(self._decision, self.prompts)


@pytest.mark.asyncio
async def test_evaluate_rollback_plan_confident_pass() -> None:
    model = _FakeGateModel(
        GateJudgment(verdict="pass", confidence=0.92, hint="Rollback via flag documented.")
    )
    ev = await evaluate_rollback_plan("# Spec\n\nRollback: disable feature flag X.", model=model)
    assert ev.gate == ROLLBACK_PLAN_GATE
    assert ev.state == "pass"
    assert ev.confidence == 0.92
    assert ev.hint == "Rollback via flag documented."
    assert ev.judge_model and ev.eval_suite_version


@pytest.mark.asyncio
async def test_evaluate_rollback_plan_low_confidence_escalates() -> None:
    model = _FakeGateModel(
        GateJudgment(verdict="pass", confidence=0.4, hint="Unsure rollback is real.")
    )
    ev = await evaluate_rollback_plan("# Spec\n\nmaybe revert somehow", model=model)
    assert ev.state == "needs_human_review"


@pytest.mark.asyncio
async def test_evaluate_rollback_plan_keeps_warn() -> None:
    model = _FakeGateModel(GateJudgment(verdict="warn", confidence=0.8, hint="Rollback is vague."))
    ev = await evaluate_rollback_plan("# Spec\n\nrollback: TBD", model=model)
    assert ev.state == "warn"
    assert ev.hint == "Rollback is vague."


@pytest.mark.asyncio
async def test_evaluate_acceptance_criteria_verifiable_pass() -> None:
    model = _FakeGateModel(
        GateJudgment(verdict="pass", confidence=0.9, hint="Every criterion is observable.")
    )
    ev = await evaluate_acceptance_criteria_verifiable(
        "# PRD\n\n- Import button disabled when supplier status = disconnected.", model=model
    )
    assert ev.gate == AC_VERIFIABLE_GATE
    assert ev.state == "pass"


@pytest.mark.asyncio
async def test_evaluate_acceptance_criteria_verifiable_warns_on_vague_criteria() -> None:
    model = _FakeGateModel(
        GateJudgment(
            verdict="warn",
            confidence=0.85,
            hint="'works well' is not testable — restate as an observable check.",
        )
    )
    ev = await evaluate_acceptance_criteria_verifiable(
        "# PRD\n\n- The feature works well.", model=model
    )
    assert ev.state == "warn"
    assert "restate" in ev.hint or "testable" in ev.hint


@pytest.mark.asyncio
async def test_evaluate_all_gates_includes_ac_verifiable() -> None:
    model = _FakeGateModel(GateJudgment(verdict="pass", confidence=0.9))
    evals = await evaluate_all_gates({"spec": "Import disabled when disconnected."}, model=model)
    gates = {e.gate for e in evals}
    assert AC_VERIFIABLE_GATE in gates
    assert len(evals) == 7


@pytest.mark.asyncio
async def test_evaluate_all_gates_can_filter_enabled_gates() -> None:
    model = _FakeGateModel(GateJudgment(verdict="pass", confidence=0.9))
    evals = await evaluate_all_gates(
        {"spec": "Import disabled when disconnected."},
        model=model,
        enabled_gates=(SCOPE_CLEAR_GATE, "spec_completeness"),
    )
    gates = [e.gate for e in evals]
    assert gates == [SCOPE_CLEAR_GATE, "spec_completeness"]
    assert len(model.prompts) == 2


@pytest.mark.asyncio
async def test_evaluate_acceptance_criteria_edge_cases_pass() -> None:
    model = _FakeGateModel(
        GateJudgment(verdict="pass", confidence=0.85, hint="ACs cover edge cases.")
    )
    ev = await evaluate_acceptance_criteria_edge_cases(
        "# AC\n- Empty cart handled\n- Network failure handled", model=model
    )
    assert ev.gate == AC_EDGE_CASES_GATE
    assert ev.state == "pass"


@pytest.mark.asyncio
async def test_evaluate_success_metric_measurable_fail() -> None:
    model = _FakeGateModel(
        GateJudgment(verdict="fail", confidence=0.9, hint="Define measurable success.")
    )
    ev = await evaluate_success_metric_measurable("# Metrics\n- Improve performance", model=model)
    assert ev.gate == SUCCESS_METRIC_GATE
    assert ev.state == "fail"


@pytest.mark.asyncio
async def test_evaluate_scope_clear_warn() -> None:
    model = _FakeGateModel(
        GateJudgment(verdict="warn", confidence=0.75, hint="Scope boundaries unclear.")
    )
    ev = await evaluate_scope_clear("# Scope\n- Maybe include auth changes", model=model)
    assert ev.gate == SCOPE_CLEAR_GATE
    assert ev.state == "warn"


@pytest.mark.asyncio
async def test_evaluate_implementation_plan_traceable_low_confidence_escalates() -> None:
    model = _FakeGateModel(
        GateJudgment(verdict="pass", confidence=0.5, hint="Uncertain traceability.")
    )
    ev = await evaluate_implementation_plan_traceable("# Plan\n- Step 1\n- Step 2", model=model)
    assert ev.gate == IMPLEMENTATION_TRACEABLE_GATE
    assert ev.state == "needs_human_review"


@pytest.mark.asyncio
async def test_evaluate_all_gates_returns_all_gates() -> None:
    model = _FakeGateModel(GateJudgment(verdict="pass", confidence=0.9, hint="Looks good."))
    artifact_bundle = {"spec": "# Spec\n\nFull plan with rollback, AC, metrics, scope, impl."}
    results = await evaluate_all_gates(artifact_bundle, model=model)
    gates = {ev.gate for ev in results}
    assert ROLLBACK_PLAN_GATE in gates
    assert AC_EDGE_CASES_GATE in gates
    assert AC_VERIFIABLE_GATE in gates
    assert SUCCESS_METRIC_GATE in gates
    assert SCOPE_CLEAR_GATE in gates
    assert IMPLEMENTATION_TRACEABLE_GATE in gates
    assert "spec_completeness" in gates
    assert len(results) == 7


# A role-keyed bundle with distinctive per-role sentinel strings so we can prove routing.
_ROUTING_BUNDLE = {
    "spec": "SPEC_SENTINEL spec body",
    "design": "DESIGN_SENTINEL design body",
    "plan": "PLAN_SENTINEL implementation tasks",
    "verification": "VERIFICATION_SENTINEL qa tasks",
    "reference": "REFERENCE_SENTINEL rollout and risks",
}


def _capture_per_gate(model: _FakeGateModel) -> dict[str, str]:
    """Map gate order -> rendered prompt. evaluate_all_gates runs gates in a fixed order."""
    # Order matches the asyncio.gather call in evaluate_all_gates.
    order = [
        ROLLBACK_PLAN_GATE,
        AC_EDGE_CASES_GATE,
        AC_VERIFIABLE_GATE,
        SUCCESS_METRIC_GATE,
        SCOPE_CLEAR_GATE,
        IMPLEMENTATION_TRACEABLE_GATE,
        "spec_completeness",
    ]
    assert len(model.prompts) == len(order)
    return dict(zip(order, model.prompts, strict=True))


@pytest.mark.asyncio
async def test_evaluate_all_gates_routes_sections_per_gate() -> None:
    model = _FakeGateModel(GateJudgment(verdict="pass", confidence=0.9, hint="ok"))
    await evaluate_all_gates(_ROUTING_BUNDLE, model=model)
    by_gate = _capture_per_gate(model)

    # implementation_plan_traceable sees Spec + Plan + Verification, not Reference.
    trace = by_gate[IMPLEMENTATION_TRACEABLE_GATE]
    assert "PLAN_SENTINEL" in trace
    assert "VERIFICATION_SENTINEL" in trace
    assert "SPEC_SENTINEL" in trace
    assert "REFERENCE_SENTINEL" not in trace
    assert "DESIGN_SENTINEL" not in trace

    # rollback_plan_present sees Spec + Reference (rollout/risks live in reference).
    rollback = by_gate[ROLLBACK_PLAN_GATE]
    assert "REFERENCE_SENTINEL" in rollback
    assert "SPEC_SENTINEL" in rollback
    assert "PLAN_SENTINEL" not in rollback

    # acceptance_criteria_edge_cases sees Spec + Verification (AC live in the spec role).
    ac = by_gate[AC_EDGE_CASES_GATE]
    assert "SPEC_SENTINEL" in ac
    assert "VERIFICATION_SENTINEL" in ac
    assert "PLAN_SENTINEL" not in ac

    # Sections are labeled.
    assert "## Verification" in ac
    assert "## Reference" in rollback


@pytest.mark.asyncio
async def test_evaluate_all_gates_work_type_in_prompt() -> None:
    model = _FakeGateModel(GateJudgment(verdict="pass", confidence=0.9, hint="ok"))
    await evaluate_all_gates(_ROUTING_BUNDLE, model=model, work_type="research_spike")
    assert all("research_spike" in p for p in model.prompts)


@pytest.mark.asyncio
async def test_judge_prompt_instructs_missing_applicable_is_fail() -> None:
    # The guidance must steer the model to fail (not not_applicable) for a
    # missing-but-expected section; we assert the instruction is present and that
    # a mocked fail verdict for an empty rollback section survives as fail.
    model = _FakeGateModel(
        GateJudgment(verdict="fail", confidence=0.9, hint="Add a rollback plan.")
    )
    # Bundle with no spec/reference — the rollback section is empty.
    results = await evaluate_all_gates({"plan": "Plan only"}, model=model, work_type="feature")
    rollback = next(ev for ev in results if ev.gate == ROLLBACK_PLAN_GATE)
    assert rollback.state == "fail"
    # The instruction text is rendered into the prompt.
    assert any('is missing or empty, that is "fail"' in p for p in model.prompts)
    assert any("change type should" in p for p in model.prompts)


@pytest.mark.asyncio
async def test_evaluate_all_gates_includes_spec_completeness() -> None:
    from specgate_agents.governance.quality_gates.completeness import (
        CompletenessJudgment,
        TopicCoverage,
    )
    from specgate_agents.governance.quality_gates.judge import (
        GateJudgment,
        evaluate_all_gates,
    )

    class _SchemaAwareFake:
        def with_structured_output(self, schema, **_kwargs):  # noqa: ANN001, ANN201
            decision = (
                CompletenessJudgment(
                    topics=[TopicCoverage(topic="outcomes", status="covered")], confidence=0.9
                )
                if schema is CompletenessJudgment
                else GateJudgment(verdict="pass", confidence=0.9, hint="", evidence="")
            )

            class _Runnable:
                async def ainvoke(self, _messages, _config=None):  # noqa: ANN001, ANN202
                    return decision

            return _Runnable()

    evals = await evaluate_all_gates(
        {"spec": "# Spec"}, model=_SchemaAwareFake(), work_type="new_feature"
    )
    gates = {e.gate for e in evals}
    assert "spec_completeness" in gates
    completeness = next(e for e in evals if e.gate == "spec_completeness")
    # The fake returns a single covered topic at high confidence → the advisory roll-up passes.
    assert completeness.state == "pass"


@pytest.mark.asyncio
async def test_evidence_flows_into_evaluation() -> None:
    model = _FakeGateModel(
        GateJudgment(
            verdict="pass",
            confidence=0.9,
            hint="ok",
            evidence="Rollout: disable flag X via LaunchDarkly.",
        )
    )
    ev = await evaluate_rollback_plan("## Rollout\nDisable flag X.", model=model)
    assert ev.evidence == "Rollout: disable flag X via LaunchDarkly."
    # model_dump() carries evidence into the posted evaluations[] payload.
    assert ev.model_dump()["evidence"] == "Rollout: disable flag X via LaunchDarkly."


# --- gate-consumes-Skills: rubric injection ---


@pytest.mark.asyncio
async def test_judge_injects_rubric_as_team_policy_section() -> None:
    # A bound Skill's prompt is appended as a team-policy rubric the judge applies.
    model = _FakeGateModel(GateJudgment(verdict="pass", confidence=0.9))
    await evaluate_rollback_plan(
        "Spec: rollback via flag.",
        model=model,
        rubric="ALWAYS require a tested backout procedure. Use {placeholder} safely.",
    )
    joined = "\n".join(model.prompts)
    assert "ALWAYS require a tested backout procedure" in joined
    # Rubric is injected AFTER str.format(), so literal braces must survive intact.
    assert "{placeholder}" in joined


@pytest.mark.asyncio
async def test_judge_without_rubric_has_no_policy_section() -> None:
    model = _FakeGateModel(GateJudgment(verdict="pass", confidence=0.9))
    await evaluate_rollback_plan("Spec: x.", model=model)
    joined = "\n".join(model.prompts).lower()
    assert "team policy" not in joined


@pytest.mark.asyncio
async def test_evaluate_all_gates_routes_rubric_to_bound_gate_only() -> None:
    model = _FakeGateModel(GateJudgment(verdict="pass", confidence=0.9))
    await evaluate_all_gates(
        {"spec": "Some spec text."},
        model=model,
        enabled_gates=[SCOPE_CLEAR_GATE, ROLLBACK_PLAN_GATE],
        gate_rubrics={SCOPE_CLEAR_GATE: "SCOPE_RUBRIC_SENTINEL"},
    )
    joined = "\n".join(model.prompts)
    # The bound gate (scope_clear) gets the rubric; the unbound one (rollback) does not.
    assert joined.count("SCOPE_RUBRIC_SENTINEL") == 1
