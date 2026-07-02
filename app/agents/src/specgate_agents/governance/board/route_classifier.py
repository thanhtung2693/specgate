"""LLM-judged handoff-route classification for the proportional-ceremony lane.

A route decision answers a judgment question a deterministic rule cannot: given
a work item's title, intent, work type, and impact level, does the change
warrant the full PRD->Spec->FE/BE/QA ceremony, or a streamlined quick handoff?
The decision comes from the model via structured output, never from keyword
matching over user content (see agents AGENTS.md — no keyword tables, regex
routing, or length heuristics over user text).

The classifier only *suggests*; a human confirms or overrides the route on the
work item. The confidence-threshold downgrade is the one thing kept in
deterministic code: a low-confidence ``quick`` is escalated to the conservative
``full`` route so a small-change shortcut is never taken on thin evidence,
mirroring the quality-gate judge's low-confidence ``pass`` downgrade.
"""

from __future__ import annotations

import json
from typing import Any, Literal

from langchain_core.messages import HumanMessage
from pydantic import BaseModel, Field

from specgate_agents.governance.llm_structured import structured_output_ainvoke

# Structural route enum, not user content. ``quick`` produces the approved
# quick-mode Context Pack (no full artifact bundle); ``full`` runs the normal
# PRD/spec/tasks flow.
Route = Literal["quick", "full"]

# The conservative route the classifier falls back to on low confidence or any
# error — never silently shortcut ceremony.
CONSERVATIVE_ROUTE: Route = "full"

CLASSIFIER_MODEL_DEFAULT = "governance-route-classifier"
EVAL_SUITE_VERSION = "route-classification-v1"

ROUTE_PROMPT = """\
You are routing one software work item to the right amount of planning ceremony,
for a human to confirm. You only *suggest*; a human applies the route.

Two routes:
- "quick" — a streamlined handoff. The work is small and well-understood: a bug
  fix, a hotfix, a copy tweak, a small contained UI change, or a low-impact
  adjustment to existing behavior. It can go straight to an engineering handoff
  without drafting a full PRD/spec/FE-BE-QA bundle.
- "full" — the full planning flow (PRD -> Spec -> FE/BE/QA). The work is
  net-new, high-impact, spans multiple services, has unclear scope, or carries
  meaningful risk that planning artifacts must de-risk.

Judge the substance of the work, not any single word. A "bug_fix" that quietly
reworks a critical payment path is "full"; a "new_feature" that is a tiny
self-contained label change can be "quick".

Return:
- route: "quick" or "full".
- confidence: 0..1, how sure you are the evidence supports the route.
- rationale: one sentence explaining the suggestion for the human reviewer.

Work item (JSON):
---
{work_item}
---
"""


class RouteJudgment(BaseModel):
    """Raw structured output from the route-classifier model."""

    route: Route
    confidence: float = Field(ge=0.0, le=1.0)
    rationale: str = ""


class RouteDecision(BaseModel):
    """A reviewed-suggestion route decision, shaped for the UI to render.

    ``route`` is the resolved suggestion after the confidence downgrade: a
    low-confidence ``quick`` is escalated to ``full``. The classifier never
    applies the route — this is data a human confirms or overrides.
    """

    route: Route
    confidence: float = Field(ge=0.0, le=1.0)
    rationale: str = ""
    classifier_model: str = CLASSIFIER_MODEL_DEFAULT
    eval_suite_version: str = EVAL_SUITE_VERSION


def resolve_route(route: Route, confidence: float) -> Route:
    """Low-confidence ``quick`` -> ``full``; everything else passes through.

    Below the threshold the classifier proposes the conservative full route: a
    human reviews instead of a small-change shortcut being taken on thin
    evidence. The floor reuses the gate confidence threshold (settings
    ``governance.gate_confidence_threshold``); the default lives in
    ``provider_keys`` and is used when no setting is hydrated.
    """
    from specgate_agents.governance.provider_keys import governance_gate_confidence_threshold

    if route == "quick" and confidence < governance_gate_confidence_threshold():
        return CONSERVATIVE_ROUTE
    return route


def build_route_prompt(
    *,
    title: str,
    intent_md: str,
    work_type: str,
    impact_level: str,
) -> str:
    """Render the classifier prompt for a work-item payload.

    The fields are structural metadata + user prose bundled as JSON for the
    model to reason over — the routing *decision* lives in the model, not in any
    string match here.
    """
    work_item = {
        "title": title,
        "work_type": work_type,
        "impact_level": impact_level or "unknown",
        "intent": intent_md,
    }
    serialized = json.dumps(work_item, ensure_ascii=False, indent=2, sort_keys=True)
    return ROUTE_PROMPT.format(work_item=serialized)


async def classify_route(
    *,
    title: str,
    intent_md: str,
    work_type: str,
    impact_level: str,
    model: Any,
    classifier_model: str = CLASSIFIER_MODEL_DEFAULT,
    config: dict[str, Any] | None = None,
) -> RouteDecision:
    """Run the route classifier over a work item and resolve the route.

    Conservatively falls back to ``full`` on any error so a quick handoff is
    never suggested when the model call fails (mirrors the gate judge).
    """
    prompt = build_route_prompt(
        title=title,
        intent_md=intent_md,
        work_type=work_type,
        impact_level=impact_level,
    )
    try:
        judgment = await structured_output_ainvoke(
            model, RouteJudgment, [HumanMessage(content=prompt)], config
        )
    except Exception:  # noqa: BLE001 — conservative fallback, never shortcut ceremony.
        return RouteDecision(
            route=CONSERVATIVE_ROUTE,
            confidence=0.0,
            rationale="Route classifier unavailable; defaulting to full planning.",
            classifier_model=classifier_model,
            eval_suite_version=EVAL_SUITE_VERSION,
        )
    return RouteDecision(
        route=resolve_route(judgment.route, judgment.confidence),
        confidence=judgment.confidence,
        rationale=judgment.rationale,
        classifier_model=classifier_model,
        eval_suite_version=EVAL_SUITE_VERSION,
    )
