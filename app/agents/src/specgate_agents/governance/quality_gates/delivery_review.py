"""Post-build delivery reviewer — the Reviewer box's after-the-agent-builds step.

Given a work item's acceptance criteria and the coding agent's
``coding_agent.completed`` payload (per-AC ``criteria`` claims, automated
``checks``, ``affected_files``, ``evidence``, ``summary``), the model judges each
criterion ``met | unmet | unclear``; the overall Pass/Fail is enforced in
deterministic code (pass only if every criterion is met AND no check failed),
with the same low-confidence downgrade the quality gates use. Decisions over user
content come from the model via structured output, never keyword matching (agents
AGENTS.md). Mirrors ``quality_gates/judge.py``.
"""

from __future__ import annotations

import json
from typing import Any, Literal

from langchain_core.messages import HumanMessage
from pydantic import BaseModel, Field

from specgate_agents.governance.llm_structured import structured_output_ainvoke
from specgate_agents.governance.quality_gates.judge import (
    JUDGE_MODEL_DEFAULT,
    GateState,
    append_rubric,
    resolve_gate_state,
)

DELIVERY_REVIEW_GATE = "delivery_review"
EVAL_SUITE_VERSION = "delivery-review-v1"

CriterionVerdict = Literal["met", "unmet", "unclear"]


class CriterionReview(BaseModel):
    criterion_id: str = ""
    text: str = ""
    verdict: CriterionVerdict
    why: str = ""


class DeliveryReviewJudgment(BaseModel):
    """Raw structured output from the reviewer model."""

    criteria: list[CriterionReview]
    confidence: float = Field(ge=0.0, le=1.0)
    summary: str = ""


class DeliveryReview(BaseModel):
    """A resolved delivery verdict, shaped for persistence as a ``delivery_review`` GateRun."""

    gate: str = DELIVERY_REVIEW_GATE
    state: GateState
    criteria: list[CriterionReview]
    checks: list[dict[str, Any]] = Field(default_factory=list)
    hint: str = ""
    confidence: float = 0.0
    judge_model: str = JUDGE_MODEL_DEFAULT
    eval_suite_version: str = EVAL_SUITE_VERSION


DELIVERY_REVIEW_PROMPT = """\
You are reviewing whether a coding agent's completed work satisfies a work item's
**acceptance criteria**. You judge each criterion; you do not run code.

For EACH acceptance criterion, return a verdict:
- "met" — the agent's claim + evidence + checks credibly show this criterion is satisfied.
- "unmet" — the agent reports it not done/partial, or the evidence contradicts it.
- "unclear" — the agent under-reported: no claim, or evidence too thin to confirm.
Default to "unclear" rather than "met" when the evidence is thin — do not give
the benefit of the doubt. Quote the deciding evidence/claim in `why`.
Schema-valid file/test evidence is acceptable: do not require pasted code
excerpts when the report cites specific tests, changed files, and behavior.

Acceptance criteria (id — text):
---
{criteria}
---

The coding agent's completion report:
---
{report}
---

Return:
- criteria: one entry per acceptance criterion above (carry its id), each with verdict + why.
- confidence: 0..1, how sure you are overall.
- summary: one sentence for the reviewer (what passed / what is missing).
"""


def _resolve_overall(
    criteria: list[CriterionReview],
    checks: list[dict[str, Any]],
    confidence: float,
    *,
    evidence_policy: str = "attested_ok",
    has_corroborated: bool = False,
) -> GateState:
    """Pass only if every criterion is met AND no check failed. Any unmet criterion
    or failed check ⇒ fail; an unclear criterion (or no criteria to judge) ⇒
    needs_human_review. Then apply the shared low-confidence downgrade.

    Slice B: when evidence_policy == "corroborated_required" and has_corroborated
    is False, a pass verdict is clamped to needs_human_review — corroborated
    evidence (e.g. delivery.pr_merged webhook) is required before a high-impact
    delivery can pass. fail / needs_human_review are unaffected by the clamp.
    """
    checks_failed = any(str(c.get("status", "")).strip().lower() == "fail" for c in checks)
    verdicts = [c.verdict for c in criteria]
    if any(v == "unmet" for v in verdicts) or checks_failed:
        base: GateState = "fail"
    elif not criteria or any(v == "unclear" for v in verdicts):
        base = "needs_human_review"
    else:
        base = "pass"
    state = resolve_gate_state(base, confidence)
    if state == "pass" and evidence_policy == "corroborated_required" and not has_corroborated:
        return "needs_human_review"
    return state


def derive_review_from_claims(
    *,
    acceptance_criteria: list[dict[str, Any]],
    completed_payload: dict[str, Any],
    evidence_policy: str = "attested_ok",
    has_corroborated: bool = False,
) -> DeliveryReview:
    """Deterministic delivery verdict from the coding agent's per-AC claims.

    Used when no platform model is configured: map each reported ``claim``
    (satisfied/partial/not_done) to a criterion verdict and reuse
    ``_resolve_overall`` so the aggregation matches the model-judged path. Iterate
    the *defined* acceptance criteria so an under-reported criterion resolves to
    ``unclear`` rather than silently passing. The verdict carries the agent's
    self-assessment (``judge_model="agent_attested"``); partial/missing claims
    resolve to ``needs_human_review``, leaving the final call to a human.
    """
    claim_to_verdict: dict[str, CriterionVerdict] = {"satisfied": "met", "not_done": "unmet"}
    reported = completed_payload.get("criteria") or []
    by_id = {str(c.get("criterion_id") or "").strip(): c for c in reported if c.get("criterion_id")}
    # Claims may carry acceptance-criteria *table* ids (what `delivery report
    # --init` prefills) while the CR's embedded criteria synthesize ac-N ids.
    # Fall back to exact-text correlation so both id spaces resolve.
    by_text = {
        str(c.get("text") or "").strip(): c for c in reported if str(c.get("text") or "").strip()
    }

    reviews: list[CriterionReview] = []
    defined = acceptance_criteria or []
    if defined:
        for ac in defined:
            ac_id = str(ac.get("id") or "").strip()
            ac_text = str(ac.get("text") or "").strip()
            entry = by_id.get(ac_id) or by_text.get(ac_text) or {}
            claim = str(entry.get("claim") or "").strip().lower()
            reviews.append(
                CriterionReview(
                    criterion_id=ac_id,
                    text=str(ac.get("text") or ""),
                    verdict=claim_to_verdict.get(claim, "unclear"),
                    why=f"coding-agent claim: {claim or 'none reported'}",
                )
            )
    else:
        for c in reported:
            claim = str(c.get("claim") or "").strip().lower()
            reviews.append(
                CriterionReview(
                    criterion_id=str(c.get("criterion_id") or ""),
                    text=str(c.get("text") or ""),
                    verdict=claim_to_verdict.get(claim, "unclear"),
                    why=f"coding-agent claim: {claim or 'none reported'}",
                )
            )

    checks = list(completed_payload.get("checks") or [])
    state = _resolve_overall(
        reviews, checks, 1.0, evidence_policy=evidence_policy, has_corroborated=has_corroborated
    )
    return DeliveryReview(
        gate=DELIVERY_REVIEW_GATE,
        state=state,
        criteria=reviews,
        checks=checks,
        hint="Verdict derived from the coding agent's acceptance-criteria claims.",
        confidence=1.0,
        judge_model="agent_attested",
        eval_suite_version=EVAL_SUITE_VERSION,
    )


def _format_criteria(acceptance_criteria: list[dict[str, Any]]) -> str:
    rows = [
        f"- {str(c.get('id') or '').strip()} — {str(c.get('text') or '').strip()}"
        for c in acceptance_criteria
    ]
    return "\n".join(rows) if rows else "(none defined)"


async def review_delivery(
    *,
    acceptance_criteria: list[dict[str, Any]],
    completed_payload: dict[str, Any],
    model: Any,
    judge_model: str = JUDGE_MODEL_DEFAULT,
    config: dict[str, Any] | None = None,
    evidence_policy: str = "attested_ok",
    has_corroborated: bool = False,
    rubric: str | None = None,
) -> DeliveryReview:
    """Judge a coding-agent completion against the work item's acceptance criteria.

    evidence_policy / has_corroborated are Slice B parameters: when the artifact's
    profile requires corroborated evidence (e.g. high_impact_feature) and none is
    present, a pass verdict is clamped to needs_human_review.

    rubric (a bound review-impl Skill prompt) is appended as team policy when present
    — gate-consumes-Skills for the delivery_review gate.
    """
    checks = list(completed_payload.get("checks") or [])
    report = {
        "summary": completed_payload.get("summary", ""),
        "criteria": completed_payload.get("criteria") or [],
        "checks": checks,
        "affected_files": completed_payload.get("affected_files") or [],
        "evidence": completed_payload.get("evidence") or [],
    }
    prompt = DELIVERY_REVIEW_PROMPT.format(
        criteria=_format_criteria(acceptance_criteria),
        report=json.dumps(report, ensure_ascii=False, indent=2),
    )
    prompt = append_rubric(prompt, rubric)
    judgment = await structured_output_ainvoke(
        model, DeliveryReviewJudgment, [HumanMessage(content=prompt)], config
    )
    if judgment is None:
        raise ValueError(
            "delivery review model returned no structured output — "
            "check that the configured LLM supports tool calling"
        )
    state = _resolve_overall(
        judgment.criteria,
        checks,
        judgment.confidence,
        evidence_policy=evidence_policy,
        has_corroborated=has_corroborated,
    )
    return DeliveryReview(
        state=state,
        criteria=judgment.criteria,
        checks=checks,
        hint=judgment.summary,
        confidence=judgment.confidence,
    )
