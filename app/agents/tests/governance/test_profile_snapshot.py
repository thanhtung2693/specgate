"""Tests for profile_snapshot parser — dual-version (legacy/v1, specgate.policy/v1) support."""

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
    assert snap.schema_version == "legacy/v1"
    assert snap.enabled_gates == []
    assert snap.required_roles == []
    assert snap.required_topics == []
    assert snap.gate_skills == {}


def test_parse_legacy_snapshot_no_schema_version() -> None:
    """A snapshot with no snapshot_schema_version is treated as legacy/v1."""
    snap = parse_profile_snapshot(
        json.dumps(
            {
                "enabled_gates": ["spec_completeness", "scope_clear"],
                "required_topics": ["outcomes"],
                "required_roles": ["spec", "design"],
                "gate_skills": {"scope_clear": "prd-review"},
            }
        )
    )
    assert snap.schema_version == "legacy/v1"
    assert snap.governance_level is None
    assert snap.enabled_gates == ["spec_completeness", "scope_clear"]
    assert snap.required_topics == ["outcomes"]
    assert snap.required_roles == ["spec", "design"]
    assert snap.gate_skills == {"scope_clear": "prd-review"}


def test_parse_explicit_legacy_v1_schema_version() -> None:
    """snapshot_schema_version == "legacy/v1" is also treated as legacy."""
    snap = parse_profile_snapshot(
        json.dumps(
            {
                "snapshot_schema_version": "legacy/v1",
                "enabled_gates": ["rollback_plan_present"],
                "required_roles": ["spec"],
                "required_topics": [],
            }
        )
    )
    assert snap.schema_version == "legacy/v1"
    assert snap.governance_level is None
    assert snap.enabled_gates == ["rollback_plan_present"]


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
                "evidence_policy": "optional",
            }
        )
    )
    assert snap.governance_level == "standard"
    assert snap.schema_version == "specgate.policy/v1"
    assert snap.approval_policy == "human_required"
    assert snap.evidence_policy == "optional"


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


def test_profile_snapshot_missing_fields_default_to_empty() -> None:
    """Snapshot with only required keys; optional keys default gracefully."""
    snap = parse_profile_snapshot(
        json.dumps(
            {
                "snapshot_schema_version": "specgate.policy/v1",
                "governance_level": "light",
            }
        )
    )
    assert snap.governance_level == "light"
    assert snap.enabled_gates == []
    assert snap.required_roles == []
    assert snap.required_topics == []
    assert snap.gate_skills == {}
    assert snap.approval_policy == ""
    assert snap.evidence_policy == ""
