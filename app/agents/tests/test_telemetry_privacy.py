from __future__ import annotations

from specgate_agents import configure_telemetry_privacy_defaults


def test_telemetry_privacy_defaults_hide_trace_payloads_without_overriding_operator() -> None:
    env = {"LANGSMITH_HIDE_INPUTS": "false"}

    configure_telemetry_privacy_defaults(env)

    assert env["LANGSMITH_HIDE_INPUTS"] == "false"
    assert env["LANGSMITH_HIDE_OUTPUTS"] == "true"
