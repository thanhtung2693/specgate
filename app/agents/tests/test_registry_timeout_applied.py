"""The configured registry timeout must reach the HTTP client.

`governance.registry_timeout_seconds` is a validated server setting, so storing
the hydrated value is not enough: every governance operation that talks to Doc
Registry has to pass it. Asserting only the getter would pass while every client
still used its hardcoded default.
"""

from __future__ import annotations

import pathlib

import pytest


@pytest.fixture(autouse=True)
def _reset() -> None:
    from specgate_agents.governance.provider_keys import clear_provider_api_keys

    clear_provider_api_keys()
    yield
    clear_provider_api_keys()


def test_hydrated_timeout_reaches_a_constructed_client() -> None:
    from specgate_agents.governance import provider_keys as pk
    from specgate_agents.governance.registry.client import DocRegistryClient

    pk.set_provider_api_keys_from_settings({"governance.registry_timeout_seconds": "45"})
    client = DocRegistryClient(
        "http://registry.test", timeout_s=pk.governance_registry_timeout_seconds()
    )

    assert client._timeout == 45.0


def test_every_governance_operation_passes_the_configured_timeout() -> None:
    """A path that constructs its own client without the timeout silently keeps
    the in-code default, which is the defect this guards."""
    root = pathlib.Path(__file__).resolve().parents[1] / "src" / "specgate_agents" / "governance"
    offenders: list[str] = []
    for path in sorted(root.rglob("*.py")):
        lines = path.read_text().splitlines()
        for number, line in enumerate(lines, start=1):
            if "DocRegistryClient(" not in line or "import" in line:
                continue
            # Reading the settings that define the timeout necessarily predates
            # hydration, so those calls accept the default.
            if "settings_unmasked_for_governance" in line:
                continue
            statement = " ".join(lines[number - 1 : number + 2])
            if "timeout_s=" not in statement:
                offenders.append(f"{path.name}:{number}")
    assert offenders == [], (
        f"these paths construct DocRegistryClient without the configured timeout: {offenders}"
    )
