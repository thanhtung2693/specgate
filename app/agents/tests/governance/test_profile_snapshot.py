"""Tests for the versioned policy-v1 profile snapshot parser."""

from __future__ import annotations

import json
from dataclasses import FrozenInstanceError

import pytest

from specgate_agents.governance.quality_gates.profile_snapshot import (
    UnsupportedSnapshotVersion,
    parse_profile_snapshot,
)


def test_parse_empty_input_returns_defaults() -> None:
    snap = parse_profile_snapshot("")
    assert snap.governance_level is None
    assert snap.schema_version == ""
    assert snap.enabled_gates == []
    assert snap.required_roles == []
    assert snap.required_topics == []
    assert snap.gate_skills == {}


@pytest.mark.parametrize("snapshot", [{}, {"snapshot_schema_version": "legacy/v1"}])
def test_non_versioned_or_legacy_snapshot_fails_closed(snapshot: dict[str, object]) -> None:
    with pytest.raises(UnsupportedSnapshotVersion):
        parse_profile_snapshot(json.dumps(snapshot))


def test_parse_policy_v1_keeps_execution_projection() -> None:
    snapshot = parse_profile_snapshot(
        json.dumps(
            {
                "snapshot_schema_version": "specgate.policy/v1",
                "governance_level": "enhanced",
                "enabled_gates": ["spec_completeness"],
                "required_topics": ["outcomes"],
                "required_roles": ["spec"],
                "gate_skills": {"spec_completeness": "spec-review"},
                "approval_policy": "human_required",
                "evidence_policy": "attested_ok",
            }
        )
    )
    assert snapshot.governance_level == "enhanced"
    assert snapshot.enabled_gates == ["spec_completeness"]
    assert snapshot.schema_version == "specgate.policy/v1"
    assert snapshot.required_topics == ["outcomes"]
    assert snapshot.required_roles == ["spec"]
    assert snapshot.gate_skills == {"spec_completeness": "spec-review"}


def test_parse_policy_v1_standard_level() -> None:
    snap = parse_profile_snapshot(
        json.dumps(
            {
                "snapshot_schema_version": "specgate.policy/v1",
                "governance_level": "standard",
                "enabled_gates": ["scope_clear", "spec_completeness"],
                "required_roles": ["spec"],
                "required_topics": ["outcomes", "acceptance_criteria"],
                "approval_policy": "human_required",
                "evidence_policy": "attested_ok",
            }
        )
    )
    assert snap.governance_level == "standard"
    assert snap.schema_version == "specgate.policy/v1"
    assert snap.approval_policy == "human_required"
    assert snap.evidence_policy == "attested_ok"


def test_unknown_snapshot_version_fails_closed() -> None:
    with pytest.raises(UnsupportedSnapshotVersion):
        parse_profile_snapshot('{"snapshot_schema_version":"future/v9"}')


def test_unknown_snapshot_version_v2_fails_closed() -> None:
    with pytest.raises(UnsupportedSnapshotVersion):
        parse_profile_snapshot('{"snapshot_schema_version":"specgate.policy/v2"}')


def test_corrupt_json_raises() -> None:
    with pytest.raises(json.JSONDecodeError):
        parse_profile_snapshot("{not valid json}")


def test_profile_snapshot_is_frozen() -> None:
    snap = parse_profile_snapshot("")
    with pytest.raises(FrozenInstanceError):
        snap.governance_level = "new_value"  # type: ignore[misc]


def test_profile_snapshot_requires_policy_fields() -> None:
    with pytest.raises(ValueError, match="approval_policy is required"):
        parse_profile_snapshot(
            json.dumps(
                {
                    "snapshot_schema_version": "specgate.policy/v1",
                    "governance_level": "light",
                }
            )
        )
