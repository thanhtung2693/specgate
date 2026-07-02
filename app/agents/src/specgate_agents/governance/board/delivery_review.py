"""Run the post-build delivery review for a change request and persist the verdict.

Loads the work item's acceptance criteria + the latest ``coding_agent.completed``
feedback payload, runs the delivery-review judge, and posts the verdict as a
``delivery_review`` GateRun (visible in the quality-gates panel + the work-item
review surface's DELIVERED phase). agents owns the LLM judging; doc-registry
persists. Mirrors ``board/quality_gates.py``.
"""

from __future__ import annotations

import asyncio
import json
import logging
from typing import Any

from specgate_agents.governance.agents.factories import build_model, ensure_llm_env
from specgate_agents.governance.board.quality_gates import _load_gate_rubrics
from specgate_agents.governance.config import doc_registry_base_url
from specgate_agents.governance.provider_keys import (
    hydrate_governance_model_settings as _hydrate_model_settings,
)
from specgate_agents.governance.quality_gates.delivery_review import (
    DELIVERY_REVIEW_GATE,
    derive_review_from_claims,
    review_delivery,
)
from specgate_agents.governance.registry.client import DocRegistryClient

logger = logging.getLogger(__name__)

_COMPLETED_EVENT = "coding_agent.completed"
_PR_MERGED_EVENT = "delivery.pr_merged"
_CI_PASSED_EVENT = "delivery.ci_passed"


def resolve_evidence_policy_from_snapshot(snapshot_json: Any) -> str:
    """Read evidence_policy from a gates_profile_snapshot_json; default attested_ok.

    Empty / missing snapshot → attested_ok (safe default: no clamp on legacy artifacts).
    """
    if not snapshot_json:
        return "attested_ok"
    try:
        data = json.loads(snapshot_json) if isinstance(snapshot_json, str) else snapshot_json
        return str(data.get("evidence_policy") or "attested_ok")
    except (TypeError, ValueError):
        return "attested_ok"


def has_pr_merged_event(events: list[dict[str, Any]], change_request_id: str) -> bool:
    """Return True if a delivery.pr_merged event exists for this CR.

    A merged PR is independently-corroborated evidence from the git webhook path
    (not agent self-reported), so its presence satisfies corroborated_required.
    """
    return any(
        e.get("event_type") == _PR_MERGED_EVENT and e.get("change_request_id") == change_request_id
        for e in events
    )


def has_ci_passed_event(events: list[dict[str, Any]], change_request_id: str) -> bool:
    """Return True if a delivery.ci_passed event exists for this CR.

    A CI run is ci_verified evidence stamped by the webhook ingestion path
    (not agent self-reported), upgrading the trust tier above agent_reported.
    """
    return any(
        e.get("event_type") == _CI_PASSED_EVENT and e.get("change_request_id") == change_request_id
        for e in events
    )


def parse_acceptance_criteria(raw: Any) -> list[dict[str, Any]]:
    """Parse a CR's ``acceptance_criteria_json`` into ``[{id, text}]`` (tolerant)."""
    if not raw:
        return []
    try:
        data = json.loads(raw) if isinstance(raw, str) else raw
    except (TypeError, ValueError):
        return []
    out: list[dict[str, Any]] = []
    if isinstance(data, list):
        for i, item in enumerate(data):
            if isinstance(item, dict):
                out.append(
                    {"id": str(item.get("id") or f"ac-{i}"), "text": str(item.get("text") or "")}
                )
            elif isinstance(item, str) and item.strip():
                out.append({"id": f"ac-{i}", "text": item.strip()})
    return out


def latest_completed_payload(
    events: list[dict[str, Any]], change_request_id: str
) -> dict[str, Any] | None:
    """The newest ``coding_agent.completed`` payload for this CR, or None."""
    completed = [
        e
        for e in events
        if e.get("event_type") == _COMPLETED_EVENT
        and e.get("change_request_id") == change_request_id
    ]
    if not completed:
        return None
    completed.sort(key=lambda e: str(e.get("created_at") or ""), reverse=True)
    raw = completed[0].get("payload_json")
    try:
        return json.loads(raw) if isinstance(raw, str) else (raw or {})
    except (TypeError, ValueError):
        return {}


async def review_change_request_delivery(change_request_id: str) -> dict[str, Any]:
    """Judge the latest completion against the CR's acceptance criteria; persist the verdict."""
    client = DocRegistryClient(doc_registry_base_url())
    change_request = await asyncio.to_thread(client.get_change_request, change_request_id)
    acceptance_criteria = parse_acceptance_criteria(change_request.get("acceptance_criteria_json"))

    events = await client.alist_governance_feedback_events(limit=200)
    completed = latest_completed_payload(events, change_request_id)
    if completed is None:
        return {
            "change_request_id": change_request_id,
            "verdict": None,
            "reason": "no_completion_report",
        }

    # Slice B: resolve evidence_policy from the CR's lead artifact snapshot so the
    # delivery review knows whether corroborated evidence is required. Falls back to
    # attested_ok (no clamp) if the artifact cannot be fetched.
    evidence_policy = "attested_ok"
    artifact: dict[str, Any] = {}
    lead_artifact_id = str(change_request.get("lead_artifact_id") or "").strip()
    if not lead_artifact_id:
        feature_id = str(change_request.get("feature_id") or "").strip()
        if feature_id:
            try:
                feature = await asyncio.to_thread(client.get_workboard_feature, feature_id)
                lead_artifact_id = str(feature.get("canonical_artifact_id") or "").strip()
            except Exception:
                pass
    if lead_artifact_id:
        try:
            artifact = await client.aget_artifact(lead_artifact_id)
            evidence_policy = resolve_evidence_policy_from_snapshot(
                artifact.get("gates_profile_snapshot_json")
            )
        except Exception:
            artifact = {}

    corroborated = has_pr_merged_event(events, change_request_id)
    has_ci = has_ci_passed_event(events, change_request_id)
    # gate-consumes-Skills: a profile may bind a review-impl rubric to delivery_review.
    gate_rubrics = await _load_gate_rubrics(client, artifact)
    rubric = gate_rubrics.get(DELIVERY_REVIEW_GATE)

    await _hydrate_model_settings()
    if ensure_llm_env():
        try:
            review = await review_delivery(
                acceptance_criteria=acceptance_criteria,
                completed_payload=completed,
                model=build_model(),
                evidence_policy=evidence_policy,
                has_corroborated=corroborated,
                rubric=rubric,
            )
        except Exception as exc:
            logger.warning("Delivery review model unavailable", exc_info=True)
            review = derive_review_from_claims(
                acceptance_criteria=acceptance_criteria,
                completed_payload=completed,
                evidence_policy=evidence_policy,
                has_corroborated=corroborated,
            )
            review.hint = (
                "Verdict derived from coding-agent claims because the delivery "
                f"review model was unavailable: {str(exc)[:160]}"
            )
    else:
        # No platform model: derive the verdict from the coding agent's per-AC
        # claims (stamped agent_attested; partial/missing claims need human review).
        review = derive_review_from_claims(
            acceptance_criteria=acceptance_criteria,
            completed_payload=completed,
            evidence_policy=evidence_policy,
            has_corroborated=corroborated,
        )

    # Build corroboration evidence items so the UI can derive the trust tier.
    # kind values match deriveTrustTier() in delivery-evidence-trust.ts:
    #   pr_merged → repository_observed, ci_run → ci_verified.
    corroboration_evidence = []
    if corroborated:
        corroboration_evidence.append({"kind": "pr_merged"})
    if has_ci:
        corroboration_evidence.append({"kind": "ci_run"})

    evaluation = {
        "gate": review.gate,
        "state": review.state,
        "hint": review.hint,
        "confidence": review.confidence,
        # criteria + checks ride the evaluation's evidence string so the UI can
        # render per-AC verdicts in the DELIVERED phase. The nested `evidence`
        # array is read by gateEvidenceItems() → deriveTrustTier() in the UI.
        "evidence": json.dumps(
            {
                "criteria": [c.model_dump() for c in review.criteria],
                "checks": review.checks,
                "evidence": corroboration_evidence,
            },
            ensure_ascii=False,
        ),
        "judge_model": review.judge_model,
        "eval_suite_version": review.eval_suite_version,
    }
    gate_runs = await asyncio.to_thread(
        client.refresh_change_request_gate_runs, change_request_id, [evaluation]
    )
    return {
        "change_request_id": change_request_id,
        "verdict": review.state,
        "criteria": [c.model_dump() for c in review.criteria],
        "checks": review.checks,
        "gate_runs": gate_runs,
    }
