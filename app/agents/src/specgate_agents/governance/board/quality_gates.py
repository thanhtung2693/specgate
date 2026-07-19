"""Run model-judged quality gates and persist readiness snapshots to Doc Registry."""

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


def _workspace_kw(workspace_id: str | None) -> dict[str, str]:
    workspace = str(workspace_id or "").strip()
    if not workspace:
        raise ValueError("workspace_id is required")
    return {"workspace_id": workspace}


def _parse_artifact_snapshot(artifact: dict[str, Any]) -> ProfileSnapshot | None:
    """Parse the artifact's policy_snapshot_json.

    Returns ``None`` on empty/missing input (callers fall back to defaults).
    Raises for corrupt input or unsupported explicit versions so operation
    boundaries can return a blocking compatibility result without persisting.
    """
    raw = artifact.get("policy_snapshot_json")
    if raw is None or (isinstance(raw, str) and not raw.strip()):
        return None
    if not isinstance(raw, str):
        raise json.JSONDecodeError("policy snapshot must be a JSON string", "", 0)
    return parse_profile_snapshot(raw)


def _snapshot_compatibility_error(exc: Exception) -> str:
    if isinstance(exc, UnsupportedSnapshotVersion):
        return "unsupported_policy_snapshot_version"
    return "invalid_policy_snapshot"


def _frozen_gate_rubrics(artifact: dict[str, Any]) -> dict[str, str]:
    """Extract gate rubric content frozen into the artifact policy snapshot."""
    try:
        snapshot = _parse_artifact_snapshot(artifact)
    except (UnsupportedSnapshotVersion, json.JSONDecodeError, ValueError):
        return {}
    if snapshot is None:
        return {}
    return {
        definition["key"]: definition["skill_content"]
        for definition in snapshot.gate_definitions
        if definition.get("key") and str(definition.get("skill_content") or "").strip()
    }


def _required_roles_evaluation(
    required_roles: list[str], present_roles: set[str]
) -> GateEvaluation:
    """Deterministic structural check: are the policy's required document roles present?"""
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


async def run_llm_gates_for_artifact(
    artifact_id: str, *, workspace_id: str | None = None
) -> dict[str, Any]:
    """Fetch one artifact, run its configured readiness gates, persist artifact-scoped rows."""
    workspace_kw = _workspace_kw(workspace_id)
    client = DocRegistryClient(doc_registry_base_url())
    artifact = await client.aget_artifact(artifact_id, **workspace_kw)

    # Parse once; every non-empty corrupt or unsupported snapshot fails closed.
    try:
        snapshot = _parse_artifact_snapshot(artifact)
    except (UnsupportedSnapshotVersion, json.JSONDecodeError, ValueError) as exc:
        logger.warning(
            "artifact %s policy snapshot incompatible; returning blocking result",
            artifact_id,
        )
        return {
            "artifact_id": artifact_id,
            "evaluations_posted": 0,
            "readiness_runs": [],
            "compatibility_error": _snapshot_compatibility_error(exc),
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
            files = await client.aget_artifact_files(artifact_id, **workspace_kw)
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
                bundle = await client.aload_artifact_bundle_by_role(artifact_id, **workspace_kw)
                model = build_model()
                work_type = str(artifact.get("request_type") or "").strip()
                feature_id = str(artifact.get("feature_id") or "").strip()
                try:
                    attachments = (
                        await client.alist_feature_attachments(feature_id, **workspace_kw)
                        if feature_id
                        else []
                    )
                except Exception:
                    attachments = []
                gate_rubrics = _frozen_gate_rubrics(artifact)
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
            # No platform model configured — dispatch model-judged gates to the
            # IDE agent instead of evaluating server-side. The deterministic
            # checks already collected above still post below.
            try:
                dispatched = await client.adispatch_gate_tasks(artifact_id, **workspace_kw)
            except Exception:
                logger.warning(
                    "gate-task dispatch failed for artifact %s", artifact_id, exc_info=True
                )

    readiness_runs = await asyncio.to_thread(
        client.refresh_artifact_readiness_runs,
        artifact_id,
        [ev.model_dump() for ev in evaluations] if evaluations else None,
        **workspace_kw,
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


async def run_llm_gates_for_change_request(
    change_request_id: str, *, workspace_id: str = ""
) -> dict[str, Any]:
    """Fetch the CR lead artifact, run model-judged gates, post verdicts to Doc Registry.

    Falls back to an empty evaluation list when the platform model is not
    configured so the deterministic gates still refresh.
    """
    workspace_kw = _workspace_kw(workspace_id)
    client = DocRegistryClient(doc_registry_base_url())
    change_request = await asyncio.to_thread(
        client.get_change_request, change_request_id, **workspace_kw
    )

    lead_artifact_id = str(change_request.get("lead_artifact_id") or "").strip()
    if not lead_artifact_id:
        feature_id = str(change_request.get("feature_id") or "").strip()
        if feature_id:
            try:
                feature = await asyncio.to_thread(
                    client.get_workboard_feature, feature_id, **workspace_kw
                )
                lead_artifact_id = str(feature.get("canonical_artifact_id") or "").strip()
            except Exception:
                pass

    evaluations: list[GateEvaluation] = []
    dispatched: dict[str, Any] | None = None
    lead_artifact: dict[str, Any] = {}
    snapshot: ProfileSnapshot | None = None
    if lead_artifact_id:
        lead_artifact = await client.aget_artifact(lead_artifact_id, **workspace_kw)
        try:
            snapshot = _parse_artifact_snapshot(lead_artifact)
        except (UnsupportedSnapshotVersion, json.JSONDecodeError, ValueError) as exc:
            logger.warning(
                "CR %s lead artifact policy snapshot incompatible; returning blocking result",
                change_request_id,
            )
            return {
                "change_request_id": change_request_id,
                "lead_artifact_id": lead_artifact_id,
                "evaluations_posted": 0,
                "gate_runs": [],
                "governance_level": "",
                "compatibility_error": _snapshot_compatibility_error(exc),
            }

        await _hydrate_model_settings()
        if ensure_llm_env():
            try:
                bundle = await client.aload_artifact_bundle_by_role(
                    lead_artifact_id, **workspace_kw
                )
                model = build_model()
                work_type = str(change_request.get("work_type") or "").strip()
                # Attachments are keyed by the feature KEY (= the artifact's
                # feature_id field), not the change-request's feature UUID. Read
                # the key off the artifact so gate review actually sees them.
                try:
                    attachments = await client.alist_feature_attachments(
                        str(lead_artifact.get("feature_id") or ""), **workspace_kw
                    )
                except Exception:
                    attachments = []
                gate_rubrics = _frozen_gate_rubrics(lead_artifact)
                # Same profile discipline as the artifact-level path: the lead
                # artifact's snapshot decides which gates run and which topics
                # are required (per spec: policy selects gates on every path).
                enabled_gates = snapshot.enabled_gates or None if snapshot is not None else None
                required_topics = snapshot.required_topics or None if snapshot is not None else None
                evaluations = await evaluate_all_gates(
                    bundle,
                    model=model,
                    work_type=work_type,
                    attachments=attachments,
                    enabled_gates=enabled_gates,
                    required_topics=required_topics,
                    gate_rubrics=gate_rubrics,
                )
            except Exception:
                logger.warning(
                    "model-judged gate evaluation failed for CR %s; "
                    "posting deterministic-only refresh",
                    change_request_id,
                    exc_info=True,
                )
        else:
            # No platform model configured — dispatch model-judged gates to the IDE agent.
            try:
                dispatched = await client.adispatch_gate_tasks(lead_artifact_id, **workspace_kw)
            except Exception:
                logger.warning(
                    "gate-task dispatch failed for CR %s (artifact %s)",
                    change_request_id,
                    lead_artifact_id,
                    exc_info=True,
                )

    governance_level = snapshot.governance_level if snapshot is not None else None

    gate_runs = await asyncio.to_thread(
        client.refresh_change_request_gate_runs,
        change_request_id,
        [ev.model_dump() for ev in evaluations] if evaluations else None,
        **workspace_kw,
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
