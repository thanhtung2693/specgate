"""Produce an LLM-suggested handoff route for a change request.

Assembles a work-item payload (title, intent, work type, and the feature's
impact level) from Doc Registry, runs the route classifier, and returns the
structured suggestion for a human to confirm or override. It only suggests —
choosing the route (and triggering the quick-mode Context Pack) stays a human
action on the work item.
"""

from __future__ import annotations

import asyncio
import logging
from typing import Any

from specgate_agents.governance.agents.factories import build_model, ensure_llm_env
from specgate_agents.governance.board.route_classifier import (
    CONSERVATIVE_ROUTE,
    RouteDecision,
    classify_route,
)
from specgate_agents.governance.config import doc_registry_base_url
from specgate_agents.governance.provider_keys import (
    hydrate_governance_model_settings as _hydrate_model_settings,
)
from specgate_agents.governance.registry.client import DocRegistryClient

logger = logging.getLogger(__name__)


def _feature_impact_level(feature: dict[str, Any]) -> str:
    """Best-effort impact level for the work item's feature.

    Impact level is an artifact-level attribute; the governance feature DTO does not
    carry it today. When absent the classifier reasons from title/intent/work
    type instead, so an empty value is a safe ``unknown`` rather than a gate.
    """
    return str(feature.get("impact_level") or "").strip() or "unknown"


async def classify_route_for_change_request(change_request_id: str) -> dict[str, Any]:
    """Fetch a change request + feature, run the route classifier, return the suggestion.

    When the LLM environment is not configured the route falls back to the
    conservative ``full`` route (no quick shortcut) alongside the work-item
    payload it was judged on, so the caller always has the evidence to render.
    """
    client = DocRegistryClient(doc_registry_base_url())

    change_request = await asyncio.to_thread(client.get_change_request, change_request_id)
    feature: dict[str, Any] = {}
    feature_id = str(change_request.get("feature_id") or "")
    if feature_id:
        try:
            feature = await asyncio.to_thread(client.get_workboard_feature, feature_id)
        except Exception:
            logger.warning("route classifier: feature fetch failed for %s", feature_id)

    title = str(change_request.get("title") or "")
    intent_md = str(change_request.get("intent_md") or "")
    work_type = str(change_request.get("work_type") or "unknown")
    impact_level = _feature_impact_level(feature)

    decision: RouteDecision | None = None
    await _hydrate_model_settings()
    if ensure_llm_env():
        try:
            model = build_model()
            decision = await classify_route(
                title=title,
                intent_md=intent_md,
                work_type=work_type,
                impact_level=impact_level,
                model=model,
            )
        except Exception:
            logger.warning(
                "route classifier failed for change request %s; defaulting to full",
                change_request_id,
                exc_info=True,
            )

    if decision is None:
        decision = RouteDecision(
            route=CONSERVATIVE_ROUTE,
            confidence=0.0,
            rationale="LLM classifier unavailable; defaulting to full planning.",
        )

    return {
        "change_request_id": change_request_id,
        "route": decision.route,
        "confidence": decision.confidence,
        "rationale": decision.rationale,
    }
