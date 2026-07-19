"""Versioned governance snapshot parser.

Mirrors the Go ``governanceprofile.ParseSnapshot`` semantics:

- Empty input  → zero ``ProfileSnapshot``, no error.
- ``"specgate.policy/v1"`` → v1 path with ``governance_level`` projection.
- Missing or any other version → raises ``UnsupportedSnapshotVersion`` (fail-closed).
- Corrupt JSON → raises the relevant ``json.JSONDecodeError``.

See ``app/doc-registry/internal/governanceprofile/snapshot.go`` for the canonical
reference implementation.
"""

from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any

SCHEMA_POLICY_V1 = "specgate.policy/v1"


class UnsupportedSnapshotVersion(Exception):
    """Raised when the snapshot carries an explicit version we do not recognise.

    Callers at the HTTP/service boundary must treat this as fail-closed (i.e.
    return a blocking compatibility result rather than falling back silently).
    """


@dataclass(frozen=True)
class ProfileSnapshot:
    """Unified execution-projection of a governance policy snapshot.

    Holds only the fields that downstream logic (approval gate, readiness engine,
    context-pack) needs, regardless of which snapshot schema version was stored.

    ``governance_level`` is optional in ``specgate.policy/v1`` snapshots.
    """

    schema_version: str
    governance_level: str | None
    enabled_gates: list[str]
    required_topics: list[str]
    required_roles: list[str]
    gate_skills: dict[str, str]
    gate_definitions: list[dict[str, str]]
    approval_policy: str
    evidence_policy: str


def _str_list(value: Any) -> list[str]:
    """Coerce a JSON array to a list of non-empty trimmed strings."""
    if not isinstance(value, list):
        return []
    return [str(item).strip() for item in value if str(item).strip()]


def _str_dict(value: Any) -> dict[str, str]:
    """Coerce a JSON object to a dict of non-empty trimmed string pairs."""
    if not isinstance(value, dict):
        return {}
    out: dict[str, str] = {}
    for key, val in value.items():
        ks, vs = str(key).strip(), str(val).strip()
        if ks and vs:
            out[ks] = vs
    return out


def _gate_definitions(value: Any) -> list[dict[str, str]]:
    """Coerce frozen gate-definition objects to their string execution fields."""
    if not isinstance(value, list):
        return []
    out: list[dict[str, str]] = []
    for item in value:
        if not isinstance(item, dict):
            continue
        definition = {
            key: str(item.get(key) or "")
            for key in ("key", "version", "skill_name", "skill_content", "skill_digest")
        }
        if definition["key"] and definition["version"]:
            out.append(definition)
    return out


def parse_profile_snapshot(raw: str) -> ProfileSnapshot:
    """Parse a governance policy snapshot JSON string.

    Returns a ``ProfileSnapshot``. Raises ``UnsupportedSnapshotVersion`` for
    unknown explicit versions. Raises ``json.JSONDecodeError`` for corrupt JSON
    (fail-closed on corruption, same as the Go reference).
    """
    if not raw or not raw.strip():
        return ProfileSnapshot(
            schema_version="",
            governance_level=None,
            enabled_gates=[],
            required_topics=[],
            required_roles=[],
            gate_skills={},
            gate_definitions=[],
            approval_policy="",
            evidence_policy="",
        )

    # Peek at the discriminator only — mirrors Go's snapshotProbe.
    probe = json.loads(raw)
    if not isinstance(probe, dict):
        # Non-object JSON is corrupt for our purposes.
        raise json.JSONDecodeError("snapshot must be a JSON object", raw, 0)

    schema_version = str(probe.get("snapshot_schema_version") or "").strip()

    if schema_version == SCHEMA_POLICY_V1:
        return _parse_policy_v1(probe)

    raise UnsupportedSnapshotVersion(f"unsupported snapshot schema version: {schema_version!r}")


def _parse_policy_v1(data: dict[str, Any]) -> ProfileSnapshot:
    """Project a ``specgate.policy/v1`` envelope into ``ProfileSnapshot``."""
    governance_level = str(data.get("governance_level") or "").strip() or None
    approval_policy = str(data.get("approval_policy") or "").strip()
    if not approval_policy:
        raise ValueError("approval_policy is required in a persisted policy snapshot")
    if approval_policy != "human_required":
        raise ValueError(f"unsupported approval policy: {approval_policy!r}")
    evidence_policy = str(data.get("evidence_policy") or "").strip()
    if not evidence_policy:
        raise ValueError("evidence_policy is required in a persisted policy snapshot")
    if evidence_policy not in {"attested_ok", "corroborated_required"}:
        raise ValueError(f"unsupported evidence policy: {evidence_policy!r}")
    return ProfileSnapshot(
        schema_version=SCHEMA_POLICY_V1,
        governance_level=governance_level,
        enabled_gates=_str_list(data.get("enabled_gates")),
        required_topics=_str_list(data.get("required_topics")),
        required_roles=_str_list(data.get("required_roles")),
        gate_skills=_str_dict(data.get("gate_skills")),
        gate_definitions=_gate_definitions(data.get("gate_definitions")),
        approval_policy=approval_policy,
        evidence_policy=evidence_policy,
    )
