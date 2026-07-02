"""Run LLM-judged quality gates and persist readiness snapshots to Doc Registry."""

from __future__ import annotations

import asyncio
import json
import logging
from typing import Any

from specgate_agents.governance.agents.factories import build_model, ensure_llm_env
from specgate_agents.governance.config import doc_registry_base_url
from specgate_agents.governance.provider_keys import (
    hydrate_governance_model_settings as _hydrate_model_settings,
)
from specgate_agents.governance.quality_gates.judge import GateEvaluation, evaluate_all_gates
from specgate_agents.governance.quality_gates.profile_snapshot import (
    ProfileSnapshot,
    UnsupportedSnapshotVersion,
    parse_profile_snapshot,
)
from specgate_agents.governance.registry.client import DocRegistryClient

logger = logging.getLogger(__name__)


def _parse_artifact_snapshot(artifact: dict[str, Any]) -> ProfileSnapshot | None:
    """Parse the artifact's gates_profile_snapshot_json.

    Returns ``None`` on empty/missing input (callers fall back to defaults).
    Raises ``UnsupportedSnapshotVersion`` for unknown explicit versions (fail-closed).
    Logs and returns ``None`` on corrupt JSON so the readiness run still proceeds
    with defaults rather than aborting — corrupt input is not the same as an
    unknown version.
    """
    raw = artifact.get("gates_profile_snapshot_json")
    if not isinstance(raw, str) or not raw.strip():
        return None
    try:
        return parse_profile_snapshot(raw)
    except UnsupportedSnapshotVersion:
        raise
    except Exception:
        logger.warning(
            "artifact %s has invalid gates_profile_snapshot_json",
            artifact.get("id"),
            exc_info=True,
        )
        return None


def _profile_readiness_config(
    artifact: dict[str, Any],
) -> tuple[list[str] | None, list[str] | None, list[str] | None]:
    """Extract (enabled_gates, required_topics, required_roles) from the profile snapshot."""
    try:
        snapshot = _parse_artifact_snapshot(artifact)
    except UnsupportedSnapshotVersion:
        logger.warning(
            "artifact %s has unsupported snapshot schema version; "
            "falling back to no profile config",
            artifact.get("id"),
        )
        return None, None, None
    if snapshot is None:
        return None, None, None
    return (
        snapshot.enabled_gates or None,
        snapshot.required_topics or None,
        snapshot.required_roles or None,
    )


def _profile_gate_skills(artifact: dict[str, Any]) -> dict[str, str]:
    """Extract the gate_skills map (gate_key -> skill_name) from the profile snapshot."""
    try:
        snapshot = _parse_artifact_snapshot(artifact)
    except UnsupportedSnapshotVersion:
        return {}
    if snapshot is None:
        return {}
    return snapshot.gate_skills


def resolve_gate_rubrics(
    gate_skills: dict[str, str], skills: list[dict[str, Any]]
) -> dict[str, str]:
    """Map each gate to its bound Skill's prompt text (the rubric the judge applies).

    A gate whose bound skill is missing or whose prompt is blank is dropped, so the
    gate falls back to its built-in prompt — gate-consumes-Skills degrades gracefully.
    """
    by_name = {str(s.get("name") or ""): str(s.get("prompt") or "") for s in (skills or [])}
    out: dict[str, str] = {}
    for gate, skill_name in (gate_skills or {}).items():
        prompt = by_name.get(skill_name, "")
        if prompt.strip():
            out[gate] = prompt
    return out


async def _load_gate_rubrics(client: Any, artifact: dict[str, Any]) -> dict[str, str]:
    """Resolve the artifact's gate_skills bindings to rubric prompts (best-effort).

    Returns {} when nothing is bound or the skills fetch fails — gates then run with
    their built-in prompts.
    """
    gate_skills = _profile_gate_skills(artifact)
    if not gate_skills:
        return {}
    try:
        skills = await client.aget_skills()
    except Exception:
        logger.warning("failed to fetch skills for gate rubrics", exc_info=True)
        return {}
    return resolve_gate_rubrics(gate_skills, skills)


def _required_roles_evaluation(
    required_roles: list[str], present_roles: set[str]
) -> GateEvaluation:
    """Deterministic structural check: are the profile's required document roles present?"""
    missing = [r for r in required_roles if r not in present_roles]
    if missing:
        hint = "Missing required document role(s): " + ", ".join(missing) + "."
    else:
        hint = "All required document roles are present."
    return GateEvaluation(
        gate="required_roles_present",
        state="warn" if missing else "pass",
        hint=hint,
        confidence=1.0,
        evidence=json.dumps({"required_roles": required_roles, "missing_roles": missing}),
        judge_model="deterministic",
    )


async def run_llm_gates_for_artifact(artifact_id: str) -> dict[str, Any]:
    """Fetch one artifact, run its configured readiness gates, persist artifact-scoped rows."""
    client = DocRegistryClient(doc_registry_base_url())
    artifact = await client.aget_artifact(artifact_id)

    # Parse the profile snapshot once; fail-closed on unknown versions.
    try:
        snapshot = _parse_artifact_snapshot(artifact)
    except UnsupportedSnapshotVersion as exc:
        logger.warning(
            "artifact %s snapshot version unsupported (%s); "
            "returning blocking compatibility result",
            artifact_id,
            exc,
        )
        return {
            "artifact_id": artifact_id,
            "evaluations_posted": 0,
            "readiness_runs": [],
            "compatibility_error": str(exc),
        }

    governance_level = snapshot.governance_level if snapshot is not None else None
    enabled_gates = snapshot.enabled_gates or None if snapshot is not None else None
    required_topics = snapshot.required_topics or None if snapshot is not None else None
    required_roles = snapshot.required_roles or None if snapshot is not None else None

    evaluations: list[GateEvaluation] = []
    dispatched: dict[str, Any] | None = None

    # Deterministic structural check — required document roles present (no LLM needed).
    if artifact_id and required_roles:
        try:
            files = await client.aget_artifact_files(artifact_id)
            present = {
                str(f.get("role") or "").strip() for f in files if str(f.get("role") or "").strip()
            }
            evaluations.append(_required_roles_evaluation(required_roles, present))
        except Exception:
            logger.warning(
                "required-roles check failed for artifact %s", artifact_id, exc_info=True
            )

    if artifact_id:
        await _hydrate_model_settings()
        if ensure_llm_env():
            try:
                bundle = await client.aload_artifact_bundle_by_role(artifact_id)
                model = build_model()
                work_type = str(artifact.get("request_type") or "").strip()
                feature_id = str(artifact.get("feature_id") or "").strip()
                try:
                    attachments = (
                        await client.alist_feature_attachments(feature_id) if feature_id else []
                    )
                except Exception:
                    attachments = []
                gate_rubrics = await _load_gate_rubrics(client, artifact)
                evaluations.extend(
                    await evaluate_all_gates(
                        bundle,
                        model=model,
                        work_type=work_type,
                        attachments=attachments,
                        enabled_gates=enabled_gates,
                        required_topics=required_topics,
                        gate_rubrics=gate_rubrics,
                    )
                )
            except Exception:
                logger.warning(
                    "artifact readiness evaluation failed for artifact %s; posting empty snapshot",
                    artifact_id,
                    exc_info=True,
                )
        else:
            # No platform model configured — dispatch the LLM gates to the IDE
            # agent instead of evaluating server-side. The deterministic checks
            # already collected above still post below.
            try:
                dispatched = await client.adispatch_gate_tasks(artifact_id)
            except Exception:
                logger.warning(
                    "gate-task dispatch failed for artifact %s", artifact_id, exc_info=True
                )

    readiness_runs = await asyncio.to_thread(
        client.refresh_artifact_readiness_runs,
        artifact_id,
        [ev.model_dump() for ev in evaluations] if evaluations else None,
    )
    result: dict[str, Any] = {
        "artifact_id": artifact_id,
        "evaluations_posted": len(evaluations),
        "readiness_runs": readiness_runs,
    }
    if dispatched is not None:
        result["dispatched_to_ide_agent"] = dispatched
    if governance_level:
        result["governance_level"] = governance_level
    return result


async def run_llm_gates_for_change_request(change_request_id: str) -> dict[str, Any]:
    """Fetch the CR's lead artifact, run all LLM gates, post verdicts to Doc Registry.

    Falls back to an empty evaluation list when the LLM environment is not
    configured so the deterministic gates still refresh.
    """
    client = DocRegistryClient(doc_registry_base_url())
    change_request = await asyncio.to_thread(client.get_change_request, change_request_id)

    lead_artifact_id = str(change_request.get("lead_artifact_id") or "").strip()
    if not lead_artifact_id:
        feature_id = str(change_request.get("feature_id") or "").strip()
        if feature_id:
            try:
                feature = await asyncio.to_thread(client.get_workboard_feature, feature_id)
                lead_artifact_id = str(feature.get("canonical_artifact_id") or "").strip()
            except Exception:
                pass

    evaluations: list[GateEvaluation] = []
    dispatched: dict[str, Any] | None = None
    lead_artifact: dict[str, Any] = {}
    if lead_artifact_id:
        await _hydrate_model_settings()
        if ensure_llm_env():
            try:
                bundle = await client.aload_artifact_bundle_by_role(lead_artifact_id)
                model = build_model()
                work_type = str(change_request.get("work_type") or "").strip()
                # Attachments are keyed by the feature KEY (= the artifact's
                # feature_id field), not the change-request's feature UUID. Read
                # the key off the artifact so gate review actually sees them.
                try:
                    lead_artifact = await client.aget_artifact(lead_artifact_id)
                    attachments = await client.alist_feature_attachments(
                        str(lead_artifact.get("feature_id") or "")
                    )
                except Exception:
                    lead_artifact = {}
                    attachments = []
                gate_rubrics = await _load_gate_rubrics(client, lead_artifact)
                evaluations = await evaluate_all_gates(
                    bundle,
                    model=model,
                    work_type=work_type,
                    attachments=attachments,
                    gate_rubrics=gate_rubrics,
                )
            except Exception:
                logger.warning(
                    "LLM gate evaluation failed for CR %s; posting deterministic-only refresh",
                    change_request_id,
                    exc_info=True,
                )
        else:
            # No platform model configured — dispatch the LLM gates to the IDE agent.
            try:
                dispatched = await client.adispatch_gate_tasks(lead_artifact_id)
            except Exception:
                logger.warning(
                    "gate-task dispatch failed for CR %s (artifact %s)",
                    change_request_id,
                    lead_artifact_id,
                    exc_info=True,
                )

    # Parse governance_level from the lead artifact's profile snapshot (best-effort).
    governance_level: str | None = None
    if lead_artifact_id:
        try:
            _artifact_for_snapshot = (
                lead_artifact if lead_artifact else await client.aget_artifact(lead_artifact_id)
            )
            _snapshot = _parse_artifact_snapshot(_artifact_for_snapshot)
            if _snapshot is not None:
                governance_level = _snapshot.governance_level
        except Exception:
            pass

    gate_runs = await asyncio.to_thread(
        client.refresh_change_request_gate_runs,
        change_request_id,
        [ev.model_dump() for ev in evaluations] if evaluations else None,
    )
    result: dict[str, Any] = {
        "change_request_id": change_request_id,
        "lead_artifact_id": lead_artifact_id,
        "evaluations_posted": len(evaluations),
        "gate_runs": gate_runs,
        "governance_level": governance_level or "",
    }
    if dispatched is not None:
        result["dispatched_to_ide_agent"] = dispatched
    return result
