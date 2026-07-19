"""Runtime policy settings hydrated from Doc Registry.

The gate confidence threshold and registry timeout are user-configurable
(settings table) instead of hardcoded constants. They ride the same hydration path as provider keys
(`set_provider_api_keys_from_settings`) and fall back to the in-code default when
unset or invalid.
"""

from __future__ import annotations

import pytest


@pytest.fixture(autouse=True)
def _reset() -> None:
    from specgate_agents.governance.provider_keys import clear_provider_api_keys

    clear_provider_api_keys()
    yield
    clear_provider_api_keys()


def test_defaults_match_documented_policy() -> None:
    from specgate_agents.governance import provider_keys as pk

    assert pk.governance_gate_confidence_threshold() == 0.7
    assert pk.governance_registry_timeout_seconds() == 30.0


def test_hydration_overrides_defaults() -> None:
    from specgate_agents.governance import provider_keys as pk

    pk.set_provider_api_keys_from_settings(
        {
            "governance.gate_confidence_threshold": "0.85",
            "governance.registry_timeout_seconds": "45",
        }
    )
    assert pk.governance_gate_confidence_threshold() == 0.85
    assert pk.governance_registry_timeout_seconds() == 45.0


def test_invalid_or_blank_values_keep_default() -> None:
    from specgate_agents.governance import provider_keys as pk

    pk.set_provider_api_keys_from_settings(
        {
            "governance.gate_confidence_threshold": "not-a-number",
        }
    )
    assert pk.governance_gate_confidence_threshold() == 0.7


def test_gate_resolution_uses_configured_threshold() -> None:
    """A pass below the configured floor downgrades to needs_human_review."""
    from specgate_agents.governance import provider_keys as pk
    from specgate_agents.governance.quality_gates.judge import resolve_gate_state

    # Default 0.7: a 0.6-confidence pass downgrades.
    assert resolve_gate_state("pass", 0.6) == "needs_human_review"
    # Lower the floor to 0.5: the same 0.6 pass now stands.
    pk.set_provider_api_keys_from_settings({"governance.gate_confidence_threshold": "0.5"})
    assert resolve_gate_state("pass", 0.6) == "pass"
