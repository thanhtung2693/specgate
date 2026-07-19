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
from specgate_agents.governance.prompt_budget import (
    MAX_DELIVERY_CRITERIA_CHARS,
    MAX_DELIVERY_REPORT_CHARS,
    cap_model_text,
)
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
    # Name of a check in checks[] that deterministically backs this verdict.
    # Empty for LLM- or claim-judged criteria.
    verification_binding: str = ""
    # Highest per-criterion trust tier reached by this review row. Empty means
    # advisory semantic judgment only. Grounded evidence is stronger than a bare
    # claim, but only verification_binding-backed rows are deterministic.
    trust_tier: str = ""


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
  A criterion that describes a structure/schema/field-list is MET when every REQUIRED
  field is present. Do NOT require exact equality of the field set: extra fields beyond
  those listed are fine (a superset is met), and a field marked optional ("?",
  "optional", "if present") may be absent. A different-but-conformant structure is
  "met", not "unmet" — schemas need not match the criterion text character-for-character.
- "unmet" — the agent reports it not done/partial, or the evidence contradicts it. For a
  schema/structure criterion, "unmet" requires a genuinely MISSING REQUIRED field or a
  field whose value/behavior contradicts the requirement — NOT merely a different field
  set (extra fields or absent optional fields are not violations).
- "unclear" — the agent under-reported: no claim, or evidence too thin to confirm.
Default to "unclear" rather than "met" when the evidence is thin — do not give
the benefit of the doubt. Quote the deciding evidence/claim in `why`.
Schema-valid file/test evidence is acceptable: do not require pasted code
excerpts when the report cites specific tests, changed files, and behavior.
For a compound criterion, every independently verifiable clause needs matching
evidence. A generic file or test citation does not prove unasserted visual,
manual, accessibility, theme, or device-smoke clauses; return "unclear" when
any required clause lacks proof.

Judge the SUBSTANCE of each criterion, not a literal string-match of the report's
wording against the criterion text. When a criterion describes a structure, schema,
or field list:
- A field marked optional (a trailing "?", or the words "optional"/"if present")
  MAY be absent — its absence is NOT a violation.
- Extra fields beyond those the criterion lists (a superset) do NOT violate it
  unless the criterion explicitly forbids additional fields. A structure that
  contains every required field plus more satisfies the criterion.
- Missing a REQUIRED (non-optional) field, or a field whose value/behavior
  contradicts the criterion, IS a violation ("unmet").
Evaluate the actual implemented structure the evidence describes, not only the
one-line restatement the agent happened to write.

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


def _find_check(checks: list[dict[str, Any]], name: str) -> dict[str, Any] | None:
    """First check in ``checks[]`` whose ``name`` matches (trimmed). First match
    wins — check names are expected unique within a report."""
    target = name.strip()
    for check in checks:
        if str(check.get("name") or "").strip() == target:
            return check
    return None


def _resolve_bound_criterion(ac: dict[str, Any], checks: list[dict[str, Any]]) -> CriterionReview:
    """Deterministic per-criterion verdict from a ``verification_binding``. The
    criterion names a check in ``checks[]``; its verdict is taken
    from that check's observed status — no LLM:

    - matching check ``pass`` → ``met``
    - matching check ``fail`` → ``unmet``
    - check missing or ``skipped`` → ``unclear`` (a removed/unrun check must never
      silently pass; it forces human review)

    Callers must only invoke this for criteria that carry a non-empty binding.
    """
    binding = str(ac.get("verification_binding") or "").strip()
    ac_id = str(ac.get("id") or "").strip()
    text = str(ac.get("text") or "").strip()
    check = _find_check(checks, binding)
    status = str((check or {}).get("status") or "").strip()
    if status == "pass":
        verdict: CriterionVerdict = "met"
        why = f"check '{binding}' passed (deterministic)"
    elif status == "fail":
        verdict = "unmet"
        why = f"check '{binding}' failed (deterministic)"
    elif check is None:
        verdict = "unclear"
        why = f"check '{binding}' not found in report — cannot verify deterministically"
    else:
        verdict = "unclear"
        why = f"check '{binding}' was skipped — cannot verify deterministically"
    return CriterionReview(
        criterion_id=ac_id,
        text=text,
        verdict=verdict,
        why=why,
        verification_binding=binding,
        trust_tier="deterministic",
    )


def _has_binding(ac: dict[str, Any]) -> bool:
    return bool(str(ac.get("verification_binding") or "").strip())


def _order_by_ac(
    acceptance_criteria: list[dict[str, Any]], resolved: list[CriterionReview]
) -> list[CriterionReview]:
    """Return ``resolved`` ordered to follow ``acceptance_criteria``. Every
    resolved review's id is an acceptance-criterion id — bound criteria and the
    (filtered) model verdicts both come from the AC list — so there are no
    leftovers to append; the first-seen review per id wins."""
    by_id: dict[str, CriterionReview] = {}
    for review in resolved:
        by_id.setdefault(review.criterion_id.strip(), review)
    ordered: list[CriterionReview] = []
    for ac in acceptance_criteria:
        criterion_id = str(ac.get("id") or "").strip()
        review = by_id.get(criterion_id)
        ordered.append(
            review
            or CriterionReview(
                criterion_id=criterion_id,
                text=str(ac.get("text") or "").strip(),
                verdict="unclear",
                why="reviewer returned no verdict for canonical criterion id",
            )
        )
    return ordered


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

    Evidence policy can clamp a would-be pass to needs_human_review.
    ``corroborated_required`` requires independently stamped evidence or
    deterministic bindings for every criterion. fail / needs_human_review are
    unaffected by the clamp.
    """
    deterministic_criteria = bool(criteria) and all(
        c.verification_binding.strip() and c.trust_tier == "deterministic" for c in criteria
    )
    checks_failed = any(str(c.get("status", "")).strip() == "fail" for c in checks)
    verdicts = [c.verdict for c in criteria]
    if any(v == "unmet" for v in verdicts) or checks_failed:
        base: GateState = "fail"
    elif not criteria or any(v == "unclear" for v in verdicts):
        base = "needs_human_review"
    else:
        base = "pass"
    state = resolve_gate_state(base, confidence)
    if (
        state == "pass"
        and evidence_policy == "corroborated_required"
        and not has_corroborated
        and not deterministic_criteria
    ):
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
    self-assessment unless every criterion is bound to a locally reproduced
    check. Partial/missing claims resolve to ``needs_human_review``, leaving the
    final call to a human.
    """
    claim_to_verdict: dict[str, CriterionVerdict] = {
        "satisfied": "met",
        "partial": "unclear",
        "not_done": "unmet",
    }

    def claim_status(raw: Any) -> str:
        return str(raw or "").strip()

    reported = completed_payload.get("criteria") or []
    by_id = _completion_entries(completed_payload)

    checks = list(completed_payload.get("checks") or [])
    reviews: list[CriterionReview] = []
    defined = acceptance_criteria or []
    if defined:
        for ac in defined:
            # A criterion bound to a named check is resolved deterministically from
            # the check's observed status, independent of
            # the agent's self-reported claim.
            if _has_binding(ac):
                reviews.append(_resolve_bound_criterion(ac, checks))
                continue
            ac_id = str(ac.get("id") or "").strip()
            entry = by_id.get(ac_id) or {}
            claim = claim_status(entry.get("claim"))
            reviews.append(
                CriterionReview(
                    criterion_id=ac_id,
                    text=str(ac.get("text") or ""),
                    verdict=claim_to_verdict.get(claim, "unclear"),
                    why=f"coding-agent claim: {claim or 'none reported'}",
                    trust_tier=_trust_tier_for_entry(entry, claim_to_verdict.get(claim, "unclear")),
                )
            )
    else:
        for c in reported:
            claim = claim_status(c.get("claim"))
            reviews.append(
                CriterionReview(
                    criterion_id=str(c.get("criterion_id") or ""),
                    text=str(c.get("text") or ""),
                    verdict=claim_to_verdict.get(claim, "unclear"),
                    why=f"coding-agent claim: {claim or 'none reported'}",
                    trust_tier=_trust_tier_for_entry(c, claim_to_verdict.get(claim, "unclear")),
                )
            )

    state = _resolve_overall(
        reviews, checks, 1.0, evidence_policy=evidence_policy, has_corroborated=has_corroborated
    )
    all_bound = bool(defined) and all(_has_binding(ac) for ac in defined)
    some_bound = bool(defined) and any(_has_binding(ac) for ac in defined)
    hint = "Verdict derived from the coding agent's acceptance-criteria claims."
    judge_model = "agent_attested"
    if all_bound:
        hint = "Verdict derived from locally reproduced deterministic checks."
        judge_model = "deterministic_checks"
    elif some_bound:
        hint = "Verdict derived from deterministic checks and coding-agent evidence."
    return DeliveryReview(
        gate=DELIVERY_REVIEW_GATE,
        state=state,
        criteria=reviews,
        checks=checks,
        hint=hint,
        confidence=1.0,
        judge_model=judge_model,
        eval_suite_version=EVAL_SUITE_VERSION,
    )


def _format_criteria(acceptance_criteria: list[dict[str, Any]]) -> str:
    rows = [
        f"- {str(c.get('id') or '').strip()} — {str(c.get('text') or '').strip()}"
        for c in acceptance_criteria
    ]
    return "\n".join(rows) if rows else "(none defined)"


def _hydrate_criterion_texts(
    reviews: list[CriterionReview], acceptance_criteria: list[dict[str, Any]]
) -> list[CriterionReview]:
    by_id = {
        str(c.get("id") or "").strip(): str(c.get("text") or "").strip()
        for c in acceptance_criteria
        if str(c.get("id") or "").strip()
    }
    hydrated: list[CriterionReview] = []
    for review in reviews:
        if review.text.strip():
            hydrated.append(review)
            continue
        text = by_id.get(review.criterion_id.strip()) or ""
        hydrated.append(review.model_copy(update={"text": text}))
    return hydrated


def _strip_model_authored_bindings(reviews: list[CriterionReview]) -> list[CriterionReview]:
    """Model-emitted bindings are advisory metadata, not authoritative trust tier.

    Only bindings carried by the canonical acceptance criteria and resolved by
    ``_resolve_bound_criterion`` may count as deterministic evidence.
    """
    return [
        review.model_copy(update={"verification_binding": ""})
        if review.verification_binding
        else review
        for review in reviews
    ]


def _completion_entries(completed_payload: dict[str, Any]) -> dict[str, Any]:
    by_id: dict[str, Any] = {}
    for raw in completed_payload.get("criteria") or []:
        if not isinstance(raw, dict):
            continue
        criterion_id = str(raw.get("criterion_id") or "").strip()
        if criterion_id:
            by_id[criterion_id] = raw
    return by_id


def _has_grounded_evidence(entry: dict[str, Any]) -> bool:
    evidence = entry.get("evidence")
    if not isinstance(evidence, dict):
        return False
    grounding = evidence.get("grounding")
    if not isinstance(grounding, dict):
        return False
    return (
        str(grounding.get("status") or "").strip() == "grounded"
        and bool(str(grounding.get("excerpt") or "").strip())
        and str(grounding.get("digest") or "").strip().startswith("sha256:")
    )


def _trust_tier_for_entry(entry: dict[str, Any], verdict: CriterionVerdict) -> str:
    if verdict == "met" and _has_grounded_evidence(entry):
        return "grounded"
    return ""


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

    evidence_policy / has_corroborated are Slice B parameters: profile-level
    evidence policies may require corroborated evidence or deterministic
    check-backed criteria before a pass verdict is allowed to stand.

    rubric (a bound review-impl Skill prompt) is appended as team policy when present
    — gate-consumes-Skills for the delivery_review gate.
    """
    checks = list(completed_payload.get("checks") or [])
    completed_by_id = _completion_entries(completed_payload)
    bound_acs = [ac for ac in acceptance_criteria if _has_binding(ac)]
    unbound_acs = [ac for ac in acceptance_criteria if not _has_binding(ac)]

    # The LLM judges only unbound criteria. Bound criteria are resolved
    # deterministically from checks[] and are not asked of the model — its
    # {criteria} block lists only the unbound ACs, so a bound criterion's check
    # reading cannot leak into the model's other verdicts (design landmine). A
    # zero-criteria work item still invokes the model with
    # "(none defined)" so its needs_human_review verdict stands.
    llm_reviews: list[CriterionReview] = []
    confidence = 1.0
    hint = ""
    if unbound_acs or not acceptance_criteria:
        report = {
            "summary": completed_payload.get("summary", ""),
            "criteria": completed_payload.get("criteria") or [],
            "checks": checks,
            "affected_files": completed_payload.get("affected_files") or [],
            "evidence": completed_payload.get("evidence") or [],
        }
        prompt = DELIVERY_REVIEW_PROMPT.format(
            criteria=cap_model_text(_format_criteria(unbound_acs), MAX_DELIVERY_CRITERIA_CHARS),
            report=cap_model_text(
                json.dumps(report, ensure_ascii=False, indent=2),
                MAX_DELIVERY_REPORT_CHARS,
            ),
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
        llm_reviews = _strip_model_authored_bindings(
            _hydrate_criterion_texts(judgment.criteria, unbound_acs)
        )
        if acceptance_criteria:
            # Keep only verdicts for the unbound ACs we asked about — a
            # misbehaving model that emits a foreign or duplicate id must not
            # trail into aggregation and flip the gate. (Skipped when there are
            # no unbound ids.)
            unbound_ids = {str(ac.get("id") or "").strip() for ac in unbound_acs}
            llm_reviews = [r for r in llm_reviews if r.criterion_id.strip() in unbound_ids]
        llm_reviews = [
            review.model_copy(
                update={
                    "trust_tier": _trust_tier_for_entry(
                        completed_by_id.get(review.criterion_id.strip()) or {},
                        review.verdict,
                    )
                }
            )
            for review in llm_reviews
        ]
        confidence = judgment.confidence
        hint = judgment.summary

    bound_reviews = [_resolve_bound_criterion(ac, checks) for ac in bound_acs]
    if bound_reviews and not unbound_acs and acceptance_criteria and not hint:
        # Every criterion resolved deterministically (model not consulted) — give
        # the DELIVERED view a summary line instead of an empty hint.
        total = len(acceptance_criteria)
        hint = f"{len(bound_reviews)}/{total} criteria verified deterministically from checks"
    criteria = (
        _order_by_ac(acceptance_criteria, bound_reviews + llm_reviews)
        if acceptance_criteria
        else llm_reviews
    )
    state = _resolve_overall(
        criteria,
        checks,
        confidence,
        evidence_policy=evidence_policy,
        has_corroborated=has_corroborated,
    )
    return DeliveryReview(
        state=state,
        criteria=criteria,
        checks=checks,
        hint=hint,
        confidence=confidence,
    )
