"""Dual-version governance snapshot parser.

Mirrors the Go ``governanceprofile.ParseSnapshot`` semantics:

- Empty input  → zero ``ProfileSnapshot``, no error.
- No ``snapshot_schema_version`` field (or explicit ``"legacy/v1"``) → legacy path.
- ``"specgate.policy/v1"`` → v1 path with ``governance_level`` projection.
- Any other explicit version → raises ``UnsupportedSnapshotVersion`` (fail-closed).
- Corrupt JSON → raises the relevant ``json.JSONDecodeError``.

See ``app/doc-registry/internal/governanceprofile/snapshot.go`` for the canonical
reference implementation.
"""

from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any

SCHEMA_LEGACY_V1 = "legacy/v1"
SCHEMA_POLICY_V1 = "specgate.policy/v1"


class UnsupportedSnapshotVersion(Exception):
    """Raised when the snapshot carries an explicit version we do not recognise.

    Callers at the HTTP/service boundary must treat this as fail-closed (i.e.
    return a blocking compatibility result rather than falling back silently).
    """


@dataclass(frozen=True)
class ProfileSnapshot:
    """Unified execution-projection of a governance gates profile snapshot.

    Holds only the fields that downstream logic (approval gate, readiness engine,
    context-pack) needs, regardless of which snapshot schema version was stored.

    ``governance_level`` is only populated for ``specgate.policy/v1`` snapshots;
    it is ``None`` for legacy snapshots.
    """

    schema_version: str
    governance_level: str | None
    enabled_gates: list[str]
    required_topics: list[str]
    required_roles: list[str]
    gate_skills: dict[str, str]
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


def parse_profile_snapshot(raw: str) -> ProfileSnapshot:
    """Parse a governance gates profile snapshot JSON string.

    Returns a ``ProfileSnapshot``. Raises ``UnsupportedSnapshotVersion`` for
    unknown explicit versions. Raises ``json.JSONDecodeError`` for corrupt JSON
    (fail-closed on corruption, same as the Go reference).
    """
    if not raw or not raw.strip():
        return ProfileSnapshot(
            schema_version=SCHEMA_LEGACY_V1,
            governance_level=None,
            enabled_gates=[],
            required_topics=[],
            required_roles=[],
            gate_skills={},
            approval_policy="",
            evidence_policy="",
        )

    # Peek at the discriminator only — mirrors Go's snapshotProbe.
    probe = json.loads(raw)
    if not isinstance(probe, dict):
        # Non-object JSON is corrupt for our purposes.
        raise json.JSONDecodeError("snapshot must be a JSON object", raw, 0)

    schema_version = str(probe.get("snapshot_schema_version") or "").strip()

    if schema_version in ("", SCHEMA_LEGACY_V1):
        return _parse_legacy(probe)
    if schema_version == SCHEMA_POLICY_V1:
        return _parse_policy_v1(probe)

    raise UnsupportedSnapshotVersion(f"unsupported snapshot schema version: {schema_version!r}")


def _parse_legacy(data: dict[str, Any]) -> ProfileSnapshot:
    """Project a legacy ResolvedProfile JSON dict into ``ProfileSnapshot``."""
    return ProfileSnapshot(
        schema_version=SCHEMA_LEGACY_V1,
        governance_level=None,
        enabled_gates=_str_list(data.get("enabled_gates")),
        required_topics=_str_list(data.get("required_topics")),
        required_roles=_str_list(data.get("required_roles")),
        gate_skills=_str_dict(data.get("gate_skills")),
        approval_policy=str(data.get("approval_policy") or "").strip(),
        evidence_policy=str(data.get("evidence_policy") or "").strip(),
    )


def _parse_policy_v1(data: dict[str, Any]) -> ProfileSnapshot:
    """Project a ``specgate.policy/v1`` envelope into ``ProfileSnapshot``."""
    governance_level = str(data.get("governance_level") or "").strip() or None
    return ProfileSnapshot(
        schema_version=SCHEMA_POLICY_V1,
        governance_level=governance_level,
        enabled_gates=_str_list(data.get("enabled_gates")),
        required_topics=_str_list(data.get("required_topics")),
        required_roles=_str_list(data.get("required_roles")),
        gate_skills=_str_dict(data.get("gate_skills")),
        approval_policy=str(data.get("approval_policy") or "").strip(),
        evidence_policy=str(data.get("evidence_policy") or "").strip(),
    )
