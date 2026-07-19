"""Model-judged quality gates.

Each gate asks a question that a presence check cannot answer — it requires
judgment. Decisions come from the model via structured output, never from keyword
matching over artifact text (see agents AGENTS.md). The confidence-threshold
downgrade is the one thing kept in deterministic code: a low-confidence ``pass``
*or* ``fail`` is escalated to ``needs_human_review`` (per spec §FR-3.4 / §FR-0.4).

Each gate is routed only the artifact sections it needs (impl plans for
traceability, rollout/risks for rollback, QA for edge cases) and the change's
``work_type``, so the model can tell a genuinely-inapplicable gate apart from a
missing-but-expected section.

Every model-judged gate follows the same shape: a prompt template, a gate-name
constant, an ``evaluate_*`` function that accepts an artifact string + model,
and a shared ``GateJudgment`` / ``GateEvaluation`` schema.
"""

from __future__ import annotations

import asyncio
from typing import Any, Literal

from langchain_core.messages import HumanMessage
from pydantic import BaseModel, Field

from specgate_agents.governance.attachments import (
    AUDIENCE_BOTH,
    AUDIENCE_GATE,
    render_attachments_section,
)
from specgate_agents.governance.config import doc_registry_base_url
from specgate_agents.governance.llm_structured import structured_output_ainvoke
from specgate_agents.governance.prompt_budget import (
    MAX_ARTIFACT_CHARS,
    MAX_RUBRIC_CHARS,
    cap_model_text,
)

GateState = Literal["pass", "warn", "fail", "needs_human_review", "not_applicable"]

ROLLBACK_PLAN_GATE = "rollback_plan_present"
AC_EDGE_CASES_GATE = "acceptance_criteria_edge_cases"
AC_VERIFIABLE_GATE = "acceptance_criteria_verifiable"
SUCCESS_METRIC_GATE = "success_metric_measurable"
SCOPE_CLEAR_GATE = "scope_clear"
IMPLEMENTATION_TRACEABLE_GATE = "implementation_plan_traceable"

SPEC_COMPLETENESS_GATE = "spec_completeness"

ALL_LLM_GATES = (
    ROLLBACK_PLAN_GATE,
    AC_EDGE_CASES_GATE,
    AC_VERIFIABLE_GATE,
    SUCCESS_METRIC_GATE,
    SCOPE_CLEAR_GATE,
    IMPLEMENTATION_TRACEABLE_GATE,
    SPEC_COMPLETENESS_GATE,
)

JUDGE_MODEL_DEFAULT = "governance-gate-judge"
EVAL_SUITE_VERSION = "quality-gate-v1"

_VERDICT_GUIDE = """\
This change's type is: {work_type}

Return:
- verdict:
  - "pass" — clearly satisfied.
  - "warn" — partially satisfied or unclear.
  - "fail" — absent or plainly inadequate.
  - "not_applicable" — the gate genuinely does not apply to THIS change type.
- confidence: 0..1, how sure you are.
- hint: one actionable sentence for the author (what to add or fix).
- evidence: a short supporting quote from the artifact, or the section name you
  relied on (empty if there is nothing to cite).

Choose "not_applicable" ONLY when the gate genuinely does not apply to this
change type — e.g. a research spike or a pure docs change may legitimately need
no rollback plan or success metric. If a section that THIS change type should
have is missing or empty, that is "fail", not "not_applicable".

Artifact:
---
{artifact}
---
"""

ROLLBACK_PLAN_PROMPT = (
    "You are reviewing a software change's governance artifact for one quality gate:\n"
    "**is there a credible rollback plan?**\n\n"
    "A rollback plan describes how to safely undo the change: a feature flag, a "
    "revert/backout procedure, data cleanup if needed, and a clear trigger.\n"
    "Judge the content, not the word 'rollback'.\n\n" + _VERDICT_GUIDE
)

AC_EDGE_CASES_PROMPT = (
    "You are reviewing a software change's governance artifact for one quality gate:\n"
    "**do the acceptance criteria cover meaningful edge cases?**\n\n"
    "Good ACs address empty/null inputs, concurrent users, failure modes, "
    "permission boundaries, and retry/idempotency where relevant.\n"
    "Only flag gaps that matter for this feature's risk level.\n\n" + _VERDICT_GUIDE
)

AC_VERIFIABLE_PROMPT = (
    "You are reviewing a software change's governance artifact for one quality gate:\n"
    "**is each acceptance criterion verifiable — observable and testable?**\n\n"
    "A verifiable criterion describes an observable outcome a person or test can "
    "confirm (a specific UI state, an API response, a value, a behavior under a "
    "named condition). Vague, subjective, or non-testable criteria — 'works well', "
    "'is fast', 'handles errors gracefully', 'good UX', 'as expected' — are the "
    "problem: a coding agent cannot prove them and the delivery review cannot judge "
    "them. This gate is the upstream half of post-build verification — clearer "
    "criteria make the delivery Pass/Fail materially more accurate.\n\n"
    "When you `warn`, the hint MUST name the offending criterion and **restate it as "
    "an observable check** (e.g. \"'fast' → 'list renders in < 500ms for 1k rows'\"). "
    "Criteria that are all observable are a `pass`. No acceptance criteria present at "
    "all (when this change type should have them) is a `warn`, not `not_applicable`.\n\n"
    + _VERDICT_GUIDE
)

SUCCESS_METRIC_PROMPT = (
    "You are reviewing a software change's governance artifact for one quality gate:\n"
    "**are the success metrics measurable and verifiable?**\n\n"
    "A measurable metric names a signal, a target, a timeframe, and a way to verify it "
    "(e.g. 'p95 checkout latency < 300 ms after rollout, verified via Datadog'). "
    "'Improve performance' is not measurable. 'Works correctly' is not measurable.\n\n"
    + _VERDICT_GUIDE
)

SCOPE_CLEAR_PROMPT = (
    "You are reviewing a software change's governance artifact for one quality gate:\n"
    "**is the scope well-defined and bounded?**\n\n"
    "Good scope states what IS included, what is explicitly OUT of scope, and "
    "which systems or services are affected. Vague statements like 'may include X' "
    "or 'depends on future decisions' are scope gaps.\n\n" + _VERDICT_GUIDE
)

IMPLEMENTATION_TRACEABLE_PROMPT = (
    "You are reviewing a software change's governance artifact for one quality gate:\n"
    "**does the implementation plan match the spec — does it trace back to the spec's "
    "requirements and acceptance criteria?**\n\n"
    "Each implementation task should map to a requirement or AC. Orphan tasks with no "
    "stated purpose, or ACs with no implementation coverage, are traceability gaps.\n\n"
    + _VERDICT_GUIDE
)


class GateJudgment(BaseModel):
    verdict: GateState
    confidence: float = Field(ge=0.0, le=1.0)
    hint: str = ""
    evidence: str = ""


class GateEvaluation(BaseModel):
    """An agent gate verdict, shaped for POST /gate-runs/refresh evaluations[]."""

    gate: str
    state: GateState
    hint: str = ""
    confidence: float = Field(ge=0.0, le=1.0)
    evidence: str = ""
    judge_model: str = JUDGE_MODEL_DEFAULT
    eval_suite_version: str = EVAL_SUITE_VERSION


def resolve_gate_state(verdict: GateState, confidence: float) -> GateState:
    """Low-confidence ``pass`` or ``fail`` → ``needs_human_review``; others pass through.

    A low-confidence ``fail`` is as unsafe to auto-trust as a low-confidence
    ``pass`` — both warrant a human look. ``warn`` and ``not_applicable`` pass
    through verbatim regardless of confidence.

    The confidence floor is user-configurable (settings ``governance.gate_confidence_threshold``);
    the default lives in ``provider_keys`` and is used when no setting is hydrated.
    """
    from specgate_agents.governance.provider_keys import governance_gate_confidence_threshold

    if verdict in ("pass", "fail") and confidence < governance_gate_confidence_threshold():
        return "needs_human_review"
    return verdict


def append_rubric(prompt: str, rubric: str | None) -> str:
    """Append a bound Skill's prompt as a team-policy rubric the judge applies.

    The rubric is policy to judge BY (an instruction), kept distinct from the
    artifact (content to judge). Injected AFTER ``str.format`` so literal braces
    in the Skill prompt never break templating, and returns the prompt unchanged
    when no rubric is bound (graceful fallback to the built-in prompt).
    """
    if not rubric or not rubric.strip():
        return prompt
    return (
        f"{prompt}\n\n"
        "Additional team policy / rubric — apply these standards in your judgment:\n"
        "---\n"
        f"{cap_model_text(rubric.strip(), MAX_RUBRIC_CHARS)}\n"
        "---"
    )


async def _judge(
    gate: str,
    prompt_template: str,
    artifact_md: str,
    *,
    model: Any,
    judge_model: str,
    work_type: str = "",
    config: dict[str, Any] | None,
    rubric: str | None = None,
) -> GateEvaluation:
    prompt = prompt_template.format(
        artifact=cap_model_text(artifact_md.strip(), MAX_ARTIFACT_CHARS),
        work_type=work_type.strip() or "unspecified",
    )
    prompt = append_rubric(prompt, rubric)
    judgment = await structured_output_ainvoke(
        model, GateJudgment, [HumanMessage(content=prompt)], config
    )
    return GateEvaluation(
        gate=gate,
        state=resolve_gate_state(judgment.verdict, judgment.confidence),
        hint=judgment.hint,
        confidence=judgment.confidence,
        evidence=judgment.evidence,
        judge_model=judge_model,
        eval_suite_version=EVAL_SUITE_VERSION,
    )


async def evaluate_rollback_plan(
    artifact_md: str,
    *,
    model: Any,
    judge_model: str = JUDGE_MODEL_DEFAULT,
    work_type: str = "",
    config: dict[str, Any] | None = None,
    rubric: str | None = None,
) -> GateEvaluation:
    """Judge the ``rollback_plan_present`` gate."""
    return await _judge(
        ROLLBACK_PLAN_GATE,
        ROLLBACK_PLAN_PROMPT,
        artifact_md,
        model=model,
        judge_model=judge_model,
        work_type=work_type,
        config=config,
        rubric=rubric,
    )


async def evaluate_acceptance_criteria_edge_cases(
    artifact_md: str,
    *,
    model: Any,
    judge_model: str = JUDGE_MODEL_DEFAULT,
    work_type: str = "",
    config: dict[str, Any] | None = None,
    rubric: str | None = None,
) -> GateEvaluation:
    """Judge the ``acceptance_criteria_edge_cases`` gate."""
    return await _judge(
        AC_EDGE_CASES_GATE,
        AC_EDGE_CASES_PROMPT,
        artifact_md,
        model=model,
        judge_model=judge_model,
        work_type=work_type,
        config=config,
        rubric=rubric,
    )


async def evaluate_acceptance_criteria_verifiable(
    artifact_md: str,
    *,
    model: Any,
    judge_model: str = JUDGE_MODEL_DEFAULT,
    work_type: str = "",
    config: dict[str, Any] | None = None,
    rubric: str | None = None,
) -> GateEvaluation:
    """Judge the ``acceptance_criteria_verifiable`` gate."""
    return await _judge(
        AC_VERIFIABLE_GATE,
        AC_VERIFIABLE_PROMPT,
        artifact_md,
        model=model,
        judge_model=judge_model,
        work_type=work_type,
        config=config,
        rubric=rubric,
    )


async def evaluate_success_metric_measurable(
    artifact_md: str,
    *,
    model: Any,
    judge_model: str = JUDGE_MODEL_DEFAULT,
    work_type: str = "",
    config: dict[str, Any] | None = None,
    rubric: str | None = None,
) -> GateEvaluation:
    """Judge the ``success_metric_measurable`` gate."""
    return await _judge(
        SUCCESS_METRIC_GATE,
        SUCCESS_METRIC_PROMPT,
        artifact_md,
        model=model,
        judge_model=judge_model,
        work_type=work_type,
        config=config,
        rubric=rubric,
    )


async def evaluate_scope_clear(
    artifact_md: str,
    *,
    model: Any,
    judge_model: str = JUDGE_MODEL_DEFAULT,
    work_type: str = "",
    config: dict[str, Any] | None = None,
    rubric: str | None = None,
) -> GateEvaluation:
    """Judge the ``scope_clear`` gate."""
    return await _judge(
        SCOPE_CLEAR_GATE,
        SCOPE_CLEAR_PROMPT,
        artifact_md,
        model=model,
        judge_model=judge_model,
        work_type=work_type,
        config=config,
        rubric=rubric,
    )


async def evaluate_implementation_plan_traceable(
    artifact_md: str,
    *,
    model: Any,
    judge_model: str = JUDGE_MODEL_DEFAULT,
    work_type: str = "",
    config: dict[str, Any] | None = None,
    rubric: str | None = None,
) -> GateEvaluation:
    """Judge the ``implementation_plan_traceable`` gate."""
    return await _judge(
        IMPLEMENTATION_TRACEABLE_GATE,
        IMPLEMENTATION_TRACEABLE_PROMPT,
        artifact_md,
        model=model,
        judge_model=judge_model,
        work_type=work_type,
        config=config,
        rubric=rubric,
    )


# Per-section markdown labels for the assembled artifact string. Keys are document
# roles, matching the bundle produced by ``client.aload_artifact_bundle_by_role``.
_SECTION_LABELS: dict[str, str] = {
    "spec": "Spec",
    "design": "Design",
    "plan": "Plan",
    "verification": "Verification",
    "research": "Research",
    "reference": "Reference",
}


def _labeled_sections(artifact_bundle: dict[str, str], keys: tuple[str, ...]) -> str:
    """Assemble a labeled markdown string from the selected, non-empty bundle keys.

    Unknown roles (``unspecified``/``custom:*``) fall back to a title-cased label so
    arbitrary-framework documents still reach the gates that select them."""
    parts: list[str] = []
    for key in keys:
        text = (artifact_bundle.get(key) or "").strip()
        if not text:
            continue
        label = _SECTION_LABELS.get(key, key.replace("_", " ").replace(":", ": ").title())
        parts.append(f"## {label}\n{text}")
    return "\n\n".join(parts)


def _register_gate_if_enabled(
    tasks: list[Any],
    *,
    selected: set[str],
    gate: str,
    evaluator: Any,
    artifact_md: str,
    model: Any,
    judge_model: str,
    work_type: str,
    config: dict[str, Any] | None,
    rubrics: dict[str, str],
    extra_kwargs: dict[str, Any] | None = None,
) -> None:
    if gate not in selected:
        return
    kwargs = {
        "model": model,
        "judge_model": judge_model,
        "work_type": work_type,
        "config": config,
        "rubric": rubrics.get(gate),
    }
    if extra_kwargs:
        kwargs.update(extra_kwargs)
    tasks.append(evaluator(artifact_md, **kwargs))


async def evaluate_all_gates(
    artifact_bundle: dict[str, str],
    *,
    model: Any,
    judge_model: str = JUDGE_MODEL_DEFAULT,
    work_type: str = "",
    config: dict[str, Any] | None = None,
    attachments: list[dict[str, Any]] | None = None,
    enabled_gates: tuple[str, ...] | list[str] | None = None,
    required_topics: list[str] | None = None,
    gate_rubrics: dict[str, str] | None = None,
) -> list[GateEvaluation]:
    """Run all model-judged gates concurrently against routed artifact sections.

    ``artifact_bundle`` is a mapping of document ROLES to markdown text
    (``spec``, ``design``, ``plan``, ``verification``, ``research``, ``reference``,
    plus any ``unspecified``/``custom:*``) — from ``client.aload_artifact_bundle_by_role``.
    Each gate is routed only the labeled sections it needs:

    - rollback_plan_present → Spec + Reference
    - acceptance_criteria_edge_cases → Spec + Verification
    - acceptance_criteria_verifiable → Spec + Verification
    - success_metric_measurable → Spec
    - scope_clear → Spec
    - implementation_plan_traceable → Spec + Plan + Verification
    - spec_completeness → all roles (minimum-executable-contract + build-readiness)

    Empty sections are omitted. If a gate's sections are all empty it receives
    an empty string; combined with ``work_type``, the model decides whether the
    gate is genuinely ``not_applicable`` or a missing-but-expected ``fail``.
    """
    # Function-level import avoids a circular import: completeness.py imports from
    # judge.py at module level, so a top-level import here would form a cycle.
    from specgate_agents.governance.quality_gates.completeness import evaluate_spec_completeness

    rollback_md = _labeled_sections(artifact_bundle, ("spec", "reference"))
    ac_md = _labeled_sections(artifact_bundle, ("spec", "verification"))
    ac_verifiable_md = _labeled_sections(artifact_bundle, ("spec", "verification"))
    metric_md = _labeled_sections(artifact_bundle, ("spec",))
    scope_md = _labeled_sections(artifact_bundle, ("spec",))
    traceable_md = _labeled_sections(artifact_bundle, ("spec", "plan", "verification"))

    # The completeness judge assesses coverage across ALL documents, so it gets the
    # whole labeled bundle (every role present, in stable order) — unlike the routed
    # single-topic gates above.
    full_md = _labeled_sections(artifact_bundle, tuple(sorted(artifact_bundle.keys())))

    # Gate-audience reference attachments (bug repros, examples, links the product
    # team marked gate/both) ground the edge-case and scope gates — the reviewer
    # judges acceptance-criteria completeness and scope clarity against them.
    att_md = "\n".join(
        render_attachments_section(
            attachments,
            audiences=(AUDIENCE_GATE, AUDIENCE_BOTH),
            base_url=doc_registry_base_url(),
        )
    ).strip()
    if att_md:
        ac_md = f"{ac_md}\n\n{att_md}".strip()
        ac_verifiable_md = f"{ac_verifiable_md}\n\n{att_md}".strip()
        scope_md = f"{scope_md}\n\n{att_md}".strip()

    selected = set(enabled_gates or ALL_LLM_GATES)
    # gate_rubrics contains immutable Skill content frozen into the policy snapshot;
    # an unbound gate gets None and runs with its built-in prompt.
    rubrics = gate_rubrics or {}
    tasks: list[Any] = []
    gate_specs = (
        (ROLLBACK_PLAN_GATE, evaluate_rollback_plan, rollback_md, None),
        (AC_EDGE_CASES_GATE, evaluate_acceptance_criteria_edge_cases, ac_md, None),
        (AC_VERIFIABLE_GATE, evaluate_acceptance_criteria_verifiable, ac_verifiable_md, None),
        (SUCCESS_METRIC_GATE, evaluate_success_metric_measurable, metric_md, None),
        (SCOPE_CLEAR_GATE, evaluate_scope_clear, scope_md, None),
        (
            IMPLEMENTATION_TRACEABLE_GATE,
            evaluate_implementation_plan_traceable,
            traceable_md,
            None,
        ),
        (
            SPEC_COMPLETENESS_GATE,
            evaluate_spec_completeness,
            full_md,
            {"required_topics": required_topics},
        ),
    )
    for gate, evaluator, artifact_md, extra_kwargs in gate_specs:
        _register_gate_if_enabled(
            tasks,
            selected=selected,
            gate=gate,
            evaluator=evaluator,
            artifact_md=artifact_md,
            model=model,
            judge_model=judge_model,
            work_type=work_type,
            config=config,
            rubrics=rubrics,
            extra_kwargs=extra_kwargs,
        )
    if not tasks:
        return []
    evals = await asyncio.gather(*tasks)
    return list(evals)
