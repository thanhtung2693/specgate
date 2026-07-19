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
from specgate_agents.governance.board.quality_gates import _frozen_gate_rubrics
from specgate_agents.governance.config import doc_registry_base_url
from specgate_agents.governance.provider_keys import (
    hydrate_governance_model_settings as _hydrate_model_settings,
)
from specgate_agents.governance.quality_gates.delivery_review import (
    DELIVERY_REVIEW_GATE,
    CriterionReview,
    derive_review_from_claims,
    review_delivery,
)
from specgate_agents.governance.quality_gates.profile_snapshot import parse_profile_snapshot
from specgate_agents.governance.registry.client import DocRegistryClient

logger = logging.getLogger(__name__)

_COMPLETED_EVENT = "coding_agent.completed"
_PEER_REVIEWED_EVENT = "coding_agent.peer_reviewed"
_PR_MERGED_EVENT = "delivery.pr_merged"


def resolve_evidence_policy_from_snapshot(snapshot_json: Any) -> str:
    """Read evidence_policy from a policy_snapshot_json.

    An absent snapshot is valid for intentional quick work and uses the built-in
    ``attested_ok`` policy. Non-empty snapshots are versioned governance records;
    malformed or unsupported values must fail closed.
    """
    if not snapshot_json:
        return "attested_ok"
    raw = snapshot_json if isinstance(snapshot_json, str) else json.dumps(snapshot_json)
    policy = parse_profile_snapshot(raw).evidence_policy
    if not policy:
        raise ValueError("evidence_policy is required in a persisted policy snapshot")
    if policy not in {"attested_ok", "corroborated_required"}:
        raise ValueError(f"unsupported evidence policy: {policy!r}")
    return policy


async def _persist_policy_unavailable(
    client: DocRegistryClient,
    change_request_id: str,
    *,
    workspace_id: str,
    completion_feedback_event_id: str,
) -> dict[str, Any]:
    """Persist an authoritative block when artifact-backed policy cannot be read."""
    hint = (
        "Governing delivery policy is unavailable; SpecGate did not assume "
        "a weaker evidence policy. Restore the approved artifact and retry."
    )
    evaluation = {
        "gate": DELIVERY_REVIEW_GATE,
        "state": "needs_human_review",
        "hint": hint,
        "confidence": 1.0,
        "evidence": json.dumps(
            {
                "reason_code": "policy_unavailable",
                "completion_feedback_event_id": completion_feedback_event_id,
                "criteria": [],
                "checks": [],
            },
            ensure_ascii=False,
        ),
        "judge_model": "deterministic_policy_guard",
        "eval_suite_version": "delivery-review-v1",
    }
    gate_runs = await asyncio.to_thread(
        client.refresh_change_request_gate_runs,
        change_request_id,
        [evaluation],
        evaluations_only=True,
        workspace_id=workspace_id,
    )
    return {
        "change_request_id": change_request_id,
        "verdict": "needs_human_review",
        "reason_code": "policy_unavailable",
        "criteria": [],
        "checks": [],
        "gate_runs": gate_runs,
    }


def model_error_hint(exc: Exception) -> str:
    """Extract the most actionable provider error without exposing full internals."""
    candidates: list[Any] = [
        getattr(exc, "response", None),
        getattr(exc, "response_data", None),
        getattr(exc, "body", None),
        getattr(exc, "error", None),
        exc.args[0] if exc.args else None,
    ]
    for candidate in candidates:
        detail = _provider_error_detail(candidate)
        if detail:
            return detail[:240]
    return str(exc)[:240]


def _provider_error_detail(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, str):
        raw = value.strip()
        if not raw:
            return ""
        try:
            parsed = json.loads(raw)
        except json.JSONDecodeError:
            return raw
        return _provider_error_detail(parsed) or raw
    if isinstance(value, dict):
        error = value.get("error")
        if isinstance(error, dict):
            metadata = error.get("metadata")
            if isinstance(metadata, dict):
                raw = str(metadata.get("raw") or "").strip()
                if raw:
                    return raw
            message = str(error.get("message") or "").strip()
            if message:
                return message
        for key in ("message", "detail", "raw"):
            detail = str(value.get(key) or "").strip()
            if detail:
                return detail
    return ""


def corroboration_evidence(
    events: list[dict[str, Any]],
    change_request_id: str,
    completed_payload: dict[str, Any],
) -> list[dict[str, str]]:
    """Return merged repository evidence bound to the latest completion head."""
    expected = (
        str((completed_payload.get("git_receipt") or {}).get("head_revision") or "").strip().lower()
    )
    if not expected:
        return []
    for event in events:
        if (
            event.get("event_type") == _PR_MERGED_EVENT
            and event.get("change_request_id") == change_request_id
            and str(_event_payload(event).get("head_sha") or "").strip().lower() == expected
        ):
            return [{"kind": "pr_merged"}]
    return []


def latest_event_payload(
    events: list[dict[str, Any]], change_request_id: str, event_type: str
) -> dict[str, Any] | None:
    """The newest payload for ``event_type`` on this CR, or None."""
    event = latest_event(events, change_request_id, event_type)
    return _event_payload(event) if event else None


def _event_payload(event: dict[str, Any] | None) -> dict[str, Any]:
    if not event:
        return {}
    raw = event.get("payload_json")
    try:
        return json.loads(raw) if isinstance(raw, str) else (raw or {})
    except (TypeError, ValueError):
        return {}


def latest_event(
    events: list[dict[str, Any]], change_request_id: str, event_type: str
) -> dict[str, Any] | None:
    matches = [
        event
        for event in events
        if event.get("event_type") == event_type
        and event.get("change_request_id") == change_request_id
    ]
    if not matches:
        return None
    return max(
        matches,
        key=lambda event: (
            str(event.get("created_at") or ""),
            str(event.get("id") or ""),
        ),
    )


def valid_bound_peer_review_payload(
    peer_event: dict[str, Any] | None, completed_event: dict[str, Any] | None
) -> dict[str, Any] | None:
    """Return a peer payload only when it reviewed this exact completion receipt."""
    peer = _event_payload(peer_event)
    completed = _event_payload(completed_event)
    binding = peer.get("peer_review_of") if isinstance(peer, dict) else None
    if not isinstance(binding, dict) or not completed_event:
        return None
    expected_event_id = str(binding.get("completion_feedback_event_id") or "")
    if expected_event_id != str(completed_event.get("id") or ""):
        return None
    if binding.get("git_receipt") != completed.get("git_receipt"):
        return None
    return peer


def latest_valid_peer_review_payload(
    events: list[dict[str, Any]], change_request_id: str, completed_event: dict[str, Any] | None
) -> dict[str, Any] | None:
    peers = [
        event
        for event in events
        if event.get("event_type") == _PEER_REVIEWED_EVENT
        and event.get("change_request_id") == change_request_id
    ]
    for peer in sorted(
        peers,
        key=lambda event: (
            str(event.get("created_at") or ""),
            str(event.get("id") or ""),
        ),
        reverse=True,
    ):
        if payload := valid_bound_peer_review_payload(peer, completed_event):
            return payload
    return None


def latest_completed_payload(
    events: list[dict[str, Any]], change_request_id: str
) -> dict[str, Any] | None:
    """The newest ``coding_agent.completed`` payload for this CR, or None."""
    return latest_event_payload(events, change_request_id, _COMPLETED_EVENT)


def _peer_satisfied_ids(peer_payload: dict[str, Any] | None) -> set[str]:
    if not peer_payload:
        return set()
    out: set[str] = set()
    for raw in peer_payload.get("criteria") or []:
        if not isinstance(raw, dict):
            continue
        claim = str(raw.get("claim") or "").strip()
        if claim != "satisfied":
            continue
        criterion_id = str(raw.get("criterion_id") or "").strip()
        if criterion_id:
            out.add(criterion_id)
    return out


def apply_peer_review_tiers(
    criteria: list[CriterionReview], peer_payload: dict[str, Any] | None
) -> list[CriterionReview]:
    """Mark matching met criteria as peer-reviewed without overriding stronger tiers."""
    satisfied = _peer_satisfied_ids(peer_payload)
    if not satisfied:
        return criteria
    upgraded = []
    for review in criteria:
        if (
            review.verdict == "met"
            and not review.trust_tier
            and review.criterion_id.strip() in satisfied
        ):
            upgraded.append(review.model_copy(update={"trust_tier": "peer_reviewed"}))
        else:
            upgraded.append(review)
    return upgraded


def peer_review_covers_attested_met_criteria(
    criteria: list[CriterionReview], peer_payload: dict[str, Any] | None
) -> bool:
    """A peer must affirm every met criterion that lacks stronger evidence."""
    independently_resolved = {"deterministic", "repository_observed"}
    required = {
        criterion.criterion_id.strip()
        for criterion in criteria
        if criterion.verdict == "met"
        and criterion.trust_tier not in independently_resolved
        and criterion.criterion_id.strip()
    }
    return required.issubset(_peer_satisfied_ids(peer_payload))


async def review_change_request_delivery(
    change_request_id: str, *, workspace_id: str
) -> dict[str, Any]:
    """Judge the latest completion against the CR's acceptance criteria; persist the verdict."""
    workspace_id = workspace_id.strip()
    if not workspace_id:
        raise ValueError("workspace_id is required")
    client = DocRegistryClient(doc_registry_base_url())
    workspace_kw = {"workspace_id": workspace_id}
    change_request = await asyncio.to_thread(
        client.get_change_request, change_request_id, **workspace_kw
    )
    criterion_rows = await client.alist_acceptance_criteria(change_request_id, **workspace_kw)
    acceptance_criteria = [
        {
            "id": str(row.get("id") or f"ac-{index}"),
            "text": str(row.get("text") or ""),
            "verification_binding": str(row.get("verification_binding") or ""),
        }
        for index, row in enumerate(criterion_rows)
    ]

    event_groups = await asyncio.gather(
        *(
            client.alist_governance_feedback_events(
                workspace_id=workspace_id,
                change_request_id=change_request_id,
                event_type=event_type,
                # ponytail: API caps lists at 200; add server-side binding filters if
                # real per-CR peer/merge histories ever exceed that ceiling.
                limit=1 if event_type == _COMPLETED_EVENT else 200,
            )
            for event_type in (
                _COMPLETED_EVENT,
                _PEER_REVIEWED_EVENT,
                _PR_MERGED_EVENT,
            )
        )
    )
    events = [event for group in event_groups for event in group]
    completed_event = latest_event(events, change_request_id, _COMPLETED_EVENT)
    completed = _event_payload(completed_event)
    peer_reviewed = latest_valid_peer_review_payload(events, change_request_id, completed_event)
    if completed_event is None:
        return {
            "change_request_id": change_request_id,
            "verdict": None,
            "reason": "no_completion_report",
        }
    completion_feedback_event_id = str(completed_event.get("id") or "")

    # Resolve evidence_policy from the CR's lead artifact snapshot so the
    # delivery review knows whether corroborated evidence is required. Only
    # quick-route bug fixes use the built-in default; every other route fails
    # closed when its frozen snapshot is unavailable.
    evidence_policy = "attested_ok"
    artifact: dict[str, Any] = {}
    lead_artifact_id = str(change_request.get("lead_artifact_id") or "").strip()
    is_quick_route = (
        not lead_artifact_id and str(change_request.get("work_type") or "").strip() == "bug_fix"
    )
    if not lead_artifact_id and not is_quick_route:
        feature_id = str(change_request.get("feature_id") or "").strip()
        if not feature_id:
            return await _persist_policy_unavailable(
                client,
                change_request_id,
                workspace_id=workspace_id,
                completion_feedback_event_id=completion_feedback_event_id,
            )
        try:
            feature = await asyncio.to_thread(
                client.get_workboard_feature, feature_id, **workspace_kw
            )
            lead_artifact_id = str(feature.get("canonical_artifact_id") or "").strip()
            if not lead_artifact_id:
                raise ValueError("feature canonical policy artifact unavailable")
        except Exception:
            logger.warning("Delivery review policy feature unavailable", exc_info=True)
            return await _persist_policy_unavailable(
                client,
                change_request_id,
                workspace_id=workspace_id,
                completion_feedback_event_id=completion_feedback_event_id,
            )
    if lead_artifact_id:
        try:
            artifact = await client.aget_artifact(lead_artifact_id, **workspace_kw)
            policy_snapshot = artifact.get("policy_snapshot_json")
            if not policy_snapshot:
                raise ValueError("artifact policy snapshot unavailable")
            evidence_policy = resolve_evidence_policy_from_snapshot(policy_snapshot)
        except Exception:
            logger.warning("Delivery review policy artifact unavailable", exc_info=True)
            return await _persist_policy_unavailable(
                client,
                change_request_id,
                workspace_id=workspace_id,
                completion_feedback_event_id=completion_feedback_event_id,
            )

    corroboration = corroboration_evidence(events, change_request_id, completed)
    corroborated = bool(corroboration)
    # The delivery rubric is frozen into the lead artifact's policy snapshot.
    gate_rubrics = _frozen_gate_rubrics(artifact)
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
                f"review model was unavailable: {model_error_hint(exc)}"
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
    review.criteria = apply_peer_review_tiers(review.criteria, peer_reviewed)
    if (
        review.judge_model == "agent_attested"
        and review.state == "pass"
        and not peer_review_covers_attested_met_criteria(review.criteria, peer_reviewed)
    ):
        review.state = "needs_human_review"
        review.hint = (f"{review.hint} " if review.hint else "") + (
            "A different agent must peer-review this exact completion receipt, "
            "or a human must review it."
        )

    # Persist completion-bound repository corroboration with the review.
    evaluation = {
        "gate": review.gate,
        "state": review.state,
        "hint": review.hint,
        "confidence": review.confidence,
        # Criteria, checks, and corroboration ride the evaluation's evidence
        # string so all delivery-status clients read the same review snapshot.
        "evidence": json.dumps(
            {
                "completion_feedback_event_id": completion_feedback_event_id,
                "criteria": [c.model_dump() for c in review.criteria],
                "checks": review.checks,
                "evidence": corroboration,
            },
            ensure_ascii=False,
        ),
        "judge_model": review.judge_model,
        "eval_suite_version": review.eval_suite_version,
    }
    gate_runs = await asyncio.to_thread(
        client.refresh_change_request_gate_runs,
        change_request_id,
        [evaluation],
        evaluations_only=True,
        **workspace_kw,
    )
    return {
        "change_request_id": change_request_id,
        "verdict": review.state,
        "criteria": [c.model_dump() for c in review.criteria],
        "checks": review.checks,
        "gate_runs": gate_runs,
    }
